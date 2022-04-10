package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/pastelnetwork/gonode/common/cli"
	"github.com/pastelnetwork/gonode/common/log"
	"github.com/pastelnetwork/pastelup/configs"
	"github.com/pastelnetwork/pastelup/constants"
	"github.com/pastelnetwork/pastelup/servicemanager"
	"github.com/pastelnetwork/pastelup/structure"
	"github.com/pastelnetwork/pastelup/utils"
)

/*var (
	wg sync.WaitGroup
)
*/
var (
	// node flags
	flagNodeExtIP string

	// walletnode flag
	flagDevMode bool
)

type startCommand uint8

const (
	nodeStart startCommand = iota
	walletStart
	superNodeStart
	ddService
	rqService
	wnService
	snService
	masterNode
	remoteStart
)

var (
	startCmdName = map[startCommand]string{
		nodeStart:      "node",
		walletStart:    "walletnode",
		superNodeStart: "supernode",
		ddService:      "dd-service",
		rqService:      "rq-service",
		wnService:      "walletnode-service",
		snService:      "supernode-service",
		masterNode:     "masterNode",
		remoteStart:    "remote",
	}
	startCmdMessage = map[startCommand]string{
		nodeStart:      "Start node",
		walletStart:    "Start Walletnode",
		superNodeStart: "Start Supernode",
		ddService:      "Start Dupe Detection service only",
		rqService:      "Start RaptorQ service only",
		wnService:      "Start Walletnode service onlyu",
		snService:      "Start Supernode service only",
		masterNode:     "Start only pasteld node as Masternode",
		remoteStart:    "Start on Remote host",
	}
)

func setupStartSubCommand(config *configs.Config,
	startCommand startCommand, remote bool,
	f func(context.Context, *configs.Config) error,
) *cli.Command {
	commonFlags := []*cli.Flag{
		cli.NewFlag("ip", &flagNodeExtIP).
			SetUsage(green("Optional, WAN address of the host")),
		cli.NewFlag("reindex", &config.ReIndex).SetAliases("r").
			SetUsage(green("Optional, Start with reindex")),
		cli.NewFlag("legacy", &config.Legacy).
			SetUsage(green("Optional, pasteld version is < 1.1")).SetValue(false),
	}

	var dirsFlags []*cli.Flag

	if !remote {
		dirsFlags = []*cli.Flag{
			cli.NewFlag("dir", &config.PastelExecDir).SetAliases("d").
				SetUsage(green("Optional, Location of pastel node directory")).SetValue(config.Configurer.DefaultPastelExecutableDir()),
			cli.NewFlag("work-dir", &config.WorkingDir).SetAliases("w").
				SetUsage(green("Optional, location of working directory")).SetValue(config.Configurer.DefaultWorkingDir()),
		}
	} else {
		dirsFlags = []*cli.Flag{
			cli.NewFlag("dir", &config.PastelExecDir).SetAliases("d").
				SetUsage(green("Optional, Location where to create pastel node directory on the remote computer (default: $HOME/pastel)")),
			cli.NewFlag("work-dir", &config.WorkingDir).SetAliases("w").
				SetUsage(green("Optional, Location where to create working directory on the remote computer (default: $HOME/.pastel)")),
		}
	}

	walletNodeFlags := []*cli.Flag{
		cli.NewFlag("development-mode", &flagDevMode),
	}

	superNodeStartFlags := []*cli.Flag{
		cli.NewFlag("name", &flagMasterNodeName).
			SetUsage(red("Required, name of the Masternode to start")).SetRequired(),
		cli.NewFlag("activate", &flagMasterNodeIsActivate).
			SetUsage(green("Optional, if specified, will try to enable node as Masternode (start-alias).")),
	}

	masternodeFlags := []*cli.Flag{
		cli.NewFlag("name", &flagMasterNodeName).
			SetUsage(red("Required, name of the Masternode to start")).SetRequired(),
	}

	remoteStartFlags := []*cli.Flag{
		cli.NewFlag("ssh-ip", &config.RemoteIP).
			SetUsage(red("Required, SSH address of the remote node")),
		cli.NewFlag("ssh-port", &config.RemotePort).
			SetUsage(green("Optional, SSH port of the remote node")).SetValue(22),
		cli.NewFlag("ssh-user", &config.RemoteUser).
			SetUsage(yellow("Optional, SSH user")),
		cli.NewFlag("ssh-key", &config.RemoteSSHKey).
			SetUsage(yellow("Optional, Path to SSH private key")),
		cli.NewFlag("inventory", &config.InventoryFile).
			SetUsage(red("Optional, Path to the file with configuration of the remote hosts")),
	}

	var commandName, commandMessage string
	if !remote {
		commandName = startCmdName[startCommand]
		commandMessage = startCmdMessage[startCommand]
	} else {
		commandName = startCmdName[remoteStart]
		commandMessage = startCmdMessage[remoteStart]
	}

	commandFlags := append(dirsFlags, commonFlags[:]...)
	if startCommand == walletStart ||
		startCommand == wnService {
		commandFlags = append(commandFlags, walletNodeFlags[:]...)
	} else if startCommand == superNodeStart {
		commandFlags = append(commandFlags, superNodeStartFlags[:]...)
	} else if startCommand == masterNode {
		commandFlags = append(commandFlags, masternodeFlags[:]...)
	}
	if remote {
		commandFlags = append(commandFlags, remoteStartFlags[:]...)
	}

	subCommand := cli.NewCommand(commandName)
	subCommand.SetUsage(cyan(commandMessage))
	subCommand.AddFlags(commandFlags...)
	if f != nil {
		subCommand.SetActionFunc(func(ctx context.Context, args []string) error {
			ctx, err := configureLogging(ctx, commandMessage, config)
			if err != nil {
				return err
			}

			// Register interrupt handler
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt)
			go func() {
				for {
					<-sigCh

					yes, _ := AskUserToContinue(ctx, "Interrupt signal received, do you want to cancel this process? Y/N")
					if yes {
						log.WithContext(ctx).Info("Gracefully shutting down...")
						cancel()
						os.Exit(0)
					}
				}
			}()

			log.WithContext(ctx).Info("Starting")
			err = f(ctx, config)
			if err != nil {
				return err
			}
			log.WithContext(ctx).Info("Finished successfully!")

			return nil
		})
	}
	return subCommand
}

