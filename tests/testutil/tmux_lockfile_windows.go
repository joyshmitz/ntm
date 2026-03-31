//go:build windows

package testutil

func withGlobalTmuxTestLock(fn func()) {
	fn()
}

func tryWithGlobalTmuxTestLock(fn func()) bool {
	fn()
	return true
}
