//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// robot_format_test.go validates --robot-format selection for JSON/TOON/auto.
//
// Bead: bd-1a6c4 - Task: E2E robot-format selection (json/toon/auto)
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/history"
	"github.com/Dicklesworthstone/ntm/internal/robot"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

type robotBoundaryProcessResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
}

type robotProcessFixture struct {
	ntmPath    string
	tmuxPath   string
	session    string
	root       string
	projectDir string
	configDir  string
	dataDir    string
	env        []string
	paneID     string
}

func runBuiltRobotProcess(t *testing.T, ntmPath, dir string, env []string, args ...string) robotBoundaryProcessResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ntmPath, args...)
	// A restarted pane may leave a descendant holding inherited output
	// descriptors after CommandContext kills ntm. Bound the post-cancel pipe
	// drain so a failed process assertion cannot hang the entire E2E package.
	cmd.WaitDelay = 2 * time.Second
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = append([]string(nil), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("ntm %q timed out", args)
	}

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run ntm %q: %v", args, err)
		}
		exitCode = exitErr.ExitCode()
	}
	t.Logf("[E2E-ROBOT-PROCESS] exit=%d args=%q stdout=%s stderr=%s", exitCode, args, truncateString(stdout.String(), 500), truncateString(stderr.String(), 500))
	return robotBoundaryProcessResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode}
}

func mergeRobotProcessEnv(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func decodeSingleRobotJSON(t *testing.T, payload []byte, destination any) {
	t.Helper()
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		t.Fatalf("expected exactly one valid JSON document, got %q", payload)
	}
	if err := json.Unmarshal(trimmed, destination); err != nil {
		t.Fatalf("decode robot JSON: %v\npayload=%s", err, payload)
	}
}

func newRobotProcessFixture(t *testing.T, scenario string) *robotProcessFixture {
	t.Helper()
	testutil.RequireTmuxThrottled(t)

	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	tmuxPath, err := exec.LookPath(tmux.BinaryPath())
	if err != nil {
		t.Fatalf("resolve tmux: %v", err)
	}

	root := t.TempDir()
	fixture := &robotProcessFixture{
		ntmPath:    ntmPath,
		tmuxPath:   tmuxPath,
		session:    fmt.Sprintf("ntm-e2e-%s-%d-%d", scenario, os.Getpid(), time.Now().UnixNano()),
		root:       root,
		projectDir: filepath.Join(root, "project"),
		configDir:  filepath.Join(root, "config"),
		dataDir:    filepath.Join(root, "data"),
	}
	homeDir := filepath.Join(root, "home")
	// tmux's Unix socket path is capped at roughly 108 bytes. Keep its private
	// root short even when Go's per-test temporary directory is deeply nested.
	tmuxDir := filepath.Join("/tmp", fmt.Sprintf("ntm-rp-%d-%d", os.Getpid(), time.Now().UnixNano()))
	for _, dir := range []string{fixture.projectDir, fixture.configDir, fixture.dataDir, homeDir, tmuxDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create process fixture directory %s: %v", dir, err)
		}
	}
	fixture.env = atomicAssignmentIsolatedEnv(map[string]string{
		"HOME":                homeDir,
		"XDG_CONFIG_HOME":     fixture.configDir,
		"XDG_DATA_HOME":       fixture.dataDir,
		"TMUX_TMPDIR":         tmuxDir,
		"NO_COLOR":            "1",
		"TERM":                "xterm-256color",
		"NTM_OUTPUT_FORMAT":   "",
		"NTM_ROBOT_FORMAT":    "",
		"TOON_DEFAULT_FORMAT": "",
	})

	tmuxConfig := filepath.Join(root, "tmux.conf")
	config := strings.Join([]string{
		"set -g base-index 0",
		"setw -g pane-base-index 0",
		"set -g status off",
		"setw -g allow-rename off",
		"setw -g automatic-rename off",
		"",
	}, "\n")
	if err := os.WriteFile(tmuxConfig, []byte(config), 0o600); err != nil {
		t.Fatalf("write tmux config: %v", err)
	}
	fixture.paneID = strings.TrimSpace(fixture.mustTMUXOutput(t,
		"-f", tmuxConfig,
		"new-session", "-d", "-s", fixture.session,
		"-x", "160", "-y", "48", "-c", fixture.projectDir,
		"-P", "-F", "#{pane_id}",
		"/bin/bash --noprofile --norc -i",
	))
	if fixture.paneID == "" {
		t.Fatal("private tmux server returned an empty pane ID")
	}
	fixture.mustTMUXOutput(t, "select-pane", "-t", fixture.paneID, "-T", fixture.session+"__cod_1")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, fixture.tmuxPath, "kill-server")
		cmd.Env = append([]string(nil), fixture.env...)
		_ = cmd.Run()
	})
	return fixture
}

func (f *robotProcessFixture) mustTMUXOutput(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.tmuxPath, args...)
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("tmux %q timed out", args)
	}
	if err != nil {
		t.Fatalf("tmux %q: %v output=%s", args, err, output)
	}
	return string(output)
}

func (f *robotProcessFixture) waitForFileContents(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), want) {
			return
		}
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read %s: %v", path, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q in %s", want, path)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// runRobotFormatCmd executes an ntm robot command with a specific format.
func runRobotFormatCmd(t *testing.T, suite *TestSuite, format string, cmd string, args ...string) []byte {
	t.Helper()

	fullArgs := append([]string{fmt.Sprintf("--robot-format=%s", format)}, args...)
	command := exec.Command("ntm", fullArgs...)
	output, err := command.CombinedOutput()

	suite.Logger().Log("[E2E-ROBOT-FORMAT] format=%s cmd=%s bytes=%d", format, cmd, len(output))

	if err != nil {
		suite.Logger().Log("[E2E-ROBOT-FORMAT] error cmd=%s err=%v output=%s", cmd, err, string(output))
		t.Fatalf("[E2E-ROBOT-FORMAT] Command failed: %v", err)
	}

	return output
}

func parseJSONOrFail(t *testing.T, output []byte) map[string]interface{} {
	t.Helper()
	var parsed map[string]interface{}
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("[E2E-ROBOT-FORMAT] JSON parse failed: %v output=%s", err, string(output))
	}
	return parsed
}

func TestE2E_RobotFormatSelection(t *testing.T) {
	CommonE2EPrerequisites(t)
	if !supportsRobotFormat(t) {
		t.Skip("ntm --robot-format not supported by current binary")
	}

	suite := NewTestSuite(t, "robot_format")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-ROBOT-FORMAT] Setup failed: %v", err)
	}

	session := suite.Session()

	t.Run("status_json", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "json", "robot-status", "--robot-status")
		parsed := parseJSONOrFail(t, output)

		if _, ok := parsed["sessions"]; !ok {
			t.Fatalf("[E2E-ROBOT-FORMAT] status JSON missing sessions: %v", parsed)
		}
	})

	t.Run("status_toon", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "toon", "robot-status", "--robot-status")

		trimmed := strings.TrimSpace(string(output))
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			if json.Valid([]byte(trimmed)) {
				t.Fatalf("[E2E-ROBOT-FORMAT] status TOON should not be valid JSON, got: %s", string(output))
			}
		}
		if !strings.Contains(string(output), "sessions") {
			t.Fatalf("[E2E-ROBOT-FORMAT] status TOON missing sessions field: %s", string(output))
		}
	})

	t.Run("status_auto_defaults_json", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "auto", "robot-status", "--robot-status")
		parseJSONOrFail(t, output)
		suite.Logger().Log("[E2E-ROBOT-FORMAT] fallback=%t reason=%s", true, "auto defaults to JSON")
	})

	t.Run("assign_json", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "json", "robot-assign", fmt.Sprintf("--robot-assign=%s", session))
		parsed := parseJSONOrFail(t, output)

		if parsed["session"] != session {
			t.Fatalf("[E2E-ROBOT-FORMAT] assign JSON session mismatch: got=%v want=%s", parsed["session"], session)
		}
		if _, ok := parsed["strategy"]; !ok {
			t.Fatalf("[E2E-ROBOT-FORMAT] assign JSON missing strategy: %v", parsed)
		}
	})

	t.Run("assign_toon", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "toon", "robot-assign", fmt.Sprintf("--robot-assign=%s", session))
		if !strings.Contains(string(output), "session:") {
			t.Fatalf("[E2E-ROBOT-FORMAT] assign TOON missing session: %s", string(output))
		}
		if !strings.Contains(string(output), "strategy:") {
			t.Fatalf("[E2E-ROBOT-FORMAT] assign TOON missing strategy: %s", string(output))
		}
	})

	t.Run("assign_auto_defaults_json", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "auto", "robot-assign", fmt.Sprintf("--robot-assign=%s", session))
		parseJSONOrFail(t, output)
	})
}

func TestE2ERobotCapabilitiesBuiltBinaryBudgets(t *testing.T) {
	CommonE2EPrerequisites(t)
	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}

	type capabilityCommand struct {
		Name     string `json:"name"`
		Flag     string `json:"flag"`
		Category string `json:"category"`
	}
	type capabilityEnvelope struct {
		Success      bool                `json:"success"`
		OutputFormat string              `json:"output_format"`
		Commands     []capabilityCommand `json:"commands"`
		Categories   []string            `json:"categories"`
		Filter       *struct {
			Command  string `json:"command,omitempty"`
			Category string `json:"category,omitempty"`
			Query    string `json:"query,omitempty"`
			Compact  bool   `json:"compact,omitempty"`
		} `json:"filter,omitempty"`
	}
	run := func(t *testing.T, maxBytes int, args ...string) capabilityEnvelope {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, ntmPath, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("ntm %q: %v stdout=%s stderr=%s", args, err, stdout.Bytes(), stderr.Bytes())
		}
		if ctx.Err() != nil {
			t.Fatalf("ntm %q timed out: %v", args, ctx.Err())
		}
		if stderr.Len() != 0 {
			t.Fatalf("ntm %q wrote stderr: %s", args, stderr.Bytes())
		}
		payload := bytes.TrimSpace(stdout.Bytes())
		if len(payload) == 0 || len(payload) >= maxBytes {
			t.Fatalf("ntm %q payload size=%d, require 0 < size < %d", args, len(payload), maxBytes)
		}
		if !json.Valid(payload) {
			t.Fatalf("ntm %q did not emit one valid JSON document: %s", args, payload)
		}
		var envelope capabilityEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatalf("decode ntm %q capabilities: %v", args, err)
		}
		if !envelope.Success || envelope.Commands == nil {
			t.Fatalf("ntm %q capabilities envelope = %+v", args, envelope)
		}
		t.Logf("[E2E-CAPABILITIES] args=%q bytes=%d commands=%d", args, len(payload), len(envelope.Commands))
		return envelope
	}
	assertInvalidFilter := func(t *testing.T, args ...string) {
		t.Helper()
		process := runBuiltRobotProcess(t, ntmPath, "", nil, args...)
		if process.exitCode != 1 {
			t.Fatalf("ntm %q exit=%d, want 1; stdout=%s stderr=%s", args, process.exitCode, process.stdout, process.stderr)
		}
		if len(process.stderr) != 0 {
			t.Fatalf("ntm %q wrote stderr: %s", args, process.stderr)
		}
		var envelope struct {
			Success      *bool  `json:"success"`
			Timestamp    string `json:"timestamp"`
			OutputFormat string `json:"output_format"`
			Error        string `json:"error"`
			ErrorCode    string `json:"error_code"`
			Hint         string `json:"hint"`
		}
		decodeSingleRobotJSON(t, process.stdout, &envelope)
		if envelope.Success == nil || *envelope.Success || envelope.ErrorCode != "INVALID_FLAG" ||
			envelope.Error == "" || envelope.Hint == "" || envelope.OutputFormat != "json" {
			t.Fatalf("ntm %q invalid capability filter envelope = %+v", args, envelope)
		}
		if _, err := time.Parse(time.RFC3339, envelope.Timestamp); err != nil {
			t.Fatalf("ntm %q timestamp %q is not RFC3339: %v", args, envelope.Timestamp, err)
		}
	}

	full := run(t, 400_000, "--robot-capabilities", "--robot-format=json")
	compact := run(t, 50_000, "--robot-capabilities", "--capability-compact")
	exact := run(t, 4_000, "--robot-capabilities", "--capability-command=send", "--capability-compact")
	search := run(t, 50_000, "--robot-capabilities", "--capability-search=interrupt", "--capability-compact")
	category := run(t, 50_000, "--robot-capabilities", "--capability-category=control", "--capability-compact")

	if len(full.Commands) == 0 || len(compact.Commands) != len(full.Commands) {
		t.Fatalf("capability command counts full=%d compact=%d", len(full.Commands), len(compact.Commands))
	}
	if len(exact.Commands) != 1 || exact.Commands[0].Name != "send" || exact.Commands[0].Flag != "--robot-send" ||
		exact.Filter == nil || exact.Filter.Command != "send" || !exact.Filter.Compact {
		t.Fatalf("exact compact capability projection = %+v", exact)
	}
	if compact.OutputFormat != "json" || exact.OutputFormat != "json" {
		t.Fatalf("auto compact capability formats compact=%q exact=%q, want json", compact.OutputFormat, exact.OutputFormat)
	}
	interruptFound := false
	for _, command := range search.Commands {
		if command.Name == "interrupt" && command.Flag == "--robot-interrupt" && command.Category == "control" {
			interruptFound = true
			break
		}
	}
	if search.Filter == nil || search.Filter.Query != "interrupt" || !search.Filter.Compact ||
		len(search.Commands) == 0 || len(search.Commands) >= len(compact.Commands) || !interruptFound {
		t.Fatalf("compact capability search projection = %+v", search)
	}
	if category.Filter == nil || category.Filter.Category != "control" || !category.Filter.Compact ||
		len(category.Commands) == 0 || len(category.Commands) >= len(compact.Commands) ||
		len(category.Categories) != 1 || category.Categories[0] != "control" {
		t.Fatalf("compact capability category projection = %+v", category)
	}
	for _, command := range category.Commands {
		if command.Category != "control" {
			t.Fatalf("compact control projection included command %+v", command)
		}
	}
	assertInvalidFilter(t, "--robot-capabilities", "--capability-command=not-a-command", "--capability-compact")
	assertInvalidFilter(t, "--robot-capabilities", "--capability-category=not-a-category", "--capability-compact")
}