func setupStartCommand() *cli.Command {
	config := configs.InitConfig()

	startNodeSubCommand := setupStartSubCommand(config, nodeStart, false, runStartNodeSubCommand)
	startWalletNodeSubCommand := setupStartSubCommand(config, walletStart, false, runStartWalletNodeSubCommand)
	startSuperNodeSubCommand := setupStartSubCommand(config, superNodeStart, false, runStartSuperNodeSubCommand)

	startRQServiceCommand := setupStartSubCommand(config, rqService, false, runRQService)
	startDDServiceCommand := setupStartSubCommand(config, ddService, false, runDDService)
	startWNServiceCommand := setupStartSubCommand(config, wnService, false, runWalletNodeService)
	startSNServiceCommand := setupStartSubCommand(config, snService, false, runSuperNodeService)
	startMasternodeCommand := setupStartSubCommand(config, masterNode, false, runStartMasternode)

	startSuperNodeRemoteSubCommand := setupStartSubCommand(config, superNodeStart, true, runRemoteSuperNodeStartSubCommand)
	startSuperNodeSubCommand.AddSubcommands(startSuperNodeRemoteSubCommand)

	startWalletNodeRemoteSubCommand := setupStartSubCommand(config, superNodeStart, true, runRemoteWalletNodeStartSubCommand)
	startWalletNodeSubCommand.AddSubcommands(startWalletNodeRemoteSubCommand)

	startNodeRemoteSubCommand := setupStartSubCommand(config, nodeStart, true, runRemoteNodeStartSubCommand)
	startNodeSubCommand.AddSubcommands(startNodeRemoteSubCommand)

	startRQServiceRemoteCommand := setupStartSubCommand(config, rqService, true, runRemoteRQServiceStartSubCommand)
	startRQServiceCommand.AddSubcommands(startRQServiceRemoteCommand)

	startDDServiceRemoteCommand := setupStartSubCommand(config, ddService, true, runRemoteDDServiceStartSubCommand)
	startDDServiceCommand.AddSubcommands(startDDServiceRemoteCommand)

	startWNServiceRemoteCommand := setupStartSubCommand(config, wnService, true, runRemoteWNServiceStartSubCommand)
	startWNServiceCommand.AddSubcommands(startWNServiceRemoteCommand)

	startSNServiceRemoteCommand := setupStartSubCommand(config, snService, true, runRemoteSNServiceStartSubCommand)
	startSNServiceCommand.AddSubcommands(startSNServiceRemoteCommand)

	startMasternodeRemoteCommand := setupStartSubCommand(config, masterNode, true, runRemoteSNServiceStartSubCommand)
	startMasternodeCommand.AddSubcommands(startMasternodeRemoteCommand)

	startCommand := cli.NewCommand("start")
	startCommand.SetUsage(blue("Performs start of the system for both WalletNode and SuperNodes"))
	startCommand.AddSubcommands(startNodeSubCommand)
	startCommand.AddSubcommands(startWalletNodeSubCommand)
	startCommand.AddSubcommands(startSuperNodeSubCommand)

	startCommand.AddSubcommands(startRQServiceCommand)
	startCommand.AddSubcommands(startDDServiceCommand)
	startCommand.AddSubcommands(startWNServiceCommand)
	startCommand.AddSubcommands(startSNServiceCommand)
	startCommand.AddSubcommands(startMasternodeCommand)

	return startCommand

}

///// Top level start commands

// Sub Command
func runStartNodeSubCommand(ctx context.Context, config *configs.Config) error {
	if err := runPastelNode(ctx, config, false, config.ReIndex, flagNodeExtIP, ""); err != nil {
		log.WithContext(ctx).WithError(err).Error("pasteld failed to start")
		return err
	}
	return nil
}

// Sub Command
func runStartWalletNodeSubCommand(ctx context.Context, config *configs.Config) error {
	// *************  1. Start pastel node  *************
	if err := runPastelNode(ctx, config, false, config.ReIndex, flagNodeExtIP, ""); err != nil {
		log.WithContext(ctx).WithError(err).Error("pasteld failed to start")
		return err
	}

	// *************  2. Start rq-servce    *************
	if err := runRQService(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("rqservice failed to start")
		return err
	}

	// *************  3. Start wallet node  *************
	if err := runWalletNodeService(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("walletnode failed to start")
		return err
	}

	return nil
}

// Sub Command
func runStartSuperNodeSubCommand(ctx context.Context, config *configs.Config) error {
	log.WithContext(ctx).Info("Starting supernode")
	if err := runStartSuperNode(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to start supernode")
		return err
	}
	log.WithContext(ctx).Info("Supernode started successfully")
	return nil
}

