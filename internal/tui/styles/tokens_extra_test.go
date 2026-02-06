package styles

import (
	"testing"
)

// ---------------------------------------------------------------------------
// UltraWide — 0% → 100%
// ---------------------------------------------------------------------------

func TestUltraWide(t *testing.T) {
	t.Parallel()

	tokens := UltraWide()
	if tokens.Spacing.MD <= 0 {
		t.Error("expected positive MD spacing")
	}
	// UltraWide should have larger values than Default
	def := DefaultTokens()
	if tokens.Spacing.MD < def.Spacing.MD {
		t.Errorf("UltraWide spacing MD (%d) should be >= DefaultTokens MD (%d)",
			tokens.Spacing.MD, def.Spacing.MD)
	}
}

// ---------------------------------------------------------------------------
// GetLayoutMode — 0% → 100%
// ---------------------------------------------------------------------------

func TestGetLayoutMode(t *testing.T) {
	t.Parallel()

	bp := DefaultBreakpoints

	tests := []struct {
		name  string
		width int
		want  LayoutMode
	}{
		{"narrow", bp.XS - 1, LayoutCompact},
		{"at_xs", bp.XS, LayoutDefault},
		{"default", bp.MD - 1, LayoutDefault},
		{"at_md", bp.MD, LayoutSpacious},
		{"spacious", bp.Wide - 1, LayoutSpacious},
		{"ultra_wide", bp.Wide, LayoutUltraWide},
		{"very_wide", 300, LayoutUltraWide},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := GetLayoutMode(tt.width)
			if got != tt.want {
				t.Errorf("GetLayoutMode(%d) = %d, want %d", tt.width, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AdaptiveCardDimensions — 0% → 100%
// ---------------------------------------------------------------------------

func TestAdaptiveCardDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		total        int
		minCard      int
		maxCard      int
		gap          int
		wantWidth    int
		wantPerRow   int
	}{
		{"zero_width", 0, 20, 40, 2, 1, 1},
		{"zero_min", 100, 0, 40, 2, 1, 1},
		{"zero_max", 100, 20, 0, 2, 1, 1},
		{"narrow", 15, 20, 40, 2, 15, 1},
		{"single_card", 20, 20, 40, 2, 20, 1},
		{"two_cards", 44, 20, 40, 2, 21, 2},
		// Wide enough for max-width clamping branch:
		// total=100, min=40, max=45, gap=2: initial cards=(102/42)=2, width=(100-2)/2=49 > 45
		// After clamp: cards=(102/47)=2, width=45
		{"max_clamp", 100, 40, 45, 2, 45, 2},
		// Negative gap produces cardsPerRow<1 after initial calc
		{"negative_gap_guard", 10, 5, 40, -100, 10, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			width, perRow := AdaptiveCardDimensions(tt.total, tt.minCard, tt.maxCard, tt.gap)
			if width != tt.wantWidth {
				t.Errorf("AdaptiveCardDimensions(%d, %d, %d, %d) width = %d, want %d",
					tt.total, tt.minCard, tt.maxCard, tt.gap, width, tt.wantWidth)
			}
			if perRow != tt.wantPerRow {
				t.Errorf("AdaptiveCardDimensions(%d, %d, %d, %d) perRow = %d, want %d",
					tt.total, tt.minCard, tt.maxCard, tt.gap, perRow, tt.wantPerRow)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Style builder functions — 0% → 100%
// ---------------------------------------------------------------------------

func TestPanelStyle(t *testing.T) {
	t.Parallel()

	// Should not panic with reasonable inputs
	s := PanelStyle(false, 40, 10)
	if s.GetWidth() <= 0 {
		t.Error("expected positive panel width")
	}

	focused := PanelStyle(true, 40, 10)
	if focused.GetWidth() <= 0 {
		t.Error("expected positive focused panel width")
	}
}

func TestHeaderStyle(t *testing.T) {
	t.Parallel()

	s := HeaderStyle(40)
	if s.GetWidth() <= 0 {
		t.Error("expected positive header width")
	}
}

func TestListItemStyle(t *testing.T) {
	t.Parallel()

	normal := ListItemStyle(false)
	selected := ListItemStyle(true)

	// Both should be valid styles (not panic)
	_ = normal.Render("test")
	_ = selected.Render("test")
}

func TestKeyBadgeStyle(t *testing.T) {
	t.Parallel()
	s := KeyBadgeStyle()
	got := s.Render("K")
	if got == "" {
		t.Error("expected non-empty badge")
	}
}

func TestKeyDescStyle(t *testing.T) {
	t.Parallel()
	s := KeyDescStyle()
	got := s.Render("description")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestMutedStyle(t *testing.T) {
	t.Parallel()
	s := MutedStyle()
	got := s.Render("muted text")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestErrorStyle(t *testing.T) {
	t.Parallel()
	s := ErrorStyle()
	got := s.Render("error text")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestSuccessStyle(t *testing.T) {
	t.Parallel()
	s := SuccessStyle()
	got := s.Render("success")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestWarningStyle(t *testing.T) {
	t.Parallel()
	s := WarningStyle()
	got := s.Render("warning")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestInfoStyle(t *testing.T) {
	t.Parallel()
	s := InfoStyle()
	got := s.Render("info")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestBoldStyle(t *testing.T) {
	t.Parallel()
	s := BoldStyle()
	got := s.Render("bold")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestSectionTitleStyle(t *testing.T) {
	t.Parallel()
	s := SectionTitleStyle()
	got := s.Render("title")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestDividerLineStyle(t *testing.T) {
	t.Parallel()
	s := DividerLineStyle()
	got := s.Render("---")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestOverlayBoxStyle(t *testing.T) {
	t.Parallel()
	s := OverlayBoxStyle()
	got := s.Render("overlay content")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestFooterHintStyle(t *testing.T) {
	t.Parallel()
	s := FooterHintStyle()
	got := s.Render("press q to quit")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestStatusBadgeStyle(t *testing.T) {
	t.Parallel()
	s := StatusBadgeStyle("46")
	got := s.Render("OK")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestTableCellStyle(t *testing.T) {
	t.Parallel()

	// Zero width: plain style
	s0 := TableCellStyle(0)
	_ = s0.Render("cell")

	// Positive width
	s20 := TableCellStyle(20)
	if s20.GetWidth() != 20 {
		t.Errorf("TableCellStyle(20) width = %d, want 20", s20.GetWidth())
	}
}

func TestTableHeaderStyle(t *testing.T) {
	t.Parallel()
	s := TableHeaderStyle()
	got := s.Render("Header")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestInlinePaddingStyle(t *testing.T) {
	t.Parallel()
	s := InlinePaddingStyle(2)
	got := s.Render("padded")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestBoxPaddingStyle(t *testing.T) {
	t.Parallel()
	s := BoxPaddingStyle(1, 2)
	got := s.Render("box")
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestCenteredStyle(t *testing.T) {
	t.Parallel()
	s := CenteredStyle(40)
	if s.GetWidth() != 40 {
		t.Errorf("CenteredStyle(40) width = %d, want 40", s.GetWidth())
	}
}
