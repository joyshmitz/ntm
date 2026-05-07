package pipeline

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// TmuxClient is the narrow tmux surface the pipeline executor needs.
// Production executors use realTmuxClient; tests can install MockTmuxClient
// with Executor.SetTmuxClient to avoid touching a live tmux server.
type TmuxClient interface {
	GetPanes(session string) ([]tmux.Pane, error)
	PasteKeys(target, content string, enter bool) error
	CapturePaneOutput(target string, lines int) (string, error)
}

type realTmuxClient struct{}

func (realTmuxClient) GetPanes(session string) ([]tmux.Pane, error) {
	return tmux.GetPanes(session)
}

func (realTmuxClient) PasteKeys(target, content string, enter bool) error {
	return tmux.PasteKeys(target, content, enter)
}

func (realTmuxClient) CapturePaneOutput(target string, lines int) (string, error) {
	return tmux.CapturePaneOutput(target, lines)
}

// MockTmuxPaste records one PasteKeys call made against the mock.
type MockTmuxPaste struct {
	Target  string
	Content string
	Enter   bool
}

type mockTmuxPaneState struct {
	session string
	pane    tmux.Pane
	output  string
	pastes  []MockTmuxPaste
}

// MockTmuxClient is a deterministic in-memory tmux substitute for executor tests.
type MockTmuxClient struct {
	mu    sync.Mutex
	panes map[string]*mockTmuxPaneState
}

// NewMockTmuxClient creates a mock with optional global pane fixtures.
func NewMockTmuxClient(panes ...tmux.Pane) *MockTmuxClient {
	m := &MockTmuxClient{panes: make(map[string]*mockTmuxPaneState)}
	for _, pane := range panes {
		m.AddPane("", pane)
	}
	return m
}

// AddPane registers a pane fixture. An empty session makes the pane visible
// to every GetPanes call, which keeps single-session tests lightweight.
func (m *MockTmuxClient) AddPane(session string, pane tmux.Pane) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensure()
	pane = normalizeMockPane(pane, len(m.panes)+1)
	m.panes[pane.ID] = &mockTmuxPaneState{session: session, pane: pane}
}

// Reset clears captured output and paste history while preserving pane fixtures.
func (m *MockTmuxClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range m.panes {
		state.output = ""
		state.pastes = nil
	}
}

// SetPaneOutput replaces a pane's captured output buffer.
func (m *MockTmuxClient) SetPaneOutput(target, output string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.panes[target]
	if !ok {
		return fmt.Errorf("mock tmux pane %q not found", target)
	}
	state.output = output
	return nil
}

// AppendPaneOutput appends content to a pane's captured output buffer.
func (m *MockTmuxClient) AppendPaneOutput(target, output string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.panes[target]
	if !ok {
		return fmt.Errorf("mock tmux pane %q not found", target)
	}
	state.output += output
	return nil
}

// PasteHistory returns a copy of the recorded PasteKeys calls.
func (m *MockTmuxClient) PasteHistory(target string) ([]MockTmuxPaste, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.panes[target]
	if !ok {
		return nil, fmt.Errorf("mock tmux pane %q not found", target)
	}
	history := make([]MockTmuxPaste, len(state.pastes))
	copy(history, state.pastes)
	return history, nil
}

// GetPanes returns the configured panes for a session.
func (m *MockTmuxClient) GetPanes(session string) ([]tmux.Pane, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensure()

	panes := make([]tmux.Pane, 0, len(m.panes))
	for _, state := range m.panes {
		if state.session == "" || state.session == session {
			panes = append(panes, state.pane)
		}
	}
	sort.Slice(panes, func(i, j int) bool {
		if panes[i].Index == panes[j].Index {
			return panes[i].ID < panes[j].ID
		}
		return panes[i].Index < panes[j].Index
	})
	return panes, nil
}

// PasteKeys records the prompt and appends it to the target pane's output.
func (m *MockTmuxClient) PasteKeys(target, content string, enter bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.panes[target]
	if !ok {
		return fmt.Errorf("mock tmux pane %q not found", target)
	}
	state.pastes = append(state.pastes, MockTmuxPaste{
		Target:  target,
		Content: content,
		Enter:   enter,
	})
	state.output += content
	if enter {
		state.output += "\n"
	}
	return nil
}

// CapturePaneOutput returns the target pane's output, optionally tailed by line count.
func (m *MockTmuxClient) CapturePaneOutput(target string, lines int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.panes[target]
	if !ok {
		return "", fmt.Errorf("mock tmux pane %q not found", target)
	}
	return tailMockLines(state.output, lines), nil
}

func (m *MockTmuxClient) ensure() {
	if m.panes == nil {
		m.panes = make(map[string]*mockTmuxPaneState)
	}
}

func normalizeMockPane(pane tmux.Pane, ordinal int) tmux.Pane {
	if pane.ID == "" {
		pane.ID = fmt.Sprintf("%%%d", ordinal)
	}
	if pane.Index == 0 {
		pane.Index = ordinal
	}
	return pane
}

func tailMockLines(output string, lines int) string {
	if lines <= 0 || output == "" {
		return output
	}
	parts := strings.SplitAfter(output, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if lines >= len(parts) {
		return output
	}
	return strings.Join(parts[len(parts)-lines:], "")
}
