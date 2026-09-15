package proxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCachedHandler_Decorator(t *testing.T) {
	cache := NewResponseCache()

	var nextCalls int32
	backendHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&nextCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp-1","choices":[{"message":{"content":"Hello"}}]}`))
	})

	cached := NewCachedHandler(backendHandler, cache, nil)

	reqPayload := []byte(`{"model":"ag/gemini-3.8-flash","messages":[{"role":"user","content":"ping"}],"temperature":0.7}`)

	// Request 1: Cache MISS -> backend called
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBuffer(reqPayload))
	w1 := httptest.NewRecorder()
	cached.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w1.Code)
	}
	if calls := atomic.LoadInt32(&nextCalls); calls != 1 {
		t.Fatalf("expected 1 backend call, got %d", calls)
	}

	// Request 2: Cache HIT -> backend NOT called, served from cache
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBuffer(reqPayload))
	w2 := httptest.NewRecorder()
	cached.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on cache hit, got %d", w2.Code)
	}
	if w2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT, got %q", w2.Header().Get("X-Cache"))
	}
	if calls := atomic.LoadInt32(&nextCalls); calls != 1 {
		t.Fatalf("expected backend not called on cache hit (still 1 call), got %d", calls)
	}

	// Request 3: Cache BYPASS via X-No-Cache -> backend called again
	req3 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBuffer(reqPayload))
	req3.Header.Set("X-No-Cache", "1")
	w3 := httptest.NewRecorder()
	cached.ServeHTTP(w3, req3)

	if w3.Header().Get("X-Cache") != "BYPASS" {
		t.Errorf("expected X-Cache: BYPASS, got %q", w3.Header().Get("X-Cache"))
	}
	if calls := atomic.LoadInt32(&nextCalls); calls != 2 {
		t.Fatalf("expected 2 backend calls after bypass, got %d", calls)
	}
}

func TestCachedHandler_StreamingBypass(t *testing.T) {
	cache := NewResponseCache()

	var nextCalls int32
	backendHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&nextCalls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})

	cached := NewCachedHandler(backendHandler, cache, nil)

	reqPayload := []byte(`{"model":"ag/gemini-3.8-flash","stream":true,"messages":[{"role":"user","content":"stream"}]}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBuffer(reqPayload))
	w := httptest.NewRecorder()
	cached.ServeHTTP(w, req)

	if calls := atomic.LoadInt32(&nextCalls); calls != 1 {
		t.Fatalf("expected streaming request to bypass cache, calls: %d", calls)
	}
	if w.Header().Get("X-Cache") == "HIT" {
		t.Error("streaming request should not be served from cache")
	}
}