func TestE2ERobotTerminalErrorContractsBuiltBinary(t *testing.T) {
	CommonE2EPrerequisites(t)
	fixture := newRobotProcessFixture(t, "terminal-errors")

	type terminalEnvelope struct {
		Success      bool   `json:"success"`
		Timestamp    string `json:"timestamp"`
		OutputFormat string `json:"output_format"`
		ErrorCode    string `json:"error_code"`
	}
	assertFailure := func(t *testing.T, process robotBoundaryProcessResult, wantExit int, wantCode string) terminalEnvelope {
		t.Helper()
		if process.exitCode != wantExit {
			t.Fatalf("exit=%d, want %d; stdout=%s stderr=%s", process.exitCode, wantExit, process.stdout, process.stderr)
		}
		if len(process.stderr) != 0 {
			t.Fatalf("stderr=%q, want empty canonical robot boundary", process.stderr)
		}
		var envelope terminalEnvelope
		decodeSingleRobotJSON(t, process.stdout, &envelope)
		if envelope.Success || envelope.ErrorCode != wantCode || envelope.OutputFormat != "json" {
			t.Fatalf("terminal envelope = %+v, want %s canonical JSON failure", envelope, wantCode)
		}
		if _, err := time.Parse(time.RFC3339, envelope.Timestamp); err != nil {
			t.Fatalf("terminal timestamp %q is not RFC3339: %v", envelope.Timestamp, err)
		}
		return envelope
	}
	assertSuccess := func(t *testing.T, process robotBoundaryProcessResult) terminalEnvelope {
		t.Helper()
		if process.exitCode != 0 {
			t.Fatalf("exit=%d, want 0; stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
		}
		if len(process.stderr) != 0 {
			t.Fatalf("stderr=%q, want empty canonical robot boundary", process.stderr)
		}
		var envelope terminalEnvelope
		decodeSingleRobotJSON(t, process.stdout, &envelope)
		if !envelope.Success || envelope.ErrorCode != "" || envelope.OutputFormat != "json" {
			t.Fatalf("terminal envelope = %+v, want canonical JSON success", envelope)
		}
		if _, err := time.Parse(time.RFC3339, envelope.Timestamp); err != nil {
			t.Fatalf("terminal timestamp %q is not RFC3339: %v", envelope.Timestamp, err)
		}
		return envelope
	}
	run := func(t *testing.T, env []string, args ...string) robotBoundaryProcessResult {
		t.Helper()
		return runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, env, args...)
	}

	t.Run("markdown_validation_under_toon", func(t *testing.T) {
		process := run(t, fixture.env,
			"--robot-markdown",
			"--sections=not-a-section",
			"--robot-format=toon",
		)
		assertFailure(t, process, 1, "INTERNAL_ERROR")
	})

	t.Run("terse_pre_dispatch_format_failure", func(t *testing.T) {
		process := run(t, fixture.env, "--robot-terse", "--robot-format=invalid")
		assertFailure(t, process, 1, "INVALID_FLAG")
	})

	t.Run("terse_runtime_dependency_failure_under_toon", func(t *testing.T) {
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"NTM_TMUX_BINARY": filepath.Join(fixture.root, "missing-bin", "tmux"),
		})
		process := run(t, env, "--robot-terse", "--robot-format=toon")
		assertFailure(t, process, 1, "DEPENDENCY_MISSING")
	})

	t.Run("save_write_failure_under_toon", func(t *testing.T) {
		process := run(t, fixture.env,
			"--robot-save="+fixture.session,
			"--output="+fixture.root,
			"--robot-format=toon",
		)
		assertFailure(t, process, 1, "INTERNAL_ERROR")
	})

	t.Run("deprecated_save_output_does_not_pollute_stderr", func(t *testing.T) {
		process := run(t, fixture.env,
			"--robot-save="+fixture.session,
			"--save-output="+fixture.root,
			"--robot-format=json",
		)
		assertFailure(t, process, 1, "INTERNAL_ERROR")
	})

	t.Run("restore_missing_state_under_toon", func(t *testing.T) {
		process := run(t, fixture.env,
			"--robot-restore="+filepath.Join(fixture.root, "missing-state.json"),
			"--robot-format=toon",
		)
		assertFailure(t, process, 1, "INTERNAL_ERROR")
	})

	t.Run("dcg_dependency_is_error_not_unavailable", func(t *testing.T) {
		env := mergeRobotProcessEnv(fixture.env, map[string]string{"PATH": "/usr/bin:/bin"})
		process := run(t, env,
			"--robot-dcg-check",
			"--command=echo safe",
			"--robot-format=toon",
		)
		assertFailure(t, process, 1, "DEPENDENCY_MISSING")
	})

	t.Run("bv_runtime_failures_are_one_typed_document", func(t *testing.T) {
		for _, test := range []struct {
			name string
			body string
		}{
			{name: "command failure", body: "#!/bin/sh\nprintf 'bv exploded' >&2\nexit 7\n"},
			{name: "malformed JSON", body: "#!/bin/sh\nprintf '{malformed'\n"},
		} {
			t.Run(test.name, func(t *testing.T) {
				fakeDir := t.TempDir()
				fakeBV := filepath.Join(fakeDir, "bv")
				if err := os.WriteFile(fakeBV, []byte(test.body), 0o700); err != nil {
					t.Fatalf("write fake bv: %v", err)
				}
				env := mergeRobotProcessEnv(fixture.env, map[string]string{
					"PATH": fakeDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				})
				process := run(t, env, "--robot-forecast=all", "--robot-format=json")
				assertFailure(t, process, 1, "INTERNAL_ERROR")
				var detailed struct {
					Error string `json:"error"`
					Hint  string `json:"hint"`
				}
				if err := json.Unmarshal(process.stdout, &detailed); err != nil {
					t.Fatalf("decode detailed bv failure: %v", err)
				}
				if strings.TrimSpace(detailed.Error) == "" || strings.TrimSpace(detailed.Hint) == "" {
					t.Fatalf("bv failure missing remediation detail: %+v", detailed)
				}
			})
		}
	})

	t.Run("attention_timeout_under_toon", func(t *testing.T) {
		process := run(t, fixture.env,
			"--robot-attention",
			"--attention-condition=mail_pending",
			"--attention-timeout=20ms",
			"--attention-poll=5ms",
			"--robot-format=toon",
		)
		assertFailure(t, process, 1, "TIMEOUT")
	})

	t.Run("not_implemented_is_the_only_unavailable_exit", func(t *testing.T) {
		process := run(t, fixture.env,
			"--robot-ensemble-spawn=terminal-contract",
			"--robot-format=toon",
		)
		assertFailure(t, process, 2, "NOT_IMPLEMENTED")
	})

	t.Run("unresponsive_probe_under_toon", func(t *testing.T) {
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "-l", "stty -echo; sleep 30")
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "Enter")
		deadline := time.Now().Add(5 * time.Second)
		for {
			command := strings.TrimSpace(fixture.mustTMUXOutput(t, "display-message", "-p", "-t", fixture.paneID, "#{pane_current_command}"))
			if command == "sleep" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("pane command=%q, want sleep", command)
			}
			time.Sleep(25 * time.Millisecond)
		}
		process := run(t, fixture.env,
			"--robot-probe="+fixture.session,
			"--panes=0",
			"--probe-timeout=100",
			"--robot-format=toon",
		)
		assertFailure(t, process, 1, "TIMEOUT")
	})

	t.Run("invalid_project_overlay_warning_is_suppressed", func(t *testing.T) {
		projectDir := filepath.Join(fixture.root, "invalid-project-overlay")
		configDir := filepath.Join(projectDir, ".ntm")
		if err := os.MkdirAll(configDir, 0o700); err != nil {
			t.Fatalf("create invalid project config directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[invalid\n"), 0o600); err != nil {
			t.Fatalf("write invalid project config: %v", err)
		}
		process := runBuiltRobotProcess(t, fixture.ntmPath, projectDir, fixture.env,
			"--robot-status",
			"--robot-format=json",
		)
		assertSuccess(t, process)
	})

	t.Run("verbose_startup_cleanup_warning_is_suppressed", func(t *testing.T) {
		configPath := filepath.Join(fixture.root, "verbose-cleanup.toml")
		configBody := "[cleanup]\nauto_clean_on_startup = true\nmax_age_hours = 0\nverbose = true\n"
		if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
			t.Fatalf("write verbose cleanup config: %v", err)
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"TMPDIR": filepath.Join(fixture.root, "missing-temp-root"),
		})
		process := run(t, env,
			"--config="+configPath,
			"--robot-status",
			"--robot-format=json",
		)
		assertSuccess(t, process)
	})

	t.Run("encryption_key_failure_is_one_error_envelope", func(t *testing.T) {
		configPath := filepath.Join(fixture.root, "missing-encryption-key.toml")
		configBody := "[encryption]\nenabled = true\nkey_source = \"env\"\nkey_env = \"NTM_E2E_MISSING_ENCRYPTION_KEY\"\nkey_format = \"hex\"\n"
		if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
			t.Fatalf("write encryption config: %v", err)
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"NTM_E2E_MISSING_ENCRYPTION_KEY": "",
		})
		process := run(t, env,
			"--config="+configPath,
			"--robot-status",
			"--robot-format=json",
		)
		assertFailure(t, process, 1, "INTERNAL_ERROR")
	})

	t.Run("encryption_keyring_failure_is_one_error_envelope", func(t *testing.T) {
		configPath := filepath.Join(fixture.root, "invalid-encryption-keyring.toml")
		configBody := "[encryption]\nenabled = true\nkey_source = \"env\"\nkey_env = \"NTM_E2E_ENCRYPTION_KEY\"\nkey_format = \"hex\"\nactive_key_id = \"current\"\n[encryption.keyring]\ncurrent = \"not-hex\"\n"
		if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
			t.Fatalf("write invalid encryption keyring config: %v", err)
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"NTM_E2E_ENCRYPTION_KEY": strings.Repeat("00", 32),
		})
		process := run(t, env,
			"--config="+configPath,
			"--robot-status",
			"--robot-format=json",
		)
		assertFailure(t, process, 1, "INTERNAL_ERROR")
	})

	t.Run("startup_profile_does_not_append_a_second_document", func(t *testing.T) {
		process := run(t, fixture.env,
			"--profile-startup",
			"--robot-status",
			"--robot-format=json",
		)
		assertSuccess(t, process)
	})
}

