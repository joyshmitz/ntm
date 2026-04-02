package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/config"
)

func TestLoadWorktreeConfig_UsesLoadedCLIConfig(t *testing.T) {
	oldCfg, oldCfgFile := cfg, cfgFile
	t.Cleanup(func() {
		cfg = oldCfg
		cfgFile = oldCfgFile
	})

	cfg = &config.Config{ProjectsBase: "/from-loaded-config"}
	cfgFile = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgFile, []byte(`projects_base = "/from-file"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	loaded, err := loadWorktreeConfig()
	if err != nil {
		t.Fatalf("loadWorktreeConfig() error = %v", err)
	}
	if loaded != cfg {
		t.Fatal("loadWorktreeConfig() should reuse already loaded CLI config")
	}
	if loaded.ProjectsBase != "/from-loaded-config" {
		t.Fatalf("ProjectsBase = %q, want /from-loaded-config", loaded.ProjectsBase)
	}
}

func TestLoadWorktreeConfig_UsesCfgFileNotLocalConfigToml(t *testing.T) {
	oldCfg, oldCfgFile := cfg, cfgFile
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	t.Cleanup(func() {
		cfg = oldCfg
		cfgFile = oldCfgFile
		_ = os.Chdir(wd)
	})

	cfg = nil
	tmpDir := t.TempDir()
	cfgFile = filepath.Join(tmpDir, "user-config.toml")
	if err := os.WriteFile(cfgFile, []byte(`projects_base = "/from-cfg-file"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(cfgFile) failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "config.toml"), []byte(`projects_base = "/wrong-local"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(local config.toml) failed: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	loaded, err := loadWorktreeConfig()
	if err != nil {
		t.Fatalf("loadWorktreeConfig() error = %v", err)
	}
	if loaded.ProjectsBase != "/from-cfg-file" {
		t.Fatalf("ProjectsBase = %q, want /from-cfg-file", loaded.ProjectsBase)
	}
}

func TestLoadWorktreeConfig_InvalidCfgFile(t *testing.T) {
	oldCfg, oldCfgFile := cfg, cfgFile
	t.Cleanup(func() {
		cfg = oldCfg
		cfgFile = oldCfgFile
	})

	cfg = nil
	cfgFile = filepath.Join(t.TempDir(), "bad-config.toml")
	if err := os.WriteFile(cfgFile, []byte("not valid toml {{{"), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	_, err := loadWorktreeConfig()
	if err == nil {
		t.Fatal("expected error for invalid cfgFile")
	}
}
