package cmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pastelnetwork/gonode/common/cli"
	"github.com/pastelnetwork/gonode/common/errors"
	"github.com/pastelnetwork/gonode/common/log"
	"github.com/pastelnetwork/gonode/common/sys"
	"github.com/pastelnetwork/pastel-utility/configs"
	"github.com/pastelnetwork/pastel-utility/constants"
	"github.com/pastelnetwork/pastel-utility/utils"
)

var (
	sshIP   string
	sshPort int
	sshKey  string
)

type installCommand uint8

const (
	nodeInstall installCommand = iota
	walletInstall
	superNodeInstall
	remoteInstall
	dupedetectionInstall
	//highLevel
)

func setupSubCommand(config *configs.Config,
	installCommand installCommand,
	f func(context.Context, *configs.Config) error,
) *cli.Command {
	commonFlags := []*cli.Flag{
		cli.NewFlag("network", &config.Network).SetAliases("n").
			SetUsage(green("Optional, network type, can be - \"mainnet\" or \"testnet\"")).SetValue("mainnet"),
		cli.NewFlag("force", &config.Force).SetAliases("f").
			SetUsage(green("Optional, Force to overwrite config files and re-download ZKSnark parameters")),
		cli.NewFlag("peers", &config.Peers).SetAliases("p").
			SetUsage(green("Optional, List of peers to add into pastel.conf file, must be in the format - \"ip\" or \"ip:port\"")),
		cli.NewFlag("release", &config.Version).SetAliases("r").
			SetUsage(green("Optional, Pastel version to install")).SetValue("beta"),
	}

	var dirsFlags []*cli.Flag

	if installCommand != remoteInstall {
		dirsFlags = []*cli.Flag{
			cli.NewFlag("dir", &config.PastelExecDir).SetAliases("d").
				SetUsage(green("Optional, Location where to create pastel node directory")).SetValue(config.Configurer.DefaultPastelExecutableDir()),
			cli.NewFlag("work-dir", &config.WorkingDir).SetAliases("w").
				SetUsage(green("Optional, Location where to create working directory")).SetValue(config.Configurer.DefaultWorkingDir()),
		}
	} else {
		dirsFlags = []*cli.Flag{
			cli.NewFlag("dir", &config.RemotePastelExecDir).SetAliases("d").
				SetUsage(green("Optional, Location where to create pastel node directory on the remote computer (default: $HOME/pastel-utility)")),
			cli.NewFlag("work-dir", &config.RemoteWorkingDir).SetAliases("w").
				SetUsage(green("Optional, Location where to create working directory on the remote computer (default: $HOME/pastel-utility)")),
		}
	}

	remoteFlags := []*cli.Flag{
		cli.NewFlag("ssh-ip", &sshIP).
			SetUsage(yellow("Required, SSH address of the remote host")).SetRequired(),
		cli.NewFlag("ssh-port", &sshPort).
			SetUsage(yellow("Optional, SSH port of the remote host, default is 22")).SetValue(22),
		cli.NewFlag("ssh-key", &sshKey).
			SetUsage(yellow("Optional, Path to SSH private key")),
		cli.NewFlag("ssh-dir", &config.RemotePastelUtilityDir).SetAliases("rpud").
			SetUsage(yellow("Required, Location where to copy pastel-utility on the remote computer")).SetRequired(),
	}

	dupeFlags := []*cli.Flag{
		cli.NewFlag("force", &config.Force).SetAliases("f").
			SetUsage(green("Optional, Force to overwrite config files and re-download ZKSnark parameters")),
	}

	var commandName, commandMessage string
	var commandFlags []*cli.Flag

	switch installCommand {
	case nodeInstall:
		commandFlags = append(dirsFlags, commonFlags[:]...)
		commandName = "node"
		commandMessage = "Install node"
	case walletInstall:
		commandFlags = append(dirsFlags, commonFlags[:]...)
		commandName = "walletnode"
		commandMessage = "Install walletnode"
	case superNodeInstall:
		commandFlags = append(dirsFlags, commonFlags[:]...)
		commandName = "supernode"
		commandMessage = "Install supernode"
	case remoteInstall:
		commandFlags = append(append(dirsFlags, commonFlags[:]...), remoteFlags[:]...)
		commandName = "remote"
		commandMessage = "Install supernode remote"
	case dupedetectionInstall:
		commandFlags = dupeFlags
		commandName = "dupedetection"
		commandMessage = "Install dupedetection"
	default:
		commandFlags = append(append(dirsFlags, commonFlags[:]...), remoteFlags[:]...)
	}

	subCommand := cli.NewCommand(commandName)
	subCommand.SetUsage(cyan(commandMessage))
	subCommand.AddFlags(commandFlags...)
	if f != nil {
		subCommand.SetActionFunc(func(ctx context.Context, args []string) error {
			ctx, err := configureLogging(ctx, commandMessage, config)
			if err != nil {
				//Logger doesn't exist
				return err
			}

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			sys.RegisterInterruptHandler(cancel, func() {
				log.WithContext(ctx).Info("Interrupt signal received. Gracefully shutting down...")
				os.Exit(0)
			})

			log.WithContext(ctx).Info("Started")
			if err = f(ctx, config); err != nil {
				return err
			}
			log.WithContext(ctx).Info("Finished successfully!")
			return nil
		})
	}
	return subCommand
}

