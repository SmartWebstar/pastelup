package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/pastelnetwork/gonode/common/cli"
	"github.com/pastelnetwork/gonode/common/log"
	"github.com/pastelnetwork/gonode/common/sys"
	"github.com/pastelnetwork/pastel-utility/configs"
)

var (
	errSubCommandRequired = fmt.Errorf("subcommand is required")
)

var (
	flagInteractiveMode bool
	flatRestart         bool
)

func setupStartCommand() *cli.Command {
	config := configs.New()

	startCommand := cli.NewCommand("start")
	startCommand.SetUsage("usage")
	addLogFlags(startCommand, config)

	superNodeSubcommand := cli.NewCommand("supernode")
	superNodeSubcommand.CustomHelpTemplate = GetColoredSubCommandHeaders()
	superNodeSubcommand.SetUsage(cyan("Starts supernode"))
	superNodeSubcommand.SetActionFunc(func(ctx context.Context, args []string) error {
		ctx, err := configureLogging(ctx, "superNodeSubCommand", config)
		if err != nil {
			return err
		}
		return runStartSuperNodeSubCommand(ctx, config)
	})
	superNodeFlags := []*cli.Flag{
		cli.NewFlag("i", &flagInteractiveMode),
		cli.NewFlag("r", &flatRestart),
	}
	superNodeSubcommand.AddFlags(superNodeFlags...)

	masterNodeSubCommand := cli.NewCommand("masternode")
	masterNodeSubCommand.CustomHelpTemplate = GetColoredSubCommandHeaders()
	masterNodeSubCommand.SetUsage(cyan("Starts master node"))
	masterNodeSubCommand.SetActionFunc(func(ctx context.Context, args []string) error {
		ctx, err := configureLogging(ctx, "masterNodeSubCommand", config)
		if err != nil {
			return err
		}
		return runStartMasterNodeSubCommand(ctx, config)
	})
	masterNodeFlags := []*cli.Flag{
		cli.NewFlag("i", &flagInteractiveMode),
		cli.NewFlag("r", &flatRestart),
	}
	masterNodeSubCommand.AddFlags(masterNodeFlags...)

	nodeSubCommand := cli.NewCommand("node")
	nodeSubCommand.CustomHelpTemplate = GetColoredSubCommandHeaders()
	nodeSubCommand.SetUsage(cyan("Starts specified node"))
	nodeSubCommand.SetActionFunc(func(ctx context.Context, args []string) error {
		ctx, err := configureLogging(ctx, "nodeSubCommand", config)
		if err != nil {
			return err
		}
		return runStartNodeSubCommand(ctx, config)
	})
	nodeFlags := []*cli.Flag{
		cli.NewFlag("i", &flagInteractiveMode),
		cli.NewFlag("r", &flatRestart),
	}
	nodeSubCommand.AddFlags(nodeFlags...)

	walletSubCommand := cli.NewCommand("wallet")
	walletSubCommand.CustomHelpTemplate = GetColoredSubCommandHeaders()
	walletSubCommand.SetUsage("Starts wallet")
	walletSubCommand.SetActionFunc(func(ctx context.Context, args []string) error {
		ctx, err := configureLogging(ctx, "nodeSubCommand", config)
		if err != nil {
			return err
		}
		return runStartWalletSubCommand(ctx, config)
	})
	walletFlags := []*cli.Flag{
		cli.NewFlag("i", &flagInteractiveMode),
		cli.NewFlag("r", &flatRestart),
	}
	walletSubCommand.AddFlags(walletFlags...)

	startCommand.AddSubcommands(
		superNodeSubcommand,
		masterNodeSubCommand,
		nodeSubCommand,
		walletSubCommand,
	)

	startCommand.SetActionFunc(func(ctx context.Context, args []string) error {
		ctx, err := configureLogging(ctx, "startcommand", config)
		if err != nil {
			return err
		}
		if len(args) == 0 {
			return errSubCommandRequired
		}
		return runStart(ctx, config)
	})
	return startCommand
}

func runStart(ctx context.Context, config *configs.Config) error {
	log.WithContext(ctx).Info("Start")
	defer log.WithContext(ctx).Info("End")

	configJSON, err := config.String()
	if err != nil {
		return err
	}
	log.WithContext(ctx).Infof("Config: %s", configJSON)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sys.RegisterInterruptHandler(cancel, func() {
		log.WithContext(ctx).Info("Interrupt signal received. Gracefully shutting down...")
		os.Exit(0)
	})

	// actions to run goes here

	return nil
}

func runStartNodeSubCommand(ctx context.Context, config *configs.Config) error {
	// TODO: Implement start node command
	panic("")
}

func runStartSuperNodeSubCommand(ctx context.Context, config *configs.Config) error {
	// TODO: Implement start supper node command
	panic("")
}

func runStartMasterNodeSubCommand(ctx context.Context, config *configs.Config) error {
	// TODO: Implement start master node command
	panic("")
}

func runStartWalletSubCommand(ctx context.Context, config *configs.Config) error {
	// TODO: Implement wallet command
	panic("")
}
