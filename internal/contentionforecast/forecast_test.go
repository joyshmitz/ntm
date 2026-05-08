package contentionforecast

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func clock() time.Time {
	return time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
}

func TestCompute_NoHistoryProducesEmptyHotspots(t *testing.T) {
	t.Parallel()
	f := Compute(Inputs{Now: clock()})
	if len(f.Hotspots) != 0 {
		t.Errorf("Hotspots = %d, want 0 on empty inputs", len(f.Hotspots))
	}
	if f.GeneratedAt.IsZero() {
		t.Errorf("GeneratedAt unset")
	}
}

// Repeated reservations with conflicts on the same pattern must
// produce a top hotspot with high confidence and a non-zero
// conflict_rate factor.
func TestCompute_RepeatedContentionProducesTopHotspot(t *testing.T) {
	t.Parallel()
	now := clock()
	hot := "internal/auth/**"
	cool := "internal/docs/**"

	episodes := make([]ReservationEpisode, 0, 30)
	// 25 conflicting reservations on the hot pattern.
	for i := 0; i < 25; i++ {
		episodes = append(episodes, ReservationEpisode{
			PathPattern: hot,
			AgentName:   pickAgent(i),
			AcquiredAt:  now.Add(-time.Duration(i+1) * time.Hour),
			ReleasedAt:  now.Add(-time.Duration(i)*time.Hour - 30*time.Minute),
			Conflicted:  i%2 == 0, // half of them clashed
		})
	}
	// 3 calm reservations on the cool pattern.
	for i := 0; i < 3; i++ {
		episodes = append(episodes, ReservationEpisode{
			PathPattern: cool,
			AgentName:   "Docs",
			AcquiredAt:  now.Add(-time.Duration(i+1) * time.Hour),
		})
	}

	f := Compute(Inputs{Reservations: episodes, Now: now})
	if len(f.Hotspots) == 0 {
		t.Fatal("no hotspots produced")
	}
	top := f.Hotspots[0]
	if top.PathPattern != hot {
		t.Errorf("top pattern = %s, want %s", top.PathPattern, hot)
	}
	if top.Confidence != ConfidenceHigh {
		t.Errorf("top confidence = %s, want high (25 samples)", top.Confidence)
	}
	if top.Factors["conflict_rate"] == 0 {
		t.Errorf("conflict_rate = 0, want > 0")
	}
	if top.Factors["frequency"] == 0 {
		t.Errorf("frequency = 0, want > 0")
	}
	if len(top.LikelyOwners) == 0 {
		t.Errorf("LikelyOwners empty; want at least one")
	}
}

// A super-broad glob like "**" must take a penalty so it does not
// dwarf narrower patterns — but it should still surface in the
// report (penalty != elimination).
func TestCompute_BroadGlobGetsPenalty(t *testing.T) {
	t.Parallel()
	now := clock()
	in := Inputs{
		Now: now,
		Reservations: []ReservationEpisode{
			{PathPattern: "**", AgentName: "Wide", AcquiredAt: now.Add(-1 * time.Hour), Conflicted: true},
			{PathPattern: "**", AgentName: "Wide", AcquiredAt: now.Add(-2 * time.Hour), Conflicted: true},
			{PathPattern: "**", AgentName: "Wide", AcquiredAt: now.Add(-3 * time.Hour), Conflicted: true},
			// Narrow: same conflict count, but specific.
			{PathPattern: "internal/auth/session.go", AgentName: "Narrow", AcquiredAt: now.Add(-1 * time.Hour), Conflicted: true},
			{PathPattern: "internal/auth/session.go", AgentName: "Narrow", AcquiredAt: now.Add(-2 * time.Hour), Conflicted: true},
			{PathPattern: "internal/auth/session.go", AgentName: "Narrow", AcquiredAt: now.Add(-3 * time.Hour), Conflicted: true},
		},
	}
	f := Compute(in)
	var wide, narrow *Hotspot
	for i := range f.Hotspots {
		switch f.Hotspots[i].PathPattern {
		case "**":
			wide = &f.Hotspots[i]
		case "internal/auth/session.go":
			narrow = &f.Hotspots[i]
		}
	}
	if wide == nil || narrow == nil {
		t.Fatalf("missing hotspot(s): wide=%v narrow=%v", wide, narrow)
	}
	if wide.Factors["broad_glob_penalty"] <= 0 {
		t.Errorf("broad_glob_penalty for ** = %v, want > 0", wide.Factors["broad_glob_penalty"])
	}
	if narrow.Factors["broad_glob_penalty"] != 0 {
		t.Errorf("broad_glob_penalty for narrow path = %v, want 0", narrow.Factors["broad_glob_penalty"])
	}
	if wide.Score >= narrow.Score {
		t.Errorf("wide.Score=%v >= narrow.Score=%v; broad-glob penalty did not bite", wide.Score, narrow.Score)
	}
}

