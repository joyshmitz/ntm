package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func TestMockTmuxClientGetPanesReturnsSortedCopy(t *testing.T) {
	mock := NewMockTmuxClient()
	mock.AddPane("session-a", tmux.Pane{ID: "%2", Index: 2, Type: tmux.AgentGemini})
	mock.AddPane("session-a", tmux.Pane{ID: "%1", Index: 1, Type: tmux.AgentCodex})
	mock.AddPane("session-b", tmux.Pane{ID: "%3", Index: 1, Type: tmux.AgentClaude})
	t.Cleanup(mock.Reset)

	panes, err := mock.GetPanes("session-a")
	if err != nil {
		t.Fatalf("GetPanes returned error: %v", err)
	}
	if len(panes) != 2 {
		t.Fatalf("GetPanes returned %d panes, want 2", len(panes))
	}
	if panes[0].ID != "%1" || panes[1].ID != "%2" {
		t.Fatalf("GetPanes order = [%s %s], want [%%1 %%2]", panes[0].ID, panes[1].ID)
	}

	panes[0].ID = "mutated"
	panes, err = mock.GetPanes("session-a")
	if err != nil {
		t.Fatalf("GetPanes after mutation returned error: %v", err)
	}
	if panes[0].ID != "%1" {
		t.Fatalf("GetPanes leaked caller mutation, got first pane %q", panes[0].ID)
	}
}

func TestMockTmuxClientPasteAndCaptureArePaneScoped(t *testing.T) {
	mock := NewMockTmuxClient(
		tmux.Pane{ID: "%1", Index: 1, Type: tmux.AgentCodex},
		tmux.Pane{ID: "%2", Index: 2, Type: tmux.AgentGemini},
	)
	t.Cleanup(mock.Reset)

	if err := mock.SetPaneOutput("%1", "seed\n"); err != nil {
		t.Fatalf("SetPaneOutput returned error: %v", err)
	}
	if err := mock.PasteKeys("%1", "first\nsecond", true); err != nil {
		t.Fatalf("PasteKeys returned error: %v", err)
	}
	if err := mock.PasteKeys("%2", "other", true); err != nil {
		t.Fatalf("PasteKeys second pane returned error: %v", err)
	}

	got, err := mock.CapturePaneOutput("%1", 0)
	if err != nil {
		t.Fatalf("CapturePaneOutput returned error: %v", err)
	}
	if got != "seed\nfirst\nsecond\n" {
		t.Fatalf("CapturePaneOutput = %q, want seeded prompt output", got)
	}

	tail, err := mock.CapturePaneOutput("%1", 2)
	if err != nil {
		t.Fatalf("CapturePaneOutput tail returned error: %v", err)
	}
	if tail != "first\nsecond\n" {
		t.Fatalf("CapturePaneOutput tail = %q, want last two lines", tail)
	}

	other, err := mock.CapturePaneOutput("%2", 0)
	if err != nil {
		t.Fatalf("CapturePaneOutput second pane returned error: %v", err)
	}
	if other != "other\n" {
		t.Fatalf("second pane output = %q, want isolated output", other)
	}

	history, err := mock.PasteHistory("%1")
	if err != nil {
		t.Fatalf("PasteHistory returned error: %v", err)
	}
	if len(history) != 1 || history[0].Content != "first\nsecond" || !history[0].Enter {
		t.Fatalf("PasteHistory = %+v, want one entered paste", history)
	}
}

