package robot

import "testing"

func TestCapitalize(t *testing.T) {

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase start", "hello", "Hello"},
		{"already uppercase", "Hello", "Hello"},
		{"single char", "a", "A"},
		{"single uppercase", "A", "A"},
		{"empty string", "", ""},
		{"digit start", "123abc", "123abc"},
		{"unicode start", "über", "über"},
		{"all caps", "ABC", "ABC"},
		{"space start", " hello", " hello"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := capitalize(tc.input)
			if got != tc.want {
				t.Errorf("capitalize(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
