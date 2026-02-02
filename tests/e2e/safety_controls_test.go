package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// safetyStatusResponse is the JSON output for ntm safety status.
type safetyStatusResponse struct {
	Installed     bool   `json:"installed"`
	PolicyPath    string `json:"policy_path,omitempty"`
	BlockedCount  int    `json:"blocked_rules"`
	ApprovalCount int    `json:"approval_rules"`
	AllowedCount  int    `json:"allowed_rules"`
	WrapperPath   string `json:"wrapper_path,omitempty"`
	HookInstalled bool   `json:"hook_installed"`
}

// safetyInstallResponse is the JSON output for ntm safety install.
type safetyInstallResponse struct {
	Success    bool   `json:"success"`
	GitWrapper string `json:"git_wrapper,omitempty"`
	RmWrapper  string `json:"rm_wrapper,omitempty"`
	Hook       string `json:"hook,omitempty"`
	Policy     string `json:"policy,omitempty"`
}

// safetyUninstallResponse is the JSON output for ntm safety uninstall.
type safetyUninstallResponse struct {
	Success bool     `json:"success"`
	Removed []string `json:"removed,omitempty"`
}

// blockedResponse is the JSON output for ntm safety blocked.
type blockedResponse struct {
	Entries []blockedEntry `json:"entries"`
	Count   int            `json:"count"`
}

// blockedEntry represents a single blocked command entry.
type blockedEntry struct {
	Timestamp string `json:"timestamp"`
	Command   string `json:"command"`
	Reason    string `json:"reason,omitempty"`
	Session   string `json:"session,omitempty"`
}

func runSafetyStatus(t *testing.T, dir string) safetyStatusResponse {
	t.Helper()
	out := runCmd(t, dir, "ntm", "--json", "safety", "status")
	var resp safetyStatusResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal safety status: %v\nout=%s", err, string(out))
	}
	return resp
}

func runSafetyInstall(t *testing.T, dir string, force bool) safetyInstallResponse {
	t.Helper()
	args := []string{"--json", "safety", "install"}
	if force {
		args = append(args, "--force")
	}
	out := runCmd(t, dir, "ntm", args...)
	var resp safetyInstallResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal safety install: %v\nout=%s", err, string(out))
	}
	return resp
}

func runSafetyUninstall(t *testing.T, dir string) safetyUninstallResponse {
	t.Helper()
	out := runCmd(t, dir, "ntm", "--json", "safety", "uninstall")
	var resp safetyUninstallResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal safety uninstall: %v\nout=%s", err, string(out))
	}
	return resp
}

func runSafetyBlocked(t *testing.T, dir string, hours int) blockedResponse {
	t.Helper()
	out := runCmd(t, dir, "ntm", "--json", "safety", "blocked", "--hours", "24")
	var resp blockedResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal safety blocked: %v\nout=%s", err, string(out))
	}
	return resp
}

func TestE2ESafetyControls_Status(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("status_shows_not_installed_by_default", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-SAFETY] Testing safety status with no installation")
		resp := runSafetyStatus(t, workDir)

		logger.Log("[E2E-SAFETY] Status: installed=%v, hook=%v, blocked=%d, approval=%d, allowed=%d",
			resp.Installed, resp.HookInstalled, resp.BlockedCount, resp.ApprovalCount, resp.AllowedCount)

		// Wrappers should not be installed
		if resp.Installed {
			t.Fatalf("expected installed=false in fresh environment")
		}
		if resp.HookInstalled {
			t.Fatalf("expected hook_installed=false in fresh environment")
		}

		// Default policy should have rules
		if resp.BlockedCount == 0 {
			t.Fatalf("expected non-zero blocked_rules from default policy")
		}
		if resp.AllowedCount == 0 {
			t.Fatalf("expected non-zero allowed_rules from default policy")
		}
	})

	t.Run("status_shows_custom_policy_path", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Create custom policy
		policyYAML := `version: 1
automation:
  auto_push: false
  auto_commit: true
  force_release: approval
allowed:
  - pattern: 'echo\s+hello'
    reason: "hello is safe"
blocked:
  - pattern: 'rm\s+-rf'
    reason: "dangerous"
  - pattern: 'dd\s+if='
    reason: "dangerous"
approval_required:
  - pattern: 'reboot'
    reason: "needs approval"
`
		writePolicyFile(t, homeDir, policyYAML)

		logger.Log("[E2E-SAFETY] Testing safety status with custom policy")
		resp := runSafetyStatus(t, workDir)

		if resp.PolicyPath == "" {
			t.Fatalf("expected policy_path to be set when custom policy exists")
		}
		if resp.BlockedCount != 2 {
			t.Fatalf("expected 2 blocked rules, got %d", resp.BlockedCount)
		}
		if resp.AllowedCount != 1 {
			t.Fatalf("expected 1 allowed rule, got %d", resp.AllowedCount)
		}
		if resp.ApprovalCount != 1 {
			t.Fatalf("expected 1 approval rule, got %d", resp.ApprovalCount)
		}
	})
}

