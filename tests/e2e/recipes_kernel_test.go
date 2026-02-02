package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// recipesListResponse is the JSON output for ntm recipes list.
type recipesListResponse struct {
	Recipes []recipeInfo `json:"recipes"`
	Total   int          `json:"total"`
}

// recipeInfo represents a single recipe in list output.
type recipeInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	TotalAgents int    `json:"total_agents"`
}

// recipeShowResponse is the JSON output for ntm recipes show.
type recipeShowResponse struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Source      string      `json:"source"`
	TotalAgents int         `json:"total_agents"`
	Agents      []agentSpec `json:"agents"`
}

// agentSpec represents an agent configuration in a recipe.
type agentSpec struct {
	Type    string `json:"type"`
	Count   int    `json:"count"`
	Model   string `json:"model,omitempty"`
	Persona string `json:"persona,omitempty"`
}

// kernelListResponse is the JSON output for ntm kernel list.
type kernelListResponse struct {
	Commands []kernelCommand `json:"commands"`
	Count    int             `json:"count"`
}

// kernelCommand represents a registered kernel command.
type kernelCommand struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Category    string       `json:"category"`
	REST        *restBinding `json:"rest,omitempty"`
}

// restBinding represents REST API binding for a kernel command.
type restBinding struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

func runRecipesList(t *testing.T, dir string) recipesListResponse {
	t.Helper()
	out := runCmd(t, dir, "ntm", "--json", "recipes", "list")
	var resp recipesListResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal recipes list: %v\nout=%s", err, string(out))
	}
	return resp
}

func runRecipesShow(t *testing.T, dir, name string) recipeShowResponse {
	t.Helper()
	out := runCmd(t, dir, "ntm", "--json", "recipes", "show", name)
	var resp recipeShowResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal recipes show: %v\nout=%s", err, string(out))
	}
	return resp
}

func runKernelList(t *testing.T, dir string) kernelListResponse {
	t.Helper()
	out := runCmd(t, dir, "ntm", "--json", "kernel", "list")
	var resp kernelListResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal kernel list: %v\nout=%s", err, string(out))
	}
	return resp
}

func writeRecipesFile(t *testing.T, dir string, toml string) {
	t.Helper()
	ntmDir := filepath.Join(dir, ".ntm")
	if err := os.MkdirAll(ntmDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", ntmDir, err)
	}
	path := filepath.Join(ntmDir, "recipes.toml")
	if err := os.WriteFile(path, []byte(toml), 0644); err != nil {
		t.Fatalf("write recipes.toml: %v", err)
	}
}

func writeUserRecipesFile(t *testing.T, xdgConfigHome string, toml string) {
	t.Helper()
	configDir := filepath.Join(xdgConfigHome, "ntm")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", configDir, err)
	}
	path := filepath.Join(configDir, "recipes.toml")
	if err := os.WriteFile(path, []byte(toml), 0644); err != nil {
		t.Fatalf("write recipes.toml: %v", err)
	}
}

func TestE2ERecipeManagement_ListBuiltins(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("list_builtin_recipes", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-RECIPE] Testing built-in recipes list")
		resp := runRecipesList(t, workDir)

		// Should have built-in recipes
		if resp.Total == 0 {
			t.Fatalf("expected built-in recipes, got none")
		}

		// Check for known built-in recipes
		foundFullStack := false
		foundMinimal := false
		for _, r := range resp.Recipes {
			if r.Name == "full-stack" {
				foundFullStack = true
				if r.Source != "builtin" {
					t.Fatalf("expected full-stack source=builtin, got %q", r.Source)
				}
				if r.TotalAgents != 6 {
					t.Fatalf("expected full-stack to have 6 agents, got %d", r.TotalAgents)
				}
			}
			if r.Name == "minimal" {
				foundMinimal = true
				if r.TotalAgents != 1 {
					t.Fatalf("expected minimal to have 1 agent, got %d", r.TotalAgents)
				}
			}
		}

		if !foundFullStack {
			t.Fatalf("expected to find built-in 'full-stack' recipe")
		}
		if !foundMinimal {
			t.Fatalf("expected to find built-in 'minimal' recipe")
		}

		logger.Log("[E2E-RECIPE] Found %d recipes", resp.Total)
	})
}