func setupInstallCommand() *cli.Command {
	config := configs.GetConfig()

	installNodeSubCommand := setupSubCommand(config, nodeInstall, runInstallNodeSubCommand)
	installWalletSubCommand := setupSubCommand(config, walletInstall, runInstallWalletSubCommand)
	installSuperNodeSubCommand := setupSubCommand(config, superNodeInstall, runInstallSuperNodeSubCommand)
	installSuperNodeRemoteSubCommand := setupSubCommand(config, remoteInstall, runInstallSuperNodeRemoteSubCommand)
	installSuperNodeSubCommand.AddSubcommands(installSuperNodeRemoteSubCommand)
	installDupeDetecionSubCommand := setupSubCommand(config, dupedetectionInstall, runInstallDupeDetectionSubCommand)

	installCommand := cli.NewCommand("install")
	installCommand.SetUsage(blue("Performs installation and initialization of the system for both WalletNode and SuperNodes"))
	installCommand.AddSubcommands(installNodeSubCommand)
	installCommand.AddSubcommands(installWalletSubCommand)
	installCommand.AddSubcommands(installSuperNodeSubCommand)
	installCommand.AddSubcommands(installDupeDetecionSubCommand)
	//installCommand := setupSubCommand(config, highLevel, nil)

	return installCommand
}

func runInstallNodeSubCommand(ctx context.Context, config *configs.Config) (err error) {
	//print(config.String())
	//return nil
	return runComponentsInstall(ctx, config, constants.PastelD)
}

func runInstallWalletSubCommand(ctx context.Context, config *configs.Config) (err error) {
	return runComponentsInstall(ctx, config, constants.WalletNode)
}

func runInstallSuperNodeSubCommand(ctx context.Context, config *configs.Config) (err error) {
	return runComponentsInstall(ctx, config, constants.SuperNode)
}

