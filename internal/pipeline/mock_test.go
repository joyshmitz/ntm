package pipeline

import (
	"context"
	"testing"

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
