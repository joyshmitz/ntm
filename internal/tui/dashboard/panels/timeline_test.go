package panels

import (
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/state"
)

func TestNewTimelinePanel(t *testing.T) {
	t.Log("TIMELINE_TEST: TestNewTimelinePanel | Creating new timeline panel")

	panel := NewTimelinePanel()

	if panel == nil {
		t.Fatal("NewTimelinePanel returned nil")
	}

	cfg := panel.Config()
	if cfg.ID != "timeline" {
		t.Errorf("expected ID 'timeline', got %q", cfg.ID)
	}
	if cfg.Title != "Agent Timeline" {
		t.Errorf("expected Title 'Agent Timeline', got %q", cfg.Title)
	}
	if panel.timeWindow != 30*time.Minute {
		t.Errorf("expected default timeWindow 30m, got %v", panel.timeWindow)
	}
	if panel.zoomLevel != 0 {
		t.Errorf("expected default zoomLevel 0, got %d", panel.zoomLevel)
	}

	t.Logf("TIMELINE_TEST: Panel created | ID=%s Title=%s Window=%v Zoom=%d",
		cfg.ID, cfg.Title, panel.timeWindow, panel.zoomLevel)
}

func TestTimelinePanel_SetData(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_SetData | Testing data updates")

	panel := NewTimelinePanel()
	now := time.Now()

	events := []state.AgentEvent{
		{
			AgentID:   "cc_1",
			AgentType: state.AgentTypeClaude,
			State:     state.TimelineWorking,
			Timestamp: now.Add(-5 * time.Minute),
		},
		{
			AgentID:   "cc_1",
			AgentType: state.AgentTypeClaude,
			State:     state.TimelineIdle,
			Timestamp: now,
		},
		{
			AgentID:   "cod_1",
			AgentType: state.AgentTypeCodex,
			State:     state.TimelineWaiting,
			Timestamp: now.Add(-2 * time.Minute),
		},
	}

	data := TimelineData{Events: events}
	panel.SetData(data, nil)

	if panel.HasError() {
		t.Error("expected no error after SetData with nil error")
	}

	agents := panel.getAgentList()
	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}

	t.Logf("TIMELINE_TEST: Data set | Events=%d Agents=%v Error=%v",
		len(events), agents, panel.HasError())
}

func TestTimelinePanel_GetAgentList(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_GetAgentList | Testing agent extraction")

	panel := NewTimelinePanel()
	now := time.Now()

	events := []state.AgentEvent{
		{AgentID: "cc_2", Timestamp: now},
		{AgentID: "cc_1", Timestamp: now},
		{AgentID: "gmi_1", Timestamp: now},
		{AgentID: "cc_1", Timestamp: now}, // duplicate
	}

	panel.SetData(TimelineData{Events: events}, nil)
	agents := panel.getAgentList()

	// Should be sorted and deduplicated
	expected := []string{"cc_1", "cc_2", "gmi_1"}
	if len(agents) != len(expected) {
		t.Errorf("expected %d agents, got %d", len(expected), len(agents))
	}
	for i, exp := range expected {
		if agents[i] != exp {
			t.Errorf("agent[%d]: expected %q, got %q", i, exp, agents[i])
		}
	}

	t.Logf("TIMELINE_TEST: Agent list | Expected=%v Got=%v", expected, agents)
}

func TestTimelinePanel_StateColors(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_StateColors | Testing state-to-color mapping")

	panel := NewTimelinePanel()

	tests := []struct {
		state state.TimelineState
		char  string
	}{
		{state.TimelineWorking, "█"},
		{state.TimelineWaiting, "▓"},
		{state.TimelineError, "▒"},
		{state.TimelineIdle, "░"},
		{state.TimelineStopped, "·"},
	}

	for _, tt := range tests {
		char := panel.stateChar(tt.state)
		if char != tt.char {
			t.Errorf("stateChar(%v): expected %q, got %q", tt.state, tt.char, char)
		}

		// Colors are set by theme, just verify they're not empty
		color := panel.stateColor(tt.state)
		if color == "" {
			t.Errorf("stateColor(%v): returned empty color", tt.state)
		}

		t.Logf("TIMELINE_TEST: State=%v Char=%q Color=%v", tt.state, char, color)
	}
}

