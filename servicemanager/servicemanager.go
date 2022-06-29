package servicemanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pastelnetwork/gonode/common/log"
	"github.com/pastelnetwork/pastelup/configs"
	"github.com/pastelnetwork/pastelup/constants"
	"github.com/pastelnetwork/pastelup/utils"
)

// ServiceManager handles registering, starting and stopping system processes on the clients respective OS system manager (i.e. linux -> systemctl)
type ServiceManager interface {
	RegisterService(context.Context, constants.ToolType, ResgistrationParams) error
	StartService(context.Context, constants.ToolType) (bool, error)
	StopService(context.Context, constants.ToolType) error
	EnableService(context.Context, constants.ToolType) error
	DisableService(context.Context, constants.ToolType) error
	IsRunning(context.Context, constants.ToolType) bool
	IsRegistered(constants.ToolType) (bool, error)
	ServiceName(constants.ToolType) string
}

type systemdCmd string

const (
	start   systemdCmd = "start"
	stop    systemdCmd = "stop"
	enable  systemdCmd = "enable"
	disable systemdCmd = "disable"
	status  systemdCmd = "status"
)

// New returns a new serviceManager, if the OS does not have one configured, the error will be set and Noop Manager will be returned
func New(os constants.OSType, homeDir string) (ServiceManager, error) {
	switch os {
	case constants.Linux:
		return LinuxSystemdManager{
			homeDir: homeDir,
		}, nil
	}
	// if you don't want to check error, we return a noop manager that will do nothing since
	// the user's system is not supported for system management
	return NoopManager{}, fmt.Errorf("services are not comptabile with your OS (%v)", os)
}

// NoopManager can be used to do nothing if the OS doesnt have a system manager configured
type NoopManager struct{}

// RegisterService registers a service with the OS system manager
func (nm NoopManager) RegisterService(context.Context, constants.ToolType, ResgistrationParams) error {
	return nil
}

// StartService starts the given service as long as it is registered
func (nm NoopManager) StartService(context.Context, constants.ToolType) (bool, error) {
	return false, nil
}

// StopService stops a running service, it it isnt running it is a no-op
func (nm NoopManager) StopService(context.Context, constants.ToolType) error {
	return nil
}

// IsRunning checks to see if the service is running
func (nm NoopManager) IsRunning(context.Context, constants.ToolType) bool {
	return false
}

// EnableService checks to see if the service is running
func (nm NoopManager) EnableService(context.Context, constants.ToolType) error {
	return nil
}

// DisableService checks to see if the service is running
func (nm NoopManager) DisableService(context.Context, constants.ToolType) error {
	return nil
}

// IsRegistered checks if the associated app's system command file exists, if it does it returns true, else it returns false
// if err is not nil, there was an error checking the existence of the file
func (nm NoopManager) IsRegistered(constants.ToolType) (bool, error) {
	return false, nil
}

// ServiceName returns the formatted service name given a tooltype
func (nm NoopManager) ServiceName(constants.ToolType) string {
	return ""
}

// LinuxSystemdManager is a service manager for linux based OS
type LinuxSystemdManager struct {
	homeDir string
}

// ResgistrationParams additional flags to pass during service registration
type ResgistrationParams struct {
	Force       bool
	FlagDevMode bool
	Config      *configs.Config
}