func runStartSuperNode(ctx context.Context, config *configs.Config) error {
	// *************  1. Parse pastel config parameters  *************
	log.WithContext(ctx).Info("Reading pastel.conf")
	if err := ParsePastelConf(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to parse pastel config")
		return err
	}
	log.WithContext(ctx).Infof("Finished Reading pastel.conf! Starting Supernode in %s mode", config.Network)

	// *************  2. Parse parameters  *************
	log.WithContext(ctx).Info("Checking arguments")
	if err := checkStartMasterNodeParams(ctx, config, false); err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to validate input arguments")
		return err
	}
	log.WithContext(ctx).Info("Finished checking arguments!")

	pastelDIsRunning := false
	if CheckProcessRunning(constants.PastelD) {
		log.WithContext(ctx).Infof("pasteld is already running")
		if yes, _ := AskUserToContinue(ctx,
			"Do you want to stop it and continue? Y/N"); !yes {
			log.WithContext(ctx).Warn("Exiting...")
			return fmt.Errorf("user terminated installation")
		}
		pastelDIsRunning = true
	}

	if flagMasterNodeConfNew || flagMasterNodeConfAdd {
		log.WithContext(ctx).Info("Prepare masternode parameters")
		if err := prepareMasterNodeParameters(ctx, config, !pastelDIsRunning); err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to validate and prepare masternode parameters")
			return err
		}
		if err := createOrUpdateMasternodeConf(ctx, config); err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to create or update masternode.conf")
			return err
		}
		if err := createOrUpdateSuperNodeConfig(ctx, config); err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to update supernode.yml")
			return err
		}
	}

	if pastelDIsRunning {
		if err := StopPastelDAndWait(ctx, config); err != nil {
			log.WithContext(ctx).WithError(err).Error("Cannot stop pasteld")
			return err
		}
	}

	// *************  3. Start Node as Masternode  *************
	if err := runStartMasternode(ctx, config); err != nil { //in masternode mode pasteld MUST be started with reindex flag
		return err
	}

	// *************  4. Wait for blockchain and masternodes sync  *************
	if _, err := CheckMasterNodeSync(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("pasteld failed to synchronize, add some peers and try again")
		return err
	}

	// *************  5. Enable Masternode  ***************
	if flagMasterNodeIsActivate {
		log.WithContext(ctx).Infof("Starting MN alias - %s", flagMasterNodeName)
		if err := runStartAliasMasternode(ctx, config, flagMasterNodeName); err != nil {
			log.WithContext(ctx).WithError(err).Errorf("Failed to start alias - %s", flagMasterNodeName)
			return err
		}
	}

	// *************  6. Start rq-servce    *************
	if err := runRQService(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("rqservice failed to start")
		return err
	}

	// *************  6. Start dd-servce    *************
	if err := runDDService(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("ddservice failed to start")
		return err
	}

	// *************  7. Start supernode  **************
	if err := runSuperNodeService(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to start supernode service")
		return err
	}

	return nil
}

func runRemoteNodeStartSubCommand(ctx context.Context, config *configs.Config) error {
	return runRemoteStart(ctx, config, "node")
}
func runRemoteSuperNodeStartSubCommand(ctx context.Context, config *configs.Config) error {
	return runRemoteStart(ctx, config, "supernode")
}
func runRemoteWalletNodeStartSubCommand(ctx context.Context, config *configs.Config) error {
	return runRemoteStart(ctx, config, "walletnode")
}
func runRemoteRQServiceStartSubCommand(ctx context.Context, config *configs.Config) error {
	return runRemoteStart(ctx, config, "rq-service")
}
func runRemoteDDServiceStartSubCommand(ctx context.Context, config *configs.Config) error {
	return runRemoteStart(ctx, config, "dd-service")
}
func runRemoteWNServiceStartSubCommand(ctx context.Context, config *configs.Config) error {
	return runRemoteStart(ctx, config, "walletnode-service")
}
func runRemoteSNServiceStartSubCommand(ctx context.Context, config *configs.Config) error {
	return runRemoteStart(ctx, config, "supernode-service")
}

func runRemoteStart(ctx context.Context, config *configs.Config, tool string) error {
	log.WithContext(ctx).Infof("Starting remote %s", tool)

	// Start remote node
	startOptions := tool

	if len(flagMasterNodeName) > 0 {
		startOptions = fmt.Sprintf("%s --name=%s", startOptions, flagMasterNodeName)
	}

	if flagMasterNodeIsActivate {
		startOptions = fmt.Sprintf("%s --activate", startOptions)
	}

	if len(flagNodeExtIP) > 0 {
		startOptions = fmt.Sprintf("%s --ip=%s", startOptions, flagNodeExtIP)
	}
	if config.ReIndex {
		startOptions = fmt.Sprintf("%s --reindex", startOptions)
	}
	if config.Legacy {
		startOptions = fmt.Sprintf("%s --legacy", startOptions)
	}
	if flagDevMode {
		startOptions = fmt.Sprintf("%s --development-mode", startOptions)
	}
	if len(config.PastelExecDir) > 0 {
		startOptions = fmt.Sprintf("%s --dir=%s", startOptions, config.PastelExecDir)
	}
	if len(config.WorkingDir) > 0 {
		startOptions = fmt.Sprintf("%s --work-dir=%s", startOptions, config.WorkingDir)
	}

	startSuperNodeCmd := fmt.Sprintf("%s start %s", constants.RemotePastelupPath, startOptions)
	if err := executeRemoteCommandsWithInventory(ctx, config, []string{startSuperNodeCmd}, false); err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Failed to start %s on remote host", tool)
	}

	log.WithContext(ctx).Infof("Remote %s started successfully", tool)
	return nil
}

