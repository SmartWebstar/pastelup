package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pastelnetwork/gonode/common/errors"
	"github.com/pastelnetwork/gonode/common/log"
	"github.com/pastelnetwork/pastel-utility/configs"
	"github.com/pastelnetwork/pastel-utility/constants"
	"github.com/pastelnetwork/pastel-utility/structure"
	"github.com/pastelnetwork/pastel-utility/utils"
)

// TODO: Remove the use of shadowing global variables and decouple
// this part from rest of the code for better maintenance of codebase

// ColdHotRunnerOpts defines opts for ColdHotRunner
type ColdHotRunnerOpts struct {
	// ssh params
	sshUser string
	sshIP   string
	sshPort int
	sshKey  string

	testnetOption string

	// remote paths
	remotePastelUtility string
	remotePasteld       string
	remotePastelCli     string
}

// ColdHotRunner starts sn in coldhot mode
type ColdHotRunner struct {
	sshClient *utils.Client
	config    *configs.Config
	opts      *ColdHotRunnerOpts
}

// Init initiates coldhot runner
func (r *ColdHotRunner) Init(ctx context.Context) error {
	if err := r.handleArgs(); err != nil {
		return fmt.Errorf("parse args: %s", err)
	}

	if err := r.handleConfigs(ctx); err != nil {
		return fmt.Errorf("parse args: %s", err)
	}

	client, err := connectSSH(ctx, r.opts.sshUser, r.opts.sshIP, r.opts.sshPort, r.opts.sshKey)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to connect with remote via SSH")
		return fmt.Errorf("ssh connection failure: %s", err)
	}
	r.sshClient = client

	// ***************** get external ip addr ***************
	if flagNodeExtIP == "" {
		out, err := client.Cmd(fmt.Sprintf("curl %s", "http://ipinfo.io/ip")).Output()
		if err != nil {
			return fmt.Errorf("failure in getting ext ip of remote %s", err)
		}

		flagNodeExtIP = string(out)
	}

	return nil
}

func (r *ColdHotRunner) handleArgs() (err error) {
	if len(r.config.RemotePastelUtilityDir) == 0 {
		return fmt.Errorf("cannot find remote pastel-utility dir")
	}

	if len(r.config.RemotePastelExecDir) == 0 {
		r.config.RemotePastelExecDir = r.config.Configurer.DefaultPastelExecutableDir()
	}

	r.opts.remotePastelCli = filepath.Join(r.config.RemotePastelExecDir, constants.PastelCliName[utils.GetOS()])
	r.opts.remotePastelCli = strings.ReplaceAll(r.opts.remotePastelCli, "\\", "/")

	r.opts.remotePasteld = filepath.Join(r.config.RemotePastelExecDir, constants.PasteldName[utils.GetOS()])
	r.opts.remotePasteld = strings.ReplaceAll(r.opts.remotePasteld, "\\", "/")

	r.opts.remotePastelUtility = filepath.Join(r.config.RemotePastelUtilityDir, "pastel-utility")
	r.opts.remotePastelUtility = strings.ReplaceAll(r.opts.remotePastelUtility, "\\", "/")

	return nil
}

func (r *ColdHotRunner) handleConfigs(ctx context.Context) error {
	log.WithContext(ctx).Infof("reading pastel.conf")
	// Check pastel config for testnet option and set config.Network
	if err := ParsePastelConf(ctx, r.config); err != nil {
		return fmt.Errorf("parse pastel.conf: %s", err)
	}

	if r.config.Network == constants.NetworkTestnet {
		r.opts.testnetOption = " --testnet"
	}
	log.WithContext(ctx).Infof("Finished Reading pastel.conf! Starting node in %s mode", r.config.Network)

	log.WithContext(ctx).Infof("checking masternode start params")
	// Check masternode params like sshIP, mnName, extIP, masternode conf, assign ports
	if err := checkStartMasterNodeParams(ctx, r.config, true); err != nil {
		return fmt.Errorf("checkStartMasterNodeParams: %s", err)
	}
	log.WithContext(ctx).Infof("finished checking masternode start params")

	return nil
}

