package layout

import "testing"

func TestTierForWidth(t *testing.T) {
	tests := []struct {
		width int
		want  Tier
	}{
		{100, TierNarrow},
		{120, TierSplit},
		{150, TierSplit},
		{200, TierWide},
		{239, TierWide},
		{240, TierUltra},
		{319, TierUltra},
		{320, TierMega},
		{400, TierMega},
	}

	for _, tt := range tests {
		if got := TierForWidth(tt.width); got != tt.want {
			t.Errorf("TierForWidth(%d) = %v, want %v", tt.width, got, tt.want)
		}
	}
}

func TestUltraProportions(t *testing.T) {
	width := 300 // Ultra tier
	l, c, r := UltraProportions(width)
	
	total := l + c + r
	expectedTotal := width - 6 // padding budget
	
	if total != expectedTotal {
		t.Errorf("UltraProportions(%d) total width = %d, want %d", width, total, expectedTotal)
	}
	
	if l == 0 || c == 0 || r == 0 {
		t.Errorf("UltraProportions(%d) returned zero width panel: %d/%d/%d", width, l, c, r)
	}
}

func TestMegaProportions(t *testing.T) {
	width := 400 // Mega tier
	p1, p2, p3, p4, p5 := MegaProportions(width)
	
	total := p1 + p2 + p3 + p4 + p5
	expectedTotal := width - 10 // padding budget
	
	if total != expectedTotal {
		t.Errorf("MegaProportions(%d) total width = %d, want %d", width, total, expectedTotal)
	}
	
	if p1 == 0 || p2 == 0 || p3 == 0 || p4 == 0 || p5 == 0 {
		t.Errorf("MegaProportions(%d) returned zero width panel", width)
	}
}
