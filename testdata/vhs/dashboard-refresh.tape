# Dashboard Refresh Test
# Tests that the ticker updates and refresh works

Output testdata/screenshots/dashboard-refresh.png

Require "./ntm"

Set FontSize 14
Set Width 340
Set Height 60
Set Shell "bash"
Set Theme "Catppuccin Mocha"

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-refresh 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-refresh 'sleep 120'"
Enter
Type "tmux split-window -h -t ntm-vhs-refresh 'sleep 120'"
Enter
Type "tmux split-window -v -t ntm-vhs-refresh:0.0 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-refresh:0.0 -T 'ntm-vhs-refresh__cc_1'"
Enter
Type "tmux select-pane -t ntm-vhs-refresh:0.1 -T 'ntm-vhs-refresh__cod_1'"
Enter
Type "tmux select-pane -t ntm-vhs-refresh:0.2 -T 'ntm-vhs-refresh__gmi_1'"
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm dashboard ntm-vhs-refresh --debug"
Enter
Sleep 2s

# Initial state
Screenshot testdata/screenshots/dashboard-refresh-t0.png

# Wait for ticker to update (default 5s interval)
Sleep 6s
Screenshot testdata/screenshots/dashboard-refresh-t1.png

# Force refresh with 'r' key
Type "r"
Sleep 1s
Screenshot testdata/screenshots/dashboard-refresh-manual.png

# Final screenshot
Screenshot testdata/screenshots/dashboard-refresh.png

Type "q"
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-refresh 2>/dev/null || true"
Enter
