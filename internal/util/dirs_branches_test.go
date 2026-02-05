package util

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// NTMDir — 0% → 100%
// ---------------------------------------------------------------------------

func TestNTMDir(t *testing.T) {
	t.Parallel()

	dir, err := NTMDir()
	if err != nil {
		t.Fatalf("NTMDir() error: %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".ntm")
	if dir != want {
		t.Errorf("NTMDir() = %q, want %q", dir, want)
	}
}

// ---------------------------------------------------------------------------
// EnsureDir — 0% → 100%
// ---------------------------------------------------------------------------

func TestEnsureDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "a", "b", "c")

	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir(%q) error: %v", dir, err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat after EnsureDir: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected directory, got file")
	}

	// Idempotent call
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir (second call) error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FindGitRoot — 0% → 100%
// ---------------------------------------------------------------------------

func TestFindGitRoot(t *testing.T) {
	t.Parallel()

	// Create a temp git repo
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "a", "b")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize git in tmp
	cmd := exec.Command("git", "init", tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git init failed: %v: %s", err, out)
	}

	root, err := FindGitRoot(sub)
	if err != nil {
		t.Fatalf("FindGitRoot(%q) error: %v", sub, err)
	}
	// Resolve symlinks for comparison (t.TempDir may use /tmp which symlinks)
	wantReal, _ := filepath.EvalSymlinks(tmp)
	gotReal, _ := filepath.EvalSymlinks(root)
	if gotReal != wantReal {
		t.Errorf("FindGitRoot(%q) = %q, want %q", sub, gotReal, wantReal)
	}
}

func TestFindGitRoot_NotARepo(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	_, err := FindGitRoot(tmp)
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}
