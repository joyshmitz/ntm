package process

import (
	"os"
	"testing"
)

func TestIsAlive_CurrentProcess(t *testing.T) {
	t.Parallel()
	pid := os.Getpid()
	if !IsAlive(pid) {
		t.Errorf("IsAlive(%d) = false, want true for current process", pid)
	}
}

func TestIsAlive_InvalidPID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pid  int
	}{
		{"zero", 0},
		{"negative", -1},
		{"very large", 999999999},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if IsAlive(tt.pid) {
				t.Errorf("IsAlive(%d) = true, want false", tt.pid)
			}
		})
	}
}

func TestGetChildPID_InvalidParent(t *testing.T) {
	t.Parallel()
	if pid := GetChildPID(0); pid != 0 {
		t.Errorf("GetChildPID(0) = %d, want 0", pid)
	}
	if pid := GetChildPID(-1); pid != 0 {
		t.Errorf("GetChildPID(-1) = %d, want 0", pid)
	}
}

func TestHasChildAlive_InvalidPID(t *testing.T) {
	t.Parallel()
	if HasChildAlive(0) {
		t.Error("HasChildAlive(0) = true, want false")
	}
	if HasChildAlive(-1) {
		t.Error("HasChildAlive(-1) = true, want false")
	}
}
