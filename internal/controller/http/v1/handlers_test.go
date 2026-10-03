package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"9router-gateway/internal/config"
	"9router-gateway/internal/database"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/proxy"
	"9router-gateway/internal/repository"
	"9router-gateway/internal/upstream"
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

func TestThemeGetSet(t *testing.T) {
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
	ctx := context.WithValue(context.Background(), userContextKey, &entity.User{Role: "admin"})

	// Default is dark
	req := httptest.NewRequest(http.MethodGet, "/api/theme", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	h.GetTheme(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on GetTheme, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode theme response: %v", err)
	}
	if resp["theme"] != "dark" {
		t.Errorf("expected default theme dark, got %v", resp["theme"])
	}

	// Set to light
	form := url.Values{}
	form.Set("theme", "light")
	reqSet := httptest.NewRequest(http.MethodPost, "/api/theme", strings.NewReader(form.Encode())).WithContext(ctx)
	reqSet.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rrSet := httptest.NewRecorder()
	h.SetTheme(rrSet, reqSet)
	if rrSet.Code != http.StatusOK {
		t.Fatalf("expected 200 on SetTheme, got %d", rrSet.Code)
	}
	var respSet map[string]interface{}
	if err := json.NewDecoder(rrSet.Body).Decode(&respSet); err != nil {
		t.Fatalf("failed to decode set-theme response: %v", err)
	}
	if respSet["success"] != true || respSet["theme"] != "light" {
		t.Errorf("expected success+light, got %v", respSet)
	}

	// Persisted value is returned
	req2 := httptest.NewRequest(http.MethodGet, "/api/theme", nil).WithContext(ctx)
	rr2 := httptest.NewRecorder()
	h.GetTheme(rr2, req2)
	var resp2 map[string]interface{}
	if err := json.NewDecoder(rr2.Body).Decode(&resp2); err != nil {
		t.Fatalf("failed to decode theme response: %v", err)
	}
	if resp2["theme"] != "light" {
		t.Errorf("expected persisted theme light, got %v", resp2["theme"])
	}

	// Invalid theme rejected
	formBad := url.Values{}
	formBad.Set("theme", "neon")
	reqBad := httptest.NewRequest(http.MethodPost, "/api/theme", strings.NewReader(formBad.Encode())).WithContext(ctx)
	reqBad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rrBad := httptest.NewRecorder()
	h.SetTheme(rrBad, reqBad)
	if rrBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on invalid theme, got %d", rrBad.Code)
	}
}

func TestAPILoginAudits(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	h := &Handler{repo: repo}
	ctx := context.Background()

	adminUser := &entity.User{
		ID:       "u-admin",
		Username: "admin",
		Name:     "Admin User",
		Role:     "admin",
		IsActive: true,
	}
	regularUser := &entity.User{
		ID:       "user-audit-test",
		Username: "audituser",
		Name:     "Audit User",
		Role:     "user",
		IsActive: true,
	}
	_ = repo.CreateUser(ctx, adminUser)
	_ = repo.CreateUser(ctx, regularUser)

	// Seed audits
	uid := "user-audit-test"
	_ = repo.RecordLoginAudit(ctx, &entity.LoginAudit{
		UserID:    &uid,
		Username:  "audituser",
		IP:        "10.0.0.99",
		UserAgent: "Mozilla/5.0",
		Status:    "success",
		Reason:    "auth ok",
	})
	_ = repo.RecordLoginAudit(ctx, &entity.LoginAudit{
		Username:  "baduser",
		IP:        "10.0.0.88",
		UserAgent: "curl/8.0",
		Status:    "failed",
		Reason:    "user not found",
	})

	// 1. APILoginAudits (all as admin)
	req := httptest.NewRequest(http.MethodGet, "/api/login-audits?limit=10", nil)
	req = req.WithContext(context.WithValue(ctx, userContextKey, adminUser))
	rr := httptest.NewRecorder()
	h.APILoginAudits(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("APILoginAudits returned %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["total"].(float64) < 2 {
		t.Errorf("expected at least 2 audits in total, got %v", resp["total"])
	}

	// 2. APILoginAudits filtered by status as admin
	reqFilter := httptest.NewRequest(http.MethodGet, "/api/login-audits?status=failed", nil)
	reqFilter = reqFilter.WithContext(context.WithValue(ctx, userContextKey, adminUser))
	rrFilter := httptest.NewRecorder()
	h.APILoginAudits(rrFilter, reqFilter)
	if rrFilter.Code != http.StatusOK {
		t.Fatalf("APILoginAudits filtered returned %d", rrFilter.Code)
	}
	var filterResp map[string]interface{}
	if err := json.NewDecoder(rrFilter.Body).Decode(&filterResp); err != nil {
		t.Fatalf("decode filterResp failed: %v", err)
	}
	if filterResp["total"].(float64) != 1 {
		t.Errorf("expected 1 failed audit, got %v", filterResp["total"])
	}

	// 3. APILoginAudits as non-admin user (should only see own audits)
	reqUserSelf := httptest.NewRequest(http.MethodGet, "/api/login-audits", nil)
	reqUserSelf = reqUserSelf.WithContext(context.WithValue(ctx, userContextKey, regularUser))
	rrUserSelf := httptest.NewRecorder()
	h.APILoginAudits(rrUserSelf, reqUserSelf)
	if rrUserSelf.Code != http.StatusOK {
		t.Fatalf("APILoginAudits as regular user returned %d", rrUserSelf.Code)
	}
	var userSelfResp map[string]interface{}
	if err := json.NewDecoder(rrUserSelf.Body).Decode(&userSelfResp); err != nil {
		t.Fatalf("decode userSelfResp failed: %v", err)
	}
	if userSelfResp["total"].(float64) != 1 {
		t.Errorf("expected regular user to only see 1 audit, got %v", userSelfResp["total"])
	}

	// 4. APIUserLoginAudits
	reqUser := httptest.NewRequest(http.MethodGet, "/api/users/user-audit-test/login-audits", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "user-audit-test")
	reqUser = reqUser.WithContext(context.WithValue(context.WithValue(ctx, userContextKey, adminUser), chi.RouteCtxKey, rctx))

	rrUser := httptest.NewRecorder()
	h.APIUserLoginAudits(rrUser, reqUser)
	if rrUser.Code != http.StatusOK {
		t.Fatalf("APIUserLoginAudits returned %d", rrUser.Code)
	}
	var userAudits []entity.LoginAudit
	if err := json.NewDecoder(rrUser.Body).Decode(&userAudits); err != nil {
		t.Fatalf("decode user audits failed: %v", err)
	}
	if len(userAudits) != 1 || userAudits[0].Username != "audituser" {
		t.Errorf("unexpected user audits response: %+v", userAudits)
	}

	// 5. Unauthenticated returns 401
	reqUnauth := httptest.NewRequest(http.MethodGet, "/api/login-audits", nil)
	rrUnauth := httptest.NewRecorder()
	h.APILoginAudits(rrUnauth, reqUnauth)
	if rrUnauth.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on unauthenticated APILoginAudits, got %d", rrUnauth.Code)
	}
}

