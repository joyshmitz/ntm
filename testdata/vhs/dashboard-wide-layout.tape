# Dashboard Wide Layout Test
# Tests that the dashboard layout adapts to ultra-wide terminals

Output testdata/screenshots/dashboard-wide-layout.png

Require "./ntm"

Set FontSize 12
Set Width 400
Set Height 50
Set Shell "bash"
Set Theme "Catppuccin Mocha"

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-wide 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-wide 'sleep 120'"
Enter
Type "tmux split-window -h -t ntm-vhs-wide 'sleep 120'"
Enter
Type "tmux split-window -v -t ntm-vhs-wide:0.0 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-wide:0.0 -T 'ntm-vhs-wide__cc_1'"
Enter
Type "tmux select-pane -t ntm-vhs-wide:0.1 -T 'ntm-vhs-wide__cod_1'"
Enter
Type "tmux select-pane -t ntm-vhs-wide:0.2 -T 'ntm-vhs-wide__gmi_1'"
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm dashboard ntm-vhs-wide --debug"
Enter
Sleep 2s

# Screenshot wide layout
Screenshot testdata/screenshots/dashboard-wide-layout-initial.png

# Wait for any animations to settle
Sleep 500ms
Screenshot testdata/screenshots/dashboard-wide-layout-settled.png

# Navigate to verify all panels are accessible
Tab
Sleep 200ms
Tab
Sleep 200ms
Tab
Sleep 200ms
Screenshot testdata/screenshots/dashboard-wide-layout-navigated.png

# Final screenshot
Screenshot testdata/screenshots/dashboard-wide-layout.png

# Quit
Type "q"
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-wide 2>/dev/null || true"
Enter
