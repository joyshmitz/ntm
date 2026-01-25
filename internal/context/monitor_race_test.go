package context

import (
	"sync"
	"testing"
)

// TestMonitor_RaceConditions checks for race conditions in ContextMonitor.
func TestMonitor_RaceConditions(t *testing.T) {
	cfg := DefaultMonitorConfig()
	monitor := NewContextMonitor(cfg)
	agentID := "test_agent"
	monitor.RegisterAgent(agentID, "pane_1", "claude-3-opus")

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Concurrently record messages
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				monitor.RecordMessage(agentID, 10, 20)
			}
		}()
	}

	// Concurrently read estimates
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				_ = monitor.GetEstimate(agentID)
			}
		}()
	}

	// Concurrently update from robot mode
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			output := `{"context_used": 15000, "context_limit": 200000}`
			for j := 0; j < 50; j++ {
				monitor.UpdateFromRobotMode(agentID, output)
			}
		}()
	}

	close(start)
	wg.Wait()

	// Verify state consistency
	state := monitor.GetState(agentID)
	if state == nil {
		t.Fatal("Agent state not found")
	}

	// Expected message count: 10 goroutines * 100 iterations = 1000
	if state.MessageCount != 1000 {
		t.Errorf("Expected 1000 messages, got %d", state.MessageCount)
	}
}
