package daemon

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/santaklouse/go-p2p-netcat/internal/appdir"
	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
)

func TestLoadTunnelTokenResolvesRelativePathUnderAppDir(t *testing.T) {
	oobe := t.TempDir()
	t.Setenv(appdir.OverrideEnvironment, oobe)

	_, err := loadTunnelToken(tunnelconfig.Tunnel{Name: "x", TokenFile: "tokens/missing.token"})
	if err == nil {
		t.Fatal("expected an error for a missing token file")
	}
	wantPath := filepath.Join(oobe, "tokens", "missing.token")
	if !strings.Contains(err.Error(), wantPath) {
		t.Fatalf("error %q does not reference the appdir-resolved path %q", err, wantPath)
	}
}
