package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"9router-gateway/internal/database"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/repository"
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

func TestAuthMiddleware_IPWhitelist(t *testing.T) {
	// Setup repo with user and key having AllowedIPs
	db, err := database.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	ctx := context.Background()

	user := &entity.User{
		ID:           "u-ip-test",
		Username:     "iptest",
		Name:         "IP Test",
		PasswordHash: "hash123",
		Role:         "user",
		IsActive:     true,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	key := &entity.APIKey{
		ID:         "k-ip-test",
		UserID:     user.ID,
		Key:        "sk-gw-iptest12345",
		Name:       "IP Test Key",
		AllowedIPs: "192.168.1.50, 10.0.0.0/24",
		IsActive:   true,
	}
	if err := repo.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	mw := NewAuthMiddleware(repo)

	// Allowed IP (192.168.1.50)
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx1 := &PipelineContext{
		Context:   ctx,
		Writer:    w1,
		Request:   req1,
		RawAPIKey: key.Key,
		ClientIP:  "192.168.1.50",
	}
	allowedProceeded := false
	_ = mw.Handle(ctx1, func(c *PipelineContext) error {
		allowedProceeded = true
		return nil
	})
	if !allowedProceeded {
		t.Fatal("expected request from allowed IP to proceed")
	}

	// Blocked IP (203.0.113.10)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx2 := &PipelineContext{
		Context:   ctx,
		Writer:    w2,
		Request:   req2,
		RawAPIKey: key.Key,
		ClientIP:  "203.0.113.10",
	}
	blockedProceeded := false
	_ = mw.Handle(ctx2, func(c *PipelineContext) error {
		blockedProceeded = true
		return nil
	})
	if blockedProceeded {
		t.Fatal("expected request from unauthorized IP to be blocked")
	}
	if w2.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", w2.Code)
	}
	if ctx2.ErrorType != "unauthorized_client_ip" {
		t.Errorf("expected errorType 'unauthorized_client_ip', got %q", ctx2.ErrorType)
	}
}
