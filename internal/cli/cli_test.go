package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// resetFlags resets global flags to default values between tests
func resetFlags() {
	jsonOutput = false
	robotHelp = false
	robotStatus = false
	robotVersion = false
	robotPlan = false
	robotSnapshot = false
	robotSince = ""
	robotTail = ""
	robotLines = 20
	robotPanes = ""
	robotSend = ""
	robotSendMsg = ""
	robotSendAll = false
	robotSendType = ""
	robotSendExclude = ""
	robotSendDelay = 0
}

// TestExecuteHelp verifies that the root command executes successfully
func TestExecuteHelp(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"--help"})

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() with --help failed: %v", err)
	}
}

// TestVersionCmdExecutes tests the version subcommand runs without error
func TestVersionCmdExecutes(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"default version", []string{"version"}},
		{"short version", []string{"version", "--short"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFlags()
			rootCmd.SetArgs(tt.args)

			err := rootCmd.Execute()
			if err != nil {
				t.Fatalf("Execute() failed: %v", err)
			}
		})
	}
}

// TestConfigPathCmdExecutes tests the config path subcommand runs
func TestConfigPathCmdExecutes(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"config", "path"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestConfigShowCmdExecutes tests the config show subcommand runs
func TestConfigShowCmdExecutes(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"config", "show"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestDepsCmdExecutes tests the deps command runs
func TestDepsCmdExecutes(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"deps"})

	err := rootCmd.Execute()
	// deps may exit 1 if missing required deps, but shouldn't panic
	_ = err
}

