package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/redaction"
)

func TestApplyOutputRedaction_NoFindings(t *testing.T) {
	out, summary, err := applyOutputRedaction("hello world", redaction.Config{Mode: redaction.ModeRedact})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if summary != nil {
		t.Fatalf("expected nil summary when no findings, got %+v", *summary)
	}
	if out != "hello world" {
		t.Fatalf("expected output to be unchanged, got %q", out)
	}
}

func TestApplyOutputRedaction_WarnMode(t *testing.T) {
	input := "prefix password=hunter2hunter2 suffix\n"
	out, summary, err := applyOutputRedaction(input, redaction.Config{Mode: redaction.ModeWarn})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if out != input {
		t.Fatalf("expected output to be unchanged in warn mode, got %q", out)
	}
	if summary == nil {
		t.Fatalf("expected non-nil summary")
	}
	if summary.Action != "warn" {
		t.Fatalf("expected action=warn, got %q", summary.Action)
	}
	if summary.Findings == 0 {
		t.Fatalf("expected findings > 0")
	}
	if got := summary.Categories["PASSWORD"]; got == 0 {
		t.Fatalf("expected PASSWORD category count > 0, got %d (%v)", got, summary.Categories)
	}
}

func TestApplyOutputRedaction_RedactMode(t *testing.T) {
	input := "prefix password=hunter2hunter2 suffix\n"
	out, summary, err := applyOutputRedaction(input, redaction.Config{Mode: redaction.ModeRedact})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if strings.Contains(out, "hunter2hunter2") {
		t.Fatalf("expected output to be redacted, got %q", out)
	}
	if !strings.Contains(out, "[REDACTED:PASSWORD:") {
		t.Fatalf("expected password placeholder, got %q", out)
	}
	if summary == nil {
		t.Fatalf("expected non-nil summary")
	}
	if summary.Action != "redact" {
		t.Fatalf("expected action=redact, got %q", summary.Action)
	}
}

func TestApplyOutputRedaction_BlockMode(t *testing.T) {
	input := "prefix password=hunter2hunter2 suffix\n"
	out, summary, err := applyOutputRedaction(input, redaction.Config{Mode: redaction.ModeBlock})
	if err == nil {
		t.Fatalf("expected error")
	}
	if out != "" {
		t.Fatalf("expected empty output on block, got %q", out)
	}
	if summary == nil {
		t.Fatalf("expected non-nil summary")
	}

	var blocked redactionBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected redactionBlockedError, got %T: %v", err, err)
	}
	if summary.Action != "block" {
		t.Fatalf("expected action=block, got %q", summary.Action)
	}
	if !strings.Contains(err.Error(), "PASSWORD") {
		t.Fatalf("expected error to mention category, got %q", err.Error())
	}
}
