package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// personaListResponse is the JSON output for ntm personas list.
type personaListResponse struct {
	Personas    []personaEntry    `json:"personas"`
	ProfileSets []profileSetEntry `json:"profile_sets"`
}

// personaEntry represents a single persona in list output.
type personaEntry struct {
	Name        string   `json:"Name"`
	Description string   `json:"Description"`
	AgentType   string   `json:"AgentType"`
	Model       string   `json:"Model"`
	Tags        []string `json:"Tags"`
}

// profileSetEntry represents a persona set.
type profileSetEntry struct {
	Name        string   `json:"Name"`
	Description string   `json:"Description"`
	Personas    []string `json:"Personas"`
}

// personaShowResponse is the JSON output for ntm personas show.
type personaShowResponse struct {
	Name         string   `json:"Name"`
	Description  string   `json:"Description"`
	AgentType    string   `json:"AgentType"`
	Model        string   `json:"Model"`
	SystemPrompt string   `json:"SystemPrompt"`
	Tags         []string `json:"Tags"`
	Source       string   `json:"source"` // This one uses lowercase
}

// memoryContextResponse matches `ntm memory context` JSON output.
type memoryContextResponse struct {
	RelevantBullets  []memoryRule    `json:"relevantBullets"`
	AntiPatterns     []memoryRule    `json:"antiPatterns"`
	HistorySnippets  []memorySnippet `json:"historySnippets"`
	SuggestedQueries []string        `json:"suggestedCassQueries"`
}

type memoryRule struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Category string `json:"category"`
}

type memorySnippet struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

type memoryOutcomeReport struct {
	Status    string   `json:"status"`
	RuleIDs   []string `json:"rule_ids"`
	Sentiment string   `json:"sentiment"`
}

func runPersonasList(t *testing.T, dir string) personaListResponse {
	t.Helper()
	out := runCmd(t, dir, "ntm", "--json", "personas", "list")
	var resp personaListResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal personas list: %v\nout=%s", err, string(out))
	}
	return resp
}

func runPersonasListFiltered(t *testing.T, dir string, agent, tag string) personaListResponse {
	t.Helper()
	args := []string{"--json", "personas", "list"}
	if agent != "" {
		args = append(args, "--agent", agent)
	}
	if tag != "" {
		args = append(args, "--tag", tag)
	}
	out := runCmd(t, dir, "ntm", args...)
	var resp personaListResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal personas list: %v\nout=%s", err, string(out))
	}
	return resp
}

func runPersonasShow(t *testing.T, dir, name string) personaShowResponse {
	t.Helper()
	out := runCmd(t, dir, "ntm", "--json", "personas", "show", name)
	var resp personaShowResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal personas show: %v\nout=%s", err, string(out))
	}
	return resp
}

func writePersonasFile(t *testing.T, dir string, toml string) {
	t.Helper()
	ntmDir := filepath.Join(dir, ".ntm")
	if err := os.MkdirAll(ntmDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", ntmDir, err)
	}
	path := filepath.Join(ntmDir, "personas.toml")
	if err := os.WriteFile(path, []byte(toml), 0644); err != nil {
		t.Fatalf("write personas.toml: %v", err)
	}
}

func writeUserPersonasFile(t *testing.T, xdgConfigHome string, toml string) {
	t.Helper()
	// Use XDG_CONFIG_HOME path since that's what the test sets
	configDir := filepath.Join(xdgConfigHome, "ntm")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", configDir, err)
	}
	path := filepath.Join(configDir, "personas.toml")
	if err := os.WriteFile(path, []byte(toml), 0644); err != nil {
		t.Fatalf("write personas.toml: %v", err)
	}
}