// TestListCmdExecutes tests list command executes
func TestListCmdExecutes(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	resetFlags()
	rootCmd.SetArgs([]string{"list"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestListCmdJSONExecutes tests list command with JSON output executes
func TestListCmdJSONExecutes(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	resetFlags()
	rootCmd.SetArgs([]string{"list", "--json"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestSpawnValidation tests spawn command argument validation
func TestSpawnValidation(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	// Initialize config for spawn command
	cfg = config.Default()

	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "missing session name",
			args:        []string{"spawn"},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "no agents specified",
			args:        []string{"spawn", "testproject"},
			expectError: true,
			errorMsg:    "no agents specified",
		},
		{
			name:        "invalid session name with colon",
			args:        []string{"spawn", "test:project", "--cc=1"},
			expectError: true,
			errorMsg:    "cannot contain ':'",
		},
		{
			name:        "invalid session name with dot",
			args:        []string{"spawn", "test.project", "--cc=1"},
			expectError: true,
			errorMsg:    "cannot contain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFlags()
			rootCmd.SetArgs(tt.args)

			err := rootCmd.Execute()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestIsJSONOutput tests the JSON output detection
func TestIsJSONOutput(t *testing.T) {
	// Save original value
	original := jsonOutput
	defer func() { jsonOutput = original }()

	jsonOutput = false
	if IsJSONOutput() {
		t.Error("Expected IsJSONOutput() to return false")
	}

	jsonOutput = true
	if !IsJSONOutput() {
		t.Error("Expected IsJSONOutput() to return true")
	}
}

// TestGetFormatter tests the formatter creation
func TestGetFormatter(t *testing.T) {
	formatter := GetFormatter()
	if formatter == nil {
		t.Fatal("Expected non-nil formatter")
	}
}

// TestBuildInfo tests that build info variables are set
func TestBuildInfo(t *testing.T) {
	// These should have default values even if not set by build
	if Version == "" {
		t.Error("Version should not be empty")
	}
	if Commit == "" {
		t.Error("Commit should not be empty")
	}
	if Date == "" {
		t.Error("Date should not be empty")
	}
	if BuiltBy == "" {
		t.Error("BuiltBy should not be empty")
	}
}

// TestRobotVersionExecutes tests robot-version flag executes
func TestRobotVersionExecutes(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"--robot-version"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestRobotHelpExecutes tests robot-help flag executes
func TestRobotHelpExecutes(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"--robot-help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestRobotStatusExecutes tests the robot-status flag executes
func TestRobotStatusExecutes(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	resetFlags()
	rootCmd.SetArgs([]string{"--robot-status"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestRobotSnapshotExecutes tests the robot-snapshot flag executes
func TestRobotSnapshotExecutes(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	resetFlags()
	rootCmd.SetArgs([]string{"--robot-snapshot"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestRobotPlanExecutes tests the robot-plan flag executes
func TestRobotPlanExecutes(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	resetFlags()
	rootCmd.SetArgs([]string{"--robot-plan"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestAttachCmdNoArgs tests attach command without arguments
func TestAttachCmdNoArgs(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	// Initialize config
	cfg = config.Default()
	resetFlags()
	rootCmd.SetArgs([]string{"attach"})

	err := rootCmd.Execute()
	// Should not error - just lists sessions
	if err != nil && !strings.Contains(err.Error(), "no server running") {
		t.Logf("Attach without args result: %v", err)
	}
}

// TestStatusCmdRequiresArg tests status command requires session name
func TestStatusCmdRequiresArg(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"status"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for status without session name")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Errorf("Expected 'accepts 1 arg' error, got: %v", err)
	}
}

// TestAddCmdRequiresSession tests add command requires session name
func TestAddCmdRequiresSession(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"add"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for add without session name")
	}
}

// TestZoomCmdRequiresArgs tests zoom command requires arguments
func TestZoomCmdRequiresArgs(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"zoom"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for zoom without arguments")
	}
}

// TestSendCmdRequiresArgs tests send command requires arguments
func TestSendCmdRequiresArgs(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"send"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for send without arguments")
	}
}

// TestCompletionCmdExecutes tests completion subcommand executes
func TestCompletionCmdExecutes(t *testing.T) {
	shells := []string{"bash", "zsh", "fish", "powershell"}

	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			resetFlags()
			rootCmd.SetArgs([]string{"completion", shell})

			err := rootCmd.Execute()
			if err != nil {
				t.Fatalf("completion %s failed: %v", shell, err)
			}
		})
	}
}

// TestInitCmdExecutes tests init subcommand for shell integration executes
func TestInitCmdExecutes(t *testing.T) {
	shells := []string{"bash", "zsh"}

	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			resetFlags()
			rootCmd.SetArgs([]string{"init", shell})

			err := rootCmd.Execute()
			if err != nil {
				t.Fatalf("init %s failed: %v", shell, err)
			}
		})
	}
}

// TestKillCmdRequiresSession tests kill command requires session name
func TestKillCmdRequiresSession(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"kill"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for kill without session name")
	}
}

// TestViewCmdRequiresSession tests view command requires session name
func TestViewCmdRequiresSession(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	resetFlags()
	rootCmd.SetArgs([]string{"view"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for view without session name")
	}
}

// TestCopyCmdRequiresSession tests copy command requires session name
func TestCopyCmdRequiresSession(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"copy"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for copy without session name")
	}
}

// TestSaveCmdRequiresSession tests save command requires session name
func TestSaveCmdRequiresSession(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"save"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for save without session name")
	}
}

// TestTutorialCmdHelp tests the tutorial command help
func TestTutorialCmdHelp(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"tutorial", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("tutorial --help failed: %v", err)
	}
}

// TestDashboardCmdHelp tests the dashboard command help
func TestDashboardCmdHelp(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"dashboard", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("dashboard --help failed: %v", err)
	}
}

// TestPaletteCmdHelp tests the palette command help
func TestPaletteCmdHelp(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"palette", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("palette --help failed: %v", err)
	}
}

// TestQuickCmdRequiresName tests quick command requires project name
func TestQuickCmdRequiresName(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"quick"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for quick without project name")
	}
}

// TestUpgradeCmdHelp tests the upgrade command help
func TestUpgradeCmdHelp(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"upgrade", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("upgrade --help failed: %v", err)
	}
}

// TestCreateCmdRequiresName tests create command requires session name
func TestCreateCmdRequiresName(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"create"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for create without session name")
	}
}

// TestBindCmdHelp tests the bind command help
func TestBindCmdHelp(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"bind", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("bind --help failed: %v", err)
	}
}

// TestCommandAliases tests command aliases work
func TestCommandAliases(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	aliases := []struct {
		alias   string
		command string
	}{
		{"ls", "list"},
		{"l", "list"},
		{"a", "attach"},
	}

	for _, a := range aliases {
		t.Run(a.alias, func(t *testing.T) {
			resetFlags()
			rootCmd.SetArgs([]string{a.alias})

			// These should not error on parsing
			err := rootCmd.Execute()
			// May error due to missing args or no sessions, but shouldn't fail on alias
			_ = err
		})
	}
}

