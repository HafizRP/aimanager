package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
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

type CacheStats struct {
	Enabled       bool        `json:"enabled"`
	Entries       int         `json:"entries"`
	Capacity      int         `json:"capacity"`
	Hits          int64       `json:"hits"`
	Misses        int64       `json:"misses"`
	HitRate       float64     `json:"hit_rate"`     // 0-1
	TokensSaved   int64       `json:"tokens_saved"` // tokens served from cache
	CostSavedUSD  float64     `json:"cost_saved_usd"`
	CostSavedIDR  int64       `json:"cost_saved_idr"`
	TTLSeconds    int         `json:"ttl_seconds"`
	TopModels     []ModelStat `json:"top_models"`
	LastHit       *time.Time  `json:"last_hit,omitempty"`
	UptimeSeconds int64       `json:"uptime_seconds"`
	CreatedAt     time.Time   `json:"created_at"`
}

type ModelStat struct {
	Model        string `json:"model"`
	Hits         int64  `json:"hits"`
	TokensSaved  int64  `json:"tokens_saved"`
	BytesSaved   int64  `json:"bytes_saved"`
	AvgLatencyMs int64  `json:"avg_latency_ms"` // Avg upstream round-trip avoided
}

type ResponseCache struct {
	mu      sync.RWMutex
	items   map[string]*cachedResponse
	enabled bool

	hits        int64
	misses      int64
	tokensSaved int64
	bytesSaved  int64
	modelStats  map[string]*ModelStat
	lastHit     time.Time
	createdAt   time.Time
	avgLatency  map[string]int64 // model -> cumulative latency ms
	latencyN    map[string]int64 // model -> count
}

const (
	maxCacheEntries = 2000
	cacheTTL        = 15 * time.Minute
)

func NewResponseCache() *ResponseCache {
	rc := &ResponseCache{
		items:      make(map[string]*cachedResponse),
		enabled:    true,
		modelStats: make(map[string]*ModelStat),
		avgLatency: make(map[string]int64),
		latencyN:   make(map[string]int64),
		createdAt:  time.Now(),
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
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if !rc.enabled {
		rc.misses++
		return nil, false
	}

	item, exists := rc.items[key]
	if !exists || time.Now().After(item.ExpiresAt) {
		rc.misses++
		return nil, false
	}

	rc.hits++
	rc.tokensSaved += int64(item.TotalTokens)
	rc.bytesSaved += int64(len(item.Body))
	rc.lastHit = time.Now()

	ms := rc.modelStats[item.Model]
	if ms == nil {
		ms = &ModelStat{Model: item.Model}
		rc.modelStats[item.Model] = ms
	}
	ms.Hits++
	ms.TokensSaved += int64(item.TotalTokens)
	ms.BytesSaved += int64(len(item.Body))
	if rc.latencyN[item.Model] > 0 {
		ms.AvgLatencyMs = rc.avgLatency[item.Model] / rc.latencyN[item.Model]
	}

	return item, true
}

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

	if ttl <= 0 {
		ttl = cacheTTL
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

// RecordUpstreamLatency logs the measured upstream round-trip time for a model.
// It is used to estimate the latency saved on every cache hit.
func (rc *ResponseCache) RecordUpstreamLatency(model string, latencyMs int64) {
	if latencyMs <= 0 || model == "" {
		return
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.avgLatency[model] += latencyMs
	rc.latencyN[model]++
}

// Stats returns a snapshot of cumulative cache analytics.
func (rc *ResponseCache) Stats() CacheStats {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	totalRequests := rc.hits + rc.misses
	hitRate := 0.0
	if totalRequests > 0 {
		hitRate = float64(rc.hits) / float64(totalRequests)
	}

	topModels := make([]ModelStat, 0, len(rc.modelStats))
	for _, ms := range rc.modelStats {
		topModels = append(topModels, *ms)
	}
	sort.Slice(topModels, func(i, j int) bool {
		return topModels[i].Hits > topModels[j].Hits
	})
	if len(topModels) > 10 {
		topModels = topModels[:10]
	}

	var lastHit *time.Time
	if !rc.lastHit.IsZero() {
		t := rc.lastHit
		lastHit = &t
	}

	// FinOps pricing: $5/1M tokens, Rp 80.000/1M tokens (same convention as dashboard)
	costUSD := float64(rc.tokensSaved) * 0.000005
	costIDR := int64(float64(rc.tokensSaved) * 0.08)

	return CacheStats{
		Enabled:       rc.enabled,
		Entries:       len(rc.items),
		Capacity:      maxCacheEntries,
		Hits:          rc.hits,
		Misses:        rc.misses,
		HitRate:       hitRate,
		TokensSaved:   rc.tokensSaved,
		CostSavedUSD:  costUSD,
		CostSavedIDR:  costIDR,
		TTLSeconds:    int(cacheTTL.Seconds()),
		TopModels:     topModels,
		LastHit:       lastHit,
		UptimeSeconds: int64(time.Since(rc.createdAt).Seconds()),
		CreatedAt:     rc.createdAt,
	}
}

// RecordMiss increments the miss counter without looking up the key
// (used when cache key generation is skipped, e.g. streaming or non-cacheable responses).
func (rc *ResponseCache) RecordMiss() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.misses++
}

// ResetStats zeroes the cumulative analytics counters (entries are preserved).
func (rc *ResponseCache) ResetStats() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.hits = 0
	rc.misses = 0
	rc.tokensSaved = 0
	rc.bytesSaved = 0
	rc.modelStats = make(map[string]*ModelStat)
	rc.avgLatency = make(map[string]int64)
	rc.latencyN = make(map[string]int64)
	rc.lastHit = time.Time{}
}

func (rc *ResponseCache) Clear() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.items = make(map[string]*cachedResponse)
}
