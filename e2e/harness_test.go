//go:build !e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestIsBenignTmuxCleanupError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{name: "missing session", stderr: "can't find session: ntm-test", want: true},
		{name: "server absent", stderr: "no server running on /tmp/tmux-1000/default", want: true},
		{name: "socket absent", stderr: "error connecting to /tmp/tmux-1000/default (No such file or directory)", want: true},
		{name: "permission failure", stderr: "error connecting to /tmp/tmux-1000/default (Permission denied)", want: false},
		{name: "connection refused", stderr: "error connecting to /tmp/tmux-1000/default (Connection refused)", want: false},
		{name: "unrelated missing file", stderr: "open /tmp/tmux.conf: no such file or directory", want: false},
		{name: "empty", stderr: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBenignTmuxCleanupError([]byte(tt.stderr)); got != tt.want {
				t.Fatalf("isBenignTmuxCleanupError(%q) = %t, want %t", tt.stderr, got, tt.want)
			}
		})
	}
}

func TestNewScenarioHarnessCreatesExpectedLayout(t *testing.T) {
	base := t.TempDir()
	fixed := time.Date(2026, 3, 21, 2, 45, 6, 123000000, time.UTC)

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "Attention Feed Harness",
		ArtifactRoot: base,
		RunToken:     "smoke",
		Retain:       RetainAlways,
		Clock:        func() time.Time { return fixed },
		FailureState: func() bool { return false },
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}
	t.Cleanup(h.Close)

	wantRoot := filepath.Join(base, "attention-feed-harness", "20260321T024506.123Z-smoke")
	if h.Root() != wantRoot {
		t.Fatalf("root mismatch: got %q want %q", h.Root(), wantRoot)
	}
	if got := h.SessionName(); got != "ntm-e2e-attention-feed-harness-20260321T024506Z-smoke" {
		t.Fatalf("session mismatch: got %q", got)
	}

	for _, kind := range allArtifactKinds {
		info, err := os.Stat(h.Dir(kind))
		if err != nil {
			t.Fatalf("stat %s dir: %v", kind, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s path is not a dir", kind)
		}
	}
}

func TestMakeSessionNamePreservesLongInputUniqueness(t *testing.T) {
	startedAt := time.Date(2026, 3, 21, 2, 45, 6, 0, time.UTC)
	scenario := strings.Repeat("long-scenario-", 8)
	first := makeSessionName("ntm-e2e", scenario, startedAt, "run-token-a")
	second := makeSessionName("ntm-e2e", scenario, startedAt, "run-token-b")

	if len(first) > 64 || len(second) > 64 {
		t.Fatalf("long session names exceed tmux limit: first=%d second=%d", len(first), len(second))
	}
	if first == second {
		t.Fatalf("long session names lost unique suffix information: %q", first)
	}
}

func TestScenarioHarnessWritesCommandArtifactsAndTimeline(t *testing.T) {
	base := t.TempDir()
	fixed := time.Date(2026, 3, 21, 3, 0, 0, 0, time.UTC)

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "command capture",
		ArtifactRoot: base,
		RunToken:     "case",
		Retain:       RetainAlways,
		Clock:        func() time.Time { return fixed },
		FailureState: func() bool { return false },
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			return CommandResult{
				StartedAt:   fixed,
				CompletedAt: fixed.Add(1500 * time.Millisecond),
				Duration:    1500 * time.Millisecond,
				ExitCode:    0,
				Stdout:      []byte("hello stdout\n"),
				Stderr:      []byte("hello stderr\n"),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}

	result, err := h.RunCommand(CommandSpec{
		Name: "robot-status",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-status"},
	})
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}
	if result.StdoutPath == "" || result.StderrPath == "" || result.MetadataPath == "" {
		t.Fatalf("expected command artifact paths, got %+v", result)
	}

	if _, err := h.WriteCursorTrace("cursor-trace.json", []byte(`{"cursor":"c-1"}`)); err != nil {
		t.Fatalf("WriteCursorTrace failed: %v", err)
	}
	if _, err := h.WriteTransportCapture("transport.ndjson", []byte("{}\n")); err != nil {
		t.Fatalf("WriteTransportCapture failed: %v", err)
	}
	if _, err := h.WriteRenderedSummary("digest.md", []byte("# Summary\n")); err != nil {
		t.Fatalf("WriteRenderedSummary failed: %v", err)
	}
	if err := h.RecordStep("after-command", map[string]any{"cursor": "c-1", "wake_reason": "manual"}); err != nil {
		t.Fatalf("RecordStep failed: %v", err)
	}
	h.Close()

	stdoutBytes, err := os.ReadFile(result.StdoutPath)
	if err != nil {
		t.Fatalf("read stdout artifact: %v", err)
	}
	if string(stdoutBytes) != "hello stdout\n" {
		t.Fatalf("stdout artifact mismatch: %q", string(stdoutBytes))
	}

	timelineBytes, err := os.ReadFile(filepath.Join(h.Root(), "timeline.jsonl"))
	if err != nil {
		t.Fatalf("read timeline: %v", err)
	}
	timeline := string(timelineBytes)
	if !strings.Contains(timeline, "\"name\":\"robot-status\"") {
		t.Fatalf("timeline missing command entry: %s", timeline)
	}
	if !strings.Contains(timeline, "\"name\":\"after-command\"") {
		t.Fatalf("timeline missing custom step entry: %s", timeline)
	}
	if _, err := os.Stat(filepath.Join(h.Dir(ArtifactTraces), "004-cursor-trace.json")); err != nil {
		t.Fatalf("expected cursor trace artifact: %v", err)
	}
}

