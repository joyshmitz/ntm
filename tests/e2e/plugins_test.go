package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// TestE2EPlugins_ListEmpty tests plugins list with no plugins installed.
func TestE2EPlugins_ListEmpty(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	t.Run("no_plugins_message", func(t *testing.T) {
		logger := testutil.NewTestLogger(t, t.TempDir())
		tmpDir := t.TempDir()
		logger.Log("tmp_dir=%s", tmpDir)

		// Set XDG_CONFIG_HOME to empty temp dir (no plugins)
		configDir := filepath.Join(tmpDir, "config")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("mkdir config: %v", err)
		}

		// Run plugins list with custom config location
		out, err := runCmdWithEnv(t, logger, tmpDir, map[string]string{
			"XDG_CONFIG_HOME": tmpDir,
			"HOME":            tmpDir,
		}, "ntm", "plugins", "list")

		if err != nil {
			t.Fatalf("plugins list failed: %v\nout=%s", err, string(out))
		}

		// Should show "No plugins installed" message
		if !strings.Contains(string(out), "No plugins installed") {
			t.Errorf("expected 'No plugins installed' message, got: %s", string(out))
		}
	})
}

// TestE2EPlugins_ListAgentPlugins tests plugins list with agent plugins.
func TestE2EPlugins_ListAgentPlugins(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	t.Run("agent_plugin_discovered", func(t *testing.T) {
		logger := testutil.NewTestLogger(t, t.TempDir())
		tmpDir := t.TempDir()
		logger.Log("tmp_dir=%s", tmpDir)

		// Create ntm config directory structure
		ntmDir := filepath.Join(tmpDir, ".config", "ntm")
		agentsDir := filepath.Join(ntmDir, "agents")
		if err := os.MkdirAll(agentsDir, 0755); err != nil {
			t.Fatalf("mkdir agents: %v", err)
		}

		// Create a test agent plugin TOML file
		pluginContent := `[agent]
name = "test-agent"
alias = "ta"
command = "echo 'test agent'"
description = "A test agent plugin"
`
		pluginPath := filepath.Join(agentsDir, "test-agent.toml")
		if err := os.WriteFile(pluginPath, []byte(pluginContent), 0644); err != nil {
			t.Fatalf("write plugin: %v", err)
		}
		logger.Log("agent_plugin_path=%s", pluginPath)

		// Run plugins list
		out, err := runCmdWithEnv(t, logger, tmpDir, map[string]string{
			"HOME": tmpDir,
		}, "ntm", "plugins", "list")

		if err != nil {
			t.Fatalf("plugins list failed: %v\nout=%s", err, string(out))
		}

		outStr := string(out)

		// Should show Agent Plugins section
		if !strings.Contains(outStr, "Agent Plugins") {
			t.Errorf("expected 'Agent Plugins' header, got: %s", outStr)
		}

		// Should show our test plugin
		if !strings.Contains(outStr, "test-agent") {
			t.Errorf("expected 'test-agent' in output, got: %s", outStr)
		}

		// Should show alias
		if !strings.Contains(outStr, "ta") {
			t.Errorf("expected alias 'ta' in output, got: %s", outStr)
		}

		// Should show description
		if !strings.Contains(outStr, "test agent plugin") {
			t.Errorf("expected description in output, got: %s", outStr)
		}
	})

	t.Run("multiple_agent_plugins", func(t *testing.T) {
		logger := testutil.NewTestLogger(t, t.TempDir())
		tmpDir := t.TempDir()
		logger.Log("tmp_dir=%s", tmpDir)

		// Create config directory
		agentsDir := filepath.Join(tmpDir, ".config", "ntm", "agents")
		if err := os.MkdirAll(agentsDir, 0755); err != nil {
			t.Fatalf("mkdir agents: %v", err)
		}

		// Create multiple test plugins
		plugins := []struct {
			name    string
			content string
		}{
			{
				name: "plugin-a.toml",
				content: `[agent]
name = "plugin-a"
command = "echo a"
description = "Plugin A"
`,
			},
			{
				name: "plugin-b.toml",
				content: `[agent]
name = "plugin-b"
alias = "pb"
command = "echo b"
description = "Plugin B"
`,
			},
		}

		for _, p := range plugins {
			path := filepath.Join(agentsDir, p.name)
			if err := os.WriteFile(path, []byte(p.content), 0644); err != nil {
				t.Fatalf("write %s: %v", p.name, err)
			}
			logger.Log("agent_plugin_path=%s", path)
		}

		// Run plugins list
		out, err := runCmdWithEnv(t, logger, tmpDir, map[string]string{
			"HOME": tmpDir,
		}, "ntm", "plugins", "list")

		if err != nil {
			t.Fatalf("plugins list failed: %v\nout=%s", err, string(out))
		}

		outStr := string(out)

		// Should show both plugins
		if !strings.Contains(outStr, "plugin-a") {
			t.Errorf("expected 'plugin-a' in output, got: %s", outStr)
		}
		if !strings.Contains(outStr, "plugin-b") {
			t.Errorf("expected 'plugin-b' in output, got: %s", outStr)
		}
	})
}

