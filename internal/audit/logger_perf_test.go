package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkLoadLastHash(b *testing.B) {
	// Setup: Create a large audit log file
	tempDir, err := os.MkdirTemp("", "audit_perf")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Mock home directory
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", oldHome)

	// Create logger to generate data
	config := &LoggerConfig{
		SessionID:     "bench-session",
		BufferSize:    1000,
		FlushInterval: 1 * time.Second,
	}

	logger, err := NewAuditLogger(config)
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}

	// Generate 100,000 entries (should be > 20MB)
	entryCount := 100000
	for i := 0; i < entryCount; i++ {
		entry := AuditEntry{
			EventType: EventTypeCommand,
			Actor:     ActorUser,
			Target:    "bench-target",
			Payload:   map[string]interface{}{"data": "some payload data to increase size", "index": i},
		}
		logger.Log(entry)
	}
	logger.Close()

	// Get file path
	auditDir := filepath.Join(tempDir, ".local", "share", "ntm", "audit")
	files, _ := os.ReadDir(auditDir)
	logPath := filepath.Join(auditDir, files[0].Name())

	fileInfo, _ := os.Stat(logPath)
	b.Logf("Generated log file size: %.2f MB", float64(fileInfo.Size())/1024/1024)

	// Benchmark loadLastHash (via NewAuditLogger)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l, err := NewAuditLogger(config)
		if err != nil {
			b.Fatalf("Failed to create logger: %v", err)
		}
		l.Close()
	}
}
