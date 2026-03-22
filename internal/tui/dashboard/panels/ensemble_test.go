package panels

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

func TestMinInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b int
		want int
	}{
		{"a smaller", 1, 5, 1},
		{"b smaller", 10, 3, 3},
		{"equal", 7, 7, 7},
		{"negative a", -5, 3, -5},
		{"negative b", 3, -5, -5},
		{"both negative", -3, -7, -7},
		{"zero and positive", 0, 5, 0},
		{"zero and negative", 0, -5, -5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := min(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("min(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestMaxInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b int
		want int
	}{
		{"a larger", 10, 5, 10},
		{"b larger", 3, 8, 8},
		{"equal", 7, 7, 7},
		{"negative a", -5, 3, 3},
		{"negative b", 3, -5, 3},
		{"both negative", -3, -7, -3},
		{"zero and positive", 0, 5, 5},
		{"zero and negative", 0, -5, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := max(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("max(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestShortenPaneName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with double underscore", "session__cc_1_opus", "cc_1_opus"},
		{"multiple double underscores", "long__path__final_part", "final_part"},
		{"no double underscore", "simple_name", "simple_name"},
		{"empty string", "", ""},
		{"double underscore at end", "name__", "name__"},
		{"just double underscore", "__", "__"},
		{"double underscore with one char", "__x", "x"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shortenPaneName(tc.input)
			if got != tc.want {
				t.Errorf("shortenPaneName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestAssignmentStatusIcon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status ensemble.AssignmentStatus
		want   string
	}{
		{"active", ensemble.AssignmentActive, "●"},
		{"injecting", ensemble.AssignmentInjecting, "◐"},
		{"pending", ensemble.AssignmentPending, "○"},
		{"done", ensemble.AssignmentDone, "✓"},
		{"error", ensemble.AssignmentError, "✗"},
		{"unknown", ensemble.AssignmentStatus("unknown"), "•"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := assignmentStatusIcon(tc.status)
			if got != tc.want {
				t.Errorf("assignmentStatusIcon(%q) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

func TestAssignmentProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status ensemble.AssignmentStatus
		want   float64
	}{
		{"pending", ensemble.AssignmentPending, 0.05},
		{"injecting", ensemble.AssignmentInjecting, 0.25},
		{"active", ensemble.AssignmentActive, 0.6},
		{"done", ensemble.AssignmentDone, 1.0},
		{"error", ensemble.AssignmentError, 1.0},
		{"unknown", ensemble.AssignmentStatus("unknown"), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := assignmentProgress(tc.status)
			if got != tc.want {
				t.Errorf("assignmentProgress(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestSummarizeAssignmentStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		assignments          []ensemble.ModeAssignment
		wantActive, wantDone int
		wantPending          int
	}{
		{
			name:        "empty",
			assignments: []ensemble.ModeAssignment{},
			wantActive:  0, wantDone: 0, wantPending: 0,
		},
		{
			name: "all active",
			assignments: []ensemble.ModeAssignment{
				{Status: ensemble.AssignmentActive},
				{Status: ensemble.AssignmentActive},
			},
			wantActive: 2, wantDone: 0, wantPending: 0,
		},
		{
			name: "mixed statuses",
			assignments: []ensemble.ModeAssignment{
				{Status: ensemble.AssignmentActive},
				{Status: ensemble.AssignmentInjecting},
				{Status: ensemble.AssignmentDone},
				{Status: ensemble.AssignmentPending},
				{Status: ensemble.AssignmentError},
			},
			wantActive: 2, wantDone: 1, wantPending: 1,
		},
		{
			name: "all done",
			assignments: []ensemble.ModeAssignment{
				{Status: ensemble.AssignmentDone},
				{Status: ensemble.AssignmentDone},
				{Status: ensemble.AssignmentDone},
			},
			wantActive: 0, wantDone: 3, wantPending: 0,
		},
		{
			name: "all pending",
			assignments: []ensemble.ModeAssignment{
				{Status: ensemble.AssignmentPending},
				{Status: ensemble.AssignmentPending},
			},
			wantActive: 0, wantDone: 0, wantPending: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			active, done, pending := summarizeAssignmentStatus(tc.assignments)
			if active != tc.wantActive {
				t.Errorf("active = %d, want %d", active, tc.wantActive)
			}
			if done != tc.wantDone {
				t.Errorf("done = %d, want %d", done, tc.wantDone)
			}
			if pending != tc.wantPending {
				t.Errorf("pending = %d, want %d", pending, tc.wantPending)
			}
		})
	}
}

func TestEnsemblePanel_FormatTimeAgo(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		time time.Time
		want string
	}{
		{"just now", fixedNow.Add(-30 * time.Second), "now"},
		{"5 minutes ago", fixedNow.Add(-5 * time.Minute), "5m"},
		{"59 minutes ago", fixedNow.Add(-59 * time.Minute), "59m"},
		{"1 hour ago", fixedNow.Add(-1 * time.Hour), "1h"},
		{"3 hours ago", fixedNow.Add(-3 * time.Hour), "3h"},
		{"23 hours ago", fixedNow.Add(-23 * time.Hour), "23h"},
		{"1 day ago", fixedNow.Add(-25 * time.Hour), "1d"},
		{"5 days ago", fixedNow.Add(-5 * 24 * time.Hour), "5d"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &EnsemblePanel{
				now: func() time.Time { return fixedNow },
			}
			got := p.formatTimeAgo(tc.time)
			if got != tc.want {
				t.Errorf("formatTimeAgo(%v) = %q, want %q", tc.time, got, tc.want)
			}
		})
	}
}

func TestEnsemblePanel_FormatTimeAgo_NilNowFunc(t *testing.T) {
	t.Parallel()

	p := &EnsemblePanel{
		now: nil,
	}

	// With nil now func, it should use time.Now
	// Just verify it doesn't panic and returns something
	result := p.formatTimeAgo(time.Now().Add(-5 * time.Minute))
	if result == "" {
		t.Error("formatTimeAgo with nil now func returned empty string")
	}
}

func TestEnsembleConfig(t *testing.T) {
	t.Parallel()

	cfg := ensembleConfig()

	if cfg.ID != "ensemble" {
		t.Errorf("ID = %q, want %q", cfg.ID, "ensemble")
	}
	if cfg.Title != "Reasoning Ensemble" {
		t.Errorf("Title = %q, want %q", cfg.Title, "Reasoning Ensemble")
	}
	if cfg.Priority != PriorityHigh {
		t.Errorf("Priority = %v, want %v", cfg.Priority, PriorityHigh)
	}
	if cfg.RefreshInterval != 5*time.Second {
		t.Errorf("RefreshInterval = %v, want %v", cfg.RefreshInterval, 5*time.Second)
	}
	if cfg.MinWidth != 30 {
		t.Errorf("MinWidth = %d, want %d", cfg.MinWidth, 30)
	}
	if cfg.MinHeight != 8 {
		t.Errorf("MinHeight = %d, want %d", cfg.MinHeight, 8)
	}
	if !cfg.Collapsible {
		t.Error("Collapsible should be true")
	}
}

func TestNewEnsemblePanel(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()

	if p == nil {
		t.Fatal("NewEnsemblePanel returned nil")
	}
	if p.session != nil {
		t.Error("new panel should have nil session")
	}
	if p.catalog != nil {
		t.Error("new panel should have nil catalog")
	}
	if p.err != nil {
		t.Error("new panel should have nil error")
	}
	if p.now == nil {
		t.Error("new panel should have non-nil now function")
	}

	// Verify config was set
	cfg := p.Config()
	if cfg.ID != "ensemble" {
		t.Errorf("Config ID = %q, want %q", cfg.ID, "ensemble")
	}
}

// --- New tests below ---

func TestEnsemblePanel_SetSession(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()

	sess := &ensemble.EnsembleSession{
		SessionName: "test-session",
		Status:      ensemble.EnsembleActive,
	}

	p.SetSession(sess, nil)

	if p.session != sess {
		t.Error("session not set")
	}
	if p.err != nil {
		t.Error("err should be nil after successful SetSession")
	}
	if p.LastUpdate().IsZero() {
		t.Error("LastUpdate should be set on nil error")
	}
}

func TestEnsemblePanel_SetSession_WithError(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	testErr := errors.New("connection failed")

	p.SetSession(nil, testErr)

	if p.err != testErr {
		t.Error("err not set")
	}
}

func TestEnsemblePanel_SetCatalog(t *testing.T) {
	t.Parallel()

	modes := []ensemble.ReasoningMode{
		{ID: "deductive", Code: "A1", Name: "Deductive Logic", Category: ensemble.CategoryFormal, Tier: ensemble.TierCore, ShortDesc: "Formal deductive logic"},
	}
	catalog, err := ensemble.NewModeCatalog(modes, "test")
	if err != nil {
		t.Fatalf("NewModeCatalog: %v", err)
	}

	p := NewEnsemblePanel()
	p.SetCatalog(catalog)

	if p.catalog != catalog {
		t.Error("catalog not set")
	}
}

func TestEnsemblePanel_Init(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	cmd := p.Init()
	if cmd != nil {
		t.Error("Init should return nil cmd")
	}
}

func TestEnsemblePanel_Update_EnsembleStatusMsg(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	sess := &ensemble.EnsembleSession{
		SessionName: "test",
		Status:      ensemble.EnsembleActive,
	}

	model, cmd := p.Update(EnsembleStatusMsg{Session: sess, Err: nil})
	if cmd != nil {
		t.Error("expected nil cmd")
	}

	updated := model.(*EnsemblePanel)
	if updated.session != sess {
		t.Error("session not updated via EnsembleStatusMsg")
	}
}

func TestEnsemblePanel_Update_EnsembleStatusMsg_WithError(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	testErr := errors.New("bad state")

	model, _ := p.Update(EnsembleStatusMsg{Session: nil, Err: testErr})
	updated := model.(*EnsemblePanel)
	if updated.err != testErr {
		t.Error("error not propagated via EnsembleStatusMsg")
	}
}

func TestEnsemblePanel_Update_KeyMsg_NotFocused(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	// Not focused by default

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd != nil {
		t.Error("expected nil cmd when panel is not focused")
	}
}

func TestEnsemblePanel_Update_KeyMsg_Synthesize(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.Focus()

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for 's' key")
	}
	msg := cmd()
	actionMsg, ok := msg.(EnsembleActionMsg)
	if !ok {
		t.Fatalf("expected EnsembleActionMsg, got %T", msg)
	}
	if actionMsg.Action != EnsembleActionSynthesize {
		t.Errorf("action = %q, want %q", actionMsg.Action, EnsembleActionSynthesize)
	}
}

func TestEnsemblePanel_Update_KeyMsg_ViewOutputs(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.Focus()

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for 'v' key")
	}
	msg := cmd()
	actionMsg, ok := msg.(EnsembleActionMsg)
	if !ok {
		t.Fatalf("expected EnsembleActionMsg, got %T", msg)
	}
	if actionMsg.Action != EnsembleActionViewOutputs {
		t.Errorf("action = %q, want %q", actionMsg.Action, EnsembleActionViewOutputs)
	}
}

func TestEnsemblePanel_Update_KeyMsg_Refresh(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.Focus()

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for 'r' key")
	}
	msg := cmd()
	actionMsg, ok := msg.(EnsembleActionMsg)
	if !ok {
		t.Fatalf("expected EnsembleActionMsg, got %T", msg)
	}
	if actionMsg.Action != EnsembleActionRefresh {
		t.Errorf("action = %q, want %q", actionMsg.Action, EnsembleActionRefresh)
	}
}

func TestEnsemblePanel_Update_UnhandledKey(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.Focus()

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Error("expected nil cmd for unhandled key")
	}
}

func TestEnsemblePanel_Keybindings(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	bindings := p.Keybindings()

	if len(bindings) != 3 {
		t.Fatalf("Keybindings returned %d, want 3", len(bindings))
	}

	expected := []struct {
		action string
		desc   string
	}{
		{"synthesize", "Trigger synthesis"},
		{"view_outputs", "View ensemble outputs"},
		{"refresh", "Refresh ensemble status"},
	}

	for i, kb := range bindings {
		if kb.Action != expected[i].action {
			t.Errorf("keybinding[%d].Action = %q, want %q", i, kb.Action, expected[i].action)
		}
		if kb.Description != expected[i].desc {
			t.Errorf("keybinding[%d].Description = %q, want %q", i, kb.Description, expected[i].desc)
		}
	}
}

func TestEnsemblePanel_View_ZeroWidth(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.SetSize(0, 20)

	view := p.View()
	if view != "" {
		t.Errorf("expected empty view for zero width, got %q", view)
	}
}

func TestEnsemblePanel_View_NoSession(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.SetSize(60, 20)

	view := p.View()
	if !strings.Contains(view, "No ensemble running") {
		t.Errorf("expected 'No ensemble running' message, got %q", view)
	}
}

func TestEnsemblePanel_View_WithError(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.SetSize(60, 20)
	p.SetSession(nil, errors.New("connection failed"))

	view := p.View()
	if !strings.Contains(view, "Error") {
		t.Errorf("expected Error badge in view, got %q", view)
	}
	if !strings.Contains(view, "connection failed") {
		t.Errorf("expected error message in view, got %q", view)
	}
}

func TestEnsemblePanel_View_WithSession_NoAssignments(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.SetSize(60, 20)
	p.now = func() time.Time { return time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC) }

	sess := &ensemble.EnsembleSession{
		Question:    "How does X work?",
		PresetUsed:  "deep-dive",
		Status:      ensemble.EnsembleActive,
		Assignments: []ensemble.ModeAssignment{},
		CreatedAt:   time.Date(2026, 1, 30, 11, 55, 0, 0, time.UTC),
	}
	p.SetSession(sess, nil)

	view := p.View()
	if !strings.Contains(view, "How does X work?") {
		t.Errorf("expected question in view, got %q", view)
	}
	if !strings.Contains(view, "deep-dive") {
		t.Errorf("expected preset in view, got %q", view)
	}
	if !strings.Contains(view, "No assignments yet") {
		t.Errorf("expected empty assignments message, got %q", view)
	}
}

func TestEnsemblePanel_View_WithAssignments(t *testing.T) {
	t.Parallel()

	modes := []ensemble.ReasoningMode{
		{ID: "deductive", Code: "A1", Name: "Deductive Logic", Category: ensemble.CategoryFormal, Tier: ensemble.TierCore, ShortDesc: "Formal deductive logic"},
		{ID: "bayesian", Code: "C2", Name: "Bayesian Analysis", Category: ensemble.CategoryUncertainty, Tier: ensemble.TierAdvanced, ShortDesc: "Probabilistic reasoning"},
	}
	catalog, err := ensemble.NewModeCatalog(modes, "test")
	if err != nil {
		t.Fatalf("NewModeCatalog: %v", err)
	}

	p := NewEnsemblePanel()
	p.SetSize(80, 30)
	p.now = func() time.Time { return time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC) }
	p.SetCatalog(catalog)

	sess := &ensemble.EnsembleSession{
		Question:   "Test question",
		PresetUsed: "quick",
		Status:     ensemble.EnsembleActive,
		Assignments: []ensemble.ModeAssignment{
			{ModeID: "deductive", PaneName: "proj__cc_1", Status: ensemble.AssignmentActive},
			{ModeID: "bayesian", PaneName: "proj__cod_1", Status: ensemble.AssignmentDone},
		},
		CreatedAt: time.Date(2026, 1, 30, 11, 50, 0, 0, time.UTC),
	}
	p.SetSession(sess, nil)

	view := p.View()
	if !strings.Contains(view, "CORE") {
		t.Errorf("expected CORE tier badge, got %q", view)
	}
	if !strings.Contains(view, "ADV") {
		t.Errorf("expected ADV tier badge, got %q", view)
	}
	if !strings.Contains(view, "active") {
		t.Errorf("expected 'active' status text, got %q", view)
	}
}

func TestEnsemblePanel_View_AdvancedWarning(t *testing.T) {
	t.Parallel()

	modes := []ensemble.ReasoningMode{
		{ID: "bayesian", Code: "C2", Name: "Bayesian", Category: ensemble.CategoryUncertainty, Tier: ensemble.TierAdvanced, ShortDesc: "Probabilistic reasoning"},
		{ID: "creative", Code: "H3", Name: "Creative", Category: ensemble.CategoryStrategic, Tier: ensemble.TierExperimental, ShortDesc: "Creative strategic reasoning"},
	}
	catalog, err := ensemble.NewModeCatalog(modes, "test")
	if err != nil {
		t.Fatalf("NewModeCatalog: %v", err)
	}

	p := NewEnsemblePanel()
	p.SetSize(80, 30)
	p.now = func() time.Time { return time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC) }
	p.SetCatalog(catalog)

	sess := &ensemble.EnsembleSession{
		Question: "Test",
		Status:   ensemble.EnsembleActive,
		Assignments: []ensemble.ModeAssignment{
			{ModeID: "bayesian", PaneName: "p__cc_1", Status: ensemble.AssignmentActive},
			{ModeID: "creative", PaneName: "p__cod_1", Status: ensemble.AssignmentActive},
		},
		CreatedAt: time.Date(2026, 1, 30, 11, 50, 0, 0, time.UTC),
	}
	p.SetSession(sess, nil)

	view := p.View()
	if !strings.Contains(view, "Advanced modes active") {
		t.Errorf("expected advanced warning, got %q", view)
	}
}

func TestEnsemblePanel_View_FocusedBorder(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.SetSize(60, 20)
	p.Focus()

	view := p.View()
	if view == "" {
		t.Error("expected non-empty view when focused")
	}
}

func TestEnsemblePanel_View_EmptyQuestion(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.SetSize(60, 20)
	p.now = func() time.Time { return time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC) }

	sess := &ensemble.EnsembleSession{
		Question:    "",
		PresetUsed:  "",
		Status:      ensemble.EnsembleActive,
		Assignments: []ensemble.ModeAssignment{},
		CreatedAt:   time.Date(2026, 1, 30, 11, 50, 0, 0, time.UTC),
	}
	p.SetSession(sess, nil)

	view := p.View()
	// Empty question should show dash
	if !strings.Contains(view, "—") {
		t.Errorf("expected dash for empty question, got %q", view)
	}
	// Empty preset should show "custom"
	if !strings.Contains(view, "custom") {
		t.Errorf("expected 'custom' for empty preset, got %q", view)
	}
}

func TestEnsemblePanel_View_HelpBar(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	p.SetSize(80, 30)
	p.now = func() time.Time { return time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC) }

	sess := &ensemble.EnsembleSession{
		Question: "Test",
		Status:   ensemble.EnsembleActive,
		Assignments: []ensemble.ModeAssignment{
			{ModeID: "mode-a", PaneName: "p__cc_1", Status: ensemble.AssignmentActive},
		},
		CreatedAt: time.Date(2026, 1, 30, 11, 50, 0, 0, time.UTC),
	}
	p.SetSession(sess, nil)

	view := p.View()
	if !strings.Contains(view, "synthesize") {
		t.Errorf("expected 'synthesize' in help bar, got %q", view)
	}
}

func TestEnsemblePanel_LookupMode_NilCatalog(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()
	mode := p.lookupMode("deductive")
	if mode != nil {
		t.Error("expected nil for nil catalog")
	}
}

func TestEnsemblePanel_LookupMode_ByID(t *testing.T) {
	t.Parallel()

	modes := []ensemble.ReasoningMode{
		{ID: "deductive", Code: "A1", Name: "Deductive Logic", Category: ensemble.CategoryFormal, Tier: ensemble.TierCore, ShortDesc: "Formal deductive logic"},
	}
	catalog, err := ensemble.NewModeCatalog(modes, "test")
	if err != nil {
		t.Fatalf("NewModeCatalog: %v", err)
	}

	p := NewEnsemblePanel()
	p.SetCatalog(catalog)

	mode := p.lookupMode("deductive")
	if mode == nil {
		t.Fatal("expected non-nil mode")
	}
	if mode.ID != "deductive" {
		t.Errorf("mode.ID = %q, want %q", mode.ID, "deductive")
	}
}

func TestEnsemblePanel_LookupMode_ByCode(t *testing.T) {
	t.Parallel()

	modes := []ensemble.ReasoningMode{
		{ID: "deductive", Code: "A1", Name: "Deductive Logic", Category: ensemble.CategoryFormal, Tier: ensemble.TierCore, ShortDesc: "Formal deductive logic"},
	}
	catalog, err := ensemble.NewModeCatalog(modes, "test")
	if err != nil {
		t.Fatalf("NewModeCatalog: %v", err)
	}

	p := NewEnsemblePanel()
	p.SetCatalog(catalog)

	// Lookup by code (case-insensitive)
	mode := p.lookupMode("a1")
	if mode == nil {
		t.Fatal("expected non-nil mode for code lookup")
	}
	if mode.ID != "deductive" {
		t.Errorf("mode.ID = %q, want %q", mode.ID, "deductive")
	}
}

func TestEnsemblePanel_LookupMode_NotFound(t *testing.T) {
	t.Parallel()

	modes := []ensemble.ReasoningMode{
		{ID: "deductive", Code: "A1", Name: "Deductive Logic", Category: ensemble.CategoryFormal, Tier: ensemble.TierCore, ShortDesc: "Formal deductive logic"},
	}
	catalog, err := ensemble.NewModeCatalog(modes, "test")
	if err != nil {
		t.Fatalf("NewModeCatalog: %v", err)
	}

	p := NewEnsemblePanel()
	p.SetCatalog(catalog)

	mode := p.lookupMode("nonexistent")
	if mode != nil {
		t.Error("expected nil for nonexistent mode")
	}
}

func TestEnsemblePanel_CountAdvanced(t *testing.T) {
	t.Parallel()

	modes := []ensemble.ReasoningMode{
		{ID: "deductive", Code: "A1", Name: "Deductive", Category: ensemble.CategoryFormal, Tier: ensemble.TierCore, ShortDesc: "Formal deductive logic"},
		{ID: "bayesian", Code: "C2", Name: "Bayesian", Category: ensemble.CategoryUncertainty, Tier: ensemble.TierAdvanced, ShortDesc: "Probabilistic reasoning"},
		{ID: "creative", Code: "H3", Name: "Creative", Category: ensemble.CategoryStrategic, Tier: ensemble.TierExperimental, ShortDesc: "Creative strategic reasoning"},
	}
	catalog, err := ensemble.NewModeCatalog(modes, "test")
	if err != nil {
		t.Fatalf("NewModeCatalog: %v", err)
	}

	p := NewEnsemblePanel()
	p.SetCatalog(catalog)

	assignments := []ensemble.ModeAssignment{
		{ModeID: "deductive", Status: ensemble.AssignmentActive},
		{ModeID: "bayesian", Status: ensemble.AssignmentActive},
		{ModeID: "creative", Status: ensemble.AssignmentActive},
	}

	count := p.countAdvanced(assignments)
	if count != 2 {
		t.Errorf("countAdvanced = %d, want 2 (1 advanced + 1 experimental)", count)
	}
}

func TestEnsemblePanel_CountAdvanced_NoCatalog(t *testing.T) {
	t.Parallel()

	p := NewEnsemblePanel()

	assignments := []ensemble.ModeAssignment{
		{ModeID: "deductive", Status: ensemble.AssignmentActive},
	}

	count := p.countAdvanced(assignments)
	if count != 0 {
		t.Errorf("countAdvanced without catalog = %d, want 0", count)
	}
}

func TestEnsemblePanel_CountAdvanced_AllCore(t *testing.T) {
	t.Parallel()

	modes := []ensemble.ReasoningMode{
		{ID: "deductive", Code: "A1", Name: "Deductive", Category: ensemble.CategoryFormal, Tier: ensemble.TierCore, ShortDesc: "Formal deductive logic"},
		{ID: "causal", Code: "F2", Name: "Causal", Category: ensemble.CategoryCausal, Tier: ensemble.TierCore, ShortDesc: "Causal reasoning"},
	}
	catalog, err := ensemble.NewModeCatalog(modes, "test")
	if err != nil {
		t.Fatalf("NewModeCatalog: %v", err)
	}

	p := NewEnsemblePanel()
	p.SetCatalog(catalog)

	assignments := []ensemble.ModeAssignment{
		{ModeID: "deductive"},
		{ModeID: "causal"},
	}

	count := p.countAdvanced(assignments)
	if count != 0 {
		t.Errorf("countAdvanced for all core = %d, want 0", count)
	}
}

func TestRenderTierBadge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tier ensemble.ModeTier
		want string
	}{
		{"core", ensemble.TierCore, "CORE"},
		{"advanced", ensemble.TierAdvanced, "ADV"},
		{"experimental", ensemble.TierExperimental, "EXP"},
		{"empty", ensemble.ModeTier(""), ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderTierBadge(tc.tier, theme.Current())
			if tc.want == "" {
				if got != "" {
					t.Errorf("renderTierBadge(%q) = %q, want empty", tc.tier, got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("renderTierBadge(%q) = %q, does not contain %q", tc.tier, got, tc.want)
			}
		})
	}
}

func TestRenderProgressBar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		percent float64
		width   int
	}{
		{"zero", 0, 10},
		{"half", 0.5, 10},
		{"full", 1.0, 10},
		{"narrow", 0.5, 2}, // should clamp to 4
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderProgressBar(tc.percent, tc.width)
			if got == "" {
				t.Error("expected non-empty progress bar")
			}
		})
	}
}

func TestEnsembleActionConstants(t *testing.T) {
	t.Parallel()

	if EnsembleActionSynthesize != "synthesize" {
		t.Errorf("EnsembleActionSynthesize = %q", EnsembleActionSynthesize)
	}
	if EnsembleActionViewOutputs != "view_outputs" {
		t.Errorf("EnsembleActionViewOutputs = %q", EnsembleActionViewOutputs)
	}
	if EnsembleActionRefresh != "refresh" {
		t.Errorf("EnsembleActionRefresh = %q", EnsembleActionRefresh)
	}
}