func runStartMasternode(ctx context.Context, config *configs.Config) error {
	log.WithContext(ctx).Info("Reading pastel.conf")
	if err := ParsePastelConf(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to parse pastel config")
		return err
	}
	log.WithContext(ctx).Infof("Finished Reading pastel.conf! Starting Supernode in %s mode", config.Network)

	// Get conf data from masternode.conf File
	privKey, extIP, _ /*extPort*/, err := getMasternodeConfData(ctx, config, flagMasterNodeName)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to get masternode details from masternode.conf")
		return err
	}

	if len(flagNodeExtIP) == 0 {
		log.WithContext(ctx).Info("--ip flag is ommited, trying to get our WAN IP address")
		externalIP, err := utils.GetExternalIPAddress()
		if err != nil {
			err := fmt.Errorf("cannot get external ip address")
			log.WithContext(ctx).WithError(err).Error("Missing parameter --ip")
			return err
		}
		flagNodeExtIP = externalIP
		log.WithContext(ctx).Infof("WAN IP address - %s", flagNodeExtIP)
	}
	if extIP != flagNodeExtIP {
		err := errors.Errorf("External IP address in masternode.conf MUST match WAN address of the node! IP in masternode.conf - %s, WAN IP passed or identified - %s", extIP, flagNodeExtIP)
		log.WithContext(ctx).WithError(err).Error("pasteld failed to start")
		return err
	}

	// *************  Start Node as Masternode  *************
	log.WithContext(ctx).Infof("Starting pasteld as masternode: nodeName: %s; mnPrivKey: %s", flagMasterNodeName, privKey)
	if err := runPastelNode(ctx, config, true, config.ReIndex, flagNodeExtIP, privKey); err != nil { //in masternode mode pasteld MUST be started with reindex flag
		log.WithContext(ctx).WithError(err).Error("pasteld failed to start as masternode")
		return err
	}
	return nil
}

// Sub Command
func runRQService(ctx context.Context, config *configs.Config) error {
	serviceEnabled := false
	sm, err := servicemanager.New(utils.GetOS(), config.Configurer.DefaultHomeDir())
	if err != nil {
		log.WithContext(ctx).Warn(err.Error())
	} else {
		serviceEnabled = true
	}
	if serviceEnabled {
		// if the service isnt registered, this will be a noop
		srvStarted, err := sm.StartService(ctx, constants.RQService)
		if err != nil {
			log.WithContext(ctx).Errorf("Failed to start service for %v: %v", constants.RQService, err)
			return err
		}
		if srvStarted {
			return nil
		}
	}
	rqExecName := constants.PastelRQServiceExecName[utils.GetOS()]
	var rqServiceArgs []string
	configFile := config.Configurer.GetRQServiceConfFile(config.WorkingDir)
	rqServiceArgs = append(rqServiceArgs, fmt.Sprintf("--config-file=%s", configFile))
	if err := runPastelService(ctx, config, constants.RQService, rqExecName, rqServiceArgs...); err != nil {
		log.WithContext(ctx).WithError(err).Error("rqservice failed")
		return err
	}
	return nil
}

// Sub Command
func runDDService(ctx context.Context, config *configs.Config) (err error) {
	serviceEnabled := false
	sm, err := servicemanager.New(utils.GetOS(), config.Configurer.DefaultHomeDir())
	if err != nil {
		log.WithContext(ctx).Warn(err.Error())
	} else {
		serviceEnabled = true
	}
	if serviceEnabled {
		// if the service isn't registered, this will be a noop
		srvStarted, err := sm.StartService(ctx, constants.DDService)
		if err != nil {
			log.WithContext(ctx).Errorf("Failed to start service for %v: %v", constants.RQService, err)
			return err
		}
		if srvStarted {
			return nil
		}
	}

	var execPath string
	if execPath, err = checkPastelFilePath(ctx, config.PastelExecDir, utils.GetDupeDetectionExecName()); err != nil {
		log.WithContext(ctx).WithError(err).Error("Could not find dupe detection service script")
		return err
	}

	ddConfigFilePath := filepath.Join(config.Configurer.DefaultHomeDir(),
		constants.DupeDetectionServiceDir,
		constants.DupeDetectionSupportFilePath,
		constants.DupeDetectionConfigFilename)

	python := "python3"
	if utils.GetOS() == constants.Windows {
		python = "python"
	}
	venv := filepath.Join(config.PastelExecDir, constants.DupeDetectionSubFolder, "venv")
	cmd := fmt.Sprintf("source %v/bin/activate && %v %v %v", venv, python, execPath, ddConfigFilePath)
	go RunCMD("bash", "-c", cmd)

	time.Sleep(10 * time.Second)

	if output, err := FindRunningProcess(constants.DupeDetectionExecFileName); len(output) == 0 {
		err = errors.Errorf("dd-service failed to start")
		log.WithContext(ctx).WithError(err).Error("dd-service failed to start")
		return err
	} else if err != nil {
		log.WithContext(ctx).WithError(err).Error("failed to test if dd-servise is running")
	} else {
		log.WithContext(ctx).Info("dd-service is successfully started")
	}
	return nil
}

// Sub Command
func runWalletNodeService(ctx context.Context, config *configs.Config) error {
	serviceEnabled := false
	sm, err := servicemanager.New(utils.GetOS(), config.Configurer.DefaultHomeDir())
	if err != nil {
		log.WithContext(ctx).Warn(err.Error())
	} else {
		serviceEnabled = true
	}
	if serviceEnabled {
		// if the service isnt registered, this will be a noop
		srvStarted, err := sm.StartService(ctx, constants.WalletNode)
		if err != nil {
			log.WithContext(ctx).Errorf("Failed to start service for %v: %v", constants.WalletNode, err)
			return err
		}
		if srvStarted {
			return nil
		}
	}
	walletnodeExecName := constants.WalletNodeExecName[utils.GetOS()]
	log.WithContext(ctx).Infof("Starting walletnode service - %s", walletnodeExecName)
	var wnServiceArgs []string
	wnServiceArgs = append(wnServiceArgs,
		fmt.Sprintf("--config-file=%s --pastel-config-file=%s/pastel.conf", config.Configurer.GetWalletNodeConfFile(config.WorkingDir), config.WorkingDir))
	if flagDevMode {
		wnServiceArgs = append(wnServiceArgs, "--swagger")
	}
	log.WithContext(ctx).Infof("Options : %s", wnServiceArgs)
	if err := runPastelService(ctx, config, constants.WalletNode, walletnodeExecName, wnServiceArgs...); err != nil {
		log.WithContext(ctx).WithError(err).Error("walletnode service failed")
		return err
	}
	return nil
}