func TestExportLoginAudits(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test_export.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	h := &Handler{repo: repo}
	ctx := context.Background()

	adminUser := &entity.User{
		ID:       "u-admin-export",
		Username: "admin",
		Name:     "Admin User",
		Role:     "admin",
		IsActive: true,
	}
	_ = repo.CreateUser(ctx, adminUser)

	uid := "u-admin-export"
	_ = repo.RecordLoginAudit(ctx, &entity.LoginAudit{
		UserID:    &uid,
		Username:  "admin",
		IP:        "127.0.0.1",
		UserAgent: "Mozilla/5.0",
		Status:    "success",
		Reason:    "auth ok",
	})

	// 1. Export CSV
	reqCSV := httptest.NewRequest(http.MethodGet, "/api/login-audits/export?format=csv", nil)
	reqCSV = reqCSV.WithContext(context.WithValue(ctx, userContextKey, adminUser))
	rrCSV := httptest.NewRecorder()
	h.ExportLoginAudits(rrCSV, reqCSV)
	if rrCSV.Code != http.StatusOK {
		t.Fatalf("ExportLoginAudits CSV returned %d", rrCSV.Code)
	}
	csvBody := rrCSV.Body.String()
	if !strings.Contains(csvBody, "Timestamp_WIB") || !strings.Contains(csvBody, "admin") {
		t.Errorf("unexpected CSV export output: %s", csvBody)
	}

	// 2. Export JSON
	reqJSON := httptest.NewRequest(http.MethodGet, "/api/login-audits/export?format=json", nil)
	reqJSON = reqJSON.WithContext(context.WithValue(ctx, userContextKey, adminUser))
	rrJSON := httptest.NewRecorder()
	h.ExportLoginAudits(rrJSON, reqJSON)
	if rrJSON.Code != http.StatusOK {
		t.Fatalf("ExportLoginAudits JSON returned %d", rrJSON.Code)
	}
	var exported []entity.LoginAudit
	if err := json.NewDecoder(rrJSON.Body).Decode(&exported); err != nil {
		t.Fatalf("failed to decode exported JSON: %v", err)
	}
	if len(exported) != 1 || exported[0].Username != "admin" {
		t.Errorf("unexpected JSON export data: %+v", exported)
	}
}

func TestLandingAndRegister(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "secret-12345678901234567890123456789012",
		AdminUsername: "admin",
		AdminPassword: "adminpassword",
	}
	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	// 1. LandingPage
	reqLanding := httptest.NewRequest(http.MethodGet, "/landing", nil)
	rrLanding := httptest.NewRecorder()
	h.LandingPage(rrLanding, reqLanding)
	if rrLanding.Code != http.StatusOK {
		t.Fatalf("LandingPage returned %d, want 200", rrLanding.Code)
	}
	if !strings.Contains(rrLanding.Body.String(), "AI Manager") {
		t.Errorf("LandingPage body does not contain 'AI Manager'")
	}

	// 2. RegisterPage
	reqReg := httptest.NewRequest(http.MethodGet, "/register", nil)
	rrReg := httptest.NewRecorder()
	h.RegisterPage(rrReg, reqReg)
	if rrReg.Code != http.StatusOK {
		t.Fatalf("RegisterPage returned %d, want 200", rrReg.Code)
	}
	if !strings.Contains(rrReg.Body.String(), "Bikin Akun Baru") {
		t.Errorf("RegisterPage body does not contain 'Bikin Akun Baru'")
	}

	// 3. RegisterPost - Validation failures
	// Empty name
	formBadName := url.Values{
		"name":             {""},
		"username":         {"testuser"},
		"password":         {"pass123"},
		"confirm_password": {"pass123"},
	}
	reqBadName := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(formBadName.Encode()))
	reqBadName.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rrBadName := httptest.NewRecorder()
	h.RegisterPost(rrBadName, reqBadName)
	if rrBadName.Code != http.StatusSeeOther || !strings.Contains(rrBadName.Header().Get("Location"), "error") {
		t.Errorf("expected redirect with error for empty name, got code=%d loc=%s", rrBadName.Code, rrBadName.Header().Get("Location"))
	}

	// Password mismatch
	formMismatch := url.Values{
		"name":             {"Test User"},
		"username":         {"testuser"},
		"password":         {"pass123"},
		"confirm_password": {"mismatch456"},
	}
	reqMismatch := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(formMismatch.Encode()))
	reqMismatch.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rrMismatch := httptest.NewRecorder()
	h.RegisterPost(rrMismatch, reqMismatch)
	if rrMismatch.Code != http.StatusSeeOther || !strings.Contains(rrMismatch.Header().Get("Location"), "error") {
		t.Errorf("expected redirect with error for password mismatch, got code=%d loc=%s", rrMismatch.Code, rrMismatch.Header().Get("Location"))
	}

	// 4. RegisterPost - Success
	formOK := url.Values{
		"name":             {"Test Developer"},
		"username":         {"testdev"},
		"password":         {"securepass123"},
		"confirm_password": {"securepass123"},
	}
	reqOK := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(formOK.Encode()))
	reqOK.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rrOK := httptest.NewRecorder()
	h.RegisterPost(rrOK, reqOK)
	if rrOK.Code != http.StatusSeeOther || rrOK.Header().Get("Location") != "/" {
		t.Fatalf("expected redirect to / on successful registration, got code=%d loc=%s", rrOK.Code, rrOK.Header().Get("Location"))
	}

	// Verify session cookie was set
	cookies := rrOK.Result().Cookies()
	var foundSession bool
	for _, c := range cookies {
		if c.Name == sessionCookieName && c.Value != "" {
			foundSession = true
			break
		}
	}
	if !foundSession {
		t.Errorf("expected session cookie %s to be set after registration", sessionCookieName)
	}

	// Verify user persisted in repository
	createdUser, err := repo.GetUserByUsername(context.Background(), "testdev")
	if err != nil || createdUser == nil {
		t.Fatalf("expected user 'testdev' to exist in repo, err=%v", err)
	}
	if createdUser.Role != "user" || createdUser.TokenQuota != 1000000 {
		t.Errorf("unexpected user attributes: role=%s quota=%d", createdUser.Role, createdUser.TokenQuota)
	}

	// Verify API key was created
	keys, err := repo.GetAPIKeysByUserID(context.Background(), createdUser.ID)
	if err != nil || len(keys) == 0 {
		t.Fatalf("expected initial API key to be created for user, err=%v", err)
	}
	if !strings.HasPrefix(keys[0].Key, "sk-gw-") {
		t.Errorf("expected API key prefix sk-gw-, got %s", keys[0].Key)
	}
}