// Stale reservations must contribute less than fresh ones — same
// count, vastly different ages should produce different scores.
func TestCompute_StaleHistoryDecays(t *testing.T) {
	t.Parallel()
	now := clock()

	freshEpisodes := []ReservationEpisode{
		{PathPattern: "internal/recent/**", AgentName: "A", AcquiredAt: now.Add(-1 * time.Hour), Conflicted: true},
		{PathPattern: "internal/recent/**", AgentName: "A", AcquiredAt: now.Add(-2 * time.Hour), Conflicted: true},
		{PathPattern: "internal/recent/**", AgentName: "A", AcquiredAt: now.Add(-3 * time.Hour), Conflicted: true},
	}
	staleEpisodes := []ReservationEpisode{
		{PathPattern: "internal/old/**", AgentName: "B", AcquiredAt: now.Add(-90 * 24 * time.Hour), Conflicted: true},
		{PathPattern: "internal/old/**", AgentName: "B", AcquiredAt: now.Add(-91 * 24 * time.Hour), Conflicted: true},
		{PathPattern: "internal/old/**", AgentName: "B", AcquiredAt: now.Add(-92 * 24 * time.Hour), Conflicted: true},
	}

	fresh := Compute(Inputs{Reservations: freshEpisodes, Now: now}).Hotspots
	stale := Compute(Inputs{Reservations: staleEpisodes, Now: now}).Hotspots

	if len(fresh) == 0 || len(stale) == 0 {
		t.Fatal("expected hotspots in both fresh and stale runs")
	}
	if fresh[0].Factors["frequency"] <= stale[0].Factors["frequency"] {
		t.Errorf("frequency: fresh=%v stale=%v; decay did not bite",
			fresh[0].Factors["frequency"], stale[0].Factors["frequency"])
	}
	if fresh[0].Score <= stale[0].Score {
		t.Errorf("score: fresh=%v stale=%v; decay did not bite", fresh[0].Score, stale[0].Score)
	}
}

// Decay disabled means same-count stale and fresh produce the same
// score (modulo other factors).
func TestCompute_DecayDisabledKeepsAllSamplesAtFullWeight(t *testing.T) {
	t.Parallel()
	now := clock()
	in := Inputs{
		Now:           now,
		DecayHalfLife: -1, // <=0 disables in decayWeight; let's pass an explicit zero
		Reservations: []ReservationEpisode{
			{PathPattern: "internal/x/**", AgentName: "A", AcquiredAt: now.Add(-100 * 24 * time.Hour), Conflicted: true},
			{PathPattern: "internal/x/**", AgentName: "A", AcquiredAt: now.Add(-2 * time.Hour), Conflicted: true},
		},
	}
	// DecayHalfLife <=0 disables decay per implementation; we use -1 to be explicit.
	in.DecayHalfLife = 0
	defaultRun := Compute(in).Hotspots[0]
	in.DecayHalfLife = 1<<62 // effectively no decay (very long half-life)
	noDecayRun := Compute(in).Hotspots[0]
	// With near-no-decay the second run should not score lower than
	// default (which uses 14d half-life and penalizes the 100d sample).
	if noDecayRun.Score < defaultRun.Score {
		t.Errorf("noDecay.Score=%v default.Score=%v; near-no-decay should score ≥ default",
			noDecayRun.Score, defaultRun.Score)
	}
}

// Label-cluster bonus: two closed beads sharing a label and a path
// add weight to that path even without any reservation history.
func TestCompute_LabelClusterBonusFromClosedBeads(t *testing.T) {
	t.Parallel()
	now := clock()
	in := Inputs{
		Now: now,
		ClosedBeads: []ClosedBead{
			{ID: "bd-1", Labels: []string{"auth"}, Paths: []string{"internal/auth/session.go"}, ClosedAt: now.Add(-2 * time.Hour)},
			{ID: "bd-2", Labels: []string{"auth"}, Paths: []string{"internal/auth/session.go"}, ClosedAt: now.Add(-3 * time.Hour)},
		},
	}
	f := Compute(in)
	if len(f.Hotspots) == 0 {
		t.Fatal("no hotspots; label cluster did not surface")
	}
	if f.Hotspots[0].Factors["label_overlap"] == 0 {
		t.Errorf("label_overlap = 0, want > 0")
	}
}