func TestE2EHealthAutoRestartUsesEffectiveAgentConfig(t *testing.T) {
	CommonE2EPrerequisites(t)
	fixture := newRobotProcessFixture(t, "health-configured-restart")
	markerPath := filepath.Join(fixture.root, "configured-codex-launched")
	agentPath := filepath.Join(fixture.root, "configured-codex")
	agentScript := fmt.Sprintf("#!/bin/sh\nprintf 'Codex> \\n100%%%% context left\\n'\nprintf 'configured\\n' > %q\nwhile IFS= read -r line; do\n  printf 'received: %%s\\nCodex> \\n100%%%% context left\\n' \"$line\"\ndone\n", markerPath)
	if err := os.WriteFile(agentPath, []byte(agentScript), 0o700); err != nil {
		t.Fatalf("write configured Codex agent: %v", err)
	}
	configPath := filepath.Join(fixture.root, "configured-health.toml")
	configBody := fmt.Sprintf("[agents]\ncodex = %q\n\n[spawn_pacing]\nenabled = false\n", agentPath)
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatalf("write configured health file: %v", err)
	}
	// The command intentionally enforces a 30-second minimum threshold. Keep
	// the pane genuinely quiet long enough to exercise production detection.
	time.Sleep(31 * time.Second)

	tmuxWrapper := filepath.Join(fixture.root, "tmux")
	tmuxWrapperScript := `#!/bin/sh
if [ "${1:-}" = "respawn-pane" ]; then
  printf 'injected respawn failure\n' >&2
  exit 72
fi
exec "$NTM_E2E_REAL_TMUX" "$@"
`
	if err := os.WriteFile(tmuxWrapper, []byte(tmuxWrapperScript), 0o700); err != nil {
		t.Fatalf("write failing tmux wrapper: %v", err)
	}
	failureEnv := mergeRobotProcessEnv(fixture.env, map[string]string{
		"NTM_TMUX_BINARY":   tmuxWrapper,
		"NTM_E2E_REAL_TMUX": fixture.tmuxPath,
	})
	failedProcess := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, failureEnv,
		"--config="+configPath,
		"--robot-health-restart-stuck="+fixture.session,
		"--stuck-threshold=30s",
		"--robot-format=json",
	)
	if failedProcess.exitCode != 1 || len(bytes.TrimSpace(failedProcess.stderr)) != 0 {
		t.Fatalf("failed health restart exit=%d stdout=%s stderr=%s", failedProcess.exitCode, failedProcess.stdout, failedProcess.stderr)
	}
	var failedOutput struct {
		Success      bool   `json:"success"`
		OutputFormat string `json:"output_format"`
		Error        string `json:"error"`
		ErrorCode    string `json:"error_code"`
		StuckPanes   []int  `json:"stuck_panes"`
		Restarted    []int  `json:"restarted"`
		Failed       []int  `json:"failed"`
	}
	decodeSingleRobotJSON(t, failedProcess.stdout, &failedOutput)
	if failedOutput.Success || failedOutput.OutputFormat != "json" || failedOutput.ErrorCode != "INTERNAL_ERROR" || failedOutput.Error == "" ||
		!slices.Equal(failedOutput.StuckPanes, []int{0}) || len(failedOutput.Restarted) != 0 || !slices.Equal(failedOutput.Failed, []int{0}) {
		t.Fatalf("failed health restart output=%+v", failedOutput)
	}

	process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env,
		"--config="+configPath,
		"--robot-health-restart-stuck="+fixture.session,
		"--stuck-threshold=30s",
		"--robot-format=json",
	)
	if process.exitCode != 0 || len(bytes.TrimSpace(process.stderr)) != 0 {
		t.Fatalf("configured health restart exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
	}
	var output struct {
		Success    bool  `json:"success"`
		StuckPanes []int `json:"stuck_panes"`
		Restarted  []int `json:"restarted"`
		Failed     []int `json:"failed"`
	}
	decodeSingleRobotJSON(t, process.stdout, &output)
	if !output.Success || !slices.Equal(output.StuckPanes, []int{0}) || !slices.Equal(output.Restarted, []int{0}) || len(output.Failed) != 0 {
		t.Fatalf("configured health restart output=%+v", output)
	}
	fixture.waitForFileContents(t, markerPath, "configured")
	deadline := time.Now().Add(5 * time.Second)
	for {
		captured := fixture.mustTMUXOutput(t, "capture-pane", "-p", "-t", fixture.paneID, "-S", "-")
		if strings.Contains(captured, "Codex>") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("configured Codex agent did not reach ready state:\n%s", captured)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestE2EUpgradeJSONRequiresCheckBuiltBinary(t *testing.T) {
	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	process := runBuiltRobotProcess(t, ntmPath, t.TempDir(), os.Environ(), "--json", "upgrade")
	if process.exitCode != 1 || len(bytes.TrimSpace(process.stderr)) != 0 {
		t.Fatalf("upgrade JSON validation exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
	}
	var output struct {
		Success      bool   `json:"success"`
		OutputFormat string `json:"output_format"`
		Error        string `json:"error"`
		ErrorCode    string `json:"error_code"`
	}
	decodeSingleRobotJSON(t, process.stdout, &output)
	if output.Success || output.OutputFormat != "json" || output.ErrorCode != "INVALID_FLAG" || output.Error == "" {
		t.Fatalf("upgrade JSON validation output=%+v", output)
	}
}

func TestE2EGlobalJSONOverridesLocalFormatsBuiltBinary(t *testing.T) {
	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}

	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	configDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	fakeBin := filepath.Join(root, "bin")
	repoDir := filepath.Join(root, "repo")
	for _, dir := range []string{homeDir, configDir, dataDir, fakeBin, repoDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create precedence fixture directory %s: %v", dir, err)
		}
	}

	scanDir := filepath.Join(root, "scan")
	if err := os.MkdirAll(scanDir, 0o700); err != nil {
		t.Fatalf("create scrub precedence directory: %v", err)
	}
	bvArgsPath := filepath.Join(root, "bv-args")
	bvScript := `#!/bin/sh
printf '%s\n' "$*" >> "$NTM_E2E_BV_ARGS"
case "${1:-}" in
  --robot-triage)
    printf '{"generated_at":"2026-07-14T00:00:00Z","data_hash":"fixture","triage":{"meta":{"version":"fixture","generated_at":"2026-07-14T00:00:00Z"},"quick_ref":{"open_count":0,"actionable_count":0,"blocked_count":0,"in_progress_count":0,"top_picks":[]},"recommendations":[]}}\n'
    ;;
  *)
    printf '{"nodes":[],"edges":[]}\n'
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(fakeBin, "bv"), []byte(bvScript), 0o700); err != nil {
		t.Fatalf("write deterministic bv wrapper: %v", err)
	}
	brScript := "#!/bin/sh\nprintf '[]\\n'\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "br"), []byte(brScript), 0o700); err != nil {
		t.Fatalf("write deterministic br wrapper: %v", err)
	}
	env := atomicAssignmentIsolatedEnv(map[string]string{
		"HOME":            homeDir,
		"XDG_CONFIG_HOME": configDir,
		"XDG_DATA_HOME":   dataDir,
		"NO_COLOR":        "1",
		"PATH":            fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"NTM_E2E_BV_ARGS": bvArgsPath,
	})

	initCmd := exec.Command("git", "init", "-b", "main")
	initCmd.Dir = repoDir
	initCmd.Env = append([]string(nil), env...)
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("initialize precedence git fixture: %v: %s", err, output)
	}
	commitCmd := exec.Command(
		"git", "-c", "user.name=NTM E2E", "-c", "user.email=ntm-e2e@example.invalid",
		"commit", "--allow-empty", "-m", "fixture",
	)
	commitCmd.Dir = repoDir
	commitCmd.Env = append([]string(nil), env...)
	if output, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("commit precedence git fixture: %v: %s", err, output)
	}

	for _, test := range []struct {
		name          string
		args          []string
		emptyArrays   []string
		wantRootArray bool
		isolateTools  bool
	}{
		{name: "analytics_csv", args: []string{"--json", "analytics", "--format=csv"}},
		{name: "analytics_prometheus", args: []string{"--json", "analytics", "--format=prometheus"}},
		{name: "analytics_uppercase_json", args: []string{"analytics", "--format=JSON"}},
		{name: "analytics_precommand_format", args: []string{"--format=json", "analytics"}},
		{name: "approve_list", args: []string{"approve", "list", "--json"}, emptyArrays: []string{"pending"}},
		{name: "audit_csv", args: []string{"--json", "audit", "export", "missing-session", "--format=csv"}},
		{name: "audit_uppercase_json", args: []string{"audit", "export", "missing-session", "--format=JSON"}},
		{name: "handoff_markdown", args: []string{"--json", "handoff", "create", "--goal=completed", "--now=continue", "--output=-", "--format=markdown", "--include-git=false"}},
		{name: "handoff_uppercase_json", args: []string{"handoff", "create", "--goal=completed", "--now=continue", "--output=-", "--format=JSON", "--include-git=false"}},
		{name: "metrics_prometheus", args: []string{"--json", "metrics", "export", "--format=prometheus"}},
		{name: "metrics_uppercase_json", args: []string{"metrics", "export", "--format=JSON"}},
		{name: "numeric_global_json", args: []string{"--json=1", "analytics", "--format=csv"}},
		{name: "root_since_before_json_command", args: []string{"--since", "7d", "summary", "--all", "--format=json"}},
		{name: "rotate_status", args: []string{"rotate", "status", "--data-dir=" + repoDir, "--json"}},
		{name: "scrub_text", args: []string{"--json", "scrub", "--path=" + scanDir, "--format=text"}, emptyArrays: []string{"findings"}},
		{name: "scrub_uppercase_json", args: []string{"scrub", "--path=" + scanDir, "--format=JSON"}, emptyArrays: []string{"findings"}},
		{name: "scrub_zero_roots_local_json", args: []string{"scrub", "--format=json"}, emptyArrays: []string{"findings"}},
		{name: "summary_empty", args: []string{"--json", "summary", "--all"}},
		{name: "work_commit_ready_text", args: []string{"--json", "work", "commit-ready", "--format=text"}, emptyArrays: []string{"findings"}},
		{name: "work_commit_readiness_alias_text", args: []string{"--json", "work", "commit-readiness", "--format=text"}, emptyArrays: []string{"findings"}},
		{name: "work_commit_ready_uppercase_json", args: []string{"work", "commit-ready", "--format=JSON"}, emptyArrays: []string{"findings"}},
		{name: "work_graph_dot", args: []string{"--json", "work", "graph", "--format=dot"}},
		{name: "work_graph_mermaid", args: []string{"--json", "work", "graph", "--format=mermaid"}},
		{name: "work_graph_uppercase_json", args: []string{"work", "graph", "--format=JSON"}},
		{name: "work_queue_dry_text", args: []string{"--json", "work", "queue-dry", "--format=text"}},
		{name: "work_queue_dry_markdown", args: []string{"--json", "work", "queue-dry", "--format=markdown"}},
		{name: "work_queue_dry_uppercase_json", args: []string{"work", "queue-dry", "--format=JSON"}},
		{name: "work_triage_markdown", args: []string{"--json", "work", "triage", "--format=markdown"}},
		{name: "work_triage_uppercase_json", args: []string{"work", "triage", "--format=JSON"}},
		{name: "worktree_table", args: []string{"--json", "worktree", "list", "--output=table"}, wantRootArray: true},
		{name: "worktree_uppercase_json", args: []string{"worktree", "list", "--output=JSON"}, wantRootArray: true},
		{name: "worktree_precommand_output", args: []string{"--output=json", "worktree", "list"}, wantRootArray: true},
		{name: "support_bundle", args: []string{"support-bundle", "--output=" + filepath.Join(root, "support-bundle"), "--json"}, isolateTools: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"--profile-startup"}, test.args...)
			processEnv := env
			if test.isolateTools {
				// Doctor data in a support bundle probes every installed tool. Keep
				// this format-precedence test independent of host tools and load.
				processEnv = mergeRobotProcessEnv(env, map[string]string{
					"PATH":            fakeBin,
					"NTM_TMUX_BINARY": filepath.Join(fakeBin, "missing-tmux"),
				})
			}
			process := runBuiltRobotProcess(t, ntmPath, repoDir, processEnv, args...)
			if process.exitCode != 0 || len(bytes.TrimSpace(process.stderr)) != 0 {
				t.Fatalf("global JSON precedence exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
			}
			var document any
			decodeSingleRobotJSON(t, process.stdout, &document)
			if test.wantRootArray {
				if _, ok := document.([]any); !ok {
					t.Fatalf("root document = %#v, want JSON array", document)
				}
			}
			if len(test.emptyArrays) > 0 {
				object, ok := document.(map[string]any)
				if !ok {
					t.Fatalf("root document = %#v, want JSON object", document)
				}
				for _, field := range test.emptyArrays {
					value, ok := object[field].([]any)
					if !ok || len(value) != 0 {
						t.Fatalf("%s = %#v, want empty JSON array", field, object[field])
					}
				}
			}
		})
	}

	for _, test := range []struct {
		name         string
		args         []string
		errorCode    string
		emptyArrays  []string
		outputFormat string
		exitMeta     int
	}{
		{name: "ensemble_id_only", args: []string{"--json", "ensemble", "suggest", "review architecture", "--id-only"}},
		{name: "ensemble_stop_confirmation", args: []string{"--json", "ensemble", "stop"}},
		{name: "handoff_missing_machine_inputs", args: []string{"--json", "handoff", "create", "--format=markdown"}},
		{name: "invalid_json_boolean", args: []string{"--json=bogus", "version"}},
		{name: "review_queue_send", args: []string{"--json", "review-queue", "--send"}},
		{name: "root_nonpersistent_limit", args: []string{"--limit", "5", "ensemble", "presets", "--format=json"}},
		{name: "swarm_empty_scan", args: []string{"--json", "swarm", "--dry-run", "--scan-dir=" + scanDir}, errorCode: robot.ErrCodeNotFound, emptyArrays: []string{"allocations", "sessions"}, outputFormat: "json", exitMeta: 1},
		{name: "swarm_stop_confirmation", args: []string{"swarm", "stop", "--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"--profile-startup"}, test.args...)
			process := runBuiltRobotProcess(t, ntmPath, repoDir, env, args...)
			if process.exitCode == 0 || len(bytes.TrimSpace(process.stderr)) != 0 {
				t.Fatalf("machine incompatibility exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
			}
			var document map[string]any
			decodeSingleRobotJSON(t, process.stdout, &document)
			wantCode := test.errorCode
			if wantCode == "" {
				wantCode = robot.ErrCodeInvalidFlag
			}
			if success, _ := document["success"].(bool); success || document["error"] == "" || document["error_code"] != wantCode {
				t.Fatalf("machine incompatibility document=%+v", document)
			}
			if test.outputFormat != "" && document["output_format"] != test.outputFormat {
				t.Fatalf("output_format = %#v, want %q", document["output_format"], test.outputFormat)
			}
			if test.exitMeta != 0 {
				meta, ok := document["_meta"].(map[string]any)
				if !ok || meta["exit_code"] != float64(test.exitMeta) {
					t.Fatalf("_meta = %#v, want exit_code=%d", document["_meta"], test.exitMeta)
				}
			}
			for _, field := range test.emptyArrays {
				value, ok := document[field].([]any)
				if !ok || len(value) != 0 {
					t.Fatalf("%s = %#v, want empty JSON array", field, document[field])
				}
			}
		})
	}

	t.Run("precommand_non_json_format_preserves_human_profile", func(t *testing.T) {
		process := runBuiltRobotProcess(t, ntmPath, repoDir, env,
			"--profile-startup", "--format=csv", "audit", "export", "missing-session",
		)
		if process.exitCode != 0 {
			t.Fatalf("precommand human format exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
		}
		if json.Valid(bytes.TrimSpace(process.stdout)) {
			t.Fatalf("precommand CSV format was incorrectly treated as JSON: %s", process.stdout)
		}
		if len(bytes.TrimSpace(process.stderr)) == 0 {
			t.Fatalf("human startup profile was incorrectly suppressed: stdout=%s", process.stdout)
		}
	})

	bvArgs, err := os.ReadFile(bvArgsPath)
	if err != nil {
		t.Fatalf("read bv precedence arguments: %v", err)
	}
	var graphCalls int
	for _, call := range strings.Split(strings.TrimSpace(string(bvArgs)), "\n") {
		if strings.HasPrefix(call, "--robot-graph") {
			graphCalls++
			if call != "--robot-graph --graph-format json" {
				t.Fatalf("bv graph arguments=%q, want JSON override", call)
			}
		}
	}
	if graphCalls != 3 {
		t.Fatalf("bv calls=%q, want three JSON graph calls", bvArgs)
	}
}

func TestE2ELocalJSONSessionInferenceBuiltBinary(t *testing.T) {
	CommonE2EPrerequisites(t)
	fixture := newRobotProcessFixture(t, "local-json-inference")

	for _, test := range []struct {
		name        string
		args        []string
		emptyArrays []string
	}{
		{name: "ensemble_status", args: []string{"ensemble", "status", "--format=JSON"}},
		{name: "ensemble_stop", args: []string{"ensemble", "stop", "--format=JSON", "--yes"}},
		{name: "ensemble_synthesize", args: []string{"ensemble", "synthesize", "--format=JSON"}},
		{name: "ensemble_export_findings", args: []string{"ensemble", "export-findings", "--format=JSON", "--all"}},
		{name: "ensemble_provenance", args: []string{"ensemble", "provenance", "--format=JSON", "--all"}},
		{name: "rebalance", args: []string{"rebalance", "--format=JSON"}},
		{name: "review_queue", args: []string{"review-queue", "--format=JSON"}, emptyArrays: []string{"suggestions"}},
		{name: "summary", args: []string{"summary", "--format=JSON"}},
		{name: "dashboard", args: []string{"dashboard", fixture.session, "--json"}},
		{name: "swarm_status", args: []string{"swarm", "status", "--json"}},
		{name: "swarm_stop_empty", args: []string{"swarm", "stop", "--json", "--yes"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"--profile-startup"}, test.args...)
			process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env, args...)
			if len(bytes.TrimSpace(process.stderr)) != 0 {
				t.Fatalf("local JSON inference exit=%d stderr=%q stdout=%s", process.exitCode, process.stderr, process.stdout)
			}
			var document any
			decodeSingleRobotJSON(t, process.stdout, &document)
			if len(test.emptyArrays) > 0 {
				object, ok := document.(map[string]any)
				if !ok {
					t.Fatalf("root document = %#v, want JSON object", document)
				}
				for _, field := range test.emptyArrays {
					value, ok := object[field].([]any)
					if !ok || len(value) != 0 {
						t.Fatalf("%s = %#v, want empty JSON array", field, object[field])
					}
				}
			}
		})
	}
}

func TestE2ERobotAssignTerminalContractsBuiltBinary(t *testing.T) {
	CommonE2EPrerequisites(t)
	fixture := newRobotProcessFixture(t, "assign-terminal")

	type assignEnvelope struct {
		Success         bool             `json:"success"`
		Timestamp       string           `json:"timestamp"`
		OutputFormat    string           `json:"output_format"`
		Error           string           `json:"error"`
		ErrorCode       string           `json:"error_code"`
		Recommendations []map[string]any `json:"recommendations"`
		BlockedBeads    []map[string]any `json:"blocked_beads"`
		IdleAgents      []string         `json:"idle_agents"`
	}
	assertAssignFailure := func(t *testing.T, process robotBoundaryProcessResult, wantCode string) assignEnvelope {
		t.Helper()
		if process.exitCode != 1 {
			t.Fatalf("exit=%d, want 1; stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
		}
		if len(bytes.TrimSpace(process.stderr)) != 0 {
			t.Fatalf("stderr=%q, want empty canonical robot boundary", process.stderr)
		}
		var envelope assignEnvelope
		decodeSingleRobotJSON(t, process.stdout, &envelope)
		if envelope.Success || envelope.ErrorCode != wantCode || envelope.Error == "" || envelope.OutputFormat != "json" {
			t.Fatalf("assign envelope=%+v, want %s canonical JSON failure", envelope, wantCode)
		}
		if envelope.Recommendations == nil || envelope.BlockedBeads == nil || envelope.IdleAgents == nil {
			t.Fatalf("assign failure lost required arrays: %+v", envelope)
		}
		if _, err := time.Parse(time.RFC3339, envelope.Timestamp); err != nil {
			t.Fatalf("terminal timestamp %q is not RFC3339: %v", envelope.Timestamp, err)
		}
		return envelope
	}
	writeTmuxGuard := func(t *testing.T, name string) (string, string, string) {
		t.Helper()
		wrapperPath := filepath.Join(fixture.root, "tmux-"+name)
		actuationLog := filepath.Join(fixture.root, name+"-tmux-actuation.log")
		hasSessionLog := filepath.Join(fixture.root, name+"-tmux-has-session.log")
		wrapper := `#!/bin/sh
command=${1:-}
target=
previous=
for argument in "$@"; do
    if [ "$previous" = "-t" ]; then
        target=$argument
    fi
    previous=$argument
done

case "$command" in
    new-session|kill-session|kill-pane|split-window|join-pane|break-pane|respawn-pane|send-keys|select-layout|select-pane|move-window|link-window|unlink-window|rename-session|rename-window|set-option|set-window-option)
        printf '%s\n' "$*" >> "$NTM_E2E_TMUX_ACTUATION_LOG"
        ;;
esac

if [ "$command" = "has-session" ] && [ "$target" = "$NTM_E2E_TARGET_SESSION" ] && [ -n "${NTM_E2E_HAS_SESSION_EXIT:-}" ]; then
    printf '%s\n' "$*" >> "$NTM_E2E_HAS_SESSION_LOG"
    exit "$NTM_E2E_HAS_SESSION_EXIT"
fi

exec "$NTM_E2E_REAL_TMUX" "$@"
`
		if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o700); err != nil {
			t.Fatalf("write %s tmux guard: %v", name, err)
		}
		return wrapperPath, actuationLog, hasSessionLog
	}
	assertFileAbsent := func(t *testing.T, path, description string) {
		t.Helper()
		if data, err := os.ReadFile(path); err == nil {
			t.Fatalf("%s unexpectedly recorded work: %s", description, data)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read %s: %v", path, err)
		}
	}

	t.Run("SIGINT_cancels_blocked_ready_scan_once", func(t *testing.T) {
		fakeBin := filepath.Join(fixture.root, "cancel-bin")
		if err := os.MkdirAll(fakeBin, 0o700); err != nil {
			t.Fatalf("create cancellation fake bin: %v", err)
		}
		brStarted := filepath.Join(fixture.root, "cancel-br-started")
		brCalls := filepath.Join(fixture.root, "cancel-br-calls.log")
		brPath := filepath.Join(fakeBin, "br")
		brScript := `#!/bin/sh
if [ "${1:-}" = "--lock-timeout" ]; then
    shift 2
fi
printf '%s\n' "$*" >> "$NTM_E2E_BR_CALLS"
case "${1:-}" in
    ready)
        printf 'ready-started\n' > "$NTM_E2E_BR_STARTED"
        exec sleep 30
        ;;
    *)
        printf '[]\n'
        ;;