func TestScenarioHarnessAllowFailureStillReportsLifecycleFailure(t *testing.T) {
	lifecycleErr := errors.New("owned process-group cleanup failure")
	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "allow failure lifecycle",
		ArtifactRoot: t.TempDir(),
		RunToken:     "lifecycle",
		Retain:       RetainAlways,
		FailureState: func() bool { return false },
		Runner: func(_ context.Context, _ CommandSpec) (CommandResult, error) {
			return CommandResult{
				ExitCode:           0,
				processExitSuccess: true,
				lifecycleErr:       lifecycleErr,
			}, errors.Join(exec.ErrWaitDelay, lifecycleErr)
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}
	defer h.Close()

	_, err = h.RunCommand(CommandSpec{
		Name:         "expected-failure",
		Path:         "/usr/bin/false",
		AllowFailure: true,
	})
	if !errors.Is(err, lifecycleErr) {
		t.Fatalf("RunCommand error = %v, want lifecycle failure despite AllowFailure", err)
	}
}

func TestScenarioHarnessRetentionPolicy(t *testing.T) {
	fixed := time.Date(2026, 3, 21, 3, 30, 0, 0, time.UTC)

	t.Run("removes successful runs when policy is never", func(t *testing.T) {
		base := t.TempDir()
		h, err := NewScenarioHarness(t, HarnessOptions{
			Scenario:     "cleanup",
			ArtifactRoot: base,
			RunToken:     "gone",
			Retain:       RetainNever,
			Clock:        func() time.Time { return fixed },
			FailureState: func() bool { return false },
		})
		if err != nil {
			t.Fatalf("NewScenarioHarness failed: %v", err)
		}

		root := h.Root()
		h.Close()
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected root to be removed, stat err=%v", err)
		}
	})

	t.Run("retains failed runs on on_failure policy", func(t *testing.T) {
		base := t.TempDir()
		h, err := NewScenarioHarness(t, HarnessOptions{
			Scenario:     "cleanup",
			ArtifactRoot: base,
			RunToken:     "kept",
			Retain:       RetainOnFailure,
			Clock:        func() time.Time { return fixed },
			FailureState: func() bool { return true },
		})
		if err != nil {
			t.Fatalf("NewScenarioHarness failed: %v", err)
		}

		root := h.Root()
		h.Close()
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("expected retained root, stat err=%v", err)
		}
	})
}

func TestAssertOperatorLoopStateWritesDiagnostics(t *testing.T) {
	base := t.TempDir()
	fixed := time.Date(2026, 3, 21, 4, 0, 0, 0, time.UTC)

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "operator loop",
		ArtifactRoot: base,
		RunToken:     "diag",
		Retain:       RetainAlways,
		Clock:        func() time.Time { return fixed },
		FailureState: func() bool { return false },
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}
	defer h.Close()

	err = h.AssertOperatorLoopState("missing-fields", OperatorLoopState{
		Degraded: true,
	}, OperatorLoopExpectation{
		RequireCursor:      true,
		RequireWakeReason:  true,
		RequireFocusTarget: true,
		AllowDegraded:      false,
	})
	if err == nil {
		t.Fatal("expected invariant failure")
	}
	msg := err.Error()
	for _, want := range []string{"cursor is required", "wake reason is required", "focus target is required", "degraded marker was not expected"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in error %q", want, msg)
		}
	}

	files, err := filepath.Glob(filepath.Join(h.Dir(ArtifactSummaries), "*-operator-loop.json"))
	if err != nil {
		t.Fatalf("glob summaries: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one operator loop summary, got %d", len(files))
	}
}

func TestSetupTmuxSessionTreatsMissingCleanupAsBenign(t *testing.T) {
	base := t.TempDir()
	fixed := time.Date(2026, 3, 21, 4, 30, 0, 0, time.UTC)
	var seen []string

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "tmux setup",
		ArtifactRoot: base,
		RunToken:     "tmux",
		Retain:       RetainAlways,
		Clock:        func() time.Time { return fixed },
		FailureState: func() bool { return false },
		LookPath: func(file string) (string, error) {
			if file != "tmux" {
				return "", errors.New("unexpected binary")
			}
			return "/usr/bin/tmux", nil
		},
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			seen = append(seen, strings.Join(append([]string{spec.Path}, spec.Args...), " "))
			switch spec.Name {
			case "tmux-new-session":
				return CommandResult{ExitCode: 0}, nil
			case "tmux-kill-session":
				return CommandResult{
					ExitCode: 1,
					Stderr:   []byte("can't find session: already-gone"),
				}, errors.New("exit status 1")
			default:
				return CommandResult{}, errors.New("unexpected command")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}

	if err := h.SetupTmuxSession(TmuxSessionOptions{Width: 120, Height: 40}); err != nil {
		t.Fatalf("SetupTmuxSession failed: %v", err)
	}
	h.Close()

	if len(seen) != 2 {
		t.Fatalf("expected create+cleanup tmux commands, got %d: %v", len(seen), seen)
	}
}

func TestSetupTmuxSessionRejectsTimedOutCleanupWithBenignStderr(t *testing.T) {
	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "tmux cleanup timeout",
		ArtifactRoot: t.TempDir(),
		RunToken:     "tmux-cleanup-timeout",
		Retain:       RetainAlways,
		FailureState: func() bool { return false },
		LookPath: func(file string) (string, error) {
			if file != "tmux" {
				return "", errors.New("unexpected binary")
			}
			return "/usr/bin/tmux", nil
		},
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			switch spec.Name {
			case "tmux-new-session":
				return CommandResult{ExitCode: 0, executionSucceeded: true}, nil
			case "tmux-kill-session":
				return CommandResult{
					ExitCode: -1,
					TimedOut: true,
					Stderr:   []byte("no server running on /tmp/tmux-1000/default"),
				}, context.DeadlineExceeded
			default:
				return CommandResult{}, errors.New("unexpected command")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}
	if err := h.SetupTmuxSession(TmuxSessionOptions{}); err != nil {
		t.Fatalf("SetupTmuxSession failed: %v", err)
	}

	h.Close()
	manifestBytes, err := os.ReadFile(filepath.Join(h.Root(), "manifest.json"))
	if err != nil {
		t.Fatalf("read retained manifest: %v", err)
	}
	var manifest HarnessManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode retained manifest: %v", err)
	}
	if len(manifest.Cleanup) != 1 || manifest.Cleanup[0].Status != "error" ||
		!strings.Contains(manifest.Cleanup[0].Error, context.DeadlineExceeded.Error()) {
		t.Fatalf("cleanup manifest = %+v, want fatal timeout despite benign stderr", manifest.Cleanup)
	}
}

