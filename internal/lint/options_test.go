package lint

import (
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/redaction"
)

// =============================================================================
// WithRuleSet — 0% → 100%
// =============================================================================

func TestWithRuleSet(t *testing.T) {
	t.Parallel()
	rs := &RuleSet{}
	l := New(WithRuleSet(rs))
	if l.rules != rs {
		t.Error("WithRuleSet did not set rules")
	}
}

// =============================================================================
// WithRedactionConfig — 0% → 100%
// =============================================================================

func TestWithRedactionConfig(t *testing.T) {
	t.Parallel()
	cfg := &redaction.Config{}
	l := New(WithRedactionConfig(cfg))
	if l.redactor != cfg {
		t.Error("WithRedactionConfig did not set redactor")
	}
}
