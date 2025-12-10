// Package dashboard provides a stunning visual session dashboard
package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/tui/components"
	"github.com/Dicklesworthstone/ntm/internal/tui/icons"
	"github.com/Dicklesworthstone/ntm/internal/tui/styles"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DashboardTickMsg is sent for animation updates
type DashboardTickMsg time.Time

// RefreshMsg triggers a refresh of session data
type RefreshMsg struct{}

// StatusUpdateMsg is sent when status detection completes
type StatusUpdateMsg struct {
	Statuses []status.AgentStatus
	Time     time.Time
}

// Model is the session dashboard model
type Model struct {
	session    string
	panes      []tmux.Pane
	width      int
	height     int
	animTick   int
	cursor     int
	quitting   bool
	err        error

	// Stats
	claudeCount int
	codexCount  int
	geminiCount int
	userCount   int

	// Theme
	theme theme.Theme
	icons icons.IconSet

	// Compaction detection and recovery
	compaction *status.CompactionRecoveryIntegration

	// Per-pane status tracking
	paneStatus map[int]PaneStatus

	// Live status detection
	detector       *status.UnifiedDetector
	agentStatuses  map[string]status.AgentStatus // keyed by pane ID
	lastRefresh    time.Time
	refreshPaused  bool
	refreshCount   int

	// Auto-refresh configuration
	refreshInterval time.Duration
}

// PaneStatus tracks the status of a pane including compaction state
type PaneStatus struct {
	LastCompaction *time.Time // When compaction was last detected
	RecoverySent   bool       // Whether recovery prompt was sent
	State          string     // "working", "idle", "error", "compacted"
}

// KeyMap defines dashboard keybindings
type KeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Left    key.Binding
	Right   key.Binding
	Zoom    key.Binding
	Send    key.Binding
	Refresh key.Binding
	Pause   key.Binding
	Quit    key.Binding
	Num1    key.Binding
	Num2    key.Binding
	Num3    key.Binding
	Num4    key.Binding
	Num5    key.Binding
	Num6    key.Binding
	Num7    key.Binding
	Num8    key.Binding
	Num9    key.Binding
}

// DefaultRefreshInterval is the default auto-refresh interval
const DefaultRefreshInterval = 2 * time.Second

var dashKeys = KeyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
	Right:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
	Zoom:    key.NewBinding(key.WithKeys("z", "enter"), key.WithHelp("z/enter", "zoom")),
	Send:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "send prompt")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Pause:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause/resume auto-refresh")),
	Quit:    key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q/esc", "quit")),
	Num1:    key.NewBinding(key.WithKeys("1")),
	Num2:    key.NewBinding(key.WithKeys("2")),
	Num3:    key.NewBinding(key.WithKeys("3")),
	Num4:    key.NewBinding(key.WithKeys("4")),
	Num5:    key.NewBinding(key.WithKeys("5")),
	Num6:    key.NewBinding(key.WithKeys("6")),
	Num7:    key.NewBinding(key.WithKeys("7")),
	Num8:    key.NewBinding(key.WithKeys("8")),
	Num9:    key.NewBinding(key.WithKeys("9")),
}

// New creates a new dashboard model
func New(session string) Model {
	t := theme.Current()
	ic := icons.Current()

	return Model{
		session:         session,
		width:           80,
		height:          24,
		theme:           t,
		icons:           ic,
		compaction:      status.NewCompactionRecoveryIntegrationDefault(),
		paneStatus:      make(map[int]PaneStatus),
		detector:        status.NewDetector(),
		agentStatuses:   make(map[string]status.AgentStatus),
		refreshInterval: DefaultRefreshInterval,
	}
}

// NewWithInterval creates a dashboard with custom refresh interval
func NewWithInterval(session string, interval time.Duration) Model {
	m := New(session)
	m.refreshInterval = interval
	return m
}

// SessionDataMsg contains the fetched session data
type SessionDataMsg struct {
	Panes      []tmux.Pane
	PaneStatus map[int]PaneStatus
	Err        error
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.tick(),
		m.fetchSessionData(),
	)
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return DashboardTickMsg(t)
	})
}

func (m Model) refresh() tea.Cmd {
	return tea.Tick(m.refreshInterval, func(t time.Time) tea.Msg {
		return RefreshMsg{}
	})
}

