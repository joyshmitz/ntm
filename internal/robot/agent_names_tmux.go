package robot

import (
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// tmuxIsInstalledReal delegates to the real tmux.IsInstalled.
func tmuxIsInstalledReal() bool {
	return tmux.IsInstalled()
}

// tmuxSessionExistsReal delegates to the real tmux.SessionExists.
func tmuxSessionExistsReal(name string) bool {
	return tmux.SessionExists(name)
}

// tmuxGetPanesReal delegates to the real tmux.GetPanes and converts to tmuxPaneInfo.
func tmuxGetPanesReal(session string) []tmuxPaneInfo {
	panes, err := tmux.GetPanes(session)
	if err != nil {
		return nil
	}
	result := make([]tmuxPaneInfo, len(panes))
	for i, p := range panes {
		result[i] = tmuxPaneInfo{Index: p.Index, Title: p.Title, Type: p.Type}
	}
	return result
}
