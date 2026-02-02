package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no user home directory available")
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "tilde only", in: "~", want: home},
		{name: "tilde slash", in: "~/", want: home},
		{name: "tilde path", in: "~/.ntm/archive", want: filepath.Join(home, filepath.FromSlash(".ntm/archive"))},
		{name: "tilde backslash path", in: `~\foo`, want: filepath.Join(home, "foo")},
		{name: "no expansion absolute", in: "/tmp/ntm", want: "/tmp/ntm"},
		{name: "no expansion tilde user", in: "~someone/.ntm", want: "~someone/.ntm"},
		{name: "no expansion relative", in: ".ntm/archive", want: ".ntm/archive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandPath(tt.in)
			if got != tt.want {
				t.Fatalf("ExpandPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
