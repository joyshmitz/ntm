package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	"github.com/Dicklesworthstone/ntm/internal/reservationsim"
)

func jsonEncodeIndent(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func TestBuildLocksAdviceResult_AgentMailUnavailableKeepsProofMode(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	result := buildLocksAdviceResult(
		"proj",
		"",
		"/repo",
		nil,
		nil,
		[]string{"Agent Mail server unavailable"},
		now,
		true,
		"connection refused",
	)

	if !result.Success {
		t.Fatal("Success = false, want proof-mode success")
	}
	if result.AgentMailAvailable {
		t.Fatal("AgentMailAvailable = true, want false")
	}
	if result.Reservations.AgentMailAvailable {
		t.Fatal("reservation report AgentMailAvailable = true, want false")
	}
	if len(result.Reservations.Warnings) != 1 {
		t.Fatalf("reservation warnings = %d, want 1", len(result.Reservations.Warnings))
	}
	if result.RecommendationCount != 0 {
		t.Fatalf("RecommendationCount = %d, want 0", result.RecommendationCount)
	}
}

func TestBuildLocksAdviceResult_CombinesReservationAndWorktreeLogRows(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	result := buildLocksAdviceResult(
		"proj",
		"BlueLake",
		"/repo",
		[]agentmail.FileReservation{
			{
				ID:          11,
				PathPattern: "**",
				AgentName:   "BlueLake",
				Exclusive:   true,
				CreatedTS:   agentmail.FlexTime{Time: now.Add(-3 * time.Hour)},
				ExpiresTS:   agentmail.FlexTime{Time: now.Add(5 * time.Minute)},
			},
		},
		nil,
		nil,
		now,
		false,
		"",
	)

	if !result.AgentMailAvailable {
		t.Fatal("AgentMailAvailable = false, want true")
	}
	if result.RecommendationCount != 1 {
		t.Fatalf("RecommendationCount = %d, want 1", result.RecommendationCount)
	}
	if len(result.LogRows) != 1 {
		t.Fatalf("LogRows = %d, want 1", len(result.LogRows))
	}
	row := result.LogRows[0]
	if !locksTextEqual(row.Source, "reservation") || row.ReservationID != 11 || !locksTextEqual(row.PathPattern, "**") || !locksTextEqual(row.Holder, "BlueLake") {
		t.Fatalf("unexpected row: %+v", row)
	}
	if !locksTextEqual(row.Action, reservationsim.ReservationActionNarrow) && !locksTextEqual(row.Action, reservationsim.ReservationActionRenew) {
		t.Fatalf("Action = %q, want narrow or renew", row.Action)
	}
}

func locksTextEqual(a, b string) bool {
	return strings.Compare(a, b) == 0
}

// Test the path-matching helper that decides whether a configured
// reservation pattern (which may include `/` directory prefixes or
// `*`/`**` glob meta) covers a queried path. The wrapper-facing
// contract from ntm#127 depends on this function being precise:
// false positives would tell wrappers a path is held when it isn't,
// false negatives would let wrappers proceed when they shouldn't.
func TestLocksCheckPathMatches_ExactAndPrefixAndGlobs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		path    string
		pattern string
		want    bool
	}{
		// Exact match
		{"exact", "src/auth.rs", "src/auth.rs", true},
		// Path inside directory pattern (prefix with /)
		{"prefix_subdir", "src/auth.rs", "src", true},
		{"prefix_trailing_slash", "src/auth.rs", "src/", true},
		// Path not inside pattern
		{"unrelated", "tests/auth.rs", "src", false},
		{"sibling", "src2/auth.rs", "src", false},
		// Recursive glob
		{"recursive_glob_match", "src/auth/handler.rs", "src/**", true},
		{"recursive_glob_root_match", "src/auth.rs", "src/**", true},
		{"recursive_glob_unrelated", "tests/auth.rs", "src/**", false},
		// Suffix recursive glob
		{"suffix_recursive", "src/auth/handler.rs", "**/handler.rs", true},
		// Single-char wildcards
		{"single_segment_glob", "auth.rs", "*.rs", true},
		// Empty pattern shouldn't match anything
		{"empty_pattern", "src/auth.rs", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := locksCheckPathMatches(tc.path, tc.pattern)
			if got != tc.want {
				t.Fatalf("locksCheckPathMatches(%q, %q) = %v, want %v",
					tc.path, tc.pattern, got, tc.want)
			}
		})
	}
}

// Pin the JSON envelope's contract: the four wrapper-facing fields
// (state, holder, audit_token, observed_at) are present in the
// stable shape, and `holder == null` cleanly distinguishes the
// `free` case from `held`/`blocked`. Wrappers depend on this for
// `jq '.holder == null'`-style filtering, so a future refactor
// that drops `omitempty` would break their integration silently.
func TestLocksCheckResult_FreeStateOmitsHolder(t *testing.T) {
	t.Parallel()
	r := LocksCheckResult{
		Success:    true,
		Session:    "myproject",
		ProjectKey: "/data/projects/foo",
		Path:       "src/auth.rs",
		State:      "free",
		ObservedAt: "2026-05-12T12:00:00Z",
		AuditToken: "ntm:locks:check:foo:src/auth.rs:2026-05-12T12:00:00Z",
	}
	bytes, err := jsonMarshalIndent(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(bytes)
	if strings.Contains(out, "\"holder\"") {
		t.Fatalf(
			"free state must omit holder field for jq filtering; got:\n%s",
			out,
		)
	}
	if !strings.Contains(out, "\"state\": \"free\"") {
		t.Fatalf("expected `\"state\": \"free\"` in output:\n%s", out)
	}
	if !strings.Contains(out, "\"audit_token\"") {
		t.Fatalf("expected audit_token field in output:\n%s", out)
	}
}

func TestLocksCheckResult_HeldStatePopulatesHolder(t *testing.T) {
	t.Parallel()
	r := LocksCheckResult{
		Success:    true,
		Session:    "myproject",
		ProjectKey: "/data/projects/foo",
		Path:       "src/auth.rs",
		State:      "held",
		Holder: &LocksCheckHolder{
			Agent:         "agent-alpha",
			Reason:        "feature work",
			ExpiresAt:     "2026-05-12T13:00:00Z",
			Exclusive:     true,
			PathPattern:   "src/auth.rs",
			ReservationID: 42,
		},
		ObservedAt: "2026-05-12T12:00:00Z",
		AuditToken: "ntm:locks:check:foo:src/auth.rs:2026-05-12T12:00:00Z",
	}
	bytes, err := jsonMarshalIndent(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(bytes)
	if !strings.Contains(out, "agent-alpha") {
		t.Fatalf("held state must serialize holder.agent; got:\n%s", out)
	}
	if !strings.Contains(out, "\"reservation_id\": 42") {
		t.Fatalf("held state must serialize reservation_id; got:\n%s", out)
	}
}

// jsonMarshalIndent serializes the LocksCheckResult envelope the
// same way the CLI does (two-space indent). Used by the
// envelope-shape tests above.
func jsonMarshalIndent(v interface{}) ([]byte, error) {
	return jsonEncodeIndent(v)
}
