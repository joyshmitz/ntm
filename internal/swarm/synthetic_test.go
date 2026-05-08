package swarm

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSyntheticHarnessShortScenario(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	harness := NewSyntheticHarness(logger)

	result, err := harness.Run(context.Background(), SyntheticScenario{
		TestRunID:             "run-123",
		Name:                  "short smoke",
		SessionName:           "synthetic_short",
		PaneCount:             4,
		CommandCount:          3,
		OutputLinesPerCommand: 2,
		Patterns: []SyntheticOutputPattern{
			SyntheticPatternIdle,
			SyntheticPatternWorking,
			SyntheticPatternRateLimit,
			SyntheticPatternCompleted,
		},
		StartTime: time.Unix(1_700_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if result.Metrics.TestRunID != "run-123" {
		t.Fatalf("TestRunID = %q, want run-123", result.Metrics.TestRunID)
	}
	if result.Metrics.PaneCount != 4 {
		t.Fatalf("PaneCount = %d, want 4", result.Metrics.PaneCount)
	}
	if result.Metrics.CommandCount != 3 {
		t.Fatalf("CommandCount = %d, want 3", result.Metrics.CommandCount)
	}
	if result.Metrics.EventCount != 12 {
		t.Fatalf("EventCount = %d, want 12", result.Metrics.EventCount)
	}
	if len(result.Panes) != 4 {
		t.Fatalf("len(Panes) = %d, want 4", len(result.Panes))
	}
	if len(result.Events) != 12 {
		t.Fatalf("len(Events) = %d, want 12", len(result.Events))
	}

	wantStates := []SyntheticAgentState{
		SyntheticStateIdle,
		SyntheticStateWorking,
		SyntheticStateRateLimit,
		SyntheticStateCompleted,
	}
	for i, want := range wantStates {
		if result.Panes[i].State != want {
			t.Fatalf("pane %d state = %q, want %q", i+1, result.Panes[i].State, want)
		}
		if result.Panes[i].CommandCount != 3 {
			t.Fatalf("pane %d command count = %d, want 3", i+1, result.Panes[i].CommandCount)
		}
		if len(result.Panes[i].OutputTail) == 0 {
			t.Fatalf("pane %d output tail is empty", i+1)
		}
	}

	if result.Metrics.LatencyP50Micros <= 0 {
		t.Fatalf("LatencyP50Micros = %d, want positive", result.Metrics.LatencyP50Micros)
	}
	if result.Metrics.LatencyP95Micros < result.Metrics.LatencyP50Micros {
		t.Fatalf("LatencyP95Micros = %d before p50 %d", result.Metrics.LatencyP95Micros, result.Metrics.LatencyP50Micros)
	}
	if result.Metrics.MemoryGrowthBytes < 0 {
		t.Fatalf("MemoryGrowthBytes = %d, want non-negative", result.Metrics.MemoryGrowthBytes)
	}
	if result.Metrics.GoroutinesLeaked < 0 {
		t.Fatalf("GoroutinesLeaked = %d, want non-negative", result.Metrics.GoroutinesLeaked)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("result did not marshal to valid JSON")
	}

	logText := logs.String()
	for _, fragment := range []string{"synthetic_swarm_start", "synthetic_swarm_complete", "test_run_id=run-123", "pane_count=4", "event_count=12"} {
		if !strings.Contains(logText, fragment) {
			t.Fatalf("logs missing %q:\n%s", fragment, logText)
		}
	}
}

func TestSyntheticHarnessRejectsInvalidScenario(t *testing.T) {
	harness := NewSyntheticHarness(nil)

	tests := []struct {
		name     string
		scenario SyntheticScenario
		wantErr  string
	}{
		{
			name:     "negative panes",
			scenario: SyntheticScenario{PaneCount: -1, CommandCount: 1},
			wantErr:  "pane count must be positive",
		},
		{
			name:     "negative commands",
			scenario: SyntheticScenario{PaneCount: 1, CommandCount: -1},
			wantErr:  "command count must be positive",
		},
		{
			name:     "unknown pattern",
			scenario: SyntheticScenario{PaneCount: 1, CommandCount: 1, Patterns: []SyntheticOutputPattern{"mystery"}},
			wantErr:  "unknown synthetic output pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := harness.Run(context.Background(), tt.scenario)
			if err == nil {
				t.Fatal("Run returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSyntheticHarnessLargeOptInWritesArtifact(t *testing.T) {
	if os.Getenv("NTM_SYNTHETIC_SWARM_LOAD") == "" {
		t.Skip("set NTM_SYNTHETIC_SWARM_LOAD=1 to run the 100-pane synthetic artifact test")
	}

	harness := NewSyntheticHarness(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	result, err := harness.Run(context.Background(), SyntheticScenario{
		TestRunID:             "load-100",
		Name:                  "load artifact",
		SessionName:           "synthetic_load",
		PaneCount:             100,
		CommandCount:          5,
		OutputLinesPerCommand: 1,
		StartTime:             time.Unix(1_700_000_100, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	path := filepath.Join(t.TempDir(), "synthetic_swarm_artifact.json")
	if err := result.WriteArtifact(path); err != nil {
		t.Fatalf("WriteArtifact returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var decoded SyntheticRunResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	if decoded.Metrics.PaneCount != 100 {
		t.Fatalf("artifact pane count = %d, want 100", decoded.Metrics.PaneCount)
	}
	if decoded.Metrics.EventCount != 500 {
		t.Fatalf("artifact event count = %d, want 500", decoded.Metrics.EventCount)
	}
}

func TestNonNegativeMemoryGrowth(t *testing.T) {
	t.Parallel()

	before := runtime.MemStats{Alloc: 4096}
	afterLower := runtime.MemStats{Alloc: 1024}
	if got := nonNegativeMemoryGrowth(before, afterLower); got != 0 {
		t.Fatalf("nonNegativeMemoryGrowth(lower alloc) = %d, want 0", got)
	}

	afterHigher := runtime.MemStats{Alloc: 6144}
	if got := nonNegativeMemoryGrowth(before, afterHigher); got != 2048 {
		t.Fatalf("nonNegativeMemoryGrowth(higher alloc) = %d, want 2048", got)
	}
}

func TestNonNegativeIntDelta(t *testing.T) {
	t.Parallel()

	if got := nonNegativeIntDelta(10, 7); got != 0 {
		t.Fatalf("nonNegativeIntDelta(10,7) = %d, want 0", got)
	}
	if got := nonNegativeIntDelta(10, 10); got != 0 {
		t.Fatalf("nonNegativeIntDelta(10,10) = %d, want 0", got)
	}
	if got := nonNegativeIntDelta(10, 15); got != 5 {
		t.Fatalf("nonNegativeIntDelta(10,15) = %d, want 5", got)
	}
}
