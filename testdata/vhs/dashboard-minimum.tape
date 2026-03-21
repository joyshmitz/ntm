# Dashboard Minimum Size Test
# Tests rendering at minimum terminal sizes

Output testdata/screenshots/dashboard-minimum.png

Require "./ntm"

Set FontSize 14
Set Shell "bash"
Set Theme "Catppuccin Mocha"

# Start at minimum standard terminal size 80x24
Set Width 80
Set Height 24

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-min 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-min 'sleep 120'"
Enter
Type "tmux split-window -h -t ntm-vhs-min 'sleep 120'"
Enter
Type "tmux split-window -v -t ntm-vhs-min:0.0 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-min:0.0 -T 'ntm-vhs-min__cc_1'"
Enter
Type "tmux select-pane -t ntm-vhs-min:0.1 -T 'ntm-vhs-min__cod_1'"
Enter
Type "tmux select-pane -t ntm-vhs-min:0.2 -T 'ntm-vhs-min__gmi_1'"
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm dashboard ntm-vhs-min --debug"
Enter
Sleep 2s

Screenshot testdata/screenshots/dashboard-minimum-80x24.png

# Even smaller (40x10 - extreme case)
Set Width 40
Set Height 10
Sleep 1s
Screenshot testdata/screenshots/dashboard-minimum-40x10.png

# Final screenshot at 80x24
Set Width 80
Set Height 24
Sleep 1s
Screenshot testdata/screenshots/dashboard-minimum.png

Type "q"
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-min 2>/dev/null || true"
Enter