// Sub Command
func runSuperNodeService(ctx context.Context, config *configs.Config) error {
	serviceEnabled := false
	sm, err := servicemanager.New(utils.GetOS(), config.Configurer.DefaultHomeDir())
	if err != nil {
		log.WithContext(ctx).Warn(err.Error())
	} else {
		serviceEnabled = true
	}
	if serviceEnabled {
		// if the service isnt registered, this will be a noop
		srvStarted, err := sm.StartService(ctx, constants.SuperNode)
		if err != nil {
			log.WithContext(ctx).Errorf("Failed to start service for %v: %v", constants.SuperNode, err)
		}
		if srvStarted {
			return nil
		}
	}
	supernodeConfigPath := config.Configurer.GetSuperNodeConfFile(config.WorkingDir)
	supernodeExecName := constants.SuperNodeExecName[utils.GetOS()]
	log.WithContext(ctx).Infof("Starting Supernode service - %s", supernodeExecName)

	var snServiceArgs []string
	snServiceArgs = append(snServiceArgs,
		fmt.Sprintf("--config-file=%s", supernodeConfigPath))

	log.WithContext(ctx).Infof("Options : %s", snServiceArgs)
	if err := runPastelService(ctx, config, constants.SuperNode, supernodeExecName, snServiceArgs...); err != nil {
		log.WithContext(ctx).WithError(err).Error("supernode failed")
		return err
	}
	return nil
}

///// Run helpers
func runPastelNode(ctx context.Context, config *configs.Config, txIndexOne bool, reindex bool, extIP string, mnPrivKey string) (err error) {
	serviceEnabled := false
	sm, err := servicemanager.New(utils.GetOS(), config.Configurer.DefaultHomeDir())
	if err != nil {
		log.WithContext(ctx).Warn(err.Error())
	} else {
		serviceEnabled = true
	}
	if serviceEnabled {
		// if the service isn't registered, this will be a noop
		srvStarted, err := sm.StartService(ctx, constants.PastelD)
		if err != nil {
			log.WithContext(ctx).Errorf("Failed to start service for %v: %v", constants.PastelD, err)
			return err
		}
		if srvStarted {
			return nil
		}
	}

	var pastelDPath string
	if pastelDPath, err = checkPastelFilePath(ctx, config.PastelExecDir, constants.PasteldName[utils.GetOS()]); err != nil {
		log.WithContext(ctx).WithError(err).Error("Could not find pasteld")
		return err
	}

	if _, err = checkPastelFilePath(ctx, config.WorkingDir, constants.PastelConfName); err != nil {
		log.WithContext(ctx).WithError(err).Error("Could not find pastel.conf")
		return err
	}
	if err = CheckZksnarkParams(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("Wrong ZKSnark files")
		return err
	}

	if len(extIP) == 0 {
		if extIP, err = utils.GetExternalIPAddress(); err != nil {
			log.WithContext(ctx).WithError(err).Error("Could not get external IP address")
			return err
		}
	}

	var pasteldArgs []string
	pasteldArgs = append(pasteldArgs,
		fmt.Sprintf("--datadir=%s", config.WorkingDir),
		fmt.Sprintf("--externalip=%s", extIP))

	if txIndexOne {
		pasteldArgs = append(pasteldArgs, "--txindex=1")
	}

	if reindex {
		pasteldArgs = append(pasteldArgs, "--reindex")
	}

	if len(mnPrivKey) != 0 {
		pasteldArgs = append(pasteldArgs, "--masternode", fmt.Sprintf("--masternodeprivkey=%s", mnPrivKey))
	}

	log.WithContext(ctx).Infof("Starting -> %s %s", pastelDPath, strings.Join(pasteldArgs, " "))

	pasteldArgs = append(pasteldArgs, "--daemon")
	go RunCMD(pastelDPath, pasteldArgs...)

	if !WaitingForPastelDToStart(ctx, config) {
		err = fmt.Errorf("pasteld was not started")
		log.WithContext(ctx).WithError(err).Error("pasteld didn't start")
		return err
	}

	return nil
}

func runPastelService(ctx context.Context, config *configs.Config, toolType constants.ToolType, toolFileName string, args ...string) (err error) {

	log.WithContext(ctx).Infof("Starting %s", toolType)

	var execPath string
	if execPath, err = checkPastelFilePath(ctx, config.PastelExecDir, toolFileName); err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Could not find %s", toolType)
		return err
	}

	go RunCMD(execPath, args...)
	time.Sleep(10 * time.Second)

	log.WithContext(ctx).Infof("Check %s is running...", toolType)
	isServiceRunning := CheckProcessRunning(toolType)
	if isServiceRunning {
		log.WithContext(ctx).Infof("The %s started succesfully!", toolType)
	} else {
		if output, err := RunCMD(execPath, args...); err != nil {
			log.WithContext(ctx).Errorf("%s start failed! : %s", toolType, output)
			return err
		}
	}

	return nil
}

