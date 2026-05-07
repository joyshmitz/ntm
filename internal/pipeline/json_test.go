package pipeline

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestJSONRoundTrip_NewSchemaTypes(t *testing.T) {
	workflow := Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "json-new-schema-types",
		Notes:         StringOrList{"operator note", "second note"},
		Defaults: map[string]interface{}{
			"triage_pane": "${vars.default_pane}",
		},
		Outputs: []OutputDecl{
			{Name: "report", Description: "Final report", Path: "deliverables/report.md"},
		},
		Settings: WorkflowSettings{
			Timeout:          Duration{Duration: 2 * time.Minute},
			OnError:          ErrorActionContinue,
			NotifyOnComplete: true,
			NotifyOnError:    true,
			NotifyChannels:   []string{"desktop", "mail"},
			MailRecipient:    "operator",
		},
		Steps: []Step{
			{
				ID:     "pane-index",
				Agent:  "codex",
				Pane:   PaneSpec{Index: 2},
				Prompt: "literal pane",
			},
			{
				ID:        "pane-expr",
				Agent:     "claude",
				Pane:      PaneSpec{Expr: "${defaults.triage_pane}"},
				Prompt:    "expr pane",
				DependsOn: []string{"pane-index"},
				After:     AfterRef{"bootstrap", "warm-cache"},
			},
			{
				ID:     "parallel-flag",
				Prompt: "fan out foreach body",
				Parallel: ParallelSpec{
					Flag: true,
				},
			},
			{
				ID: "parallel-steps",
				Parallel: ParallelSpec{
					Steps: []Step{
						{ID: "branch-a", Prompt: "A"},
						{ID: "branch-b", Command: "printf B"},
					},
				},
			},
			{
				ID:      "on-failure-retry",
				Command: "false",
				OnFailure: OnFailureSpec{
					Action:     "retry",
					RetryCount: 3,
				},
			},
			{
				ID:      "on-failure-fallback",
				Command: "false",
				OnFailure: OnFailureSpec{
					Fallback: map[string]interface{}{
						"pane":     "${defaults.triage_pane}",
						"template": "recovery.md",
					},
				},
			},
			{
				ID: "loop-limit-value",
				Loop: &LoopConfig{
					Items:         "${vars.items}",
					As:            "item",
					MaxIterations: IntOrExpr{Value: 10},
					Steps: []Step{
						{ID: "loop-body", Prompt: "process ${item}"},
					},
				},
			},
			{
				ID: "foreach-config",
				Foreach: &ForeachConfig{
					Items:         "${vars.files}",
					As:            "file",
					Models:        StringOrList{"codex", "gemini"},
					MaxRounds:     IntOrExpr{Expr: "${defaults.max_rounds}"},
					Filter:        "state==active",
					PaneStrategy:  "round_robin",
					Template:      "review.md",
					Params:        map[string]interface{}{"severity": "high"},
					Parallel:      true,
					MaxConcurrent: 4,
					Steps: []Step{
						{ID: "foreach-body", Prompt: "review ${file}"},
					},
				},
			},
		},
	}

	data, err := json.Marshal(workflow)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got Workflow
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v\nJSON:\n%s", err, data)
	}

	if !reflect.DeepEqual(got, workflow) {
		t.Fatalf("JSON round trip mismatch\nwant: %#v\n got: %#v\nJSON:\n%s", workflow, got, data)
	}
}

func TestJSON_DocumentsUnsupportedScalarAlternation(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "pane scalar",
			content: `{
				"schema_version": "2.0",
				"name": "bad-pane",
				"steps": [{"id": "pane", "prompt": "hello", "pane": 3}]
			}`,
		},
		{
			name: "parallel scalar",
			content: `{
				"schema_version": "2.0",
				"name": "bad-parallel",
				"steps": [{"id": "parallel", "prompt": "hello", "parallel": true}]
			}`,
		},
		{
			name: "after scalar",
			content: `{
				"schema_version": "2.0",
				"name": "bad-after",
				"steps": [{"id": "after", "prompt": "hello", "after": "pane"}]
			}`,
		},
		{
			name: "notes scalar",
			content: `{
				"schema_version": "2.0",
				"name": "bad-notes",
				"notes": "single note",
				"steps": [{"id": "step", "prompt": "hello"}]
			}`,
		},
		{
			name: "output bare string",
			content: `{
				"schema_version": "2.0",
				"name": "bad-output",
				"outputs": ["deliverables/report.md"],
				"steps": [{"id": "step", "prompt": "hello"}]
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Workflow
			err := json.Unmarshal([]byte(tt.content), &got)
			if err == nil {
				t.Fatal("Unmarshal() error = nil, want scalar alternation parse error")
			}
			if !strings.Contains(err.Error(), "cannot unmarshal") {
				t.Fatalf("Unmarshal() error = %v, want JSON type error", err)
			}
		})
	}
}

func TestJSONExecutionState_StableShape(t *testing.T) {
	started := time.Date(2026, 5, 7, 6, 0, 0, 0, time.UTC)
	finished := started.Add(2 * time.Second)
	state := ExecutionState{
		RunID:      "run-20260507-060000-abcd",
		WorkflowID: "json-state",
		Status:     StatusCompleted,
		StartedAt:  started,
		UpdatedAt:  finished,
		Steps: map[string]StepResult{
			"step-1": {
				StepID:     "step-1",
				Status:     StatusCompleted,
				StartedAt:  started,
				FinishedAt: finished,
				PaneUsed:   "%1",
				AgentType:  "codex",
				Output:     "done",
			},
		},
		Variables: map[string]interface{}{
			"answer": "42",
		},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got ExecutionState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v\nJSON:\n%s", err, data)
	}
	if !reflect.DeepEqual(got, state) {
		t.Fatalf("ExecutionState JSON mismatch\nwant: %#v\n got: %#v\nJSON:\n%s", state, got, data)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal(raw) error = %v", err)
	}
	for _, key := range []string{"run_id", "workflow_id", "status", "started_at", "updated_at", "steps", "variables"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("ExecutionState JSON missing key %q in %s", key, data)
		}
	}
}