// fetchSessionData performs the blocking tmux calls
func (m Model) fetchSessionData() tea.Cmd {
	return func() tea.Msg {
		panes, err := tmux.GetPanes(m.session)
		if err != nil {
			return SessionDataMsg{Err: err}
		}

		// Check compaction for each pane
		// Note: We use a local map here to avoid race conditions with the model's map
		paneStatus := make(map[int]PaneStatus)
		
		// Create a temporary compaction checker for this fetch
		// In a real scenario, we might want to share state, but for now this avoids 
		// sharing the 'compaction' struct pointer across goroutines without locking
		// or we can just access m.compaction if it's thread-safe.
		// Assuming m.compaction.CheckAndRecover is thread-safe or stateless enough.
		// Actually, CheckAndRecover tracks state. We should probably just pass the output 
		// and let the update loop handle the logic, OR make this part of the message.
		// For simplicity/safety in this refactor, let's just do the capture here
		// and the logic in Update, OR trust that CheckAndRecover is safe.
		// Let's do the capture here and return the raw data? 
		// No, let's keep the logic here but ensure we don't race on m.paneStatus.
		// We return a NEW map.
		
		// Copy existing status to preserve history if needed, 
		// but really we want the latest status.
		// Actually, m.paneStatus has history (LastCompaction). 
		// If we create a new map, we lose history unless we pass the old one in.
		// Let's just return the compaction events and merge in Update.
		
		// For this pass, let's do the tmux calls here.
		// We need to capture output.
		
		newStatuses := make(map[int]PaneStatus)
		
		for _, pane := range panes {
			if pane.Type == tmux.AgentUser {
				continue
			}
			
			// Capture output
			output, err := tmux.CapturePaneOutput(pane.ID, 50)
			if err != nil {
				continue
			}
			
			// We can't safely call m.compaction.CheckAndRecover here if it mutates state
			// and is also used by other goroutines (unlikely as tea.Cmd is one at a time, 
			// but multiple fetches could theoretically overlap if slow).
			// However, since we are inside a tea.Cmd, we are effectively concurrent with the main loop.
			// The standard Bubble Tea way is to pass data back.
			
			// Let's store the raw output or the result in the Msg.
			// Ideally, we'd move CheckAndRecover logic to the update loop or make it pure.
			// But CheckAndRecover likely updates internal trackers.
			// Let's assume for now we just return the captured output and let Update handle the logic
			// to avoid concurrency issues with m.compaction.
			
			// Wait, the original code called m.compaction.CheckAndRecover in Update (main thread).
			// So it was safe.
			// If we move it here (goroutine), we have a race on m.compaction.
			
			// Alternative: fetch panes and fetch output here. Return them.
			// Process logic in Update.
			
			// But fetching output is the heavy part.
			
			// Let's define a struct for PaneOutput.
			newStatuses[pane.Index] = PaneStatus{
				// Store output temporarily in State? No, that's hacky.
				// Let's add a field to SessionDataMsg.
			}
			// To keep it simple and safe:
			// Just fetch panes here. Fetching output for 20 panes is heavy, yes.
			// Maybe we can optimize by only fetching active panes or staggering?
			// For now, let's just fetch panes asynchronously. That solves the "tmux list-panes" blocking.
			// The output capture is also blocking.
			
			// Let's stick to the plan: fetch panes asynchronously.
			// The compaction check is also heavy (exec).
			// We really should do it async.
			// We can use a mutex on m.compaction if needed, but it's better to isolate.
		}
		
		return SessionDataMsg{Panes: panes, PaneStatus: newStatuses}
	}
}

// Helper struct to carry output data
type PaneOutputData struct {
	PaneIndex int
	Output    string
	AgentType string
}

type SessionDataWithOutputMsg struct {
	Panes   []tmux.Pane
	Outputs []PaneOutputData
	Err     error
}