///// Validates input parameters
func checkStartMasterNodeParams(ctx context.Context, config *configs.Config, coldHot bool) error {

	// --name supernode name - Required, name of the Masternode to start and create in the masternode.conf if --create or --update are specified
	if len(flagMasterNodeName) == 0 {
		err := fmt.Errorf("required: --name, name of the Masternode to start")
		log.WithContext(ctx).WithError(err).Error("Missing parameter --name")
		return err
	}

	// --ip WAN IP address of the node - Required, WAN address of the host
	if len(flagNodeExtIP) == 0 && !coldHot { //coldHot will try to get WAN address in the step that is executed on remote host

		log.WithContext(ctx).Info("--ip flag is ommited, trying to get our WAN IP address")
		externalIP, err := utils.GetExternalIPAddress()
		if err != nil {
			err := fmt.Errorf("cannot get external ip address")
			log.WithContext(ctx).WithError(err).Error("Missing parameter --ip")
			return err
		}
		flagNodeExtIP = externalIP
		log.WithContext(ctx).Infof("WAN IP address - %s", flagNodeExtIP)
	}

	if !flagMasterNodeConfNew { // if we don't create new masternode.conf - it must exist!
		masternodeConfPath := getMasternodeConfPath(config, "", "masternode.conf")
		if _, err := checkPastelFilePath(ctx, config.WorkingDir, masternodeConfPath); err != nil {
			log.WithContext(ctx).WithError(err).Error("Could not find masternode.conf - use --create flag")
			return err
		}
	}

	if coldHot {
		if len(config.RemoteIP) == 0 {
			err := fmt.Errorf("required if --coldhot is specified, –-ssh-ip, SSH address of the remote HOT node")
			log.WithContext(ctx).WithError(err).Error("Missing parameter --ssh-ip")
			return err
		}
	}

	flagMasterNodeRPCIP = func() string {
		if len(flagMasterNodeRPCIP) == 0 {
			return flagNodeExtIP
		}
		return flagMasterNodeRPCIP
	}()
	flagMasterNodeP2PIP = func() string {
		if len(flagMasterNodeP2PIP) == 0 {
			return flagNodeExtIP
		}
		return flagMasterNodeP2PIP
	}()

	portList := GetSNPortList(config)

	flagMasterNodePort = func() int {
		if flagMasterNodePort == 0 {
			return portList[constants.NodePort]
		}
		return flagMasterNodePort
	}()
	flagMasterNodeRPCPort = func() int {
		if flagMasterNodeRPCPort == 0 {
			return portList[constants.SNPort]
		}
		return flagMasterNodeRPCPort
	}()
	flagMasterNodeP2PPort = func() int {
		if flagMasterNodeP2PPort == 0 {
			return portList[constants.P2PPort]
		}
		return flagMasterNodeP2PPort
	}()

	return nil
}

///// Run helpers
func prepareMasterNodeParameters(ctx context.Context, config *configs.Config, startPasteld bool) (err error) {

	// this function must only be called when --create or --update
	if !flagMasterNodeConfNew && !flagMasterNodeConfAdd {
		return nil
	}

	if startPasteld {
		log.WithContext(ctx).Infof("Starting pasteld")
		// in masternode mode pasteld MUST be start with txIndex=1 flag
		if err = runPastelNode(ctx, config, true, config.ReIndex, flagNodeExtIP, ""); err != nil {
			log.WithContext(ctx).WithError(err).Error("pasteld failed to start")
			return err
		}
	}

	// Check masternode status
	if _, err = CheckMasterNodeSync(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("pasteld failed to synchronize, add some peers and try again")
		return err
	}

	if err := checkCollateral(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("Missing collateral transaction")
		return err
	}

	if err := checkPassphrase(ctx); err != nil {
		log.WithContext(ctx).WithError(err).Error("Missing passphrase")
		return err
	}

	if err := checkMasternodePrivKey(ctx, config, nil); err != nil {
		log.WithContext(ctx).WithError(err).Error("Missing masternode private key")
		return err
	}

	if err := checkPastelID(ctx, config, nil); err != nil {
		log.WithContext(ctx).WithError(err).Error("Missing masternode PastelID")
		return err
	}

	if startPasteld {
		if err = StopPastelDAndWait(ctx, config); err != nil {
			log.WithContext(ctx).WithError(err).Error("Cannot stop pasteld")
			return err
		}
	}
	return nil
}

func checkPastelID(ctx context.Context, config *configs.Config, client *utils.Client) (err error) {
	if len(flagMasterNodePastelID) == 0 {

		log.WithContext(ctx).Info("Masternode PastelID is empty - will create new one")

		if len(flagMasterNodePassPhrase) == 0 { //check one more time just because
			err := fmt.Errorf("required parameter if --create or --update specified: --passphrase <passphrase to pastelid private key>")
			log.WithContext(ctx).WithError(err).Error("Missing parameter --passphrase")
			return err
		}

		var pastelid string
		if client == nil {
			pastelid, err = RunPastelCLI(ctx, config, "pastelid", "newkey", flagMasterNodePassPhrase)
			if err != nil {
				log.WithContext(ctx).WithError(err).Error("Failed to generate new pastelid key")
				return err
			}
		} else { //client is not nil when called from ColdHot Init
			pastelcliPath := filepath.Join(config.RemoteHotPastelExecDir, constants.PastelCliName[utils.GetOS()])
			out, err := client.Cmd(fmt.Sprintf("%s %s %s", pastelcliPath, "pastelid newkey",
				flagMasterNodePassPhrase)).Output()
			if err != nil {
				log.WithContext(ctx).WithError(err).Error("Failed to generate new pastelid key on Hot node")
				return err
			}
			pastelid = string(out)
			fmt.Println("generated pastel key on hotnode: ", pastelid)
		}

		var pastelidSt structure.RPCPastelID
		if err = json.Unmarshal([]byte(pastelid), &pastelidSt); err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to parse pastelid json")
			return err
		}
		flagMasterNodePastelID = pastelidSt.Pastelid
	}
	log.WithContext(ctx).Infof("Masternode pastelid = %s", flagMasterNodePastelID)
	return nil
}

func checkMasternodePrivKey(ctx context.Context, config *configs.Config, client *utils.Client) (err error) {
	if len(flagMasterNodePrivateKey) == 0 {
		log.WithContext(ctx).Info("Masternode private key is empty - will create new one")

		var mnPrivKey string
		if client == nil {
			mnPrivKey, err = RunPastelCLI(ctx, config, "masternode", "genkey")
			if err != nil {
				log.WithContext(ctx).WithError(err).Error("Failed to generate new masternode private key")
				return err
			}
		} else { //client is not nil when called from ColdHot Init
			pastelcliPath := filepath.Join(config.RemoteHotPastelExecDir, constants.PastelCliName[utils.GetOS()])
			cmd := fmt.Sprintf("%s %s", pastelcliPath, "masternode genkey")
			out, err := client.Cmd(cmd).Output()
			if err != nil {
				log.WithContext(ctx).WithField("cmd", cmd).WithField("out", string(out)).WithError(err).Error("Failed to generate new masternode private key on Hot node")
				return err
			}

			mnPrivKey = string(out)
			fmt.Println("generated priv key on hotnode: ", mnPrivKey)
		}

		flagMasterNodePrivateKey = strings.TrimSuffix(mnPrivKey, "\n")
	}
	log.WithContext(ctx).Infof("masternode private key = %s", flagMasterNodePrivateKey)
	return nil
}

