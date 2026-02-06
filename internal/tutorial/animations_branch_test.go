package tutorial

import (
	"strings"
	"testing"
)

// TestNewParticle_AllTypes exercises every particle type branch in the constructor.
func TestNewParticle_AllTypes(t *testing.T) {
	t.Parallel()

	types := []struct {
		name  string
		ptype ParticleType
		char  string // expected char (or prefix for random chars)
	}{
		{"sparkle", ParticleSparkle, ""},
		{"star", ParticleStar, ""},
		{"confetti", ParticleConfetti, ""},
		{"firework", ParticleFirework, "●"},
		{"rain", ParticleRain, "│"},
		{"snow", ParticleSnow, ""},
		{"glow", ParticleGlow, "◉"},
	}

	for _, tt := range types {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewParticle(10, 20, tt.ptype)

			if p.X != 10.0 {
				t.Errorf("X = %f, want 10.0", p.X)
			}
			if p.Y != 20.0 {
				t.Errorf("Y = %f, want 20.0", p.Y)
			}
			if p.Type != tt.ptype {
				t.Errorf("Type = %v, want %v", p.Type, tt.ptype)
			}
			if p.Char == "" {
				t.Error("Char should be non-empty")
			}
			if tt.char != "" && p.Char != tt.char {
				t.Errorf("Char = %q, want %q", p.Char, tt.char)
			}
			if p.Life <= 0 {
				t.Error("Life should be positive")
			}
			if p.MaxLife <= 0 {
				t.Error("MaxLife should be positive")
			}
			if p.Color == "" {
				t.Error("Color should be non-empty")
			}
		})
	}
}

// TestNewParticle_SparkleZeroGravity verifies sparkle has zero gravity.
func TestNewParticle_SparkleZeroGravity(t *testing.T) {
	t.Parallel()

	p := NewParticle(0, 0, ParticleSparkle)
	if p.Gravity != 0 {
		t.Errorf("Sparkle gravity = %f, want 0", p.Gravity)
	}
}

// TestNewParticle_GlowZeroVelocity verifies glow has zero velocity.
func TestNewParticle_GlowZeroVelocity(t *testing.T) {
	t.Parallel()

	p := NewParticle(0, 0, ParticleGlow)
	if p.VX != 0 || p.VY != 0 {
		t.Errorf("Glow velocity = (%f, %f), want (0, 0)", p.VX, p.VY)
	}
	if p.Gravity != 0 {
		t.Errorf("Glow gravity = %f, want 0", p.Gravity)
	}
}

// TestNewParticle_RainVertical verifies rain has zero horizontal velocity.
func TestNewParticle_RainVertical(t *testing.T) {
	t.Parallel()

	p := NewParticle(5, 5, ParticleRain)
	if p.VX != 0 {
		t.Errorf("Rain VX = %f, want 0", p.VX)
	}
	if p.VY <= 0 {
		t.Error("Rain VY should be positive (downward)")
	}
}

// TestGlitchText_ProducesOutput verifies GlitchText returns non-empty output.
func TestGlitchText_ProducesOutput(t *testing.T) {
	t.Parallel()

	// With intensity=1.0, we always get the glitch path
	got := GlitchText("hello world", 0, 1.0)
	if got == "" {
		t.Error("GlitchText should produce non-empty output")
	}

	// Stripped result should have non-zero length
	stripped := stripANSI(got)
	if len(stripped) == 0 {
		t.Error("GlitchText stripped should have content")
	}
}

// TestGlitchText_LowIntensityPassthrough verifies low intensity returns text unchanged.
func TestGlitchText_LowIntensityPassthrough(t *testing.T) {
	t.Parallel()

	// With intensity=0.0, rand.Float64() > 0.0 is always true, so text returned as-is
	got := GlitchText("hello", 0, 0.0)
	if got != "hello" {
		t.Errorf("GlitchText with intensity=0.0 should return text unchanged, got %q", got)
	}
}

// TestDissolveEffect_FullProgress shows all characters at progress=1.0.
func TestDissolveEffect_FullProgress(t *testing.T) {
	t.Parallel()

	got := dissolveEffect("hello\nworld", 1.0)
	stripped := stripANSI(got)
	if !strings.Contains(stripped, "hello") {
		t.Errorf("dissolveEffect at progress=1.0 should show all chars, got %q", stripped)
	}
	if !strings.Contains(stripped, "world") {
		t.Errorf("dissolveEffect at progress=1.0 should show all lines, got %q", stripped)
	}
}

