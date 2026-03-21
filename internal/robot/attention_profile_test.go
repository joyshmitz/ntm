package robot

import (
	"testing"
)

// =============================================================================
// Profile Resolution Tests (br-9bmtl: Phase 4b1)
// Tests for profile lookup, listing, and effective filter resolution.
// =============================================================================

func TestGetProfile_OperatorProfile(t *testing.T) {
	t.Parallel()

	profile := GetProfile("operator")
	if profile == nil {
		t.Fatal("GetProfile(operator) returned nil")
	}
	if profile.Name != "operator" {
		t.Errorf("profile.Name = %q, want %q", profile.Name, "operator")
	}
	if profile.Filters.MinSeverity != SeverityInfo {
		t.Errorf("operator profile MinSeverity = %q, want %q", profile.Filters.MinSeverity, SeverityInfo)
	}
	if profile.Filters.MinActionability != ActionabilityInteresting {
		t.Errorf("operator profile MinActionability = %q, want %q", profile.Filters.MinActionability, ActionabilityInteresting)
	}
}

func TestGetProfile_DebugProfile(t *testing.T) {
	t.Parallel()

	profile := GetProfile("debug")
	if profile == nil {
		t.Fatal("GetProfile(debug) returned nil")
	}
	if profile.Name != "debug" {
		t.Errorf("profile.Name = %q, want %q", profile.Name, "debug")
	}
	if profile.Filters.MinSeverity != SeverityDebug {
		t.Errorf("debug profile MinSeverity = %q, want %q", profile.Filters.MinSeverity, SeverityDebug)
	}
	if profile.Filters.MinActionability != ActionabilityBackground {
		t.Errorf("debug profile MinActionability = %q, want %q", profile.Filters.MinActionability, ActionabilityBackground)
	}
}

func TestGetProfile_MinimalProfile(t *testing.T) {
	t.Parallel()

	profile := GetProfile("minimal")
	if profile == nil {
		t.Fatal("GetProfile(minimal) returned nil")
	}
	if profile.Name != "minimal" {
		t.Errorf("profile.Name = %q, want %q", profile.Name, "minimal")
	}
	if profile.Filters.MinSeverity != SeverityError {
		t.Errorf("minimal profile MinSeverity = %q, want %q", profile.Filters.MinSeverity, SeverityError)
	}
	if profile.Filters.MinActionability != ActionabilityActionRequired {
		t.Errorf("minimal profile MinActionability = %q, want %q", profile.Filters.MinActionability, ActionabilityActionRequired)
	}
}

