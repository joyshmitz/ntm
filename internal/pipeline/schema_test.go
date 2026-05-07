package pipeline

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDuration_UnmarshalText(t *testing.T) {

	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{
			name:  "seconds",
			input: "30s",
			want:  30 * time.Second,
		},
		{
			name:  "minutes",
			input: "5m",
			want:  5 * time.Minute,
		},
		{
			name:  "hours",
			input: "2h",
			want:  2 * time.Hour,
		},
		{
			name:  "combined",
			input: "1h30m45s",
			want:  1*time.Hour + 30*time.Minute + 45*time.Second,
		},
		{
			name:  "milliseconds",
			input: "500ms",
			want:  500 * time.Millisecond,
		},
		{
			name:  "zero",
			input: "0s",
			want:  0,
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
		{
			// Bare-integer form is now accepted as seconds (deliberate
			// extension; matches author convenience in YAML where
			// `timeout: 300` is a frequent shorthand). Use "invalid"
			// above for the negative-format test.
			name:  "bare integer means seconds",
			input: "30",
			want:  30 * time.Second,
		},
		{
			name:  "empty string parses to zero",
			input: "",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := d.UnmarshalText([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Errorf("UnmarshalText(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("UnmarshalText(%q) unexpected error: %v", tt.input, err)
				return
			}
			if d.Duration != tt.want {
				t.Errorf("UnmarshalText(%q) = %v, want %v", tt.input, d.Duration, tt.want)
			}
		})
	}
}

func TestDuration_MarshalText(t *testing.T) {

	tests := []struct {
		name string
		d    Duration
		want string
	}{
		{
			name: "seconds",
			d:    Duration{Duration: 30 * time.Second},
			want: "30s",
		},
		{
			name: "minutes",
			d:    Duration{Duration: 5 * time.Minute},
			want: "5m0s",
		},
		{
			name: "hours",
			d:    Duration{Duration: 2 * time.Hour},
			want: "2h0m0s",
		},
		{
			name: "combined",
			d:    Duration{Duration: 1*time.Hour + 30*time.Minute + 45*time.Second},
			want: "1h30m45s",
		},
		{
			name: "zero",
			d:    Duration{Duration: 0},
			want: "0s",
		},
		{
			name: "milliseconds",
			d:    Duration{Duration: 500 * time.Millisecond},
			want: "500ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.d.MarshalText()
			if err != nil {
				t.Errorf("MarshalText() unexpected error: %v", err)
				return
			}
			if string(got) != tt.want {
				t.Errorf("MarshalText() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestDuration_RoundTrip(t *testing.T) {

	durations := []time.Duration{
		0,
		time.Second,
		5 * time.Minute,
		2*time.Hour + 30*time.Minute,
		time.Hour,
	}

	for _, original := range durations {
		d := Duration{Duration: original}
		marshaled, err := d.MarshalText()
		if err != nil {
			t.Errorf("MarshalText(%v) unexpected error: %v", original, err)
			continue
		}

		var unmarshaled Duration
		if err := unmarshaled.UnmarshalText(marshaled); err != nil {
			t.Errorf("UnmarshalText(%q) unexpected error: %v", string(marshaled), err)
			continue
		}

		if unmarshaled.Duration != original {
			t.Errorf("RoundTrip(%v) = %v, want %v", original, unmarshaled.Duration, original)
		}
	}
}

func TestOutputParse_UnmarshalText(t *testing.T) {

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "json",
			input: "json",
			want:  "json",
		},
		{
			name:  "yaml",
			input: "yaml",
			want:  "yaml",
		},
		{
			name:  "lines",
			input: "lines",
			want:  "lines",
		},
		{
			name:  "first_line",
			input: "first_line",
			want:  "first_line",
		},
		{
			name:  "regex",
			input: "regex",
			want:  "regex",
		},
		{
			name:  "none",
			input: "none",
			want:  "none",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var o OutputParse
			err := o.UnmarshalText([]byte(tt.input))
			if err != nil {
				t.Errorf("UnmarshalText(%q) unexpected error: %v", tt.input, err)
				return
			}
			if o.Type != tt.want {
				t.Errorf("UnmarshalText(%q).Type = %q, want %q", tt.input, o.Type, tt.want)
			}
		})
	}
}

func TestDefaultStepTimeout(t *testing.T) {

	d := DefaultStepTimeout()
	expected := 5 * time.Minute

	if d.Duration != expected {
		t.Errorf("DefaultStepTimeout() = %v, want %v", d.Duration, expected)
	}
}

func TestDefaultWorkflowSettings(t *testing.T) {

	s := DefaultWorkflowSettings()

	if s.Timeout.Duration != 30*time.Minute {
		t.Errorf("DefaultWorkflowSettings().Timeout = %v, want 30m", s.Timeout.Duration)
	}

	if s.OnError != ErrorActionFail {
		t.Errorf("DefaultWorkflowSettings().OnError = %q, want %q", s.OnError, ErrorActionFail)
	}

	if s.NotifyOnComplete {
		t.Error("DefaultWorkflowSettings().NotifyOnComplete = true, want false")
	}

	if !s.NotifyOnError {
		t.Error("DefaultWorkflowSettings().NotifyOnError = false, want true")
	}
}

func TestPaneSpec_UnmarshalYAML(t *testing.T) {
	type paneDoc struct {
		Pane PaneSpec `yaml:"pane,omitempty"`
	}

	tests := []struct {
		name     string
		input    string
		want     PaneSpec
		wantZero bool
	}{
		{
			name:  "literal int",
			input: "pane: 3\n",
			want:  PaneSpec{Index: 3},
		},
		{
			name:     "zero is unset",
			input:    "pane: 0\n",
			want:     PaneSpec{},
			wantZero: true,
		},
		{
			name:  "template expression",
			input: "pane: '${defaults.triage_pane}'\n",
			want:  PaneSpec{Expr: "${defaults.triage_pane}"},
		},
		{
			name:  "literal string expression",
			input: "pane: literal-string\n",
			want:  PaneSpec{Expr: "literal-string"},
		},
		{
			name:     "empty string is unset",
			input:    "pane: ''\n",
			want:     PaneSpec{},
			wantZero: true,
		},
		{
			name:     "null is unset",
			input:    "pane: null\n",
			want:     PaneSpec{},
			wantZero: true,
		},
		{
			name:     "missing is unset",
			input:    "{}\n",
			want:     PaneSpec{},
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got paneDoc
			if err := yaml.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("yaml.Unmarshal() error = %v", err)
			}
			if got.Pane != tt.want {
				t.Fatalf("Pane = %+v, want %+v", got.Pane, tt.want)
			}
			if got.Pane.IsZero() != tt.wantZero {
				t.Fatalf("Pane.IsZero() = %v, want %v", got.Pane.IsZero(), tt.wantZero)
			}
		})
	}
}

