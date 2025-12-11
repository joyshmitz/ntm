package dashboard

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/tui/layout"
	"github.com/charmbracelet/lipgloss"
)

func newTestModel(width int) Model {
	m := New("test")
	m.width = width
	m.height = 30
	m.tier = layout.TierForWidth(width)
	m.panes = []tmux.Pane{
		{
			ID:      "1",
			Index:   1,
			Title:   "codex-long-title-for-wrap-check",
			Type:    tmux.AgentCodex,
			Variant: "VARIANT",
			Command: "run --flag",
		},
	}
	m.cursor = 0
	m.paneStatus[1] = PaneStatus{
		State:          "working",
		ContextPercent: 50,
		ContextLimit:   1000,
	}
	return m
}

func TestPaneListColumnsByWidthTiers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		width       int
		expectCTX   bool
		expectModel bool
		name        string
	}{
		{width: 80, expectCTX: false, expectModel: false, name: "narrow"},
		{width: 120, expectCTX: false, expectModel: false, name: "split-threshold"},
		{width: 160, expectCTX: false, expectModel: false, name: "mid-split"},
		{width: 200, expectCTX: true, expectModel: true, name: "wide"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := newTestModel(tc.width)
			// Use a fixed content width so comparisons stay stable across tiers.
			list := m.renderPaneList(60)
			lines := strings.Split(list, "\n")
			if len(lines) < 2 {
				t.Fatalf("expected header and at least one row, got %d lines", len(lines))
			}

			header := lines[0]
			headerClean := status.StripANSI(header)

			var row string
			for _, line := range lines[1:] {
				clean := strings.Trim(status.StripANSI(line), " ─")
				if clean == "" {
					continue // skip border lines from the header style
				}
				row = line
				break
			}
			if row == "" {
				t.Fatalf("width %d: no pane row found in rendered list", tc.width)
			}
			rowClean := status.StripANSI(row)

			if tc.expectCTX {
				if !strings.Contains(headerClean, "CTX") {
					t.Fatalf("width %d: expected CTX column in header", tc.width)
				}
			} else if strings.Contains(headerClean, "CTX") {
				t.Fatalf("width %d: unexpected CTX column in header", tc.width)
			}

			if tc.expectModel {
				if !strings.Contains(headerClean, "MODEL") {
					t.Fatalf("width %d: expected MODEL column in header", tc.width)
				}
				if !strings.Contains(rowClean, "VARIANT") {
					t.Fatalf("width %d: expected variant to be rendered in row (row=%q header=%q)", tc.width, rowClean, headerClean)
				}
			} else {
				if strings.Contains(headerClean, "MODEL") {
					t.Fatalf("width %d: unexpected MODEL column in header", tc.width)
				}
				if strings.Contains(rowClean, "VARIANT") {
					t.Fatalf("width %d: expected variant to be hidden for narrower tiers", tc.width)
				}
			}

			if strings.Contains(headerClean, "CMD") {
				t.Fatalf("width %d: CMD column should only appear on ultra-wide tiers", tc.width)
			}

			if w := lipgloss.Width(row); w != 60 {
				t.Fatalf("width %d: rendered row width = %d, want 60", tc.width, w)
			}
		})
	}
}

func TestPaneRowSelectionStyling_NoWrapAcrossWidths(t *testing.T) {
	t.Parallel()

	widths := []int{80, 120, 160, 200}
	for _, w := range widths {
		w := w
		t.Run(fmt.Sprintf("width_%d", w), func(t *testing.T) {
			t.Parallel()

			m := newTestModel(w)
			m.cursor = 0 // selected row

			row := m.renderPaneRow(m.panes[0], 0, true, 60)
			clean := status.StripANSI(row)

			if lipgloss.Width(clean) != 60 {
				t.Fatalf("width %d: row width = %d, want 60", w, lipgloss.Width(clean))
			}

			if strings.Contains(clean, "\n") {
				t.Fatalf("width %d: row contained unexpected newline", w)
			}
		})
	}
}

func TestSplitViewLayouts_ByWidthTiers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		width        int
		expectList   bool
		expectDetail bool
	}{
		{width: 120, expectList: true, expectDetail: true},
		{width: 160, expectList: true, expectDetail: true},
		{width: 200, expectList: true, expectDetail: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("width_%d", tc.width), func(t *testing.T) {
			t.Parallel()

			m := newTestModel(tc.width)
			m.height = 30
			if m.tier < layout.TierSplit {
				t.Skip("split view not used below split threshold")
			}
			out := m.renderSplitView()
			plain := status.StripANSI(out)

			// Ensure we always render the list panel
			if !strings.Contains(plain, "TITLE") {
				t.Fatalf("width %d: expected list header 'TITLE' in split view", tc.width)
			}

			if tc.expectDetail {
				if !strings.Contains(plain, "Context Usage") && m.tier >= layout.TierWide {
					t.Fatalf("width %d: expected detail pane content (Context Usage) at wide tier", tc.width)
				}
			} else {
				// For narrow widths we shouldn't render split view; ensure single-panel fallback
				if strings.Contains(plain, "Context Usage") && tc.width < layout.SplitViewThreshold {
					t.Fatalf("width %d: unexpected detail content for narrow layout", tc.width)
				}
			}
		})
	}
}

func TestSplitProportionsAcrossThresholds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		total         int
		expectSplit   bool
		expectNonZero bool
		name          string
	}{
		{total: 80, expectSplit: false, expectNonZero: false, name: "narrow"},
		{total: 120, expectSplit: true, expectNonZero: true, name: "split-threshold"},
		{total: 160, expectSplit: true, expectNonZero: true, name: "mid-split"},
		{total: 200, expectSplit: true, expectNonZero: true, name: "wide"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			left, right := layout.SplitProportions(tc.total)

			if left+right > tc.total {
				t.Fatalf("total %d: left+right=%d exceeds total width", tc.total, left+right)
			}

			if tc.expectSplit {
				if right == 0 {
					t.Fatalf("total %d: expected split view to allocate right panel", tc.total)
				}
			} else if right != 0 {
				t.Fatalf("total %d: expected single column layout, got right=%d", tc.total, right)
			}

			if tc.expectNonZero && (left == 0 || right == 0) {
				t.Fatalf("total %d: both panels should be non-zero (left=%d right=%d)", tc.total, left, right)
			}
		})
	}
}
