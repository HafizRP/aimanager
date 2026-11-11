package v1

import (
	"bytes"
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
	"9router-gateway/internal/repository"
	"9router-gateway/internal/usecase"
)

func TestProfile_UpdateProfilePost(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{SessionSecret: "test-secret-32-chars-long-123456"}
	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	ctx := context.Background()
	user := &entity.User{
		ID:            "u-prof-1",
		Username:      "proftest",
		Name:          "Old Name",
		PasswordHash:  "hash",
		Role:          "user",
		TokenQuota:    1000,
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// 1. Unauthenticated request redirects to /login
	req := httptest.NewRequest(http.MethodPost, "/profile", strings.NewReader("name=New+Name"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.UpdateProfilePost(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
		t.Errorf("expected 303 to /login, got code=%d location=%s", rr.Code, rr.Header().Get("Location"))
	}

	// 2. Empty name fails validation with error message
	authCtx := context.WithValue(ctx, userContextKey, user)
	req = httptest.NewRequest(http.MethodPost, "/profile", strings.NewReader("name=+")).WithContext(authCtx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr = httptest.NewRecorder()
	h.UpdateProfilePost(rr, req)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "error=") {
		t.Errorf("expected redirect with error for empty name, got %d %s", rr.Code, rr.Header().Get("Location"))
	}

	// 3. Successful update
	req = httptest.NewRequest(http.MethodPost, "/profile", strings.NewReader("name=Hafiz+Updated")).WithContext(authCtx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr = httptest.NewRecorder()
	h.UpdateProfilePost(rr, req)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "msg=Profile+updated+successfully") {
		t.Errorf("expected 303 to /profile with success msg, got %d %s", rr.Code, rr.Header().Get("Location"))
	}

	updated, err := repo.GetUserByID(ctx, user.ID)
	if err != nil || updated.Name != "Hafiz Updated" {
		t.Errorf("expected updated name in db, got err=%v name=%q", err, updated.Name)
	}
}

func TestProfile_APIUpdateProfile(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{SessionSecret: "test-secret-32-chars-long-123456"}
	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	ctx := context.Background()
	user := &entity.User{
		ID:            "u-prof-2",
		Username:      "apiuser",
		Name:          "Initial Name",
		PasswordHash:  "hash",
		Role:          "user",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// 1. JSON Update Success
	authCtx := context.WithValue(ctx, userContextKey, user)
	body := bytes.NewBufferString(`{"name": "API Changed Name"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/profile", body).WithContext(authCtx)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.APIUpdateProfile(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
	}

	var res map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil || res["success"] != true {
		t.Fatalf("invalid API response: %v", res)
	}
}

func TestProfile_ExportOwnLoginAudits(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{SessionSecret: "test-secret-32-chars-long-123456"}
	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	ctx := context.Background()
	user := &entity.User{
		ID:            "u-prof-audit",
		Username:      "audittest",
		Name:          "Audit Tester",
		PasswordHash:  "hash",
		Role:          "user",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	_ = repo.RecordLoginAudit(ctx, &entity.LoginAudit{
		UserID:    &user.ID,
		Username:  user.Username,
		Status:    "success",
		IP:        "192.168.1.100",
		UserAgent: "Mozilla/5.0 Test",
	})

	authCtx := context.WithValue(ctx, userContextKey, user)

	// CSV Export
	reqCSV := httptest.NewRequest(http.MethodGet, "/api/profile/login-audits/export?format=csv", nil).WithContext(authCtx)
	rrCSV := httptest.NewRecorder()
	h.ExportOwnLoginAudits(rrCSV, reqCSV)
	if rrCSV.Code != http.StatusOK {
		t.Fatalf("expected 200 for CSV, got %d", rrCSV.Code)
	}
	if !strings.Contains(rrCSV.Body.String(), "Timestamp_WIB") || !strings.Contains(rrCSV.Body.String(), "192.168.1.100") {
		t.Errorf("CSV output missing audit details: %s", rrCSV.Body.String())
	}

	// JSON Export
	reqJSON := httptest.NewRequest(http.MethodGet, "/api/profile/login-audits/export?format=json", nil).WithContext(authCtx)
	rrJSON := httptest.NewRecorder()
	h.ExportOwnLoginAudits(rrJSON, reqJSON)
	if rrJSON.Code != http.StatusOK {
		t.Fatalf("expected 200 for JSON, got %d", rrJSON.Code)
	}
	if !strings.Contains(rrJSON.Body.String(), "192.168.1.100") {
		t.Errorf("JSON output missing audit details: %s", rrJSON.Body.String())
	}
}

func TestProfile_UpdatePasswordWithRedirect(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	repo := repository.NewSQLiteRepo(db)
	cfg := &config.Config{SessionSecret: "test-secret-32-chars-long-123456"}
	h, err := NewHandler(cfg, repo, nil, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	ctx := context.Background()
	hash, _ := usecase.HashPassword("oldpassword123")
	user := &entity.User{
		ID:            "u-prof-pass",
		Username:      "passtest",
		Name:          "Password Tester",
		PasswordHash:  hash,
		Role:          "user",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	authCtx := context.WithValue(ctx, userContextKey, user)

	// Update password with redirect=/profile
	form := url.Values{}
	form.Set("redirect", "/profile")
	form.Set("current_password", "oldpassword123")
	form.Set("new_password", "newpassword123")

	req := httptest.NewRequest(http.MethodPost, "/settings/password", strings.NewReader(form.Encode())).WithContext(authCtx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.UpdatePasswordPost(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/profile?msg=") {
		t.Errorf("expected redirect to /profile?msg=..., got %s", loc)
	}
}