func TestAPIKeyRestrictionsAndIPWhitelist(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_keys.db")
	db, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-12345678901234567890",
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	ctx := context.Background()
	user := &entity.User{
		ID:           "u-ip-test",
		Username:     "keytester",
		Name:         "Key Tester",
		PasswordHash: "hash123",
		Role:         "admin",
		IsActive:     true,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// 1. CreateKey with AllowedIPs
	form := url.Values{
		"user_id":       {user.ID},
		"name":          {"Restricted Key"},
		"allowed_ips":   {"192.168.1.10, 10.0.0.0/8"},
		"allowed_models": {"main"},
		"rate_limit_rpm": {"60"},
	}
	req := httptest.NewRequest(http.MethodPost, "/keys", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(ctx, userContextKey, user))
	rr := httptest.NewRecorder()

	h.CreateKey(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("CreateKey returned status %d, want 303", rr.Code)
	}

	keys, err := repo.GetAPIKeysByUserID(ctx, user.ID)
	if err != nil || len(keys) == 0 {
		t.Fatalf("expected created key in repo, err=%v", err)
	}
	key := keys[0]
	if key.AllowedIPs != "192.168.1.10, 10.0.0.0/8" {
		t.Errorf("expected AllowedIPs '192.168.1.10, 10.0.0.0/8', got %q", key.AllowedIPs)
	}

	// 2. Update restrictions via APIKeyBudget
	bodyJSON := `{"max_tokens_limit": 500000, "daily_token_quota": 50000, "allowed_ips": "172.16.0.0/12, 127.0.0.1"}`
	reqBudget := httptest.NewRequest(http.MethodPost, "/api/keys/"+key.ID+"/budget", strings.NewReader(bodyJSON))
	reqBudget.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", key.ID)
	reqBudget = reqBudget.WithContext(context.WithValue(reqBudget.Context(), chi.RouteCtxKey, rctx))
	rrBudget := httptest.NewRecorder()

	h.APIKeyBudget(rrBudget, reqBudget)
	if rrBudget.Code != http.StatusOK {
		t.Fatalf("APIKeyBudget returned status %d, want 200", rrBudget.Code)
	}

	// Fetch updated key
	updatedKey, err := repo.GetAPIKeyByKey(ctx, key.Key)
	if err != nil {
		t.Fatalf("GetAPIKeyByKey failed: %v", err)
	}
	if updatedKey.MaxTokensLimit != 500000 {
		t.Errorf("expected MaxTokensLimit 500000, got %d", updatedKey.MaxTokensLimit)
	}
	if updatedKey.DailyTokenQuota != 50000 {
		t.Errorf("expected DailyTokenQuota 50000, got %d", updatedKey.DailyTokenQuota)
	}
	if updatedKey.AllowedIPs != "172.16.0.0/12, 127.0.0.1" {
		t.Errorf("expected AllowedIPs '172.16.0.0/12, 127.0.0.1', got %q", updatedKey.AllowedIPs)
	}
}

func TestEditKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_edit_keys.db")
	db, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-12345678901234567890",
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	ctx := context.Background()
	owner := &entity.User{
		ID:           "u-owner",
		Username:     "keyowner",
		Name:         "Key Owner",
		PasswordHash: "hash123",
		Role:         "user",
		IsActive:     true,
	}
	otherUser := &entity.User{
		ID:           "u-other",
		Username:     "otheruser",
		Name:         "Other User",
		PasswordHash: "hash123",
		Role:         "user",
		IsActive:     true,
	}
	adminUser := &entity.User{
		ID:           "u-admin",
		Username:     "adminuser",
		Name:         "Admin User",
		PasswordHash: "hash123",
		Role:         "admin",
		IsActive:     true,
	}

	_ = repo.CreateUser(ctx, owner)
	_ = repo.CreateUser(ctx, otherUser)
	_ = repo.CreateUser(ctx, adminUser)

	key := &entity.APIKey{
		ID:            "k-edit-1",
		UserID:        owner.ID,
		Key:           "sk-gw-test-edit-key-12345",
		Name:          "Original Key Name",
		AllowedModels: `["main"]`,
		RateLimitRPM:  30,
		IsActive:      true,
	}
	if err := repo.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	// 1. Owner updates key via Form POST
	form := url.Values{
		"name":              {"Modified Key Name"},
		"allowed_models":    {"main, ag/claude-sonnet-4-6"},
		"allowed_ips":       {"192.168.1.100, 10.0.0.0/16"},
		"rate_limit_rpm":    {"90"},
		"max_tokens_limit":  {"1500000"},
		"daily_token_quota": {"75000"},
		"expires_at":        {"2026-12-31T23:59"},
	}
	reqForm := httptest.NewRequest(http.MethodPost, "/keys/"+key.ID+"/edit", strings.NewReader(form.Encode()))
	reqForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", key.ID)
	reqForm = reqForm.WithContext(context.WithValue(context.WithValue(ctx, userContextKey, owner), chi.RouteCtxKey, rctx))
	rrForm := httptest.NewRecorder()

	h.EditKey(rrForm, reqForm)
	if rrForm.Code != http.StatusSeeOther {
		t.Fatalf("EditKey form returned status %d, want 303", rrForm.Code)
	}

	updated, err := repo.GetAPIKeyByID(ctx, key.ID)
	if err != nil || updated == nil {
		t.Fatalf("GetAPIKeyByID failed: %v", err)
	}
	if updated.Name != "Modified Key Name" {
		t.Errorf("expected Name 'Modified Key Name', got %q", updated.Name)
	}
	if updated.RateLimitRPM != 90 {
		t.Errorf("expected RateLimitRPM 90, got %d", updated.RateLimitRPM)
	}
	if updated.MaxTokensLimit != 1500000 {
		t.Errorf("expected MaxTokensLimit 1500000, got %d", updated.MaxTokensLimit)
	}
	if updated.DailyTokenQuota != 75000 {
		t.Errorf("expected DailyTokenQuota 75000, got %d", updated.DailyTokenQuota)
	}
	if updated.AllowedIPs != "192.168.1.100, 10.0.0.0/16" {
		t.Errorf("expected AllowedIPs '192.168.1.100, 10.0.0.0/16', got %q", updated.AllowedIPs)
	}
	if updated.ExpiresAt == nil {
		t.Errorf("expected ExpiresAt to be non-nil")
	}

	// 2. Unauthorized user attempts to edit key via JSON API -> 403
	jsonBody := `{"name": "Hacked Name"}`
	reqUnauth := httptest.NewRequest(http.MethodPost, "/api/keys/"+key.ID+"/edit", strings.NewReader(jsonBody))
	reqUnauth.Header.Set("Content-Type", "application/json")
	reqUnauth.Header.Set("Accept", "application/json")
	rctxUnauth := chi.NewRouteContext()
	rctxUnauth.URLParams.Add("id", key.ID)
	reqUnauth = reqUnauth.WithContext(context.WithValue(context.WithValue(ctx, userContextKey, otherUser), chi.RouteCtxKey, rctxUnauth))
	rrUnauth := httptest.NewRecorder()

	h.EditKey(rrUnauth, reqUnauth)
	if rrUnauth.Code != http.StatusForbidden {
		t.Fatalf("EditKey unauth returned status %d, want 403", rrUnauth.Code)
	}

	// 3. Admin updates key via JSON API -> 200 OK
	adminJSON := `{"name": "Admin Managed Key", "rate_limit_rpm": 200, "max_tokens_limit": 5000000, "daily_token_quota": 200000, "allowed_ips": "127.0.0.1"}`
	reqAdmin := httptest.NewRequest(http.MethodPost, "/api/keys/"+key.ID+"/edit", strings.NewReader(adminJSON))
	reqAdmin.Header.Set("Content-Type", "application/json")
	reqAdmin.Header.Set("Accept", "application/json")
	rctxAdmin := chi.NewRouteContext()
	rctxAdmin.URLParams.Add("id", key.ID)
	reqAdmin = reqAdmin.WithContext(context.WithValue(context.WithValue(ctx, userContextKey, adminUser), chi.RouteCtxKey, rctxAdmin))
	rrAdmin := httptest.NewRecorder()

	h.EditKey(rrAdmin, reqAdmin)
	if rrAdmin.Code != http.StatusOK {
		t.Fatalf("EditKey admin returned status %d, want 200", rrAdmin.Code)
	}

	adminUpdated, _ := repo.GetAPIKeyByID(ctx, key.ID)
	if adminUpdated.Name != "Admin Managed Key" || adminUpdated.RateLimitRPM != 200 {
		t.Errorf("expected admin update to succeed, got %+v", adminUpdated)
	}

	// 4. Non-existent key returns 404
	req404 := httptest.NewRequest(http.MethodPost, "/api/keys/non-existent-id/edit", strings.NewReader(adminJSON))
	req404.Header.Set("Content-Type", "application/json")
	rctx404 := chi.NewRouteContext()
	rctx404.URLParams.Add("id", "non-existent-id")
	req404 = req404.WithContext(context.WithValue(context.WithValue(ctx, userContextKey, adminUser), chi.RouteCtxKey, rctx404))
	rr404 := httptest.NewRecorder()

	h.EditKey(rr404, req404)
	if rr404.Code != http.StatusNotFound {
		t.Fatalf("EditKey 404 returned status %d, want 404", rr404.Code)
	}
}

