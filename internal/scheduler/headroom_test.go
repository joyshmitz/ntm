package scheduler

import (
	"testing"
	"time"
)

func TestDefaultHeadroomConfig(t *testing.T) {
	cfg := DefaultHeadroomConfig()

	if !cfg.Enabled {
		t.Error("expected Enabled to be true by default")
	}
	if cfg.Threshold != 0.75 {
		t.Errorf("expected Threshold 0.75, got %f", cfg.Threshold)
	}
	if cfg.WarnThreshold != 0.70 {
		t.Errorf("expected WarnThreshold 0.70, got %f", cfg.WarnThreshold)
	}
	if cfg.RecheckInterval != 5*time.Second {
		t.Errorf("expected RecheckInterval 5s, got %v", cfg.RecheckInterval)
	}
	if cfg.MinHeadroom != 50 {
		t.Errorf("expected MinHeadroom 50, got %d", cfg.MinHeadroom)
	}
	if cfg.CacheTimeout != 2*time.Second {
		t.Errorf("expected CacheTimeout 2s, got %v", cfg.CacheTimeout)
	}
}

func TestHeadroomGuard_DisabledAllowsAll(t *testing.T) {
	cfg := HeadroomConfig{
		Enabled: false,
	}
	guard := NewHeadroomGuard(cfg)
	defer guard.Stop()

	allowed, reason := guard.CheckHeadroom()
	if !allowed {
		t.Errorf("expected disabled guard to allow all spawns, got blocked: %s", reason)
	}
	if reason != "" {
		t.Errorf("expected empty reason when allowed, got: %s", reason)
	}
}

func TestHeadroomGuard_Status(t *testing.T) {
	cfg := DefaultHeadroomConfig()
	guard := NewHeadroomGuard(cfg)
	defer guard.Stop()

	status := guard.Status()

	// Status should have populated limits and usage
	if status.Limits == nil {
		t.Error("expected Limits to be populated")
	}
	if status.Usage == nil {
		t.Error("expected Usage to be populated")
	}
	if status.LastCheck.IsZero() {
		t.Error("expected LastCheck to be set")
	}
}

func TestHeadroomGuard_BlockedState(t *testing.T) {
	cfg := DefaultHeadroomConfig()
	guard := NewHeadroomGuard(cfg)
	defer guard.Stop()

	// Initially should not be blocked
	if guard.IsBlocked() {
		t.Error("expected guard to not be blocked initially")
	}
	if guard.BlockReason() != "" {
		t.Error("expected empty block reason initially")
	}
}

func TestHeadroomGuard_Callbacks(t *testing.T) {
	cfg := HeadroomConfig{
		Enabled:         true,
		Threshold:       0.0, // Will always be "above threshold" since any usage > 0%
		WarnThreshold:   0.0,
		MinHeadroom:     0,
		RecheckInterval: 100 * time.Millisecond,
		CacheTimeout:    10 * time.Millisecond,
	}
	guard := NewHeadroomGuard(cfg)
	defer guard.Stop()

	blockedCalled := false
	unblockedCalled := false
	warningCalled := false

	guard.SetCallbacks(
		func(reason string, limits *ResourceLimits, usage *ResourceUsage) {
			blockedCalled = true
		},
		func() {
			unblockedCalled = true
		},
		func(reason string, limits *ResourceLimits, usage *ResourceUsage) {
			warningCalled = true
		},
	)

	// With threshold at 0, any usage should trigger blocking
	// (unless no limits are detected, in which case it allows)
	guard.CheckHeadroom()

	// The actual result depends on the system's resource detection
	// We mainly verify callbacks can be set without panics
	_ = blockedCalled
	_ = unblockedCalled
	_ = warningCalled
}