func TestSetupTmuxSessionRegistersCleanupWhenArtifactPersistenceFails(t *testing.T) {
	base := t.TempDir()
	fixed := time.Date(2026, 3, 21, 4, 45, 0, 0, time.UTC)
	active := false
	lifecycleErr := errors.New("owned process-group cleanup failure")
	var seen []string

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "tmux artifact failure cleanup",
		ArtifactRoot: base,
		RunToken:     "tmux-artifact-failure",
		Retain:       RetainAlways,
		Clock:        func() time.Time { return fixed },
		FailureState: func() bool { return false },
		LookPath: func(file string) (string, error) {
			if file != "tmux" {
				return "", errors.New("unexpected binary")
			}
			return "/usr/bin/tmux", nil
		},
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			seen = append(seen, spec.Name)
			switch spec.Name {
			case "tmux-new-session":
				active = true
				return CommandResult{
					ExitCode:           0,
					processExitSuccess: true,
					lifecycleErr:       lifecycleErr,
				}, errors.Join(exec.ErrWaitDelay, lifecycleErr)
			case "tmux-kill-session":
				if !active {
					return CommandResult{ExitCode: 1}, errors.New("session is not active")
				}
				active = false
				return CommandResult{ExitCode: 0}, nil
			default:
				return CommandResult{}, errors.New("unexpected command")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}

	h.dirs[ArtifactCommands] = filepath.Join(base, "never-created", "commands")
	err = h.SetupTmuxSession(TmuxSessionOptions{})
	if err == nil || !strings.Contains(err.Error(), "write commands artifact") ||
		!errors.Is(err, os.ErrNotExist) || !errors.Is(err, lifecycleErr) {
		t.Fatalf("SetupTmuxSession error = %v, want artifact and lifecycle failures", err)
	}
	if !active {
		t.Fatal("fake tmux session was not active after successful create execution")
	}

	h.Close()
	if active {
		t.Fatal("fake tmux session remained active after harness cleanup")
	}
	if len(seen) != 2 || seen[0] != "tmux-new-session" || seen[1] != "tmux-kill-session" {
		t.Fatalf("commands = %v, want create followed by cleanup", seen)
	}

	manifestBytes, err := os.ReadFile(filepath.Join(h.Root(), "manifest.json"))
	if err != nil {
		t.Fatalf("read retained manifest: %v", err)
	}
	var manifest HarnessManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode retained manifest: %v", err)
	}
	if len(manifest.Cleanup) != 1 || manifest.Cleanup[0].Name != "tmux-kill-session" || manifest.Cleanup[0].Status != "ok" {
		t.Fatalf("cleanup manifest = %+v, want one successful tmux cleanup", manifest.Cleanup)
	}
}