func TestResetKeyUsage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_reset_keys.db")
	db, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-12345678901234567890",
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	ctx := context.Background()
	ownerUser := &entity.User{
		ID:           "u-owner",
		Username:     "owner",
		Name:         "Key Owner",
		PasswordHash: "hash123",
		Role:         "user",
		IsActive:     true,
	}
	otherUser := &entity.User{
		ID:           "u-other",
		Username:     "other",
		Name:         "Other User",
		PasswordHash: "hash123",
		Role:         "user",
		IsActive:     true,
	}
	if err := repo.CreateUser(ctx, ownerUser); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := repo.CreateUser(ctx, otherUser); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Create key for owner and accumulate token usage
	key := &entity.APIKey{
		ID:             "key-owner-1",
		UserID:         ownerUser.ID,
		Key:            "sk-gw-ownerkey123",
		Name:           "Owner Key",
		MaxTokensLimit: 100000,
		IsActive:       true,
	}
	if err := repo.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	if err := repo.UpdateKeyTokenUsage(ctx, key.ID, 75000); err != nil {
		t.Fatalf("UpdateKeyTokenUsage failed: %v", err)
	}

	// 1. Other unprivileged user attempts to reset owner's key via JSON API -> 403 Forbidden
	reqOtherAPI := httptest.NewRequest(http.MethodPost, "/api/keys/"+key.ID+"/reset-usage", nil)
	reqOtherAPI.Header.Set("Accept", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", key.ID)
	reqOtherAPI = reqOtherAPI.WithContext(context.WithValue(context.WithValue(ctx, userContextKey, otherUser), chi.RouteCtxKey, rctx))
	rrOtherAPI := httptest.NewRecorder()
	h.ResetKeyUsage(rrOtherAPI, reqOtherAPI)
	if rrOtherAPI.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 Forbidden, got %d", rrOtherAPI.Code)
	}

	// 2. Owner resets own key via form POST -> 303 redirect and tokens_used reset to 0
	form := url.Values{"redirect": {"/keys"}}
	reqOwnerForm := httptest.NewRequest(http.MethodPost, "/keys/"+key.ID+"/reset-usage", strings.NewReader(form.Encode()))
	reqOwnerForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctxOwner := chi.NewRouteContext()
	rctxOwner.URLParams.Add("id", key.ID)
	reqOwnerForm = reqOwnerForm.WithContext(context.WithValue(context.WithValue(ctx, userContextKey, ownerUser), chi.RouteCtxKey, rctxOwner))
	rrOwnerForm := httptest.NewRecorder()
	h.ResetKeyUsage(rrOwnerForm, reqOwnerForm)
	if rrOwnerForm.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303 SeeOther, got %d", rrOwnerForm.Code)
	}

	usage, err := repo.GetAPIKeyTokenUsage(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyTokenUsage failed: %v", err)
	}
	if usage != 0 {
		t.Fatalf("expected usage 0 after reset, got %d", usage)
	}

	// Accumulate usage again
	if err := repo.UpdateKeyTokenUsage(ctx, key.ID, 50000); err != nil {
		t.Fatalf("UpdateKeyTokenUsage failed: %v", err)
	}

	// 3. Admin resets key via JSON API -> 200 OK and tokens_used reset to 0
	adminUser := &entity.User{
		ID:       "u-admin",
		Username: "adminuser",
		Role:     "admin",
		IsActive: true,
	}
	reqAdminAPI := httptest.NewRequest(http.MethodPost, "/api/keys/"+key.ID+"/reset-usage", nil)
	reqAdminAPI.Header.Set("Accept", "application/json")
	rctxAdmin := chi.NewRouteContext()
	rctxAdmin.URLParams.Add("id", key.ID)
	reqAdminAPI = reqAdminAPI.WithContext(context.WithValue(context.WithValue(ctx, userContextKey, adminUser), chi.RouteCtxKey, rctxAdmin))
	rrAdminAPI := httptest.NewRecorder()
	h.ResetKeyUsage(rrAdminAPI, reqAdminAPI)
	if rrAdminAPI.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rrAdminAPI.Code)
	}

	usageAfterAdmin, err := repo.GetAPIKeyTokenUsage(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyTokenUsage failed: %v", err)
	}
	if usageAfterAdmin != 0 {
		t.Fatalf("expected usage 0 after admin reset, got %d", usageAfterAdmin)
	}
}

