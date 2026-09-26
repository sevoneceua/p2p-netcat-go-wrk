package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceInstallRequiresConfigFlag(t *testing.T) {
	command := newServiceInstallCommand()
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.SetArgs(nil)
	if err := command.Execute(); err == nil {
		t.Fatal("expected an error when --config is not set")
	}
}

func TestServiceInstallRejectsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("tunnels: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := newServiceInstallCommand()
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.SetArgs([]string{"--config", path})
	// This must fail on config validation, before ever touching the SCM
	// or systemd, so it's safe to run in CI without elevated privileges.
	if err := command.Execute(); err == nil {
		t.Fatal("expected an error for a config with no tunnels")
	}
}