func checkPassphrase(ctx context.Context) error {
	if len(flagMasterNodePassPhrase) == 0 {

		_, flagMasterNodePassPhrase = AskUserToContinue(ctx, "No --passphrase provided."+
			" Please type new passphrase and press Enter. Or N to exit")
		if strings.EqualFold(flagMasterNodePassPhrase, "n") ||
			len(flagMasterNodePassPhrase) == 0 {

			flagMasterNodePassPhrase = ""
			err := fmt.Errorf("required parameter if --create or --update specified: --passphrase <passphrase to pastelid private key>")
			log.WithContext(ctx).WithError(err).Error("User terminated - exiting")
			return err
		}
	}
	log.WithContext(ctx).Infof(red(fmt.Sprintf("passphrase - %s", flagMasterNodePassPhrase)))
	return nil
}

func getMasternodeOutputs(ctx context.Context, config *configs.Config) (map[string]string, error) {

	var mnOutputs map[string]string
	outputs, err := RunPastelCLI(ctx, config, "masternode", "outputs")
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to get masternode outputs from pasteld")
		return nil, err
	}
	if len(outputs) != 0 {
		if err := json.Unmarshal([]byte(outputs), &mnOutputs); err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to parse masternode outputs json")
			return nil, err
		}
	}
	return mnOutputs, nil
}

func checkCollateral(ctx context.Context, config *configs.Config) error {

	var address string
	var err error

	if len(flagMasterNodeTxID) == 0 || len(flagMasterNodeInd) == 0 {

		log.WithContext(ctx).Warn(red("No collateral --txid and/or --ind provided"))
		yes, _ := AskUserToContinue(ctx, "Search existing masternode collateral ready transaction in the wallet? Y/N")

		if yes {
			var mnOutputs map[string]string
			mnOutputs, err = getMasternodeOutputs(ctx, config)
			if err != nil {
				log.WithContext(ctx).WithError(err).Error("Failed")
				return err
			}

			if len(mnOutputs) > 0 {

				n := 0
				arr := []string{}
				for txid, txind := range mnOutputs {
					log.WithContext(ctx).Warn(red(fmt.Sprintf("%d - %s:%s", n, txid, txind)))
					arr = append(arr, txid)
					n++
				}
				_, strNum := AskUserToContinue(ctx, "Enter number to use, or N to exit")
				dNum, err := strconv.Atoi(strNum)
				if err != nil || dNum < 0 || dNum >= n {
					err = fmt.Errorf("user terminated - no collateral funds")
					log.WithContext(ctx).WithError(err).Error("No collateral funds - exiting")
					return err
				}

				flagMasterNodeTxID = arr[dNum]
				flagMasterNodeInd = mnOutputs[flagMasterNodeTxID]
			} else {
				log.WithContext(ctx).Warn(red("No existing collateral ready transactions"))
			}
		}
	}

	if len(flagMasterNodeTxID) == 0 || len(flagMasterNodeInd) == 0 {

		collateralAmount := "5"
		collateralCoins := "PSL"
		if config.Network == constants.NetworkTestnet {
			collateralAmount = "1"
			collateralCoins = "LSP"
		} else if config.Network == constants.NetworkRegTest {
			collateralAmount = "0.1"
			collateralCoins = "REG"
		}

		yes, _ := AskUserToContinue(ctx, fmt.Sprintf("Do you want to generate new local address and send %sM %s to it from another wallet? Y/N",
			collateralAmount, collateralCoins))

		if !yes {
			err = fmt.Errorf("no collateral funds")
			log.WithContext(ctx).WithError(err).Error("No collateral funds - exiting")
			return err
		}
		address, err = RunPastelCLI(ctx, config, "getnewaddress")
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to get new address")
			return err
		}
		address = strings.Trim(address, "\n")
		log.WithContext(ctx).Warnf(red(fmt.Sprintf("Your new address for collateral payment is %s", address)))
		log.WithContext(ctx).Warnf(red(fmt.Sprintf("Use another wallet to send exactly %sM %s to that address.", collateralAmount, collateralCoins)))
		_, newTxid := AskUserToContinue(ctx, "Enter txid of the send and press Enter to continue when ready")
		flagMasterNodeTxID = strings.Trim(newTxid, "\n")
	}

	for i := 1; i <= 10; i++ {

		var mnOutputs map[string]string
		mnOutputs, err = getMasternodeOutputs(ctx, config)
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed")
			return err
		}

		txind, ok := mnOutputs[flagMasterNodeTxID]
		if ok {
			flagMasterNodeInd = txind
			break
		}

		log.WithContext(ctx).Info("Waiting for transaction...")
		time.Sleep(10 * time.Second)
		if i == 10 {
			yes, _ := AskUserToContinue(ctx, "Still no collateral transaction. Continue? - Y/N")
			if !yes {
				err := fmt.Errorf("user terminated")
				log.WithContext(ctx).WithError(err).Error("Exiting")
				return err
			}
			i = 1
		}
	}

	if len(flagMasterNodeTxID) == 0 || len(flagMasterNodeInd) == 0 {

		err := errors.Errorf("Cannot find masternode outputs = %s:%s", flagMasterNodeTxID, flagMasterNodeInd)
		log.WithContext(ctx).WithError(err).Error("Try again after some time")
		return err
	}

	// if receives PSL go to next step
	log.WithContext(ctx).Infof(red(fmt.Sprintf("masternode outputs = %s, %s", flagMasterNodeTxID, flagMasterNodeInd)))
	return nil
}