// TestE2EPlugins_ListCommandPlugins tests plugins list with command plugins.
func TestE2EPlugins_ListCommandPlugins(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	t.Run("command_plugin_discovered", func(t *testing.T) {
		logger := testutil.NewTestLogger(t, t.TempDir())
		tmpDir := t.TempDir()
		logger.Log("tmp_dir=%s", tmpDir)

		// Create config directory
		commandsDir := filepath.Join(tmpDir, ".config", "ntm", "commands")
		if err := os.MkdirAll(commandsDir, 0755); err != nil {
			t.Fatalf("mkdir commands: %v", err)
		}

		// Create a test command plugin (executable script)
		scriptContent := `#!/bin/bash
# Description: A test command plugin
# Usage: test-cmd [args]
echo "test command"
`
		scriptPath := filepath.Join(commandsDir, "test-cmd")
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
			t.Fatalf("write script: %v", err)
		}
		logger.Log("command_plugin_path=%s", scriptPath)

		// Run plugins list
		out, err := runCmdWithEnv(t, logger, tmpDir, map[string]string{
			"HOME": tmpDir,
		}, "ntm", "plugins", "list")

		if err != nil {
			t.Fatalf("plugins list failed: %v\nout=%s", err, string(out))
		}

		outStr := string(out)

		// Should show Command Plugins section
		if !strings.Contains(outStr, "Command Plugins") {
			t.Errorf("expected 'Command Plugins' header, got: %s", outStr)
		}

		// Should show our test command
		if !strings.Contains(outStr, "test-cmd") {
			t.Errorf("expected 'test-cmd' in output, got: %s", outStr)
		}

		// Should show description from header
		if !strings.Contains(outStr, "test command plugin") {
			t.Errorf("expected description in output, got: %s", outStr)
		}
	})
}

// TestE2EPlugins_InvalidPlugins tests handling of invalid plugin files.
func TestE2EPlugins_InvalidPlugins(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	t.Run("skip_invalid_toml", func(t *testing.T) {
		logger := testutil.NewTestLogger(t, t.TempDir())
		tmpDir := t.TempDir()
		logger.Log("tmp_dir=%s", tmpDir)

		// Create config directory
		agentsDir := filepath.Join(tmpDir, ".config", "ntm", "agents")
		if err := os.MkdirAll(agentsDir, 0755); err != nil {
			t.Fatalf("mkdir agents: %v", err)
		}

		// Create invalid TOML file
		invalidContent := `this is not valid toml {{{`
		invalidPath := filepath.Join(agentsDir, "invalid.toml")
		if err := os.WriteFile(invalidPath, []byte(invalidContent), 0644); err != nil {
			t.Fatalf("write invalid: %v", err)
		}
		logger.Log("invalid_plugin_path=%s", invalidPath)

		// Create valid plugin
		validContent := `[agent]
name = "valid-plugin"
command = "echo valid"
description = "Valid plugin"
`
		validPath := filepath.Join(agentsDir, "valid.toml")
		if err := os.WriteFile(validPath, []byte(validContent), 0644); err != nil {
			t.Fatalf("write valid: %v", err)
		}
		logger.Log("valid_plugin_path=%s", validPath)

		// Run plugins list - should not fail, just skip invalid
		out, err := runCmdWithEnv(t, logger, tmpDir, map[string]string{
			"HOME": tmpDir,
		}, "ntm", "plugins", "list")

		if err != nil {
			t.Fatalf("plugins list failed: %v\nout=%s", err, string(out))
		}

		outStr := string(out)

		// Should show valid plugin
		if !strings.Contains(outStr, "valid-plugin") {
			t.Errorf("expected 'valid-plugin' in output, got: %s", outStr)
		}

		// Should NOT show invalid plugin as a listed plugin
		// Note: log messages may contain "invalid.toml" which is fine - we're checking
		// that "invalid" doesn't appear as a plugin NAME in the output table
		// The plugin name would appear without the .toml extension
		lines := strings.Split(outStr, "\n")
		for _, line := range lines {
			// Skip log lines (contain timestamp or "failed to parse")
			if strings.Contains(line, "failed to parse") || strings.Contains(line, "invalid.toml") {
				continue
			}
			// Check if "invalid" appears as a plugin name (not in log context)
			// Plugin names appear in table rows, typically after whitespace
			if strings.Contains(line, "invalid") && !strings.Contains(line, "invalid.toml") {
				t.Errorf("invalid plugin should not be listed, found in line: %s", line)
			}
		}
	})

	t.Run("skip_missing_command", func(t *testing.T) {
		logger := testutil.NewTestLogger(t, t.TempDir())
		tmpDir := t.TempDir()
		logger.Log("tmp_dir=%s", tmpDir)

		// Create config directory
		agentsDir := filepath.Join(tmpDir, ".config", "ntm", "agents")
		if err := os.MkdirAll(agentsDir, 0755); err != nil {
			t.Fatalf("mkdir agents: %v", err)
		}

		// Create plugin without command field
		noCommandContent := `[agent]
name = "no-command"
description = "Plugin without command"
`
		pluginPath := filepath.Join(agentsDir, "no-command.toml")
		if err := os.WriteFile(pluginPath, []byte(noCommandContent), 0644); err != nil {
			t.Fatalf("write no-command: %v", err)
		}
		logger.Log("agent_plugin_path=%s", pluginPath)

		// Run plugins list
		out, err := runCmdWithEnv(t, logger, tmpDir, map[string]string{
			"HOME": tmpDir,
		}, "ntm", "plugins", "list")

		if err != nil {
			t.Fatalf("plugins list failed: %v\nout=%s", err, string(out))
		}

		outStr := string(out)

		// Should show "No plugins" since the only plugin is invalid
		if !strings.Contains(outStr, "No plugins installed") {
			// Or it could just not show the invalid one
			if strings.Contains(outStr, "no-command") {
				t.Errorf("should not show plugin without command field, got: %s", outStr)
			}
		}
	})
}