func (m Model) fetchSessionDataWithOutputs() tea.Cmd {
	return func() tea.Msg {
		panes, err := tmux.GetPanes(m.session)
		if err != nil {
			return SessionDataWithOutputMsg{Err: err}
		}
		
		var outputs []PaneOutputData
		for _, pane := range panes {
			if pane.Type == tmux.AgentUser {
				continue
			}
			out, err := tmux.CapturePaneOutput(pane.ID, 50)
			if err == nil {
				outputs = append(outputs, PaneOutputData{
					PaneIndex: pane.Index,
					Output:    out,
					AgentType: string(pane.Type), // Simplified mapping
				})
			}
		}
		
		return SessionDataWithOutputMsg{Panes: panes, Outputs: outputs}
	}
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case DashboardTickMsg:
		m.animTick++
		return m, m.tick()

	case RefreshMsg:
		// Trigger async fetch
		return m, m.fetchSessionDataWithOutputs()
		
	case SessionDataWithOutputMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			m.panes = msg.Panes
			m.updateStats()
			
			// Process compaction checks on the main thread using fetched outputs
			for _, data := range msg.Outputs {
				// Map type string
				agentType := "unknown"
				switch data.AgentType {
				case string(tmux.AgentClaude), "cc": agentType = "claude"
				case string(tmux.AgentCodex), "cod": agentType = "codex"
				case string(tmux.AgentGemini), "gmi": agentType = "gemini"
				}
				
				event, recoverySent, _ := m.compaction.CheckAndRecover(data.Output, agentType, m.session, data.PaneIndex)
				
				if event != nil {
					ps := m.paneStatus[data.PaneIndex]
					now := time.Now()
					ps.LastCompaction = &now
					ps.RecoverySent = recoverySent
					ps.State = "compacted"
					m.paneStatus[data.PaneIndex] = ps
				}
			}
		}
		// Schedule next refresh
		return m, m.refresh()

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, dashKeys.Quit):
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, dashKeys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, dashKeys.Down):
			if m.cursor < len(m.panes)-1 {
				m.cursor++
			}

		case key.Matches(msg, dashKeys.Refresh):
			// Manual refresh
			return m, m.fetchSessionDataWithOutputs()

		case key.Matches(msg, dashKeys.Zoom):
			if len(m.panes) > 0 && m.cursor < len(m.panes) {
				// Zoom to selected pane
				p := m.panes[m.cursor]
				_ = tmux.ZoomPane(m.session, p.Index)
				return m, tea.Quit
			}

		// Number quick-select
		case key.Matches(msg, dashKeys.Num1):
			m.selectByNumber(1)
		case key.Matches(msg, dashKeys.Num2):
			m.selectByNumber(2)
		case key.Matches(msg, dashKeys.Num3):
			m.selectByNumber(3)
		case key.Matches(msg, dashKeys.Num4):
			m.selectByNumber(4)
		case key.Matches(msg, dashKeys.Num5):
			m.selectByNumber(5)
		case key.Matches(msg, dashKeys.Num6):
			m.selectByNumber(6)
		case key.Matches(msg, dashKeys.Num7):
			m.selectByNumber(7)
		case key.Matches(msg, dashKeys.Num8):
			m.selectByNumber(8)
		case key.Matches(msg, dashKeys.Num9):
			m.selectByNumber(9)
		}
	}

	return m, nil
}

func (m *Model) selectByNumber(n int) {
	idx := n - 1
	if idx >= 0 && idx < len(m.panes) {
		m.cursor = idx
	}
}

func (m *Model) updateStats() {
	m.claudeCount = 0
	m.codexCount = 0
	m.geminiCount = 0
	m.userCount = 0

	for _, p := range m.panes {
		switch p.Type {
		case tmux.AgentClaude:
			m.claudeCount++
		case tmux.AgentCodex:
			m.codexCount++
		case tmux.AgentGemini:
			m.geminiCount++
		default:
			m.userCount++
		}
	}
}

// checkCompaction polls pane output and checks for compaction events
func (m *Model) checkCompaction() {
	for _, pane := range m.panes {
		// Skip user panes
		if pane.Type == tmux.AgentUser {
			continue
		}

		// Capture recent output from the pane
		output, err := tmux.CapturePaneOutput(pane.ID, 50)
		if err != nil {
			continue
		}

		// Determine agent type string for detection
		agentType := "unknown"
		switch pane.Type {
		case tmux.AgentClaude:
			agentType = "claude"
		case tmux.AgentCodex:
			agentType = "codex"
		case tmux.AgentGemini:
			agentType = "gemini"
		}

		// Check for compaction and send recovery if detected
		event, recoverySent, _ := m.compaction.CheckAndRecover(output, agentType, m.session, pane.Index)

		// Update pane status
		ps := m.paneStatus[pane.Index]
		if event != nil {
			now := time.Now()
			ps.LastCompaction = &now
			ps.RecoverySent = recoverySent
			ps.State = "compacted"
		}
		m.paneStatus[pane.Index] = ps
	}
}