// TestDissolveEffect_ZeroProgress replaces all characters with spaces.
func TestDissolveEffect_ZeroProgress(t *testing.T) {
	t.Parallel()

	got := dissolveEffect("hello", 0.0)
	// At progress=0.0, rand.Float64() < 0.0 is always false, so all chars become spaces
	if got != "     " {
		t.Errorf("dissolveEffect at progress=0.0 should be all spaces, got %q", got)
	}
}

// TestTransitionEffect_AllBranches exercises every TransitionType through the dispatcher.
func TestTransitionEffect_AllBranches(t *testing.T) {
	t.Parallel()

	content := "test content\nline two"

	types := []struct {
		name  string
		ttype TransitionType
	}{
		{"FadeOut", TransitionFadeOut},
		{"FadeIn", TransitionFadeIn},
		{"SlideLeft", TransitionSlideLeft},
		{"SlideRight", TransitionSlideRight},
		{"Dissolve", TransitionDissolve},
		{"ZoomIn", TransitionZoomIn},
		{"ZoomOut", TransitionZoomOut},
		{"None", TransitionNone},
	}

	for _, tt := range types {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TransitionEffect(content, tt.ttype, 0.5, 80, 24)
			if got == "" && tt.ttype != TransitionNone {
				t.Error("TransitionEffect should produce non-empty output")
			}
			if tt.ttype == TransitionNone && got != content {
				t.Errorf("TransitionNone should return content unchanged, got %q", got)
			}
		})
	}
}

// TestTransitionEffect_FadeOut_Progress verifies FadeOut uses inverted progress.
func TestTransitionEffect_FadeOut_Progress(t *testing.T) {
	t.Parallel()

	// FadeOut at progress=0.0 means alpha = 1-0 = 1.0 (full brightness)
	got := TransitionEffect("hi", TransitionFadeOut, 0.0, 80, 24)
	stripped := stripANSI(got)
	if !strings.Contains(stripped, "hi") {
		t.Errorf("FadeOut at progress=0 should show full content, got %q", stripped)
	}
}

// TestTransitionEffect_SlideRight_Produces verifies SlideRight output.
func TestTransitionEffect_SlideRight_Produces(t *testing.T) {
	t.Parallel()

	// SlideRight at progress=1.0 (fully visible) should contain the content
	got := TransitionEffect("content here", TransitionSlideRight, 1.0, 80, 24)
	if !strings.Contains(got, "content here") {
		t.Errorf("SlideRight at progress=1.0 should contain content, got %q", got)
	}
}

// TestTransitionEffect_ZoomOut_Produces verifies ZoomOut output.
func TestTransitionEffect_ZoomOut_Produces(t *testing.T) {
	t.Parallel()

	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8"
	// ZoomIn at 0.3 → scale=0.3, ZoomOut at 0.3 → scale=0.7
	gotIn := TransitionEffect(content, TransitionZoomIn, 0.3, 40, 20)
	gotOut := TransitionEffect(content, TransitionZoomOut, 0.3, 40, 20)
	if gotIn == gotOut {
		t.Error("ZoomIn and ZoomOut at same progress should differ")
	}
}

// TestSparkleText_ProducesOutput verifies SparkleText returns non-empty and contains original text.
func TestSparkleText_ProducesOutput(t *testing.T) {
	t.Parallel()

	// Density=0 means no sparkles added
	got := SparkleText("hello world", 0, 0.0)
	if got != "hello world" {
		t.Errorf("SparkleText with density=0 should return text unchanged, got %q", got)
	}

	// Non-zero density produces output with text inside
	got2 := SparkleText("hello", 0, 0.5)
	if got2 == "" {
		t.Error("SparkleText should produce non-empty output")
	}
}

// TestAnimatedBorder_NegativePadding tests the padding < 0 guard.
func TestAnimatedBorder_NegativePadding(t *testing.T) {
	t.Parallel()

	// Content wider than the border width
	longContent := strings.Repeat("x", 100)
	got := AnimatedBorder(longContent, 10, 0, []string{"#89b4fa"})
	if got == "" {
		t.Error("AnimatedBorder with long content should still produce output")
	}
}
