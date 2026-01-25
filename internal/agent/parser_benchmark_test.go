package agent

import (
	"strings"
	"testing"
)

func BenchmarkParser_Parse(b *testing.B) {
	p := NewParser()
	output := `Opus 4.5 · Claude Max · Personal

I'll help you implement this feature. Let me create the file structure first.

Writing to internal/handler/user.go

` + strings.Repeat("some code here\n", 100) + `

Now let me add the tests for this handler...`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Parse(output)
	}
}

func BenchmarkParser_DetectAgentType(b *testing.B) {
	p := NewParser()
	output := `Opus 4.5 · Claude Max · Personal`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.DetectAgentType(output)
	}
}
