//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// unitPath is where Install writes the systemd unit file. A package-level
// var, not a const, so tests can point it at a temp directory instead of
// the real /etc/systemd/system.
var unitPath = "/etc/systemd/system/" + Name + ".service"

// Install writes a systemd unit file whose ExecStart re-uses the ordinary
// "p2p-nc run --config <path>" command unchanged -- unlike Windows,
// systemd runs a unit as a plain foreground process and handles signals
// itself, so there is no separate service-mode entry point to write for
// this platform (contrast internal/service/service_windows.go's
// RunAsService, which Windows genuinely needs).
func Install(configPath string) error {
	if _, err := loadConfig(configPath); err != nil {
		return fmt.Errorf("config is invalid, not installing the service: %w", err)
	}
	absoluteConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	exePath, err := resolveExecutablePath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(unitPath); err == nil {
		return fmt.Errorf("unit file %s already exists (uninstall first)", unitPath)
	}
	unit := buildSystemdUnit(exePath, absoluteConfigPath)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write unit file %s (this needs root): %w", unitPath, err)
	}

	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl("enable", Name); err != nil {
		return err
	}
	return nil
}

// Uninstall disables and removes the unit; it does not error if the unit
// was already stopped.
func Uninstall() error {
	_ = runSystemctl("disable", "--now", Name)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file %s: %w", unitPath, err)
	}
	return runSystemctl("daemon-reload")
}

// Start starts the already-installed unit.
func Start() error {
	return runSystemctl("start", Name)
}

// Stop stops the unit; systemctl stop on an already-stopped unit is a
// no-op that exits 0, so this stays idempotent without any extra checks.
func Stop() error {
	return runSystemctl("stop", Name)
}

// StatusText reports systemd's own active-state word for the unit
// ("active", "inactive", "failed", ...), or an error if the unit isn't
// installed at all.
func StatusText() (string, error) {
	if _, err := os.Stat(unitPath); err != nil {
		return "", fmt.Errorf("unit %s is not installed", Name)
	}
	output, _ := exec.Command("systemctl", "is-active", Name).CombinedOutput()
	// systemctl is-active exits non-zero for every state except
	// "active", so its error (if any) is not itself the failure here --
	// the printed word is the answer either way ("inactive", "failed",
	// "activating", ...).
	return strings.TrimSpace(string(output)), nil
}

// RunAsService exists only so the CLI command tree is identical across
// platforms; Linux systemd runs a unit as a plain foreground process and
// never calls this (its ExecStart is "p2p-nc run --config <path>",
// exactly the ordinary command), so this is never actually invoked in
// normal use here -- unlike service_windows.go's RunAsService, which the
// Service Control Manager genuinely does call.
func RunAsService(string) error {
	return fmt.Errorf("run-service is a Windows-only entry point; use 'p2p-nc run --config <path>' directly on Linux")
}

func runSystemctl(args ...string) error {
	output, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

// buildSystemdUnit is pure (no filesystem/process access) so it can be
// unit-tested without root or a running systemd.
func buildSystemdUnit(exePath, configPath string) string {
	return fmt.Sprintf(`[Unit]
Description=%s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run --config %s
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
`, Description, quoteArg(exePath), quoteArg(configPath))
}

// quoteArg wraps an argument in double quotes for the unit file's
// ExecStart= line (systemd's own quoting rules, not the shell's -- it
// never goes through a shell). A path containing a literal double quote
// is exotic enough not to worry about here.
func quoteArg(arg string) string {
	return `"` + arg + `"`
}
