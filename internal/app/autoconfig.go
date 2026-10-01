package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/santaklouse/go-p2p-netcat/internal/daemon"
	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
)

// autoConfigExtensions is checked in this exact order for every
// candidate base name: same-named file first, p2p-nc.* fallback second,
// each tried .cfg, .ini, .yaml, .mui in turn.
var autoConfigExtensions = []string{".cfg", ".ini", ".yaml", ".mui"}

// tryAutoConfig is the whole reason "double-click p2p-nc.exe in Explorer"
// can do something useful instead of cobra's stock mousetrap splash
// (see internal/cli.NewRoot, which still customizes that splash's text
// for the case this function does NOT handle -- no config file found at
// all). Only engages when invoked with zero arguments: with any argument
// present the person is clearly already driving the CLI deliberately, so
// normal cobra dispatch proceeds completely unchanged.
//
// Lookup order, first match wins:
//  1. <dir of the running executable>/<executable base name><ext>
//  2. <dir of the running executable>/p2p-nc<ext>   (skipped if (1)'s
//     base name is already "p2p-nc", to avoid checking the same name twice)
//
// for ext in .cfg, .ini, .yaml, .mui, in that order. Whatever file is
// found is parsed as the same tunnelconfig YAML schema regardless of
// its extension -- .cfg/.ini/.mui are accepted purely as alternate file
// names for convenience (renaming a config to blend in, or matching a
// test harness's existing naming convention), not as different formats.
func tryAutoConfig(ctx context.Context, args []string, stdout, stderr io.Writer) (code int, handled bool) {
	if len(args) != 0 {
		return 0, false
	}
	exePath, err := os.Executable()
	if err != nil {
		return 0, false // can't locate "next to the executable" at all; fall through to normal CLI behavior
	}
	configPath, found := findAdjacentConfig(exePath)
	if !found {
		return 0, false
	}

	fmt.Fprintf(stdout, "[p2p-nc] no arguments given; found %s next to the executable, running it as the multi-tunnel daemon\n", configPath)
	cfg, err := loadConfigFile(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "[p2p-nc] error: %s: %v\n", configPath, err)
		pauseIfLaunchedFromExplorer(stdout)
		return 1, true
	}

	logger := func(format string, a ...any) {
		fmt.Fprintf(stdout, "[p2p-ncd] "+format+"\n", a...)
	}
	runningDaemon, err := daemon.Run(ctx, cfg, logger)
	if err != nil {
		fmt.Fprintf(stderr, "[p2p-nc] error: %v\n", err)
		pauseIfLaunchedFromExplorer(stdout)
		return 1, true
	}
	defer runningDaemon.Close()
	fmt.Fprintf(stdout, "[p2p-ncd] PeerId: %s\n", runningDaemon.Node.Host.ID())
	for _, address := range runningDaemon.Node.Addresses() {
		fmt.Fprintf(stdout, "[p2p-ncd] address: %s\n", address)
	}
	fmt.Fprintf(stdout, "[p2p-ncd] %d tunnel(s) running; close this window or Ctrl+C to stop\n", len(cfg.Tunnels))

	<-ctx.Done()
	return 0, true
}

func findAdjacentConfig(exePath string) (string, bool) {
	dir := filepath.Dir(exePath)
	base := strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))

	for _, ext := range autoConfigExtensions {
		candidate := filepath.Join(dir, base+ext)
		if isRegularFile(candidate) {
			return candidate, true
		}
	}
	if base == "p2p-nc" {
		return "", false
	}
	for _, ext := range autoConfigExtensions {
		candidate := filepath.Join(dir, "p2p-nc"+ext)
		if isRegularFile(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func loadConfigFile(path string) (*tunnelconfig.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return tunnelconfig.Parse(data)
}