// Run starts coldhot runner
func (r *ColdHotRunner) Run(ctx context.Context) (err error) {

	// ***************  1. Start the local Pastel Network Node ***************
	log.WithContext(ctx).Infof("Starting pasteld")
	if err = runPastelNode(ctx, r.config, true, "", ""); err != nil {
		log.WithContext(ctx).WithError(err).Error("pasteld failed to start")
		return err
	}

	// ***************  2. If flag --create or --update is provided ***************
	if flagMasterNodeIsCreate || flagMasterNodeIsUpdate {
		log.WithContext(ctx).Info("Prepare mastenode parameters")
		if err := r.handleCreateUpdateStartColdHot(ctx); err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to validate and prepare masternode parameters")
			return err
		}
		if flagMasterNodeP2PIP == "" {
			flagMasterNodeP2PIP = flagNodeExtIP
		}
		if err := createOrUpdateMasternodeConf(ctx, r.config); err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to create or update masternode.conf")
			return err
		}
		if err := createOrUpdateSuperNodeConfig(ctx, r.config); err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to update supernode.yml")
			return err
		}
	}

	if err = StopPastelDAndWait(ctx, r.config); err != nil {
		log.WithContext(ctx).WithError(err).Error("unable to stop local node")
		return err
	}

	// ***************  3. Execute following commands over SSH on the remote node (using ssh-ip and ssh-port)  ***************

	if err = r.remoteHotNodeCtrl(ctx); err != nil {
		log.WithContext(ctx).WithError(err).Error("failed on remoteHotNodeCtrl")
		return err
	}
	log.WithContext(ctx).Info("The hot wallet node has been successfully launched!")

	//Get conf data from masternode.conf File
	privkey, _, _, err := getMasternodeConfData(ctx, r.config, flagMasterNodeName)
	if err != nil {
		return err
	}
	flagMasterNodePrivateKey = privkey

	if err := r.runRemoteNodeAsMasterNode(ctx); err != nil {
		log.WithContext(ctx).WithError(err).Error("unable to run remote as masternode")
		return fmt.Errorf("run remote as masternode: %s", err)
	}
	log.WithContext(ctx).Info("remote node started as masternode successfully..")

	log.WithContext(ctx).Info("restart cold node..")
	if err = runPastelNode(ctx, r.config, true, "", ""); err != nil {
		log.WithContext(ctx).WithError(err).Error("pasteld failed to start")
		return err
	}

	// ***************  4. If --activate are provided, ***************
	if flagMasterNodeIsActivate {
		log.WithContext(ctx).Info("found --activate flag, checking local node sync..")
		if err = CheckMasterNodeSync(ctx, r.config); err != nil {
			log.WithError(err).Error("local Masternode sync failure")
			return err
		}

		log.WithContext(ctx).Info("now activating mn...")
		if err = runStartAliasMasternode(ctx, r.config, flagMasterNodeName); err != nil {
			log.WithError(err).Error("Masternode activation failure")
			return fmt.Errorf("masternode activation failed: %s", err)
		}

		if flagMasterNodeIsCreate {
			log.WithContext(ctx).Info("registering pastelID ticket...")
			if err := r.registerTicketPastelID(ctx); err != nil {
				log.WithContext(ctx).WithError(err).Error("unable to register pastelID ticket")
			}
		}
	}

	log.WithContext(ctx).Info("stopping cold node..")
	// ***************  5. Stop Cold Node  ***************
	if err = StopPastelDAndWait(ctx, r.config); err != nil {
		log.WithContext(ctx).WithError(err).Error("unable to stop local node")
		return err
	}

	// *************  6. Start rq-servce    *************
	log.WithContext(ctx).Info("starting rq-service..")
	if err = r.runServiceRemote(ctx, string(constants.RQService)); err != nil {
		return fmt.Errorf("failed to start rq-service on hot node: %s", err)
	}
	log.WithContext(ctx).Info("rq-service started successfully")

	// *************  Start dd-servce    *************
	log.WithContext(ctx).Info("starting dd-service..")
	if err = r.runServiceRemote(ctx, string(constants.DDService)); err != nil {
		return fmt.Errorf("failed to start dd-service on hot node: %s", err)
	}
	log.WithContext(ctx).Info("dd-service started successfully")

	// ***************  7. Start supernode  **************
	// TODO (MATEE): improve the following code
	snConfigPath := r.config.Configurer.GetSuperNodeConfFile(r.config.WorkingDir)
	remoteSnConfigPath := r.config.Configurer.GetSuperNodeConfFile(r.config.RemoteWorkingDir)
	remoteSnConfigPath = strings.ReplaceAll(remoteSnConfigPath, "\\", "/")

	log.WithContext(ctx).Info("copying supernode config..")
	if err := r.sshClient.Scp(snConfigPath, remoteSnConfigPath); err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to copy pastel-utility executable to remote host")
		return err
	}
	if err = r.sshClient.ShellCmd(ctx, fmt.Sprintf("chmod 755 %s", snConfigPath)); err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to change permission of pastel-utility")
		return err
	}

	log.WithContext(ctx).Info("starting supernode-service..")
	snService := fmt.Sprintf("%s-%s", string(constants.SuperNode), "service")
	if err = r.runServiceRemote(ctx, snService); err != nil {
		return fmt.Errorf("failed to start supernode-service on hot node: %s", err)
	}
	log.WithContext(ctx).Info("started supernode-service successfully..")

	return nil
}