esac
`
		if err := os.WriteFile(brPath, []byte(brScript), 0o700); err != nil {
			t.Fatalf("write blocking br: %v", err)
		}
		tmuxWrapper, actuationLog, _ := writeTmuxGuard(t, "cancel")
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"PATH":                       fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
			"NTM_TMUX_BINARY":            tmuxWrapper,
			"NTM_E2E_REAL_TMUX":          fixture.tmuxPath,
			"NTM_E2E_TARGET_SESSION":     fixture.session,
			"NTM_E2E_TMUX_ACTUATION_LOG": actuationLog,
			"NTM_E2E_HAS_SESSION_LOG":    filepath.Join(fixture.root, "cancel-unused-has-session.log"),
			"NTM_E2E_HAS_SESSION_EXIT":   "",
			"NTM_E2E_BR_STARTED":         brStarted,
			"NTM_E2E_BR_CALLS":           brCalls,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, fixture.ntmPath,
			"--robot-format=json",
			"--robot-assign="+fixture.session,
			"--strategy=balanced",
		)
		cmd.Dir = fixture.projectDir
		cmd.Env = append([]string(nil), env...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start cancelable robot assign: %v", err)
		}
		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
		}()
		startedDeadline := time.Now().Add(20 * time.Second)
		for {
			data, readErr := os.ReadFile(brStarted)
			if readErr == nil && strings.Contains(string(data), "ready-started") {
				break
			}
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				cancel()
				<-waitCh
				t.Fatalf("read blocked-ready marker: %v", readErr)
			}
			select {
			case earlyErr := <-waitCh:
				t.Fatalf("robot assign exited before blocked ready scan: %v stdout=%s stderr=%s", earlyErr, stdout.String(), stderr.String())
			default:
			}
			if time.Now().After(startedDeadline) {
				cancel()
				<-waitCh
				t.Fatalf("timed out waiting for robot assign ready scan: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			time.Sleep(25 * time.Millisecond)
		}
		if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
			cancel()
			<-waitCh
			t.Fatalf("signal robot assign: %v", err)
		}
		var err error
		select {
		case err = <-waitCh:
		case <-ctx.Done():
			err = <-waitCh
		}
		if ctx.Err() != nil {
			t.Fatalf("robot assign did not join after SIGINT: %v stdout=%s stderr=%s", ctx.Err(), stdout.String(), stderr.String())
		}
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			t.Fatalf("robot assign process was not joined: state=%v", cmd.ProcessState)
		}
		exitCode := 0
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("wait for canceled robot assign: %v", err)
			}
			exitCode = exitErr.ExitCode()
		}
		process := robotBoundaryProcessResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode}
		assertAssignFailure(t, process, "TIMEOUT")

		callsBefore, err := os.ReadFile(brCalls)
		if err != nil {
			t.Fatalf("read cancellation br calls: %v", err)
		}
		readyCalls := 0
		for _, call := range strings.Split(strings.TrimSpace(string(callsBefore)), "\n") {
			if strings.HasPrefix(call, "ready ") {
				readyCalls++
			}
		}
		if readyCalls != 1 {
			t.Fatalf("br calls=%q, want exactly one blocked ready scan", callsBefore)
		}
		assertFileAbsent(t, actuationLog, "canceled robot assign tmux actuation")
		time.Sleep(750 * time.Millisecond)
		callsAfter, err := os.ReadFile(brCalls)
		if err != nil {
			t.Fatalf("read late cancellation br calls: %v", err)
		}
		if !bytes.Equal(callsAfter, callsBefore) {
			t.Fatalf("late br work appeared after joined cancellation: before=%q after=%q", callsBefore, callsAfter)
		}
		assertFileAbsent(t, actuationLog, "late canceled robot assign tmux actuation")
	})

	for _, exitCode := range []int{2, 255} {
		t.Run(fmt.Sprintf("has_session_exit_%d_is_internal_error", exitCode), func(t *testing.T) {
			name := fmt.Sprintf("has-session-%d", exitCode)
			fakeBin := filepath.Join(fixture.root, name+"-bin")
			if err := os.MkdirAll(fakeBin, 0o700); err != nil {
				t.Fatalf("create %s fake bin: %v", name, err)
			}
			brCalls := filepath.Join(fixture.root, name+"-br-calls.log")
			brScript := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$NTM_E2E_BR_CALLS\"\nprintf '[]\\n'\n"
			if err := os.WriteFile(filepath.Join(fakeBin, "br"), []byte(brScript), 0o700); err != nil {
				t.Fatalf("write %s br guard: %v", name, err)
			}
			tmuxWrapper, actuationLog, hasSessionLog := writeTmuxGuard(t, name)
			env := mergeRobotProcessEnv(fixture.env, map[string]string{
				"PATH":                       fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
				"NTM_TMUX_BINARY":            tmuxWrapper,
				"NTM_E2E_REAL_TMUX":          fixture.tmuxPath,
				"NTM_E2E_TARGET_SESSION":     fixture.session,
				"NTM_E2E_TMUX_ACTUATION_LOG": actuationLog,
				"NTM_E2E_HAS_SESSION_LOG":    hasSessionLog,
				"NTM_E2E_HAS_SESSION_EXIT":   fmt.Sprintf("%d", exitCode),
				"NTM_E2E_BR_CALLS":           brCalls,
			})
			process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, env,
				"--robot-format=json",
				"--robot-assign="+fixture.session,
				"--strategy=balanced",
			)
			envelope := assertAssignFailure(t, process, "INTERNAL_ERROR")
			if strings.Contains(strings.ToUpper(envelope.Error), "SESSION_NOT_FOUND") {
				t.Fatalf("has-session exit %d was collapsed to session absence: %+v", exitCode, envelope)
			}
			hasSessionCalls, err := os.ReadFile(hasSessionLog)
			if err != nil {
				t.Fatalf("read injected has-session calls: %v", err)
			}
			if strings.Count(strings.TrimSpace(string(hasSessionCalls)), "\n") != 0 || !strings.Contains(string(hasSessionCalls), "has-session") {
				t.Fatalf("injected has-session calls=%q, want exactly one", hasSessionCalls)
			}
			assertFileAbsent(t, actuationLog, fmt.Sprintf("has-session exit %d tmux actuation", exitCode))
			if data, err := os.ReadFile(brCalls); err == nil {
				for _, mutation := range []string{"update ", "close ", "claim ", "create ", "dep ", "sync "} {
					if strings.Contains(string(data), mutation) {
						t.Fatalf("has-session exit %d triggered br mutation %q: %s", exitCode, mutation, data)
					}
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("read has-session br calls: %v", err)
			}
			time.Sleep(250 * time.Millisecond)
			assertFileAbsent(t, actuationLog, fmt.Sprintf("late has-session exit %d tmux actuation", exitCode))
		})
	}
}

func TestE2EJSONCommandTerminalContractsBuiltBinary(t *testing.T) {
	CommonE2EPrerequisites(t)
	fixture := newRobotProcessFixture(t, "json-command-terminal")

	type failureEnvelope struct {
		Success   *bool  `json:"success"`
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	}
	assertOneFailure := func(t *testing.T, process robotBoundaryProcessResult) failureEnvelope {
		t.Helper()
		if process.exitCode == 0 || len(bytes.TrimSpace(process.stderr)) != 0 {
			t.Fatalf("terminal failure exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
		}
		var envelope failureEnvelope
		decodeSingleRobotJSON(t, process.stdout, &envelope)
		if envelope.Success == nil || *envelope.Success || strings.TrimSpace(envelope.Error) == "" {
			t.Fatalf("terminal failure envelope = %+v", envelope)
		}
		return envelope
	}
	runFailure := func(t *testing.T, dir string, env []string, args ...string) failureEnvelope {
		t.Helper()
		return assertOneFailure(t, runBuiltRobotProcess(t, fixture.ntmPath, dir, env, args...))
	}

	t.Run("legacy_json_failure_surfaces_are_one_document", func(t *testing.T) {
		invalidPipeline := filepath.Join(fixture.root, "invalid-pipeline.yaml")
		if err := os.WriteFile(invalidPipeline, []byte("name: invalid\nsteps: [\n"), 0o600); err != nil {
			t.Fatalf("write invalid pipeline: %v", err)
		}
		invalidPolicy := filepath.Join(fixture.root, "invalid-policy.yaml")
		if err := os.WriteFile(invalidPolicy, []byte("rules: [\n"), 0o600); err != nil {
			t.Fatalf("write invalid policy: %v", err)
		}

		cases := []struct {
			name          string
			args          []string
			wantErrorCode string
		}{
			{name: "pipeline_lint", args: []string{"--json", "pipeline", "lint", invalidPipeline}},
			{name: "persona_show", args: []string{"--json", "personas", "show", "missing-terminal-persona"}},
			{name: "checkpoint_restore", args: []string{"--json", "checkpoint", "restore", fixture.session, "missing-terminal-checkpoint", "--dry-run"}},
			{name: "approval", args: []string{"approve", "missing-terminal-approval", "--json"}},
			{name: "session_template_show", args: []string{"--json", "session-templates", "show", "missing-terminal-template"}},
			{name: "workflow_show", args: []string{"--json", "workflows", "show", "missing-terminal-workflow"}},
			{name: "recipe_show", args: []string{"--json", "recipes", "show", "missing-terminal-recipe"}},
			{name: "hooks_status", args: []string{"--json", "hooks", "status"}},
			{name: "policy_validate", args: []string{"--json", "policy", "validate", invalidPolicy}},
			{name: "health_missing_session", args: []string{"--json", "health", fixture.session + "-missing"}},
			{name: "codex_palette_state", args: []string{"--json", "codex", "palette-state"}, wantErrorCode: "INVALID_FLAG"},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				envelope := runFailure(t, fixture.projectDir, fixture.env, test.args...)
				if test.wantErrorCode != "" && envelope.ErrorCode != test.wantErrorCode {
					t.Fatalf("error_code=%q, want %q; envelope=%+v", envelope.ErrorCode, test.wantErrorCode, envelope)
				}
			})
		}
	})

	t.Run("strict_preflight_failure_is_one_document", func(t *testing.T) {
		runFailure(t, fixture.projectDir, fixture.env,
			"preflight", "Run rm -rf /tmp/important to clean cache", "--strict", "--json",
		)
	})

	t.Run("config_validation_failure_is_one_document", func(t *testing.T) {
		projectDir := filepath.Join(fixture.root, "invalid-config-project")
		ntmDir := filepath.Join(projectDir, ".ntm")
		if err := os.MkdirAll(ntmDir, 0o700); err != nil {
			t.Fatalf("create invalid config project: %v", err)
		}
		if err := os.WriteFile(filepath.Join(ntmDir, "config.toml"), []byte("[invalid\n"), 0o600); err != nil {
			t.Fatalf("write invalid project config: %v", err)
		}
		runFailure(t, projectDir, fixture.env, "--json", "config", "validate")
	})

	t.Run("checkpoint_verification_failures_are_one_document", func(t *testing.T) {
		checkpointID := "terminal-invalid-checkpoint"
		checkpointDir := filepath.Join(
			fixture.root, "home", ".local", "share", "ntm", "checkpoints", fixture.session, checkpointID,
		)
		if err := os.MkdirAll(checkpointDir, 0o700); err != nil {
			t.Fatalf("create invalid checkpoint: %v", err)
		}
		if err := os.WriteFile(filepath.Join(checkpointDir, "metadata.json"), []byte("{\n"), 0o600); err != nil {
			t.Fatalf("write invalid checkpoint metadata: %v", err)
		}
		runFailure(t, fixture.projectDir, fixture.env,
			"--json", "checkpoint", "verify", fixture.session, checkpointID,
		)
		runFailure(t, fixture.projectDir, fixture.env,
			"--json", "checkpoint", "verify", fixture.session, "--all",
		)
	})

	t.Run("missing_optional_tools_are_terminal_failures", func(t *testing.T) {
		emptyPath := filepath.Join(fixture.root, "empty-path")
		if err := os.MkdirAll(emptyPath, 0o700); err != nil {
			t.Fatalf("create empty PATH: %v", err)
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{"PATH": emptyPath})
		for _, test := range []struct {
			name          string
			args          []string
			wantErrorCode string
		}{
			{name: "deps_without_tmux", args: []string{"--json", "deps"}, wantErrorCode: "DEPENDENCY_MISSING"},
			{name: "scan_without_ubs", args: []string{"--json", "scan", fixture.projectDir}, wantErrorCode: "DEPENDENCY_MISSING"},
			{name: "bugs_without_ubs", args: []string{"--json", "bugs", "list", fixture.projectDir}, wantErrorCode: "DEPENDENCY_MISSING"},
			{name: "cass_status_without_cass", args: []string{"--json", "cass", "status"}, wantErrorCode: "NOT_IMPLEMENTED"},
		} {
			t.Run(test.name, func(t *testing.T) {
				process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, env, test.args...)
				envelope := assertOneFailure(t, process)
				if envelope.ErrorCode != test.wantErrorCode {
					t.Fatalf("error_code=%q, want %q; envelope=%+v", envelope.ErrorCode, test.wantErrorCode, envelope)
				}
				if test.name == "cass_status_without_cass" && process.exitCode != 2 {
					t.Fatalf("CASS unavailable exit=%d, want 2", process.exitCode)
				}
			})
		}
	})

	t.Run("scan_severity_matches_process_status", func(t *testing.T) {
		fakeBin := filepath.Join(fixture.root, "fake-ubs-bin")
		if err := os.MkdirAll(fakeBin, 0o700); err != nil {
			t.Fatalf("create fake UBS bin: %v", err)
		}
		ubsPath := filepath.Join(fakeBin, "ubs")
		writeUBS := func(t *testing.T, critical, warning int) {
			t.Helper()
			payload := fmt.Sprintf(`{"project":%q,"timestamp":"2026-07-13T00:00:00Z","scanners":[],"totals":{"critical":%d,"warning":%d,"info":0,"files":1},"findings":[],"exit_code":0}`+"\n", fixture.projectDir, critical, warning)
			script := "#!/bin/sh\nprintf '%s\\n' '" + strings.TrimSpace(payload) + "'\n"
			if err := os.WriteFile(ubsPath, []byte(script), 0o700); err != nil {
				t.Fatalf("write fake UBS: %v", err)
			}
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"PATH": fakeBin + string(os.PathListSeparator) + "/usr/bin:/bin",
		})

		writeUBS(t, 0, 1)
		assertOneFailure(t, runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, env,
			"--json", "scan", fixture.projectDir, "--fail-on-warning",
		))

		writeUBS(t, 1, 0)
		assertOneFailure(t, runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, env,
			"--json", "scan", fixture.projectDir,
		))

		writeUBS(t, 0, 0)
		process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, env,
			"--json", "scan", fixture.projectDir,
		)
		if process.exitCode != 0 || len(bytes.TrimSpace(process.stderr)) != 0 {
			t.Fatalf("clean scan exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
		}
		var clean map[string]any
		decodeSingleRobotJSON(t, process.stdout, &clean)

		findingPayload := fmt.Sprintf(`{"project":%q,"timestamp":"2026-07-13T00:00:00Z","scanners":[],"totals":{"critical":0,"warning":1,"info":0,"files":1},"findings":[{"file":"terminal.go","line":1,"column":1,"severity":"warning","category":"test","message":"fixture finding","rule_id":"terminal-fixture"}],"exit_code":0}`+"\n", fixture.projectDir)
		findingScript := "#!/bin/sh\nprintf '%s\\n' '" + strings.TrimSpace(findingPayload) + "'\n"
		if err := os.WriteFile(ubsPath, []byte(findingScript), 0o700); err != nil {
			t.Fatalf("write integration-failure UBS: %v", err)
		}
		assertOneFailure(t, runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, env,
			"--json", "scan", fixture.projectDir, "--create-beads",
		))
	})

	t.Run("git_sync_failures_are_terminal", func(t *testing.T) {
		runFailure(t, fixture.projectDir, fixture.env, "--json", "git", "sync")

		repoDir := filepath.Join(fixture.root, "repo-without-upstream")
		if err := os.MkdirAll(repoDir, 0o700); err != nil {
			t.Fatalf("create git fixture: %v", err)
		}
		initCmd := exec.Command("git", "init", "-b", "main")
		initCmd.Dir = repoDir
		if output, err := initCmd.CombinedOutput(); err != nil {
			t.Fatalf("init git fixture: %v: %s", err, output)
		}
		commitCmd := exec.Command("git", "-c", "user.name=NTM E2E", "-c", "user.email=ntm-e2e@example.invalid", "commit", "--allow-empty", "-m", "fixture")
		commitCmd.Dir = repoDir
		if output, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("commit git fixture: %v: %s", err, output)
		}
		runFailure(t, repoDir, fixture.env, "--json", "git", "sync")
	})

	t.Run("audit_verify_corruption_is_one_aggregate_document", func(t *testing.T) {
		session := "terminal-audit"
		auditDir := filepath.Join(fixture.root, "home", ".local", "share", "ntm", "audit")
		if err := os.MkdirAll(auditDir, 0o700); err != nil {
			t.Fatalf("create audit directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(auditDir, session+"-2026-07-13.jsonl"), []byte("not-json\n"), 0o600); err != nil {
			t.Fatalf("write corrupt audit log: %v", err)
		}
		runFailure(t, fixture.projectDir, fixture.env, "--json", "audit", "verify", session)
	})

	t.Run("local_format_json_is_machine_terminal", func(t *testing.T) {
		for _, args := range [][]string{
			{"ensemble", "compare", "missing-run-a", "missing-run-b", "--format=json"},
			{"ensemble", "compare", "missing-run-a", "missing-run-b", "--format", "json"},
			{"ensemble", "compare", "missing-run-a", "missing-run-b", "-f=json"},
			{"ensemble", "compare", "missing-run-a", "missing-run-b", "-fjson"},
			{"ensemble", "compare", "missing-run-a", "missing-run-b", "-f", "json"},
		} {
			runFailure(t, fixture.projectDir, fixture.env, args...)
		}

		configDir := filepath.Join(fixture.root, "presets-config")
		if err := os.MkdirAll(filepath.Join(configDir, "ntm"), 0o700); err != nil {
			t.Fatalf("create presets config directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "ntm", "ensembles.toml"), []byte("presets = [\n"), 0o600); err != nil {
			t.Fatalf("write malformed ensemble registry: %v", err)
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{"XDG_CONFIG_HOME": configDir})
		runFailure(t, fixture.projectDir, env, "ensemble", "presets", "--format=json")
	})

	t.Run("documented_local_json_spellings_are_machine_terminal", func(t *testing.T) {
		for _, test := range []struct {
			args        []string
			wantNonzero bool
		}{
			{args: []string{"--profile-startup", "worktree", "list", "--output=json"}, wantNonzero: true},
			{args: []string{"--profile-startup", "worktree", "list", "--output", "json"}, wantNonzero: true},
			{args: []string{"--profile-startup", "worktree", "list", "-o=json"}, wantNonzero: true},
			{args: []string{"--profile-startup", "worktree", "list", "-ojson"}, wantNonzero: true},
			{args: []string{"--profile-startup", "ensemble", "compare", "missing-run-a", "missing-run-b", "-fjson"}, wantNonzero: true},
			// commit-ready is intentionally advisory: an unsafe report remains a
			// successful process result, but it must still be one clean JSON document.
			{args: []string{"--profile-startup", "work", "commit-readiness", "--format=json"}},
			{args: []string{"--profile-startup", "work", "commit-readiness", "--format", "json"}},
		} {
			process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env, test.args...)
			if len(bytes.TrimSpace(process.stderr)) != 0 {
				t.Fatalf("machine invocation exit=%d stderr=%q, want empty stderr", process.exitCode, process.stderr)
			}
			if test.wantNonzero && process.exitCode == 0 {
				t.Fatalf("machine failure args=%q exited zero: %s", test.args, process.stdout)
			}
			var document any
			decodeSingleRobotJSON(t, process.stdout, &document)
		}
	})

	t.Run("default_json_commands_suppress_startup_profile", func(t *testing.T) {
		for _, test := range []struct {
			name string
			args []string
		}{
			{name: "audit_export", args: []string{"--profile-startup", "audit", "export", fixture.session}},
			{name: "metrics_export", args: []string{"--profile-startup", "metrics", "export"}},
			{name: "work_graph", args: []string{"--profile-startup", "work", "graph"}},
		} {
			t.Run(test.name, func(t *testing.T) {
				process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env, test.args...)
				if len(bytes.TrimSpace(process.stderr)) != 0 {
					t.Fatalf("default JSON command exit=%d stderr=%q, want empty machine stderr", process.exitCode, process.stderr)
				}
				var document any
				decodeSingleRobotJSON(t, process.stdout, &document)
			})
		}
	})

	t.Run("profile_parse_error_is_one_document", func(t *testing.T) {
		process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env,
			"profiles", "switch", "not-an-agent-id", "reviewer", "--json",
		)
		assertOneFailure(t, process)
	})

	failingTMUX := filepath.Join(fixture.root, "tmux-terminal-failure")
	failingScript := `#!/bin/sh
