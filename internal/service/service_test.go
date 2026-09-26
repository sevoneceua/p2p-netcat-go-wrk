package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigRejectsEmptyPath(t *testing.T) {
	if _, err := loadConfig(""); err == nil {
		t.Fatal("expected an error for an empty config path")
	}
}

func TestLoadConfigRejectsMissingFile(t *testing.T) {
	if _, err := loadConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}

func TestLoadConfigRejectsInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	writeFile(t, path, "tunnels: []\n") // valid YAML, invalid config: no tunnels
	if _, err := loadConfig(path); err == nil {
		t.Fatal("expected an error for a config with no tunnels")
	}
}

func TestLoadConfigAcceptsValidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "good.yaml")
	writeFile(t, path, `
tunnels:
  - name: a
    type: forward
    mode: server
    protocol: tcp
    logical_port: 1
    target: 127.0.0.1:80
    allow_unauthenticated: true
`)
	if _, err := loadConfig(path); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
