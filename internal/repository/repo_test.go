package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"9router-gateway/internal/database"
	"9router-gateway/internal/entity"
)

func setupTestRepo(t *testing.T) (*SQLiteRepo, func()) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "repo_test.db")

	db, err := database.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db for repo test: %v", err)
	}

	repo := NewSQLiteRepo(db)
	cleanup := func() {
		_ = db.Close()
	}
	return repo, cleanup
}

func TestUserOperations(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Create User
	u := &entity.User{
		ID:            "user-1",
		Username:      "alice",
		Name:          "Alice Wonderland",
		PasswordHash:  "hashed_pw_123",
		Role:          "user",
		TokenQuota:    50000,
		TokensUsed:    0,
		AllowedModels: `["*"]`,
		IsActive:      true,
	}

	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// 2. Get by ID
	fetched, err := repo.GetUserByID(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if fetched.Username != "alice" || fetched.Name != "Alice Wonderland" {
		t.Errorf("unexpected user data: %+v", fetched)
	}

	// 3. Get by Username
	byUsername, err := repo.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername failed: %v", err)
	}
	if byUsername.ID != "user-1" {
		t.Errorf("expected user ID 'user-1', got '%s'", byUsername.ID)
	}

	// 4. Update Password
	newHash := "new_hashed_password"
	if err := repo.UpdateUserPassword(ctx, "user-1", newHash); err != nil {
		t.Fatalf("UpdateUserPassword failed: %v", err)
	}
	updatedUser, _ := repo.GetUserByID(ctx, "user-1")
	if updatedUser.PasswordHash != newHash {
		t.Errorf("expected updated password hash '%s', got '%s'", newHash, updatedUser.PasswordHash)
	}

	// 5. Deduct & Credit Tokens
	if err := repo.DeductTokens(ctx, "user-1", 1500); err != nil {
		t.Fatalf("DeductTokens failed: %v", err)
	}
	afterDeduct, _ := repo.GetUserByID(ctx, "user-1")
	if afterDeduct.TokensUsed != 1500 {
		t.Errorf("expected 1500 tokens used, got %d", afterDeduct.TokensUsed)
	}

	if err := repo.CreditUserTokens(ctx, "user-1", 10000); err != nil {
		t.Fatalf("CreditUserTokens failed: %v", err)
	}
	afterCredit, _ := repo.GetUserByID(ctx, "user-1")
	if afterCredit.TokenQuota != 60000 {
		t.Errorf("expected token quota 60000, got %d", afterCredit.TokenQuota)
	}

	// 6. Reset Usage
	if err := repo.ResetUserUsage(ctx, "user-1"); err != nil {
		t.Fatalf("ResetUserUsage failed: %v", err)
	}
	afterReset, _ := repo.GetUserByID(ctx, "user-1")
	if afterReset.TokensUsed != 0 {
		t.Errorf("expected 0 tokens used after reset, got %d", afterReset.TokensUsed)
	}

	// 7. Toggle User Status
	if err := repo.ToggleUserStatus(ctx, "user-1", false); err != nil {
		t.Fatalf("ToggleUserStatus failed: %v", err)
	}
	deactivated, _ := repo.GetUserByID(ctx, "user-1")
	if deactivated.IsActive {
		t.Errorf("expected user to be inactive")
	}

	// 8. Delete User
	if err := repo.DeleteUser(ctx, "user-1"); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}
	_, err = repo.GetUserByID(ctx, "user-1")
	if err == nil {
		t.Errorf("expected error getting deleted user, got nil")
	}
}

func TestAPIKeyOperations(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Setup user first
	u := &entity.User{
		ID:            "user-keys",
		Username:      "bob",
		Name:          "Bob",
		Role:          "user",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// 1. Create Key
	key := &entity.APIKey{
		ID:            "key-1",
		UserID:        "user-keys",
		Key:           "sk-gw-test-key-12345",
		Name:          "Primary Test Key",
		AllowedModels: `["ag/gemini-3.8-flash"]`,
		RateLimitRPM:  60,
		IsActive:      true,
	}
	if err := repo.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	// 2. Get Key
	fetchedKey, err := repo.GetAPIKeyByKey(ctx, "sk-gw-test-key-12345")
	if err != nil {
		t.Fatalf("GetAPIKeyByKey failed: %v", err)
	}
	if fetchedKey.Name != "Primary Test Key" || fetchedKey.RateLimitRPM != 60 {
		t.Errorf("unexpected key data: %+v", fetchedKey)
	}

	// 3. Get Keys By User
	userKeys, err := repo.GetAPIKeysByUserID(ctx, "user-keys")
	if err != nil {
		t.Fatalf("GetAPIKeysByUserID failed: %v", err)
	}
	if len(userKeys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(userKeys))
	}

	// 4. Update Last Used
	if err := repo.UpdateKeyLastUsed(ctx, "key-1"); err != nil {
		t.Fatalf("UpdateKeyLastUsed failed: %v", err)
	}

	// 5. Toggle Status
	if err := repo.ToggleAPIKeyStatus(ctx, "key-1", false); err != nil {
		t.Fatalf("ToggleAPIKeyStatus failed: %v", err)
	}
	toggledKey, _ := repo.GetAPIKeyByKey(ctx, "sk-gw-test-key-12345")
	if toggledKey.IsActive {
		t.Errorf("expected key to be inactive")
	}

	// 6. Delete Key
	if err := repo.DeleteAPIKey(ctx, "key-1"); err != nil {
		t.Fatalf("DeleteAPIKey failed: %v", err)
	}
	_, err = repo.GetAPIKeyByKey(ctx, "sk-gw-test-key-12345")
	if err == nil {
		t.Errorf("expected error getting deleted key, got nil")
	}
}

