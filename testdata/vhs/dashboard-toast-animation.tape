# Dashboard Toast Animation Test
# Tests that toast notifications appear, animate, and auto-dismiss

Output testdata/screenshots/dashboard-toast-animation.png

Require "./ntm"

Set FontSize 14
Set Width 200
Set Height 50
Set Shell "bash"
Set Theme "Catppuccin Mocha"

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-toast 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-toast 'sleep 120'"
Enter
Type "tmux split-window -h -t ntm-vhs-toast 'sleep 120'"
Enter
Type "tmux split-window -v -t ntm-vhs-toast:0.0 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-toast:0.0 -T 'ntm-vhs-toast__cc_1'"
Enter
Type "tmux select-pane -t ntm-vhs-toast:0.1 -T 'ntm-vhs-toast__cod_1'"
Enter
Type "tmux select-pane -t ntm-vhs-toast:0.2 -T 'ntm-vhs-toast__gmi_1'"
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm dashboard ntm-vhs-toast --debug"
Enter
Sleep 2s

# Screenshot initial state (no toasts)
Screenshot testdata/screenshots/dashboard-toast-animation-initial.png

# Focus on the toast region and auto-dismiss timing.
Sleep 1s

# Wait for any startup toasts to animate
Sleep 500ms
Screenshot testdata/screenshots/dashboard-toast-animation-animating.png

# Wait for auto-dismiss (toasts typically dismiss after 3-5s)
Sleep 4s
Screenshot testdata/screenshots/dashboard-toast-animation-dismissed.png

# Final screenshot
Screenshot testdata/screenshots/dashboard-toast-animation.png

# Quit
Type "q"
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-toast 2>/dev/null || true"
Enter