func TestPaneSpec_MarshalYAML(t *testing.T) {
	type paneDoc struct {
		Pane PaneSpec `yaml:"pane,omitempty"`
	}

	tests := []struct {
		name      string
		pane      PaneSpec
		wantValue interface{}
		wantOmit  bool
	}{
		{
			name:      "literal int",
			pane:      PaneSpec{Index: 2},
			wantValue: 2,
		},
		{
			name:      "template expression",
			pane:      PaneSpec{Expr: "${foo}"},
			wantValue: "${foo}",
		},
		{
			name:     "zero is omitted",
			pane:     PaneSpec{},
			wantOmit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := yaml.Marshal(paneDoc{Pane: tt.pane})
			if err != nil {
				t.Fatalf("yaml.Marshal() error = %v", err)
			}
			if tt.wantOmit {
				if strings.Contains(string(out), "pane:") {
					t.Fatalf("yaml.Marshal() = %q, want pane omitted", string(out))
				}
				return
			}

			var raw map[string]interface{}
			if err := yaml.Unmarshal(out, &raw); err != nil {
				t.Fatalf("yaml.Unmarshal(raw) error = %v", err)
			}
			if raw["pane"] != tt.wantValue {
				t.Fatalf("marshaled pane = %#v, want %#v; yaml = %q", raw["pane"], tt.wantValue, string(out))
			}

			var roundTrip paneDoc
			if err := yaml.Unmarshal(out, &roundTrip); err != nil {
				t.Fatalf("yaml.Unmarshal(roundTrip) error = %v", err)
			}
			if roundTrip.Pane != tt.pane {
				t.Fatalf("roundTrip.Pane = %+v, want %+v; yaml = %q", roundTrip.Pane, tt.pane, string(out))
			}
		})
	}
}

