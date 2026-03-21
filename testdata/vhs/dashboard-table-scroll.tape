# Dashboard Table Scroll Test
# Tests keyboard navigation in table panels (history, etc.)

Output testdata/screenshots/dashboard-table-scroll.png

Require "./ntm"

Set FontSize 14
Set Width 200
Set Height 50
Set Shell "bash"
Set Theme "Catppuccin Mocha"

Hide
Type "cd /data/projects/ntm"
Enter
Type "tmux kill-session -t ntm-vhs-table 2>/dev/null || true"
Enter
Type "tmux new-session -d -s ntm-vhs-table 'sleep 120'"
Enter
Type "tmux split-window -h -t ntm-vhs-table 'sleep 120'"
Enter
Type "tmux split-window -v -t ntm-vhs-table:0.0 'sleep 120'"
Enter
Type "tmux select-pane -t ntm-vhs-table:0.0 -T 'ntm-vhs-table__cc_1'"
Enter
Type "tmux select-pane -t ntm-vhs-table:0.1 -T 'ntm-vhs-table__cod_1'"
Enter
Type "tmux select-pane -t ntm-vhs-table:0.2 -T 'ntm-vhs-table__gmi_1'"
Enter
Type "mkdir -p \"$HOME/.ntm/sessions/ntm-vhs-table\""
Enter
Type "printf '%s' '{\"session\":\"ntm-vhs-table\",\"prompts\":[{\"id\":\"1\",\"timestamp\":\"2026-03-21T00:00:01Z\",\"session\":\"ntm-vhs-table\",\"content\":\"status alpha\",\"targets\":[\"all\"],\"source\":\"cli\"},{\"id\":\"2\",\"timestamp\":\"2026-03-21T00:00:02Z\",\"session\":\"ntm-vhs-table\",\"content\":\"status beta\",\"targets\":[\"all\"],\"source\":\"cli\"},{\"id\":\"3\",\"timestamp\":\"2026-03-21T00:00:03Z\",\"session\":\"ntm-vhs-table\",\"content\":\"status gamma\",\"targets\":[\"all\"],\"source\":\"cli\"},{\"id\":\"4\",\"timestamp\":\"2026-03-21T00:00:04Z\",\"session\":\"ntm-vhs-table\",\"content\":\"status delta\",\"targets\":[\"all\"],\"source\":\"cli\"},{\"id\":\"5\",\"timestamp\":\"2026-03-21T00:00:05Z\",\"session\":\"ntm-vhs-table\",\"content\":\"status epsilon\",\"targets\":[\"all\"],\"source\":\"cli\"},{\"id\":\"6\",\"timestamp\":\"2026-03-21T00:00:06Z\",\"session\":\"ntm-vhs-table\",\"content\":\"status zeta\",\"targets\":[\"all\"],\"source\":\"cli\"},{\"id\":\"7\",\"timestamp\":\"2026-03-21T00:00:07Z\",\"session\":\"ntm-vhs-table\",\"content\":\"status eta\",\"targets\":[\"all\"],\"source\":\"cli\"},{\"id\":\"8\",\"timestamp\":\"2026-03-21T00:00:08Z\",\"session\":\"ntm-vhs-table\",\"content\":\"status theta\",\"targets\":[\"all\"],\"source\":\"cli\"}],\"updated_at\":\"2026-03-21T00:00:09Z\"}' > \"$HOME/.ntm/sessions/ntm-vhs-table/prompts.json\""
Enter
Show

Type "NTM_ANIMATIONS=1 NTM_ICONS=nerd ./ntm dashboard ntm-vhs-table --debug"
Enter
Sleep 2s

# Screenshot initial state
Screenshot testdata/screenshots/dashboard-table-scroll-initial.png

# Tab to reach history/table panel
Tab
Sleep 200ms
Tab
Sleep 200ms
Tab
Sleep 200ms
Tab
Sleep 200ms
Tab
Sleep 200ms
Screenshot testdata/screenshots/dashboard-table-scroll-focused.png

# Navigate down in table
Down
Sleep 200ms
Screenshot testdata/screenshots/dashboard-table-scroll-down1.png

Down
Sleep 200ms
Down
Sleep 200ms
Screenshot testdata/screenshots/dashboard-table-scroll-down3.png

# Page down
PageDown
Sleep 300ms
Screenshot testdata/screenshots/dashboard-table-scroll-pagedown.png

# Navigate up
Up
Sleep 200ms
Screenshot testdata/screenshots/dashboard-table-scroll-up.png

# Page up
PageUp
Sleep 300ms
Screenshot testdata/screenshots/dashboard-table-scroll-pageup.png

# Final screenshot
Screenshot testdata/screenshots/dashboard-table-scroll.png

# Quit
Type "q"
Sleep 500ms

Hide
Type "tmux kill-session -t ntm-vhs-table 2>/dev/null || true"
Enter