func runInstallSuperNodeRemoteSubCommand(ctx context.Context, config *configs.Config) (err error) {
	if len(sshIP) == 0 {
		return fmt.Errorf("--ssh-ip IP address - Required, SSH address of the remote host")
	}

	if len(config.RemotePastelUtilityDir) == 0 {
		return fmt.Errorf("--ssh-dir RemotePastelUtilityDir - Required, pastel-utility path of the remote host")
	}

	var client *utils.Client
	log.WithContext(ctx).Infof("Connecting to SSH Hot node wallet -> %s:%d...", sshIP, sshPort)
	if len(sshKey) == 0 {
		username, password, _ := credentials(true)
		client, err = utils.DialWithPasswd(fmt.Sprintf("%s:%d", sshIP, sshPort), username, password)
	} else {
		username, _, _ := credentials(false)
		client, err = utils.DialWithKey(fmt.Sprintf("%s:%d", sshIP, sshPort), username, sshKey)
	}
	if err != nil {
		return err
	}

	defer client.Close()

	log.WithContext(ctx).Info("Connected successfully")

	pastelUtilityPath := filepath.Join(config.RemotePastelUtilityDir, "pastel-utility")
	pastelUtilityPath = strings.ReplaceAll(pastelUtilityPath, "\\", "/")
	pastelUtilityDownloadPath := constants.PastelUtilityDownloadURL

	_, err = client.Cmd(fmt.Sprintf("rm -r -f %s", pastelUtilityPath)).Output()
	if err != nil {
		log.WithContext(ctx).Error("Failed to delete pastel-utility file")
		return err
	}

	log.WithContext(ctx).Info("Downloading Pastel-Utility Executable...")
	_, err = client.Cmd(fmt.Sprintf("wget -O %s %s", pastelUtilityPath, pastelUtilityDownloadPath)).Output()

	log.WithContext(ctx).Debugf("wget -O %s  %s", pastelUtilityPath, pastelUtilityDownloadPath)
	if err != nil {
		log.WithContext(ctx).Error("Failed to download pastel-utility")
		return err
	}

	log.WithContext(ctx).Info("Finished Downloading Pastel-Utility Successfully")

	_, err = client.Cmd(fmt.Sprintf("chmod 777 /%s", pastelUtilityPath)).Output()
	if err != nil {
		log.WithContext(ctx).Error("Failed to change permission of pastel-utility")
		return err
	}

	_, err = client.Cmd(fmt.Sprintf("%s stop supernode ", pastelUtilityPath)).Output()
	if err != nil {
		log.WithContext(ctx).Errorf("failed to stop supernode, err: %s", err)
		return err
	}

	log.WithContext(ctx).Info("Installing Supernode ...")

	log.WithContext(ctx).Debugf("pastel-utility path: %s", pastelUtilityPath)

	remoteOptions := ""
	if len(config.RemotePastelExecDir) > 0 {
		remoteOptions = fmt.Sprintf("%s --dir=%s", remoteOptions, config.RemotePastelExecDir)
	}

	if len(config.RemoteWorkingDir) > 0 {
		remoteOptions = fmt.Sprintf("%s --work-dir=%s", remoteOptions, config.RemoteWorkingDir)
	}

	if config.Force {
		remoteOptions = fmt.Sprintf("%s --force", remoteOptions)
	}

	if len(config.Version) > 0 {
		remoteOptions = fmt.Sprintf("%s --release=%s", remoteOptions, config.Version)
	}

	if len(config.Peers) > 0 {
		remoteOptions = fmt.Sprintf("%s --peers=%s", remoteOptions, config.Peers)
	}

	stdin := bytes.NewBufferString(fmt.Sprintf("/%s install supernode%s", pastelUtilityPath, remoteOptions))
	var stdout, stderr io.Writer

	return client.Shell().SetStdio(stdin, stdout, stderr).Start()
}

func runInstallDupeDetectionSubCommand(ctx context.Context, config *configs.Config) error {
	return installDupeDetection(ctx, config)
}

func runComponentsInstall(ctx context.Context, config *configs.Config, installCommand constants.ToolType) (err error) {

	if err = CreateUtilityConfigFile(ctx, config); err != nil {
		log.WithContext(ctx).WithError(err).Error("Failed to create pastel-utility config file")
		return err
	}

	if err = createInstallDir(ctx, config, config.PastelExecDir); err != nil {
		//error was logged inside createInstallDir
		return err
	}

	if err = checkInstalledPackages(ctx); err != nil {
		log.WithContext(ctx).WithError(err).Error("Missing packages...")
		return err
	}

	if installCommand == constants.PastelD ||
		installCommand == constants.WalletNode ||
		installCommand == constants.SuperNode {
		if err = installComponent(ctx, config, constants.PastelD, config.Version); err != nil {
			return err
		}
	}
	if installCommand == constants.WalletNode ||
		installCommand == constants.SuperNode {
		if err = installComponent(ctx, config, constants.RQService, config.Version); err != nil {
			return err
		}
	}
	if installCommand == constants.WalletNode {
		if err = installComponent(ctx, config, constants.WalletNode, config.Version); err != nil {
			return err
		}
	}
	if installCommand == constants.SuperNode {
		if err = installComponent(ctx, config, constants.SuperNode, config.Version); err != nil {
			return err
		}
		// Open ports
		openErr := openPort(ctx, constants.PortList)
		if openErr != nil {
			return openErr
		}

		if err = installDupeDetection(ctx, config); err != nil {
			log.WithContext(ctx).WithError(err).Error("Installing dd-service executable failed")
			return err
		}
	}

	return nil
}