func TestParallelSpec_UnmarshalYAML(t *testing.T) {
	type doc struct {
		Parallel ParallelSpec `yaml:"parallel"`
	}

	tests := []struct {
		name      string
		yaml      string
		wantFlag  bool
		wantSteps int
		wantZero  bool
		wantErr   bool
	}{
		{
			name:     "bool true",
			yaml:     "parallel: true",
			wantFlag: true,
			wantZero: false,
		},
		{
			name:     "bool false",
			yaml:     "parallel: false",
			wantFlag: false,
			wantZero: true,
		},
		{
			name:      "list of steps",
			yaml:      "parallel:\n  - id: a\n    prompt: do A\n  - id: b\n    prompt: do B",
			wantSteps: 2,
			wantZero:  false,
		},
		{
			name:     "empty list",
			yaml:     "parallel: []",
			wantZero: true,
		},
		{
			name:     "missing",
			yaml:     "other: value",
			wantZero: true,
		},
		{
			name:    "invalid type",
			yaml:    "parallel: not-a-bool-or-list",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d doc
			err := yaml.Unmarshal([]byte(tt.yaml), &d)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Parallel.Flag != tt.wantFlag {
				t.Errorf("Flag = %v, want %v", d.Parallel.Flag, tt.wantFlag)
			}
			if len(d.Parallel.Steps) != tt.wantSteps {
				t.Errorf("len(Steps) = %d, want %d", len(d.Parallel.Steps), tt.wantSteps)
			}
			if d.Parallel.IsZero() != tt.wantZero {
				t.Errorf("IsZero() = %v, want %v", d.Parallel.IsZero(), tt.wantZero)
			}
		})
	}
}

func TestParallelSpec_MarshalYAML(t *testing.T) {
	type doc struct {
		Parallel ParallelSpec `yaml:"parallel,omitempty"`
	}

	tests := []struct {
		name       string
		spec       ParallelSpec
		wantInYAML string
	}{
		{
			name:       "flag true marshals as bool",
			spec:       ParallelSpec{Flag: true},
			wantInYAML: "parallel: true",
		},
		{
			name:       "steps marshal as list",
			spec:       ParallelSpec{Steps: []Step{{ID: "a"}, {ID: "b"}}},
			wantInYAML: "- id: a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := doc{Parallel: tt.spec}
			out, err := yaml.Marshal(d)
			if err != nil {
				t.Fatalf("yaml.Marshal error: %v", err)
			}
			if !strings.Contains(string(out), tt.wantInYAML) {
				t.Errorf("output = %q, want to contain %q", string(out), tt.wantInYAML)
			}
		})
	}
}

