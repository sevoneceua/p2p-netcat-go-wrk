package daemon

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/santaklouse/go-p2p-netcat/internal/secretfile"
	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
)

// loadTunnelToken loads and validates the pairing token for one tunnel, if
// it has one configured. A tunnel with AllowUnauthenticated and no
// TokenFile returns (nil, nil); tunnelconfig.Validate already guarantees
// every other server tunnel has exactly one of the two set.
func loadTunnelToken(t tunnelconfig.Tunnel) (*pairing.Token, error) {
	if t.TokenFile == "" {
		return nil, nil
	}
	path, err := expandHome(t.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("tunnel %q: token_file: %w", t.Name, err)
	}
	raw, err := readTokenFile(path)
	if err != nil {
		return nil, fmt.Errorf("tunnel %q: %w", t.Name, err)
	}
	token, err := pairing.Decode(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("tunnel %q: decode pairing token: %w", t.Name, err)
	}
	if err := token.Validate(time.Now()); err != nil {
		return nil, fmt.Errorf("tunnel %q: %w", t.Name, err)
	}
	return token, nil
}

// readTokenFile mirrors internal/cli's readPairingTokenFile (unexported
// there, so reimplemented here rather than imported): reject anything
// that isn't a regular, privately-permissioned file, and cap the read at
// the largest token pairing.Decode could possibly accept.
func readTokenFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open pairing token file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect pairing token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("pairing token file must be a regular file")
	}
	if err := secretfile.CheckPermissions(file, info); err != nil {
		return "", fmt.Errorf("pairing token file is not private: %w", err)
	}
	maximum := len(pairing.TokenPrefix) + base64.RawURLEncoding.EncodedLen(pairing.MaxTokenSize) + 2
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum+1)))
	if err != nil {
		return "", fmt.Errorf("read pairing token file: %w", err)
	}
	if len(data) > maximum {
		return "", fmt.Errorf("pairing token file exceeds %d bytes", maximum)
	}
	return string(data), nil
}

// expandHome expands a leading "~" or "~/..." to the current user's home
// directory. YAML config files have no shell to do this for the user, so
// unlike CLI flags (which the shell already expands), tunnelconfig paths
// need it done explicitly. Anything not starting with "~" passes through
// unchanged; "~otheruser/..." is left alone (rare enough, and resolving
// another account's home directory portably is its own can of worms).
func expandHome(path string) (string, error) {
	if path == "" || path == "~" {
		if path == "~" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			return home, nil
		}
		return path, nil
	}
	if !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, path[2:]), nil
}