func createInstallDir(ctx context.Context, config *configs.Config, installPath string) error {
	defer log.WithContext(ctx).Infof("Install path is %s", installPath)

	if err := utils.CreateFolder(ctx, installPath, config.Force); os.IsExist(err) {
		reader := bufio.NewReader(os.Stdin)
		log.WithContext(ctx).Warnf("%s. Do you want continue to install? Y/N", err.Error())
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			log.WithContext(ctx).WithError(readErr).Error("Exiting...")
			return readErr
		}

		if strings.TrimSpace(line) == "Y" || strings.TrimSpace(line) == "y" {
			config.Force = true
			if err = utils.CreateFolder(ctx, installPath, config.Force); err != nil {
				log.WithContext(ctx).WithError(err).Error("Exiting...")
				return err
			}
		} else {
			log.WithContext(ctx).Warn("Exiting...")
			return err
		}
	}

	return nil
}

func checkInstalledPackages(ctx context.Context) (err error) {
	// TODO: 1) must offer to install missing packages
	// TODO: 2) add support for Windows and Mac
	if utils.GetOS() == constants.Linux {
		installedCmd := utils.GetInstalledPackages(ctx)
		var notInstall []string
		for _, p := range constants.DependenciesPackages {
			if _, ok := installedCmd[p]; !ok {
				notInstall = append(notInstall, p)
			}
		}

		if len(notInstall) > 0 {
			return errors.New(strings.Join(notInstall, ", ") + " is missing from your OS, which is required for running, please install them")
		}
	}
	return nil
}

func installComponent(ctx context.Context, config *configs.Config, installCommand constants.ToolType, version string) (err error) {
	commandName := strings.Split(string(installCommand), "/")[len(strings.Split(string(installCommand), "/"))-1]
	log.WithContext(ctx).Infof("Installing %s executable...", commandName)

	downloadURL, execArchiveName, err := config.Configurer.GetDownloadURL(version, installCommand)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("failed to get download url")
		return err
	}

	if err = installExecutable(ctx, config, downloadURL.String(), execArchiveName, installCommand); err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Install %s executable failed", commandName)
		return err
	}

	log.WithContext(ctx).Infof("%s executable installed successfully", commandName)

	return nil
}

func installExecutable(ctx context.Context, config *configs.Config,
	downloadURL string,
	archiveName string,
	toolType constants.ToolType) (err error) {

	if err = utils.DownloadFile(ctx, filepath.Join(config.PastelExecDir, archiveName), downloadURL); err != nil {
		log.WithContext(ctx).Errorf(fmt.Sprintf("Failed to download pastel executable file : %s", downloadURL))
		return err
	}

	log.WithContext(ctx).Info("Installing...")

	if strings.Contains(downloadURL, ".zip") {
		if err = processArchive(ctx, config.PastelExecDir, filepath.Join(config.PastelExecDir, archiveName)); err != nil {
			//Error was logged in processArchive
			return err
		}
	}

	switch toolType {
	case constants.PastelD:
		if err = makeExecutable(ctx, config.PastelExecDir, constants.PasteldName[utils.GetOS()]); err != nil {
			return err
		}
		if err = makeExecutable(ctx, config.PastelExecDir, constants.PastelCliName[utils.GetOS()]); err != nil {
			return err
		}
		if err = setupBasePasteWorkingEnvironment(ctx, config); err != nil {
			log.WithContext(ctx).WithError(err).Error("Initialize the node")
			return err
		}
	case constants.WalletNode:
		if err = makeExecutable(ctx, config.PastelExecDir, constants.WalletNodeExecName[utils.GetOS()]); err != nil {
			return err
		}

		toolConfig := fmt.Sprintf(configs.WalletDefaultConfig, config.RPCPort, config.RPCUser, config.RPCPwd, "50051")
		setupComponentWorkingEnvironment(ctx, config, "walletnode", "walletnode.yml", toolConfig)

	case constants.SuperNode:
		if err = makeExecutable(ctx, config.PastelExecDir, constants.SuperNodeExecName[utils.GetOS()]); err != nil {
			return err
		}

		toolConfig := fmt.Sprintf(configs.SupernodeDefaultConfig, config.RPCPort, config.RPCUser, config.RPCPwd, "50051")
		setupComponentWorkingEnvironment(ctx, config, "supernode", "supernode.yml", toolConfig)
	case constants.RQService:
		if err = makeExecutable(ctx, config.PastelExecDir, constants.PastelRQServiceExecName[utils.GetOS()]); err != nil {
			return err
		}

		toolConfig := fmt.Sprintf(configs.RQServiceConfig, "127.0.0.1", "50051")
		setupComponentWorkingEnvironment(ctx, config, "rqservice", "rqservice.toml", toolConfig)

	default:
		log.WithContext(ctx).Warn("Please select correct tool type!")
		return nil
	}

	return nil
}

