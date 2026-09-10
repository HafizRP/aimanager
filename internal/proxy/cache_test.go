package proxy

import (
	"testing"
	"time"
)

func TestResponseCache_Stats(t *testing.T) {
	cache := NewResponseCache()

	// Miss then hit
	key := cache.GenerateKey("ag/gemini-3.8-flash-low", []map[string]string{{"role": "user", "content": "hello"}}, 0.7)
	if _, found := cache.Get(key); found {
		t.Fatal("expected initial miss")
	}
	cache.Set(key, []byte(`{"choices":[{"message":{"content":"world"}}]}`), 200, "application/json", 100, "ag/gemini-3.8-flash-low", 1*time.Minute)
	if _, found := cache.Get(key); !found {
		t.Fatal("expected hit after set")
	}

	stats := cache.Stats()
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.HitRate != 0.5 {
		t.Errorf("expected hit rate 0.5, got %f", stats.HitRate)
	}
	if stats.TokensSaved != 100 {
		t.Errorf("expected 100 tokens saved, got %d", stats.TokensSaved)
	}
	if stats.Entries != 1 {
		t.Errorf("expected 1 entry, got %d", stats.Entries)
	}
	if len(stats.TopModels) != 1 || stats.TopModels[0].Model != "ag/gemini-3.8-flash-low" {
		t.Errorf("expected top model entry, got %+v", stats.TopModels)
	}
}

func TestResponseCache_ResetStats(t *testing.T) {
	cache := NewResponseCache()
	cache.RecordMiss()
	cache.RecordMiss()
	cache.ResetStats()

	stats := cache.Stats()
	if stats.Hits != 0 || stats.Misses != 0 || stats.TokensSaved != 0 {
		t.Errorf("expected zeroed stats after reset, got %+v", stats)
	}
}

func TestResponseCache_RecordUpstreamLatency(t *testing.T) {
	cache := NewResponseCache()
	cache.RecordUpstreamLatency("ag/gemini-3.8-flash-low", 100)
	cache.RecordUpstreamLatency("ag/gemini-3.8-flash-low", 300)

	key := cache.GenerateKey("ag/gemini-3.8-flash-low", []map[string]string{{"role": "user", "content": "hello"}}, 0.7)
	cache.Set(key, []byte(`{"choices":[{"message":{"content":"world"}}]}`), 200, "application/json", 10, "ag/gemini-3.8-flash-low", 1*time.Minute)
	if _, found := cache.Get(key); !found {
		t.Fatal("expected hit after set")
	}

	stats := cache.Stats()
	if len(stats.TopModels) != 1 {
		t.Fatalf("expected 1 model, got %d", len(stats.TopModels))
	}
	if stats.TopModels[0].AvgLatencyMs != 200 {
		t.Errorf("expected avg latency 200ms, got %d", stats.TopModels[0].AvgLatencyMs)
	}
}

func TestResponseCache_DefaultTTL(t *testing.T) {
	cache := NewResponseCache()
	key := "default-ttl-key"
	cache.Set(key, []byte("data"), 200, "text/plain", 5, "m", 0)
	item, found := cache.Get(key)
	if !found {
		t.Fatal("expected item found")
	}
	if item.ExpiresAt.Sub(time.Now()) > 16*time.Minute {
		t.Errorf("expected TTL around 15m, got %v", item.ExpiresAt.Sub(time.Now()))
	}
}

func TestResponseCache_Basic(t *testing.T) {
	cache := NewResponseCache()

	key := cache.GenerateKey("main", []map[string]string{{"role": "user", "content": "hello"}}, 0.7)
	if key == "" {
		t.Fatal("expected non-empty cache key")
	}

	// Verify miss initially
	if _, found := cache.Get(key); found {
		t.Fatal("expected cache miss for unpopulated key")
	}

	// Set cache item
	body := []byte(`{"choices":[{"message":{"content":"world"}}]}`)
	cache.Set(key, body, 200, "application/json", 15, "main", 1*time.Minute)

	// Verify hit
	cached, found := cache.Get(key)
	if !found {
		t.Fatal("expected cache hit after Set")
	}
	if string(cached.Body) != string(body) {
		t.Errorf("expected body %s, got %s", string(body), string(cached.Body))
	}
	if cached.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", cached.StatusCode)
	}
	if cached.TotalTokens != 15 {
		t.Errorf("expected total tokens 15, got %d", cached.TotalTokens)
	}
}

func TestResponseCache_Expiry(t *testing.T) {
	cache := NewResponseCache()

	key := "test-expiry-key"
	// Set with very short TTL
	cache.Set(key, []byte("quick"), 200, "text/plain", 5, "m", 50*time.Millisecond)

	if _, found := cache.Get(key); !found {
		t.Fatal("expected item to be found immediately")
	}

	time.Sleep(70 * time.Millisecond)

	if _, found := cache.Get(key); found {
		t.Fatal("expected item to be expired after sleep")
	}
}

func TestResponseCache_Disabled(t *testing.T) {
	cache := NewResponseCache()
	key := "disabled-key"
	cache.Set(key, []byte("data"), 200, "text/plain", 10, "m", 1*time.Minute)

	cache.SetEnabled(false)
	if _, found := cache.Get(key); found {
		t.Fatal("expected cache miss when cache is disabled")
	}
}