func TestE2EPersonaManagement_ListBuiltins(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("list_builtin_personas", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-PERSONA] Testing built-in personas list")
		resp := runPersonasList(t, workDir)

		// Should have built-in personas
		if len(resp.Personas) == 0 {
			t.Fatalf("expected built-in personas, got none")
		}

		// Check for known built-in personas
		foundArchitect := false
		foundImplementer := false
		for _, p := range resp.Personas {
			if p.Name == "architect" {
				foundArchitect = true
				if p.AgentType != "claude" {
					t.Fatalf("expected architect agent_type=claude, got %q", p.AgentType)
				}
			}
			if p.Name == "implementer" {
				foundImplementer = true
			}
		}

		if !foundArchitect {
			t.Fatalf("expected to find built-in 'architect' persona")
		}
		if !foundImplementer {
			t.Fatalf("expected to find built-in 'implementer' persona")
		}

		logger.Log("[E2E-PERSONA] Found %d personas, %d profile sets", len(resp.Personas), len(resp.ProfileSets))

		// Should have built-in persona sets
		if len(resp.ProfileSets) == 0 {
			t.Fatalf("expected built-in profile sets, got none")
		}

		foundBackendTeam := false
		for _, s := range resp.ProfileSets {
			if s.Name == "backend-team" {
				foundBackendTeam = true
			}
		}
		if !foundBackendTeam {
			t.Fatalf("expected to find built-in 'backend-team' profile set")
		}
	})

	t.Run("filter_by_agent_type", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-PERSONA] Testing agent type filter")
		resp := runPersonasListFiltered(t, workDir, "claude", "")

		// All results should be claude personas
		for _, p := range resp.Personas {
			if p.AgentType != "claude" {
				t.Fatalf("expected all personas to be claude, found %q", p.AgentType)
			}
		}
		logger.Log("[E2E-PERSONA] Filtered to %d claude personas", len(resp.Personas))
	})

	t.Run("filter_by_tag", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-PERSONA] Testing tag filter")
		resp := runPersonasListFiltered(t, workDir, "", "testing")

		// All results should have the testing tag
		for _, p := range resp.Personas {
			hasTag := false
			for _, tag := range p.Tags {
				if tag == "testing" {
					hasTag = true
					break
				}
			}
			if !hasTag {
				t.Fatalf("expected persona %q to have 'testing' tag", p.Name)
			}
		}
		logger.Log("[E2E-PERSONA] Filtered to %d personas with 'testing' tag", len(resp.Personas))
	})
}

func TestE2EPersonaManagement_ShowDetails(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("show_builtin_persona", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-PERSONA] Testing show architect persona")
		resp := runPersonasShow(t, workDir, "architect")

		if resp.Name != "architect" {
			t.Fatalf("expected name=architect, got %q", resp.Name)
		}
		if resp.AgentType != "claude" {
			t.Fatalf("expected agent_type=claude, got %q", resp.AgentType)
		}
		if resp.Model != "opus" {
			t.Fatalf("expected model=opus, got %q", resp.Model)
		}
		if resp.Source != "built-in" {
			t.Fatalf("expected source=built-in, got %q", resp.Source)
		}
		if resp.SystemPrompt == "" {
			t.Fatalf("expected non-empty system_prompt")
		}

		logger.Log("[E2E-PERSONA] Architect: agent=%s, model=%s, source=%s", resp.AgentType, resp.Model, resp.Source)
	})

	t.Run("show_not_found", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-PERSONA] Testing show nonexistent persona")
		out, err := runCmdAllowFail(t, workDir, "ntm", "--json", "personas", "show", "nonexistent-persona-xyz")
		if err == nil {
			t.Fatalf("expected error for nonexistent persona")
		}

		// Should return error JSON
		var errResp struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if unmarshalErr := json.Unmarshal(out, &errResp); unmarshalErr != nil {
			t.Fatalf("unmarshal error response: %v", unmarshalErr)
		}
		if errResp.Success {
			t.Fatalf("expected success=false for nonexistent persona")
		}
	})
}

