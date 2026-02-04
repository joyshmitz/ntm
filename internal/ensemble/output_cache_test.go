package ensemble

import (
	"os"
	"testing"
	"time"
)

func TestModeOutputCache_PutGet(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewModeOutputCacheWithDir(dir, ModeOutputCacheConfig{Enabled: true, TTL: time.Minute, MaxEntries: 5}, nil)
	if err != nil {
		t.Fatalf("cache init: %v", err)
	}

	mode := sampleMode(t)
	cfg := ModeOutputConfig{Question: "cache question", AgentType: "cc", SchemaVersion: SchemaVersion}
	fingerprint, err := BuildModeOutputFingerprint("context-hash", mode, cfg)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	output := &ModeOutput{
		ModeID: mode.ID,
		Thesis: "Cached output",
		TopFindings: []Finding{{
			Finding:    "Cache hit",
			Impact:     ImpactLow,
			Confidence: 0.5,
		}},
		Confidence:  0.6,
		GeneratedAt: time.Now().UTC(),
	}

	if err := cache.Put(fingerprint, output); err != nil {
		t.Fatalf("cache put: %v", err)
	}

	lookup := cache.Lookup(fingerprint)
	if !lookup.Hit {
		t.Fatalf("expected cache hit, got miss (%s)", lookup.Reason)
	}
	if lookup.Output == nil || lookup.Output.ModeID != mode.ID {
		t.Fatalf("unexpected cached output: %#v", lookup.Output)
	}
}

func TestModeOutputCache_PersistsAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	mode := sampleMode(t)
	cfg := ModeOutputConfig{Question: "persist", AgentType: "cc", SchemaVersion: SchemaVersion}
	fingerprint, err := BuildModeOutputFingerprint("context-hash", mode, cfg)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	output := &ModeOutput{
		ModeID: mode.ID,
		Thesis: "Persisted output",
		TopFindings: []Finding{{
			Finding:    "Persist",
			Impact:     ImpactLow,
			Confidence: 0.5,
		}},
		Confidence:  0.7,
		GeneratedAt: time.Now().UTC(),
	}

	cache1, err := NewModeOutputCacheWithDir(dir, ModeOutputCacheConfig{Enabled: true, TTL: time.Minute, MaxEntries: 5}, nil)
	if err != nil {
		t.Fatalf("cache init 1: %v", err)
	}
	if err := cache1.Put(fingerprint, output); err != nil {
		t.Fatalf("cache put: %v", err)
	}

	cache2, err := NewModeOutputCacheWithDir(dir, ModeOutputCacheConfig{Enabled: true, TTL: time.Minute, MaxEntries: 5}, nil)
	if err != nil {
		t.Fatalf("cache init 2: %v", err)
	}
	lookup := cache2.Lookup(fingerprint)
	if !lookup.Hit {
		t.Fatalf("expected cache hit after reload, got miss (%s)", lookup.Reason)
	}
}

func TestModeOutputCache_Expires(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewModeOutputCacheWithDir(dir, ModeOutputCacheConfig{Enabled: true, TTL: 10 * time.Millisecond, MaxEntries: 5}, nil)
	if err != nil {
		t.Fatalf("cache init: %v", err)
	}

	mode := sampleMode(t)
	cfg := ModeOutputConfig{Question: "expire", AgentType: "cc", SchemaVersion: SchemaVersion}
	fingerprint, err := BuildModeOutputFingerprint("context-hash", mode, cfg)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	output := &ModeOutput{
		ModeID: mode.ID,
		Thesis: "Expired",
		TopFindings: []Finding{{
			Finding:    "Expire",
			Impact:     ImpactLow,
			Confidence: 0.5,
		}},
		Confidence:  0.6,
		GeneratedAt: time.Now().UTC(),
	}
	if err := cache.Put(fingerprint, output); err != nil {
		t.Fatalf("cache put: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	lookup := cache.Lookup(fingerprint)
	if lookup.Hit {
		t.Fatal("expected cache entry to expire")
	}

	if _, err := os.Stat(cache.filePath(fingerprint.CacheKey())); !os.IsNotExist(err) {
		t.Fatalf("expected cache file removed, err=%v", err)
	}
}

func TestModeOutputCache_InvalidationReason_ConfigMismatch(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewModeOutputCacheWithDir(dir, ModeOutputCacheConfig{Enabled: true, TTL: time.Minute, MaxEntries: 5}, nil)
	if err != nil {
		t.Fatalf("cache init: %v", err)
	}

	mode := sampleMode(t)
	cfg := ModeOutputConfig{Question: "config", AgentType: "cc", SchemaVersion: SchemaVersion}
	fingerprint, err := BuildModeOutputFingerprint("context-hash", mode, cfg)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	output := &ModeOutput{
		ModeID: mode.ID,
		Thesis: "Config mismatch",
		TopFindings: []Finding{{
			Finding:    "Config",
			Impact:     ImpactLow,
			Confidence: 0.5,
		}},
		Confidence:  0.6,
		GeneratedAt: time.Now().UTC(),
	}
	if err := cache.Put(fingerprint, output); err != nil {
		t.Fatalf("cache put: %v", err)
	}

	altCfg := ModeOutputConfig{Question: "config", AgentType: "cod", SchemaVersion: SchemaVersion}
	altFingerprint, err := BuildModeOutputFingerprint("context-hash", mode, altCfg)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	lookup := cache.Lookup(altFingerprint)
	if lookup.Hit {
		t.Fatal("expected cache miss for config mismatch")
	}
	if lookup.Reason != "config_mismatch" {
		t.Fatalf("expected config_mismatch, got %s", lookup.Reason)
	}
}

func sampleMode(t *testing.T) *ReasoningMode {
	t.Helper()
	catalog, err := LoadModeCatalog()
	if err != nil {
		t.Fatalf("load mode catalog: %v", err)
	}
	modes := catalog.ListModes()
	if len(modes) == 0 {
		t.Fatal("no modes available")
	}
	mode := modes[0]
	return &mode
}
