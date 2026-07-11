package tools

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNewSLBAdapter(t *testing.T) {
	adapter := NewSLBAdapter()
	if adapter == nil {
		t.Fatal("NewSLBAdapter returned nil")
	}
	if adapter.Name() != ToolSLB {
		t.Errorf("Expected name %s, got %s", ToolSLB, adapter.Name())
	}
	if adapter.BinaryName() != "slb" {
		t.Errorf("Expected binary name 'slb', got %s", adapter.BinaryName())
	}
}

func TestSLBAdapterImplementsInterface(t *testing.T) {
	var _ Adapter = (*SLBAdapter)(nil)
}

// TestSLBHealthWhenInstalled is an end-to-end regression for issue #202: slb
// exposes version via the `version` SUBCOMMAND, not a `--version` flag, so the
// old probe made a healthy slb report "health check failed". This live-host
// probe is opt-in so a developer's PATH cannot affect the default unit suite.
func TestSLBHealthWhenInstalled(t *testing.T) {
	if os.Getenv("NTM_LIVE_TOOL_TESTS") != "1" {
		t.Skip("set NTM_LIVE_TOOL_TESTS=1 to test the host-installed slb binary")
	}

	adapter := NewSLBAdapter()
	if _, installed := adapter.Detect(); !installed {
		t.Skip("slb not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := adapter.Version(ctx); err != nil {
		t.Fatalf("Version() failed for installed slb (regression: --version vs version subcommand): %v", err)
	}

	health, err := adapter.Health(ctx)
	if err != nil {
		t.Fatalf("Health() returned error: %v", err)
	}
	if !health.Healthy {
		t.Fatalf("installed slb reported unhealthy: %s", health.Message)
	}
}