func processArchive(ctx context.Context, dstFolder string, archivePath string) error {
	log.WithContext(ctx).Debugf("Extracting archive files from %s to %s", archivePath, dstFolder)

	file, err := os.Open(archivePath)
	if err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Not found archive file - %s", archivePath)
		return err
	}
	defer file.Close()
	_, err = utils.Unzip(archivePath, dstFolder)
	if err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Failed to extract executables from %s", archivePath)
		return err
	}
	log.WithContext(ctx).Debug("Delete archive files")
	if err = utils.DeleteFile(archivePath); err != nil {
		log.WithContext(ctx).Errorf("Failed to delete archive file : %s", archivePath)
		return err
	}

	return nil
}

func makeExecutable(ctx context.Context, dirPath string, fileName string) error {
	if utils.GetOS() == constants.Linux {
		if _, err := RunCMD("chmod", "777", filepath.Join(dirPath, fileName)); err != nil {
			log.WithContext(ctx).Errorf("Failed to make %s as executable", fileName)
			return err
		}
	}
	return nil
}

func setupComponentWorkingEnvironment(ctx context.Context, config *configs.Config,
	toolName string, configFileName string, toolConfig string) error {

	log.WithContext(ctx).Infof("Initialize working directory for %s", toolName)

	workDirPath := filepath.Join(config.WorkingDir, toolName)
	if err := utils.CreateFolder(ctx, workDirPath, config.Force); err != nil {
		log.WithContext(ctx).Errorf("Failed to %s directory ", workDirPath)
		return err
	}

	fileName, err := utils.CreateFile(ctx, filepath.Join(workDirPath, configFileName), config.Force)
	if err != nil {
		log.WithContext(ctx).Errorf("Failed to create %s file", configFileName)
		return err
	}

	if err = utils.WriteFile(fileName, toolConfig); err != nil {
		log.WithContext(ctx).Errorf("Failed to write config to %s file", configFileName)
		return err
	}

	return nil
}

func setupBasePasteWorkingEnvironment(ctx context.Context, config *configs.Config) error {

	// create working dir
	if err := utils.CreateFolder(ctx, config.WorkingDir, config.Force); err != nil {
		if config.WorkingDir != config.PastelExecDir {
			log.WithContext(ctx).WithError(err).Errorf("Failed to create folder %s", config.WorkingDir)
			return err
		}
	}

	config.RPCPort = "9932"
	if config.Network == "testnet" {
		config.RPCPort = "19932"
	}
	config.RPCUser = utils.GenerateRandomString(8)
	config.RPCPwd = utils.GenerateRandomString(15)

	// create pastel.conf file
	f, err := utils.CreateFile(ctx, config.WorkingDir+"/pastel.conf", config.Force)
	if err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Failed to create %s/pastel.conf", config.WorkingDir)
		return err
	}

	// write to file
	if err = updatePastelConfigFile(ctx, f, config); err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Failed to update %s/pastel.conf", config.WorkingDir)
		return err
	}

	// create zksnark parameters path
	if err := utils.CreateFolder(ctx, config.Configurer.DefaultZksnarkDir(), config.Force); err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Failed to update folder %s", config.Configurer.DefaultZksnarkDir())
		return err
	}

	// download zksnark params
	if err := downloadZksnarkParams(ctx, config.Configurer.DefaultZksnarkDir(), config.Force); err != nil &&
		!(os.IsExist(err) && !config.Force) {
		log.WithContext(ctx).WithError(err).Errorf("Failed to download Zksnark parameters into folder %s", config.Configurer.DefaultZksnarkDir())
		return err
	}

	return nil
}

