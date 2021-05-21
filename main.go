package main

import (
	"github.com/fatih/color"
	"os"

	"github.com/pastelnetwork/pastel-utility/cmd"
	"github.com/pastelnetwork/gonode/common/errors"
	"github.com/pastelnetwork/gonode/common/log"
	"github.com/pastelnetwork/gonode/common/sys"
)

const (
	debugModeEnvName = "PASTEL_UTILITY_DEBUG"
)

var (
	debugMode = sys.GetBoolEnv(debugModeEnvName, false)
)

func main() {
	color.Cyan("hello")
	defer errors.Recover(log.FatalAndExit)

	app := cmd.NewApp()
	err := app.Run(os.Args)

	log.FatalAndExit(err)

}

func init() {
	log.SetDebugMode(debugMode)
}
