package summary

import (
	"context"
	"strings"
	"testing"
)

type stubSummarizer struct {
	text string
	fail bool
}

func (s stubSummarizer) Summarize(ctx context.Context, prompt string, maxTokens int) (string, error) {
	if s.fail {
		return "", context.Canceled
	}
	return s.text, nil
}

func TestSummarizeSessionMissingOutput(t *testing.T) {
	_, err := SummarizeSession(context.Background(), Options{})
	if err == nil {
		t.Fatalf("expected error for missing outputs")
	}
}

func TestSummarizeSessionBriefFallback(t *testing.T) {
	output := strings.Join([]string{
		"## Accomplishments",
		"- Implemented SummarizeSession",
		"- Added tests",
		"",
		"## Changes",
		"- Updated internal/summary/generator.go",
		"- Modified internal/cli/summary.go",
		"",
		"## Pending",
		"- Wire into CLI",
		"- Add docs",
		"",
		"## Errors",
		"- Failed: lint error in file",
		"",
		"## Decisions",
		"- Using regex parsing",
		"",
		"Created internal/summary/generator.go",
		"Modified internal/cli/summary.go",
	}, "\n")

	summary, err := SummarizeSession(context.Background(), Options{
		Session: "demo",
		Outputs: []AgentOutput{{AgentID: "a1", AgentType: "cc", Output: output}},
		Format:  FormatBrief,
	})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	if len(summary.Accomplishments) == 0 {
		t.Fatalf("expected accomplishments parsed")
	}
	if summary.Text == "" {
		t.Fatalf("expected summary text")
	}
	if !strings.Contains(summary.Text, "Accomplishments") {
		t.Fatalf("expected brief format to include accomplishments")
	}

	var created, modified bool
	for _, f := range summary.Files {
		if f.Path == "internal/summary/generator.go" && f.Action == FileActionCreated {
			created = true
		}
		if f.Path == "internal/cli/summary.go" && f.Action == FileActionModified {
			modified = true
		}
	}
	if !created || !modified {
		t.Fatalf("expected file changes extracted (created=%v modified=%v)", created, modified)
	}
}

func TestSummarizeSessionStructuredJSON(t *testing.T) {
	output := `{
  "summary": {
    "accomplishments": ["Implemented API"],
    "changes": ["Refactored router"],
    "pending": ["Add tests"],
    "errors": ["Error: foo"],
    "decisions": ["Use cobra"],
    "files": {
      "created": ["cmd/ntm/main.go"],
      "modified": ["internal/cli/root.go"],
      "deleted": ["old.txt"]
    }
  }
}`

	summary, err := SummarizeSession(context.Background(), Options{
		Session: "json",
		Outputs: []AgentOutput{{AgentID: "a1", AgentType: "cc", Output: output}},
		Format:  FormatDetailed,
	})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	if len(summary.Accomplishments) != 1 || summary.Accomplishments[0] != "Implemented API" {
		t.Fatalf("unexpected accomplishments: %+v", summary.Accomplishments)
	}
	if len(summary.Files) == 0 {
		t.Fatalf("expected file changes from json")
	}
	var hasCreated, hasModified, hasDeleted bool
	for _, f := range summary.Files {
		switch f.Path {
		case "cmd/ntm/main.go":
			hasCreated = f.Action == FileActionCreated
		case "internal/cli/root.go":
			hasModified = f.Action == FileActionModified
		case "old.txt":
			hasDeleted = f.Action == FileActionDeleted
		}
	}
	if !hasCreated || !hasModified || !hasDeleted {
		t.Fatalf("expected created/modified/deleted entries")
	}
}

func TestSummarizeSessionHandoffFormat(t *testing.T) {
	output := strings.Join([]string{
		"Completed: Implemented session summarizer",
		"Next: Add wiring",
	}, "\n")

	summary, err := SummarizeSession(context.Background(), Options{
		Session: "handoff",
		Outputs: []AgentOutput{{AgentID: "a1", AgentType: "cc", Output: output}},
		Format:  FormatHandoff,
	})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.Handoff == nil {
		t.Fatalf("expected handoff output")
	}
	if !strings.Contains(summary.Text, "goal:") || !strings.Contains(summary.Text, "now:") {
		t.Fatalf("expected yaml text to include goal/now")
	}
}

func TestSummarizeSessionUsesSummarizer(t *testing.T) {
	summary, err := SummarizeSession(context.Background(), Options{
		Session:    "llm",
		Outputs:    []AgentOutput{{AgentID: "a1", AgentType: "cc", Output: "done"}},
		Format:     FormatBrief,
		Summarizer: stubSummarizer{text: "LLM summary"},
	})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.Text != "LLM summary" {
		t.Fatalf("expected summarizer text, got %q", summary.Text)
	}
}

func TestSummarizeSessionTruncation(t *testing.T) {
	longText := strings.Repeat("a", 800)
	summary, err := SummarizeSession(context.Background(), Options{
		Session:    "truncate",
		Outputs:    []AgentOutput{{AgentID: "a1", AgentType: "cc", Output: "done"}},
		Format:     FormatBrief,
		MaxTokens:  10,
		Summarizer: stubSummarizer{text: longText},
	})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if !strings.Contains(summary.Text, "Summary truncated") {
		t.Fatalf("expected truncation note")
	}
	if summary.TokenEstimate > 10 {
		t.Fatalf("expected token estimate <= 10, got %d", summary.TokenEstimate)
	}
}