func TestSessionAndSettings(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Create user
	u := &entity.User{
		ID:            "user-session",
		Username:      "charlie",
		Name:          "Charlie",
		Role:          "admin",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	_ = repo.CreateUser(ctx, u)

	// 1. Session creation & retrieval
	sessionToken := "sess-abcdef1234567890abcdef1234567890"
	expires := time.Now().Add(24 * time.Hour)
	if err := repo.CreateSession(ctx, sessionToken, "user-session", expires); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	sessionUser, err := repo.GetSessionUser(ctx, sessionToken)
	if err != nil {
		t.Fatalf("GetSessionUser failed: %v", err)
	}
	if sessionUser.ID != "user-session" {
		t.Errorf("expected user ID 'user-session', got '%s'", sessionUser.ID)
	}

	// Delete session
	if err := repo.DeleteSession(ctx, sessionToken); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	_, err = repo.GetSessionUser(ctx, sessionToken)
	if err == nil {
		t.Errorf("expected error getting deleted session, got nil")
	}

	// 2. Settings Save & Get
	if err := repo.SaveSetting(ctx, "test_setting", "setting_value_123"); err != nil {
		t.Fatalf("SaveSetting failed: %v", err)
	}
	val, err := repo.GetSetting(ctx, "test_setting")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if val != "setting_value_123" {
		t.Errorf("expected 'setting_value_123', got '%s'", val)
	}
}

func TestRequestLogs(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Log entry
	logEntry := &entity.RequestLog{
		UserID:           "user-logs",
		APIKeyID:         "key-logs",
		Path:             "/v1/chat/completions",
		Method:           "POST",
		Model:            "ag/gemini-3.8-flash",
		IsStream:         true,
		PromptTokens:     10,
		CompletionTokens: 25,
		TotalTokens:      35,
		StatusCode:       200,
		DurationMs:       150,
		ClientIP:         "127.0.0.1",
	}

	if err := repo.CreateRequestLog(ctx, logEntry); err != nil {
		t.Fatalf("CreateRequestLog failed: %v", err)
	}

	logs, total, err := repo.GetRequestLogs(ctx, 10, 0, "", "", 0)
	if err != nil {
		t.Fatalf("GetRequestLogs failed: %v", err)
	}
	if total < 1 || len(logs) < 1 {
		t.Fatalf("expected at least 1 log, got total=%d, len=%d", total, len(logs))
	}
	if logs[0].Model != "ag/gemini-3.8-flash" {
		t.Errorf("expected model 'ag/gemini-3.8-flash', got '%s'", logs[0].Model)
	}
}

func TestCleanExpiredSessionsAndLoginAttempts(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Session cleanup
	u := &entity.User{
		ID:            "user-clean",
		Username:      "dan",
		Name:          "Dan",
		Role:          "user",
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	_ = repo.CreateUser(ctx, u)

	expiredToken := "sess-expired-12345"
	_ = repo.CreateSession(ctx, expiredToken, "user-clean", time.Now().Add(-2*time.Hour))

	if err := repo.CleanExpiredSessions(ctx); err != nil {
		t.Fatalf("CleanExpiredSessions failed: %v", err)
	}
	if _, err := repo.GetSessionUser(ctx, expiredToken); err == nil {
		t.Errorf("expected expired session to be deleted")
	}

	// 2. Login attempts
	testIP := "198.51.100.42"
	_ = repo.RecordLoginAttempt(ctx, testIP)
	count, err := repo.GetRecentLoginAttempts(ctx, testIP, 15)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 login attempt, got %d (err: %v)", count, err)
	}

	if err := repo.ClearLoginAttempts(ctx, testIP); err != nil {
		t.Fatalf("ClearLoginAttempts failed: %v", err)
	}
	countAfter, _ := repo.GetRecentLoginAttempts(ctx, testIP, 15)
	if countAfter != 0 {
		t.Errorf("expected 0 login attempts after clear, got %d", countAfter)
	}

	if err := repo.CleanOldLoginAttempts(ctx); err != nil {
		t.Fatalf("CleanOldLoginAttempts failed: %v", err)
	}
}
