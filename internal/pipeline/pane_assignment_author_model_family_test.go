package pipeline

import "testing"

func TestForeachAuthorModelFamilyPrefersCanonicalKeys(t *testing.T) {
	item := map[string]interface{}{
		"author_model": "claude-sonnet-4",
		"model_family": "cc",
	}
	if got := foreachAuthorModelFamily(item); got != "cc" {
		t.Fatalf("foreachAuthorModelFamily() = %q, want cc", got)
	}
}

func TestForeachAuthorModelFamilyFallsBackToAuthorModel(t *testing.T) {
	item := map[string]interface{}{"author_model": "cod"}
	if got := foreachAuthorModelFamily(item); got != "cod" {
		t.Fatalf("foreachAuthorModelFamily() = %q, want cod", got)
	}
}

func TestForeachAuthorModelFamilyNormalizesVerboseAuthorModel(t *testing.T) {
	item := map[string]interface{}{"author_model": "claude-sonnet-4"}
	if got := foreachAuthorModelFamily(item); got != "cc" {
		t.Fatalf("foreachAuthorModelFamily() = %q, want cc", got)
	}
}

func TestForeachAuthorModelFamilySkipsBlankAliases(t *testing.T) {
	item := map[string]interface{}{
		"model_family": "   ",
		"family":       "",
		"type":         "gmi",
	}
	if got := foreachAuthorModelFamily(item); got != "gmi" {
		t.Fatalf("foreachAuthorModelFamily() = %q, want gmi", got)
	}
}

func TestSelectForeachPaneModelFamilyDifferencePrefersCanonicalOverVerboseAuthor(t *testing.T) {
	strategyPanes := []paneStrategyPane{
		{ID: "p1", ModelFamily: "cc"},
		{ID: "p2", ModelFamily: "cod"},
	}
	item := map[string]interface{}{
		"author_model": "claude-sonnet-4",
		"model_family": "cc",
	}

	got, _, _, err := selectForeachPane("by_model_family_difference", strategyPanes, nil, item, 0)
	if err != nil {
		t.Fatalf("selectForeachPane() error = %v", err)
	}
	if got != "p2" {
		t.Fatalf("selectForeachPane() = %q, want p2", got)
	}
}

func TestForeachAuthorModelFamilyForPanesPrefersPaneVocabulary(t *testing.T) {
	strategyPanes := []paneStrategyPane{
		{ID: "p1", ModelFamily: "codex"},
		{ID: "p2", ModelFamily: "claude"},
	}
	item := map[string]interface{}{"author_model": "openai-codex"}

	if got := foreachAuthorModelFamilyForPanes(item, strategyPanes); got != "codex" {
		t.Fatalf("foreachAuthorModelFamilyForPanes() = %q, want codex", got)
	}
}