func TestGetProfile_AlertsProfile(t *testing.T) {
	t.Parallel()

	profile := GetProfile("alerts")
	if profile == nil {
		t.Fatal("GetProfile(alerts) returned nil")
	}
	if profile.Name != "alerts" {
		t.Errorf("profile.Name = %q, want %q", profile.Name, "alerts")
	}
	if len(profile.Filters.Categories) == 0 {
		t.Error("alerts profile Categories should not be empty")
	}
	found := false
	for _, cat := range profile.Filters.Categories {
		if cat == EventCategoryAlert {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("alerts profile should include category %q", EventCategoryAlert)
	}
}

func TestGetProfile_UnknownProfile(t *testing.T) {
	t.Parallel()

	profile := GetProfile("nonexistent")
	if profile != nil {
		t.Errorf("GetProfile(nonexistent) = %+v, want nil", profile)
	}
}

func TestGetProfile_EmptyName(t *testing.T) {
	t.Parallel()

	profile := GetProfile("")
	if profile != nil {
		t.Errorf("GetProfile('') = %+v, want nil", profile)
	}
}

func TestGetProfile_CaseInsensitive(t *testing.T) {
	t.Parallel()

	tests := []string{"OPERATOR", "Operator", "OpErAtOr", "debug", "DEBUG", "Debug"}
	expectedNames := []string{"operator", "operator", "operator", "debug", "debug", "debug"}

	for i, input := range tests {
		profile := GetProfile(input)
		if profile == nil {
			t.Errorf("GetProfile(%q) returned nil, want profile", input)
			continue
		}
		// Profile name in struct is lowercase
		if profile.Name != expectedNames[i] {
			t.Errorf("GetProfile(%q).Name = %q, want %q", input, profile.Name, expectedNames[i])
		}
	}
}

func TestGetProfile_Whitespace(t *testing.T) {
	t.Parallel()

	// GetProfile trims whitespace
	profile := GetProfile("  operator  ")
	if profile == nil {
		t.Fatal("GetProfile with whitespace should find the profile")
	}
	if profile.Name != "operator" {
		t.Errorf("profile.Name = %q, want %q", profile.Name, "operator")
	}
}

func TestListProfiles_AllBuiltins(t *testing.T) {
	t.Parallel()

	profiles := ListProfiles()
	if len(profiles) != len(BuiltinProfiles) {
		t.Errorf("ListProfiles() returned %d profiles, want %d", len(profiles), len(BuiltinProfiles))
	}

	// Verify all expected profiles are present
	expectedNames := map[string]bool{
		"operator": false,
		"debug":    false,
		"minimal":  false,
		"alerts":   false,
	}

	for _, p := range profiles {
		if _, ok := expectedNames[p.Name]; ok {
			expectedNames[p.Name] = true
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("ListProfiles() missing profile %q", name)
		}
	}
}

func TestListProfiles_IsolatedFromBuiltins(t *testing.T) {
	t.Parallel()

	profiles := ListProfiles()
	if len(profiles) == 0 {
		t.Skip("no profiles to test isolation")
	}

	// Modify returned profile
	profiles[0].Name = "modified"
	profiles[0].Description = "modified description"

	// Original builtin should be unchanged
	refetch := ListProfiles()
	if refetch[0].Name == "modified" {
		t.Error("ListProfiles leaked mutable reference to BuiltinProfiles")
	}
}

// =============================================================================
// ResolveEffectiveFilters Tests
// =============================================================================

func TestResolveEffectiveFilters_NoProfileNoExplicit(t *testing.T) {
	t.Parallel()

	filters := ResolveEffectiveFilters("", ProfileFilters{})

	if filters.SourceProfile != "" {
		t.Errorf("SourceProfile = %q, want empty", filters.SourceProfile)
	}
	// Defaults should be applied
	if filters.MinSeverity != SeverityInfo {
		t.Errorf("MinSeverity = %q, want %q", filters.MinSeverity, SeverityInfo)
	}
	if filters.MinActionability != ActionabilityBackground {
		t.Errorf("MinActionability = %q, want %q", filters.MinActionability, ActionabilityBackground)
	}
	if len(filters.ExplicitOverrides) != 0 {
		t.Errorf("ExplicitOverrides = %v, want empty", filters.ExplicitOverrides)
	}
}

func TestResolveEffectiveFilters_ProfileOnly(t *testing.T) {
	t.Parallel()

	filters := ResolveEffectiveFilters("minimal", ProfileFilters{})

	if filters.SourceProfile != "minimal" {
		t.Errorf("SourceProfile = %q, want %q", filters.SourceProfile, "minimal")
	}
	// Should have minimal profile settings
	if filters.MinSeverity != SeverityError {
		t.Errorf("MinSeverity = %q, want %q (from minimal)", filters.MinSeverity, SeverityError)
	}
	if filters.MinActionability != ActionabilityActionRequired {
		t.Errorf("MinActionability = %q, want %q (from minimal)", filters.MinActionability, ActionabilityActionRequired)
	}
	if len(filters.ExplicitOverrides) != 0 {
		t.Errorf("ExplicitOverrides = %v, want empty (no explicit overrides)", filters.ExplicitOverrides)
	}
}

func TestResolveEffectiveFilters_ExplicitOnlyNoProfile(t *testing.T) {
	t.Parallel()

	explicit := ProfileFilters{
		MinSeverity:      SeverityWarning,
		MinActionability: ActionabilityInteresting,
		Categories:       []EventCategory{EventCategorySystem},
	}
	filters := ResolveEffectiveFilters("", explicit)

	if filters.SourceProfile != "" {
		t.Errorf("SourceProfile = %q, want empty", filters.SourceProfile)
	}
	if filters.MinSeverity != SeverityWarning {
		t.Errorf("MinSeverity = %q, want %q", filters.MinSeverity, SeverityWarning)
	}
	if filters.MinActionability != ActionabilityInteresting {
		t.Errorf("MinActionability = %q, want %q", filters.MinActionability, ActionabilityInteresting)
	}
	if len(filters.Categories) != 1 || filters.Categories[0] != EventCategorySystem {
		t.Errorf("Categories = %v, want [%v]", filters.Categories, EventCategorySystem)
	}
	// Should have explicit overrides listed
	if len(filters.ExplicitOverrides) != 3 {
		t.Errorf("ExplicitOverrides = %v, want 3 fields", filters.ExplicitOverrides)
	}
}

func TestResolveEffectiveFilters_UnknownProfile(t *testing.T) {
	t.Parallel()

	filters := ResolveEffectiveFilters("nonexistent", ProfileFilters{})

	// Unknown profile should be ignored, defaults applied
	if filters.SourceProfile != "" {
		t.Errorf("SourceProfile = %q, want empty (unknown profile ignored)", filters.SourceProfile)
	}
	if filters.MinSeverity != SeverityInfo {
		t.Errorf("MinSeverity = %q, want %q (default)", filters.MinSeverity, SeverityInfo)
	}
}

func TestResolveEffectiveFilters_ExcludeTypes(t *testing.T) {
	t.Parallel()

	explicit := ProfileFilters{
		ExcludeTypes: []EventType{EventTypePaneOutput, EventTypeSpawn},
	}
	filters := ResolveEffectiveFilters("", explicit)

	if len(filters.ExcludeTypes) != 2 {
		t.Errorf("ExcludeTypes = %v, want 2 items", filters.ExcludeTypes)
	}

	hasOverride := false
	for _, o := range filters.ExplicitOverrides {
		if o == "exclude_types" {
			hasOverride = true
			break
		}
	}
	if !hasOverride {
		t.Errorf("ExplicitOverrides = %v, want to include 'exclude_types'", filters.ExplicitOverrides)
	}
}

// =============================================================================
// MatchesFilters Tests
// =============================================================================

func TestMatchesFilters_CategoryFilter_Matches(t *testing.T) {
	t.Parallel()

	filters := ResolvedFilters{
		Categories:       []EventCategory{EventCategorySystem, EventCategoryAlert},
		MinSeverity:      SeverityInfo,
		MinActionability: ActionabilityBackground,
	}

	event := &AttentionEvent{
		Category:      EventCategorySystem,
		Severity:      SeverityInfo,
		Actionability: ActionabilityInteresting,
	}

	if !filters.MatchesFilters(event) {
		t.Error("MatchesFilters should return true for matching category")
	}
}

func TestMatchesFilters_CategoryFilter_NoMatch(t *testing.T) {
	t.Parallel()

	filters := ResolvedFilters{
		Categories:       []EventCategory{EventCategorySystem},
		MinSeverity:      SeverityInfo,
		MinActionability: ActionabilityBackground,
	}

	event := &AttentionEvent{
		Category:      EventCategoryMail,
		Severity:      SeverityInfo,
		Actionability: ActionabilityInteresting,
	}

	if filters.MatchesFilters(event) {
		t.Error("MatchesFilters should return false for non-matching category")
	}
}

func TestMatchesFilters_CategoryFilter_Empty(t *testing.T) {
	t.Parallel()

	// Empty categories means all pass
	filters := ResolvedFilters{
		Categories:       []EventCategory{},
		MinSeverity:      SeverityInfo,
		MinActionability: ActionabilityBackground,
	}

	event := &AttentionEvent{
		Category:      EventCategoryMail,
		Severity:      SeverityInfo,
		Actionability: ActionabilityInteresting,
	}

	if !filters.MatchesFilters(event) {
		t.Error("MatchesFilters should return true when Categories is empty (all allowed)")
	}
}

func TestMatchesFilters_SeverityFilter_Meets(t *testing.T) {
	t.Parallel()

	filters := ResolvedFilters{
		MinSeverity:      SeverityWarning,
		MinActionability: ActionabilityBackground,
	}

	tests := []struct {
		severity Severity
		want     bool
	}{
		{SeverityCritical, true},
		{SeverityError, true},
		{SeverityWarning, true},
		{SeverityInfo, false},
		{SeverityDebug, false},
	}

	for _, tc := range tests {
		event := &AttentionEvent{
			Category:      EventCategorySystem,
			Severity:      tc.severity,
			Actionability: ActionabilityInteresting,
		}
		got := filters.MatchesFilters(event)
		if got != tc.want {
			t.Errorf("MatchesFilters(severity=%q) = %v, want %v", tc.severity, got, tc.want)
		}
	}
}

func TestMatchesFilters_ActionabilityFilter_Meets(t *testing.T) {
	t.Parallel()

	filters := ResolvedFilters{
		MinSeverity:      SeverityInfo,
		MinActionability: ActionabilityInteresting,
	}

	tests := []struct {
		actionability Actionability
		want          bool
	}{
		{ActionabilityActionRequired, true},
		{ActionabilityInteresting, true},
		{ActionabilityBackground, false},
	}

	for _, tc := range tests {
		event := &AttentionEvent{
			Category:      EventCategorySystem,
			Severity:      SeverityInfo,
			Actionability: tc.actionability,
		}
		got := filters.MatchesFilters(event)
		if got != tc.want {
			t.Errorf("MatchesFilters(actionability=%q) = %v, want %v", tc.actionability, got, tc.want)
		}
	}
}

func TestMatchesFilters_ExcludeTypes(t *testing.T) {
	t.Parallel()

	filters := ResolvedFilters{
		MinSeverity:      SeverityInfo,
		MinActionability: ActionabilityBackground,
		ExcludeTypes:     []EventType{EventTypePaneOutput, EventTypeSpawn},
	}

	tests := []struct {
		eventType EventType
		want      bool
	}{
		{EventTypePaneOutput, false},
		{EventTypeSpawn, false},
		{EventTypeAlert, true},
		{EventTypeHealthChange, true},
	}

	for _, tc := range tests {
		event := &AttentionEvent{
			Category:      EventCategorySystem,
			Type:          tc.eventType,
			Severity:      SeverityInfo,
			Actionability: ActionabilityInteresting,
		}
		got := filters.MatchesFilters(event)
		if got != tc.want {
			t.Errorf("MatchesFilters(type=%q) = %v, want %v", tc.eventType, got, tc.want)
		}
	}
}

func TestMatchesFilters_CombinedFilters(t *testing.T) {
	t.Parallel()

	// Minimal profile-like filters
	filters := ResolvedFilters{
		Categories:       []EventCategory{EventCategoryAlert},
		MinSeverity:      SeverityError,
		MinActionability: ActionabilityActionRequired,
		ExcludeTypes:     []EventType{EventTypePaneOutput},
	}

	tests := []struct {
		name   string
		event  AttentionEvent
		expect bool
	}{
		{
			name: "all_pass",
			event: AttentionEvent{
				Category:      EventCategoryAlert,
				Type:          EventTypeAlert,
				Severity:      SeverityCritical,
				Actionability: ActionabilityActionRequired,
			},
			expect: true,
		},
		{
			name: "wrong_category",
			event: AttentionEvent{
				Category:      EventCategorySystem,
				Type:          EventTypeAlert,
				Severity:      SeverityCritical,
				Actionability: ActionabilityActionRequired,
			},
			expect: false,
		},
		{
			name: "severity_too_low",
			event: AttentionEvent{
				Category:      EventCategoryAlert,
				Type:          EventTypeAlert,
				Severity:      SeverityWarning,
				Actionability: ActionabilityActionRequired,
			},
			expect: false,
		},
		{
			name: "actionability_too_low",
			event: AttentionEvent{
				Category:      EventCategoryAlert,
				Type:          EventTypeAlert,
				Severity:      SeverityCritical,
				Actionability: ActionabilityInteresting,
			},
			expect: false,
		},
		{
			name: "excluded_type",
			event: AttentionEvent{
				Category:      EventCategoryAlert,
				Type:          EventTypePaneOutput,
				Severity:      SeverityCritical,
				Actionability: ActionabilityActionRequired,
			},
			expect: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filters.MatchesFilters(&tc.event)
			if got != tc.expect {
				t.Errorf("MatchesFilters = %v, want %v", got, tc.expect)
			}
		})
	}
}