func TestAPICircuitBreakers(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "secret-12345678901234567890123456789012",
	}
	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	gw := proxy.NewGatewayProxy(cfg, repo)
	h.SetGatewayProxy(gw)

	// Register breakers and trip one
	cb1 := gw.CircuitBreakers().GetOrCreate("model:ag/gemini-3.7-flash-high")
	cb2 := gw.CircuitBreakers().GetOrCreate("model:ag/claude-sonnet-4-6")
	cb1.RecordFailure()
	cb1.RecordFailure()
	cb1.RecordFailure()

	if cb1.State() != proxy.StateOpen {
		t.Fatalf("expected cb1 state Open, got %s", cb1.State())
	}
	if cb2.State() != proxy.StateClosed {
		t.Fatalf("expected cb2 state Closed, got %s", cb2.State())
	}

	// 1. GET /api/upstream/circuit-breakers
	reqList := httptest.NewRequest(http.MethodGet, "/api/upstream/circuit-breakers", nil)
	rrList := httptest.NewRecorder()
	h.APICircuitBreakers(rrList, reqList)
	if rrList.Code != http.StatusOK {
		t.Fatalf("APICircuitBreakers returned %d", rrList.Code)
	}
	var listResp struct {
		Breakers map[string]proxy.CircuitBreakerSnapshot `json:"breakers"`
		Total    int                                     `json:"total"`
	}
	if err := json.NewDecoder(rrList.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode circuit breakers failed: %v", err)
	}
	if listResp.Total != 2 {
		t.Fatalf("expected 2 breakers, got %d", listResp.Total)
	}
	if listResp.Breakers["model:ag/gemini-3.7-flash-high"].State != proxy.StateOpen {
		t.Errorf("expected gemini breaker Open in snapshot, got %s", listResp.Breakers["model:ag/gemini-3.7-flash-high"].State)
	}

	// 2. POST /api/upstream/circuit-breakers/{name}/reset
	reqReset := httptest.NewRequest(http.MethodPost, "/api/upstream/circuit-breakers/model:ag/gemini-3.7-flash-high/reset", nil)
	rctxReset := chi.NewRouteContext()
	rctxReset.URLParams.Add("name", "model:ag/gemini-3.7-flash-high")
	reqReset = reqReset.WithContext(context.WithValue(reqReset.Context(), chi.RouteCtxKey, rctxReset))
	rrReset := httptest.NewRecorder()
	h.APICircuitBreakerReset(rrReset, reqReset)
	if rrReset.Code != http.StatusOK {
		t.Fatalf("APICircuitBreakerReset returned %d", rrReset.Code)
	}
	if cb1.State() != proxy.StateClosed {
		t.Fatalf("expected cb1 state Closed after reset, got %s", cb1.State())
	}

	// Reset non-existent breaker -> 404
	reqReset404 := httptest.NewRequest(http.MethodPost, "/api/upstream/circuit-breakers/non-existent/reset", nil)
	rctx404 := chi.NewRouteContext()
	rctx404.URLParams.Add("name", "non-existent")
	reqReset404 = reqReset404.WithContext(context.WithValue(reqReset404.Context(), chi.RouteCtxKey, rctx404))
	rrReset404 := httptest.NewRecorder()
	h.APICircuitBreakerReset(rrReset404, reqReset404)
	if rrReset404.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent breaker, got %d", rrReset404.Code)
	}

	// 3. Trip both and test ResetAll
	cb1.RecordFailure()
	cb1.RecordFailure()
	cb1.RecordFailure()
	cb2.RecordFailure()
	cb2.RecordFailure()
	cb2.RecordFailure()

	reqResetAll := httptest.NewRequest(http.MethodPost, "/api/upstream/circuit-breakers/reset", nil)
	rrResetAll := httptest.NewRecorder()
	h.APICircuitBreakersResetAll(rrResetAll, reqResetAll)
	if rrResetAll.Code != http.StatusOK {
		t.Fatalf("APICircuitBreakersResetAll returned %d", rrResetAll.Code)
	}
	if cb1.State() != proxy.StateClosed || cb2.State() != proxy.StateClosed {
		t.Fatalf("expected all breakers Closed after ResetAll, got cb1=%s, cb2=%s", cb1.State(), cb2.State())
	}
}