// TestE2EPlugins_MixedPlugins tests plugins list with both agent and command plugins.
func TestE2EPlugins_MixedPlugins(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	t.Run("both_types_shown", func(t *testing.T) {
		logger := testutil.NewTestLogger(t, t.TempDir())
		tmpDir := t.TempDir()
		logger.Log("tmp_dir=%s", tmpDir)

		// Create config directories
		agentsDir := filepath.Join(tmpDir, ".config", "ntm", "agents")
		commandsDir := filepath.Join(tmpDir, ".config", "ntm", "commands")
		if err := os.MkdirAll(agentsDir, 0755); err != nil {
			t.Fatalf("mkdir agents: %v", err)
		}
		if err := os.MkdirAll(commandsDir, 0755); err != nil {
			t.Fatalf("mkdir commands: %v", err)
		}

		// Create agent plugin
		agentContent := `[agent]
name = "my-agent"
command = "echo agent"
description = "My agent"
`
		agentPath := filepath.Join(agentsDir, "my-agent.toml")
		if err := os.WriteFile(agentPath, []byte(agentContent), 0644); err != nil {
			t.Fatalf("write agent: %v", err)
		}
		logger.Log("agent_plugin_path=%s", agentPath)

		// Create command plugin
		cmdContent := `#!/bin/bash
# Description: My command
echo "command"
`
		commandPath := filepath.Join(commandsDir, "my-cmd")
		if err := os.WriteFile(commandPath, []byte(cmdContent), 0755); err != nil {
			t.Fatalf("write cmd: %v", err)
		}
		logger.Log("command_plugin_path=%s", commandPath)

		// Run plugins list
		out, err := runCmdWithEnv(t, logger, tmpDir, map[string]string{
			"HOME": tmpDir,
		}, "ntm", "plugins", "list")

		if err != nil {
			t.Fatalf("plugins list failed: %v\nout=%s", err, string(out))
		}

		outStr := string(out)

		// Should show both sections
		if !strings.Contains(outStr, "Agent Plugins") {
			t.Errorf("expected 'Agent Plugins' section, got: %s", outStr)
		}
		if !strings.Contains(outStr, "Command Plugins") {
			t.Errorf("expected 'Command Plugins' section, got: %s", outStr)
		}

		// Should show both plugins
		if !strings.Contains(outStr, "my-agent") {
			t.Errorf("expected 'my-agent' in output, got: %s", outStr)
		}
		if !strings.Contains(outStr, "my-cmd") {
			t.Errorf("expected 'my-cmd' in output, got: %s", outStr)
		}
	})
}

// runCmdWithEnv runs a command with custom environment variables.
func runCmdWithEnv(t *testing.T, logger *testutil.TestLogger, dir string, env map[string]string, name string, args ...string) ([]byte, error) {
	t.Helper()
	if logger != nil {
		logger.Log("EXEC: %s %s", name, strings.Join(args, " "))
		logger.Log("DIR: %s", dir)
		for k, v := range env {
			logger.Log("ENV: %s=%s", k, v)
		}
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.CombinedOutput()
	if logger != nil {
		outStr := string(out)
		if len(outStr) > 2000 {
			outStr = outStr[:2000] + "\n... (truncated)"
		}
		if outStr != "" {
			logger.Log("OUTPUT:\n%s", outStr)
		}
		if err != nil {
			logger.Log("EXIT: error: %v", err)
		} else {
			logger.Log("EXIT: success (exit 0)")
		}
	}
	return out, err
}