func TestE2ERecipeManagement_ShowDetails(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("show_builtin_recipe", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-RECIPE] Testing show full-stack recipe")
		resp := runRecipesShow(t, workDir, "full-stack")

		if resp.Name != "full-stack" {
			t.Fatalf("expected name=full-stack, got %q", resp.Name)
		}
		if resp.Source != "builtin" {
			t.Fatalf("expected source=builtin, got %q", resp.Source)
		}
		if resp.TotalAgents != 6 {
			t.Fatalf("expected 6 total agents, got %d", resp.TotalAgents)
		}

		// Verify agent breakdown: 3 cc, 2 cod, 1 gmi
		ccCount := 0
		codCount := 0
		gmiCount := 0
		for _, a := range resp.Agents {
			switch a.Type {
			case "cc":
				ccCount += a.Count
			case "cod":
				codCount += a.Count
			case "gmi":
				gmiCount += a.Count
			}
		}
		if ccCount != 3 {
			t.Fatalf("expected 3 cc agents, got %d", ccCount)
		}
		if codCount != 2 {
			t.Fatalf("expected 2 cod agents, got %d", codCount)
		}
		if gmiCount != 1 {
			t.Fatalf("expected 1 gmi agent, got %d", gmiCount)
		}

		logger.Log("[E2E-RECIPE] full-stack: %d cc, %d cod, %d gmi", ccCount, codCount, gmiCount)
	})

	t.Run("show_not_found", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-RECIPE] Testing show nonexistent recipe")
		// Command exits 0 but returns error in JSON
		out := runCmd(t, workDir, "ntm", "--json", "recipes", "show", "nonexistent-recipe-xyz")

		// Should return error JSON
		var errResp struct {
			Error string `json:"error"`
		}
		if unmarshalErr := json.Unmarshal(out, &errResp); unmarshalErr != nil {
			t.Fatalf("unmarshal error response: %v", unmarshalErr)
		}
		if errResp.Error == "" {
			t.Fatalf("expected error message for nonexistent recipe")
		}
		if !contains(errResp.Error, "not found") {
			t.Fatalf("expected 'not found' in error, got %q", errResp.Error)
		}

		logger.Log("[E2E-RECIPE] Not found error: %s", errResp.Error)
	})
}

func TestE2ERecipeManagement_CustomRecipes(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("project_recipe_overrides_builtin", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Create custom project recipe that overrides full-stack
		recipesTOML := `
[[recipes]]
name = "full-stack"
description = "Custom project full-stack team"
[[recipes.agents]]
type = "cc"
count = 5
`
		writeRecipesFile(t, workDir, recipesTOML)

		logger.Log("[E2E-RECIPE] Testing project recipe override")
		resp := runRecipesShow(t, workDir, "full-stack")

		if resp.Description != "Custom project full-stack team" {
			t.Fatalf("expected custom description, got %q", resp.Description)
		}
		if resp.Source != "project" {
			t.Fatalf("expected source=project (from override), got %q", resp.Source)
		}
		if resp.TotalAgents != 5 {
			t.Fatalf("expected 5 agents (from override), got %d", resp.TotalAgents)
		}

		logger.Log("[E2E-RECIPE] Project override: source=%s, agents=%d", resp.Source, resp.TotalAgents)
	})

	t.Run("user_recipe_visible", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		xdgConfigHome := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)

		// Create custom user recipe
		recipesTOML := `
[[recipes]]
name = "custom-user-recipe"
description = "A custom user recipe"
[[recipes.agents]]
type = "cc"
count = 3
`
		writeUserRecipesFile(t, xdgConfigHome, recipesTOML)

		logger.Log("[E2E-RECIPE] Testing user recipe visibility")
		resp := runRecipesList(t, workDir)

		found := false
		for _, r := range resp.Recipes {
			if r.Name == "custom-user-recipe" {
				found = true
				if r.Source != "user" {
					t.Fatalf("expected source=user, got %q", r.Source)
				}
			}
		}
		if !found {
			t.Fatalf("expected to find custom-user-recipe in list")
		}

		logger.Log("[E2E-RECIPE] User recipe found")
	})

	t.Run("create_custom_recipe_with_model_override", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Create custom recipe with model override
		recipesTOML := `
[[recipes]]
name = "opus-team"
description = "Team with specific model overrides"
[[recipes.agents]]
type = "cc"
count = 2
model = "opus"
[[recipes.agents]]
type = "cc"
count = 1
model = "sonnet"
`
		writeRecipesFile(t, workDir, recipesTOML)

		logger.Log("[E2E-RECIPE] Testing custom recipe with model overrides")
		resp := runRecipesShow(t, workDir, "opus-team")

		if resp.Name != "opus-team" {
			t.Fatalf("expected name=opus-team, got %q", resp.Name)
		}
		if resp.TotalAgents != 3 {
			t.Fatalf("expected 3 agents, got %d", resp.TotalAgents)
		}

		// Verify model overrides
		opusCount := 0
		sonnetCount := 0
		for _, a := range resp.Agents {
			if a.Model == "opus" {
				opusCount += a.Count
			}
			if a.Model == "sonnet" {
				sonnetCount += a.Count
			}
		}
		if opusCount != 2 {
			t.Fatalf("expected 2 opus agents, got %d", opusCount)
		}
		if sonnetCount != 1 {
			t.Fatalf("expected 1 sonnet agent, got %d", sonnetCount)
		}

		logger.Log("[E2E-RECIPE] Model overrides: %d opus, %d sonnet", opusCount, sonnetCount)
	})
}

