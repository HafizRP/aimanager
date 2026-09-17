package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
	if err := repo.UpdateKeyLastUsed(ctx, "key-1", "192.168.1.50"); err != nil {
		t.Fatalf("UpdateKeyLastUsed failed: %v", err)
	}
	keyWithIP, err := repo.GetAPIKeyByKey(ctx, key.Key)
	if err != nil {
		t.Fatalf("GetAPIKeyByKey failed: %v", err)
	}
	if keyWithIP.LastUsedIP != "192.168.1.50" {
		t.Errorf("expected LastUsedIP 192.168.1.50, got %s", keyWithIP.LastUsedIP)
	}

	// 5. Toggle Status
	if err := repo.ToggleAPIKeyStatus(ctx, "key-1", false); err != nil {
		t.Fatalf("ToggleAPIKeyStatus failed: %v", err)
	}
	toggledKey, errTog := repo.GetAPIKeyByKey(ctx, "sk-gw-test-key-12345")
	if errTog != nil {
		t.Fatalf("GetAPIKeyByKey after toggle failed: %v", errTog)
	}
	if toggledKey.IsActive {
		t.Errorf("expected key to be inactive")
	}

	// 5b. Token budget accumulation
	if err := repo.UpdateKeyTokenUsage(ctx, "key-1", 250); err != nil {
		t.Fatalf("UpdateKeyTokenUsage failed: %v", err)
	}
	if err := repo.UpdateKeyTokenUsage(ctx, "key-1", 750); err != nil {
		t.Fatalf("UpdateKeyTokenUsage failed: %v", err)
	}
	used, err := repo.GetAPIKeyTokenUsage(ctx, "key-1")
	if err != nil {
		t.Fatalf("GetAPIKeyTokenUsage failed: %v", err)
	}
	if used != 1000 {
		t.Errorf("expected token usage 1000, got %d", used)
	}
	// Budget check inside the loaded key
	loaded, err := repo.GetAPIKeyByKey(ctx, "sk-gw-test-key-12345")
	if err != nil {
		t.Fatalf("GetAPIKeyByKey failed: %v", err)
	}
	if loaded.TokenUsage != 1000 {
		t.Errorf("expected loaded TokenUsage 1000, got %d", loaded.TokenUsage)
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

func TestRequestLogPayloadRoundtrip(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	longBody := `{"model":"m","messages":[{"role":"user","content":"` + strings.Repeat("x", 9000) + `"}]}`
	longResp := strings.Repeat("y", 5000)
	logEntry := &entity.RequestLog{
		UserID:       "user-replay",
		APIKeyID:     "key-replay",
		Path:         "/v1/chat/completions",
		Method:       "POST",
		Model:        "ag/gemini-3.8-flash",
		StatusCode:   200,
		ClientIP:     "127.0.0.1",
		RequestBody:  longBody,
		ResponseText: longResp,
	}

	if err := repo.CreateRequestLog(ctx, logEntry); err != nil {
		t.Fatalf("CreateRequestLog failed: %v", err)
	}

	logs, total, err := repo.GetRequestLogs(ctx, 10, 0, "", "", 0)
	if err != nil || total < 1 {
		t.Fatalf("GetRequestLogs failed: %v total=%d", err, total)
	}

	got, err := repo.GetRequestLogByID(ctx, logs[0].ID)
	if err != nil {
		t.Fatalf("GetRequestLogByID failed: %v", err)
	}
	if len(got.RequestBody) != 8192 {
		t.Errorf("expected request_body capped at 8192, got %d", len(got.RequestBody))
	}
	if len(got.ResponseText) != 4096 {
		t.Errorf("expected response_text capped at 4096, got %d", len(got.ResponseText))
	}
	if !strings.HasPrefix(longBody, got.RequestBody[:100]) {
		t.Errorf("request_body prefix mismatch")
	}

	if _, err := repo.GetRequestLogByID(ctx, 999999999); err == nil {
		t.Errorf("expected error for missing log id")
	}
}

func TestRequestLogsDateRangeFilter(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Three logs with distinct WIB-day creation times (stored as UTC).
	newLog := func(dayOffset int) *entity.RequestLog {
		return &entity.RequestLog{
			UserID:      "user-dates",
			APIKeyID:    "key-dates",
			Path:        "/v1/chat/completions",
			Method:      "POST",
			Model:       "ag/gemini-3.8-flash",
			TotalTokens: 10,
			StatusCode:  200,
			ClientIP:    "127.0.0.1",
			CreatedAt:   time.Now().UTC().Add(time.Duration(dayOffset) * 24 * time.Hour),
		}
	}
	for i := 0; i < 3; i++ {
		if err := repo.CreateRequestLog(ctx, newLog(i)); err != nil {
			t.Fatalf("CreateRequestLog failed: %v", err)
		}
	}

	// Filter to only the middle day (WIB start-of-day bounds).
	loc, _ := time.LoadLocation("Asia/Jakarta")
	nowWIB := time.Now().In(loc)
	start := time.Date(nowWIB.Year(), nowWIB.Month(), nowWIB.Day(), 0, 0, 0, 0, loc)
	end := start.Add(24*time.Hour - time.Second)

	logs, _, err := repo.GetRequestLogsCursor(ctx, 100, "", "next", "", "", 0, &start, &end)
	if err != nil {
		t.Fatalf("GetRequestLogsCursor failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected exactly 1 log in the middle WIB day, got %d", len(logs))
	}

	// No bounds returns all rows.
	logsAll, _, err := repo.GetRequestLogsCursor(ctx, 100, "", "next", "", "", 0, nil, nil)
	if err != nil {
		t.Fatalf("GetRequestLogsCursor (no bounds) failed: %v", err)
	}
	if len(logsAll) != 3 {
		t.Fatalf("expected 3 logs without bounds, got %d", len(logsAll))
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

func TestSQLiteRepo_UnitOfWork(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Seed a user
	u := &entity.User{
		ID:         "uow-user-1",
		Username:   "uowuser",
		Name:       "UoW User",
		TokenQuota: 1000,
		IsActive:   true,
	}
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// 1. Successful transaction: Credit tokens and update transaction
	tx := &entity.Transaction{
		ID:        "uow-tx-1",
		UserID:    "uow-user-1",
		PackageID: "pkg-1",
		Tokens:    500,
		AmountIDR: 50000,
		Status:    "pending",
	}
	if err := repo.CreateTransaction(ctx, tx); err != nil {
		t.Fatalf("CreateTransaction failed: %v", err)
	}

	err := repo.Do(ctx, func(txRepo Repository) error {
		if err := txRepo.CreditUserTokens(ctx, "uow-user-1", 500); err != nil {
			return err
		}
		return txRepo.UpdateTransactionStatus(ctx, "uow-tx-1", "settlement", "qris", "midtrans-uow-1")
	})
	if err != nil {
		t.Fatalf("Do commit failed: %v", err)
	}

	// Verify committed state
	uAfter, _ := repo.GetUserByID(ctx, "uow-user-1")
	if uAfter.TokenQuota != 1500 {
		t.Errorf("expected 1500 tokens after commit, got %d", uAfter.TokenQuota)
	}
	txAfter, _ := repo.GetTransactionByID(ctx, "uow-tx-1")
	if txAfter.Status != "settlement" {
		t.Errorf("expected settlement status, got %s", txAfter.Status)
	}

	// 2. Failing transaction: Rollback must revert all changes within the tx
	errRollback := repo.Do(ctx, func(txRepo Repository) error {
		if err := txRepo.CreditUserTokens(ctx, "uow-user-1", 10000); err != nil {
			return err
		}
		// Return error to trigger rollback
		return fmt.Errorf("intentional failure to trigger rollback")
	})
	if errRollback == nil {
		t.Fatal("expected error from rolling back transaction, got nil")
	}

	// Verify rollback state (token quota must remain 1500)
	uAfterRollback, _ := repo.GetUserByID(ctx, "uow-user-1")
	if uAfterRollback.TokenQuota != 1500 {
		t.Errorf("expected token quota to remain 1500 after rollback, got %d", uAfterRollback.TokenQuota)
	}
}
