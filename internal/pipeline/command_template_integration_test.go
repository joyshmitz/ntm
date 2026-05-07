package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func TestIntegrationCommandAndTemplatePipeline(t *testing.T) {
	projectDir := t.TempDir()
	templatePath := filepath.Join(projectDir, "fixture-template.md")
	if err := os.WriteFile(templatePath, []byte("Template dispatch says <PARAM>"), 0o644); err != nil {
		t.Fatalf("WriteFile(template) returned error: %v", err)
	}

	mock := NewMockTmuxClient(tmux.Pane{
		ID:    "%1",
		Index: 1,
		Type:  tmux.AgentCodex,
	})
	t.Cleanup(mock.Reset)

	cfg := DefaultExecutorConfig("command-template-session")
	cfg.ProjectDir = projectDir
	cfg.DefaultTimeout = time.Second
	executor := NewExecutor(cfg)
	executor.SetTmuxClient(mock)

	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "command-template-integration",
		Settings:      DefaultWorkflowSettings(),
		Steps: []Step{
			{
				ID:      "write_file",
				Command: `printf '%s' hello > test-out.txt`,
			},
			{
				ID:        "render_template",
				Template:  "fixture-template.md",
				Params:    map[string]interface{}{"PARAM": "world"},
				Pane:      PaneSpec{Index: 1},
				Wait:      WaitNone,
				DependsOn: []string{"write_file"},
			},
			{
				ID:        "read_file",
				Command:   `cat test-out.txt`,
				OutputVar: "file_contents",
				DependsOn: []string{"render_template"},
			},
			{
				ID:        "cleanup_file",
				Command:   `rm test-out.txt`,
				DependsOn: []string{"read_file"},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	state, err := executor.Run(ctx, workflow, nil, nil)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	if state.Status != StatusCompleted {
		t.Fatalf("workflow status = %q, want %q", state.Status, StatusCompleted)
	}

	assertCompletedStepOutput(t, state, "write_file", "")
	assertCompletedStepOutput(t, state, "render_template", "")
	assertCompletedStepOutput(t, state, "read_file", "hello")
	assertCompletedStepOutput(t, state, "cleanup_file", "")
	if got := state.Variables["file_contents"]; got != "hello" {
		t.Fatalf("file_contents output var = %#v, want hello", got)
	}

	history, err := mock.PasteHistory("%1")
	if err != nil {
		t.Fatalf("PasteHistory returned error: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("PasteHistory length = %d, want one template dispatch", len(history))
	}
	if !history[0].Enter {
		t.Fatal("template dispatch did not send Enter")
	}
	for _, want := range []string{"Template dispatch says world"} {
		if !strings.Contains(history[0].Content, want) {
			t.Fatalf("template dispatch missing %q:\n%s", want, history[0].Content)
		}
	}

	if _, err := os.Stat(filepath.Join(projectDir, "test-out.txt")); !os.IsNotExist(err) {
		t.Fatalf("cleanup_file left test-out.txt stat error = %v, want not exist", err)
	}
}

func assertCompletedStepOutput(t *testing.T, state *ExecutionState, stepID string, wantOutput string) {
	t.Helper()
	result, ok := state.Steps[stepID]
	if !ok {
		t.Fatalf("missing result for step %s", stepID)
	}
	if result.Status != StatusCompleted {
		t.Fatalf("%s status = %q, want %q; error = %#v", stepID, result.Status, StatusCompleted, result.Error)
	}
	if result.Output != wantOutput {
		t.Fatalf("%s output = %q, want %q", stepID, result.Output, wantOutput)
	}
}