func TestAgentScripterSubstringRegexAndDefaultResponses(t *testing.T) {
	mock := NewMockTmuxClient(tmux.Pane{ID: "%1", Index: 1, Type: tmux.AgentCodex})
	scripter := NewAgentScripter().
		Match("H_ID=H-001", "EV-001 supports H-001\n").
		Default("DEFAULT RESPONSE\n")
	if err := scripter.MatchRegex(`mode=audit-[0-9]+`, "AUDIT RESPONSE\n"); err != nil {
		t.Fatalf("MatchRegex returned error: %v", err)
	}
	mock.SetAgentScripter(scripter)
	t.Cleanup(mock.Reset)

	if err := mock.PasteKeys("%1", "dispatch MO-04a-investigate H_ID=H-001", true); err != nil {
		t.Fatalf("PasteKeys substring returned error: %v", err)
	}
	if err := mock.PasteKeys("%1", "mode=audit-42", true); err != nil {
		t.Fatalf("PasteKeys regex returned error: %v", err)
	}
	if err := mock.PasteKeys("%1", "no registered pattern here", true); err != nil {
		t.Fatalf("PasteKeys default returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scripter.Wait(ctx, 3); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	output, err := mock.CapturePaneOutput("%1", 0)
	if err != nil {
		t.Fatalf("CapturePaneOutput returned error: %v", err)
	}
	for _, want := range []string{"EV-001 supports H-001", "AUDIT RESPONSE", "DEFAULT RESPONSE"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestAgentScripterSequentialResponses(t *testing.T) {
	mock := NewMockTmuxClient(tmux.Pane{ID: "%1", Index: 1, Type: tmux.AgentCodex})
	scripter := NewAgentScripter().
		Match("repeat", "first response\n").
		Match("repeat", "second response\n")
	mock.SetAgentScripter(scripter)
	t.Cleanup(mock.Reset)

	if err := mock.PasteKeys("%1", "repeat prompt", true); err != nil {
		t.Fatalf("first PasteKeys returned error: %v", err)
	}
	if err := mock.PasteKeys("%1", "repeat prompt", true); err != nil {
		t.Fatalf("second PasteKeys returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scripter.Wait(ctx, 2); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	output, err := mock.CapturePaneOutput("%1", 0)
	if err != nil {
		t.Fatalf("CapturePaneOutput returned error: %v", err)
	}
	first := strings.Index(output, "first response")
	second := strings.Index(output, "second response")
	if first == -1 || second == -1 || first >= second {
		t.Fatalf("responses not captured in registration order:\n%s", output)
	}
}

func TestAgentScripterNoMatchReturnsClearError(t *testing.T) {
	mock := NewMockTmuxClient(tmux.Pane{ID: "%1", Index: 1, Type: tmux.AgentCodex})
	mock.SetAgentScripter(NewAgentScripter())
	t.Cleanup(mock.Reset)

	err := mock.PasteKeys("%1", "unmatched prompt", true)
	if err == nil {
		t.Fatal("PasteKeys returned nil error, want script exhaustion error")
	}
	if !strings.Contains(err.Error(), "no response") {
		t.Fatalf("PasteKeys error = %q, want clear no-response message", err.Error())
	}

	history, err := mock.PasteHistory("%1")
	if err != nil {
		t.Fatalf("PasteHistory returned error: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("PasteHistory length = %d, want no paste recorded for rejected prompt", len(history))
	}
}

func TestExecutorRunUsesMockTmuxClient(t *testing.T) {
	mock := NewMockTmuxClient()
	mock.AddPane("mock-session", tmux.Pane{
		ID:    "%1",
		Index: 1,
		Title: "mock-session__cod_1",
		Type:  tmux.AgentCodex,
	})
	t.Cleanup(mock.Reset)

	cfg := DefaultExecutorConfig("mock-session")
	cfg.ProjectDir = t.TempDir()
	executor := NewExecutor(cfg)
	executor.SetTmuxClient(mock)

	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "mock-workflow",
		Settings:      DefaultWorkflowSettings(),
		Steps: []Step{
			{
				ID:     "send",
				Prompt: "hello from mock",
				Pane:   PaneSpec{Index: 1},
				Wait:   WaitNone,
			},
		},
	}

	state, err := executor.Run(context.Background(), workflow, nil, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	result, ok := state.Steps["send"]
	if !ok {
		t.Fatal("state missing send step result")
	}
	if result.Status != StatusCompleted {
		t.Fatalf("step status = %q, want %q; error: %+v", result.Status, StatusCompleted, result.Error)
	}
	if result.PaneUsed != "%1" {
		t.Fatalf("PaneUsed = %q, want %%1", result.PaneUsed)
	}

	history, err := mock.PasteHistory("%1")
	if err != nil {
		t.Fatalf("PasteHistory returned error: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("PasteHistory length = %d, want 1", len(history))
	}
	if history[0].Content != "hello from mock" || !history[0].Enter {
		t.Fatalf("PasteHistory[0] = %+v, want entered prompt", history[0])
	}
}