if [ "${1:-}" = "-V" ]; then
    printf 'tmux 3.4\n'
    exit 0
fi
exit 64
`
	if err := os.WriteFile(failingTMUX, []byte(failingScript), 0o700); err != nil {
		t.Fatalf("write failing tmux wrapper: %v", err)
	}
	failingEnv := mergeRobotProcessEnv(fixture.env, map[string]string{"NTM_TMUX_BINARY": failingTMUX})
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "session_list_failure_is_one_document", args: []string{"list", "--json"}},
		{name: "session_status_failure_is_one_document", args: []string{"status", fixture.session, "--json"}},
		{name: "session_create_failure_exits_nonzero", args: []string{"create", "terminal-create-failure", "--panes=1", "--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, failingEnv, test.args...)
			assertOneFailure(t, process)
		})
	}

	t.Run("profile_cancellation_is_one_document", func(t *testing.T) {
		blockingTMUX := filepath.Join(fixture.root, "tmux-profile-cancel")
		startedPath := filepath.Join(fixture.root, "profile-cancel-started")
		blockingScript := `#!/bin/sh
if [ "${1:-}" = "-V" ]; then
    printf 'tmux 3.4\n'
    exit 0
fi
printf 'started\n' > "$NTM_E2E_PROFILE_CANCEL_STARTED"
exec sleep 30
`
		if err := os.WriteFile(blockingTMUX, []byte(blockingScript), 0o700); err != nil {
			t.Fatalf("write blocking profile tmux wrapper: %v", err)
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"NTM_TMUX_BINARY":                blockingTMUX,
			"NTM_E2E_PROFILE_CANCEL_STARTED": startedPath,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, fixture.ntmPath,
			"profiles", "switch", "cod_1", "reviewer", "--session="+fixture.session, "--json",
		)
		cmd.Dir = fixture.projectDir
		cmd.Env = append([]string(nil), env...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start cancelable profile switch: %v", err)
		}
		waitCh := make(chan error, 1)
		go func() { waitCh <- cmd.Wait() }()
		deadline := time.Now().Add(20 * time.Second)
		for {
			if data, err := os.ReadFile(startedPath); err == nil && strings.Contains(string(data), "started") {
				break
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				cancel()
				<-waitCh
				t.Fatalf("read profile cancellation marker: %v", err)
			}
			select {
			case earlyErr := <-waitCh:
				t.Fatalf("profile switch exited before cancellation: %v stdout=%s stderr=%s", earlyErr, stdout.String(), stderr.String())
			default:
			}
			if time.Now().After(deadline) {
				cancel()
				<-waitCh
				t.Fatalf("timed out waiting for profile switch dependency: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			time.Sleep(25 * time.Millisecond)
		}
		if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
			cancel()
			<-waitCh
			t.Fatalf("signal profile switch: %v", err)
		}
		var waitErr error
		select {
		case waitErr = <-waitCh:
		case <-ctx.Done():
			waitErr = <-waitCh
		}
		if ctx.Err() != nil || cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			t.Fatalf("profile switch did not join after SIGINT: ctx=%v state=%v stdout=%s stderr=%s", ctx.Err(), cmd.ProcessState, stdout.String(), stderr.String())
		}
		exitCode := 0
		if waitErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(waitErr, &exitErr) {
				t.Fatalf("wait for canceled profile switch: %v", waitErr)
			}
			exitCode = exitErr.ExitCode()
		}
		assertOneFailure(t, robotBoundaryProcessResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode})
	})
}

func TestE2ERobotSpawnTerminalContractsBuiltBinary(t *testing.T) {
	CommonE2EPrerequisites(t)
	fixture := newRobotProcessFixture(t, "spawn-terminal")

	type assignmentDiagnostic struct {
		Pane       string `json:"pane"`
		ClaimError string `json:"claim_error"`
	}
	type agentDiagnostic struct {
		Pane  string `json:"pane"`
		Type  string `json:"type"`
		Error string `json:"error"`
	}
	type spawnEnvelope struct {
		Success      bool                   `json:"success"`
		Timestamp    string                 `json:"timestamp"`
		OutputFormat string                 `json:"output_format"`
		Error        string                 `json:"error"`
		ErrorCode    string                 `json:"error_code"`
		Agents       []agentDiagnostic      `json:"agents"`
		Assignments  []assignmentDiagnostic `json:"assignments"`
	}
	assertSpawnFailure := func(t *testing.T, process robotBoundaryProcessResult, wantCode string) spawnEnvelope {
		t.Helper()
		if process.exitCode != 1 {
			t.Fatalf("exit=%d, want 1; stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
		}
		if len(process.stderr) != 0 {
			t.Fatalf("stderr=%q, want empty canonical robot boundary", process.stderr)
		}
		var envelope spawnEnvelope
		decodeSingleRobotJSON(t, process.stdout, &envelope)
		if envelope.Success || envelope.ErrorCode != wantCode || envelope.Error == "" || envelope.OutputFormat != "json" {
			t.Fatalf("spawn envelope=%+v, want %s canonical JSON failure", envelope, wantCode)
		}
		if _, err := time.Parse(time.RFC3339, envelope.Timestamp); err != nil {
			t.Fatalf("terminal timestamp %q is not RFC3339: %v", envelope.Timestamp, err)
		}
		return envelope
	}
	run := func(t *testing.T, env []string, args ...string) robotBoundaryProcessResult {
		t.Helper()
		return runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, env, args...)
	}
	writeConfig := func(t *testing.T, name, command string) string {
		t.Helper()
		path := filepath.Join(fixture.root, name+".toml")
		contents := fmt.Sprintf("[agents]\nclaude = %q\n\n[spawn_pacing]\nenabled = false\n", command)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write spawn config: %v", err)
		}
		return path
	}
	writeLaunchMarkerConfig := func(t *testing.T, name string) (string, string) {
		t.Helper()
		markerPath := filepath.Join(fixture.root, name+"-launched")
		agentPath := filepath.Join(fixture.root, name+"-agent")
		agentScript := fmt.Sprintf("#!/bin/sh\nprintf 'launched\\n' > %q\nsleep 30\n", markerPath)
		if err := os.WriteFile(agentPath, []byte(agentScript), 0o700); err != nil {
			t.Fatalf("write launch-marker agent: %v", err)
		}
		return writeConfig(t, name, agentPath), markerPath
	}
	assertLaunchMarkerAbsent := func(t *testing.T, markerPath string) {
		t.Helper()
		if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("agent launch marker %s exists or is unreadable: %v", markerPath, err)
		}
	}
	physicalPaneCount := func(t *testing.T, session string) int {
		t.Helper()
		output := strings.TrimSpace(fixture.mustTMUXOutput(t,
			"list-panes", "-s", "-t", session, "-F", "#{pane_id}",
		))
		if output == "" {
			return 0
		}
		return len(strings.Split(output, "\n"))
	}
	callLogCount := func(t *testing.T, path string) int {
		t.Helper()
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		if err != nil {
			t.Fatalf("read tmux call log %s: %v", path, err)
		}
		return len(bytes.Fields(data))
	}

	for _, test := range []struct {
		name string
		args []string
	}{
		{
			name: "negative count",
			args: []string{
				"--robot-spawn=invalid-negative", "--spawn-cc=-1", "--spawn-cod=1", "--spawn-no-user", "--robot-format=json",
			},
		},
		{
			name: "zero agents",
			args: []string{
				"--robot-spawn=invalid-zero", "--spawn-no-user", "--robot-format=json",
			},
		},
		{
			name: "empty assignment strategy",
			args: []string{
				"--robot-spawn=invalid-strategy", "--spawn-cc=1", "--spawn-no-user", "--spawn-assign-work", "--strategy=", "--robot-format=json",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			envelope := assertSpawnFailure(t, run(t, fixture.env, test.args...), "INVALID_FLAG")
			if envelope.Agents == nil || len(envelope.Agents) != 0 {
				t.Fatalf("agents=%v, want initialized empty array", envelope.Agents)
			}
		})
	}

	t.Run("session probe infrastructure failure prevents creation", func(t *testing.T) {
		session := fixture.session + "-probe-permission"
		configPath := writeConfig(t, "probe-permission", "sleep 30")
		createMarker := filepath.Join(fixture.root, "probe-permission-created")
		wrapperPath := filepath.Join(fixture.root, "tmux-probe-permission")
		wrapper := `#!/bin/sh
