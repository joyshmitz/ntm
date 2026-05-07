// Package pipeline — iteration_sources.go implements the resolver for
// foreach iteration source fields (Beads, Pairs, Debates). Phase B-1 owns
// only the beads resolver; pairs/debates land in sibling beads.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// IterationSourceResolver expands foreach iteration source expressions into
// concrete []interface{} item lists. Runners are injectable for tests; nil
// runners default to real /bin/sh and `br` exec.
type IterationSourceResolver struct {
	ProjectDir string
	RunShell   func(ctx context.Context, shellCmd string) ([]byte, error)
	RunBr      func(ctx context.Context, args []string) ([]byte, error)
}

// ResolveBeads expands a foreach.Beads expression to a list of bead records.
//
// Two input forms:
//   - Shell pipe: "$(br list --label=hypothesis ... | jq ...)" — execute the
//     shell command; if stdout parses as a JSON array, each element becomes an
//     iteration item; otherwise stdout is line-delimited and each non-empty
//     line is treated as a bead ID string.
//   - Structured query: "hypothesis,state:active" — comma-separated terms.
//     Bare tokens are treated as labels (AND); `key:value` terms map to
//     `br list` flags (status/state, type, priority, assignee). The resolver
//     shells out to `br list --json` and returns one map per bead with at
//     minimum {id, title, description, labels, status}, suitable for
//     ${item.id}, ${item.title}, etc.
//
// Empty stdout yields zero iterations (not an error).
func (r *IterationSourceResolver) ResolveBeads(ctx context.Context, expr string) ([]interface{}, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return []interface{}{}, nil
	}

	if shellCmd, ok := stripShellInvocation(expr); ok {
		out, err := r.runShell(ctx, shellCmd)
		if err != nil {
			return nil, fmt.Errorf("beads shell command failed: %w", err)
		}
		return parseBeadsShellOutput(out)
	}

	args, err := parseStructuredBeadsQuery(expr)
	if err != nil {
		return nil, fmt.Errorf("beads structured query %q: %w", expr, err)
	}
	out, err := r.runBr(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("br list failed: %w", err)
	}
	return parseBrListJSON(out)
}

// stripShellInvocation returns the inner command for a "$(...)" expression
// and ok=true. For any other input it returns ("", false).
func stripShellInvocation(s string) (string, bool) {
	if strings.HasPrefix(s, "$(") && strings.HasSuffix(s, ")") {
		return s[2 : len(s)-1], true
	}
	return "", false
}

// parseBeadsShellOutput parses stdout from a beads-source shell command.
// Tries JSON-array first; falls back to one-ID-per-line.
func parseBeadsShellOutput(out []byte) ([]interface{}, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return []interface{}{}, nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var arr []interface{}
		if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
			return arr, nil
		}
		// fall through to line parse if the [ was just inside a line
	}

	lines := strings.Split(trimmed, "\n")
	items := make([]interface{}, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Strip surrounding quotes — `jq '.issues[].id'` emits "bd-foo".
		line = strings.Trim(line, `"'`)
		if line == "" {
			continue
		}
		items = append(items, line)
	}
	return items, nil
}

// parseStructuredBeadsQuery turns "label1,label2,key:value,..." into the
// argv for `br list --json`. Bare tokens become --label flags (AND logic).
// Recognised key:value forms: status/state, type, priority, assignee.
func parseStructuredBeadsQuery(expr string) ([]string, error) {
	args := []string{"list", "--json"}
	for _, raw := range strings.Split(expr, ",") {
		term := strings.TrimSpace(raw)
		if term == "" {
			continue
		}
		key, val, hasColon := splitKV(term)
		if !hasColon {
			args = append(args, "--label", term)
			continue
		}
		val = strings.TrimSpace(val)
		if val == "" {
			return nil, fmt.Errorf("empty value for %q", key)
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "label":
			args = append(args, "--label", val)
		case "status", "state":
			args = append(args, "--status", val)
		case "type":
			args = append(args, "--type", val)
		case "priority":
			args = append(args, "--priority", val)
		case "assignee":
			args = append(args, "--assignee", val)
		default:
			return nil, fmt.Errorf("unsupported filter key %q (allowed: label, status, type, priority, assignee)", key)
		}
	}
	return args, nil
}

// splitKV splits "key:value" once on the first colon. Returns hasColon=false
// when no colon is present.
func splitKV(s string) (key, value string, hasColon bool) {
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return s, "", false
	}
	return s[:idx], s[idx+1:], true
}

// parseBrListJSON parses `br list --json` output (an object with "issues":
// [...]) into []interface{} of map records. Empty output yields zero items.
func parseBrListJSON(out []byte) ([]interface{}, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return []interface{}{}, nil
	}
	var doc struct {
		Issues []map[string]interface{} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return nil, fmt.Errorf("parse br --json output: %w", err)
	}
	items := make([]interface{}, 0, len(doc.Issues))
	for _, issue := range doc.Issues {
		items = append(items, issue)
	}
	return items, nil
}

// runShell executes a shell command, defaulting to /bin/sh -c in ProjectDir.
func (r *IterationSourceResolver) runShell(ctx context.Context, shellCmd string) ([]byte, error) {
	if r.RunShell != nil {
		return r.RunShell(ctx, shellCmd)
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", shellCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	if r.ProjectDir != "" {
		cmd.Dir = r.ProjectDir
	}
	return cmd.Output()
}

// runBr executes the br CLI with the given args, defaulting to ProjectDir.
func (r *IterationSourceResolver) runBr(ctx context.Context, args []string) ([]byte, error) {
	if r.RunBr != nil {
		return r.RunBr(ctx, args)
	}
	cmd := exec.CommandContext(ctx, "br", args...)
	if r.ProjectDir != "" {
		cmd.Dir = r.ProjectDir
	}
	return cmd.Output()
}
