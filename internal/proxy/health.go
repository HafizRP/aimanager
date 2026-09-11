// Package proxy implements the OpenAI/Anthropic-compatible reverse proxy gateway.
package proxy

import (
	"sync"
	"time"
)

// UpstreamHealth tracks rolling upstream reliability for smart routing decisions.
type UpstreamHealth struct {
	mu               sync.RWMutex
	total            int64
	failures         int64
	consecutiveFails int
	lastOK           time.Time
	lastErr          time.Time
	lastStatus       int
	windowStart      time.Time
}

// HealthSnapshot is a JSON-safe view of upstream health.
type HealthSnapshot struct {
	Status           string     `json:"status"` // ok | degraded | down
	Total            int64      `json:"total_requests"`
	Failures         int64      `json:"failures"`
	ErrorRate        float64    `json:"error_rate"`
	ConsecutiveFails int        `json:"consecutive_failures"`
	LastOK           *time.Time `json:"last_ok,omitempty"`
	LastErr          *time.Time `json:"last_error,omitempty"`
	LastStatus       int        `json:"last_status"`
}

// NewUpstreamHealth creates a tracker with an open window.
func NewUpstreamHealth() *UpstreamHealth {
	return &UpstreamHealth{windowStart: time.Now()}
}

// retryableStatus reports whether an upstream status deserves another attempt.
func retryableStatus(code int) bool {
	return code == 429 || code == 502 || code == 503 || code == 504
}

// Record logs one upstream attempt outcome.
func (h *UpstreamHealth) Record(status int, transportErr bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Reset rolling window hourly to reflect recent health.
	if time.Since(h.windowStart) > time.Hour {
		h.total, h.failures = 0, 0
		h.windowStart = time.Now()
	}
	h.total++
	now := time.Now()
	if transportErr || status >= 500 || status == 429 {
		h.failures++
		h.consecutiveFails++
		h.lastErr = now
	} else {
		h.consecutiveFails = 0
		h.lastOK = now
	}
	if status != 0 {
		h.lastStatus = status
	}
}

// Snapshot returns the current health view.
func (h *UpstreamHealth) Snapshot() HealthSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	rate := 0.0
	if h.total > 0 {
		rate = float64(h.failures) / float64(h.total)
	}
	status := "ok"
	if h.consecutiveFails >= 5 {
		status = "down"
	} else if h.consecutiveFails >= 2 || (h.total >= 10 && rate >= 0.3) {
		status = "degraded"
	}
	snap := HealthSnapshot{
		Status:           status,
		Total:            h.total,
		Failures:         h.failures,
		ErrorRate:        rate,
		ConsecutiveFails: h.consecutiveFails,
		LastStatus:       h.lastStatus,
	}
	if !h.lastOK.IsZero() {
		t := h.lastOK
		snap.LastOK = &t
	}
	if !h.lastErr.IsZero() {
		t := h.lastErr
		snap.LastErr = &t
	}
	return snap
}
