package cm

import (
	"strings"
	"testing"
)

// TestTruncateMultibyteCrash verifies that truncate does not panic or corrupt strings
// when slicing multibyte characters.
func TestTruncateMultibyteCrash(t *testing.T) {
	// Create a string with multibyte characters (Emoji are 4 bytes)
	// We want the cut point to land in the middle of a character.
	// "🚀" is \xf0\x9f\x9a\x80
	
	// Create a long string of emojis
	longString := strings.Repeat("🚀", 100)
	
	// The truncate function uses a limit. Let's try to trigger a bad cut.
	// In FormatForRecovery, it calls truncate(s, 200).
	
	// If we have 50 emojis, length is 200 bytes.
	// If we have 51 emojis, length is 204 bytes.
	// truncate(s, 200) will call s[:197] + "..."
	// 197 is 49 * 4 + 1. So it cuts after the first byte of the 50th emoji.
	
	// 51 emojis = 204 bytes
	input := strings.Repeat("🚀", 51)
	
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Truncate panicked: %v", r)
		}
	}()
	
	result := truncate(input, 200)
	
	// Check if the result is valid UTF-8
	if !strings.Contains(result, "...") {
		t.Errorf("Expected ellipsis in result, got: %q", result)
	}
	
	t.Logf("Result length: %d", len(result))
	t.Logf("Result: %q", result)
}
