package proxy

import (
	"testing"
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
