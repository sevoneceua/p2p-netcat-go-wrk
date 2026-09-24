package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/santaklouse/go-p2p-netcat/internal/daemon"
	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var configPath string
	var quiet bool
	var watchInterval time.Duration
	command := &cobra.Command{
		Use:   "run --config <path>",
		Short: "run every tunnel in a YAML config on one shared node (multi-tunnel daemon)",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}
			cfg, err := loadTunnelConfigFile(configPath)
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

			if watchInterval > 0 {
				go watchConfigFile(command.Context(), configPath, watchInterval, runningDaemon, logger)
			}

			<-command.Context().Done()
			return nil
		},
	}
	command.Flags().StringVarP(&configPath, "config", "c", "", "path to the tunnel YAML config (required)")
	command.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress diagnostics")
	command.Flags().DurationVar(&watchInterval, "watch-interval", 2*time.Second,
		"poll the config file for changes and hot-reload tunnels on edit; 0 disables watching")
	return command
}

func loadTunnelConfigFile(path string) (*tunnelconfig.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := tunnelconfig.Parse(data)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// watchConfigFile polls path's modification time every interval and calls
// Reload whenever it changes. Polling rather than a filesystem-event
// watcher (fsnotify or similar) is deliberate: it needs no extra
// dependency and behaves identically on every platform this project
// targets, which matters most exactly while the Windows track is the
// priority (native change-notification APIs differ enough between
// platforms, and SIGHUP-based reload has no real equivalent on Windows,
// that polling is the one mechanism that doesn't need a platform split).
// A parse or validation error in the edited file is logged and otherwise
// ignored: the daemon keeps running the last good config rather than
// tearing itself down over a typo the person is presumably about to fix.
func watchConfigFile(
	ctx context.Context,
	path string,
	interval time.Duration,
	runningDaemon *daemon.Daemon,
	logger daemon.Logger,
) {
	var lastModified time.Time
	if info, err := os.Stat(path); err == nil {
		lastModified = info.ModTime()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(path)
			if err != nil {
				continue // transient (e.g. mid-write on some editors); try again next tick
			}
			if !info.ModTime().After(lastModified) {
				continue
			}
			lastModified = info.ModTime()
			newCfg, err := loadTunnelConfigFile(path)
			if err != nil {
				logger("config reload: %v (keeping the previous config running)", err)
				continue
			}
			if err := runningDaemon.Reload(newCfg); err != nil {
				logger("config reload: %v (some tunnels may not have applied)", err)
				continue
			}
			logger("config reloaded from %s", path)
		}
	}
}