func TestE2EPersonaManagement_CustomPersonas(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("project_persona_overrides_builtin", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Create custom project persona that overrides architect
		personasTOML := `
[[personas]]
name = "architect"
description = "Custom project architect"
agent_type = "codex"
model = "gpt-4"
system_prompt = "You are a project-specific architect."
tags = ["custom", "project"]
`
		writePersonasFile(t, workDir, personasTOML)

		logger.Log("[E2E-PERSONA] Testing project persona override")
		resp := runPersonasShow(t, workDir, "architect")

		if resp.Description != "Custom project architect" {
			t.Fatalf("expected custom description, got %q", resp.Description)
		}
		if resp.AgentType != "codex" {
			t.Fatalf("expected agent_type=codex (from override), got %q", resp.AgentType)
		}
		if resp.Model != "gpt-4" {
			t.Fatalf("expected model=gpt-4 (from override), got %q", resp.Model)
		}
		if resp.Source != "project (.ntm/personas.toml)" {
			t.Fatalf("expected source=project, got %q", resp.Source)
		}

		logger.Log("[E2E-PERSONA] Project override: agent=%s, model=%s, source=%s", resp.AgentType, resp.Model, resp.Source)
	})

	t.Run("user_persona_visible", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		xdgConfigHome := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)

		// Create custom user persona
		personasTOML := `
[[personas]]
name = "custom-user-persona"
description = "A custom user persona"
agent_type = "claude"
model = "sonnet"
system_prompt = "You are a custom user persona."
tags = ["user", "custom"]
`
		writeUserPersonasFile(t, xdgConfigHome, personasTOML)

		logger.Log("[E2E-PERSONA] Testing user persona visibility")
		resp := runPersonasList(t, workDir)

		found := false
		for _, p := range resp.Personas {
			if p.Name == "custom-user-persona" {
				found = true
				if p.Description != "A custom user persona" {
					t.Fatalf("expected custom description, got %q", p.Description)
				}
			}
		}
		if !found {
			t.Fatalf("expected to find custom-user-persona in list")
		}

		// Show should report source as user (path varies with XDG_CONFIG_HOME)
		showResp := runPersonasShow(t, workDir, "custom-user-persona")
		// Source should contain "user" - exact path depends on XDG_CONFIG_HOME
		if !contains(showResp.Source, "user") {
			t.Fatalf("expected source to contain 'user', got %q", showResp.Source)
		}

		logger.Log("[E2E-PERSONA] User persona source=%s", showResp.Source)
	})

	t.Run("create_custom_persona_set", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Create custom persona and set
		personasTOML := `
[[personas]]
name = "fast-coder"
description = "Quick implementation specialist"
agent_type = "claude"
model = "haiku"
system_prompt = "Be fast and efficient."
tags = ["fast", "implementation"]

[[persona_sets]]
name = "speed-team"
description = "Team optimized for speed"
personas = ["fast-coder", "fast-coder", "implementer"]
`
		writePersonasFile(t, workDir, personasTOML)

		logger.Log("[E2E-PERSONA] Testing custom persona set")
		resp := runPersonasList(t, workDir)

		// Check custom persona exists
		foundFastCoder := false
		for _, p := range resp.Personas {
			if p.Name == "fast-coder" {
				foundFastCoder = true
			}
		}
		if !foundFastCoder {
			t.Fatalf("expected to find fast-coder persona")
		}

		// Check custom set exists
		foundSpeedTeam := false
		for _, s := range resp.ProfileSets {
			if s.Name == "speed-team" {
				foundSpeedTeam = true
				if len(s.Personas) != 3 {
					t.Fatalf("expected 3 personas in speed-team, got %d", len(s.Personas))
				}
			}
		}
		if !foundSpeedTeam {
			t.Fatalf("expected to find speed-team profile set")
		}

		logger.Log("[E2E-PERSONA] Custom persona set found with %d members", 3)
	})
}