// TestEnvVarConfig tests that environment variables are respected
func TestEnvVarConfig(t *testing.T) {
	// Test that XDG_CONFIG_HOME affects config path
	original := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", original)

	testDir := "/tmp/ntm_test_config"
	os.Setenv("XDG_CONFIG_HOME", testDir)

	path := config.DefaultPath()
	if !strings.HasPrefix(path, testDir) {
		t.Errorf("Expected config path to start with %s, got: %s", testDir, path)
	}
}

// TestInterruptCmdRequiresSession tests interrupt command requires session name
func TestInterruptCmdRequiresSession(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"interrupt"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for interrupt without session name")
	}
}

// TestDepsVerboseExecutes tests deps command with verbose flag
func TestDepsVerboseExecutes(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"deps", "-v"})

	// Should execute without panicking
	_ = rootCmd.Execute()
}

// TestConfigInitCreatesFile tests config init creates a config file
func TestConfigInitCreatesFile(t *testing.T) {
	// Use temp dir for config
	original := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", original)

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)

	resetFlags()
	rootCmd.SetArgs([]string{"config", "init"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("config init failed: %v", err)
	}

	// Check file exists
	expectedPath := tmpDir + "/ntm/config.toml"
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Expected config file at %s", expectedPath)
	}
}

// TestStatusCmdNonExistentSession tests status with non-existent session
func TestStatusCmdNonExistentSession(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	cfg = config.Default()
	resetFlags()
	rootCmd.SetArgs([]string{"status", "nonexistent_session_12345"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for non-existent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

// TestRobotSendRequiresMsg tests robot-send requires --msg
func TestRobotSendRequiresMsg(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"--robot-send", "testsession"})

	// Command should execute but exit with error about missing msg
	// The error is handled internally by printing to stderr and os.Exit
	// We can't easily test this without capturing os.Exit
	_ = rootCmd.Execute()
}

// TestRobotSnapshotWithSince tests robot-snapshot with --since flag
func TestRobotSnapshotWithSince(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	resetFlags()
	rootCmd.SetArgs([]string{"--robot-snapshot", "--since", "2025-01-01T00:00:00Z"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestRobotSnapshotInvalidSince tests robot-snapshot with invalid --since
func TestRobotSnapshotInvalidSince(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"--robot-snapshot", "--since", "invalid-timestamp"})

	// Command handles this internally with os.Exit, so we can't catch the error easily
	// But it shouldn't panic
	_ = rootCmd.Execute()
}

// TestRobotTailExecutes tests robot-tail flag executes
func TestRobotTailExecutes(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	resetFlags()
	rootCmd.SetArgs([]string{"--robot-tail", "nonexistent_session_xyz"})

	// Will error because session doesn't exist, but shouldn't panic
	_ = rootCmd.Execute()
}

// TestRobotTailWithLines tests robot-tail with --lines flag
func TestRobotTailWithLines(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	resetFlags()
	rootCmd.SetArgs([]string{"--robot-tail", "nonexistent", "--lines", "50"})

	// Will error because session doesn't exist
	_ = rootCmd.Execute()
}

// TestGlobalJSONFlag tests the global --json flag works
func TestGlobalJSONFlag(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	resetFlags()
	rootCmd.SetArgs([]string{"--json", "list"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestGlobalConfigFlag tests the global --config flag parses
func TestGlobalConfigFlag(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs([]string{"--config", "/nonexistent/config.toml", "version"})

	// Should still work even with nonexistent config (falls back to defaults)
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

// TestMultipleSubcommands tests various subcommand combinations
func TestMultipleSubcommands(t *testing.T) {
	helpCommands := []string{
		"spawn --help",
		"add --help",
		"send --help",
		"create --help",
		"quick --help",
		"view --help",
		"zoom --help",
		"copy --help",
		"save --help",
		"kill --help",
		"attach --help",
		"list --help",
		"status --help",
		"config --help",
	}

	for _, cmd := range helpCommands {
		t.Run(cmd, func(t *testing.T) {
			resetFlags()
			args := strings.Split(cmd, " ")
			rootCmd.SetArgs(args)

			err := rootCmd.Execute()
			if err != nil {
				t.Fatalf("%s failed: %v", cmd, err)
			}
		})
	}
}
