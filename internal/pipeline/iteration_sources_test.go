package pipeline

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestResolveBeads_StructuredFormResolvesIDs(t *testing.T) {
	const fixture = `{
  "issues": [
    {"id": "bd-h001", "title": "First", "description": "d1", "labels": ["hypothesis"], "status": "active"},
    {"id": "bd-h002", "title": "Second", "description": "d2", "labels": ["hypothesis"], "status": "active"}
  ]
}`

	var capturedArgs []string
	r := &IterationSourceResolver{
		RunBr: func(_ context.Context, args []string) ([]byte, error) {
			capturedArgs = args
			return []byte(fixture), nil
		},
	}

	got, err := r.ResolveBeads(context.Background(), "hypothesis,state:active")
	if err != nil {
		t.Fatalf("ResolveBeads: %v", err)
	}
	wantArgs := []string{"list", "--json", "--label", "hypothesis", "--status", "active"}
	if !reflect.DeepEqual(capturedArgs, wantArgs) {
		t.Fatalf("br args = %v, want %v", capturedArgs, wantArgs)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2", len(got))
	}
	wantIDs := []string{"bd-h001", "bd-h002"}
	for i, item := range got {
		m, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("item %d is %T, want map", i, item)
		}
		if m["id"] != wantIDs[i] {
			t.Errorf("item %d id = %v, want %s", i, m["id"], wantIDs[i])
		}
		if _, ok := m["title"]; !ok {
			t.Errorf("item %d missing title field", i)
		}
		if _, ok := m["status"]; !ok {
			t.Errorf("item %d missing status field", i)
		}
	}
}

func TestResolveBeads_ShellFormParsesLineDelimitedIDs(t *testing.T) {
	r := &IterationSourceResolver{
		RunShell: func(_ context.Context, cmd string) ([]byte, error) {
			if !strings.Contains(cmd, "br list") {
				t.Fatalf("unexpected shell cmd: %q", cmd)
			}
			return []byte("bd-foo\nbd-bar\n\nbd-baz\n"), nil
		},
	}

	got, err := r.ResolveBeads(context.Background(), "$(br list --label=hypothesis --status=open --json | jq -r '.issues[].id')")
	if err != nil {
		t.Fatalf("ResolveBeads: %v", err)
	}
	want := []interface{}{"bd-foo", "bd-bar", "bd-baz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveBeads_ShellFormParsesJSONArray(t *testing.T) {
	r := &IterationSourceResolver{
		RunShell: func(_ context.Context, _ string) ([]byte, error) {
			return []byte(`["bd-a","bd-b","bd-c"]`), nil
		},
	}

	got, err := r.ResolveBeads(context.Background(), "$(br list --json | jq -c '.issues|map(.id)')")
	if err != nil {
		t.Fatalf("ResolveBeads: %v", err)
	}
	want := []interface{}{"bd-a", "bd-b", "bd-c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveBeads_ShellFormStripsQuotes(t *testing.T) {
	// `jq '.issues[].id'` (without -r) emits quoted strings, one per line.
	r := &IterationSourceResolver{
		RunShell: func(_ context.Context, _ string) ([]byte, error) {
			return []byte(`"bd-q1"` + "\n" + `"bd-q2"` + "\n"), nil
		},
	}

	got, err := r.ResolveBeads(context.Background(), `$(br list --json | jq '.issues[].id')`)
	if err != nil {
		t.Fatalf("ResolveBeads: %v", err)
	}
	want := []interface{}{"bd-q1", "bd-q2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveBeads_EmptyResultIsNotError(t *testing.T) {
	cases := map[string]*IterationSourceResolver{
		"shell": {
			RunShell: func(context.Context, string) ([]byte, error) { return []byte(""), nil },
		},
		"structured": {
			RunBr: func(context.Context, []string) ([]byte, error) { return []byte(`{"issues":[]}`), nil },
		},
		"structured-empty-stdout": {
			RunBr: func(context.Context, []string) ([]byte, error) { return []byte(""), nil },
		},
	}
	exprs := map[string]string{
		"shell":                   "$(true)",
		"structured":              "hypothesis,state:active",
		"structured-empty-stdout": "label:hypothesis",
	}

	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := r.ResolveBeads(context.Background(), exprs[name])
			if err != nil {
				t.Fatalf("ResolveBeads: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("got %d items, want 0", len(got))
			}
		})
	}
}

func TestResolveBeads_EmptyExpressionShortCircuits(t *testing.T) {
	r := &IterationSourceResolver{
		RunShell: func(context.Context, string) ([]byte, error) {
			t.Fatal("shell runner must not be called for empty expr")
			return nil, nil
		},
		RunBr: func(context.Context, []string) ([]byte, error) {
			t.Fatal("br runner must not be called for empty expr")
			return nil, nil
		},
	}
	got, err := r.ResolveBeads(context.Background(), "  ")
	if err != nil {
		t.Fatalf("ResolveBeads: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d items, want 0", len(got))
	}
}

func TestResolveBeads_ShellErrorPropagates(t *testing.T) {
	want := errors.New("nonzero exit")
	r := &IterationSourceResolver{
		RunShell: func(context.Context, string) ([]byte, error) { return nil, want },
	}
	_, err := r.ResolveBeads(context.Background(), "$(false)")
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrap of %v", err, want)
	}
}

