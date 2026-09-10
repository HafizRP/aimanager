package proxy

import (
	"sync"
	"time"
)

type clientBucket struct {
	windowStart time.Time
	reqCount    int
	tokenCount  int64
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*clientBucket
}

func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*clientBucket),
	}
	// Cleanup routine every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			rl.mu.Lock()
			now := time.Now()
			for k, b := range rl.buckets {
				if now.Sub(b.windowStart) > 2*time.Minute {
					delete(rl.buckets, k)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

// Allow checks if the request is within rate limits (RPM and TPM).
// limitRPM <= 0 means unlimited. limitTPM <= 0 means unlimited.
func (rl *RateLimiter) Allow(identifier string, limitRPM int, limitTPM int64, estTokens int64) (bool, string) {
	if limitRPM <= 0 && limitTPM <= 0 {
		return true, ""
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.buckets[identifier]
	if !exists || now.Sub(b.windowStart) >= time.Minute {
		b = &clientBucket{
			windowStart: now,
			reqCount:    0,
			tokenCount:  0,
		}
		rl.buckets[identifier] = b
	}

	if limitRPM > 0 && b.reqCount >= limitRPM {
		return false, "Rate limit exceeded (Requests Per Minute limit reached). Please wait a moment."
	}

	if limitTPM > 0 && (b.tokenCount+estTokens) > limitTPM {
		return false, "Rate limit exceeded (Tokens Per Minute limit reached). Please wait a moment."
	}

	b.reqCount++
	b.tokenCount += estTokens
	return true, ""
}