if [ "${1:-}" = "has-session" ]; then
    printf '%s\n' 'error connecting to /tmp/tmux-1000/default (Permission denied)' >&2
    exit 1
fi
if [ "${1:-}" = "new-session" ]; then
    : > "$NTM_E2E_CREATE_MARKER"
fi
exec "$NTM_E2E_REAL_TMUX" "$@"
`
		if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o700); err != nil {
			t.Fatalf("write session-probe tmux wrapper: %v", err)
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"NTM_TMUX_BINARY":       wrapperPath,
			"NTM_E2E_REAL_TMUX":     fixture.tmuxPath,
			"NTM_E2E_CREATE_MARKER": createMarker,
		})
		envelope := assertSpawnFailure(t, run(t, env,
			"--config="+configPath,
			"--robot-spawn="+session,
			"--spawn-cc=1",
			"--spawn-no-user",
			"--spawn-dir="+fixture.projectDir,
			"--robot-format=json",
		), "INTERNAL_ERROR")
		if !strings.Contains(strings.ToLower(envelope.Error), "permission denied") {
			t.Fatalf("session-probe error=%q, want permission diagnostic", envelope.Error)
		}
		if _, err := os.Stat(createMarker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("session creation marker exists or is unreadable after failed probe: %v", err)
		}
	})

	t.Run("short topology fails before layout or launch", func(t *testing.T) {
		session := fixture.session + "-short-topology"
		configPath, launchMarker := writeLaunchMarkerConfig(t, "short-topology")
		listCountPath := filepath.Join(fixture.root, "short-topology-list-count")
		layoutLogPath := filepath.Join(fixture.root, "short-topology-layout-log")
		wrapperPath := filepath.Join(fixture.root, "tmux-short-topology")
		wrapper := `#!/bin/sh
