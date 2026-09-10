package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"9router-gateway/internal/config"
	"9router-gateway/internal/database"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/proxy"
	"9router-gateway/internal/repository"
)

func TestSafeRedirectURL(t *testing.T) {
	tests := []struct {
		input    string
		fallback string
		expected string
	}{
		{"/users", "/dashboard", "/users"},
		{"/keys?msg=ok", "/dashboard", "/keys?msg=ok"},
		{"https://evil.com", "/dashboard", "/dashboard"},
		{"//evil.com", "/dashboard", "/dashboard"},
		{`/\evil.com`, "/dashboard", "/dashboard"},
		{"", "/dashboard", "/dashboard"},
		{"   ", "/dashboard", "/dashboard"},
		{"javascript:alert(1)", "/dashboard", "/dashboard"},
	}

	for _, tc := range tests {
		res := safeRedirectURL(tc.input, tc.fallback)
		if res != tc.expected {
			t.Errorf("safeRedirectURL(%q, %q) = %q, want %q", tc.input, tc.fallback, res, tc.expected)
		}
	}
}

func TestCSRFValidation(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{
			SessionSecret: "test-secret-key-12345",
		},
	}

	sessionCookie := &http.Cookie{
		Name:  sessionCookieName,
		Value: "dummy-session-token-32-bytes-long",
	}

	req := httptest.NewRequest(http.MethodPost, "/keys", nil)
	req.AddCookie(sessionCookie)

	expectedToken := h.getCSRFToken(req)
	if expectedToken == "" {
		t.Fatalf("expected non-empty CSRF token")
	}

	// Missing CSRF token -> 403 Forbidden
	rr := httptest.NewRecorder()
	testHandler := h.ValidateCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	testHandler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for missing CSRF token, got: %d", rr.Code)
	}

	// Valid header token -> 200 OK
	reqHeader := httptest.NewRequest(http.MethodPost, "/keys", nil)
	reqHeader.AddCookie(sessionCookie)
	reqHeader.Header.Set("X-CSRF-Token", expectedToken)

	rrHeader := httptest.NewRecorder()
	testHandler.ServeHTTP(rrHeader, reqHeader)
	if rrHeader.Code != http.StatusOK {
		t.Errorf("expected 200 OK for valid CSRF token header, got: %d", rrHeader.Code)
	}

	// Valid form value token -> 200 OK
	reqForm := httptest.NewRequest(http.MethodPost, "/keys?csrf_token="+expectedToken, nil)
	reqForm.AddCookie(sessionCookie)

	rrForm := httptest.NewRecorder()
	testHandler.ServeHTTP(rrForm, reqForm)
	if rrForm.Code != http.StatusOK {
		t.Errorf("expected 200 OK for valid CSRF token in form, got: %d", rrForm.Code)
	}
}

func TestNewHandler_TemplatesLoad(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-32-character-token-key",
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	if len(h.templates) < 15 {
		t.Errorf("expected at least 15 loaded templates, got %d", len(h.templates))
	}
	if _, ok := h.templates["login.html"]; !ok {
		t.Errorf("expected login.html template to be loaded")
	}
	if _, ok := h.templates["dashboard.html"]; !ok {
		t.Errorf("expected dashboard.html template to be loaded")
	}
}

func TestUpdateUpstreamPost_Admin(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		UpstreamURL:      "http://127.0.0.1:20128",
		NineRouterDBPath: "./data/core/db/data.sqlite",
		UpstreamAPIKey:   "",
		SessionSecret:    "test-secret-32-character-token-key",
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	form := url.Values{}
	form.Set("upstream_url", "http://10.0.0.88:20128/ ")
	form.Set("ninerouter_db_path", "/custom/core.sqlite")
	form.Set("upstream_api_key", "secret-upstream-key")

	req := httptest.NewRequest(http.MethodPost, "/settings/upstream", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), userContextKey, &entity.User{Role: "admin"})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.UpdateUpstreamPost(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected status 303, got %d", rr.Code)
	}

	// Verify runtime config was updated
	if cfg.GetUpstreamURL() != "http://10.0.0.88:20128" {
		t.Errorf("expected updated UpstreamURL 'http://10.0.0.88:20128', got '%s'", cfg.GetUpstreamURL())
	}
	if cfg.GetNineRouterDBPath() != "/custom/core.sqlite" {
		t.Errorf("expected updated NineRouterDBPath '/custom/core.sqlite', got '%s'", cfg.GetNineRouterDBPath())
	}
	if cfg.GetUpstreamAPIKey() != "secret-upstream-key" {
		t.Errorf("expected updated UpstreamAPIKey 'secret-upstream-key', got '%s'", cfg.GetUpstreamAPIKey())
	}

	// Verify database persistence
	savedURL, err := repo.GetSetting(context.Background(), "upstream_url")
	if err != nil || savedURL != "http://10.0.0.88:20128" {
		t.Errorf("expected database upstream_url 'http://10.0.0.88:20128', got '%s' (err: %v)", savedURL, err)
	}

	savedDB, err := repo.GetSetting(context.Background(), "ninerouter_db_path")
	if err != nil || savedDB != "/custom/core.sqlite" {
		t.Errorf("expected database ninerouter_db_path '/custom/core.sqlite', got '%s' (err: %v)", savedDB, err)
	}

	savedKey, err := repo.GetSetting(context.Background(), "upstream_api_key")
	if err != nil || savedKey != "secret-upstream-key" {
		t.Errorf("expected database upstream_api_key 'secret-upstream-key', got '%s' (err: %v)", savedKey, err)
	}
}

