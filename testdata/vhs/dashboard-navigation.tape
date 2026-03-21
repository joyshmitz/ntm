# Dashboard Navigation Test
# Tests keyboard navigation between panels

Output testdata/screenshots/dashboard-navigation.png

Require "./ntm"

Set FontSize 14
Set Width 340
Set Height 60
Set Shell "bash"
Set Theme "Catppuccin Mocha"

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-nav 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-nav 'sleep 120'"
Enter
Type "tmux split-window -h -t ntm-vhs-nav 'sleep 120'"
Enter
Type "tmux split-window -v -t ntm-vhs-nav:0.0 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-nav:0.0 -T 'ntm-vhs-nav__cc_1'"
Enter
Type "tmux select-pane -t ntm-vhs-nav:0.1 -T 'ntm-vhs-nav__cod_1'"
Enter
Type "tmux select-pane -t ntm-vhs-nav:0.2 -T 'ntm-vhs-nav__gmi_1'"
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm dashboard ntm-vhs-nav --debug"
Enter
Sleep 2s

# Initial state - pane list focused
Screenshot testdata/screenshots/dashboard-nav-initial.png

# Tab to next panel
Tab
Sleep 500ms
Screenshot testdata/screenshots/dashboard-nav-tab1.png

# Tab again
Tab
Sleep 500ms
Screenshot testdata/screenshots/dashboard-nav-tab2.png

# Shift+Tab back
Shift+Tab
Sleep 500ms
Screenshot testdata/screenshots/dashboard-nav-back.png

# Arrow key navigation within panel
Down
Sleep 300ms
Down
Sleep 300ms
Screenshot testdata/screenshots/dashboard-nav-arrows.png

# Final screenshot
Screenshot testdata/screenshots/dashboard-navigation.png

Type "q"
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-nav 2>/dev/null || true"
Enter