func updatePastelConfigFile(ctx context.Context, fileName string, config *configs.Config) error {
	// Open file using READ & WRITE permission.
	var file, err = os.OpenFile(fileName, os.O_RDWR, 0644)
	if err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Failed to open %s", fileName)
		return err
	}
	defer file.Close()

	// Populate pastel.conf line-by-line to file.
	_, err = file.WriteString("server=1\n") // creates server line
	if err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Failed to write into %s", fileName)
		return err
	}

	_, err = file.WriteString("listen=1\n\n") // creates server line
	if err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Failed to write into %s", fileName)
		return err
	}

	_, err = file.WriteString("rpcuser=" + config.RPCUser + "\n") // creates  rpcuser line
	if err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Failed to write into %s", fileName)
		return err
	}

	_, err = file.WriteString("rpcpassword=" + config.RPCPwd + "\n") // creates rpcpassword line
	if err != nil {
		log.WithContext(ctx).WithError(err).Errorf("Failed to write into %s", fileName)
		return err
	}

	if config.Network == "testnet" {
		_, err = file.WriteString("testnet=1\n") // creates testnet line
		if err != nil {
			log.WithContext(ctx).WithError(err).Errorf("Failed to write into %s", fileName)
			return err
		}
	}

	if config.Peers != "" {
		nodes := strings.Split(config.Peers, ",")
		for _, node := range nodes {
			_, err = file.WriteString("addnode=" + node + "\n") // creates addnode line
			if err != nil {
				log.WithContext(ctx).WithError(err).Errorf("Failed to write into %s", fileName)
				return err
			}
		}

	}

	// Save file changes.
	err = file.Sync()
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("Error saving file")
		return errors.Errorf("failed to save file changes: %v", err)
	}

	log.WithContext(ctx).Info("File updated successfully")

	return nil
}

func downloadZksnarkParams(ctx context.Context, path string, force bool) error {
	log.WithContext(ctx).Info("Downloading pastel-param files:")
	for _, zksnarkParamsName := range configs.ZksnarkParamsNames {
		checkSum := ""
		zksnarkParamsPath := filepath.Join(path, zksnarkParamsName)
		log.WithContext(ctx).Infof("Downloading: %s", zksnarkParamsPath)
		_, err := os.Stat(zksnarkParamsPath)
		// check if file exists and force is not set
		if err == nil && !force {
			log.WithContext(ctx).WithError(err).Errorf("Pastel param file already exists %s", zksnarkParamsPath)
			return errors.Errorf("pastel-param exists:  %s", zksnarkParamsPath)

		} else if err == nil {

			checkSum, err = utils.GetChecksum(ctx, zksnarkParamsPath)
			if err != nil {
				log.WithContext(ctx).WithError(err).Errorf("Checking pastel param file failed: %s", zksnarkParamsPath)
				return err
			}
		}

		if checkSum != constants.PastelParamsCheckSums[zksnarkParamsName] {
			err := utils.DownloadFile(ctx, zksnarkParamsPath, configs.ZksnarkParamsURL+zksnarkParamsName)
			if err != nil {
				log.WithContext(ctx).WithError(err).Errorf("Failed to download file: %s", configs.ZksnarkParamsURL+zksnarkParamsName)
				return err
			}
		} else {
			log.WithContext(ctx).Infof("Pastel param file %s already exists and checksum matched, so skipping download.", zksnarkParamsName)
		}

	}

	log.WithContext(ctx).Info("Pastel params downloaded.\n")

	return nil

}

func openPort(ctx context.Context, portList []string) (err error) {
	var out string
	for k := range portList {
		log.WithContext(ctx).Infof("Opening port: %s", portList[k])

		switch utils.GetOS() {
		case constants.Linux:
			out, err = RunCMD("sudo", "ufw", "allow", portList[k])
		case constants.Windows:
			out, err = RunCMD("netsh", "advfirewall", "firewall", "add", "rule", "name=TCP Port "+portList[k], "dir=in", "action=allow", "protocol=TCP", "localport="+portList[k])
		case constants.Mac:
			out, err = RunCMD("sudo", "ipfw", "allow", "tcp", "from", "any", "to", "any", "dst-port", portList[k])
		}

		if err != nil {
			if utils.GetOS() == constants.Windows {
				log.WithContext(ctx).Error("Please run as administrator to open ports!")
			}
			log.WithContext(ctx).Error(err.Error())
			return err
		}
		log.WithContext(ctx).Info(out)
	}

	return nil
}

