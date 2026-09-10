package proxy

import (
	"testing"
	"time"
)

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
