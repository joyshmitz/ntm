package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func TestDispatchLogFormat_AuditContract(t *testing.T) {
	got := formatDispatchLog(dispatchLogOptions{
		Template: "MO-review.md",
		PaneID:   "%2",
		Session:  "audit-session",
		Args: map[string]interface{}{
			"ALPHA": "from-args",
			"BETA":  "two",
		},
		Params: map[string]interface{}{
			"ALPHA": "from-params",
			"GAMMA": 3,
		},
	}, "rendered body")

	for _, want := range []string{
		"=== Dispatch ===\n",
		"MO: MO-review.md\n",
		"Target pane: %2\n",
		"Target session: audit-session\n",
		"Params:\n",
		"  ALPHA=from-params\n",
		"  BETA=two\n",
		"  GAMMA=3\n",
		"=== Rendered ===\n",
		"rendered body\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dispatch log missing %q:\n%s", want, got)
		}
	}
}

func TestWriteDispatchLog_WritesAuditFile(t *testing.T) {
	tmpDir := t.TempDir()
	executor := NewExecutor(ExecutorConfig{ProjectDir: tmpDir, Session: "audit-session"})
	executor.writeDispatchLog("step/one", "body", dispatchLogOptions{
		Template: "MO.md",
		PaneID:   "%1",
		Session:  "audit-session",
		Params:   map[string]interface{}{"KEY": "value"},
	})

	entries, err := os.ReadDir(filepath.Join(tmpDir, "session-logs"))
	if err != nil {
		t.Fatalf("ReadDir(session-logs) returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("dispatch log count = %d, want 1", len(entries))
	}
	name := entries[0].Name()
	if !strings.HasPrefix(name, "dispatch-") || !strings.HasSuffix(name, "-step_one.log") {
		t.Fatalf("dispatch log filename = %q, want dispatch timestamp plus sanitized step id", name)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, "session-logs", name))
	if err != nil {
		t.Fatalf("ReadFile(dispatch log) returned error: %v", err)
	}
	if !strings.Contains(string(content), "MO: MO.md\n") || !strings.Contains(string(content), "  KEY=value\n") {
		t.Fatalf("dispatch log content missing audit fields:\n%s", content)
	}
}

func TestExecuteTemplate_WritesFormattedDispatchLog(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "MO.md"), []byte("Hello <NAME>"), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := NewMockTmuxClient(tmux.Pane{ID: "%1", Index: 1, Type: tmux.AgentCodex})
	cfg := DefaultExecutorConfig("tpl-session")
	cfg.ProjectDir = tmpDir
	executor := NewExecutor(cfg)
	executor.SetTmuxClient(mock)

	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "dispatch-log-workflow",
		Settings:      WorkflowSettings{},
		Steps: []Step{{
			ID:       "render",
			Template: "MO.md",
			Params:   map[string]interface{}{"NAME": "Ada"},
			Pane:     PaneSpec{Index: 1},
			Wait:     WaitNone,
		}},
	}

	state, err := executor.Run(context.Background(), workflow, nil, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if state.Steps["render"].Status != StatusCompleted {
		t.Fatalf("render status = %q, want %q", state.Steps["render"].Status, StatusCompleted)
	}

	content := readOnlyDispatchLog(t, tmpDir)
	for _, want := range []string{
		"MO: MO.md\n",
		"Target pane: %1\n",
		"Target session: tpl-session\n",
		"  NAME=Ada\n",
		"=== Rendered ===\nHello Ada\n",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("dispatch log missing %q:\n%s", want, content)
		}
	}
}

func TestExecuteTemplate_SkipsDispatchLogWhenDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "MO.md"), []byte("Hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := NewMockTmuxClient(tmux.Pane{ID: "%1", Index: 1, Type: tmux.AgentCodex})
	cfg := DefaultExecutorConfig("tpl-session")
	cfg.ProjectDir = tmpDir
	executor := NewExecutor(cfg)
	executor.SetTmuxClient(mock)
	disabled := false

	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "dispatch-log-disabled",
		Settings:      WorkflowSettings{LogDispatch: &disabled},
		Steps: []Step{{
			ID:       "render",
			Template: "MO.md",
			Pane:     PaneSpec{Index: 1},
			Wait:     WaitNone,
		}},
	}

	state, err := executor.Run(context.Background(), workflow, nil, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if state.Steps["render"].Status != StatusCompleted {
		t.Fatalf("render status = %q, want %q", state.Steps["render"].Status, StatusCompleted)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "session-logs")); !os.IsNotExist(err) {
		t.Fatalf("session-logs stat error = %v, want not exist", err)
	}
}

func TestWorkflowSettingsDispatchLoggingEnabled(t *testing.T) {
	if !(WorkflowSettings{}).DispatchLoggingEnabled() {
		t.Fatal("zero-value settings should enable dispatch logging")
	}
	enabled := true
	if !(WorkflowSettings{LogDispatch: &enabled}).DispatchLoggingEnabled() {
		t.Fatal("log_dispatch=true should enable dispatch logging")
	}
	disabled := false
	if (WorkflowSettings{LogDispatch: &disabled}).DispatchLoggingEnabled() {
		t.Fatal("log_dispatch=false should disable dispatch logging")
	}
}

func readOnlyDispatchLog(t *testing.T, projectDir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(projectDir, "session-logs"))
	if err != nil {
		t.Fatalf("ReadDir(session-logs) returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("dispatch log count = %d, want 1", len(entries))
	}
	content, err := os.ReadFile(filepath.Join(projectDir, "session-logs", entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile(dispatch log) returned error: %v", err)
	}
	return string(content)
}
