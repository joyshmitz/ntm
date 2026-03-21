# Dashboard Resize Test
# Tests tier transitions when terminal is resized

Output testdata/screenshots/dashboard-resize.png

Require "./ntm"

Set FontSize 14
Set Shell "bash"
Set Theme "Catppuccin Mocha"

# Start at mega width (340 cols)
Set Width 340
Set Height 60

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-resize 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-resize 'sleep 120'"
Enter
Type "tmux split-window -h -t ntm-vhs-resize 'sleep 120'"
Enter
Type "tmux split-window -v -t ntm-vhs-resize:0.0 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-resize:0.0 -T 'ntm-vhs-resize__cc_1'"
Enter
Type "tmux select-pane -t ntm-vhs-resize:0.1 -T 'ntm-vhs-resize__cod_1'"
Enter
Type "tmux select-pane -t ntm-vhs-resize:0.2 -T 'ntm-vhs-resize__gmi_1'"
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm dashboard ntm-vhs-resize --debug"
Enter
Sleep 2s

Screenshot testdata/screenshots/dashboard-resize-mega.png

# Resize to ultra width (250 cols)
Set Width 250
Sleep 1s
Screenshot testdata/screenshots/dashboard-resize-ultra.png

# Resize to wide width (210 cols)
Set Width 210
Sleep 1s
Screenshot testdata/screenshots/dashboard-resize-wide.png

# Resize to split width (140 cols)
Set Width 140
Sleep 1s
Screenshot testdata/screenshots/dashboard-resize-split.png

# Resize to narrow width (100 cols)
Set Width 100
Sleep 1s
Screenshot testdata/screenshots/dashboard-resize-narrow.png

# Resize back to mega (test hysteresis - should not flicker)
Set Width 340
Sleep 1s
Screenshot testdata/screenshots/dashboard-resize.png

Type "q"
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-resize 2>/dev/null || true"
Enter