// View implements tea.Model
func (m Model) View() string {
	t := m.theme
	ic := m.icons

	var b strings.Builder

	b.WriteString("\n")

	// ═══════════════════════════════════════════════════════════════
	// HEADER with animated banner
	// ═══════════════════════════════════════════════════════════════
	bannerText := components.RenderBannerMedium(true, m.animTick)
	b.WriteString(bannerText + "\n")

	// Session title with gradient
	sessionTitle := ic.Session + "  " + m.session
	animatedSession := styles.Shimmer(sessionTitle, m.animTick,
		string(t.Blue), string(t.Lavender), string(t.Mauve))
	b.WriteString("  " + animatedSession + "\n")
	b.WriteString("  " + styles.GradientDivider(m.width-4,
		string(t.Blue), string(t.Mauve)) + "\n\n")

	// ═══════════════════════════════════════════════════════════════
	// STATS BAR with agent counts
	// ═══════════════════════════════════════════════════════════════
	statsBar := m.renderStatsBar()
	b.WriteString("  " + statsBar + "\n\n")

	// ═══════════════════════════════════════════════════════════════
	// PANE GRID VISUALIZATION
	// ═══════════════════════════════════════════════════════════════
	if m.err != nil {
		errorStyle := lipgloss.NewStyle().Foreground(t.Error)
		b.WriteString("  " + errorStyle.Render(ic.Cross+" Error: "+m.err.Error()) + "\n")
	} else if len(m.panes) == 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(t.Overlay).Italic(true)
		b.WriteString("  " + emptyStyle.Render("No panes found in session") + "\n")
	} else {
		// Render pane cards in a grid
		paneGrid := m.renderPaneGrid()
		b.WriteString(paneGrid + "\n")
	}

	// ═══════════════════════════════════════════════════════════════
	// HELP BAR
	// ═══════════════════════════════════════════════════════════════
	b.WriteString("\n")
	b.WriteString("  " + styles.GradientDivider(m.width-4,
		string(t.Surface2), string(t.Surface1)) + "\n")
	b.WriteString("  " + m.renderHelpBar() + "\n")

	return b.String()
}

func (m Model) renderStatsBar() string {
	t := m.theme
	ic := m.icons

	var parts []string

	// Total panes
	totalBadge := lipgloss.NewStyle().
		Background(t.Surface0).
		Foreground(t.Text).
		Padding(0, 1).
		Render(fmt.Sprintf("%s %d panes", ic.Pane, len(m.panes)))
	parts = append(parts, totalBadge)

	// Claude count
	if m.claudeCount > 0 {
		claudeBadge := lipgloss.NewStyle().
			Background(t.Claude).
			Foreground(t.Base).
			Bold(true).
			Padding(0, 1).
			Render(fmt.Sprintf("%s %d", ic.Claude, m.claudeCount))
		parts = append(parts, claudeBadge)
	}

	// Codex count
	if m.codexCount > 0 {
		codexBadge := lipgloss.NewStyle().
			Background(t.Codex).
			Foreground(t.Base).
			Bold(true).
			Padding(0, 1).
			Render(fmt.Sprintf("%s %d", ic.Codex, m.codexCount))
		parts = append(parts, codexBadge)
	}

	// Gemini count
	if m.geminiCount > 0 {
		geminiBadge := lipgloss.NewStyle().
			Background(t.Gemini).
			Foreground(t.Base).
			Bold(true).
			Padding(0, 1).
			Render(fmt.Sprintf("%s %d", ic.Gemini, m.geminiCount))
		parts = append(parts, geminiBadge)
	}

	// User count
	if m.userCount > 0 {
		userBadge := lipgloss.NewStyle().
			Background(t.Green).
			Foreground(t.Base).
			Bold(true).
			Padding(0, 1).
			Render(fmt.Sprintf("%s %d", ic.User, m.userCount))
		parts = append(parts, userBadge)
	}

	return strings.Join(parts, "  ")
}

