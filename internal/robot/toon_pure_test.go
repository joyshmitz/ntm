package robot

import "testing"

// =============================================================================
// toonIsIdentifierStart tests
// =============================================================================

func TestToonIsIdentifierStart(t *testing.T) {

	tests := []struct {
		name string
		c    rune
		want bool
	}{
		{"lowercase a", 'a', true},
		{"lowercase z", 'z', true},
		{"uppercase A", 'A', true},
		{"uppercase Z", 'Z', true},
		{"underscore", '_', true},
		{"digit 0", '0', false},
		{"digit 9", '9', false},
		{"space", ' ', false},
		{"hyphen", '-', false},
		{"dot", '.', false},
		{"at sign", '@', false},
		{"unicode letter", 'é', false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toonIsIdentifierStart(tc.c)
			if got != tc.want {
				t.Errorf("toonIsIdentifierStart(%q) = %v, want %v", tc.c, got, tc.want)
			}
		})
	}
}

// =============================================================================
// toonIsIdentifierChar tests
// =============================================================================

func TestToonIsIdentifierChar(t *testing.T) {

	tests := []struct {
		name string
		c    rune
		want bool
	}{
		{"lowercase a", 'a', true},
		{"uppercase Z", 'Z', true},
		{"underscore", '_', true},
		{"digit 0", '0', true},
		{"digit 9", '9', true},
		{"space", ' ', false},
		{"hyphen", '-', false},
		{"dot", '.', false},
		{"at sign", '@', false},
		{"unicode letter", 'ü', false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toonIsIdentifierChar(tc.c)
			if got != tc.want {
				t.Errorf("toonIsIdentifierChar(%q) = %v, want %v", tc.c, got, tc.want)
			}
		})
	}
}
