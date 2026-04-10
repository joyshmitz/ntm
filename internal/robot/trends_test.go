package robot

import (
	"sync"
	"testing"
	"time"
)

// Tests in this file supplement the TrendTracker tests in monitor_test.go.
// They cover concurrency, nil-value handling, and edge cases not covered there.

func TestTrendTracker_SeparatePaneTracking(t *testing.T) {

	tt := NewTrendTracker(5)
	now := time.Now()

	tt.AddSample(1, TrendSample{Timestamp: now, ContextRemaining: floatPtr(80.0)})
	tt.AddSample(2, TrendSample{Timestamp: now, ContextRemaining: floatPtr(90.0)})
	tt.AddSample(3, TrendSample{Timestamp: now, ContextRemaining: floatPtr(50.0)})

	if tt.GetSampleCount(1) != 1 {
		t.Errorf("pane 1 count = %d, want 1", tt.GetSampleCount(1))
	}
	if tt.GetSampleCount(2) != 1 {
		t.Errorf("pane 2 count = %d, want 1", tt.GetSampleCount(2))
	}
	if tt.GetSampleCount(3) != 1 {
		t.Errorf("pane 3 count = %d, want 1", tt.GetSampleCount(3))
	}
	if tt.GetSampleCount(99) != 0 {
		t.Errorf("pane 99 count = %d, want 0", tt.GetSampleCount(99))
	}
}

func TestTrendTracker_NilContextValues(t *testing.T) {

	tt := NewTrendTracker(5)
	now := time.Now()

	// All nil context values should produce unknown trend even with multiple samples
	tt.AddSample(1, TrendSample{Timestamp: now})
	tt.AddSample(1, TrendSample{Timestamp: now.Add(time.Minute)})
	tt.AddSample(1, TrendSample{Timestamp: now.Add(2 * time.Minute)})

	trend, count := tt.GetTrend(1)
	if trend != TrendUnknown {
		t.Errorf("nil-values trend = %s, want %s", trend, TrendUnknown)
	}
	if count != 3 {
		t.Errorf("nil-values count = %d, want 3", count)
	}
}

func TestTrendTracker_MixedNilAndValues(t *testing.T) {

	tt := NewTrendTracker(5)
	now := time.Now()

	// Mix of nil and non-nil values: only consecutive non-nil pairs count as deltas
	tt.AddSample(1, TrendSample{Timestamp: now, ContextRemaining: floatPtr(80.0)})
	tt.AddSample(1, TrendSample{Timestamp: now.Add(time.Minute)}) // nil
	tt.AddSample(1, TrendSample{Timestamp: now.Add(2 * time.Minute), ContextRemaining: floatPtr(60.0)})

	trend, count := tt.GetTrend(1)
	// No consecutive non-nil pairs, so unknown
	if trend != TrendUnknown {
		t.Errorf("mixed nil trend = %s, want %s", trend, TrendUnknown)
	}
	if count != 3 {
		t.Errorf("mixed nil count = %d, want 3", count)
	}
}

func TestTrendTracker_ConcurrentAccess(t *testing.T) {

	tt := NewTrendTracker(100)
	var wg sync.WaitGroup
	now := time.Now()

	// Concurrent writes and reads across multiple panes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(pane int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				tt.AddSample(pane, TrendSample{
					Timestamp:        now.Add(time.Duration(j) * time.Second),
					ContextRemaining: floatPtr(80.0 - float64(j)),
				})
				tt.GetTrend(pane)
				tt.GetSampleCount(pane)
				tt.GetLastSample(pane)
				tt.GetTrendInfo(pane)
			}
		}(i)
	}

	wg.Wait()

	for i := 0; i < 10; i++ {
		count := tt.GetSampleCount(i)
		if count != 20 {
			t.Errorf("pane %d count = %d, want 20", i, count)
		}
	}
}

func TestTrendTypeConstants(t *testing.T) {

	expected := map[TrendType]string{
		TrendDeclining: "declining",
		TrendStable:    "stable",
		TrendRising:    "rising",
		TrendUnknown:   "unknown",
	}

	for tt, s := range expected {
		if string(tt) != s {
			t.Errorf("TrendType %s != %q", tt, s)
		}
	}
}

func TestClassifyTrend_Boundaries(t *testing.T) {

	// Test exact boundary values
	tests := []struct {
		name     string
		delta    float64
		expected TrendType
	}{
		{"exact -2.0 is stable", -2.0, TrendStable},
		{"exact +2.0 is stable", 2.0, TrendStable},
		{"just below -2.0", -2.001, TrendDeclining},
		{"just above +2.0", 2.001, TrendRising},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := classifyTrend(tc.delta)
			if result != tc.expected {
				t.Errorf("classifyTrend(%f) = %s, want %s", tc.delta, result, tc.expected)
			}
		})
	}
}

func TestTrendInfo_EmptyPaneFields(t *testing.T) {

	tt := NewTrendTracker(5)

	info := tt.GetTrendInfo(999)
	if info.Trend != TrendUnknown {
		t.Errorf("empty pane trend = %s, want %s", info.Trend, TrendUnknown)
	}
	if info.SampleCount != 0 {
		t.Errorf("empty pane sample count = %d, want 0", info.SampleCount)
	}
	if info.LastValue != nil {
		t.Errorf("empty pane last value = %v, want nil", info.LastValue)
	}
	if !info.LastUpdate.IsZero() {
		t.Errorf("empty pane last update should be zero time")
	}
	if info.AvgDelta != 0 {
		t.Errorf("empty pane avg delta = %f, want 0", info.AvgDelta)
	}
}
