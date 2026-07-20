package checkpoint

import (
	"fmt"
	"os"
	"testing"

	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// TestMain isolates every real-tmux checkpoint test behind a private tmux
// server (private TMUX_TMPDIR socket root) so capture/restore suites can never
// touch sessions on the developer's default tmux server (#220).
func TestMain(m *testing.M) {
	cleanupTmux, err := testutil.IsolateTmuxTestProcess()
	if err != nil {
		fmt.Fprintf(os.Stderr, "isolate checkpoint tmux tests: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := cleanupTmux(); err != nil {
		fmt.Fprintf(os.Stderr, "clean up isolated checkpoint tmux: %v\n", err)
		code = 1
	}

	os.Exit(code)
}
