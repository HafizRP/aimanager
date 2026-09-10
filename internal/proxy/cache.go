package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type cachedResponse struct {
	Body        []byte
	StatusCode  int
	ContentType string
	TotalTokens int
	ExpiresAt   time.Time
	Model       string
}

type ResponseCache struct {
	mu      sync.RWMutex
	items   map[string]*cachedResponse
	enabled bool
}

func NewResponseCache() *ResponseCache {
	rc := &ResponseCache{
		items:   make(map[string]*cachedResponse),
		enabled: true,
	}
	// Background cleanup routine
	go func() {
		ticker := time.NewTicker(3 * time.Minute)
		for range ticker.C {
			rc.mu.Lock()
			now := time.Now()
			for k, v := range rc.items {
				if now.After(v.ExpiresAt) {
					delete(rc.items, k)
				}
			}
			rc.mu.Unlock()
		}
	}()
	return rc
}

func (rc *ResponseCache) SetEnabled(enabled bool) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.enabled = enabled
}

func (rc *ResponseCache) GenerateKey(model string, messages interface{}, temperature float64) string {
	msgBytes, _ := json.Marshal(messages)
	h := sha256.New()
	h.Write([]byte(model))
	h.Write([]byte("\n"))
	h.Write(msgBytes)
	h.Write([]byte(fmt.Sprintf("\n%.2f", temperature)))
	return hex.EncodeToString(h.Sum(nil))
}

func (rc *ResponseCache) Get(key string) (*cachedResponse, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	if !rc.enabled {
		return nil, false
	}

	item, exists := rc.items[key]
	if !exists || time.Now().After(item.ExpiresAt) {
		return nil, false
	}
	return item, true
}

const maxCacheEntries = 2000

func (rc *ResponseCache) Set(key string, body []byte, statusCode int, contentType string, tokens int, model string, ttl time.Duration) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if !rc.enabled {
		return
	}

	// Evict if cache exceeds maximum capacity
	if len(rc.items) >= maxCacheEntries {
		now := time.Now()
		for k, v := range rc.items {
			if now.After(v.ExpiresAt) {
				delete(rc.items, k)
			}
		}
		// If still full, prune oldest
		if len(rc.items) >= maxCacheEntries {
			count := 0
			for k := range rc.items {
				delete(rc.items, k)
				count++
				if count >= maxCacheEntries/10 {
					break
				}
			}
		}
	}

	rc.items[key] = &cachedResponse{
		Body:        body,
		StatusCode:  statusCode,
		ContentType: contentType,
		TotalTokens: tokens,
		ExpiresAt:   time.Now().Add(ttl),
		Model:       model,
	}
}

func (rc *ResponseCache) Clear() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.items = make(map[string]*cachedResponse)
}
