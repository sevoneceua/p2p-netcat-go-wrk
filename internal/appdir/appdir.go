// Package appdir resolves where p2p-netcat keeps its own files (identity
// key, pairing tokens, tunnel configs, listener-lock state, and anything
// else the program needs later) — a folder named "oobe" ("Overlay
// Outbound Bridging Endpoints") sitting next to the running executable,
// rather than scattered across the OS's per-user config/cache
// directories (~/.config on Linux/macOS, a ~/.config fallback on Windows
// that isn't even the native convention there — see the identity package's
// old DefaultPath for what this replaces).
//
// The point is portability and findability: one predictable folder next
// to the program, so nothing has to be hunted down by OS convention, and
// so the same folder still makes sense once the program runs as a
// service under a system account with its own, less discoverable profile
// (Epic 2).
package appdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirName is the folder's name, created next to the executable.
const DirName = "oobe"

// OverrideEnvironment lets tests (and anyone deliberately running an
// isolated instance) point Dir somewhere else without touching the real
// installation folder.
const OverrideEnvironment = "P2P_NETCAT_APPDIR"

// Dir returns the oobe folder next to the running executable, creating it
// (mode 0700 — the identity key and pairing tokens that end up in it are
// secrets) if it doesn't exist yet. If the executable's own path can't be
// resolved (embedding scenarios, some minimal containers), Dir falls back
// to an "oobe" folder under the current working directory rather than
// failing outright, since every caller today treats a Dir error as fatal
// only for the specific file it was trying to place — see
// identity.DefaultPath's own further fallback.
func Dir() (string, error) {
	if override := os.Getenv(OverrideEnvironment); override != "" {
		return ensureDir(override)
	}
	exePath, err := os.Executable()
	if err != nil {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return "", fmt.Errorf("resolve executable path: %w", err)
		}
		return ensureDir(filepath.Join(cwd, DirName))
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	return ensureDir(filepath.Join(filepath.Dir(exePath), DirName))
}

// ResolvePath resolves a user-supplied path the way every p2p-netcat
// config field should: an absolute path passes through unchanged; a
// leading "~" or "~/..." expands to the user's home directory (YAML has
// no shell to do that expansion itself); anything else -- a bare
// relative path -- resolves against the oobe folder rather than the
// process's current working directory, because CWD is not something a
// config file (or a process started as a service, per Epic 2) can rely
// on staying meaningful. An empty path returns "", so callers can still
// tell "not configured" apart from "resolves to something".
func ResolvePath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, path), nil
}

func ensureDir(dir string) (string, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", absolute, err)
	}
	return absolute, nil
}