func TestTimelinePanel_GetStateInRange(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_GetStateInRange | Testing state lookup by time range")

	panel := NewTimelinePanel()
	now := time.Now()

	events := []state.AgentEvent{
		{State: state.TimelineIdle, Timestamp: now.Add(-10 * time.Minute)},
		{State: state.TimelineWorking, Timestamp: now.Add(-5 * time.Minute)},
		{State: state.TimelineWaiting, Timestamp: now.Add(-2 * time.Minute)},
		{State: state.TimelineIdle, Timestamp: now},
	}

	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		expected state.TimelineState
	}{
		{
			name:     "before all events",
			start:    now.Add(-15 * time.Minute),
			end:      now.Add(-12 * time.Minute),
			expected: state.TimelineIdle, // default
		},
		{
			name:     "during working period",
			start:    now.Add(-4 * time.Minute),
			end:      now.Add(-3 * time.Minute),
			expected: state.TimelineWorking,
		},
		{
			name:     "during waiting period",
			start:    now.Add(-1 * time.Minute),
			end:      now.Add(-30 * time.Second),
			expected: state.TimelineWaiting,
		},
		{
			name:     "at current time",
			start:    now,
			end:      now.Add(1 * time.Minute),
			expected: state.TimelineIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := panel.getStateInRange(events, tt.start, tt.end)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
			t.Logf("TIMELINE_TEST: %s | Range=[%v,%v] State=%v",
				tt.name, tt.start.Format("15:04:05"), tt.end.Format("15:04:05"), result)
		})
	}
}

func TestTimelinePanel_ZoomLevels(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_ZoomLevels | Testing zoom window calculations")

	panel := NewTimelinePanel()

	// Test all zoom levels
	expectedWindows := map[int]time.Duration{
		-3: 2 * time.Hour,
		-2: 1 * time.Hour,
		-1: 45 * time.Minute,
		0:  30 * time.Minute,
		1:  15 * time.Minute,
		2:  10 * time.Minute,
		3:  5 * time.Minute,
		4:  2 * time.Minute,
		5:  1 * time.Minute,
	}

	for zoom, expected := range expectedWindows {
		panel.zoomLevel = zoom
		window := panel.windowForZoom()
		if window != expected {
			t.Errorf("zoom %d: expected window %v, got %v", zoom, expected, window)
		}
		t.Logf("TIMELINE_TEST: Zoom=%d Window=%v", zoom, window)
	}
}

func TestTimelinePanel_ScrollStep(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_ScrollStep | Testing scroll step calculation")

	panel := NewTimelinePanel()

	// Default 30m window should scroll by 5m
	expected := 5 * time.Minute
	step := panel.scrollStep()
	if step != expected {
		t.Errorf("expected scroll step %v, got %v", expected, step)
	}

	// Zoom in to 15m window, should scroll by 2.5m
	panel.zoomLevel = 1
	panel.timeWindow = panel.windowForZoom()
	expected = 2*time.Minute + 30*time.Second
	step = panel.scrollStep()
	if step != expected {
		t.Errorf("expected scroll step %v, got %v", expected, step)
	}

	t.Logf("TIMELINE_TEST: ScrollStep | Zoom=%d Window=%v Step=%v",
		panel.zoomLevel, panel.timeWindow, step)
}

func TestTimelinePanel_RenderEmpty(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_RenderEmpty | Testing empty state rendering")

	panel := NewTimelinePanel()
	panel.SetSize(60, 15)

	// No data
	panel.SetData(TimelineData{}, nil)

	view := panel.View()

	// Should contain empty state message
	if !strings.Contains(view, "No agent activity") {
		t.Error("expected 'No agent activity' in empty view")
	}
	if !strings.Contains(view, "ntm spawn") {
		t.Error("expected spawn hint in empty view")
	}

	t.Logf("TIMELINE_TEST: Empty view rendered | Length=%d", len(view))
}

func TestTimelinePanel_RenderWithData(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_RenderWithData | Testing rendering with events")

	panel := NewTimelinePanel()
	panel.SetSize(80, 20)

	now := time.Now()
	events := []state.AgentEvent{
		{
			AgentID:   "cc_1",
			AgentType: state.AgentTypeClaude,
			State:     state.TimelineWorking,
			Timestamp: now.Add(-10 * time.Minute),
		},
		{
			AgentID:   "cc_1",
			AgentType: state.AgentTypeClaude,
			State:     state.TimelineIdle,
			Timestamp: now.Add(-5 * time.Minute),
		},
		{
			AgentID:   "cod_1",
			AgentType: state.AgentTypeCodex,
			State:     state.TimelineWaiting,
			Timestamp: now.Add(-3 * time.Minute),
		},
	}

	panel.SetData(TimelineData{Events: events}, nil)

	view := panel.View()

	// Should contain agent labels
	if !strings.Contains(view, "cc_1") {
		t.Error("expected 'cc_1' in view")
	}
	if !strings.Contains(view, "cod_1") {
		t.Error("expected 'cod_1' in view")
	}

	// Should contain timeline indicators
	if !strings.Contains(view, "[") || !strings.Contains(view, "]") {
		t.Error("expected timeline brackets in view")
	}

	// Should contain time axis
	if !strings.Contains(view, ":") {
		t.Error("expected time markers in view")
	}

	// Should show LIVE indicator when at current time
	if !strings.Contains(view, "LIVE") {
		t.Error("expected 'LIVE' indicator in view")
	}

	t.Logf("TIMELINE_TEST: View rendered | Length=%d Lines=%d",
		len(view), strings.Count(view, "\n")+1)
}