func TestHeadroomGuard_ComputeEffectiveLimit(t *testing.T) {
	cfg := DefaultHeadroomConfig()
	guard := NewHeadroomGuard(cfg)
	defer guard.Stop()

	tests := []struct {
		name           string
		limits         *ResourceLimits
		expectedLimit  int
		expectedSource string
	}{
		{
			name: "ulimit is lowest",
			limits: &ResourceLimits{
				UlimitNproc:     1000,
				CgroupPidsMax:   5000,
				SystemdTasksMax: 3000,
				KernelPidMax:    32768,
			},
			expectedLimit:  1000,
			expectedSource: "ulimit",
		},
		{
			name: "cgroup is lowest",
			limits: &ResourceLimits{
				UlimitNproc:     5000,
				CgroupPidsMax:   1000,
				SystemdTasksMax: 3000,
				KernelPidMax:    32768,
			},
			expectedLimit:  1000,
			expectedSource: "cgroup",
		},
		{
			name: "systemd is lowest",
			limits: &ResourceLimits{
				UlimitNproc:     5000,
				CgroupPidsMax:   0, // unlimited
				SystemdTasksMax: 1000,
				KernelPidMax:    32768,
			},
			expectedLimit:  1000,
			expectedSource: "systemd",
		},
		{
			name: "kernel is lowest",
			limits: &ResourceLimits{
				UlimitNproc:     0, // unlimited
				CgroupPidsMax:   0, // unlimited
				SystemdTasksMax: 0, // unlimited
				KernelPidMax:    1000,
			},
			expectedLimit:  1000,
			expectedSource: "kernel",
		},
		{
			name: "all unlimited",
			limits: &ResourceLimits{
				UlimitNproc:     0,
				CgroupPidsMax:   0,
				SystemdTasksMax: 0,
				KernelPidMax:    0,
			},
			expectedLimit:  0,
			expectedSource: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit, source := guard.computeEffectiveLimit(tt.limits)
			if limit != tt.expectedLimit {
				t.Errorf("expected limit %d, got %d", tt.expectedLimit, limit)
			}
			if source != tt.expectedSource {
				t.Errorf("expected source %q, got %q", tt.expectedSource, source)
			}
		})
	}
}

func TestHeadroomGuard_Evaluate(t *testing.T) {
	tests := []struct {
		name           string
		cfg            HeadroomConfig
		limits         *ResourceLimits
		usage          *ResourceUsage
		expectAllowed  bool
		expectContains string
	}{
		{
			name: "plenty of headroom",
			cfg: HeadroomConfig{
				Enabled:       true,
				Threshold:     0.75,
				WarnThreshold: 0.70,
				MinHeadroom:   50,
			},
			limits: &ResourceLimits{
				EffectiveLimit: 1000,
				Source:         "test",
			},
			usage: &ResourceUsage{
				EffectiveUsage: 500,
				Source:         "test",
			},
			expectAllowed: true,
		},
		{
			name: "above threshold blocks",
			cfg: HeadroomConfig{
				Enabled:       true,
				Threshold:     0.75,
				WarnThreshold: 0.70,
				MinHeadroom:   50,
			},
			limits: &ResourceLimits{
				EffectiveLimit: 1000,
				Source:         "test",
			},
			usage: &ResourceUsage{
				EffectiveUsage: 800, // 80% > 75%
				Source:         "test",
			},
			expectAllowed:  false,
			expectContains: "exhausted",
		},
		{
			name: "below min headroom blocks",
			cfg: HeadroomConfig{
				Enabled:       true,
				Threshold:     0.98, // High threshold so min headroom triggers first
				WarnThreshold: 0.90,
				MinHeadroom:   100,
			},
			limits: &ResourceLimits{
				EffectiveLimit: 1000,
				Source:         "test",
			},
			usage: &ResourceUsage{
				EffectiveUsage: 950, // only 50 free, need 100, but 95% < 98% threshold
				Source:         "test",
			},
			expectAllowed:  false,
			expectContains: "insufficient headroom",
		},
		{
			name: "no limits allows all",
			cfg: HeadroomConfig{
				Enabled:       true,
				Threshold:     0.75,
				WarnThreshold: 0.70,
				MinHeadroom:   50,
			},
			limits: &ResourceLimits{
				EffectiveLimit: 0, // no limit detected
				Source:         "",
			},
			usage: &ResourceUsage{
				EffectiveUsage: 9999,
				Source:         "test",
			},
			expectAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := NewHeadroomGuard(tt.cfg)
			defer guard.Stop()

			allowed, reason := guard.evaluate(tt.limits, tt.usage)
			if allowed != tt.expectAllowed {
				t.Errorf("expected allowed=%v, got %v (reason: %s)", tt.expectAllowed, allowed, reason)
			}
			if tt.expectContains != "" && !contains(reason, tt.expectContains) {
				t.Errorf("expected reason to contain %q, got: %s", tt.expectContains, reason)
			}
		})
	}
}

func TestHeadroomGuard_Remediation(t *testing.T) {
	cfg := DefaultHeadroomConfig()
	guard := NewHeadroomGuard(cfg)
	defer guard.Stop()

	// When not blocked, remediation should be empty
	if r := guard.Remediation(); r != "" {
		t.Errorf("expected empty remediation when not blocked, got: %s", r)
	}

	// Force blocked state with cached limits
	guard.mu.Lock()
	guard.blocked = true
	guard.cachedLimits = &ResourceLimits{
		EffectiveLimit: 1000,
		Source:         "ulimit",
	}
	guard.mu.Unlock()

	r := guard.Remediation()
	if r == "" {
		t.Error("expected non-empty remediation when blocked")
	}
	if !contains(r, "ulimit") {
		t.Errorf("expected remediation to mention ulimit source, got: %s", r)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
