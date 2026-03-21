package dashboard

import (
	"os"
	"sync"
	"testing"
)

var attentionFeedTestMu sync.Mutex

func lockAttentionFeedForTest(t *testing.T) {
	attentionFeedTestMu.Lock()
	t.Cleanup(attentionFeedTestMu.Unlock)
}

func TestMain(m *testing.M) {
	// Many dashboard rendering helpers assume colors are enabled.
	// The repo's default environment sets NO_COLOR=1, so force colors on
	// for this package's tests to keep expectations stable.
	_ = os.Setenv("NTM_NO_COLOR", "0")
	_ = os.Setenv("NTM_THEME", "mocha")

	os.Exit(m.Run())
}
