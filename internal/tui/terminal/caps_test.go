package terminal

import (
	"sync"
	"testing"
)

func TestCapabilityCacheConcurrentAccess(t *testing.T) {
	ResetCache()
	t.Cleanup(ResetCache)
	want := Detect()
	ResetCache()

	const (
		readers    = 32
		iterations = 200
	)

	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(readers + 1)

	for range readers {
		go func() {
			defer workers.Done()
			<-start
			for range iterations {
				if got := Detect(); got != want {
					t.Errorf("Detect() = %+v, want %+v", got, want)
				}
				if got := SupportsTrueColor(); got != want.TrueColor {
					t.Errorf("SupportsTrueColor() = %t, want %t", got, want.TrueColor)
				}
				if got := SupportsUnicodeBlocks(); got != want.UnicodeBlocks {
					t.Errorf("SupportsUnicodeBlocks() = %t, want %t", got, want.UnicodeBlocks)
				}
			}
		}()
	}

	go func() {
		defer workers.Done()
		<-start
		for range iterations {
			ResetCache()
		}
	}()

	close(start)
	workers.Wait()
}