func TestChatPageHandler(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_chat_page.db")
	db, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-12345678901234567890",
		Port:          20129,
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	ctx := context.Background()
	user := &entity.User{
		ID:            "u-chat-tester",
		Username:      "chattester",
		Name:          "Chat Tester",
		PasswordHash:  "hash123",
		Role:          "user",
		AllowedModels: `["main", "free-only"]`,
		IsActive:      true,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	key := &entity.APIKey{
		ID:       "key-chat-1",
		UserID:   user.ID,
		Key:      "«redacted:sk-…»",
		Name:     "Test Chat Key",
		IsActive: true,
	}
	if err := repo.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	// 1. Authenticated user accesses /chat
	req := httptest.NewRequest(http.MethodGet, "/chat", nil)
	req = req.WithContext(context.WithValue(ctx, userContextKey, user))
	rr := httptest.NewRecorder()
	h.ChatPage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for /chat, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "Playground") {
		t.Errorf("expected response to contain 'Playground', got %s", body)
	}
	if !strings.Contains(body, "PRESETS") && !strings.Contains(body, "Presets") {
		t.Errorf("expected response to contain Presets toolbar")
	}
	if !strings.Contains(body, "BUILTIN_PRESETS") {
		t.Errorf("expected response to contain BUILTIN_PRESETS script")
	}
	if !strings.Contains(body, "exportAllSessionsJSON") {
		t.Errorf("expected response to contain exportAllSessionsJSON")
	}
}

func TestPricingAndCostEstimator(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-12345678901234567890",
		Port:          20129,
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}
	ctx := context.Background()

	// Mock Pricing on CoreClient
	mockCore := &upstream.MockCoreClient{
		PricingFunc: func(ctx context.Context) (map[string]interface{}, error) {
			return map[string]interface{}{
				"tokenrouter": map[string]interface{}{
					"anthropic/claude-3-7-sonnet": map[string]interface{}{
						"input":     3.0,
						"output":    15.0,
						"cached":    0.3,
						"reasoning": 15.0,
					},
				},
				"gh": map[string]interface{}{
					"gpt-4o": map[string]interface{}{
						"input":  2.5,
						"output": 10.0,
					},
				},
			}, nil
		},
	}
	h.coreClient = mockCore

	adminUser := &entity.User{
		ID:       "admin-1",
		Username: "admin",
		Role:     "admin",
		IsActive: true,
	}

	// 1. Test PricingPage renders 200 OK and contains calculator elements
	t.Run("PricingPage Render", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/pricing", nil)
		req = req.WithContext(context.WithValue(ctx, userContextKey, adminUser))
		rr := httptest.NewRecorder()
		h.PricingPage(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK for /pricing, got %d", rr.Code)
		}

		body := rr.Body.String()
		if !strings.Contains(body, "Pricing &amp; Cost Calculator") && !strings.Contains(body, "Pricing & Cost Calculator") {
			t.Errorf("expected response to contain 'Pricing & Cost Calculator'")
		}
		if !strings.Contains(body, "Cost Calculator") {
			t.Errorf("expected response to contain 'Cost Calculator'")
		}
		if !strings.Contains(body, "Rate Catalog") {
			t.Errorf("expected response to contain 'Rate Catalog'")
		}
		if !strings.Contains(body, "calculateCost") {
			t.Errorf("expected response to contain 'calculateCost' function")
		}
	})

	// 2. Test APIPricing returns raw pricing map
	t.Run("APIPricing JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
		req = req.WithContext(context.WithValue(ctx, userContextKey, adminUser))
		rr := httptest.NewRecorder()
		h.APIPricing(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK for /api/pricing, got %d", rr.Code)
		}

		var data map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &data); err != nil {
			t.Fatalf("invalid json response: %v", err)
		}
		if _, ok := data["tokenrouter"]; !ok {
			t.Errorf("expected 'tokenrouter' key in pricing response")
		}
	})

	// 3. Test APIPricingEstimate with catalog model
	t.Run("APIPricingEstimate Catalog Model", func(t *testing.T) {
		payload := map[string]interface{}{
			"source":            "tokenrouter",
			"model":             "anthropic/claude-3-7-sonnet",
			"prompt_tokens":     1000,
			"completion_tokens": 500,
			"cached_tokens":     200,
			"reasoning_tokens":  100,
			"requests":          100,
			"usd_to_idr":        16000.0,
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/pricing/estimate", bytes.NewReader(bodyBytes))
		req = req.WithContext(context.WithValue(ctx, userContextKey, adminUser))
		rr := httptest.NewRecorder()
		h.APIPricingEstimate(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK for /api/pricing/estimate, got %d: %s", rr.Code, rr.Body.String())
		}

		var res PricingEstimateResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
			t.Fatalf("invalid response json: %v", err)
		}

		if res.Model != "anthropic/claude-3-7-sonnet" {
			t.Errorf("expected model 'anthropic/claude-3-7-sonnet', got %s", res.Model)
		}

		// Calculations:
		// input: 1000 tokens * (3.0 / 1M) = $0.003
		// output: 500 tokens * (15.0 / 1M) = $0.0075
		// cached: 200 tokens * (0.3 / 1M) = $0.00006
		// reasoning: 100 tokens * (15.0 / 1M) = $0.0015
		// single req = 0.003 + 0.0075 + 0.00006 + 0.0015 = 0.01206
		// total 100 reqs = 1.206 USD
		totalCostUSD := res.CostUSD["total_cost"]
		if totalCostUSD < 1.20 || totalCostUSD > 1.21 {
			t.Errorf("expected total_cost around 1.206 USD, got %f", totalCostUSD)
		}

		totalCostIDR := res.CostIDR["total_cost"]
		expectedIDR := totalCostUSD * 16000.0
		if totalCostIDR != expectedIDR {
			t.Errorf("expected total IDR %f, got %f", expectedIDR, totalCostIDR)
		}
	})

	// 4. Test APIPricingEstimate with Custom Model
	t.Run("APIPricingEstimate Custom Rates", func(t *testing.T) {
		payload := map[string]interface{}{
			"source":              "custom",
			"prompt_tokens":       2000,
			"completion_tokens":   1000,
			"requests":            50,
			"usd_to_idr":          16500.0,
			"custom_input_rate":   1.0,
			"custom_output_rate":  2.0,
			"custom_cached_rate":  0.1,
			"custom_reasoning_rate": 2.0,
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/pricing/estimate", bytes.NewReader(bodyBytes))
		req = req.WithContext(context.WithValue(ctx, userContextKey, adminUser))
		rr := httptest.NewRecorder()
		h.APIPricingEstimate(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK, got %d", rr.Code)
		}

		var res PricingEstimateResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
			t.Fatalf("invalid json: %v", err)
		}

		// Single req:
		// input: 2000 * (1.0 / 1M) = 0.002
		// output: 1000 * (2.0 / 1M) = 0.002
		// single = 0.004
		// total 50 reqs = 0.20 USD
		if res.CostUSD["total_cost"] != 0.20 {
			t.Errorf("expected total_cost 0.20 USD, got %f", res.CostUSD["total_cost"])
		}
	})

	// 5. Test APIPricingEstimate Invalid JSON
	t.Run("APIPricingEstimate Bad Request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/pricing/estimate", bytes.NewReader([]byte("invalid json")))
		req = req.WithContext(context.WithValue(ctx, userContextKey, adminUser))
		rr := httptest.NewRecorder()
		h.APIPricingEstimate(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 Bad Request, got %d", rr.Code)
		}
	})
}

