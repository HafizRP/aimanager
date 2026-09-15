// Package proxy implements the OpenAI/Anthropic-compatible reverse proxy gateway.
package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"9router-gateway/internal/repository"
)

// cachingResponseWriter captures the HTTP response body and status for cache insertion.
type cachingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func (w *cachingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *cachingResponseWriter) Write(b []byte) (int, error) {
	// Buffer response for cache if under 2MB
	if w.body.Len() < 2*1024*1024 {
		w.body.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// Flush propagates flushes to the underlying ResponseWriter if supported.
func (w *cachingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// CachedHandler is a Decorator that wraps an http.Handler with exact-match caching.
type CachedHandler struct {
	next    http.Handler
	cache   *ResponseCache
	repo    repository.Repository
	enabled bool
}

// NewCachedHandler creates a CachedHandler decorator wrapping next.
func NewCachedHandler(next http.Handler, cache *ResponseCache, repo repository.Repository) *CachedHandler {
	return &CachedHandler{
		next:    next,
		cache:   cache,
		repo:    repo,
		enabled: true,
	}
}

// SetEnabled toggles cache decorator execution.
func (ch *CachedHandler) SetEnabled(enabled bool) {
	ch.enabled = enabled
}

// ServeHTTP intercepts requests, serves cache hits immediately, or wraps the response to cache misses.
func (ch *CachedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !ch.enabled || ch.cache == nil || r.Method != http.MethodPost {
		ch.next.ServeHTTP(w, r)
		return
	}

	// 1. Check cache bypass headers
	bypass := r.Header.Get("X-No-Cache") == "1" || strings.Contains(r.Header.Get("Cache-Control"), "no-cache")
	if bypass {
		w.Header().Set("X-Cache", "BYPASS")
		ch.next.ServeHTTP(w, r)
		return
	}

	// 2. Inspect request body for cacheability
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		ch.next.ServeHTTP(w, r)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var reqBodyMap map[string]interface{}
	_ = json.Unmarshal(bodyBytes, &reqBodyMap)

	if reqBodyMap == nil {
		ch.next.ServeHTTP(w, r)
		return
	}

	// Streaming requests cannot be cached
	if stream, ok := reqBodyMap["stream"].(bool); ok && stream {
		ch.cache.RecordMiss()
		ch.next.ServeHTTP(w, r)
		return
	}

	model, _ := reqBodyMap["model"].(string)
	temp := 0.7
	if t, ok := reqBodyMap["temperature"].(float64); ok {
		temp = t
	}

	cacheKey := ch.cache.GenerateKey(model, reqBodyMap["messages"], temp)

	// 3. Cache HIT: Return immediately without touching downstream handler
	if cached, found := ch.cache.Get(cacheKey); found {
		w.Header().Set("Content-Type", cached.ContentType)
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(cached.StatusCode)
		_, _ = w.Write(cached.Body)
		return
	}

	// 4. Cache MISS: Execute next handler with response capture
	crw := &cachingResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
	}

	start := time.Now()
	ch.next.ServeHTTP(crw, r)
	latency := time.Since(start).Milliseconds()

	// 5. Cache response if 200 OK and cacheable JSON
	if crw.statusCode == http.StatusOK && crw.body.Len() > 0 {
		cType := crw.Header().Get("Content-Type")
		if strings.Contains(cType, "application/json") {
			tokens := extractTokensFromJSON(crw.body.Bytes())
			if tokens <= 0 {
				tokens = (len(bodyBytes) + crw.body.Len()) / 4
			}
			ch.cache.Set(cacheKey, crw.body.Bytes(), crw.statusCode, cType, tokens, model, 15*time.Minute)
			ch.cache.RecordUpstreamLatency(model, latency)
		}
	}
}

func extractTokensFromJSON(body []byte) int {
	var respMap struct {
		Usage *struct {
			TotalTokens int `json:"total_tokens"`
			InputTokens int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &respMap); err == nil && respMap.Usage != nil {
		if respMap.Usage.TotalTokens > 0 {
			return respMap.Usage.TotalTokens
		}
		return respMap.Usage.InputTokens + respMap.Usage.OutputTokens
	}
	return 0
}
