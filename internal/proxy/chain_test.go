package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"9router-gateway/internal/entity"
)

type dummyStepMiddleware struct {
	name      string
	executed  *[]string
	abortWith error
	shortCirc bool
}

func (m *dummyStepMiddleware) Name() string {
	return m.name
}

func (m *dummyStepMiddleware) Handle(ctx *PipelineContext, next NextFunc) error {
	*m.executed = append(*m.executed, m.name)
	if m.shortCirc {
		ctx.WriteJSONError(http.StatusForbidden, "short circuit from "+m.name, "forbidden")
		return nil
	}
	if m.abortWith != nil {
		return m.abortWith
	}
	return next(ctx)
}

func TestPipeline_SequentialExecution(t *testing.T) {
	executed := []string{}
	p := NewPipeline(
		&dummyStepMiddleware{name: "step1", executed: &executed},
		&dummyStepMiddleware{name: "step2", executed: &executed},
		&dummyStepMiddleware{name: "step3", executed: &executed},
	)

	pctx := &PipelineContext{
		Context:   context.Background(),
		Writer:    httptest.NewRecorder(),
		Request:   httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil),
		StartTime: time.Now(),
	}

	err := p.Execute(pctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(executed) != 3 {
		t.Fatalf("expected 3 executed steps, got %d", len(executed))
	}
	if executed[0] != "step1" || executed[1] != "step2" || executed[2] != "step3" {
		t.Errorf("unexpected execution order: %v", executed)
	}
}

func TestPipeline_ShortCircuit(t *testing.T) {
	executed := []string{}
	p := NewPipeline(
		&dummyStepMiddleware{name: "step1", executed: &executed},
		&dummyStepMiddleware{name: "step2_blocks", executed: &executed, shortCirc: true},
		&dummyStepMiddleware{name: "step3_never_reached", executed: &executed},
	)

	w := httptest.NewRecorder()
	pctx := &PipelineContext{
		Context:   context.Background(),
		Writer:    w,
		Request:   httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil),
		StartTime: time.Now(),
	}

	err := p.Execute(pctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(executed) != 2 {
		t.Fatalf("expected exactly 2 steps executed before short-circuit, got %d: %v", len(executed), executed)
	}
	if !pctx.ShortCircuited {
		t.Error("expected context to be marked ShortCircuited")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 status code from short circuit, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_Chain(t *testing.T) {
	limiter := NewRateLimiter()
	mw := NewRateLimitMiddleware(limiter)

	key := &entity.APIKey{
		ID:           "test-key-limit",
		RateLimitRPM: 1,
	}

	// First request: should proceed
	w1 := httptest.NewRecorder()
	ctx1 := &PipelineContext{
		Context: context.Background(),
		Writer:  w1,
		APIKey:  key,
	}
	proceeded1 := false
	_ = mw.Handle(ctx1, func(c *PipelineContext) error {
		proceeded1 = true
		return nil
	})
	if !proceeded1 {
		t.Fatal("expected first request to proceed")
	}

	// Second request within same minute: should be rejected with 429
	w2 := httptest.NewRecorder()
	ctx2 := &PipelineContext{
		Context: context.Background(),
		Writer:  w2,
		APIKey:  key,
	}
	proceeded2 := false
	_ = mw.Handle(ctx2, func(c *PipelineContext) error {
		proceeded2 = true
		return nil
	})
	if proceeded2 {
		t.Fatal("expected second request to be rejected by rate limiter")
	}
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests, got %d", w2.Code)
	}
}
