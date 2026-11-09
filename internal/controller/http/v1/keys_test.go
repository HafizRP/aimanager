package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"9router-gateway/internal/config"
	"9router-gateway/internal/database"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/repository"
)

func TestParseExpiryInput(t *testing.T) {
	// 1. Empty / Never strings return zero time without error
	for _, input := range []string{"", "  ", "never", "NEVER", "none", "0"} {
		res, err := parseExpiryInput(input)
		if err != nil {
			t.Errorf("expected no error for %q, got: %v", input, err)
		}
		if !res.IsZero() {
			t.Errorf("expected zero time for %q, got: %v", input, res)
		}
	}

	// 2. Relative durations
	now := time.Now()
	cases := []struct {
		input       string
		minDuration time.Duration
		maxDuration time.Duration
	}{
		{"7d", 6*24*time.Hour + 20*time.Hour, 7*24*time.Hour + 4*time.Hour},
		{"+7d", 6*24*time.Hour + 20*time.Hour, 7*24*time.Hour + 4*time.Hour},
		{"30d", 29*24*time.Hour, 31*24*time.Hour},
		{"2w", 13*24*time.Hour, 15*24*time.Hour},
		{"1m", 27*24*time.Hour, 32*24*time.Hour},
		{"1y", 364*24*time.Hour, 367*24*time.Hour},
		{"24h", 23*time.Hour, 25*time.Hour},
		{"+48h", 47*time.Hour, 49*time.Hour},
	}

	for _, tc := range cases {
		res, err := parseExpiryInput(tc.input)
		if err != nil {
			t.Errorf("failed to parse relative duration %q: %v", tc.input, err)
			continue
		}
		diff := res.Sub(now)
		if diff < tc.minDuration || diff > tc.maxDuration {
			t.Errorf("for input %q: expected duration between %v and %v, got %v", tc.input, tc.minDuration, tc.maxDuration, diff)
		}
	}

	// 3. Absolute formats
	absInputs := []string{
		"2027-01-15T10:30",
		"2027-01-15",
		"2027-01-15 10:30",
	}
	for _, input := range absInputs {
		res, err := parseExpiryInput(input)
		if err != nil {
			t.Errorf("failed to parse absolute input %q: %v", input, err)
			continue
		}
		if res.Year() != 2027 || res.Month() != time.January || res.Day() != 15 {
			t.Errorf("unexpected date parsed for %q: %v", input, res)
		}
	}

	// 4. Invalid inputs
	invalid := []string{"invalid", "abc", "7x", "-10d", "2027-99-99"}
	for _, input := range invalid {
		_, err := parseExpiryInput(input)
		if err == nil {
			t.Errorf("expected error for invalid input %q, got nil", input)
		}
	}
}

func TestCreateKeyWithRelativeExpiry(t *testing.T) {
	db, err := database.InitDB(filepath.Join(t.TempDir(), "test_keys.db"))
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
		ID:           "test-user-expiry",
		Name:         "Expiry Tester",
		Username:     "expirytester",
		PasswordHash: "hashed",
		Role:         "admin",
		IsActive:     true,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Submit key creation with "30d" relative duration
	form := url.Values{
		"user_id":        {user.ID},
		"name":           {"Temporary Key 30D"},
		"allowed_models": {"main"},
		"expires_at":     {"30d"},
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
		t.Fatalf("expected key in repo, err=%v", err)
	}
	key := keys[0]
	if key.ExpiresAt == nil {
		t.Fatal("expected key to have non-nil ExpiresAt")
	}

	diff := key.ExpiresAt.Sub(time.Now())
	if diff < 29*24*time.Hour || diff > 31*24*time.Hour {
		t.Errorf("expected expiry around ~30d, got %v", diff)
	}
}
