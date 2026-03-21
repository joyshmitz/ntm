# Dashboard Focus Ring Test
# Tests that Tab key cycles focus through all panels with visible focus indicator

Output testdata/screenshots/dashboard-focus-ring.png

Require "./ntm"

Set FontSize 14
Set Width 200
Set Height 50
Set Shell "bash"
Set Theme "Catppuccin Mocha"

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-focus 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-focus 'sleep 120'"
Enter
Type "tmux split-window -h -t ntm-vhs-focus 'sleep 120'"
Enter
Type "tmux split-window -v -t ntm-vhs-focus:0.0 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-focus:0.0 -T 'ntm-vhs-focus__cc_1'"
Enter
Type "tmux select-pane -t ntm-vhs-focus:0.1 -T 'ntm-vhs-focus__cod_1'"
Enter
Type "tmux select-pane -t ntm-vhs-focus:0.2 -T 'ntm-vhs-focus__gmi_1'"
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm dashboard ntm-vhs-focus --debug"
Enter
Sleep 2s

# Screenshot initial state (first panel focused)
Screenshot testdata/screenshots/dashboard-focus-ring-initial.png

# Tab through panels - each Tab moves focus to next panel
Tab
Sleep 300ms
Screenshot testdata/screenshots/dashboard-focus-ring-panel2.png

Tab
Sleep 300ms
Screenshot testdata/screenshots/dashboard-focus-ring-panel3.png

Tab
Sleep 300ms
Screenshot testdata/screenshots/dashboard-focus-ring-panel4.png

# Shift+Tab to go back
Shift+Tab
Sleep 300ms
Screenshot testdata/screenshots/dashboard-focus-ring-reverse.png

# Final screenshot
Screenshot testdata/screenshots/dashboard-focus-ring.png

# Quit
Type "q"
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-focus 2>/dev/null || true"
Enter
