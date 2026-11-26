package proxy

import (
	"net/http/httptest"
	"testing"

	"9router-gateway/internal/config"
	"9router-gateway/internal/entity"
)

func TestRateLimiter_Unlimited(t *testing.T) {
	rl := NewRateLimiter()

	// Both RPM and TPM <= 0 means unlimited
	allowed, msg := rl.Allow("test-unlimited", 0, 0, 1000)
	if !allowed || msg != "" {
		t.Errorf("expected unlimited to allow request, got allowed=%v, msg=%s", allowed, msg)
	}
}

func TestRateLimiter_RPM(t *testing.T) {
	rl := NewRateLimiter()
	id := "test-rpm-user"
	limitRPM := 3

	// Requests 1, 2, 3 should be allowed
	for i := 1; i <= 3; i++ {
		allowed, msg := rl.Allow(id, limitRPM, 0, 10)
		if !allowed {
			t.Fatalf("request %d should be allowed, got msg: %s", i, msg)
		}
	}

	// Request 4 should be blocked
	allowed, msg := rl.Allow(id, limitRPM, 0, 10)
	if allowed {
		t.Fatal("request 4 should have been rate-limited")
	}
	if msg == "" {
		t.Error("expected rate limit message, got empty string")
	}
}

func TestRateLimiter_TPM(t *testing.T) {
	rl := NewRateLimiter()
	id := "test-tpm-user"
	limitTPM := int64(500)

	// First request with 300 tokens: allowed
	allowed, _ := rl.Allow(id, 0, limitTPM, 300)
	if !allowed {
		t.Fatal("expected request within TPM limit to be allowed")
	}

	// Second request with 300 tokens: 300 + 300 = 600 > 500: blocked
	allowed, msg := rl.Allow(id, 0, limitTPM, 300)
	if allowed {
		t.Fatal("expected request exceeding TPM limit to be blocked")
	}
	if msg == "" {
		t.Error("expected rate limit error message")
	}
}

func TestRateLimiter_Used(t *testing.T) {
	rl := NewRateLimiter()
	if used, reset := rl.Used("never-seen"); used != 0 || reset != 60 {
		t.Errorf("expected 0 used / 60s reset for unknown id, got %d / %d", used, reset)
	}
	for i := 0; i < 2; i++ {
		rl.Allow("used-id", 10, 0, 0)
	}
	used, reset := rl.Used("used-id")
	if used != 2 {
		t.Errorf("expected 2 used, got %d", used)
	}
	if reset <= 0 || reset > 60 {
		t.Errorf("expected reset within 1..60s, got %d", reset)
	}
}

func TestSetRateLimitHeaders(t *testing.T) {
	p := NewGatewayProxy(&config.Config{}, nil)
	key := &entity.APIKey{ID: "k1", RateLimitRPM: 10}
	user := &entity.User{ID: "u1"}

	// Unlimited user + limited key -> key budget advertised.
	rec := httptest.NewRecorder()
	p.setRateLimitHeaders(rec, key, user)
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "10" {
		t.Fatalf("expected limit 10, got %q", got)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "10" {
		t.Errorf("expected remaining 10, got %q", got)
	}

	// Tighter user budget wins.
	user.RateLimitRPM = 3
	for i := 0; i < 2; i++ {
		p.rateLimiter.Allow("user:u1", user.RateLimitRPM, 0, 0)
	}
	rec = httptest.NewRecorder()
	p.setRateLimitHeaders(rec, key, user)
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "3" {
		t.Errorf("expected tighter user limit 3, got %q", got)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "1" {
		t.Errorf("expected remaining 1, got %q", got)
	}

	// No limits -> no headers.
	rec = httptest.NewRecorder()
	p.setRateLimitHeaders(rec, &entity.APIKey{ID: "k2"}, &entity.User{ID: "u2"})
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "" {
		t.Errorf("expected no header for unlimited caller, got %q", got)
	}
}