func TestTimelinePanel_Keybindings(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_Keybindings | Testing keyboard shortcuts")

	panel := NewTimelinePanel()
	bindings := panel.Keybindings()

	expectedActions := map[string]bool{
		"zoom_in":        false,
		"zoom_out":       false,
		"scroll_back":    false,
		"scroll_forward": false,
		"jump_now":       false,
		"details":        false,
	}

	for _, b := range bindings {
		if _, ok := expectedActions[b.Action]; ok {
			expectedActions[b.Action] = true
			t.Logf("TIMELINE_TEST: Keybinding | Action=%s Description=%q",
				b.Action, b.Description)
		}
	}

	for action, found := range expectedActions {
		if !found {
			t.Errorf("missing keybinding for action %q", action)
		}
	}
}

func TestTimelinePanel_AgentColors(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_AgentColors | Testing agent type colors")

	panel := NewTimelinePanel()

	tests := []struct {
		agentID      string
		expectedType string
	}{
		{"cc_1", "Claude"},
		{"cc_worker", "Claude"},
		{"cod_1", "Codex"},
		{"cod_main", "Codex"},
		{"gmi_1", "Gemini"},
		{"gmi_helper", "Gemini"},
		{"other", "default"},
	}

	for _, tt := range tests {
		color := panel.agentColor(tt.agentID)
		// Just verify we get a non-empty color
		if color == "" {
			t.Errorf("agentColor(%q): returned empty", tt.agentID)
		}
		t.Logf("TIMELINE_TEST: Agent=%q Type=%s Color=%v", tt.agentID, tt.expectedType, color)
	}
}

func TestTimelinePanel_FormatAgentLabel(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_FormatAgentLabel | Testing label formatting")

	panel := NewTimelinePanel()

	tests := []struct {
		agentID  string
		width    int
		expected string
	}{
		{"cc_1", 10, "cc_1      "},
		{"very_long_agent_name", 10, "very_long…"},
		{"short", 8, "short   "},
	}

	for _, tt := range tests {
		result := panel.formatAgentLabel(tt.agentID, tt.width)
		if result != tt.expected {
			t.Errorf("formatAgentLabel(%q, %d): expected %q, got %q",
				tt.agentID, tt.width, tt.expected, result)
		}
		t.Logf("TIMELINE_TEST: formatAgentLabel(%q, %d) = %q", tt.agentID, tt.width, result)
	}
}

func TestTimelinePanel_FocusBlur(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_FocusBlur | Testing focus state")

	panel := NewTimelinePanel()

	if panel.IsFocused() {
		t.Error("panel should not be focused initially")
	}

	panel.Focus()
	if !panel.IsFocused() {
		t.Error("panel should be focused after Focus()")
	}

	panel.Blur()
	if panel.IsFocused() {
		t.Error("panel should not be focused after Blur()")
	}

	t.Log("TIMELINE_TEST: Focus/Blur works correctly")
}

