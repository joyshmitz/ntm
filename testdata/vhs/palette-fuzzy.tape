# Palette Fuzzy Search Test
# Tests the command palette fuzzy filtering

Output testdata/screenshots/palette-fuzzy.png

Require "./ntm"

Set FontSize 14
Set Width 120
Set Height 40
Set Shell "bash"
Set Theme "Catppuccin Mocha"

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-palette 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-palette 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-palette:0.0 -T 'ntm-vhs-palette__cc_1'"
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm palette ntm-vhs-palette"
Enter
Sleep 2s

# Screenshot initial palette
Screenshot testdata/screenshots/palette-fuzzy-initial.png

# Type partial command to filter
Type "sta"
Sleep 300ms
Screenshot testdata/screenshots/palette-fuzzy-sta.png

# More specific
Type "tus"
Sleep 300ms
Screenshot testdata/screenshots/palette-fuzzy-status.png

# Clear and try another
Ctrl+u
Sleep 200ms
Type "spawn"
Sleep 300ms
Screenshot testdata/screenshots/palette-fuzzy-spawn.png

# Navigate selection
Down
Sleep 200ms
Screenshot testdata/screenshots/palette-fuzzy-navigate.png

# Final screenshot
Screenshot testdata/screenshots/palette-fuzzy.png

# Escape to cancel
Escape
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-palette 2>/dev/null || true"
Enter