func TestParallelSpec_IsZero(t *testing.T) {
	tests := []struct {
		name string
		spec ParallelSpec
		want bool
	}{
		{"default", ParallelSpec{}, true},
		{"flag false only", ParallelSpec{Flag: false}, true},
		{"flag true", ParallelSpec{Flag: true}, false},
		{"has steps", ParallelSpec{Steps: []Step{{ID: "x"}}}, false},
		{"flag true and steps", ParallelSpec{Flag: true, Steps: []Step{{ID: "x"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParallelSpec_Len(t *testing.T) {
	flagOnly := ParallelSpec{Flag: true}
	if flagOnly.Len() != 0 {
		t.Error("Flag-only Len should be 0")
	}
	withSteps := ParallelSpec{Steps: []Step{{ID: "a"}, {ID: "b"}}}
	if withSteps.Len() != 2 {
		t.Error("two-step Len should be 2")
	}
}

func TestParallelSpec_RoundTrip(t *testing.T) {
	type doc struct {
		Parallel ParallelSpec `yaml:"parallel"`
	}

	origFlag := doc{Parallel: ParallelSpec{Flag: true}}
	out, err := yaml.Marshal(origFlag)
	if err != nil {
		t.Fatal(err)
	}
	var rtFlag doc
	if err := yaml.Unmarshal(out, &rtFlag); err != nil {
		t.Fatal(err)
	}
	if !rtFlag.Parallel.Flag {
		t.Error("round-trip flag=true lost")
	}

	origSteps := doc{Parallel: ParallelSpec{Steps: []Step{{ID: "x", Prompt: "do X"}}}}
	out, err = yaml.Marshal(origSteps)
	if err != nil {
		t.Fatal(err)
	}
	var rtSteps doc
	if err := yaml.Unmarshal(out, &rtSteps); err != nil {
		t.Fatal(err)
	}
	if len(rtSteps.Parallel.Steps) != 1 || rtSteps.Parallel.Steps[0].ID != "x" {
		t.Errorf("round-trip steps lost: %+v", rtSteps.Parallel)
	}
}

func TestOnFailureSpec_UnmarshalYAML(t *testing.T) {
	type doc struct {
		OnFailure OnFailureSpec `yaml:"on_failure"`
	}

	tests := []struct {
		name       string
		yaml       string
		wantAction string
		wantRetry  int
		wantFB     bool
		wantZero   bool
		wantErr    bool
	}{
		{
			name:       "continue string",
			yaml:       "on_failure: continue",
			wantAction: "continue",
		},
		{
			name:       "retry:3",
			yaml:       "on_failure: 'retry:3'",
			wantAction: "retry",
			wantRetry:  3,
		},
		{
			name:       "retry:abc (unparseable count)",
			yaml:       "on_failure: 'retry:abc'",
			wantAction: "retry:abc",
			wantRetry:  0,
		},
		{
			name:   "structured fallback",
			yaml:   "on_failure:\n  pane: 1\n  template: foo.md",
			wantFB: true,
		},
		{
			name:       "fallback_to_ntm_inbox",
			yaml:       "on_failure: fallback_to_ntm_inbox",
			wantAction: "fallback_to_ntm_inbox",
		},
		{
			name:     "missing",
			yaml:     "other: value",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d doc
			err := yaml.Unmarshal([]byte(tt.yaml), &d)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.OnFailure.Action != tt.wantAction {
				t.Errorf("Action = %q, want %q", d.OnFailure.Action, tt.wantAction)
			}
			if d.OnFailure.RetryCount != tt.wantRetry {
				t.Errorf("RetryCount = %d, want %d", d.OnFailure.RetryCount, tt.wantRetry)
			}
			if tt.wantFB && len(d.OnFailure.Fallback) == 0 {
				t.Error("expected Fallback to be non-empty")
			}
			if d.OnFailure.IsZero() != tt.wantZero {
				t.Errorf("IsZero() = %v, want %v", d.OnFailure.IsZero(), tt.wantZero)
			}
		})
	}
}

func TestOnFailureSpec_MarshalYAML(t *testing.T) {
	tests := []struct {
		name string
		spec OnFailureSpec
		want string
	}{
		{
			name: "continue",
			spec: OnFailureSpec{Action: "continue"},
			want: "continue",
		},
		{
			name: "retry with count",
			spec: OnFailureSpec{Action: "retry", RetryCount: 3},
			want: "retry:3",
		},
		{
			name: "fallback map",
			spec: OnFailureSpec{Fallback: map[string]interface{}{"pane": 1}},
			want: "pane:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := yaml.Marshal(tt.spec)
			if err != nil {
				t.Fatalf("yaml.Marshal error: %v", err)
			}
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("output = %q, want to contain %q", string(out), tt.want)
			}
		})
	}
}

func TestOnFailureSpec_IsZero(t *testing.T) {
	tests := []struct {
		name string
		spec OnFailureSpec
		want bool
	}{
		{"empty", OnFailureSpec{}, true},
		{"action only", OnFailureSpec{Action: "continue"}, false},
		{"retry count only", OnFailureSpec{RetryCount: 3}, false},
		{"fallback only", OnFailureSpec{Fallback: map[string]interface{}{"pane": 1}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOnFailureSpec_RoundTrip(t *testing.T) {
	type doc struct {
		OnFailure OnFailureSpec `yaml:"on_failure"`
	}

	orig := doc{OnFailure: OnFailureSpec{Action: "retry", RetryCount: 5}}
	out, err := yaml.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var rt doc
	if err := yaml.Unmarshal(out, &rt); err != nil {
		t.Fatal(err)
	}
	if rt.OnFailure.Action != "retry" || rt.OnFailure.RetryCount != 5 {
		t.Errorf("round-trip failed: %+v", rt.OnFailure)
	}
}

func TestIntOrExpr_UnmarshalYAML(t *testing.T) {
	type doc struct {
		Val IntOrExpr `yaml:"val"`
	}

	tests := []struct {
		name     string
		yaml     string
		wantVal  int
		wantExpr string
		wantZero bool
		wantErr  bool
	}{
		{
			name:    "literal int 6",
			yaml:    "val: 6",
			wantVal: 6,
		},
		{
			name:     "template expression",
			yaml:     "val: '${defaults.foo}'",
			wantExpr: "${defaults.foo}",
		},
		{
			name:     "zero is unset",
			yaml:     "val: 0",
			wantZero: true,
		},
		{
			name:    "negative",
			yaml:    "val: -1",
			wantVal: -1,
		},
		{
			name:     "missing",
			yaml:     "other: 1",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d doc
			err := yaml.Unmarshal([]byte(tt.yaml), &d)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Val.Value != tt.wantVal {
				t.Errorf("Value = %d, want %d", d.Val.Value, tt.wantVal)
			}
			if d.Val.Expr != tt.wantExpr {
				t.Errorf("Expr = %q, want %q", d.Val.Expr, tt.wantExpr)
			}
			if d.Val.IsZero() != tt.wantZero {
				t.Errorf("IsZero() = %v, want %v", d.Val.IsZero(), tt.wantZero)
			}
		})
	}
}

func TestIntOrExpr_MarshalYAML(t *testing.T) {
	tests := []struct {
		name string
		spec IntOrExpr
		want string
	}{
		{
			name: "int 6",
			spec: IntOrExpr{Value: 6},
			want: "6",
		},
		{
			name: "expr",
			spec: IntOrExpr{Expr: "${defaults.foo}"},
			want: "${defaults.foo}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := yaml.Marshal(tt.spec)
			if err != nil {
				t.Fatalf("yaml.Marshal error: %v", err)
			}
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("output = %q, want to contain %q", string(out), tt.want)
			}
		})
	}
}

func TestIntOrExpr_IsZero(t *testing.T) {
	tests := []struct {
		name string
		spec IntOrExpr
		want bool
	}{
		{"default", IntOrExpr{}, true},
		{"zero value", IntOrExpr{Value: 0}, true},
		{"non-zero value", IntOrExpr{Value: 5}, false},
		{"has expr", IntOrExpr{Expr: "x"}, false},
		{"negative", IntOrExpr{Value: -1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}