func TestLogsPageAndPayloadInspector(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_logs_inspect.db")
	db, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-12345678901234567890",
		Port:          20129,
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	ctx := context.Background()
	user1 := &entity.User{
		ID:            "u-logs-1",
		Username:      "loguser1",
		Name:          "Log User 1",
		PasswordHash:  "hash123",
		Role:          "user",
		AllowedModels: `["main"]`,
		IsActive:      true,
	}
	if err := repo.CreateUser(ctx, user1); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	user2 := &entity.User{
		ID:            "u-logs-2",
		Username:      "loguser2",
		Name:          "Log User 2",
		PasswordHash:  "hash123",
		Role:          "user",
		AllowedModels: `["main"]`,
		IsActive:      true,
	}
	if err := repo.CreateUser(ctx, user2); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	adminUser := &entity.User{
		ID:            "u-logs-admin",
		Username:      "adminlogs",
		Name:          "Admin Logs",
		PasswordHash:  "hash123",
		Role:          "admin",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	if err := repo.CreateUser(ctx, adminUser); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	key1 := &entity.APIKey{
		ID:       "k-log-1",
		UserID:   user1.ID,
		Key:      "«redacted:sk-…»",
		Name:     "Test Key 1",
		IsActive: true,
	}
	if err := repo.CreateAPIKey(ctx, key1); err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	logEntry := &entity.RequestLog{
		UserID:           user1.ID,
		APIKeyID:         key1.ID,
		Path:             "/v1/chat/completions",
		Method:           "POST",
		Model:            "ag/gemini-3.7-flash-high",
		IsStream:         true,
		PromptTokens:     42,
		CompletionTokens: 88,
		TotalTokens:      130,
		StatusCode:       200,
		DurationMs:       650,
		ClientIP:         "127.0.0.1",
		RequestBody:      `{"model":"ag/gemini-3.7-flash-high","messages":[{"role":"user","content":"Hello AI"}]}`,
		ResponseText:     "Hello there! How can I assist you today?",
	}
	if err := repo.CreateRequestLog(ctx, logEntry); err != nil {
		t.Fatalf("CreateRequestLog failed: %v", err)
	}

	logs, _, err := repo.GetRequestLogs(ctx, 10, 0, user1.ID, "", 0)
	if err != nil || len(logs) == 0 {
		t.Fatalf("failed to retrieve inserted log: %v", err)
	}
	logID := logs[0].ID

	// 1. LogsPage renders 200 OK and includes inspect modal & table
	reqLogs := httptest.NewRequest(http.MethodGet, "/logs", nil)
	reqLogs = reqLogs.WithContext(context.WithValue(ctx, userContextKey, user1))
	rrLogs := httptest.NewRecorder()
	h.LogsPage(rrLogs, reqLogs)

	if rrLogs.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /logs, got %d", rrLogs.Code)
	}
	bodyLogs := rrLogs.Body.String()
	if !strings.Contains(bodyLogs, "Request Audit Logs") {
		t.Errorf("expected page to contain 'Request Audit Logs'")
	}
	if !strings.Contains(bodyLogs, "inspectLogModal") {
		t.Errorf("expected page to contain 'inspectLogModal'")
	}
	if !strings.Contains(bodyLogs, "inspectLog(") {
		t.Errorf("expected page to contain 'inspectLog(' JS call")
	}

	// 2. Fetch log payload via APIReplaySource by owner (user1) -> 200 OK
	reqSource := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/replay/source?id=%d", logID), nil)
	reqSource = reqSource.WithContext(context.WithValue(ctx, userContextKey, user1))
	rrSource := httptest.NewRecorder()
	h.APIReplaySource(rrSource, reqSource)

	if rrSource.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from APIReplaySource for owner, got %d", rrSource.Code)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(rrSource.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode APIReplaySource response: %v", err)
	}
	if payload["model"] != "ag/gemini-3.7-flash-high" {
		t.Errorf("expected model 'ag/gemini-3.7-flash-high', got %v", payload["model"])
	}
	if payload["request_body"] != logEntry.RequestBody {
		t.Errorf("expected request_body match, got %v", payload["request_body"])
	}
	if payload["response_text"] != logEntry.ResponseText {
		t.Errorf("expected response_text match, got %v", payload["response_text"])
	}
	if int(payload["status_code"].(float64)) != 200 {
		t.Errorf("expected status_code 200, got %v", payload["status_code"])
	}
	if int(payload["total_tokens"].(float64)) != 130 {
		t.Errorf("expected total_tokens 130, got %v", payload["total_tokens"])
	}

	// 3. User2 (non-admin, not owner) attempts to fetch user1's log -> 404
	reqForbidden := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/replay/source?id=%d", logID), nil)
	reqForbidden = reqForbidden.WithContext(context.WithValue(ctx, userContextKey, user2))
	rrForbidden := httptest.NewRecorder()
	h.APIReplaySource(rrForbidden, reqForbidden)

	if rrForbidden.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for non-owner, got %d", rrForbidden.Code)
	}

	// 4. Admin attempts to fetch user1's log -> 200 OK
	reqAdmin := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/replay/source?id=%d", logID), nil)
	reqAdmin = reqAdmin.WithContext(context.WithValue(ctx, userContextKey, adminUser))
	rrAdmin := httptest.NewRecorder()
	h.APIReplaySource(rrAdmin, reqAdmin)

	if rrAdmin.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for admin, got %d", rrAdmin.Code)
	}
}