func TestResolveBeads_BrErrorPropagates(t *testing.T) {
	want := errors.New("br: not found")
	r := &IterationSourceResolver{
		RunBr: func(context.Context, []string) ([]byte, error) { return nil, want },
	}
	_, err := r.ResolveBeads(context.Background(), "hypothesis")
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrap of %v", err, want)
	}
}

func TestResolveBeads_BrJSONParseError(t *testing.T) {
	r := &IterationSourceResolver{
		RunBr: func(context.Context, []string) ([]byte, error) { return []byte("not-json"), nil },
	}
	_, err := r.ResolveBeads(context.Background(), "hypothesis")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse br --json output") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseStructuredBeadsQuery_Translations(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want []string
	}{
		{
			name: "single label",
			expr: "hypothesis",
			want: []string{"list", "--json", "--label", "hypothesis"},
		},
		{
			name: "label and status alias",
			expr: "hypothesis,state:active",
			want: []string{"list", "--json", "--label", "hypothesis", "--status", "active"},
		},
		{
			name: "explicit label key",
			expr: "label:foo,status:open",
			want: []string{"list", "--json", "--label", "foo", "--status", "open"},
		},
		{
			name: "type/priority/assignee",
			expr: "type:bug,priority:1,assignee:alice",
			want: []string{"list", "--json", "--type", "bug", "--priority", "1", "--assignee", "alice"},
		},
		{
			name: "skips empty terms",
			expr: ",hypothesis,,state:open,",
			want: []string{"list", "--json", "--label", "hypothesis", "--status", "open"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStructuredBeadsQuery(tc.expr)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseStructuredBeadsQuery_RejectsUnknownKeys(t *testing.T) {
	_, err := parseStructuredBeadsQuery("foo:bar")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "unsupported filter key") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseStructuredBeadsQuery_RejectsEmptyValue(t *testing.T) {
	_, err := parseStructuredBeadsQuery("status:")
	if err == nil {
		t.Fatal("expected error for empty value")
	}
}

func TestStripShellInvocation(t *testing.T) {
	cases := map[string]struct {
		in     string
		want   string
		wantOk bool
	}{
		"shell":        {"$(echo foo)", "echo foo", true},
		"empty-shell":  {"$()", "", true},
		"plain":        {"hypothesis", "", false},
		"missing-open": {"echo foo)", "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := stripShellInvocation(tc.in)
			if ok != tc.wantOk || got != tc.want {
				t.Fatalf("got (%q,%v), want (%q,%v)", got, ok, tc.want, tc.wantOk)
			}
		})
	}
}