target=
previous=
for argument in "$@"; do
    if [ "$previous" = "-t" ]; then
        target=$argument
    fi
    previous=$argument
done

case "$target" in
    "$NTM_E2E_TARGET_SESSION"|"$NTM_E2E_TARGET_SESSION":*)
        if [ "${1:-}" = "select-layout" ]; then
            printf 'layout\n' >> "$NTM_E2E_LAYOUT_LOG"
        fi
        ;;
esac

if [ "${1:-}" = "list-panes" ] && [ "$target" = "$NTM_E2E_TARGET_SESSION" ]; then
    count=0
    if [ -f "$NTM_E2E_LIST_COUNT" ]; then
        read -r count < "$NTM_E2E_LIST_COUNT"
    fi
    count=$((count + 1))
    printf '%s\n' "$count" > "$NTM_E2E_LIST_COUNT"
    if [ "$count" -ge 2 ]; then
        output=$("$NTM_E2E_REAL_TMUX" "$@") || exit $?
        printf '%s\n' "$output" | sed -n '1p'
        exit 0
    fi
fi

exec "$NTM_E2E_REAL_TMUX" "$@"
`
		if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o700); err != nil {
			t.Fatalf("write short-topology tmux wrapper: %v", err)
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"NTM_TMUX_BINARY":        wrapperPath,
			"NTM_E2E_REAL_TMUX":      fixture.tmuxPath,
			"NTM_E2E_TARGET_SESSION": session,
			"NTM_E2E_LIST_COUNT":     listCountPath,
			"NTM_E2E_LAYOUT_LOG":     layoutLogPath,
		})
		envelope := assertSpawnFailure(t, run(t, env,
			"--config="+configPath,
			"--robot-spawn="+session,
			"--spawn-cc=2",
			"--spawn-no-user",
			"--spawn-dir="+fixture.projectDir,
			"--robot-format=json",
		), "PANE_NOT_FOUND")
		if !strings.Contains(envelope.Error, "1 pane") || !strings.Contains(envelope.Error, "2 are required") {
			t.Fatalf("short-topology error=%q, want observed and required pane counts", envelope.Error)
		}
		if envelope.Agents == nil || len(envelope.Agents) != 0 {
			t.Fatalf("agents=%v, want initialized empty array before launch", envelope.Agents)
		}
		if got := physicalPaneCount(t, session); got != 2 {
			t.Fatalf("physical pane count=%d, want two panes retained after the real split", got)
		}
		if got := callLogCount(t, layoutLogPath); got != 1 {
			t.Fatalf("layout calls=%d, want only the split-window layout before topology rejection", got)
		}
		assertLaunchMarkerAbsent(t, launchMarker)
		time.Sleep(500 * time.Millisecond)
		assertLaunchMarkerAbsent(t, launchMarker)
		if got := physicalPaneCount(t, session); got != 2 {
			t.Fatalf("late physical pane count=%d, want stable partial topology of two", got)
		}
		if got := callLogCount(t, layoutLogPath); got != 1 {
			t.Fatalf("late layout calls=%d, want no final layout after topology rejection", got)
		}
	})

	t.Run("layout failure prevents all launches", func(t *testing.T) {
		session := fixture.session + "-layout-failure"
		configPath, launchMarker := writeLaunchMarkerConfig(t, "layout-failure")
		layoutLogPath := filepath.Join(fixture.root, "layout-failure-log")
		wrapperPath := filepath.Join(fixture.root, "tmux-layout-failure")
		wrapper := `#!/bin/sh
target=
previous=
for argument in "$@"; do
    if [ "$previous" = "-t" ]; then
        target=$argument
    fi
    previous=$argument
done

case "$target" in
    "$NTM_E2E_TARGET_SESSION"|"$NTM_E2E_TARGET_SESSION":*)
        if [ "${1:-}" = "select-layout" ]; then
            printf 'layout\n' >> "$NTM_E2E_LAYOUT_LOG"
            printf 'injected robot spawn layout failure\n' >&2
            exit 93
        fi
        ;;
esac

exec "$NTM_E2E_REAL_TMUX" "$@"
`
		if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o700); err != nil {
			t.Fatalf("write layout-failure tmux wrapper: %v", err)
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"NTM_TMUX_BINARY":        wrapperPath,
			"NTM_E2E_REAL_TMUX":      fixture.tmuxPath,
			"NTM_E2E_TARGET_SESSION": session,
			"NTM_E2E_LAYOUT_LOG":     layoutLogPath,
		})
		envelope := assertSpawnFailure(t, run(t, env,
			"--config="+configPath,
			"--robot-spawn="+session,
			"--spawn-cc=1",
			"--spawn-no-user",
			"--spawn-dir="+fixture.projectDir,
			"--robot-format=json",
		), "INTERNAL_ERROR")
		if !strings.Contains(envelope.Error, "applying tiled layout") || !strings.Contains(envelope.Error, "injected robot spawn layout failure") {
			t.Fatalf("layout failure error=%q, want operation and injected cause", envelope.Error)
		}
		if envelope.Agents == nil || len(envelope.Agents) != 0 {
			t.Fatalf("agents=%v, want initialized empty array before launch", envelope.Agents)
		}
		if got := physicalPaneCount(t, session); got != 1 {
			t.Fatalf("physical pane count=%d, want created one-pane session retained", got)
		}
		if got := callLogCount(t, layoutLogPath); got != 1 {
			t.Fatalf("layout calls=%d, want one failed final layout", got)
		}
		assertLaunchMarkerAbsent(t, launchMarker)
		time.Sleep(500 * time.Millisecond)
		assertLaunchMarkerAbsent(t, launchMarker)
		if got := physicalPaneCount(t, session); got != 1 {
			t.Fatalf("late physical pane count=%d, want stable one-pane partial topology", got)
		}
		if got := callLogCount(t, layoutLogPath); got != 1 {
			t.Fatalf("late layout calls=%d, want no retry after terminal failure", got)
		}
	})

	t.Run("launch failure retains per-agent diagnostics", func(t *testing.T) {
		// A TOML multiline command reaches the production command sanitizer and
		// deterministically fails before any agent input is sent to the pane.
		configPath := filepath.Join(fixture.root, "launch-failure.toml")
		configBody := "[agents]\nclaude = \"\"\"bad\ncommand\"\"\"\n\n[spawn_pacing]\nenabled = false\n"
		if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
			t.Fatalf("write launch failure config: %v", err)
		}
		envelope := assertSpawnFailure(t, run(t, fixture.env,
			"--config="+configPath,
			"--robot-spawn="+fixture.session+"-launch",
			"--spawn-cc=1",
			"--spawn-no-user",
			"--spawn-dir="+fixture.projectDir,
			"--robot-format=json",
		), "INTERNAL_ERROR")
		if len(envelope.Agents) != 1 || envelope.Agents[0].Error == "" {
			t.Fatalf("agents=%+v, want one per-agent launch diagnostic", envelope.Agents)
		}
	})

	t.Run("subsecond readiness deadline is TIMEOUT", func(t *testing.T) {
		scriptPath := filepath.Join(fixture.root, "agent-block")
		script := "#!/bin/sh\nprintf 'booting\\n'\nsleep 30\n"
		if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
			t.Fatalf("write blocking agent: %v", err)
		}
		captureMarker := filepath.Join(fixture.root, "readiness-capture-started")
		tmuxWrapper := filepath.Join(fixture.root, "tmux-readiness")
		wrapper := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "capture-pane" ] && [ ! -e %q ]; then
  : > %q
fi
exec %q "$@"
`, captureMarker, captureMarker, fixture.tmuxPath)
		if err := os.WriteFile(tmuxWrapper, []byte(wrapper), 0o700); err != nil {
			t.Fatalf("write readiness tmux wrapper: %v", err)
		}
		configPath := writeConfig(t, "readiness", scriptPath)
		env := mergeRobotProcessEnv(fixture.env, map[string]string{"NTM_TMUX_BINARY": tmuxWrapper})
		started := time.Now()
		envelope := assertSpawnFailure(t, run(t, env,
			"--config="+configPath,
			"--robot-spawn="+fixture.session+"-wait",
			"--spawn-cc=1",
			"--spawn-no-user",
			"--spawn-dir="+fixture.projectDir,
			"--spawn-wait",
			"--timeout=500ms",
			"--robot-format=json",
		), "TIMEOUT")
		finished := time.Now()
		wallElapsed := finished.Sub(started)
		if !strings.Contains(envelope.Error, "500ms timeout") {
			t.Fatalf("readiness error=%q, want exact subsecond timeout diagnostic", envelope.Error)
		}
		markerInfo, err := os.Stat(captureMarker)
		if err != nil {
			t.Fatalf("stat readiness capture marker: %v", err)
		}
		readinessElapsed := finished.Sub(markerInfo.ModTime())
		if readinessElapsed < 350*time.Millisecond || readinessElapsed > 12*time.Second {
			t.Fatalf("500ms readiness timeout after launch=%s (total process time=%s)", readinessElapsed, wallElapsed)
		}
		if wallElapsed > 12*time.Second {
			t.Fatalf("500ms readiness timeout total process time=%s, want bounded startup plus readiness", wallElapsed)
		}
		if len(envelope.Agents) != 1 {
			t.Fatalf("agents=%+v, want launched agent retained", envelope.Agents)
		}
	})

	t.Run("zero work items covers every agent and fails", func(t *testing.T) {
		fakeBin := filepath.Join(fixture.root, "fake-bin")
		if err := os.MkdirAll(fakeBin, 0o700); err != nil {
			t.Fatalf("create fake bin: %v", err)
		}
		bvPath := filepath.Join(fakeBin, "bv")
		bvScript := "#!/bin/sh\nprintf '%s\\n' '{\"recommendations\":[],\"quick_wins\":[],\"blockers_to_clear\":[]}'\n"
		if err := os.WriteFile(bvPath, []byte(bvScript), 0o700); err != nil {
			t.Fatalf("write fake bv: %v", err)
		}
		env := mergeRobotProcessEnv(fixture.env, map[string]string{
			"PATH": fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		})
		configPath := writeConfig(t, "zero-work", "sleep 30")
		envelope := assertSpawnFailure(t, run(t, env,
			"--config="+configPath,
			"--robot-spawn="+fixture.session+"-assign",
			"--spawn-cc=1",
			"--spawn-no-user",
			"--spawn-dir="+fixture.projectDir,
			"--spawn-assign-work",
			"--strategy=top-n",
			"--robot-format=json",
		), "ASSIGNMENT_FAILED")
		if len(envelope.Agents) != 1 || len(envelope.Assignments) != 1 || envelope.Assignments[0].ClaimError == "" {
			t.Fatalf("agents=%+v assignments=%+v, want complete zero-work diagnostics", envelope.Agents, envelope.Assignments)
		}
	})
}