func (m Model) renderPaneGrid() string {
	t := m.theme
	ic := m.icons

	var lines []string

	// Calculate card width based on terminal width
	cardWidth := 25
	cardsPerRow := (m.width - 4) / (cardWidth + 2)
	if cardsPerRow < 1 {
		cardsPerRow = 1
	}

	var cards []string

	for i, p := range m.panes {
		isSelected := i == m.cursor

		// Determine card colors based on agent type
		var borderColor, iconColor lipgloss.Color
		var agentIcon string

		switch p.Type {
		case tmux.AgentClaude:
			borderColor = t.Claude
			iconColor = t.Claude
			agentIcon = ic.Claude
		case tmux.AgentCodex:
			borderColor = t.Codex
			iconColor = t.Codex
			agentIcon = ic.Codex
		case tmux.AgentGemini:
			borderColor = t.Gemini
			iconColor = t.Gemini
			agentIcon = ic.Gemini
		default:
			borderColor = t.Green
			iconColor = t.Green
			agentIcon = ic.User
		}

		// Selection highlight
		if isSelected {
			borderColor = t.Pink
		}

		// Build card content
		var cardContent strings.Builder

		// Header line with icon and title
		iconStyled := lipgloss.NewStyle().Foreground(iconColor).Bold(true).Render(agentIcon)
		title := p.Title
		if len(title) > cardWidth-6 {
			title = title[:cardWidth-9] + "..."
		}

		titleStyled := lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(title)
		cardContent.WriteString(iconStyled + " " + titleStyled + "\n")

		// Index badge
		numBadge := lipgloss.NewStyle().
			Foreground(t.Overlay).
			Render(fmt.Sprintf("#%d", p.Index))
		cardContent.WriteString(numBadge + "\n")

		// Size info
		sizeStyle := lipgloss.NewStyle().Foreground(t.Subtext)
		cardContent.WriteString(sizeStyle.Render(fmt.Sprintf("%dx%d", p.Width, p.Height)) + "\n")

		// Command running (if any)
		if p.Command != "" {
			cmdStyle := lipgloss.NewStyle().Foreground(t.Overlay).Italic(true)
			cmd := p.Command
			if len(cmd) > cardWidth-4 {
				cmd = cmd[:cardWidth-7] + "..."
			}
			cardContent.WriteString(cmdStyle.Render(cmd))
		}

		// Compaction indicator
		if ps, ok := m.paneStatus[p.Index]; ok && ps.LastCompaction != nil {
			cardContent.WriteString("\n")
			compactStyle := lipgloss.NewStyle().Foreground(t.Warning).Bold(true)
			indicator := "⚠ compacted"
			if ps.RecoverySent {
				indicator = "↻ recovering"
			}
			cardContent.WriteString(compactStyle.Render(indicator))
		}

		// Create card box
		cardStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Width(cardWidth).
			Padding(0, 1)

		if isSelected {
			// Add glow effect for selected card
			cardStyle = cardStyle.
				Background(t.Surface0)
		}

		cards = append(cards, cardStyle.Render(cardContent.String()))
	}

	// Arrange cards in rows
	for i := 0; i < len(cards); i += cardsPerRow {
		end := i + cardsPerRow
		if end > len(cards) {
			end = len(cards)
		}
		row := lipgloss.JoinHorizontal(lipgloss.Top, cards[i:end]...)
		lines = append(lines, "  "+row)
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderHelpBar() string {
	t := m.theme

	keyStyle := lipgloss.NewStyle().
		Background(t.Surface0).
		Foreground(t.Text).
		Bold(true).
		Padding(0, 1)

	descStyle := lipgloss.NewStyle().
		Foreground(t.Overlay)

	items := []struct {
		key  string
		desc string
	}{
		{"↑↓", "navigate"},
		{"1-9", "select"},
		{"z", "zoom"},
		{"r", "refresh"},
		{"q", "quit"},
	}

	var parts []string
	for _, item := range items {
		parts = append(parts, keyStyle.Render(item.key)+" "+descStyle.Render(item.desc))
	}

	return strings.Join(parts, "  ")
}

// Run starts the dashboard
func Run(session string) error {
	model := New(session)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
