package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/output"
)

func TestResolveAddAgentCommandTemplate_Ollama(t *testing.T) {

	oldCfg := cfg
	defer func() {
		cfg = oldCfg
	}()

	cfg = config.Default()
	cfg.Agents.Ollama = "ollama run {{shellQuote (.Model | default \"codellama:latest\")}}"

	cmd, env, err := resolveAddAgentCommandTemplate(AgentTypeOllama, nil, "http://127.0.0.1:11434")
	if err != nil {
		t.Fatalf("resolveAddAgentCommandTemplate() error = %v", err)
	}
	if cmd != cfg.Agents.Ollama {
		t.Fatalf("resolveAddAgentCommandTemplate() cmd = %q, want %q", cmd, cfg.Agents.Ollama)
	}
	if env["OLLAMA_HOST"] != "http://127.0.0.1:11434" {
		t.Fatalf("resolveAddAgentCommandTemplate() env OLLAMA_HOST = %q", env["OLLAMA_HOST"])
	}
}

func TestNewAddCmd_RegistersOllamaFlag(t *testing.T) {

	cmd := newAddCmd()
	if cmd.Flags().Lookup("ollama") == nil {
		t.Fatal("expected add command to register --ollama")
	}
}

func TestAddResponseJSONIncludesOllama(t *testing.T) {

	data, err := json.Marshal(output.AddResponse{
		AddedClaude: 1,
		AddedOllama: 2,
		TotalAdded:  3,
	})
	if err != nil {
		t.Fatalf("json.Marshal(AddResponse) error = %v", err)
	}

	encoded := string(data)
	if !strings.Contains(encoded, "\"added_ollama\":2") {
		t.Fatalf("AddResponse JSON = %s, want added_ollama field", encoded)
	}
}