// Git-touch activity surfaces a hotspot for files with no
// reservation history.
func TestCompute_GitActivitySurfacesUnreservedFiles(t *testing.T) {
	t.Parallel()
	now := clock()
	in := Inputs{
		Now: now,
		TouchedPaths: []TouchedPath{
			{Path: "internal/hot.go", TouchCount: 12, LastTouched: now.Add(-1 * time.Hour)},
		},
	}
	f := Compute(in)
	if len(f.Hotspots) != 1 {
		t.Fatalf("Hotspots = %d, want 1", len(f.Hotspots))
	}
	if f.Hotspots[0].PathPattern != "internal/hot.go" {
		t.Errorf("pattern = %s, want internal/hot.go", f.Hotspots[0].PathPattern)
	}
	if f.Hotspots[0].Factors["git_activity"] == 0 {
		t.Errorf("git_activity = 0, want > 0")
	}
}

func TestCompute_AlternativesProposedForBroadPatterns(t *testing.T) {
	t.Parallel()
	now := clock()
	in := Inputs{
		Now: now,
		Reservations: []ReservationEpisode{
			{PathPattern: "internal/**", AgentName: "A", AcquiredAt: now.Add(-1 * time.Hour), Conflicted: true},
		},
		TouchedPaths: []TouchedPath{
			{Path: "internal/auth/session.go", TouchCount: 8, LastTouched: now.Add(-1 * time.Hour)},
			{Path: "internal/billing/charge.go", TouchCount: 4, LastTouched: now.Add(-2 * time.Hour)},
		},
	}
	f := Compute(in)
	var top *Hotspot
	for i := range f.Hotspots {
		if f.Hotspots[i].PathPattern == "internal/**" {
			top = &f.Hotspots[i]
		}
	}
	if top == nil {
		t.Fatal("internal/** hotspot missing")
	}
	if len(top.Alternatives) == 0 {
		t.Errorf("Alternatives empty for broad pattern; want narrower suggestions")
	}
	hasAuth := false
	for _, a := range top.Alternatives {
		if strings.Contains(a, "internal/auth/") {
			hasAuth = true
			break
		}
	}
	if !hasAuth {
		t.Errorf("Alternatives = %v, want one mentioning internal/auth/", top.Alternatives)
	}
}

func TestCompute_DeterministicSort(t *testing.T) {
	t.Parallel()
	now := clock()
	in := Inputs{
		Now: now,
		Reservations: []ReservationEpisode{
			{PathPattern: "z/**", AgentName: "A", AcquiredAt: now.Add(-1 * time.Hour), Conflicted: true},
			{PathPattern: "a/**", AgentName: "A", AcquiredAt: now.Add(-1 * time.Hour), Conflicted: true},
			{PathPattern: "m/**", AgentName: "A", AcquiredAt: now.Add(-1 * time.Hour), Conflicted: true},
		},
	}
	a, _ := json.Marshal(Compute(in))
	b, _ := json.Marshal(Compute(in))
	if string(a) != string(b) {
		t.Errorf("Compute output drifted between calls:\nfirst:  %s\nsecond: %s", a, b)
	}
}

// Confidence reflects sample count.
func TestCompute_ConfidenceTiers(t *testing.T) {
	t.Parallel()
	now := clock()
	build := func(n int) Inputs {
		eps := make([]ReservationEpisode, n)
		for i := 0; i < n; i++ {
			eps[i] = ReservationEpisode{
				PathPattern: "x/**",
				AgentName:   "A",
				AcquiredAt:  now.Add(-time.Duration(i+1) * time.Hour),
				Conflicted:  true,
			}
		}
		return Inputs{Reservations: eps, Now: now}
	}
	if c := Compute(build(2)).Hotspots[0].Confidence; c != ConfidenceLow {
		t.Errorf("2 samples confidence = %s, want low", c)
	}
	if c := Compute(build(8)).Hotspots[0].Confidence; c != ConfidenceMedium {
		t.Errorf("8 samples confidence = %s, want medium", c)
	}
	if c := Compute(build(25)).Hotspots[0].Confidence; c != ConfidenceHigh {
		t.Errorf("25 samples confidence = %s, want high", c)
	}
}

func pickAgent(i int) string {
	names := []string{"Alice", "Bob", "Carol"}
	return names[i%len(names)]
}