func TestUpdateUpstreamPost_Validation(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		UpstreamURL:   "http://127.0.0.1:20128",
		SessionSecret: "test-secret-32-character-token-key",
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	// 1. Empty URL
	formEmpty := url.Values{}
	formEmpty.Set("upstream_url", "")
	reqEmpty := httptest.NewRequest(http.MethodPost, "/settings/upstream", strings.NewReader(formEmpty.Encode()))
	reqEmpty.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqEmpty = reqEmpty.WithContext(context.WithValue(reqEmpty.Context(), userContextKey, &entity.User{Role: "admin"}))

	rrEmpty := httptest.NewRecorder()
	h.UpdateUpstreamPost(rrEmpty, reqEmpty)
	if !strings.Contains(rrEmpty.Header().Get("Location"), "Upstream+URL+cannot+be+empty") {
		t.Errorf("expected empty URL error redirect, got %s", rrEmpty.Header().Get("Location"))
	}

	// 2. Invalid Scheme
	formInvalid := url.Values{}
	formInvalid.Set("upstream_url", "ftp://127.0.0.1:20128")
	reqInvalid := httptest.NewRequest(http.MethodPost, "/settings/upstream", strings.NewReader(formInvalid.Encode()))
	reqInvalid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqInvalid = reqInvalid.WithContext(context.WithValue(reqInvalid.Context(), userContextKey, &entity.User{Role: "admin"}))

	rrInvalid := httptest.NewRecorder()
	h.UpdateUpstreamPost(rrInvalid, reqInvalid)
	if !strings.Contains(rrInvalid.Header().Get("Location"), "Invalid+upstream+URL") {
		t.Errorf("expected invalid URL scheme error redirect, got %s", rrInvalid.Header().Get("Location"))
	}

	// 3. Unauthorized non-admin user
	formNormal := url.Values{}
	formNormal.Set("upstream_url", "http://127.0.0.1:20128")
	reqUser := httptest.NewRequest(http.MethodPost, "/settings/upstream", strings.NewReader(formNormal.Encode()))
	reqUser.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqUser = reqUser.WithContext(context.WithValue(reqUser.Context(), userContextKey, &entity.User{Role: "user"}))

	rrUser := httptest.NewRecorder()
	h.UpdateUpstreamPost(rrUser, reqUser)
	if !strings.Contains(rrUser.Header().Get("Location"), "Unauthorized") {
		t.Errorf("expected Unauthorized redirect for normal user, got %s", rrUser.Header().Get("Location"))
	}
}

func TestAPICacheStats(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-32-character-token-key",
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	// GET returns fresh stats (all zero)
	req := httptest.NewRequest(http.MethodGet, "/api/cache/stats", nil)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, &entity.User{Role: "admin"}))
	rr := httptest.NewRecorder()
	h.APICacheStats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var stats proxy.CacheStats
	if err := json.NewDecoder(rr.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode stats: %v", err)
	}
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Errorf("expected zero initial stats, got hits=%d misses=%d", stats.Hits, stats.Misses)
	}

	// DELETE resets (no crash)
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/cache/stats", nil)
	reqDel = reqDel.WithContext(context.WithValue(reqDel.Context(), userContextKey, &entity.User{Role: "admin"}))
	rrDel := httptest.NewRecorder()
	h.APICacheStats(rrDel, reqDel)
	if rrDel.Code != http.StatusOK {
		t.Fatalf("expected 200 on reset, got %d", rrDel.Code)
	}
}

func TestTestUpstreamConnection(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		UpstreamURL:   mockServer.URL,
		SessionSecret: "test-secret-32-character-token-key",
	}

	h, err := NewHandler(cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	// Test default configured URL
	req := httptest.NewRequest(http.MethodGet, "/api/upstream/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, &entity.User{Role: "admin"}))

	rr := httptest.NewRecorder()
	h.TestUpstreamConnection(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["success"] != true {
		t.Errorf("expected success true, got %v", resp["success"])
	}

	// Test candidate URL parameter
	reqCandidate := httptest.NewRequest(http.MethodGet, "/api/upstream/test?url="+url.QueryEscape(mockServer.URL), nil)
	reqCandidate = reqCandidate.WithContext(context.WithValue(reqCandidate.Context(), userContextKey, &entity.User{Role: "admin"}))

	rrCandidate := httptest.NewRecorder()
	h.TestUpstreamConnection(rrCandidate, reqCandidate)

	var respCandidate map[string]interface{}
	if err := json.NewDecoder(rrCandidate.Body).Decode(&respCandidate); err != nil {
		t.Fatalf("failed to decode candidate response: %v", err)
	}
	if respCandidate["success"] != true {
		t.Errorf("expected candidate success true, got %v", respCandidate["success"])
	}
}