// =============================================================================
// Profile-to-Filter Integration Tests
// =============================================================================

func TestOperatorProfile_FiltersNoiseCorrectly(t *testing.T) {
	t.Parallel()

	filters := ResolveEffectiveFilters("operator", ProfileFilters{})

	// Background noise should be filtered
	bgEvent := &AttentionEvent{
		Category:      EventCategorySystem,
		Type:          EventTypePaneOutput,
		Severity:      SeverityInfo,
		Actionability: ActionabilityBackground,
	}
	if filters.MatchesFilters(bgEvent) {
		t.Error("operator profile should filter background actionability")
	}

	// Interesting events should pass
	interestingEvent := &AttentionEvent{
		Category:      EventCategorySystem,
		Type:          EventTypeHealthChange,
		Severity:      SeverityInfo,
		Actionability: ActionabilityInteresting,
	}
	if !filters.MatchesFilters(interestingEvent) {
		t.Error("operator profile should pass interesting events")
	}
}

func TestDebugProfile_AllowsEverything(t *testing.T) {
	t.Parallel()

	filters := ResolveEffectiveFilters("debug", ProfileFilters{})

	events := []AttentionEvent{
		{Category: EventCategorySystem, Severity: SeverityDebug, Actionability: ActionabilityBackground},
		{Category: EventCategoryMail, Severity: SeverityInfo, Actionability: ActionabilityInteresting},
		{Category: EventCategoryAlert, Severity: SeverityCritical, Actionability: ActionabilityActionRequired},
	}

	for i, ev := range events {
		if !filters.MatchesFilters(&ev) {
			t.Errorf("debug profile should allow event %d: %+v", i, ev)
		}
	}
}

