package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/policy"
)

func TestPolicyPrecedence(t *testing.T) {
	// Create a temporary policy file with conflicting rules
	content := `
version: 1
allowed:
  - pattern: 'git\s+push\s+.*--force-with-lease'
blocked:
  - pattern: 'git\s+push\s+.*--force'
approval_required:
  - pattern: 'git\s+rebase'
`
	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "policy.yaml")
	if err := os.WriteFile(policyPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write policy: %v", err)
	}

	p, err := policy.Load(policyPath)
	if err != nil {
		t.Fatalf("failed to load policy: %v", err)
	}

	tests := []struct {
		name    string
		command string
		want    policy.Action
	}{
		{
			name:    "Allowed takes precedence over blocked",
			command: "git push origin master --force-with-lease",
			want:    policy.ActionAllow,
		},
		{
			name:    "Blocked pattern matches",
			command: "git push origin master --force",
			want:    policy.ActionBlock,
		},
		{
			name:    "Approval required",
			command: "git rebase master",
			want:    policy.ActionApprove,
		},
		{
			name:    "Implicitly allowed",
			command: "ls -la",
			want:    "", // nil match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := p.Check(tt.command)
			if tt.want == "" {
				if match != nil {
					t.Errorf("Check(%q) = %v, want nil", tt.command, match)
				}
			} else {
				if match == nil {
					t.Errorf("Check(%q) = nil, want %v", tt.command, tt.want)
				} else if match.Action != tt.want {
					t.Errorf("Check(%q) action = %v, want %v", tt.command, match.Action, tt.want)
				}
			}
		})
	}
}
