// Package service installs, uninstalls, starts, and stops p2p-netcat's
// multi-tunnel daemon (internal/daemon, driven from the CLI's "run"
// command) as a background service: Windows Service Control Manager on
// Windows, a systemd unit on Linux. Every OS file in this package
// (service_windows.go, service_linux.go, service_other.go) exports the
// same five functions -- Install, Uninstall, Start, Stop, StatusText --
// so the CLI layer (internal/cli) never branches on GOOS itself.
//
// Deliberately no third-party service-manager dependency (kardianos/
// service and similar): golang.org/x/sys/windows/svc + svc/mgr are
// already a direct dependency of this module for Windows, and systemd
// unit generation on Linux needs nothing beyond the standard library, so
// this whole package adds zero new external dependencies.
package service

import (
	"fmt"
	"os"

	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
)

// Name is both the Windows service name and the systemd unit name (with
// ".service" appended there); it must be stable across versions, since
// Uninstall on an old install must still find what a newer binary's
// Install created.
const Name = "p2p-netcat"

// DisplayName and Description are cosmetic (Windows Services console,
// systemd unit file's Description=).
const (
	DisplayName = "p2p-netcat multi-tunnel daemon"
	Description = "Runs every tunnel in a p2p-netcat YAML config on one shared node (see 'p2p-nc run --config')."
)

// loadConfig re-reads and validates the tunnel config at path. Every
// Install implementation calls this before touching the OS's service
// manager, so a typo in the config file is caught before a service
// pointing at a broken config gets registered at all -- Install fails
// loudly instead of creating a service that will just fail every time
// the OS tries to start it.
func loadConfig(path string) (*tunnelconfig.Config, error) {
	if path == "" {
		return nil, fmt.Errorf("a config path is required")
	}
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

func resolveExecutablePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve the running executable's path: %w", err)
	}
	return exePath, nil
}
