package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/pipeline"
)

func TestParsePipelineRunVariablesPrecedenceAndJSONTypes(t *testing.T) {
	varFile := filepath.Join(t.TempDir(), "vars.json")
	if err := os.WriteFile(varFile, []byte(`{"foo":"file","n":5,"arr":["x","y"],"flag":false}`), 0o644); err != nil {
		t.Fatalf("write var file: %v", err)
	}

	got, err := parsePipelineRunVariables(varFile, []string{
		"foo=cli",
		"flag=true",
		"items=a,b,c",
	})
	if err != nil {
		t.Fatalf("parsePipelineRunVariables() error = %v", err)
	}

	if got["foo"] != "cli" {
		t.Fatalf("foo = %#v, want CLI override", got["foo"])
	}
	if got["flag"] != "true" {
		t.Fatalf("flag = %#v, want CLI string override", got["flag"])
	}
	if got["n"] != float64(5) {
		t.Fatalf("n = %#v, want JSON float64", got["n"])
	}
	if !reflect.DeepEqual(got["arr"], []interface{}{"x", "y"}) {
		t.Fatalf("arr = %#v, want JSON array", got["arr"])
	}
	if got["items"] != "a,b,c" {
		t.Fatalf("items = %#v, want raw CLI comma string", got["items"])
	}
}

func TestParsePipelineRunVariablesErrors(t *testing.T) {
	if _, err := parsePipelineRunVariables("", []string{"missing_equals"}); err == nil {
		t.Fatal("parsePipelineRunVariables() error = nil, want invalid format error")
	}

	badJSON := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badJSON, []byte(`{"unterminated"`), 0o644); err != nil {
		t.Fatalf("write bad var file: %v", err)
	}
	_, err := parsePipelineRunVariables(badJSON, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to parse var file") {
		t.Fatalf("parsePipelineRunVariables() error = %v, want parse var file error", err)
	}
}

func TestPipelineRunVariablesValidateDeclaredTypes(t *testing.T) {
	varFile := filepath.Join(t.TempDir(), "vars.json")
	if err := os.WriteFile(varFile, []byte(`{"n":5,"arr":["x","y"],"flag":false}`), 0o644); err != nil {
		t.Fatalf("write var file: %v", err)
	}

	vars, err := parsePipelineRunVariables(varFile, []string{
		"flag=true",
		"required=present",
		"extra=available",
	})
	if err != nil {
		t.Fatalf("parsePipelineRunVariables() error = %v", err)
	}

	workflow := &pipeline.Workflow{Vars: map[string]pipeline.VarDef{
		"n":        {Type: pipeline.VarTypeNumber},
		"arr":      {Type: pipeline.VarTypeArray},
		"flag":     {Type: pipeline.VarTypeBoolean},
		"required": {Type: pipeline.VarTypeString, Required: true},
	}}
	result, parseErr := pipeline.ValidateWorkflowVariables(workflow, vars)
	if parseErr != nil {
		t.Fatalf("ValidateWorkflowVariables() error = %v", parseErr)
	}
	if result.Variables["flag"] != true {
		t.Fatalf("flag = %#v, want normalized bool true", result.Variables["flag"])
	}
	if !reflect.DeepEqual(result.Variables["arr"], []interface{}{"x", "y"}) {
		t.Fatalf("arr = %#v, want JSON array", result.Variables["arr"])
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0].Message, "undeclared variable \"extra\"") {
		t.Fatalf("warnings = %#v, want undeclared extra warning", result.Warnings)
	}
}