func TestE2ERecipeManagement_Precedence(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("project_overrides_user", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		xdgConfigHome := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)

		// Create user recipe
		userRecipesTOML := `
[[recipes]]
name = "test-precedence"
description = "User version"
[[recipes.agents]]
type = "cc"
count = 1
`
		writeUserRecipesFile(t, xdgConfigHome, userRecipesTOML)

		// Create project recipe with same name (should override)
		projectRecipesTOML := `
[[recipes]]
name = "test-precedence"
description = "Project version"
[[recipes.agents]]
type = "cc"
count = 2
`
		writeRecipesFile(t, workDir, projectRecipesTOML)

		logger.Log("[E2E-RECIPE] Testing project > user precedence")
		resp := runRecipesShow(t, workDir, "test-precedence")

		if resp.Description != "Project version" {
			t.Fatalf("expected 'Project version' (project should override user), got %q", resp.Description)
		}
		if resp.Source != "project" {
			t.Fatalf("expected source=project, got %q", resp.Source)
		}
		if resp.TotalAgents != 2 {
			t.Fatalf("expected 2 agents (from project), got %d", resp.TotalAgents)
		}

		logger.Log("[E2E-RECIPE] Precedence verified: source=%s", resp.Source)
	})
}

func TestE2EKernelRegistry_List(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("list_kernel_commands", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-KERNEL] Testing kernel list")
		resp := runKernelList(t, workDir)

		// Should have registered commands
		if resp.Count == 0 {
			t.Fatalf("expected registered kernel commands, got none")
		}

		// Check for kernel.list command itself
		foundKernelList := false
		for _, cmd := range resp.Commands {
			if cmd.Name == "kernel.list" {
				foundKernelList = true
				if cmd.Category != "kernel" {
					t.Fatalf("expected kernel.list category=kernel, got %q", cmd.Category)
				}
				if cmd.REST == nil {
					t.Fatalf("expected kernel.list to have REST binding")
				}
				if cmd.REST.Method != "GET" {
					t.Fatalf("expected kernel.list REST method=GET, got %q", cmd.REST.Method)
				}
			}
		}

		if !foundKernelList {
			t.Fatalf("expected to find kernel.list command in registry")
		}

		logger.Log("[E2E-KERNEL] Found %d kernel commands", resp.Count)
	})

	t.Run("kernel_categories", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-KERNEL] Testing kernel categories")
		resp := runKernelList(t, workDir)

		// Collect unique categories
		categories := make(map[string]int)
		for _, cmd := range resp.Commands {
			categories[cmd.Category]++
		}

		if len(categories) == 0 {
			t.Fatalf("expected at least one category")
		}

		logger.Log("[E2E-KERNEL] Categories: %v", categories)
	})

	t.Run("kernel_rest_bindings", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-KERNEL] Testing kernel REST bindings")
		resp := runKernelList(t, workDir)

		// Count commands with REST bindings
		withREST := 0
		for _, cmd := range resp.Commands {
			if cmd.REST != nil {
				withREST++
				// Verify REST binding has valid method
				switch cmd.REST.Method {
				case "GET", "POST", "PUT", "DELETE", "PATCH":
					// Valid
				default:
					t.Fatalf("unexpected REST method %q for command %s", cmd.REST.Method, cmd.Name)
				}
				// Verify path starts with /
				if len(cmd.REST.Path) == 0 || cmd.REST.Path[0] != '/' {
					t.Fatalf("REST path should start with /, got %q for command %s", cmd.REST.Path, cmd.Name)
				}
			}
		}

		logger.Log("[E2E-KERNEL] %d/%d commands have REST bindings", withREST, resp.Count)
	})
}

func TestE2EKernelRegistry_CommandStructure(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("command_structure", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-KERNEL] Testing command structure")

		// ntm kernel --help should work
		out, err := runCmdAllowFail(t, workDir, "ntm", "kernel", "--help")
		if err != nil {
			t.Fatalf("ntm kernel --help failed: %v", err)
		}

		outStr := string(out)
		if len(outStr) == 0 {
			t.Fatalf("expected help output")
		}

		// Check for expected subcommands
		if !contains(outStr, "list") {
			t.Fatalf("expected 'list' in kernel help output")
		}

		logger.Log("[E2E-KERNEL] Command structure verified")
	})
}