func installDupeDetection(ctx context.Context, config *configs.Config) (err error) {
	log.WithContext(ctx).Info("Installing dd-service...")

	subCmd := []string{"-m", "pip", "install"}
	subCmd = append(subCmd, constants.DependenciesDupeDetectionPackages...)

	log.WithContext(ctx).Info("Installing Pip...")
	if utils.GetOS() == constants.Windows {
		if err := RunCMDWithInteractive("python", subCmd...); err != nil {
			return err
		}
	} else {
		if err := RunCMDWithInteractive("python3", subCmd...); err != nil {
			return err
		}
	}
	log.WithContext(ctx).Info("Pip install finished")

	if err = installChrome(ctx, config); err != nil {
		return err
	}

	downloadURL, execArchiveName, err := config.Configurer.GetDownloadURL(config.Version, constants.DDService)
	if err != nil {
		return errors.Errorf("failed to get download url, err: %s", err)
	}

	if err = installExecutable(ctx, config, downloadURL.String(), execArchiveName, constants.DDService); err != nil {
		log.WithContext(ctx).Errorf("Install %s executable failed", constants.DDService)
		return err
	}
	log.WithContext(ctx).Infof("%s executable installed successfully", constants.DDService)

	homeDir := config.WorkingDir
	homeDir = filepath.Join(homeDir, "pastel_dupe_detection_service")
	var pathList []interface{}
	for index := range constants.DupeDetectionConfigs {
		dupeDetectionDirPath := filepath.Join(homeDir, constants.DupeDetectionConfigs[index])
		if err = utils.CreateFolder(ctx, dupeDetectionDirPath, config.Force); err != nil {
			return err
		}
		pathList = append(pathList, dupeDetectionDirPath)
	}

	targetDir := filepath.Join(homeDir, constants.DupeDetectionSupportFilePath)
	for index := range constants.DupeDetectionSupportDownloadURL {
		if err = utils.DownloadFile(ctx,
			filepath.Join(targetDir, "temp.zip"),
			constants.DupeDetectionSupportDownloadURL[index]); err != nil {
			log.WithContext(ctx).Errorf("Failed to download archive file : %s", constants.DupeDetectionSupportDownloadURL[index])
			return err
		}

		log.WithContext(ctx).Infof("Extracting archive file : %s", filepath.Join(targetDir, "temp.zip"))
		if err = processArchive(ctx, targetDir, filepath.Join(targetDir, "temp.zip")); err != nil {
			log.WithContext(ctx).Errorf("Failed to extract archive file : %s", filepath.Join(targetDir, "temp.zip"))
			return err
		}
	}

	targetDir = filepath.Join(homeDir, constants.DupeDetectionSupportFilePath)
	fileName, err := utils.CreateFile(ctx, filepath.Join(targetDir, "config.ini"), config.Force)
	if err != nil {
		return err
	}

	if err = utils.WriteFile(fileName, fmt.Sprintf(configs.DupeDetectionConfig, pathList...)); err != nil {
		return err
	}

	configPath := filepath.Join(targetDir, "config.ini")

	if utils.GetOS() == constants.Linux {
		RunCMDWithInteractive("export", "DUPEDETECTIONCONFIGPATH=%s", configPath)
	}

	log.WithContext(ctx).Info("Installing DupeDetection finished successfully")
	return nil
}

func installChrome(ctx context.Context, config *configs.Config) (err error) {
	if utils.GetOS() == constants.Linux {
		log.WithContext(ctx).Infof("Downloading Chrome to install: %s \n", constants.ChromeDownloadURL[utils.GetOS()])

		err = utils.DownloadFile(ctx, filepath.Join(config.PastelExecDir, constants.ChromeExecFileName[utils.GetOS()]), constants.ChromeDownloadURL[utils.GetOS()])
		if err != nil {
			return err
		}

		if _, err = RunCMD("chmod", "777",
			filepath.Join(config.PastelExecDir, constants.ChromeExecFileName[utils.GetOS()])); err != nil {
			log.WithContext(ctx).Error("Failed to make chrome-install as executable")
			return err
		}

		log.WithContext(ctx).Infof("Installing Chrome : %s \n", filepath.Join(config.PastelExecDir, constants.ChromeExecFileName[utils.GetOS()]))

		RunCMDWithInteractive("sudo", "dpkg", "-i", filepath.Join(config.PastelExecDir, constants.ChromeExecFileName[utils.GetOS()]))

		utils.DeleteFile(filepath.Join(config.PastelExecDir, constants.ChromeExecFileName[utils.GetOS()]))
	}
	return nil
}
