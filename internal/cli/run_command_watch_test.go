package cli

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/santaklouse/go-p2p-netcat/internal/daemon"
	"github.com/santaklouse/go-p2p-netcat/internal/listenerlock"
)

func TestWatchConfigFileReloadsOnChange(t *testing.T) {
	t.Setenv(listenerlock.DirectoryEnvironment, t.TempDir())

	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = echoListener.Close() })
	go func() {
		for {
			conn, acceptErr := echoListener.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	echoAddr := echoListener.Addr().String()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "tunnels.yaml")
	identityPath := filepath.Join(dir, "identity.key")

	initialConfig := "identity: " + identityPath + "\n" +
		"tunnels:\n" +
		"  - name: a\n" +
		"    type: forward\n" +
		"    mode: server\n" +
		"    protocol: tcp\n" +
		"    logical_port: 45501\n" +
		"    target: " + echoAddr + "\n" +
		"    allow_unauthenticated: true\n"
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadTunnelConfigFile(configPath)
	if err != nil {
		t.Fatalf("loadTunnelConfigFile: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	logger := testLogger(t)
	runningDaemon, err := daemon.Run(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("daemon.Run: %v", err)
	}
	defer runningDaemon.Close()

	go watchConfigFile(ctx, configPath, 100*time.Millisecond, runningDaemon, logger)

	// Give the watcher a moment to record the initial mtime before the
	// file is rewritten, so the rewrite below is unambiguously "after".
	time.Sleep(150 * time.Millisecond)

	updatedConfig := initialConfig +
		"  - name: b\n" +
		"    type: forward\n" +
		"    mode: server\n" +
		"    protocol: tcp\n" +
		"    logical_port: 45502\n" +
		"    target: " + echoAddr + "\n" +
		"    allow_unauthenticated: true\n"
	if err := os.WriteFile(configPath, []byte(updatedConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, tunnel := range runningDaemon.Config().Tunnels {
			if tunnel.Name == "b" {
				found = true
				break
			}
		}
		if found {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("tunnel b was not picked up by the config watcher within the deadline")
}

func testLogger(t *testing.T) daemon.Logger {
	return func(format string, args ...any) {
		t.Logf(format, args...)
	}
}
