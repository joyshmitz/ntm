# Dashboard Fuzzy Filter Test
# Tests the "/" key filter mode for pane filtering

Output testdata/screenshots/dashboard-fuzzy-filter.png

Require "./ntm"

Set FontSize 14
Set Width 200
Set Height 50
Set Shell "bash"
Set Theme "Catppuccin Mocha"

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-filter 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-filter 'sleep 120'"
Enter
Type "tmux split-window -h -t ntm-vhs-filter 'sleep 120'"
Enter
Type "tmux split-window -v -t ntm-vhs-filter:0.0 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-filter:0.0 -T 'ntm-vhs-filter__cc_1'"
Enter
Type "tmux select-pane -t ntm-vhs-filter:0.1 -T 'ntm-vhs-filter__cod_1'"
Enter
Type "tmux select-pane -t ntm-vhs-filter:0.2 -T 'ntm-vhs-filter__gmi_1'"
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm dashboard ntm-vhs-filter --debug"
Enter
Sleep 2s

# Screenshot initial state
Screenshot testdata/screenshots/dashboard-fuzzy-filter-initial.png

# Enter filter mode with /
Type "/"
Sleep 300ms
Screenshot testdata/screenshots/dashboard-fuzzy-filter-mode.png

# Type filter text
Type "cc"
Sleep 300ms
Screenshot testdata/screenshots/dashboard-fuzzy-filter-results.png

# Clear filter with Escape
Escape
Sleep 300ms
Screenshot testdata/screenshots/dashboard-fuzzy-filter-cleared.png

# Final screenshot
Screenshot testdata/screenshots/dashboard-fuzzy-filter.png

# Quit
Type "q"
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-filter 2>/dev/null || true"
Enter
