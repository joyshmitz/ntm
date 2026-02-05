package cli

import (
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// ---------------------------------------------------------------------------
// filterByPrefix — 28.6% → 100%
// ---------------------------------------------------------------------------

func TestFilterByPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options []string
		prefix  string
		wantLen int
	}{
		{"empty prefix returns all", []string{"a", "b", "c"}, "", 3},
		{"matching prefix", []string{"foo", "foobar", "baz"}, "foo", 2},
		{"no matches", []string{"abc", "def"}, "xyz", 0},
		{"empty options", []string{}, "foo", 0},
		{"nil options", nil, "foo", 0},
		{"exact match", []string{"hello"}, "hello", 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := filterByPrefix(tc.options, tc.prefix)
			if len(got) != tc.wantLen {
				t.Errorf("filterByPrefix(%v, %q) returned %d items, want %d", tc.options, tc.prefix, len(got), tc.wantLen)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// coerceDepsResponse — 33.3% → 100%
// ---------------------------------------------------------------------------

func TestCoerceDepsResponse(t *testing.T) {
	t.Parallel()

	t.Run("direct value", func(t *testing.T) {
		t.Parallel()
		resp := output.DepsResponse{AllInstalled: true}
		got, err := coerceDepsResponse(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.AllInstalled {
			t.Error("expected AllInstalled=true")
		}
	})

	t.Run("pointer value", func(t *testing.T) {
		t.Parallel()
		resp := &output.DepsResponse{AllInstalled: true}
		got, err := coerceDepsResponse(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.AllInstalled {
			t.Error("expected AllInstalled=true")
		}
	})

	t.Run("nil pointer", func(t *testing.T) {
		t.Parallel()
		var resp *output.DepsResponse
		_, err := coerceDepsResponse(resp)
		if err == nil {
			t.Fatal("expected error for nil pointer")
		}
	})

	t.Run("unexpected type", func(t *testing.T) {
		t.Parallel()
		_, err := coerceDepsResponse("not a DepsResponse")
		if err == nil {
			t.Fatal("expected error for wrong type")
		}
	})
}

// ---------------------------------------------------------------------------
// agentTypeToString — 85.7% → 100%
// ---------------------------------------------------------------------------

func TestAgentTypeToString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   tmux.AgentType
		want string
	}{
		{"claude", tmux.AgentClaude, "claude"},
		{"codex", tmux.AgentCodex, "codex"},
		{"gemini", tmux.AgentGemini, "gemini"},
		{"empty string type", tmux.AgentType(""), "user"},
		{"custom type", tmux.AgentType("aider"), "aider"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := agentTypeToString(tc.in)
			if got != tc.want {
				t.Errorf("agentTypeToString(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