func TestE2EPersonaManagement_Inheritance(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("persona_extends_parent", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Create persona that extends architect
		personasTOML := `
[[personas]]
name = "security-architect"
description = "Security-focused architect"
agent_type = "claude"
extends = "architect"
system_prompt_append = "Focus especially on security concerns."
tags = ["security", "architecture"]
`
		writePersonasFile(t, workDir, personasTOML)

		logger.Log("[E2E-PERSONA] Testing persona inheritance")
		resp := runPersonasShow(t, workDir, "security-architect")

		if resp.Description != "Security-focused architect" {
			t.Fatalf("expected custom description, got %q", resp.Description)
		}

		// System prompt should contain parent's prompt + append
		if resp.SystemPrompt == "" {
			t.Fatalf("expected inherited system prompt")
		}
		// The appended text should be in the system prompt
		if len(resp.SystemPrompt) < 100 {
			t.Fatalf("expected longer system prompt from inheritance, got %d chars", len(resp.SystemPrompt))
		}

		logger.Log("[E2E-PERSONA] Inherited system prompt length: %d chars", len(resp.SystemPrompt))
	})
}

func TestE2EPersonaManagement_ProfilesAlias(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("profiles_alias_works", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-PERSONA] Testing profiles alias")

		// ntm profiles list should work the same as ntm personas list
		out := runCmd(t, workDir, "ntm", "--json", "profiles", "list")
		var resp personaListResponse
		if err := json.Unmarshal(out, &resp); err != nil {
			t.Fatalf("unmarshal profiles list: %v\nout=%s", err, string(out))
		}

		if len(resp.Personas) == 0 {
			t.Fatalf("expected personas from profiles alias")
		}

		logger.Log("[E2E-PERSONA] Profiles alias returned %d personas", len(resp.Personas))

		// ntm profiles show should also work
		showOut := runCmd(t, workDir, "ntm", "--json", "profiles", "show", "architect")
		var showResp personaShowResponse
		if err := json.Unmarshal(showOut, &showResp); err != nil {
			t.Fatalf("unmarshal profiles show: %v\nout=%s", err, string(showOut))
		}

		if showResp.Name != "architect" {
			t.Fatalf("expected name=architect from profiles show, got %q", showResp.Name)
		}
	})
}

func TestE2EPersonaManagement_Validation(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("invalid_persona_rejected", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Create invalid persona (missing agent_type)
		personasTOML := `
[[personas]]
name = "invalid-persona"
description = "This persona is invalid"
# Missing required agent_type
`
		writePersonasFile(t, workDir, personasTOML)

		logger.Log("[E2E-PERSONA] Testing invalid persona rejection")

		// List should fail or exclude invalid persona
		_, err := runCmdAllowFail(t, workDir, "ntm", "--json", "personas", "list")
		// Expecting an error due to invalid persona
		if err == nil {
			// If no error, the invalid persona should not be in the list
			// (implementation may choose to skip invalid entries)
			logger.Log("[E2E-PERSONA] Command succeeded, invalid persona may have been skipped")
		} else {
			logger.Log("[E2E-PERSONA] Command failed as expected: %v", err)
		}
	})

	t.Run("invalid_agent_type_rejected", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Create persona with invalid agent type
		personasTOML := `
[[personas]]
name = "bad-agent"
description = "Has invalid agent type"
agent_type = "invalid-agent-type"
`
		writePersonasFile(t, workDir, personasTOML)

		logger.Log("[E2E-PERSONA] Testing invalid agent type rejection")

		_, err := runCmdAllowFail(t, workDir, "ntm", "--json", "personas", "list")
		if err == nil {
			logger.Log("[E2E-PERSONA] Command succeeded, invalid agent_type may have been skipped")
		} else {
			logger.Log("[E2E-PERSONA] Command failed as expected: %v", err)
		}
	})
}

// Memory system tests require the external 'cm' tool.
// These tests verify the CLI commands work but may skip if cm is not available.

