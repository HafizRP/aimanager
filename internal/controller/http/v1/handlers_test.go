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

	"github.com/go-chi/chi/v5"

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

	// Seed audit
	uid := "user-audit-test"
	_ = repo.RecordLoginAudit(ctx, &entity.LoginAudit{
		UserID:    &uid,
		Username:  "audituser",
		IP:        "10.0.0.99",
		UserAgent: "Mozilla/5.0",
		Status:    "success",
		Reason:    "auth ok",
	})

	// 1. APILoginAudits (all)
	req := httptest.NewRequest(http.MethodGet, "/api/login-audits?limit=10", nil)
	rr := httptest.NewRecorder()
	h.APILoginAudits(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("APILoginAudits returned %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["total"].(float64) < 1 {
		t.Errorf("expected at least 1 audit in total, got %v", resp["total"])
	}

	// 2. APIUserLoginAudits
	reqUser := httptest.NewRequest(http.MethodGet, "/api/users/user-audit-test/login-audits", nil)
	// Add chi route param context
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "user-audit-test")
	reqUser = reqUser.WithContext(context.WithValue(reqUser.Context(), chi.RouteCtxKey, rctx))

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
		Key:      "sk-gw-chat123456",
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
