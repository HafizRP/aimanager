package proxy

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"9router-gateway/internal/config"
)

// flakyListener accepts the first closeN connections and immediately drops them
// (simulating a core that is still booting), then serves httpHandler.
func flakyListener(t *testing.T, closeN int, httpHandler http.Handler) (addr string, done func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stopped := make(chan struct{})
	go func() {
		for i := 0; i < closeN; i++ {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
		srv := &http.Server{Handler: httpHandler}
		go func() { _ = srv.Serve(ln) }()
		<-stopped
		_ = srv.Close()
	}()
	return ln.Addr().String(), func() { close(stopped) }
}

func fastRetries(t *testing.T) {
	t.Helper()
	orig := transportRetryDelays
	transportRetryDelays = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond}
	t.Cleanup(func() { transportRetryDelays = orig })
}

func TestDoUpstreamWithRetry_RecoversWithinBootWindow(t *testing.T) {
	fastRetries(t)
	addr, stop := flakyListener(t, 2, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer stop()

	p := NewGatewayProxy(&config.Config{}, nil)
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/chat/completions", nil)
	resp, attempts := p.doUpstreamWithRetry(req, []byte(`{}`), nil)
	if resp == nil {
		t.Fatalf("expected recovery after 2 dropped connections, got nil response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDoUpstreamWithRetry_TransportExhaustedRecordsOneFailure(t *testing.T) {
	fastRetries(t)
	addr, stop := flakyListener(t, 10, http.NotFoundHandler())
	defer stop()

	cb := NewCircuitBreaker(CircuitBreakerSettings{Name: "retry-exhausted", FailureThreshold: 3})
	p := NewGatewayProxy(&config.Config{}, nil)
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/chat/completions", nil)
	resp, attempts := p.doUpstreamWithRetry(req, []byte(`{}`), cb)
	if resp != nil {
		t.Fatalf("expected nil response when all attempts fail")
	}
	if attempts != 4 {
		t.Fatalf("expected 1 initial + 3 retries = 4 attempts, got %d", attempts)
	}
	// One request must not trip a threshold-3 breaker by itself.
	if cb.State() != StateClosed {
		t.Fatalf("expected breaker to stay Closed after a single exhausted request, got %s", cb.State())
	}
}

func TestDoUpstreamWithRetry_StatusRetriedOnce(t *testing.T) {
	fastRetries(t)
	var hits int
	addr, stop := flakyListener(t, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "booting")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer stop()

	p := NewGatewayProxy(&config.Config{}, nil)
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/chat/completions", nil)
	resp, attempts := p.doUpstreamWithRetry(req, []byte(`{}`), nil)
	if resp == nil {
		t.Fatal("expected retry to recover from a single 503")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || attempts != 2 {
		t.Fatalf("expected 200 after 2 attempts, got status=%d attempts=%d", resp.StatusCode, attempts)
	}
}