func TestAPIKeyStats(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_key_stats.db")
	db, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{
		SessionSecret: "test-secret-12345678901234567890",
		Port:          20129,
	}

	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	ctx := context.Background()

	// 1. Setup User 1 (Admin)
	adminUser := &entity.User{
		ID:            "u-admin-stats",
		Username:      "adminstats",
		Name:          "Admin Stats",
		PasswordHash:  "hash123",
		Role:          "admin",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	_ = repo.CreateUser(ctx, adminUser)

	// 2. Setup User 2 (Standard User)
	user2 := &entity.User{
		ID:            "u-user2-stats",
		Username:      "user2stats",
		Name:          "User Two",
		PasswordHash:  "hash123",
		Role:          "user",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	_ = repo.CreateUser(ctx, user2)

	// 3. Setup User 3 (Standard User)
	user3 := &entity.User{
		ID:            "u-user3-stats",
		Username:      "user3stats",
		Name:          "User Three",
		PasswordHash:  "hash123",
		Role:          "user",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	_ = repo.CreateUser(ctx, user3)

	// Setup API Key for User 2
	key2 := &entity.APIKey{
		ID:              "key-u2-1",
		UserID:          user2.ID,
		Key:             "«redacted:sk-…»",
		Name:            "User 2 Key",
		RateLimitRPM:    120,
		MaxTokensLimit:  500000,
		DailyTokenQuota: 50000,
		IsActive:        true,
	}
	_ = repo.CreateAPIKey(ctx, key2)

	// Insert test request logs for key2
	log1 := &entity.RequestLog{
		UserID:           user2.ID,
		APIKeyID:         key2.ID,
		Path:             "/v1/chat/completions",
		Method:           "POST",
		Model:            "ag/gemini-3.7-flash-high",
		IsStream:         false,
		PromptTokens:     100,
		CompletionTokens: 300,
		TotalTokens:      400,
		StatusCode:       200,
		DurationMs:       180,
		ClientIP:         "10.0.0.2",
	}
	_ = repo.CreateRequestLog(ctx, log1)

	// Router setup for Chi URL param binding
	r := chi.NewRouter()
	r.Get("/api/keys/{id}/stats", h.APIKeyStats)

	// Case A: Owner (User 2) queries own key stats -> 200 OK
	reqA := httptest.NewRequest(http.MethodGet, "/api/keys/"+key2.ID+"/stats", nil)
	reqA = reqA.WithContext(context.WithValue(ctx, userContextKey, user2))
	rrA := httptest.NewRecorder()
	r.ServeHTTP(rrA, reqA)

	if rrA.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for owner key stats, got %d: %s", rrA.Code, rrA.Body.String())
	}

	var respA entity.KeyStatsResponse
	if err := json.Unmarshal(rrA.Body.Bytes(), &respA); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}
	if respA.Key.ID != key2.ID || respA.Summary.TotalRequests != 1 || respA.Summary.TotalTokens != 400 {
		t.Errorf("unexpected KeyStatsResponse payload: %+v", respA)
	}

	// Case B: Non-owner standard user (User 3) queries User 2's key stats -> 404 Not Found (Security isolation)
	reqB := httptest.NewRequest(http.MethodGet, "/api/keys/"+key2.ID+"/stats", nil)
	reqB = reqB.WithContext(context.WithValue(ctx, userContextKey, user3))
	rrB := httptest.NewRecorder()
	r.ServeHTTP(rrB, reqB)

	if rrB.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for non-owner user, got %d", rrB.Code)
	}

	// Case C: Admin queries User 2's key stats -> 200 OK
	reqC := httptest.NewRequest(http.MethodGet, "/api/keys/"+key2.ID+"/stats", nil)
	reqC = reqC.WithContext(context.WithValue(ctx, userContextKey, adminUser))
	rrC := httptest.NewRecorder()
	r.ServeHTTP(rrC, reqC)

	if rrC.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for admin query, got %d", rrC.Code)
	}

	// Case D: Non-existent key -> 404 Not Found
	reqD := httptest.NewRequest(http.MethodGet, "/api/keys/non-existent-key-999/stats", nil)
	reqD = reqD.WithContext(context.WithValue(ctx, userContextKey, adminUser))
	rrD := httptest.NewRecorder()
	r.ServeHTTP(rrD, reqD)

	if rrD.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for non-existent key, got %d", rrD.Code)
	}
}
