//go:build linux

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSystemdUnit(t *testing.T) {
	unit := buildSystemdUnit("/usr/local/bin/p2p-nc", "/etc/p2p-netcat/tunnels.yaml")
	mustContain := []string{
		"Description=" + Description,
		`ExecStart="/usr/local/bin/p2p-nc" run --config "/etc/p2p-netcat/tunnels.yaml"`,
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	}
	for _, want := range mustContain {
		if !strings.Contains(unit, want) {
			t.Errorf("generated unit missing %q; got:\n%s", want, unit)
		}
	}
}

// TestInstallRejectsInvalidConfigBeforeTouchingSystemd is the one part of
// the Linux install path that's safe to exercise in ordinary CI without
// root: an invalid config must be rejected before Install ever calls
// systemctl or writes the unit file, so this needs neither privileges
// nor a real systemd to actually be running.
func TestInstallRejectsInvalidConfigBeforeTouchingSystemd(t *testing.T) {
	previous := unitPath
	unitPath = filepath.Join(t.TempDir(), Name+".service")
	t.Cleanup(func() { unitPath = previous })

	badConfig := filepath.Join(t.TempDir(), "bad.yaml")
	writeFile(t, badConfig, "tunnels: []\n")

	if err := Install(badConfig); err == nil {
		t.Fatal("expected Install to reject an invalid config")
	}
	if _, err := os.Stat(unitPath); err == nil {
		t.Fatal("Install must not write the unit file when the config is invalid")
	}
}
