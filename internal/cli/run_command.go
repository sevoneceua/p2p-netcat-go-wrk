package cli

import (
	"fmt"
	"os"

	"github.com/santaklouse/go-p2p-netcat/internal/daemon"
	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var configPath string
	var quiet bool
	command := &cobra.Command{
		Use:   "run --config <path>",
		Short: "run every tunnel in a YAML config on one shared node (multi-tunnel daemon)",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("read config: %w", err)
			}
			cfg, err := tunnelconfig.Parse(data)
			if err != nil {
				return err
			}
			logger := func(format string, args ...any) {
				if quiet {
					return
				}
				fmt.Fprintf(os.Stderr, "[p2p-ncd] "+format+"\n", args...)
			}
			runningDaemon, err := daemon.Run(command.Context(), cfg, logger)
			if err != nil {
				return err
			}
			defer runningDaemon.Close()
			fmt.Fprintf(os.Stderr, "[p2p-ncd] PeerId: %s\n", runningDaemon.Node.Host.ID())
			for _, address := range runningDaemon.Node.Addresses() {
				fmt.Fprintf(os.Stderr, "[p2p-ncd] address: %s\n", address)
			}
			fmt.Fprintf(os.Stderr, "[p2p-ncd] %d tunnel(s) running; Ctrl+C to stop\n", len(cfg.Tunnels))
			<-command.Context().Done()
			return nil
		},
	}
	command.Flags().StringVarP(&configPath, "config", "c", "", "path to the tunnel YAML config (required)")
	command.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress diagnostics")
	return command
}
