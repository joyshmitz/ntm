package pipeline

import (
	"strings"
	"testing"
)

func TestSubstituteRecursiveReferences(t *testing.T) {
	state := &ExecutionState{
		Variables: map[string]interface{}{
			"x": "${vars.y}",
			"y": "world",
		},
	}
	sub := NewSubstitutor(state, "sess", "wf")

	got, err := sub.Substitute("hello ${vars.x}")
	if err != nil {
		t.Fatalf("Substitute() error = %v", err)
	}
	if got != "hello world" {
		t.Fatalf("Substitute() = %q, want hello world", got)
	}
}

func TestSubstituteRecursionDepthExceeded(t *testing.T) {
	state := &ExecutionState{
		Variables: map[string]interface{}{
			"x": "${vars.x}",
		},
	}
	sub := NewSubstitutor(state, "sess", "wf")

	got, err := sub.Substitute("loop ${vars.x}")
	if err == nil {
		t.Fatalf("Substitute() error = nil, got %q", got)
	}
	if !strings.Contains(err.Error(), "substitution recursion depth exceeded") {
		t.Fatalf("Substitute() error = %v, want recursion depth exceeded", err)
	}
	if got != "loop ${vars.x}" {
		t.Fatalf("Substitute() = %q, want unresolved self-reference", got)
	}
}

func TestSubstituteEscapedVariableReference(t *testing.T) {
	state := &ExecutionState{
		Variables: map[string]interface{}{
			"x": "secret",
		},
	}
	sub := NewSubstitutor(state, "sess", "wf")

	got, err := sub.Substitute(`echo \${vars.x}`)
	if err != nil {
		t.Fatalf("Substitute() error = %v", err)
	}
	if got != "echo ${vars.x}" {
		t.Fatalf("Substitute() = %q, want literal variable reference", got)
	}
}

func TestSubstituteEscapedDollar(t *testing.T) {
	state := &ExecutionState{
		Variables: map[string]interface{}{
			"x": "ok",
		},
	}
	sub := NewSubstitutor(state, "sess", "wf")

	got, err := sub.Substitute(`price \$5 ${vars.x}`)
	if err != nil {
		t.Fatalf("Substitute() error = %v", err)
	}
	if got != "price $5 ok" {
		t.Fatalf("Substitute() = %q, want escaped dollar restored", got)
	}
}

func TestSubstituteEscapedReferenceFromVariableValue(t *testing.T) {
	state := &ExecutionState{
		Variables: map[string]interface{}{
			"x": `\${vars.y}`,
			"y": "must-not-expand",
		},
	}
	sub := NewSubstitutor(state, "sess", "wf")

	got, err := sub.Substitute("${vars.x}")
	if err != nil {
		t.Fatalf("Substitute() error = %v", err)
	}
	if got != "${vars.y}" {
		t.Fatalf("Substitute() = %q, want escaped reference from value preserved", got)
	}
}
