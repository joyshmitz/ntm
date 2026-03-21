# Dashboard Basic Rendering Test
# Tests that the dashboard renders correctly at startup

Output testdata/screenshots/dashboard-basic.png

Require "./ntm"

Set FontSize 14
Set Width 340
Set Height 60
Set Shell "bash"
Set Theme "Catppuccin Mocha"

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-basic 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-basic 'sleep 120'"
Enter
Type "tmux split-window -h -t ntm-vhs-basic 'sleep 120'"
Enter
Type "tmux split-window -v -t ntm-vhs-basic:0.0 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-basic:0.0 -T 'ntm-vhs-basic__cc_1'"
Enter
Type "tmux select-pane -t ntm-vhs-basic:0.1 -T 'ntm-vhs-basic__cod_1'"
Enter
Type "tmux select-pane -t ntm-vhs-basic:0.2 -T 'ntm-vhs-basic__gmi_1'"
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm dashboard ntm-vhs-basic --debug"
Enter
Sleep 2s

# Take screenshot of initial render
Screenshot testdata/screenshots/dashboard-basic-initial.png

# Wait for any animations to settle
Sleep 500ms

# Final screenshot
Screenshot testdata/screenshots/dashboard-basic.png

Type "q"
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-basic 2>/dev/null || true"
Enter