// RegisterService registers the service and starts it
func (sm LinuxSystemdManager) RegisterService(ctx context.Context, app constants.ToolType, params ResgistrationParams) error {
	if isRegistered, _ := sm.IsRegistered(app); isRegistered {
		return nil // already registered
	}

	systemdDir := constants.SystemdSystemDir

	var systemdFile string
	var err error
	var execCmd, execPath, workDir string

	// Service file - will be installed at /etc/systemd/system
	appServiceFileName := sm.ServiceName(app)
	appServiceFilePath := filepath.Join(systemdDir, appServiceFileName)

	switch app {
	case constants.DDImgService:
		appBaseDir := filepath.Join(sm.homeDir, constants.DupeDetectionServiceDir)
		appServiceWorkDirPath := filepath.Join(appBaseDir, "img_server")
		execCmd = "python3 -m  http.server 8000"
		workDir = appServiceWorkDirPath
	case constants.PastelD:
		var extIP string
		// Get pasteld path
		execPath = filepath.Join(params.Config.PastelExecDir, constants.PasteldName[utils.GetOS()])
		if exists := utils.CheckFileExist(execPath); !exists {
			log.WithContext(ctx).WithError(err).Error(fmt.Sprintf("Could not find %v executable file", app))
			return err
		}
		// Get external IP
		if extIP, err = utils.GetExternalIPAddress(); err != nil {
			log.WithContext(ctx).WithError(err).Error("Could not get external IP address")
			return err
		}
		execCmd = execPath + " --datadir=" + params.Config.WorkingDir + " --externalip=" + extIP
		workDir = params.Config.PastelExecDir
	case constants.RQService:
		execPath = filepath.Join(params.Config.PastelExecDir, constants.PastelRQServiceExecName[utils.GetOS()])
		if exists := utils.CheckFileExist(execPath); !exists {
			log.WithContext(ctx).WithError(err).Error(fmt.Printf("Could not find %v executable file", app))
			return err
		}
		rqServiceArgs := fmt.Sprintf("--config-file=%s", params.Config.Configurer.GetRQServiceConfFile(params.Config.WorkingDir))
		execCmd = execPath + " " + rqServiceArgs
		workDir = params.Config.PastelExecDir
	case constants.DDService:
		execPath = filepath.Join(params.Config.PastelExecDir, utils.GetDupeDetectionExecName())
		if exists := utils.CheckFileExist(execPath); !exists {
			log.WithContext(ctx).WithError(err).Error(fmt.Printf("Could not find %v executable file", app))
			return err
		}
		ddConfigFilePath := filepath.Join(sm.homeDir,
			constants.DupeDetectionServiceDir,
			constants.DupeDetectionSupportFilePath,
			constants.DupeDetectionConfigFilename)
		execCmd = "python3 " + execPath + " " + ddConfigFilePath
		workDir = params.Config.PastelExecDir
	case constants.SuperNode:
		execPath = filepath.Join(params.Config.PastelExecDir, constants.SuperNodeExecName[utils.GetOS()])
		if exists := utils.CheckFileExist(execPath); !exists {
			log.WithContext(ctx).WithError(err).Error(fmt.Sprintf("Could not find %v executable file", app))
			return err
		}
		supernodeConfigPath := params.Config.Configurer.GetSuperNodeConfFile(params.Config.WorkingDir)
		execCmd = execPath + " --config-file=" + supernodeConfigPath
		workDir = params.Config.PastelExecDir
	case constants.WalletNode:
		execPath = filepath.Join(params.Config.PastelExecDir, constants.WalletNodeExecName[utils.GetOS()])
		if exists := utils.CheckFileExist(execPath); !exists {
			log.WithContext(ctx).WithError(err).Error(fmt.Sprintf("Could not find %v executable file", app))
			return err
		}
		walletnodeConfigFile := params.Config.Configurer.GetWalletNodeConfFile(params.Config.WorkingDir)
		execCmd = execPath + " --config-file=" + walletnodeConfigFile
		if params.FlagDevMode {
			execCmd += " --swagger"
		}
		workDir = params.Config.PastelExecDir
	default:
		return nil
	}

	// Create systemd file
	systemdFile, err = utils.GetServiceConfig(string(app), configs.SystemdService,
		&configs.SystemdServiceScript{
			Desc:    fmt.Sprintf("%v daemon", app),
			ExecCmd: execCmd,
			WorkDir: workDir,
		})
	if err != nil {
		e := fmt.Errorf("unable ot create service file for (%v): %v", app, err)
		log.WithContext(ctx).WithError(err).Error(e.Error())
		return e
	}

	// write systemdFile to home dir with the intention to move it
	// we write then move because it is hard to write directly to a protected directory using golang
	// tmpPath := filepath.Join(params.Config.WorkingDir, appServiceFileName)
	if err := ioutil.WriteFile(appServiceFilePath, []byte(systemdFile), 0644); err != nil {
		log.WithContext(ctx).WithError(err).Error("unable to write " + appServiceFileName + " file")
	}
	// c := fmt.Sprintf("mv %v %v", tmpPath, appServiceFilePath)
	// fmt.Printf("running sudo cmd: %v\n", c)
	// if _, err := runSudoCMD(params.Config, "mv", tmpPath, appServiceFileName); err != nil {
	// 	log.WithContext(ctx).WithError(err).Error("unable to write " + appServiceFileName + " file")
	// }
	// Enable service
	// @todo -- should this be optional? implications are at device reboot or startup, these services start automatically
	sm.EnableService(ctx, app)
	return nil
}

// StartService starts the given service as long as it is registered
func (sm LinuxSystemdManager) StartService(ctx context.Context, app constants.ToolType) (bool, error) {
	isRegisted, _ := sm.IsRegistered(app)
	if !isRegisted {
		log.WithContext(ctx).Infof("skipping start service because %v is not a registered service", app)
		return false, nil
	}
	isRunning := sm.IsRunning(ctx, app)
	if isRunning {
		log.WithContext(ctx).Infof("service %v is already running: noop", app)
		return true, nil
	}
	_, err := runSystemdCmd(start, sm.ServiceName(app))
	if err != nil {
		return false, fmt.Errorf("unable to start service (%v): %v", app, err)
	}
	return true, nil
}

