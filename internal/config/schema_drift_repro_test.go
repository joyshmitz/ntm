package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRateLimitAutoRotateAlias — repro from ntm#113 Gap 3. The TOML key
// `[resilience.rate_limit] auto_rotate = true` must (a) parse cleanly through
// the strict loader instead of being rejected as `unknown field`, and (b) set
// `Rotation.AutoTrigger = true` so the runtime monitor in
// internal/resilience/monitor.go (which keys on cfg.Rotation.AutoTrigger) acts
// on it. The alias does not override an explicit
// `[rotation] auto_trigger = false` to true unless the rate-limit alias is
// itself true; setting both is an OR.
func TestRateLimitAutoRotateAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`projects_base = "/tmp"

[resilience.rate_limit]
detect = true
notify = true
auto_rotate = true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() rejected resilience.rate_limit.auto_rotate: %v", err)
	}

	if !cfg.Resilience.RateLimit.AutoRotate {
		t.Errorf("RateLimit.AutoRotate = false, want true (TOML set it true)")
	}
	if !cfg.Rotation.AutoTrigger {
		t.Errorf("Rotation.AutoTrigger = false, want true (alias should fold into canonical knob)")
	}
}

// TestRateLimitAutoRotateAliasIsAdditive — the alias must NOT clobber an
// explicit `[rotation] auto_trigger = true` to false when the alias is unset.
// If the user has only `[rotation]` configured, the loader must leave it
// alone.
func TestRateLimitAutoRotateAliasIsAdditive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`projects_base = "/tmp"

[rotation]
auto_trigger = true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Resilience.RateLimit.AutoRotate {
		t.Errorf("RateLimit.AutoRotate = true unexpectedly; alias should default false")
	}
	if !cfg.Rotation.AutoTrigger {
		t.Errorf("Rotation.AutoTrigger = false, want true (TOML set it directly)")
	}
}