func TestMinimalProfile_OnlyCriticalActions(t *testing.T) {
	t.Parallel()

	filters := ResolveEffectiveFilters("minimal", ProfileFilters{})

	// Should filter most events
	shouldFilter := []AttentionEvent{
		{Category: EventCategorySystem, Severity: SeverityInfo, Actionability: ActionabilityInteresting},
		{Category: EventCategorySystem, Severity: SeverityWarning, Actionability: ActionabilityActionRequired},
		{Category: EventCategorySystem, Severity: SeverityError, Actionability: ActionabilityInteresting},
	}
	for i, ev := range shouldFilter {
		if filters.MatchesFilters(&ev) {
			t.Errorf("minimal profile should filter event %d: %+v", i, ev)
		}
	}

	// Should pass critical action-required
	shouldPass := AttentionEvent{
		Category:      EventCategoryAlert,
		Severity:      SeverityCritical,
		Actionability: ActionabilityActionRequired,
	}
	if !filters.MatchesFilters(&shouldPass) {
		t.Error("minimal profile should pass critical action-required events")
	}
}

func TestAlertsProfile_OnlyAlertCategory(t *testing.T) {
	t.Parallel()

	filters := ResolveEffectiveFilters("alerts", ProfileFilters{})

	// Should filter non-alert categories
	nonAlert := &AttentionEvent{
		Category:      EventCategorySystem,
		Severity:      SeverityCritical,
		Actionability: ActionabilityActionRequired,
	}
	if filters.MatchesFilters(nonAlert) {
		t.Error("alerts profile should filter non-alert categories")
	}

	// Should pass alert category
	alert := &AttentionEvent{
		Category:      EventCategoryAlert,
		Severity:      SeverityInfo,
		Actionability: ActionabilityBackground,
	}
	if !filters.MatchesFilters(alert) {
		t.Error("alerts profile should pass alert category")
	}
}
