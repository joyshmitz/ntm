package pipeline

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestTOMLRoundTrip_NewSchemaTypes(t *testing.T) {
	workflow := Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "toml-new-schema-types",
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

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(workflow); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var got Workflow
	md, err := toml.Decode(buf.String(), &got)
	if err != nil {
		t.Fatalf("Decode() error = %v\nTOML:\n%s", err, buf.String())
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		t.Fatalf("Decode() left undecoded keys: %v\nTOML:\n%s", undecoded, buf.String())
	}

	if !reflect.DeepEqual(got, workflow) {
		t.Fatalf("TOML round trip mismatch\nwant: %#v\n got: %#v\nTOML:\n%s", workflow, got, buf.String())
	}
}

func TestParseStringTOML_NewSchemaTypesCanonicalShape(t *testing.T) {
	var buf bytes.Buffer
	workflow := Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "canonical-toml-shape",
		Steps: []Step{
			{
				ID:     "pane",
				Prompt: "hello",
				Pane:   PaneSpec{Index: 1},
			},
			{
				ID: "foreach",
				Foreach: &ForeachConfig{
					Items:     "${vars.items}",
					Models:    StringOrList{"codex"},
					MaxRounds: IntOrExpr{Value: 2},
					Steps: []Step{
						{ID: "body", Prompt: "body"},
					},
				},
			},
		},
	}
	if err := toml.NewEncoder(&buf).Encode(workflow); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	got, err := ParseString(buf.String(), "toml")
	if err != nil {
		t.Fatalf("ParseString() error = %v\nTOML:\n%s", err, buf.String())
	}
	if got.Steps[0].Pane != (PaneSpec{Index: 1}) {
		t.Fatalf("Pane = %#v, want Index=1", got.Steps[0].Pane)
	}
	if got.Steps[1].Foreach == nil {
		t.Fatal("Foreach = nil, want config")
	}
	if !reflect.DeepEqual(got.Steps[1].Foreach.Models, StringOrList{"codex"}) {
		t.Fatalf("Foreach.Models = %#v, want codex list", got.Steps[1].Foreach.Models)
	}
	if got.Steps[1].Foreach.MaxRounds != (IntOrExpr{Value: 2}) {
		t.Fatalf("Foreach.MaxRounds = %#v, want Value=2", got.Steps[1].Foreach.MaxRounds)
	}
}

func TestParseStringTOML_DocumentsUnsupportedScalarAlternation(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "pane scalar",
			content: `
schema_version = "2.0"
name = "bad-pane"

[[steps]]
id = "pane"
prompt = "hello"
pane = 3
`,
		},
		{
			name: "parallel scalar",
			content: `
schema_version = "2.0"
name = "bad-parallel"

[[steps]]
id = "parallel"
prompt = "hello"
parallel = true
`,
		},
		{
			name: "after scalar",
			content: `
schema_version = "2.0"
name = "bad-after"

[[steps]]
id = "after"
prompt = "hello"
after = "pane"
`,
		},
		{
			name: "notes scalar",
			content: `
schema_version = "2.0"
name = "bad-notes"
notes = "single note"

[[steps]]
id = "step"
prompt = "hello"
`,
		},
		{
			name: "output bare string",
			content: `
schema_version = "2.0"
name = "bad-output"
outputs = ["deliverables/report.md"]

[[steps]]
id = "step"
prompt = "hello"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseString(tt.content, "toml")
			if err == nil {
				t.Fatal("ParseString() error = nil, want scalar alternation parse error")
			}
			if !strings.Contains(err.Error(), "TOML parse error") {
				t.Fatalf("ParseString() error = %v, want TOML parse error", err)
			}
		})
	}
}