func TestE2ESafetyControls_InstallUninstall(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("install_creates_wrappers_and_hooks", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-SAFETY] Testing safety install")
		resp := runSafetyInstall(t, workDir, false)

		if !resp.Success {
			t.Fatalf("expected success=true")
		}
		logger.Log("[E2E-SAFETY] Install response: git=%s, rm=%s, hook=%s, policy=%s",
			resp.GitWrapper, resp.RmWrapper, resp.Hook, resp.Policy)

		// Verify git wrapper exists
		gitWrapper := filepath.Join(homeDir, ".ntm", "bin", "git")
		if _, err := os.Stat(gitWrapper); os.IsNotExist(err) {
			t.Fatalf("git wrapper not created at %s", gitWrapper)
		}

		// Verify rm wrapper exists
		rmWrapper := filepath.Join(homeDir, ".ntm", "bin", "rm")
		if _, err := os.Stat(rmWrapper); os.IsNotExist(err) {
			t.Fatalf("rm wrapper not created at %s", rmWrapper)
		}

		// Verify Claude hook exists
		hook := filepath.Join(homeDir, ".claude", "hooks", "PreToolUse", "ntm-safety.sh")
		if _, err := os.Stat(hook); os.IsNotExist(err) {
			t.Fatalf("Claude hook not created at %s", hook)
		}

		// Verify policy file exists
		policy := filepath.Join(homeDir, ".ntm", "policy.yaml")
		if _, err := os.Stat(policy); os.IsNotExist(err) {
			t.Fatalf("policy file not created at %s", policy)
		}

		// Verify status shows installed
		status := runSafetyStatus(t, workDir)
		if !status.Installed {
			t.Fatalf("expected installed=true after install")
		}
		if !status.HookInstalled {
			t.Fatalf("expected hook_installed=true after install")
		}
	})

	t.Run("install_force_overwrites_existing", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// First install
		logger.Log("[E2E-SAFETY] First install")
		runSafetyInstall(t, workDir, false)

		// Write custom content to policy
		policyPath := filepath.Join(homeDir, ".ntm", "policy.yaml")
		customContent := []byte("# Custom policy\nversion: 1\nblocked: []\nallowed: []\napproval_required: []\n")
		if err := os.WriteFile(policyPath, customContent, 0644); err != nil {
			t.Fatalf("write custom policy: %v", err)
		}

		// Second install with force
		logger.Log("[E2E-SAFETY] Second install with --force")
		resp := runSafetyInstall(t, workDir, true)

		if !resp.Success {
			t.Fatalf("expected success=true")
		}

		// Verify policy was overwritten
		content, err := os.ReadFile(policyPath)
		if err != nil {
			t.Fatalf("read policy: %v", err)
		}
		if string(content) == string(customContent) {
			t.Fatalf("expected policy to be overwritten with --force")
		}
	})

	t.Run("uninstall_removes_wrappers_and_hooks", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// First install
		logger.Log("[E2E-SAFETY] Installing safety system")
		runSafetyInstall(t, workDir, false)

		// Then uninstall
		logger.Log("[E2E-SAFETY] Uninstalling safety system")
		resp := runSafetyUninstall(t, workDir)

		if !resp.Success {
			t.Fatalf("expected success=true")
		}
		if len(resp.Removed) == 0 {
			t.Fatalf("expected removed files to be listed")
		}
		logger.Log("[E2E-SAFETY] Removed: %v", resp.Removed)

		// Verify wrappers removed
		gitWrapper := filepath.Join(homeDir, ".ntm", "bin", "git")
		if _, err := os.Stat(gitWrapper); err == nil {
			t.Fatalf("git wrapper should be removed")
		}

		rmWrapper := filepath.Join(homeDir, ".ntm", "bin", "rm")
		if _, err := os.Stat(rmWrapper); err == nil {
			t.Fatalf("rm wrapper should be removed")
		}

		// Verify hook removed
		hook := filepath.Join(homeDir, ".claude", "hooks", "PreToolUse", "ntm-safety.sh")
		if _, err := os.Stat(hook); err == nil {
			t.Fatalf("Claude hook should be removed")
		}

		// Verify status shows not installed
		status := runSafetyStatus(t, workDir)
		if status.Installed {
			t.Fatalf("expected installed=false after uninstall")
		}
		if status.HookInstalled {
			t.Fatalf("expected hook_installed=false after uninstall")
		}
	})
}