func TestSetupTmuxSessionRegistersCleanupAfterTimedOutCreate(t *testing.T) {
	base := t.TempDir()
	active := false
	var seen []string

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "tmux timed-out create cleanup",
		ArtifactRoot: base,
		RunToken:     "tmux-create-timeout",
		Retain:       RetainAlways,
		FailureState: func() bool { return false },
		LookPath: func(file string) (string, error) {
			if file != "tmux" {
				return "", errors.New("unexpected binary")
			}
			return "/usr/bin/tmux", nil
		},
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			seen = append(seen, spec.Name)
			switch spec.Name {
			case "tmux-new-session":
				active = true
				return CommandResult{ExitCode: -1, TimedOut: true, executionStarted: true}, context.DeadlineExceeded
			case "tmux-kill-session":
				if !active {
					return CommandResult{ExitCode: 1}, errors.New("session is not active")
				}
				active = false
				return CommandResult{ExitCode: 0}, nil
			default:
				return CommandResult{}, errors.New("unexpected command")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}

	if err := h.SetupTmuxSession(TmuxSessionOptions{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SetupTmuxSession error = %v, want deadline exceeded", err)
	}
	if !active {
		t.Fatal("fake tmux session was not left active by the timed-out create")
	}

	h.Close()
	if active {
		t.Fatal("timed-out tmux creation left its session active after harness cleanup")
	}
	if len(seen) != 2 || seen[0] != "tmux-new-session" || seen[1] != "tmux-kill-session" {
		t.Fatalf("commands = %v, want create followed by cleanup", seen)
	}
}

func TestSetupTmuxSessionTimeoutBeforeStartPreservesPreexistingSession(t *testing.T) {
	base := t.TempDir()
	preexistingActive := true
	var seen []string

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "tmux timeout before process start",
		ArtifactRoot: base,
		RunToken:     "tmux-prestart-timeout",
		Retain:       RetainAlways,
		FailureState: func() bool { return false },
		LookPath: func(file string) (string, error) {
			if file != "tmux" {
				return "", errors.New("unexpected binary")
			}
			return "/usr/bin/tmux", nil
		},
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			seen = append(seen, spec.Name)
			switch spec.Name {
			case "tmux-new-session":
				return CommandResult{ExitCode: -1, TimedOut: true}, context.DeadlineExceeded
			case "tmux-kill-session":
				preexistingActive = false
				return CommandResult{ExitCode: 0}, nil
			default:
				return CommandResult{}, errors.New("unexpected command")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}

	err = h.SetupTmuxSession(TmuxSessionOptions{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SetupTmuxSession error = %v, want deadline exceeded", err)
	}
	h.Close()

	if !preexistingActive {
		t.Fatal("pre-start timeout cleanup killed a pre-existing same-name tmux session")
	}
	if len(seen) != 1 || seen[0] != "tmux-new-session" {
		t.Fatalf("commands = %v, want only the timed-out create attempt", seen)
	}
}

func TestSetupTmuxSessionTimedOutDuplicateCreatePreservesPreexistingSession(t *testing.T) {
	base := t.TempDir()
	preexistingActive := true
	var seen []string

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "tmux timed-out duplicate session ownership",
		ArtifactRoot: base,
		RunToken:     "tmux-timed-out-duplicate-session",
		Retain:       RetainAlways,
		FailureState: func() bool { return false },
		LookPath: func(file string) (string, error) {
			if file != "tmux" {
				return "", errors.New("unexpected binary")
			}
			return "/usr/bin/tmux", nil
		},
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			seen = append(seen, spec.Name)
			switch spec.Name {
			case "tmux-new-session":
				return CommandResult{
					ExitCode:         -1,
					TimedOut:         true,
					executionStarted: true,
					Stderr:           []byte("duplicate session: already exists"),
				}, context.DeadlineExceeded
			case "tmux-kill-session":
				preexistingActive = false
				return CommandResult{ExitCode: 0}, nil
			default:
				return CommandResult{}, errors.New("unexpected command")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}

	err = h.SetupTmuxSession(TmuxSessionOptions{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SetupTmuxSession error = %v, want deadline exceeded", err)
	}
	h.Close()

	if !preexistingActive {
		t.Fatal("timed-out duplicate create cleanup killed a pre-existing same-name tmux session")
	}
	if len(seen) != 1 || seen[0] != "tmux-new-session" {
		t.Fatalf("commands = %v, want only the timed-out duplicate create attempt", seen)
	}
}

func TestSetupTmuxSessionRegistersCleanupAfterSuccessfulCreatePipeDrainFailure(t *testing.T) {
	base := t.TempDir()
	active := false
	var seen []string

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "tmux successful create pipe drain failure",
		ArtifactRoot: base,
		RunToken:     "tmux-create-pipe-drain",
		Retain:       RetainAlways,
		FailureState: func() bool { return false },
		LookPath: func(file string) (string, error) {
			if file != "tmux" {
				return "", errors.New("unexpected binary")
			}
			return "/usr/bin/tmux", nil
		},
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			seen = append(seen, spec.Name)
			switch spec.Name {
			case "tmux-new-session":
				active = true
				return CommandResult{ExitCode: 0}, exec.ErrWaitDelay
			case "tmux-kill-session":
				active = false
				return CommandResult{ExitCode: 0}, nil
			default:
				return CommandResult{}, errors.New("unexpected command")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}

	err = h.SetupTmuxSession(TmuxSessionOptions{})
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("SetupTmuxSession error = %v, want exec.ErrWaitDelay", err)
	}
	if !active {
		t.Fatal("fake tmux session was not active after successful process exit")
	}
	h.Close()

	if active {
		t.Fatal("successful tmux create with pipe-drain failure was not cleaned up")
	}
	if len(seen) != 2 || seen[0] != "tmux-new-session" || seen[1] != "tmux-kill-session" {
		t.Fatalf("commands = %v, want create followed by cleanup", seen)
	}
}

func TestSetupTmuxSessionRegistersCleanupAfterSuccessfulCreateGuardianFailure(t *testing.T) {
	base := t.TempDir()
	active := false
	var seen []string

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "tmux successful create guardian failure",
		ArtifactRoot: base,
		RunToken:     "tmux-create-guardian-failure",
		Retain:       RetainAlways,
		FailureState: func() bool { return false },
		LookPath: func(file string) (string, error) {
			if file != "tmux" {
				return "", errors.New("unexpected binary")
			}
			return "/usr/bin/tmux", nil
		},
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			seen = append(seen, spec.Name)
			switch spec.Name {
			case "tmux-new-session":
				active = true
				return CommandResult{
					ExitCode:           0,
					executionStarted:   true,
					processExitSuccess: true,
				}, errors.New("process-group guardian cleanup failed")
			case "tmux-kill-session":
				active = false
				return CommandResult{ExitCode: 0}, nil
			default:
				return CommandResult{}, errors.New("unexpected command")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}

	err = h.SetupTmuxSession(TmuxSessionOptions{})
	if err == nil || !strings.Contains(err.Error(), "guardian cleanup failed") {
		t.Fatalf("SetupTmuxSession error = %v, want guardian cleanup failure", err)
	}
	if !active {
		t.Fatal("fake tmux session was not active after successful process exit")
	}
	h.Close()

	if active {
		t.Fatal("successful tmux create with guardian failure was not cleaned up")
	}
	if len(seen) != 2 || seen[0] != "tmux-new-session" || seen[1] != "tmux-kill-session" {
		t.Fatalf("commands = %v, want create followed by cleanup", seen)
	}
}

func TestSetupTmuxSessionCreateFailurePreservesPreexistingSession(t *testing.T) {
	base := t.TempDir()
	preexistingActive := true
	var seen []string

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "tmux duplicate session ownership",
		ArtifactRoot: base,
		RunToken:     "tmux-duplicate-session",
		Retain:       RetainAlways,
		FailureState: func() bool { return false },
		LookPath: func(file string) (string, error) {
			if file != "tmux" {
				return "", errors.New("unexpected binary")
			}
			return "/usr/bin/tmux", nil
		},
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			seen = append(seen, spec.Name)
			switch spec.Name {
			case "tmux-new-session":
				return CommandResult{
					ExitCode: 1,
					Stderr:   []byte("duplicate session: already exists"),
				}, errors.New("exit status 1")
			case "tmux-kill-session":
				preexistingActive = false
				return CommandResult{ExitCode: 0}, nil
			default:
				return CommandResult{}, errors.New("unexpected command")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}

	err = h.SetupTmuxSession(TmuxSessionOptions{})
	if err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("SetupTmuxSession error = %v, want duplicate-session create failure", err)
	}
	h.Close()

	if !preexistingActive {
		t.Fatal("failed create cleanup killed a pre-existing same-name tmux session")
	}
	if len(seen) != 1 || seen[0] != "tmux-new-session" {
		t.Fatalf("commands = %v, want only the failed create attempt", seen)
	}
}

func TestDefaultRunnerCancellationKillsDescendantHoldingOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process-tree disappearance assertion uses Linux /proc")
	}

	pidPath := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	type runnerOutcome struct {
		result CommandResult
		err    error
	}
	done := make(chan runnerOutcome, 1)
	go func() {
		result, err := defaultRunner(ctx, CommandSpec{
			Path: "/bin/sh",
			Args: []string{
				"-c",
				`sleep 600 & child=$!; printf '%s\n' "$child" > "$1"; wait "$child"`,
				"harness-descendant",
				pidPath,
			},
		})
		done <- runnerOutcome{result: result, err: err}
	}()
	joined := false
	defer func() {
		if joined {
			return
		}
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}()

	var pid int
	readinessDeadline := time.Now().Add(30 * time.Second)
	for pid <= 0 {
		pidBytes, err := os.ReadFile(pidPath)
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(pidBytes)))
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read descendant pid: %v", err)
		}
		select {
		case outcome := <-done:
			joined = true
			t.Fatalf("defaultRunner returned before descendant readiness: result=%+v err=%v", outcome.result, outcome.err)
		default:
		}
		if time.Now().After(readinessDeadline) {
			t.Fatalf("descendant pid was not published within 30s")
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancelStarted := time.Now()
	cancel()
	var outcome runnerOutcome
	select {
	case outcome = <-done:
		joined = true
	case <-time.After(10 * time.Second):
		t.Fatal("defaultRunner did not return within 10s of cancellation")
	}
	if !errors.Is(ctx.Err(), context.Canceled) || outcome.err == nil || outcome.result.TimedOut {
		t.Fatalf("defaultRunner result=%+v err=%v ctx=%v, want a canceled non-timeout process group", outcome.result, outcome.err, ctx.Err())
	}
	if elapsed := time.Since(cancelStarted); elapsed > 10*time.Second {
		t.Fatalf("defaultRunner returned %s after cancellation, want bounded pipe drain", elapsed)
	}

	requireLinuxProcessTerminated(t, pid, 3*time.Second)
}