func (r *ColdHotRunner) runRemoteNodeAsMasterNode(ctx context.Context) error {
	go func() {
		cmdLine := fmt.Sprintf("%s --masternode --txindex=1 --reindex --masternodeprivkey=%s --externalip=%s  --data-dir=%s %s --daemon ",
			r.opts.remotePasteld, flagMasterNodePrivateKey, flagNodeExtIP, r.config.RemoteWorkingDir, r.opts.testnetOption)

		log.WithContext(ctx).Infof("start remote node as masternode%s\n", cmdLine)

		if err := r.sshClient.Cmd(cmdLine).Run(); err != nil {
			fmt.Println("pasteld run err: ", err.Error())
		}
	}()

	if !CheckPastelDRunningRemote(ctx, r.sshClient, r.opts.remotePastelCli, true) {
		err := fmt.Errorf("unable to start pasteld on remote")
		log.WithContext(ctx).WithError(err).Error("run remote as master failed")
		return err
	}

	if err := r.checkMasterNodeSyncRemote(ctx, 0); err != nil {
		log.WithContext(ctx).Error("Remote::Master node sync failed")
		return err
	}

	return nil
}

func (r *ColdHotRunner) handleCreateUpdateStartColdHot(ctx context.Context) error {
	if err := checkCollateral(ctx, r.config); err != nil {
		log.WithContext(ctx).WithError(err).Error("Missing collateral transaction")
		return err
	}

	if err := checkPassphrase(ctx); err != nil {
		log.WithContext(ctx).WithError(err).Error("Missing passphrase")
		return err
	}

	go func() {
		if err := r.sshClient.Cmd(fmt.Sprintf("%s --reindex --externalip=%s --data-dir=%s --daemon %s",
			r.opts.remotePasteld, flagNodeExtIP, r.config.RemoteWorkingDir, r.opts.testnetOption)).Run(); err != nil {
			log.WithContext(ctx).WithError(err).Error("unable to start pasteld on remote")
		}
	}()

	if !CheckPastelDRunningRemote(ctx, r.sshClient, r.opts.remotePastelCli, true) {
		return errors.New("unable to start pasteld on remote")
	}

	if err := checkMasternodePrivKey(ctx, r.config, r.sshClient); err != nil {
		log.WithContext(ctx).WithError(err).Error("Missing masternode private key")
		return err
	}

	if err := checkPastelID(ctx, r.config, r.sshClient); err != nil {
		log.WithContext(ctx).WithError(err).Error("Missing masternode PastelID")
		return err
	}

	if _, err := r.sshClient.Cmd(fmt.Sprintf("%s stop", r.opts.remotePastelCli)).Output(); err != nil {
		log.WithContext(ctx).Error("Error - stopping on remote pasteld")
		return err
	}

	time.Sleep(5 * time.Second)

	if flagMasterNodeIsCreate {
		if _, err := backupConfFile(ctx, r.config); err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to backup masternode.conf")
			return err
		}
	}

	return nil
}

