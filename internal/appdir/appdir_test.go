package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirUsesOverride(t *testing.T) {
	base := t.TempDir()
	override := filepath.Join(base, "custom-oobe")
	t.Setenv(OverrideEnvironment, override)

	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if dir != override {
		t.Fatalf("Dir() = %q, want %q", dir, override)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
}

func TestDirIsNextToExecutable(t *testing.T) {
	t.Setenv(OverrideEnvironment, "")
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	exePath, err := os.Executable()
	if err != nil {
		t.Skip("os.Executable unavailable in this environment")
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		resolved = exePath
	}
	want := filepath.Join(filepath.Dir(resolved), DirName)
	if dir != want {
		t.Fatalf("Dir() = %q, want %q", dir, want)
	}
}

func TestDirIsIdempotent(t *testing.T) {
	base := t.TempDir()
	t.Setenv(OverrideEnvironment, filepath.Join(base, "oobe"))
	first, err := Dir()
	if err != nil {
		t.Fatalf("Dir (first): %v", err)
	}
	second, err := Dir()
	if err != nil {
		t.Fatalf("Dir (second): %v", err)
	}
	if first != second {
		t.Fatalf("Dir() not stable across calls: %q vs %q", first, second)
	}
}