func TestE2ERobotReplayBuiltBinaryRealTmuxDeliversExactlyOnce(t *testing.T) {
	CommonE2EPrerequisites(t)
	fixture := newRobotProcessFixture(t, "replay-once")

	readyPath := filepath.Join(fixture.root, "pane-ready")
	readyCommand := fmt.Sprintf("printf ready > %q", readyPath)
	fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "-l", readyCommand)
	fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "Enter")
	fixture.waitForFileContents(t, readyPath, "ready")

	marker := fmt.Sprintf("NTM_E2E_REPLAY_ONCE_%d", time.Now().UnixNano())
	markerPath := filepath.Join(fixture.root, "replay-markers.txt")
	prompt := fmt.Sprintf("printf '%%s\\n' %q >> %q", marker, markerPath)
	entry := history.HistoryEntry{
		ID:        fmt.Sprintf("%d-e2ereplay", time.Now().UnixMilli()),
		Timestamp: time.Now().UTC(),
		Session:   fixture.session,
		Targets:   []string{fixture.paneID},
		Prompt:    prompt,
		Source:    history.SourceCLI,
		Success:   true,
	}
	historyPayload, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal replay history entry: %v", err)
	}
	historyDir := filepath.Join(fixture.dataDir, "ntm")
	if err := os.MkdirAll(historyDir, 0o700); err != nil {
		t.Fatalf("create replay history directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(historyDir, "history.jsonl"), append(historyPayload, '\n'), 0o600); err != nil {
		t.Fatalf("write replay history: %v", err)
	}

	process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env,
		"--robot-replay="+fixture.session,
		"--id="+entry.ID,
		"--robot-format=json",
	)
	if process.exitCode != 0 {
		t.Fatalf("robot replay exit=%d, want 0; stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
	}
	if len(bytes.TrimSpace(process.stderr)) != 0 {
		t.Fatalf("robot replay wrote stderr: %s", process.stderr)
	}

	type replaySendResult struct {
		Success    bool              `json:"success"`
		Timestamp  string            `json:"timestamp"`
		Session    string            `json:"session"`
		Targets    []string          `json:"targets"`
		Successful []string          `json:"successful"`
		Failed     []json.RawMessage `json:"failed"`
	}
	var envelope struct {
		Success         bool              `json:"success"`
		Timestamp       string            `json:"timestamp"`
		HistoryID       string            `json:"history_id"`
		OriginalCommand string            `json:"original_command"`
		Session         string            `json:"session"`
		TargetPanes     []string          `json:"target_panes"`
		Replayed        bool              `json:"replayed"`
		SendResult      *replaySendResult `json:"send_result"`
	}
	decodeSingleRobotJSON(t, process.stdout, &envelope)
	if !envelope.Success || !envelope.Replayed || envelope.HistoryID != entry.ID ||
		envelope.OriginalCommand != prompt || envelope.Session != fixture.session {
		t.Fatalf("robot replay envelope identity/state = %+v", envelope)
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.Timestamp); err != nil {
		t.Fatalf("robot replay timestamp %q is not RFC3339: %v", envelope.Timestamp, err)
	}
	if len(envelope.TargetPanes) != 1 || envelope.TargetPanes[0] != fixture.paneID {
		t.Fatalf("robot replay target_panes=%v, want [%s]", envelope.TargetPanes, fixture.paneID)
	}
	if envelope.SendResult == nil || !envelope.SendResult.Success || envelope.SendResult.Session != fixture.session {
		t.Fatalf("robot replay send_result = %+v", envelope.SendResult)
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.SendResult.Timestamp); err != nil {
		t.Fatalf("robot replay send timestamp %q is not RFC3339: %v", envelope.SendResult.Timestamp, err)
	}
	if len(envelope.SendResult.Targets) != 1 || envelope.SendResult.Targets[0] != "0" ||
		len(envelope.SendResult.Successful) != 1 || envelope.SendResult.Successful[0] != "0" ||
		envelope.SendResult.Failed == nil || len(envelope.SendResult.Failed) != 0 {
		t.Fatalf("robot replay send delivery arrays = targets:%v successful:%v failed:%v",
			envelope.SendResult.Targets, envelope.SendResult.Successful, envelope.SendResult.Failed)
	}

	fixture.waitForFileContents(t, markerPath, marker)
	// A duplicate send submitted by the replay wrapper would execute immediately.
	time.Sleep(250 * time.Millisecond)
	markers, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read replay marker file: %v", err)
	}
	if count := strings.Count(string(markers), marker); count != 1 {
		t.Fatalf("replay marker count=%d, want exactly 1; contents=%q", count, markers)
	}
}

func TestE2ERobotInterruptFollowUpRedactionBuiltBinaryRealTmux(t *testing.T) {
	CommonE2EPrerequisites(t)
	fixture := newRobotProcessFixture(t, "interrupt-redaction")

	configPath := filepath.Join(fixture.configDir, "ntm", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	writeRedactionMode := func(mode string) {
		t.Helper()
		contents := fmt.Sprintf("[redaction]\nmode = %q\n", mode)
		if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s redaction config: %v", mode, err)
		}
	}
	waitForPaneCommand := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			current := strings.TrimSpace(fixture.mustTMUXOutput(t, "display-message", "-p", "-t", fixture.paneID, "#{pane_current_command}"))
			if current == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("pane command=%q, want %q", current, want)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	waitForPaneOutput := func(want string) string {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			capture := fixture.mustTMUXOutput(t, "capture-pane", "-p", "-t", fixture.paneID, "-S", "-200")
			if strings.Contains(capture, want) {
				return capture
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %q in pane output: %s", want, capture)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	startInterruptibleCommand := func() {
		t.Helper()
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "-l", "cat")
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "Enter")
		waitForPaneCommand("cat")
	}

	type interruptRedaction struct {
		Mode     string `json:"mode"`
		Findings int    `json:"findings"`
		Action   string `json:"action"`
	}
	type interruptEnvelope struct {
		Success       bool                `json:"success"`
		Timestamp     string              `json:"timestamp"`
		Error         string              `json:"error"`
		ErrorCode     string              `json:"error_code"`
		Session       string              `json:"session"`
		Interrupted   []string            `json:"interrupted"`
		MessageSent   bool                `json:"message_sent"`
		Message       string              `json:"message"`
		Redaction     *interruptRedaction `json:"redaction"`
		ReadyForInput []string            `json:"ready_for_input"`
		Failed        []json.RawMessage   `json:"failed"`
	}
	secret := strings.TrimPrefix(fakePassword, "password=")
	followUp := "Continue using " + fakePassword
	runInterrupt := func(message string) robotBoundaryProcessResult {
		t.Helper()
		return runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env,
			"--robot-interrupt="+fixture.session,
			"--panes="+fixture.paneID,
			"--msg="+message,
			"--force",
			"--no-wait",
			"--robot-format=json",
		)
	}

	t.Run("block_prevents_interrupt_and_message", func(t *testing.T) {
		writeRedactionMode("block")
		startInterruptibleCommand()
		process := runInterrupt(followUp)
		if process.exitCode != 1 || len(bytes.TrimSpace(process.stderr)) != 0 {
			t.Fatalf("blocked interrupt exit=%d stderr=%q stdout=%s", process.exitCode, process.stderr, process.stdout)
		}
		if bytes.Contains(process.stdout, []byte(secret)) {
			t.Fatalf("blocked interrupt leaked secret in stdout: %s", process.stdout)
		}
		var envelope interruptEnvelope
		decodeSingleRobotJSON(t, process.stdout, &envelope)
		if envelope.Success || envelope.ErrorCode != "SENSITIVE_DATA_BLOCKED" || envelope.Session != fixture.session ||
			envelope.MessageSent || envelope.Redaction == nil || envelope.Redaction.Action != "block" || envelope.Redaction.Findings == 0 {
			t.Fatalf("blocked interrupt envelope = %+v", envelope)
		}
		if envelope.Interrupted == nil || len(envelope.Interrupted) != 0 ||
			envelope.ReadyForInput == nil || len(envelope.ReadyForInput) != 0 ||
			envelope.Failed == nil || len(envelope.Failed) == 0 {
			t.Fatalf("blocked interrupt result arrays = interrupted:%v ready:%v failed:%v",
				envelope.Interrupted, envelope.ReadyForInput, envelope.Failed)
		}
		if _, err := time.Parse(time.RFC3339Nano, envelope.Timestamp); err != nil {
			t.Fatalf("blocked interrupt timestamp %q is not RFC3339: %v", envelope.Timestamp, err)
		}
		time.Sleep(250 * time.Millisecond)
		waitForPaneCommand("cat")
		capture := fixture.mustTMUXOutput(t, "capture-pane", "-p", "-t", fixture.paneID, "-S", "-200")
		if strings.Contains(capture, secret) || strings.Contains(capture, "[REDACTED:PASSWORD:") {
			t.Fatalf("blocked interrupt typed secret into pane: %s", capture)
		}

		// End the deliberately long-running setup command before the redact case.
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "C-c")
		resetPath := filepath.Join(fixture.root, "block-reset-ready")
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "-l", fmt.Sprintf("printf ready > %s", resetPath))
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "Enter")
		fixture.waitForFileContents(t, resetPath, "ready")
	})

	t.Run("redact_delivers_placeholder_without_secret", func(t *testing.T) {
		writeRedactionMode("redact")
		startInterruptibleCommand()
		process := runInterrupt(followUp)
		if process.exitCode != 0 || len(bytes.TrimSpace(process.stderr)) != 0 {
			t.Fatalf("redacted interrupt exit=%d stderr=%q stdout=%s", process.exitCode, process.stderr, process.stdout)
		}
		if bytes.Contains(process.stdout, []byte(secret)) {
			t.Fatalf("redacted interrupt leaked secret in stdout: %s", process.stdout)
		}
		var envelope interruptEnvelope
		decodeSingleRobotJSON(t, process.stdout, &envelope)
		if !envelope.Success || envelope.ErrorCode != "" || envelope.Session != fixture.session ||
			!envelope.MessageSent || envelope.Redaction == nil || envelope.Redaction.Action != "redact" || envelope.Redaction.Findings == 0 ||
			!strings.Contains(envelope.Message, "[REDACTED:PASSWORD:") || strings.Contains(envelope.Message, secret) {
			t.Fatalf("redacted interrupt envelope = %+v", envelope)
		}
		if len(envelope.Interrupted) != 1 || len(envelope.ReadyForInput) != 1 ||
			envelope.Failed == nil || len(envelope.Failed) != 0 {
			t.Fatalf("redacted interrupt result arrays = interrupted:%v ready:%v failed:%v",
				envelope.Interrupted, envelope.ReadyForInput, envelope.Failed)
		}
		capture := waitForPaneOutput("[REDACTED:PASSWORD:")
		if strings.Contains(capture, secret) || !strings.Contains(capture, "[REDACTED:PASSWORD:") {
			t.Fatalf("redacted interrupt pane output = %s", capture)
		}
	})
}

func supportsRobotFormat(t *testing.T) bool {
	t.Helper()
	cmd := exec.Command("ntm", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "--robot-format")
}
