package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScrub_CommandJSONOutputIsRedacted(t *testing.T) {
	resetFlags()

	// Avoid reading any user config by isolating XDG config home.
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	artifactPath := filepath.Join(tmpDir, "artifact.log")
	if err := os.WriteFile(artifactPath, []byte("prefix password=hunter2hunter2 suffix\n"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Ensure this test is deterministic regardless of prior tests.
	oldCfg := cfg
	cfg = nil
	t.Cleanup(func() { cfg = oldCfg })

	out, err := captureStdout(t, func() error {
		rootCmd.SetArgs([]string{"scrub", "--path", artifactPath, "--format", "json"})
		return rootCmd.Execute()
	})
	if err != nil {
		t.Fatalf("scrub command failed: %v\noutput:\n%s", err, out)
	}

	var res scrubResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("failed to parse scrub JSON: %v\noutput:\n%s", err, out)
	}

	if len(res.Roots) != 1 {
		t.Fatalf("expected 1 root, got %d (%v)", len(res.Roots), res.Roots)
	}
	if res.Roots[0] != artifactPath {
		t.Fatalf("expected root %q, got %q", artifactPath, res.Roots[0])
	}
	if res.FilesScanned != 1 {
		t.Fatalf("expected FilesScanned=1, got %d", res.FilesScanned)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d (%v)", len(res.Findings), res.Findings)
	}

	f := res.Findings[0]
	if f.Path != artifactPath {
		t.Fatalf("expected finding path %q, got %q", artifactPath, f.Path)
	}
	if f.Category == "" {
		t.Fatalf("expected non-empty category")
	}
	if f.Start <= 0 || f.End <= f.Start {
		t.Fatalf("expected valid start/end offsets, got start=%d end=%d", f.Start, f.End)
	}
	if f.Line == 0 || f.Column == 0 {
		t.Fatalf("expected line/column to be populated, got line=%d col=%d", f.Line, f.Column)
	}
	if strings.Contains(f.Preview, "hunter2hunter2") {
		t.Fatalf("expected preview to be redacted, got %q", f.Preview)
	}
	if !strings.Contains(f.Preview, "[REDACTED:PASSWORD:") {
		t.Fatalf("expected password redaction placeholder, got %q", f.Preview)
	}
}

func TestScrub_CommandSinceSkipsOldFiles(t *testing.T) {
	resetFlags()

	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	artifactPath := filepath.Join(tmpDir, "old.log")
	if err := os.WriteFile(artifactPath, []byte("password=hunter2hunter2\n"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(artifactPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}

	oldCfg := cfg
	cfg = nil
	t.Cleanup(func() { cfg = oldCfg })

	out, err := captureStdout(t, func() error {
		rootCmd.SetArgs([]string{"scrub", "--path", artifactPath, "--since", "1h", "--format", "json"})
		return rootCmd.Execute()
	})
	if err != nil {
		t.Fatalf("scrub command failed: %v\noutput:\n%s", err, out)
	}

	var res scrubResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("failed to parse scrub JSON: %v\noutput:\n%s", err, out)
	}
	if res.FilesScanned != 0 {
		t.Fatalf("expected FilesScanned=0, got %d", res.FilesScanned)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(res.Findings))
	}
}

func TestResolveScrubRootsUsesSelectedConfigDir(t *testing.T) {
	oldCfgFile := cfgFile
	cfgFile = ""
	t.Cleanup(func() {
		cfgFile = oldCfgFile
	})

	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "xdg"))

	customPath := filepath.Join(base, "custom", "ntm.toml")
	customDir := filepath.Dir(customPath)
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(customDir) failed: %v", err)
	}
	cfgFile = customPath

	roots, err := resolveScrubRoots(nil)
	if err != nil {
		t.Fatalf("resolveScrubRoots(nil) failed: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("expected 1 scrub root, got %d (%v)", len(roots), roots)
	}
	if roots[0] != customDir {
		t.Fatalf("scrub root = %q, want %q", roots[0], customDir)
	}
}
