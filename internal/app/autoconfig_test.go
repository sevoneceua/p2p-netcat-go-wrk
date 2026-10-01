package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTryAutoConfigIgnoresNonEmptyArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_, handled := tryAutoConfig(context.Background(), []string{"-l", "8080"}, &stdout, &stderr)
	if handled {
		t.Fatal("tryAutoConfig must not engage when arguments were given")
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("tunnels: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindAdjacentConfigSameNameWins(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "p2p-nc.exe")
	touch(t, filepath.Join(dir, "p2p-nc.yaml"))

	got, found := findAdjacentConfig(exePath)
	if !found {
		t.Fatal("expected to find the same-named config")
	}
	if want := filepath.Join(dir, "p2p-nc.yaml"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindAdjacentConfigFallsBackToP2PNCName(t *testing.T) {
	dir := t.TempDir()
	// Running as a differently-named binary (e.g. the "pnc" alias) with
	// no "pnc.*" config present, but a "p2p-nc.*" one is.
	exePath := filepath.Join(dir, "pnc.exe")
	touch(t, filepath.Join(dir, "p2p-nc.yaml"))

	got, found := findAdjacentConfig(exePath)
	if !found {
		t.Fatal("expected to find the p2p-nc.* fallback config")
	}
	if want := filepath.Join(dir, "p2p-nc.yaml"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindAdjacentConfigSameNameTakesPriorityOverFallback(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "pnc.exe")
	touch(t, filepath.Join(dir, "pnc.yaml"))    // same-named
	touch(t, filepath.Join(dir, "p2p-nc.yaml")) // fallback -- must NOT win

	got, found := findAdjacentConfig(exePath)
	if !found {
		t.Fatal("expected to find a config")
	}
	if want := filepath.Join(dir, "pnc.yaml"); got != want {
		t.Fatalf("got %q, want %q (same-named file must win over the p2p-nc.* fallback)", got, want)
	}
}

func TestFindAdjacentConfigExtensionOrder(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "p2p-nc.exe")
	// .ini and .mui both present; .cfg/.ini/.yaml/.mui order means .ini
	// must win since it comes before .mui and no .cfg/.yaml exists.
	touch(t, filepath.Join(dir, "p2p-nc.ini"))
	touch(t, filepath.Join(dir, "p2p-nc.mui"))

	got, found := findAdjacentConfig(exePath)
	if !found {
		t.Fatal("expected to find a config")
	}
	if want := filepath.Join(dir, "p2p-nc.ini"); got != want {
		t.Fatalf("got %q, want %q (.ini must be tried before .mui)", got, want)
	}
}

func TestFindAdjacentConfigNoneFound(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "p2p-nc.exe")
	if _, found := findAdjacentConfig(exePath); found {
		t.Fatal("expected no config to be found in an empty directory")
	}
}

func TestFindAdjacentConfigP2PNCNameDoesNotDoubleCheck(t *testing.T) {
	// When the executable is itself named "p2p-nc", there is no distinct
	// fallback set to additionally check -- this just confirms that case
	// still finds its own config normally rather than erroring or looping.
	dir := t.TempDir()
	exePath := filepath.Join(dir, "p2p-nc.exe")
	touch(t, filepath.Join(dir, "p2p-nc.cfg"))

	got, found := findAdjacentConfig(exePath)
	if !found || got != filepath.Join(dir, "p2p-nc.cfg") {
		t.Fatalf("got (%q, %v), want (%q, true)", got, found, filepath.Join(dir, "p2p-nc.cfg"))
	}
}