func (r *ColdHotRunner) runServiceRemote(ctx context.Context, service string) (err error) {
	log.WithContext(ctx).WithField("service", service).Info("starting service on remote")

	cmd := fmt.Sprintf("%s %s %s", r.opts.remotePastelUtility, "start", service)
	if r.config.RemoteWorkingDir != "" {
		cmd = fmt.Sprintf("%s --work-dir=%s", cmd, r.config.RemoteWorkingDir)
	}

	out, err := r.sshClient.Cmd(cmd).Output()
	if err != nil {
		log.WithContext(ctx).WithField("service", service).WithField("out", string(out)).WithField("cmd", cmd).
			WithError(err).Error("failed to start service on remote")
		return fmt.Errorf("failed to start service on remote: %s", err.Error())
	}

	return err
}

// CheckPastelDRunningRemote whether pasteld is running
func CheckPastelDRunningRemote(ctx context.Context, client *utils.Client, cliPath string, want bool) (ret bool) {
	var failCnt = 0
	var err error

	log.WithContext(ctx).Info("Waiting the pasteld to be started...")

	for {
		if _, err = client.Cmd(fmt.Sprintf("%s %s", cliPath, "getinfo")).Output(); err != nil {
			if !want {
				log.WithContext(ctx).Info("remote pasteld stopped.")
				return false
			}

			time.Sleep(5 * time.Second)
			failCnt++
			if failCnt == 12 {
				return false
			}
		} else {
			break
		}
	}

	log.WithContext(ctx).Info("remote pasteld started successfully")
	return true
}

func (r *ColdHotRunner) remoteHotNodeCtrl(ctx context.Context) error {
	go func() {
		if err := r.sshClient.Cmd(fmt.Sprintf("%s --reindex --externalip=%s --data-dir=%s --daemon %s",
			r.opts.remotePasteld, flagNodeExtIP, r.config.RemoteWorkingDir, r.opts.testnetOption)).Run(); err != nil {
			fmt.Println("pasteld run err: ", err.Error())
		}
	}()

	if !CheckPastelDRunningRemote(ctx, r.sshClient, r.opts.remotePastelCli, true) {
		return fmt.Errorf("unable to start pasteld on remote")
	}

	if err := r.checkMasterNodeSyncRemote(ctx, 0); err != nil {
		log.WithContext(ctx).Error("Remote::Master node sync failed")
		return err
	}

	if _, err := r.sshClient.Cmd(fmt.Sprintf("%s stop", r.opts.remotePastelCli)).Output(); err != nil {
		log.WithContext(ctx).Error("Error - stopping on pasteld")
		return err
	}
	time.Sleep(5 * time.Second)

	return nil
}

func (r *ColdHotRunner) checkMasterNodeSyncRemote(ctx context.Context, retryCount int) (err error) {
	var mnstatus structure.RPCPastelMSStatus
	var output []byte

	for {
		if output, err = r.sshClient.Cmd(fmt.Sprintf("%s mnsync status", r.opts.remotePastelCli)).Output(); err != nil {
			log.WithContext(ctx).WithField("out", string(output)).WithError(err).
				Error("Remote:::failed to get mnsync status")
			if retryCount == 0 {
				log.WithContext(ctx).WithError(err).Error("retrying mynsyc staus...")
				time.Sleep(5 * time.Second)

				return r.checkMasterNodeSyncRemote(ctx, 1)
			}

			return err
		}
		// Master Node Output
		if err = json.Unmarshal([]byte(output), &mnstatus); err != nil {
			log.WithContext(ctx).WithField("payload", string(output)).WithError(err).
				Error("Remote:::failed to unmarshal mnsync status")

			return err
		}

		if mnstatus.AssetName == "Initial" {
			if out, err := r.sshClient.Cmd(fmt.Sprintf("%s mnsync reset", r.opts.remotePastelCli)).Output(); err != nil {
				log.WithContext(ctx).WithField("out", string(out)).WithError(err).
					Error("Remote:::master node reset was failed")

				return err
			}
			time.Sleep(10 * time.Second)
		}
		if mnstatus.IsSynced {
			log.WithContext(ctx).Info("Remote:::master node was synced!")
			break
		}
		log.WithContext(ctx).Info("Remote:::Waiting for sync...")
		time.Sleep(10 * time.Second)
	}
	return nil
}