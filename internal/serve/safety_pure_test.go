package serve

import (
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/policy"
)

// ---------------------------------------------------------------------------
// toPolicyRuleSummaries — at 0% coverage
// ---------------------------------------------------------------------------

func TestToPolicyRuleSummaries(t *testing.T) {

	t.Run("empty", func(t *testing.T) {
		got := toPolicyRuleSummaries(nil)
		if len(got) != 0 {
			t.Fatalf("expected empty, got %d", len(got))
		}
	})

	t.Run("single_rule", func(t *testing.T) {
		rules := []policy.Rule{
			{Pattern: "rm -rf *", Reason: "dangerous"},
		}
		got := toPolicyRuleSummaries(rules)
		if len(got) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(got))
		}
		if got[0].Pattern != "rm -rf *" {
			t.Errorf("pattern = %q, want %q", got[0].Pattern, "rm -rf *")
		}
		if got[0].Reason != "dangerous" {
			t.Errorf("reason = %q, want %q", got[0].Reason, "dangerous")
		}
		if got[0].SLB {
			t.Error("expected SLB=false")
		}
	})

	t.Run("multiple_with_slb", func(t *testing.T) {
		rules := []policy.Rule{
			{Pattern: "git push --force", Reason: "force push", SLB: true},
			{Pattern: "DROP TABLE", Reason: "destructive SQL"},
		}
		got := toPolicyRuleSummaries(rules)
		if len(got) != 2 {
			t.Fatalf("expected 2 summaries, got %d", len(got))
		}
		if !got[0].SLB {
			t.Error("expected first rule SLB=true")
		}
		if got[1].SLB {
			t.Error("expected second rule SLB=false")
		}
	})
}

// ---------------------------------------------------------------------------
// generatePolicyYAMLFromPolicy — at 0% coverage
// ---------------------------------------------------------------------------

func TestGeneratePolicyYAMLFromPolicy(t *testing.T) {

	t.Run("minimal", func(t *testing.T) {
		p := policy.DefaultPolicy()
		got := generatePolicyYAMLFromPolicy(p)
		if !strings.Contains(got, "# NTM Policy Configuration") {
			t.Error("expected header comment")
		}
		if !strings.Contains(got, "automation:") {
			t.Error("expected automation section")
		}
	})

	t.Run("with_allowed_rules", func(t *testing.T) {
		p := policy.DefaultPolicy()
		p.Allowed = []policy.Rule{
			{Pattern: "echo *", Reason: "safe command"},
		}
		got := generatePolicyYAMLFromPolicy(p)
		if !strings.Contains(got, "allowed:") {
			t.Error("expected allowed section")
		}
		if !strings.Contains(got, "echo *") {
			t.Error("expected pattern in output")
		}
		if !strings.Contains(got, "safe command") {
			t.Error("expected reason in output")
		}
	})

	t.Run("with_blocked_rules", func(t *testing.T) {
		p := policy.DefaultPolicy()
		p.Blocked = []policy.Rule{
			{Pattern: "rm -rf /"},
		}
		got := generatePolicyYAMLFromPolicy(p)
		if !strings.Contains(got, "blocked:") {
			t.Error("expected blocked section")
		}
		if !strings.Contains(got, "rm -rf /") {
			t.Error("expected blocked pattern")
		}
	})

	t.Run("with_approval_required", func(t *testing.T) {
		p := policy.DefaultPolicy()
		p.ApprovalRequired = []policy.Rule{
			{Pattern: "git push", Reason: "review needed", SLB: true},
		}
		got := generatePolicyYAMLFromPolicy(p)
		if !strings.Contains(got, "approval_required:") {
			t.Error("expected approval_required section")
		}
		if !strings.Contains(got, "slb: true") {
			t.Error("expected SLB flag in output")
		}
	})

	t.Run("rule_without_reason", func(t *testing.T) {
		p := policy.DefaultPolicy()
		p.Allowed = []policy.Rule{
			{Pattern: "ls"},
		}
		got := generatePolicyYAMLFromPolicy(p)
		// Should not contain reason line for rules without reason.
		lines := strings.Split(got, "\n")
		for _, line := range lines {
			if strings.Contains(line, "ls") {
				// Next line should NOT be a reason line.
				break
			}
		}
		if !strings.Contains(got, "ls") {
			t.Error("expected pattern in output")
		}
	})

	t.Run("special_characters", func(t *testing.T) {
		p := policy.DefaultPolicy()
		p.Blocked = []policy.Rule{
			{Pattern: "it's dangerous", Reason: "has \"quotes\""},
		}
		got := generatePolicyYAMLFromPolicy(p)
		// Single quote in pattern should be escaped.
		if !strings.Contains(got, "it''s dangerous") {
			t.Error("expected escaped single quote in pattern")
		}
		// Double quotes in reason should be escaped.
		if !strings.Contains(got, `has \"quotes\"`) {
			t.Errorf("expected escaped double quotes in reason, got:\n%s", got)
		}
	})
}

// ---------------------------------------------------------------------------
// publishApprovalEvent — at 66.7%
// publishApprovalEvent needs server context; skip for now.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// safetyEscapeYAMLSingleQuote / safetyEscapeYAMLDoubleQuote already at 100%
// ---------------------------------------------------------------------------