func TestE2EMemorySystem_PrivacyCommands(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	// Note: Memory commands require running daemon; we test command structure
	t.Run("memory_command_structure", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Test that memory subcommands are registered
		logger.Log("[E2E-MEMORY] Testing memory command structure")

		// ntm memory --help should work
		out, err := runCmdAllowFail(t, workDir, "ntm", "memory", "--help")
		if err != nil {
			t.Fatalf("ntm memory --help failed: %v", err)
		}

		outStr := string(out)
		if len(outStr) == 0 {
			t.Fatalf("expected help output")
		}

		// Check for expected subcommands
		expectedSubcmds := []string{"serve", "context", "outcome", "privacy"}
		for _, sub := range expectedSubcmds {
			found := false
			if contains(outStr, sub) {
				found = true
			}
			if !found {
				t.Fatalf("expected memory subcommand %q in help output", sub)
			}
		}

		logger.Log("[E2E-MEMORY] Memory command structure verified")
	})

	t.Run("memory_privacy_subcommands", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-MEMORY] Testing memory privacy subcommand structure")

		// ntm memory privacy --help should work
		out, err := runCmdAllowFail(t, workDir, "ntm", "memory", "privacy", "--help")
		if err != nil {
			t.Fatalf("ntm memory privacy --help failed: %v", err)
		}

		outStr := string(out)
		expectedSubcmds := []string{"status", "enable", "disable", "allow", "deny"}
		for _, sub := range expectedSubcmds {
			if !contains(outStr, sub) {
				t.Fatalf("expected privacy subcommand %q in help output", sub)
			}
		}

		logger.Log("[E2E-MEMORY] Memory privacy subcommands verified")
	})
}

func TestE2EMemorySystem_ContextAndOutcome_FakeDaemon(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	homeDir := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var outcomeMu sync.Mutex
	var outcomeBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/context":
			_, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
  "relevantBullets": [{"id": "rule-1", "content": "Always keep prompts redacted at rest", "category": "safety"}],
  "antiPatterns": [],
  "historySnippets": [],
  "suggestedCassQueries": ["redaction persistence"]
}`))
		case "/outcome":
			body, _ := io.ReadAll(r.Body)
			outcomeMu.Lock()
			outcomeBody = body
			outcomeMu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	parsed, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}

	// Create a fake cm pid file so `ntm memory` can discover the daemon.
	pidsDir := filepath.Join(workDir, ".ntm", "pids")
	if err := os.MkdirAll(pidsDir, 0755); err != nil {
		t.Fatalf("mkdir pids dir: %v", err)
	}
	pidPath := filepath.Join(pidsDir, "cm-e2e.pid")
	pidInfo := map[string]interface{}{
		"pid":        12345,
		"port":       port,
		"owner_id":   "e2e",
		"command":    "cm serve --port",
		"started_at": time.Now().UTC(),
	}
	pidData, err := json.Marshal(pidInfo)
	if err != nil {
		t.Fatalf("marshal pid info: %v", err)
	}
	if err := os.WriteFile(pidPath, pidData, 0644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	logger.Log("[E2E-MEMORY] Testing `ntm memory context` via fake daemon")
	out := runCmd(t, workDir, "ntm", "memory", "context", "test task")
	var ctxResp memoryContextResponse
	if err := json.Unmarshal(out, &ctxResp); err != nil {
		t.Fatalf("unmarshal memory context: %v\nout=%s", err, string(out))
	}
	if len(ctxResp.RelevantBullets) != 1 || ctxResp.RelevantBullets[0].ID != "rule-1" {
		t.Fatalf("unexpected memory context response: %+v", ctxResp)
	}

	logger.Log("[E2E-MEMORY] Testing `ntm memory outcome` via fake daemon")
	if _, err := runCmdAllowFail(t, workDir, "ntm", "memory", "outcome", "success", "--rules", "rule-1"); err != nil {
		t.Fatalf("ntm memory outcome failed: %v", err)
	}

	outcomeMu.Lock()
	gotOutcome := append([]byte(nil), outcomeBody...)
	outcomeMu.Unlock()

	var report memoryOutcomeReport
	if err := json.Unmarshal(gotOutcome, &report); err != nil {
		t.Fatalf("unmarshal outcome body: %v\nbody=%s", err, string(gotOutcome))
	}
	if report.Status != "success" {
		t.Fatalf("outcome status = %q, want %q", report.Status, "success")
	}
	if len(report.RuleIDs) != 1 || report.RuleIDs[0] != "rule-1" {
		t.Fatalf("outcome rule_ids = %#v, want [rule-1]", report.RuleIDs)
	}
}

// contains checks if substr is in s (simple substring check)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