///// Masternode specific
func runStartAliasMasternode(ctx context.Context, config *configs.Config, masternodeName string) (err error) {
	var output string
	if output, err = RunPastelCLI(ctx, config, "masternode", "start-alias", masternodeName); err != nil {
		return err
	}
	var aliasStatus map[string]interface{}

	if err = json.Unmarshal([]byte(output), &aliasStatus); err != nil {
		return err
	}

	if aliasStatus["result"] == "failed" {
		err = fmt.Errorf("masternode start alias failed")
		log.WithContext(ctx).WithError(err).Error(aliasStatus["errorMessage"])
		return err
	}

	log.WithContext(ctx).Infof("masternode alias status = %s\n", output)
	return nil
}

///// supernode.yml hlpers
func createOrUpdateSuperNodeConfig(ctx context.Context, config *configs.Config) error {

	supernodeConfigPath := config.Configurer.GetSuperNodeConfFile(config.WorkingDir)
	log.WithContext(ctx).Infof("Updating supernode config - %s", supernodeConfigPath)

	if _, err := os.Stat(supernodeConfigPath); os.IsNotExist(err) {
		// create new
		if err = utils.CreateFile(ctx, supernodeConfigPath, true); err != nil {
			log.WithContext(ctx).WithError(err).Errorf("Failed to create new supernode.yml file at - %s", supernodeConfigPath)
			return err
		}

		portList := GetSNPortList(config)

		snTempDirPath := filepath.Join(config.WorkingDir, constants.TempDir)
		rqWorkDirPath := filepath.Join(config.WorkingDir, constants.RQServiceDir)
		p2pDataPath := filepath.Join(config.WorkingDir, constants.P2PDataDir)
		mdlDataPath := filepath.Join(config.WorkingDir, constants.MDLDataDir)
		ddDirPath := filepath.Join(config.Configurer.DefaultHomeDir(), constants.DupeDetectionServiceDir)

		toolConfig, err := utils.GetServiceConfig(string(constants.SuperNode), configs.SupernodeDefaultConfig, &configs.SuperNodeConfig{
			LogFilePath:                     config.Configurer.GetSuperNodeLogFile(config.WorkingDir),
			LogCompress:                     constants.LogConfigDefaultCompress,
			LogMaxSizeMB:                    constants.LogConfigDefaultMaxSizeMB,
			LogMaxAgeDays:                   constants.LogConfigDefaultMaxAgeDays,
			LogMaxBackups:                   constants.LogConfigDefaultMaxBackups,
			LogLevelCommon:                  constants.SuperNodeDefaultCommonLogLevel,
			LogLevelP2P:                     constants.SuperNodeDefaultP2PLogLevel,
			LogLevelMetadb:                  constants.SuperNodeDefaultMetaDBLogLevel,
			LogLevelDD:                      constants.SuperNodeDefaultDDLogLevel,
			SNTempDir:                       snTempDirPath,
			SNWorkDir:                       config.WorkingDir,
			RQDir:                           rqWorkDirPath,
			DDDir:                           ddDirPath,
			SuperNodePort:                   portList[constants.SNPort],
			P2PPort:                         portList[constants.P2PPort],
			P2PDataDir:                      p2pDataPath,
			MDLPort:                         portList[constants.MDLPort],
			RAFTPort:                        portList[constants.RAFTPort],
			MDLDataDir:                      mdlDataPath,
			RaptorqPort:                     constants.RQServiceDefaultPort,
			DDServerPort:                    constants.DDServerDefaultPort,
			NumberOfChallengeReplicas:       constants.NumberOfChallengeReplicas,
			StorageChallengeExpiredDuration: constants.StorageChallengeExpiredDuration,
		})
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("Failed to get supernode config")
			return err
		}
		if err = utils.WriteFile(supernodeConfigPath, toolConfig); err != nil {
			log.WithContext(ctx).WithError(err).Errorf("Failed to update new supernode.yml file at - %s", supernodeConfigPath)
			return err
		}

	} else if err == nil {
		//update existing
		var snConfFile []byte
		snConfFile, err = ioutil.ReadFile(supernodeConfigPath)
		if err != nil {
			log.WithContext(ctx).WithError(err).Errorf("Failed to open existing supernode.yml file at - %s", supernodeConfigPath)
			return err
		}
		snConf := make(map[string]interface{})
		if err = yaml.Unmarshal(snConfFile, &snConf); err != nil {
			log.WithContext(ctx).WithError(err).Errorf("Failed to parse existing supernode.yml file at - %s", supernodeConfigPath)
			return err
		}

		node := snConf["node"].(map[interface{}]interface{})

		node["pastel_id"] = flagMasterNodePastelID
		node["pass_phrase"] = flagMasterNodePassPhrase
		node["storage_challenge_expired_duration"] = constants.StorageChallengeExpiredDuration
		node["number_of_challenge_replicas"] = constants.NumberOfChallengeReplicas

		var snConfFileUpdated []byte
		if snConfFileUpdated, err = yaml.Marshal(&snConf); err != nil {
			log.WithContext(ctx).WithError(err).Errorf("Failed to unparse yml for supernode.yml file at - %s", supernodeConfigPath)
			return err
		}
		if ioutil.WriteFile(supernodeConfigPath, snConfFileUpdated, 0644) != nil {
			log.WithContext(ctx).WithError(err).Errorf("Failed to update supernode.yml file at - %s", supernodeConfigPath)
			return err
		}
	} else {
		log.WithContext(ctx).WithError(err).Errorf("Failed to update or create supernode.yml file at - %s", supernodeConfigPath)
		return err
	}
	log.WithContext(ctx).Info("Supernode config updated")
	return nil
}
