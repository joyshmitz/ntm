package robot

import "testing"

// =============================================================================
// contextUsageForPane tests
// =============================================================================

func TestContextUsageForPane(t *testing.T) {

	tests := []struct {
		name      string
		usage     map[int]float64
		paneIndex int
		want      float64
	}{
		{"nil map", nil, 0, 0},
		{"empty map", map[int]float64{}, 1, 0},
		{"key exists", map[int]float64{1: 45.5, 2: 80.0}, 1, 45.5},
		{"key missing", map[int]float64{1: 45.5}, 3, 0},
		{"zero value exists", map[int]float64{0: 0.0}, 0, 0},
		{"negative index", map[int]float64{-1: 10.0}, -1, 10.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := contextUsageForPane(tc.usage, tc.paneIndex)
			if got != tc.want {
				t.Errorf("contextUsageForPane(_, %d) = %f, want %f", tc.paneIndex, got, tc.want)
			}
		})
	}
}
