//go:build windows

package service

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows/svc"
)

func TestStateText(t *testing.T) {
	cases := map[svc.State]string{
		svc.Stopped:         "stopped",
		svc.StartPending:    "start_pending",
		svc.StopPending:     "stop_pending",
		svc.Running:         "running",
		svc.ContinuePending: "continue_pending",
		svc.PausePending:    "pause_pending",
		svc.Paused:          "paused",
	}
	for state, want := range cases {
		if got := stateText(state); got != want {
			t.Errorf("stateText(%v) = %q, want %q", state, got, want)
		}
	}
}

// TestInstallRejectsInvalidConfigBeforeTouchingSCM is the one part of the
// Windows install path safe to exercise without Administrator rights: an
// invalid config must be rejected before Install ever calls
// mgr.Connect(), which is the call that actually needs elevation.
func TestInstallRejectsInvalidConfigBeforeTouchingSCM(t *testing.T) {
	badConfig := filepath.Join(t.TempDir(), "bad.yaml")
	writeFile(t, badConfig, "tunnels: []\n")

	if err := Install(badConfig); err == nil {
		t.Fatal("expected Install to reject an invalid config")
	}
}