func TestE2ESafetyControls_BlockedHistory(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("blocked_shows_empty_history", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		logger.Log("[E2E-SAFETY] Testing blocked history with no blocked commands")
		resp := runSafetyBlocked(t, workDir, 24)

		if resp.Count != 0 {
			t.Fatalf("expected 0 blocked entries, got %d", resp.Count)
		}
		if len(resp.Entries) != 0 {
			t.Fatalf("expected empty entries slice, got %d entries", len(resp.Entries))
		}
	})
}

func TestE2ESafetyControls_SLBApproval(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("slb_pattern_requires_two_person_approval", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Create policy with SLB rule
		policyYAML := `version: 1
automation:
  auto_push: false
  auto_commit: true
  force_release: approval
allowed: []
blocked: []
approval_required:
  - pattern: 'force_release'
    reason: "Force releasing another agent's file reservation"
    slb: true
  - pattern: 'git\s+commit\s+--amend'
    reason: "Amending commits rewrites history"
`
		writePolicyFile(t, homeDir, policyYAML)

		logger.Log("[E2E-SAFETY] Testing SLB approval pattern")

		// Check force_release (SLB required)
		out := runCmd(t, workDir, "ntm", "--json", "safety", "check", "--", "force_release", "lock-123")

		var resp struct {
			Action  string `json:"action"`
			Pattern string `json:"pattern"`
			Policy  struct {
				SLB bool `json:"slb"`
			} `json:"policy"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			t.Fatalf("unmarshal safety check: %v\nout=%s", err, string(out))
		}

		if resp.Action != "approve" {
			t.Fatalf("expected action=approve for SLB command, got %q", resp.Action)
		}
		if !resp.Policy.SLB {
			t.Fatalf("expected policy.slb=true for SLB command")
		}

		logger.Log("[E2E-SAFETY] SLB pattern check: action=%s, slb=%v", resp.Action, resp.Policy.SLB)

		// Check git commit --amend (not SLB)
		out2 := runCmd(t, workDir, "ntm", "--json", "safety", "check", "--", "git", "commit", "--amend")

		var resp2 struct {
			Action string `json:"action"`
			Policy struct {
				SLB bool `json:"slb"`
			} `json:"policy"`
		}
		if err := json.Unmarshal(out2, &resp2); err != nil {
			t.Fatalf("unmarshal safety check: %v\nout=%s", err, string(out2))
		}

		if resp2.Action != "approve" {
			t.Fatalf("expected action=approve for commit --amend, got %q", resp2.Action)
		}
		if resp2.Policy.SLB {
			t.Fatalf("expected policy.slb=false for commit --amend")
		}
	})
}

func TestE2ESafetyControls_AutomationConfig(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	t.Run("automation_settings_in_policy", func(t *testing.T) {
		homeDir := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		// Create policy with specific automation settings
		policyYAML := `version: 1
automation:
  auto_push: true
  auto_commit: false
  force_release: never
allowed: []
blocked: []
approval_required: []
`
		writePolicyFile(t, homeDir, policyYAML)

		logger.Log("[E2E-SAFETY] Testing automation config in policy")

		// Get policy show output
		resp := runPolicyShow(t, workDir)
		if resp.IsDefault {
			t.Fatalf("expected is_default=false with custom policy")
		}

		logger.Log("[E2E-SAFETY] Policy stats: blocked=%d, approval=%d, allowed=%d",
			resp.Stats.Blocked, resp.Stats.Approval, resp.Stats.Allowed)
	})

	t.Run("force_release_modes", func(t *testing.T) {
		testCases := []struct {
			name        string
			forceMode   string
			expectValid bool
		}{
			{"never_mode", "never", true},
			{"approval_mode", "approval", true},
			{"auto_mode", "auto", true},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				homeDir := t.TempDir()
				workDir := t.TempDir()
				t.Setenv("HOME", homeDir)
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())

				policyYAML := `version: 1
automation:
  auto_push: false
  auto_commit: true
  force_release: ` + tc.forceMode + `
allowed: []
blocked: []
approval_required: []
`
				writePolicyFile(t, homeDir, policyYAML)

				logger.Log("[E2E-SAFETY] Testing force_release mode: %s", tc.forceMode)

				// Verify policy loads without error
				resp := runPolicyShow(t, workDir)
				if resp.IsDefault {
					t.Fatalf("expected custom policy to load for force_release=%s", tc.forceMode)
				}
			})
		}
	})
}
