package identity

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/santaklouse/go-p2p-netcat/internal/appdir"
	"github.com/santaklouse/go-p2p-netcat/internal/secretfile"
)

const maxIdentityFileSize = 64 * 1024

// DefaultPath returns <oobe>/identity.key — see internal/appdir for what
// "oobe" means and why identity, pairing tokens, and tunnel configs all
// live in one predictable folder next to the executable rather than
// scattered across OS-specific per-user directories. Falls back to the
// old $XDG_CONFIG_HOME/p2p-netcat (or ~/.config/p2p-netcat) location, and
// finally to ./.p2p-netcat, only if appdir.Dir can't be resolved or
// created at all (read-only install location, exotic sandboxing) — this
// keeps a node able to start somewhere rather than not at all, at the
// cost of the file landing somewhere less discoverable in that one case.
func DefaultPath() string {
	if dir, err := appdir.Dir(); err == nil {
		return filepath.Join(dir, "identity.key")
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".", ".p2p-netcat", "identity.key")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "p2p-netcat", "identity.key")
}

func LoadOrCreate(path string) (crypto.PrivKey, error) {
	if path == "" {
		key, _, err := crypto.GenerateEd25519Key(rand.Reader)
		return key, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absolute)
	if err == nil {
		info, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("inspect private key %s: %w", absolute, statErr)
		}
		if !info.Mode().IsRegular() {
			_ = file.Close()
			return nil, fmt.Errorf("private key %s must be a regular file", absolute)
		}
		if permissionErr := secretfile.CheckPermissions(file, info); permissionErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("private key %s is not private: %w", absolute, permissionErr)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxIdentityFileSize+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read private key %s: %w", absolute, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close private key %s: %w", absolute, closeErr)
		}
		if len(data) > maxIdentityFileSize {
			return nil, fmt.Errorf("private key %s exceeds %d bytes", absolute, maxIdentityFileSize)
		}
		key, decodeErr := crypto.UnmarshalPrivateKey(data)
		if decodeErr != nil {
			return nil, fmt.Errorf("read private key %s: %w", absolute, decodeErr)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read private key %s: %w", absolute, err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return nil, fmt.Errorf("create identity directory: %w", err)
	}
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("create Ed25519 identity: %w", err)
	}
	encoded, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, err
	}
	file, err = os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return LoadOrCreate(absolute)
		}
		return nil, fmt.Errorf("create identity %s: %w", absolute, err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		_ = os.Remove(absolute)
		return nil, fmt.Errorf("write identity %s: %w", absolute, err)
	}
	if err := secretfile.Protect(file); err != nil {
		_ = file.Close()
		_ = os.Remove(absolute)
		return nil, fmt.Errorf("protect identity %s: %w", absolute, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(absolute)
		return nil, err
	}
	return key, nil
}
