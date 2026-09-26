package cli

import (
	"fmt"

	"github.com/santaklouse/go-p2p-netcat/internal/service"
	"github.com/spf13/cobra"
)

// newServiceCommand groups the Epic-2 service management subcommands.
// Every subcommand calls straight into internal/service, which picks the
// right OS backend (Windows SCM / Linux systemd / an explicit
// not-yet-supported error elsewhere) at compile time — this file never
// branches on GOOS itself.
func newServiceCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "service",
		Short: "install/start/stop the multi-tunnel daemon as a background service",
	}
	command.AddCommand(
		newServiceInstallCommand(),
		newServiceUninstallCommand(),
		newServiceStartCommand(),
		newServiceStopCommand(),
		newServiceStatusCommand(),
		newServiceRunCommand(),
	)
	return command
}

func newServiceInstallCommand() *cobra.Command {
	var configPath string
	command := &cobra.Command{
		Use:   "install --config <path>",
		Short: "register the service (Windows: SCM, start automatically at boot; Linux: systemd unit, enabled)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}
			if err := service.Install(configPath); err != nil {
				return err
			}
			fmt.Printf("service %q installed (config: %s)\n", service.Name, configPath)
			return nil
		},
	}
	command.Flags().StringVarP(&configPath, "config", "c", "", "path to the tunnel YAML config (required)")
	return command
}

func newServiceUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "stop (if running) and remove the service registration",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := service.Uninstall(); err != nil {
				return err
			}
			fmt.Printf("service %q uninstalled\n", service.Name)
			return nil
		},
	}
}

func newServiceStartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "start the already-installed service",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := service.Start(); err != nil {
				return err
			}
			fmt.Printf("service %q started\n", service.Name)
			return nil
		},
	}
}

func newServiceStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "stop the running service",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := service.Stop(); err != nil {
				return err
			}
			fmt.Printf("service %q stopped\n", service.Name)
			return nil
		},
	}
}

func newServiceStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "print the service's current state",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			status, err := service.StatusText()
			if err != nil {
				return err
			}
			fmt.Println(status)
			return nil
		},
	}
}

// newServiceRunCommand is the hidden entry point the OS service manager
// itself launches (Windows: the exact command line Install registered
// with the SCM). It is not meant to be run by hand -- on Windows it
// refuses to do anything unless the Service Control Manager is actually
// the one starting it (see service.RunAsService), and on Linux it's
// never invoked at all (the systemd unit's ExecStart is the ordinary
// "run" command, not this).
func newServiceRunCommand() *cobra.Command {
	var configPath string
	command := &cobra.Command{
		Use:    "run-service --config <path>",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return service.RunAsService(configPath)
		},
	}
	command.Flags().StringVarP(&configPath, "config", "c", "", "path to the tunnel YAML config (required)")
	return command
}