func TestDefaultRunnerNaturalRootExitKillsDescendantHoldingOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process-tree disappearance assertion uses Linux /proc")
	}

	pidPath := filepath.Join(t.TempDir(), "child.pid")
	result, err := defaultRunner(context.Background(), CommandSpec{
		Path: "/bin/sh",
		Args: []string{
			"-c",
			`sleep 600 & child=$!; printf '%s\n' "$child" > "$1"; exit 7`,
			"harness-natural-exit-descendant",
			pidPath,
		},
	})
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || errors.Is(err, exec.ErrWaitDelay) || result.ExitCode != 7 || result.TimedOut {
		t.Fatalf("defaultRunner result=%+v err=%v, want masked pipe-drain error with exit code 7", result, err)
	}
	pidBytes, readErr := os.ReadFile(pidPath)
	if readErr != nil {
		t.Fatalf("read natural-exit descendant pid: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if parseErr != nil || pid <= 0 {
		t.Fatalf("parse natural-exit descendant pid %q: %v", pidBytes, parseErr)
	}
	requireLinuxProcessTerminated(t, pid, 3*time.Second)
}

func requireLinuxProcessTerminated(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	statusPath := filepath.Join("/proc", strconv.Itoa(pid), "status")
	deadline := time.Now().Add(timeout)
	for {
		status, readErr := os.ReadFile(statusPath)
		if errors.Is(readErr, os.ErrNotExist) || errors.Is(readErr, syscall.ESRCH) {
			break
		}
		if readErr != nil {
			t.Fatalf("read descendant process %d status: %v", pid, readErr)
		}
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "State:" && fields[1] == "Z" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant process %d still exists after owned process-group cleanup", pid)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestScenarioHarnessOperatorLoopTranscriptArtifacts(t *testing.T) {
	base := t.TempDir()
	fixed := time.Date(2026, 3, 21, 5, 0, 0, 0, time.UTC)

	mustJSON := func(v map[string]any) []byte {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal fake command payload: %v", err)
		}
		return data
	}

	decodeJSON := func(result CommandResult) map[string]any {
		t.Helper()
		var payload map[string]any
		if err := json.Unmarshal(result.Stdout, &payload); err != nil {
			t.Fatalf("decode %s stdout as JSON: %v\nstdout=%s", result.Name, err, string(result.Stdout))
		}
		return payload
	}

	var snapshotCalls int
	var dashboardCalls int
	var terseCalls int

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "operator loop transcript",
		ArtifactRoot: base,
		RunToken:     "transcript",
		Retain:       RetainAlways,
		Clock:        func() time.Time { return fixed },
		FailureState: func() bool { return false },
		Runner: func(_ context.Context, spec CommandSpec) (CommandResult, error) {
			result := CommandResult{
				StartedAt:   fixed,
				CompletedAt: fixed.Add(25 * time.Millisecond),
				Duration:    25 * time.Millisecond,
				ExitCode:    0,
			}

			command := strings.Join(spec.Args, " ")
			switch {
			case command == "--robot-snapshot":
				snapshotCalls++
				latestCursor := int64(42)
				if snapshotCalls > 1 {
					latestCursor = 44
				}
				result.Stdout = mustJSON(map[string]any{
					"success":       true,
					"latest_cursor": latestCursor,
					"replay_window": map[string]any{
						"oldest_cursor":  40,
						"latest_cursor":  latestCursor,
						"resync_command": "ntm --robot-snapshot",
					},
				})
				return result, nil
			case command == "--robot-dashboard":
				dashboardCalls++
				if dashboardCalls == 1 {
					result.Stdout = []byte("# Dashboard\n## Attention\n_Feed is clear._\n### Suggested Next Steps\n- `ntm --robot-attention --since-cursor=42`\n")
				} else {
					result.Stdout = []byte("# Dashboard\n## Attention\n| Key | Value |\n|---|---|\n| Action Required | 1 |\n### Suggested Next Steps\n- `ntm --robot-tail=proj --panes=2 --lines=50`\n")
				}
				return result, nil
			case command == "--robot-terse":
				terseCalls++
				if terseCalls == 1 {
					result.Stdout = []byte("S:proj|A:1/1|W:0|I:1|E:0|C:0%|B:R1/I0/B0|M:0|^:0|!:0\n")
				} else {
					result.Stdout = []byte("S:proj|A:1/1|W:1|I:0|E:0|C:0%|B:R1/I0/B0|M:1|^:1a|!:1c\n")
				}
				return result, nil
			case command == "--robot-events --since-cursor=42 --limit=20":
				result.Stdout = mustJSON(map[string]any{
					"success": true,
					"events": []any{
						map[string]any{
							"cursor":        43,
							"summary":       "mail requires ack",
							"actionability": "action_required",
						},
					},
					"next_cursor": 43,
				})
				return result, nil
			case command == "--robot-attention --since-cursor=43 --profile=operator":
				result.Stdout = mustJSON(map[string]any{
					"success":       true,
					"wake_reason":   "action_required",
					"focus_targets": []any{"proj:2"},
					"cursor_info": map[string]any{
						"start_cursor": 43,
						"end_cursor":   44,
					},
					"digest": map[string]any{
						"event_count": 1,
						"summary":     "1 action-required event",
					},
				})
				return result, nil
			case command == "--robot-attention --since-cursor=43 --profile=debug":
				result.Stdout = mustJSON(map[string]any{
					"success":       true,
					"wake_reason":   "action_required",
					"wake_reasons":  []any{"action_required", "mail_pending"},
					"focus_targets": []any{"proj:2"},
					"profile":       "debug",
					"cursor_info": map[string]any{
						"start_cursor": 43,
						"end_cursor":   44,
					},
					"digest": map[string]any{
						"event_count": 2,
						"summary":     "2 events including interesting context",
					},
				})
				return result, nil
			case command == "--robot-capabilities":
				result.Stdout = mustJSON(map[string]any{
					"success": true,
					"attention": map[string]any{
						"contract_version": "1.0.0",
						"default_profile":  "operator",
						"features": map[string]any{
							"cursor_replay": map[string]any{
								"status": "available",
								"note":   "Replay uses monotonic cursors with explicit resync instructions on expiration.",
							},
							"operator_boundary": map[string]any{
								"status": "available",
								"note":   "Attention feed remains a sensing/actuation surface only; it does not assign work, infer intent, or replace beads, bv, or Agent Mail.",
							},
						},
					},
				})
				return result, nil
			case command == "--robot-help":
				result.Stdout = []byte("Attention Feed (Operator Loop):\n  --robot-attention      Wait-then-digest (the one obvious tending command)\nIf cursor expires: re-run --robot-snapshot to resync.\nTips for AI Agents:\n- Attention feed is a sensing/actuation surface, not a planner: it does not assign beads, infer intent, or replace beads, bv, or Agent Mail.\n")
				return result, nil
			case command == "--robot-events --since-cursor=1 --limit=20":
				result.ExitCode = 1
				result.Stdout = mustJSON(map[string]any{
					"success":    false,
					"error_code": "CURSOR_EXPIRED",
					"details": map[string]any{
						"requested_cursor": 1,
						"earliest_cursor":  40,
						"resync_command":   "ntm --robot-snapshot",
					},
				})
				return result, errors.New("exit status 1")
			default:
				return CommandResult{}, errors.New("unexpected command: " + command)
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness failed: %v", err)
	}
	defer h.Close()

	bootstrap, err := h.RunCommand(CommandSpec{
		Name: "robot-snapshot-bootstrap",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-snapshot"},
	})
	if err != nil {
		t.Fatalf("bootstrap snapshot failed: %v", err)
	}
	bootstrapPayload := decodeJSON(bootstrap)
	if got := int64(bootstrapPayload["latest_cursor"].(float64)); got != 42 {
		t.Fatalf("bootstrap latest_cursor = %d, want 42", got)
	}
	if err := h.AssertOperatorLoopState("bootstrap", OperatorLoopState{
		Cursor:      "42",
		FocusTarget: "snapshot-bootstrap",
		Details: map[string]any{
			"replay_window": bootstrapPayload["replay_window"],
		},
	}, OperatorLoopExpectation{
		RequireCursor:      true,
		RequireFocusTarget: true,
		AllowDegraded:      true,
	}); err != nil {
		t.Fatalf("bootstrap operator-loop assertion failed: %v", err)
	}
	if err := h.RecordStep("bootstrap_snapshot", map[string]any{
		"latest_cursor": 42,
		"oldest_cursor": 40,
	}); err != nil {
		t.Fatalf("RecordStep bootstrap_snapshot failed: %v", err)
	}

	dashboardQuiet, err := h.RunCommand(CommandSpec{
		Name: "robot-dashboard-quiet",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-dashboard"},
	})
	if err != nil {
		t.Fatalf("quiet dashboard failed: %v", err)
	}
	if _, err := h.WriteRenderedSummary("dashboard-quiet.md", dashboardQuiet.Stdout); err != nil {
		t.Fatalf("WriteRenderedSummary dashboard quiet failed: %v", err)
	}
	if !strings.Contains(string(dashboardQuiet.Stdout), "_Feed is clear._") {
		t.Fatalf("quiet dashboard missing clear marker: %s", string(dashboardQuiet.Stdout))
	}

	terseQuiet, err := h.RunCommand(CommandSpec{
		Name: "robot-terse-quiet",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-terse"},
	})
	if err != nil {
		t.Fatalf("quiet terse failed: %v", err)
	}
	if _, err := h.WriteRenderedSummary("terse-quiet.txt", terseQuiet.Stdout); err != nil {
		t.Fatalf("WriteRenderedSummary terse quiet failed: %v", err)
	}
	if !strings.Contains(string(terseQuiet.Stdout), "|^:0|") {
		t.Fatalf("quiet terse missing attention-clear encoding: %s", string(terseQuiet.Stdout))
	}

	replay, err := h.RunCommand(CommandSpec{
		Name: "robot-events-replay",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-events", "--since-cursor=42", "--limit=20"},
	})
	if err != nil {
		t.Fatalf("events replay failed: %v", err)
	}
	replayPayload := decodeJSON(replay)
	if got := int64(replayPayload["next_cursor"].(float64)); got != 43 {
		t.Fatalf("replay next_cursor = %d, want 43", got)
	}
	if err := h.RecordStep("raw_replay", map[string]any{
		"next_cursor": 43,
		"event_count": len(replayPayload["events"].([]any)),
	}); err != nil {
		t.Fatalf("RecordStep raw_replay failed: %v", err)
	}

	operatorAttention, err := h.RunCommand(CommandSpec{
		Name: "robot-attention-operator",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-attention", "--since-cursor=43", "--profile=operator"},
	})
	if err != nil {
		t.Fatalf("operator attention failed: %v", err)
	}
	operatorPayload := decodeJSON(operatorAttention)
	operatorDigest := operatorPayload["digest"].(map[string]any)
	operatorCursor := operatorPayload["cursor_info"].(map[string]any)
	if got := int64(operatorCursor["end_cursor"].(float64)); got != 44 {
		t.Fatalf("operator end_cursor = %d, want 44", got)
	}
	if err := h.AssertOperatorLoopState("operator-profile", OperatorLoopState{
		Cursor:      "44",
		WakeReason:  "action_required",
		FocusTarget: "proj:2",
		Details: map[string]any{
			"profile": "operator",
			"digest":  operatorDigest,
		},
	}, OperatorLoopExpectation{
		RequireCursor:      true,
		RequireWakeReason:  true,
		RequireFocusTarget: true,
	}); err != nil {
		t.Fatalf("operator profile assertion failed: %v", err)
	}

	debugAttention, err := h.RunCommand(CommandSpec{
		Name: "robot-attention-debug",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-attention", "--since-cursor=43", "--profile=debug"},
	})
	if err != nil {
		t.Fatalf("debug attention failed: %v", err)
	}
	debugPayload := decodeJSON(debugAttention)
	debugDigest := debugPayload["digest"].(map[string]any)
	if debugDigest["event_count"].(float64) <= operatorDigest["event_count"].(float64) {
		t.Fatalf("debug digest should contain more context than operator digest: operator=%v debug=%v", operatorDigest, debugDigest)
	}
	if err := h.RecordStep("profile_compare", map[string]any{
		"operator_event_count": operatorDigest["event_count"],
		"debug_event_count":    debugDigest["event_count"],
		"wake_reasons":         debugPayload["wake_reasons"],
	}); err != nil {
		t.Fatalf("RecordStep profile_compare failed: %v", err)
	}

	dashboardBusy, err := h.RunCommand(CommandSpec{
		Name: "robot-dashboard-busy",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-dashboard"},
	})
	if err != nil {
		t.Fatalf("busy dashboard failed: %v", err)
	}
	if _, err := h.WriteRenderedSummary("dashboard-busy.md", dashboardBusy.Stdout); err != nil {
		t.Fatalf("WriteRenderedSummary dashboard busy failed: %v", err)
	}
	if !strings.Contains(string(dashboardBusy.Stdout), "| Action Required | 1 |") {
		t.Fatalf("busy dashboard missing action-required summary: %s", string(dashboardBusy.Stdout))
	}

	terseBusy, err := h.RunCommand(CommandSpec{
		Name: "robot-terse-busy",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-terse"},
	})
	if err != nil {
		t.Fatalf("busy terse failed: %v", err)
	}
	if _, err := h.WriteRenderedSummary("terse-busy.txt", terseBusy.Stdout); err != nil {
		t.Fatalf("WriteRenderedSummary terse busy failed: %v", err)
	}
	if !strings.Contains(string(terseBusy.Stdout), "|^:1a|") {
		t.Fatalf("busy terse missing action-required encoding: %s", string(terseBusy.Stdout))
	}

	expired, err := h.RunCommand(CommandSpec{
		Name:         "robot-events-expired",
		Path:         "/usr/bin/ntm",
		Args:         []string{"--robot-events", "--since-cursor=1", "--limit=20"},
		AllowFailure: true,
	})
	if err != nil {
		t.Fatalf("expired cursor replay should be allowed, got error: %v", err)
	}
	expiredPayload := decodeJSON(expired)
	if expired.ExitCode != 1 {
		t.Fatalf("expired cursor exit_code = %d, want 1", expired.ExitCode)
	}
	if got := expiredPayload["error_code"]; got != "CURSOR_EXPIRED" {
		t.Fatalf("expired cursor error_code = %v, want CURSOR_EXPIRED", got)
	}
	if err := h.RecordStep("cursor_resync_required", map[string]any{
		"requested_cursor": 1,
		"resync_command":   "ntm --robot-snapshot",
	}); err != nil {
		t.Fatalf("RecordStep cursor_resync_required failed: %v", err)
	}

	tracePayload, err := json.MarshalIndent([]map[string]any{
		{"step": "bootstrap", "cursor": 42},
		{"step": "replay", "cursor": 43},
		{"step": "operator_attention", "cursor": 44, "wake_reason": "action_required"},
		{"step": "expired_cursor", "requested_cursor": 1, "resync_command": "ntm --robot-snapshot"},
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal cursor trace: %v", err)
	}
	if _, err := h.WriteCursorTrace("operator-loop-trace.json", append(tracePayload, '\n')); err != nil {
		t.Fatalf("WriteCursorTrace failed: %v", err)
	}
	if _, err := h.WriteTransportCapture("attention-feed.ndjson", []byte("{\"cursor\":43,\"summary\":\"mail requires ack\"}\n")); err != nil {
		t.Fatalf("WriteTransportCapture failed: %v", err)
	}

	resync, err := h.RunCommand(CommandSpec{
		Name: "robot-snapshot-resync",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-snapshot"},
	})
	if err != nil {
		t.Fatalf("resync snapshot failed: %v", err)
	}
	resyncPayload := decodeJSON(resync)
	if got := int64(resyncPayload["latest_cursor"].(float64)); got != 44 {
		t.Fatalf("resync latest_cursor = %d, want 44", got)
	}

	capabilities, err := h.RunCommand(CommandSpec{
		Name: "robot-capabilities",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-capabilities"},
	})
	if err != nil {
		t.Fatalf("capabilities command failed: %v", err)
	}
	if _, err := h.WriteRenderedSummary("capabilities-surface.json", capabilities.Stdout); err != nil {
		t.Fatalf("WriteRenderedSummary capabilities failed: %v", err)
	}
	capsPayload := decodeJSON(capabilities)
	attentionCaps := capsPayload["attention"].(map[string]any)
	features := attentionCaps["features"].(map[string]any)
	if _, ok := features["operator_boundary"]; !ok {
		t.Fatalf("capabilities surface missing operator_boundary feature: %+v", attentionCaps)
	}
	if err := h.RecordStep("capabilities_surface", map[string]any{
		"default_profile": attentionCaps["default_profile"],
		"features":        []string{"cursor_replay", "operator_boundary"},
	}); err != nil {
		t.Fatalf("RecordStep capabilities_surface failed: %v", err)
	}

	helpOut, err := h.RunCommand(CommandSpec{
		Name: "robot-help",
		Path: "/usr/bin/ntm",
		Args: []string{"--robot-help"},
	})
	if err != nil {
		t.Fatalf("help command failed: %v", err)
	}
	if _, err := h.WriteRenderedSummary("robot-help.txt", helpOut.Stdout); err != nil {
		t.Fatalf("WriteRenderedSummary robot-help failed: %v", err)
	}
	helpText := string(helpOut.Stdout)
	for _, want := range []string{
		"Wait-then-digest (the one obvious tending command)",
		"re-run --robot-snapshot to resync",
		"sensing/actuation surface, not a planner",
	} {
		if !strings.Contains(helpText, want) {
			t.Fatalf("help output missing %q: %s", want, helpText)
		}
	}
	if err := h.RecordStep("help_surface", map[string]any{
		"guardrail": "not a planner",
		"resync":    true,
	}); err != nil {
		t.Fatalf("RecordStep help_surface failed: %v", err)
	}

	h.Close()

	timelineBytes, err := os.ReadFile(filepath.Join(h.Root(), "timeline.jsonl"))
	if err != nil {
		t.Fatalf("read timeline: %v", err)
	}
	timeline := string(timelineBytes)
	for _, want := range []string{"bootstrap_snapshot", "raw_replay", "profile_compare", "cursor_resync_required", "capabilities_surface", "help_surface"} {
		if !strings.Contains(timeline, want) {
			t.Fatalf("timeline missing %q entry: %s", want, timeline)
		}
	}

	manifestBytes, err := os.ReadFile(filepath.Join(h.Root(), "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(manifestBytes), `"failed": false`) {
		t.Fatalf("manifest should record a passing transcript run: %s", string(manifestBytes))
	}

	cursorTraceFiles, err := filepath.Glob(filepath.Join(h.Dir(ArtifactTraces), "*-operator-loop-trace.json"))
	if err != nil {
		t.Fatalf("glob cursor trace artifacts: %v", err)
	}
	if len(cursorTraceFiles) != 1 {
		t.Fatalf("expected one operator-loop trace artifact, got %d", len(cursorTraceFiles))
	}
	cursorTraceBytes, err := os.ReadFile(cursorTraceFiles[0])
	if err != nil {
		t.Fatalf("read cursor trace artifact: %v", err)
	}
	if !strings.Contains(string(cursorTraceBytes), "\"resync_command\": \"ntm --robot-snapshot\"") {
		t.Fatalf("cursor trace missing resync command: %s", string(cursorTraceBytes))
	}

	busyDashboardFiles, err := filepath.Glob(filepath.Join(h.Dir(ArtifactSummaries), "*-dashboard-busy.md"))
	if err != nil {
		t.Fatalf("glob busy dashboard artifacts: %v", err)
	}
	if len(busyDashboardFiles) != 1 {
		t.Fatalf("expected one busy dashboard artifact, got %d", len(busyDashboardFiles))
	}
	busyDashboardBytes, err := os.ReadFile(busyDashboardFiles[0])
	if err != nil {
		t.Fatalf("read busy dashboard artifact: %v", err)
	}
	if !strings.Contains(string(busyDashboardBytes), "| Action Required | 1 |") {
		t.Fatalf("busy dashboard artifact missing attention summary: %s", string(busyDashboardBytes))
	}

	helpFiles, err := filepath.Glob(filepath.Join(h.Dir(ArtifactSummaries), "*-robot-help.txt"))
	if err != nil {
		t.Fatalf("glob help artifacts: %v", err)
	}
	if len(helpFiles) != 1 {
		t.Fatalf("expected one robot-help artifact, got %d", len(helpFiles))
	}
	helpBytes, err := os.ReadFile(helpFiles[0])
	if err != nil {
		t.Fatalf("read help artifact: %v", err)
	}
	if !strings.Contains(string(helpBytes), "not a planner") {
		t.Fatalf("help artifact missing operator boundary guardrail: %s", string(helpBytes))
	}

	capsFiles, err := filepath.Glob(filepath.Join(h.Dir(ArtifactSummaries), "*-capabilities-surface.json"))
	if err != nil {
		t.Fatalf("glob capabilities artifacts: %v", err)
	}
	if len(capsFiles) != 1 {
		t.Fatalf("expected one capabilities artifact, got %d", len(capsFiles))
	}
	capsBytes, err := os.ReadFile(capsFiles[0])
	if err != nil {
		t.Fatalf("read capabilities artifact: %v", err)
	}
	if !strings.Contains(string(capsBytes), "\"operator_boundary\"") {
		t.Fatalf("capabilities artifact missing operator boundary feature: %s", string(capsBytes))
	}
}
