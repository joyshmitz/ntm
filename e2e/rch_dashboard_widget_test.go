//go:build e2e
// +build e2e

package e2e

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tools"
	"github.com/Dicklesworthstone/ntm/internal/tui/dashboard"
	"github.com/Dicklesworthstone/ntm/internal/tui/dashboard/panels"
)

func TestRCHDashboardWidgetDisplay(t *testing.T) {
	model := dashboard.New("test", "")
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 180, Height: 50})
	model = updated.(dashboard.Model)

	data := panels.RCHPanelData{
		Loaded:    true,
		Enabled:   true,
		Available: true,
		Status: &tools.RCHStatus{
			Enabled: true,
			Workers: []tools.RCHWorker{
				{Name: "builder-1", Available: true, Healthy: true, Load: 10},
				{Name: "builder-2", Available: true, Healthy: true, Load: 90, Queue: 1, CurrentBuild: "cargo build --release"},
			},
			SessionStats: &tools.RCHSessionStats{
				BuildsTotal:      45,
				BuildsRemote:     30,
				BuildsLocal:      15,
				TimeSavedSeconds: 1200,
			},
		},
	}

	updated, _ = model.Update(dashboard.RCHStatusUpdateMsg{Data: data})
	model = updated.(dashboard.Model)

	view := status.StripANSI(model.View())
	for _, want := range []string{
		"RCH Build Offload",
		"Remote:",
		"Local",
		"Time saved",
		"builder-1",
		"builder-2",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected dashboard view to contain %q, got:\n%s", want, view)
		}
	}
}