// StopService stops a running service, it isn't running it is a no-op
func (sm LinuxSystemdManager) StopService(ctx context.Context, app constants.ToolType) error {
	err := sm.DisableService(ctx, app)
	if err != nil {
		return fmt.Errorf("unable to disable service (%v): %v", app, err)
	}
	isRunning := sm.IsRunning(ctx, app) // if not registered, this will be false
	if !isRunning {
		return nil // service isnt running, no need to stop
	}
	_, err = runSystemdCmd(stop, sm.ServiceName(app))
	if err != nil {
		return fmt.Errorf("unable to stop service (%v): %v", app, err)
	}
	return nil
}

// EnableService enables a systemd service
func (sm LinuxSystemdManager) EnableService(ctx context.Context, app constants.ToolType) error {
	appServiceFileName := sm.ServiceName(app)
	log.WithContext(ctx).Info("Enabiling service for auto-start")
	if out, err := runSystemdCmd(enable, appServiceFileName); err != nil {
		log.WithContext(ctx).WithFields(log.Fields{"message": out}).
			WithError(err).Error("unable to enable " + appServiceFileName + " service")
		return fmt.Errorf("err enabling "+appServiceFileName+" - err: %s", err)
	}
	return nil
}

// DisableService disables a systemd service
func (sm LinuxSystemdManager) DisableService(ctx context.Context, app constants.ToolType) error {
	appServiceFileName := sm.ServiceName(app)
	log.WithContext(ctx).Info("Disabling service")
	if out, err := runSystemdCmd(disable, appServiceFileName); err != nil {
		log.WithContext(ctx).WithFields(log.Fields{"message": out}).
			WithError(err).Error("unable to disable " + appServiceFileName + " service")
		return fmt.Errorf("err enabling "+appServiceFileName+" - err: %s", err)
	}
	return nil
}

// IsRunning checks to see if the service is running
func (sm LinuxSystemdManager) IsRunning(ctx context.Context, app constants.ToolType) bool {
	res, _ := runSystemdCmd(status, sm.ServiceName(app))
	isRunning := strings.Contains(res, "(running)")
	log.WithContext(ctx).Infof("%v status: %v", sm.ServiceName(app), res)
	return isRunning
}

// IsRegistered checks if the associated app's system command file exists, if it does, it returns true, else it returns false
// if err is not nil, there was an error checking the existence of the file
func (sm LinuxSystemdManager) IsRegistered(app constants.ToolType) (bool, error) {
	fp := filepath.Join(sm.homeDir, constants.SystemdUserDir, sm.ServiceName(app))
	if _, err := os.Stat(fp); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ServiceName returns the formatted service name given a tooltype
func (sm LinuxSystemdManager) ServiceName(app constants.ToolType) string {
	return fmt.Sprintf("%v%v.service", constants.SystemdServicePrefix, app)
}

func runSystemdCmd(command systemdCmd, serviceName string) (string, error) {
	//cmd := exec.Command("systemctl", "--user", string(command), serviceName)
	cmd := exec.Command("systemctl", string(command), serviceName)
	var stdBuffer bytes.Buffer
	mw := io.MultiWriter(os.Stdout, &stdBuffer)
	cmd.Stdout = mw
	cmd.Stderr = mw
	if err := cmd.Run(); err != nil {
		return stdBuffer.String(), err
	}
	return stdBuffer.String(), nil
}

/*
 * @todo these functions are copied from common.go file
 *	to avoid an import cycle. We need to move this shared functionality to its own package
 *  so that both the servicemanager and cmd packages can use it.
 *
 */
func runSudoCMD(config *configs.Config, args ...string) (string, error) {
	if len(config.UserPw) > 0 {
		return runCMD("bash", "-c", "echo "+config.UserPw+" | sudo -S "+strings.Join(args, " "))
	}
	return runCMD("sudo", args...)
}

// RunCMD runs shell command and returns output and error
func runCMD(command string, args ...string) (string, error) {
	return runCMDWithEnvVariable(command, "", "", args...)
}

// RunCMDWithEnvVariable runs shell command with environmental variable and returns output and error
func runCMDWithEnvVariable(command string, evName string, evValue string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)

	if len(evName) != 0 && len(evValue) != 0 {
		additionalEnv := fmt.Sprintf("%s=%s", evName, evValue)
		cmd.Env = append(os.Environ(), additionalEnv)
	}

	var stdBuffer bytes.Buffer
	mw := io.MultiWriter(os.Stdout, &stdBuffer)

	cmd.Stdout = mw
	cmd.Stderr = mw

	// Execute the command
	if err := cmd.Run(); err != nil {
		return stdBuffer.String(), err
	}

	return stdBuffer.String(), nil
}