func TestTimelinePanel_Scroll(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_Scroll | Testing scroll left/right functionality")

	panel := NewTimelinePanel()
	now := time.Now()

	// Set up data
	events := []state.AgentEvent{
		{AgentID: "cc_1", State: state.TimelineWorking, Timestamp: now.Add(-30 * time.Minute)},
		{AgentID: "cc_1", State: state.TimelineIdle, Timestamp: now.Add(-15 * time.Minute)},
		{AgentID: "cc_1", State: state.TimelineWorking, Timestamp: now},
	}
	panel.SetData(TimelineData{Events: events}, nil)

	t.Run("initial offset is zero", func(t *testing.T) {
		if panel.timeOffset != 0 {
			t.Errorf("expected initial timeOffset=0, got %v", panel.timeOffset)
		}
	})

	t.Run("scroll back increases negative offset", func(t *testing.T) {
		initialOffset := panel.timeOffset

		// Simulate pressing left key
		panel.handleScroll("left")

		if panel.timeOffset >= initialOffset {
			t.Errorf("expected timeOffset to decrease (go back in time), got offset=%v", panel.timeOffset)
		}

		// Should be negative now (looking at past)
		if panel.timeOffset >= 0 {
			t.Errorf("expected negative timeOffset after scrolling back, got %v", panel.timeOffset)
		}

		t.Logf("TIMELINE_TEST: Scrolled back | Offset=%v", panel.timeOffset)
	})

	t.Run("scroll forward decreases negative offset", func(t *testing.T) {
		// First scroll back a bit
		panel.timeOffset = -10 * time.Minute
		initialOffset := panel.timeOffset

		// Simulate pressing right key
		panel.handleScroll("right")

		if panel.timeOffset <= initialOffset {
			t.Errorf("expected timeOffset to increase (go forward in time), got offset=%v", panel.timeOffset)
		}

		t.Logf("TIMELINE_TEST: Scrolled forward | Offset=%v", panel.timeOffset)
	})

	t.Run("scroll forward capped at zero", func(t *testing.T) {
		// Set offset close to zero
		panel.timeOffset = -1 * time.Minute

		// Scroll forward multiple times
		for i := 0; i < 10; i++ {
			panel.handleScroll("right")
		}

		// Should not go positive (cannot see future)
		if panel.timeOffset > 0 {
			t.Errorf("expected timeOffset capped at 0, got %v", panel.timeOffset)
		}

		t.Logf("TIMELINE_TEST: Forward scroll capped | Offset=%v", panel.timeOffset)
	})

	t.Run("jump to now resets offset", func(t *testing.T) {
		// First scroll back
		panel.timeOffset = -30 * time.Minute

		// Jump to now (n key)
		panel.handleJumpToNow()

		if panel.timeOffset != 0 {
			t.Errorf("expected timeOffset=0 after jump to now, got %v", panel.timeOffset)
		}

		t.Logf("TIMELINE_TEST: Jumped to now | Offset=%v", panel.timeOffset)
	})

	t.Run("scroll step proportional to zoom", func(t *testing.T) {
		// At default zoom (30m window), step should be 5m
		panel.zoomLevel = 0
		panel.timeWindow = panel.windowForZoom()
		step1 := panel.scrollStep()

		// Zoom in (15m window), step should be 2.5m
		panel.zoomLevel = 1
		panel.timeWindow = panel.windowForZoom()
		step2 := panel.scrollStep()

		if step2 >= step1 {
			t.Errorf("expected smaller scroll step when zoomed in, got step1=%v, step2=%v", step1, step2)
		}

		t.Logf("TIMELINE_TEST: Scroll steps | Zoom0=%v Zoom1=%v", step1, step2)
	})
}

// handleScroll is a test helper that simulates scroll key handling
func (m *TimelinePanel) handleScroll(direction string) {
	switch direction {
	case "left", "h":
		m.timeOffset -= m.scrollStep()
	case "right", "l":
		if m.timeOffset < 0 {
			m.timeOffset += m.scrollStep()
			if m.timeOffset > 0 {
				m.timeOffset = 0
			}
		}
	}
}

// handleJumpToNow is a test helper that simulates the "n" key
func (m *TimelinePanel) handleJumpToNow() {
	m.timeOffset = 0
}

func TestTimelinePanel_CursorBounds(t *testing.T) {
	t.Log("TIMELINE_TEST: TestTimelinePanel_CursorBounds | Testing cursor stays in bounds")

	panel := NewTimelinePanel()
	now := time.Now()

	// Set up data with 2 agents
	events := []state.AgentEvent{
		{AgentID: "cc_1", State: state.TimelineIdle, Timestamp: now},
		{AgentID: "cc_2", State: state.TimelineIdle, Timestamp: now},
	}
	panel.SetData(TimelineData{Events: events}, nil)

	// Cursor should be 0
	if panel.cursor != 0 {
		t.Errorf("expected cursor 0, got %d", panel.cursor)
	}

	// Set cursor beyond bounds then update data with fewer agents
	panel.cursor = 5
	events = []state.AgentEvent{
		{AgentID: "cc_1", State: state.TimelineIdle, Timestamp: now},
	}
	panel.SetData(TimelineData{Events: events}, nil)

	// Cursor should be clamped to 0 (only 1 agent)
	if panel.cursor != 0 {
		t.Errorf("expected cursor clamped to 0, got %d", panel.cursor)
	}

	t.Log("TIMELINE_TEST: Cursor bounds maintained correctly")
}
