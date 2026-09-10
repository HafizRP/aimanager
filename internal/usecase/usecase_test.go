package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"9router-gateway/internal/models"
)

// ---- Fake store implementation ----

type fakeStore struct {
	users        map[string]*models.User
	keys         map[string]*models.APIKey
	packages     map[string]*models.TokenPackage
	transactions map[string]*models.Transaction
	settings     map[string]string
	sessions     map[string]string // token -> userID
	loginFails   map[string]int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:        map[string]*models.User{},
		keys:         map[string]*models.APIKey{},
		packages:     map[string]*models.TokenPackage{},
		transactions: map[string]*models.Transaction{},
		settings:     map[string]string{},
		sessions:     map[string]string{},
		loginFails:   map[string]int{},
	}
}

// UserStore
func (f *fakeStore) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return u, nil
}
func (f *fakeStore) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	for _, u := range f.users {
		if strings.EqualFold(u.Username, username) || strings.EqualFold(u.Name, username) {
			return u, nil
		}
	}
	return nil, errors.New("not found")
}
func (f *fakeStore) GetAllUsers(ctx context.Context) ([]models.User, error) {
	out := []models.User{}
	for _, u := range f.users {
		out = append(out, *u)
	}
	return out, nil
}
func (f *fakeStore) CreateUser(ctx context.Context, u *models.User) error {
	f.users[u.ID] = u
	return nil
}
func (f *fakeStore) UpdateUser(ctx context.Context, u *models.User) error {
	f.users[u.ID] = u
	return nil
}
func (f *fakeStore) UpdateUserPassword(ctx context.Context, id, hash string) error {
	if u, ok := f.users[id]; ok {
		u.PasswordHash = hash
	}
	return nil
}
func (f *fakeStore) ResetUserUsage(ctx context.Context, id string) error {
	if u, ok := f.users[id]; ok {
		u.TokensUsed = 0
	}
	return nil
}
func (f *fakeStore) UpdateUserLastLogin(ctx context.Context, id string) error { return nil }
func (f *fakeStore) ToggleUserStatus(ctx context.Context, id string, active bool) error {
	if u, ok := f.users[id]; ok {
		u.IsActive = active
	}
	return nil
}
func (f *fakeStore) DeleteUser(ctx context.Context, id string) error {
	delete(f.users, id)
	return nil
}
func (f *fakeStore) DeductTokens(ctx context.Context, id string, tokens int) error {
	if u, ok := f.users[id]; ok {
		u.TokensUsed += int64(tokens)
	}
	return nil
}

// APIKeyStore
func (f *fakeStore) GetAPIKeyByKey(ctx context.Context, key string) (*models.APIKey, error) {
	for _, k := range f.keys {
		if k.Key == key {
			return k, nil
		}
	}
	return nil, errors.New("not found")
}
func (f *fakeStore) GetAPIKeysByUserID(ctx context.Context, userID string) ([]models.APIKey, error) {
	out := []models.APIKey{}
	for _, k := range f.keys {
		if k.UserID == userID {
			out = append(out, *k)
		}
	}
	return out, nil
}
func (f *fakeStore) GetAllAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	out := []models.APIKey{}
	for _, k := range f.keys {
		out = append(out, *k)
	}
	return out, nil
}
func (f *fakeStore) CreateAPIKey(ctx context.Context, k *models.APIKey) error {
	f.keys[k.ID] = k
	return nil
}
func (f *fakeStore) ToggleAPIKeyStatus(ctx context.Context, id string, active bool) error {
	if k, ok := f.keys[id]; ok {
		k.IsActive = active
	}
	return nil
}
func (f *fakeStore) DeleteAPIKey(ctx context.Context, id string) error {
	delete(f.keys, id)
	return nil
}
func (f *fakeStore) UpdateKeyLastUsed(ctx context.Context, id string) error { return nil }

// RequestLogStore
func (f *fakeStore) CreateRequestLog(ctx context.Context, l *models.RequestLog) error { return nil }
func (f *fakeStore) GetRequestLogs(ctx context.Context, limit, offset int, userID, model string, status int) ([]models.RequestLog, int, error) {
	return []models.RequestLog{}, 0, nil
}
func (f *fakeStore) GetRequestLogsCursor(ctx context.Context, limit int, cursor, dir, userID, model string, status int) ([]models.RequestLog, *models.CursorPageInfo, error) {
	return []models.RequestLog{}, &models.CursorPageInfo{Limit: limit}, nil
}

// StatsStore
func (f *fakeStore) GetDashboardStats(ctx context.Context, tf string) (*models.DashboardStats, error) {
	return &models.DashboardStats{TotalUsers: int64(len(f.users))}, nil
}
func (f *fakeStore) GetUserDashboardStats(ctx context.Context, userID, tf string) (*models.DashboardStats, error) {
	return &models.DashboardStats{}, nil
}

// TransactionStore
func (f *fakeStore) GetActivePackages(ctx context.Context) ([]models.TokenPackage, error) {
	out := []models.TokenPackage{}
	for _, p := range f.packages {
		out = append(out, *p)
	}
	return out, nil
}
func (f *fakeStore) GetPackageByID(ctx context.Context, id string) (*models.TokenPackage, error) {
	p, ok := f.packages[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return p, nil
}
func (f *fakeStore) CreateTransaction(ctx context.Context, tx *models.Transaction) error {
	f.transactions[tx.ID] = tx
	return nil
}
func (f *fakeStore) GetTransactionByID(ctx context.Context, id string) (*models.Transaction, error) {
	tx, ok := f.transactions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return tx, nil
}
func (f *fakeStore) UpdateTransactionStatus(ctx context.Context, id, status, payType, mtTx string) error {
	if tx, ok := f.transactions[id]; ok {
		tx.Status = status
		tx.PaymentType = payType
		tx.MidtransTxID = mtTx
	}
	return nil
}
func (f *fakeStore) GetTransactionsByUserID(ctx context.Context, userID string, limit, offset int) ([]models.Transaction, int, error) {
	return []models.Transaction{}, 0, nil
}
func (f *fakeStore) GetAllTransactions(ctx context.Context, limit, offset int) ([]models.Transaction, int, error) {
	return []models.Transaction{}, 0, nil
}
func (f *fakeStore) CreditUserTokens(ctx context.Context, userID string, tokens int64) error {
	if u, ok := f.users[userID]; ok {
		u.TokenQuota += tokens
	}
	return nil
}

// SettingsStore
func (f *fakeStore) SaveSetting(ctx context.Context, k, v string) error {
	f.settings[k] = v
	return nil
}
func (f *fakeStore) GetSetting(ctx context.Context, k string) (string, error) {
	v, ok := f.settings[k]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

// SessionStore
func (f *fakeStore) CreateSession(ctx context.Context, token, userID string, exp time.Time) error {
	f.sessions[token] = userID
	return nil
}
func (f *fakeStore) GetSessionUser(ctx context.Context, token string) (*models.User, error) {
	userID, ok := f.sessions[token]
	if !ok {
		return nil, errors.New("not found")
	}
	return f.users[userID], nil
}
func (f *fakeStore) DeleteSession(ctx context.Context, token string) error {
	delete(f.sessions, token)
	return nil
}
func (f *fakeStore) CleanExpiredSessions(ctx context.Context) error { return nil }

// LoginAttemptStore
func (f *fakeStore) RecordLoginAttempt(ctx context.Context, ip string) error {
	f.loginFails[ip]++
	return nil
}
func (f *fakeStore) GetRecentLoginAttempts(ctx context.Context, ip string, window int) (int, error) {
	return f.loginFails[ip], nil
}
func (f *fakeStore) ClearLoginAttempts(ctx context.Context, ip string) error {
	f.loginFails[ip] = 0
	return nil
}
func (f *fakeStore) CleanOldLoginAttempts(ctx context.Context) error { return nil }

// Ensure fakeStore satisfies Store
var _ Store = (*fakeStore)(nil)

// ---- Tests ----

func TestAuthService_Authenticate(t *testing.T) {
	store := newFakeStore()
	hash, _ := HashPassword("secret123")
	store.users["u1"] = &models.User{
		ID: "u1", Username: "hafiz", Name: "Hafiz",
		PasswordHash: hash, Role: "user", IsActive: true,
	}

	svc := NewAuthService(store, "test-secret", "admin", "admin")
	ctx := context.Background()

	u, err := svc.Authenticate(ctx, AuthInput{Username: "hafiz", Password: "secret123"}, "10.0.0.1")
	if err != nil {
		t.Fatalf("expected successful login, got %v", err)
	}
	if u.Username != "hafiz" {
		t.Errorf("expected user hafiz, got %s", u.Username)
	}

	// Wrong password
	if _, err := svc.Authenticate(ctx, AuthInput{Username: "hafiz", Password: "wrong"}, "10.0.0.1"); err == nil {
		t.Fatal("expected error for wrong password")
	}
	// Attempt counter incremented
	if store.loginFails["10.0.0.1"] == 0 {
		t.Error("expected failed attempt recorded")
	}
}

func TestAuthService_SessionLifecycle(t *testing.T) {
	store := newFakeStore()
	store.users["u1"] = &models.User{ID: "u1", Username: "hafiz", IsActive: true}
	svc := NewAuthService(store, "test-secret", "admin", "admin")
	ctx := context.Background()

	token, exp, err := svc.StartSession(ctx, "u1")
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	if token == "" || exp.IsZero() {
		t.Error("expected non-empty token and expiry")
	}

	u, err := svc.GetSessionUser(ctx, token)
	if err != nil || u == nil {
		t.Fatalf("expected session user, got %v", err)
	}
	if u.ID != "u1" {
		t.Errorf("expected u1, got %s", u.ID)
	}

	if err := svc.DeleteSession(ctx, token); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	if _, err := svc.GetSessionUser(ctx, token); err == nil {
		t.Fatal("expected session gone after delete")
	}
}

func TestSignSession(t *testing.T) {
	a := SignSession("secret", "csrf:abc")
	b := SignSession("secret", "csrf:abc")
	if a != b {
		t.Fatal("deterministic HMAC expected")
	}
	c := SignSession("other", "csrf:abc")
	if a == c {
		t.Fatal("different secret should produce different signature")
	}
}

func TestUserService_CreateUser(t *testing.T) {
	store := newFakeStore()
	svc := NewUserService(store, nil)

	u, pass, key, err := svc.CreateUser(context.Background(), CreateUserInput{
		Name: "Budi", Username: "budi", Password: "pass123",
		Role: "user", TokenQuota: 1000, CreateKey: true,
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if u.Username != "budi" || u.TokenQuota != 1000 {
		t.Errorf("unexpected user: %+v", u)
	}
	if pass != "pass123" {
		t.Errorf("expected pass123, got %s", pass)
	}
	if key == "" {
		t.Error("expected generated API key")
	}
	if !CheckPasswordHash("pass123", u.PasswordHash) {
		t.Error("password hash mismatch")
	}
}

func TestKeyService_CreateKey(t *testing.T) {
	store := newFakeStore()
	store.users["u1"] = &models.User{ID: "u1", Username: "hafiz", Name: "Hafiz"}
	svc := NewKeyService(store, nil)

	exp := time.Now().Add(24 * time.Hour)
	k, err := svc.CreateKey(context.Background(), CreateKeyInput{
		UserID: "u1", Name: "Test Key", RateLimitRPM: 10, ExpiresAt: &exp,
	})
	if err != nil {
		t.Fatalf("CreateKey failed: %v", err)
	}
	if !strings.HasPrefix(k.Key, "sk-gw-") {
		t.Errorf("expected sk-gw- prefix, got %s", k.Key)
	}
	if k.RateLimitRPM != 10 || k.ExpiresAt == nil {
		t.Errorf("unexpected key: %+v", k)
	}

	// Duplicate custom key rejected
	_, err = svc.CreateKey(context.Background(), CreateKeyInput{UserID: "u1", CustomKey: k.Key})
	if err == nil {
		t.Fatal("expected duplicate key error")
	}
}

func TestBillingService_ManualCredit(t *testing.T) {
	store := newFakeStore()
	store.users["u1"] = &models.User{ID: "u1", Username: "hafiz", TokenQuota: 100}
	svc := NewBillingService(store)

	if err := svc.ManualCredit(context.Background(), "u1", 500); err != nil {
		t.Fatalf("ManualCredit failed: %v", err)
	}
	if store.users["u1"].TokenQuota != 600 {
		t.Errorf("expected quota 600, got %d", store.users["u1"].TokenQuota)
	}
	if len(store.transactions) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(store.transactions))
	}

	// Invalid amount
	if err := svc.ManualCredit(context.Background(), "u1", 0); err == nil {
		t.Fatal("expected error for zero amount")
	}
}

func TestBillingService_MarkPaid_NoDoubleCredit(t *testing.T) {
	store := newFakeStore()
	store.users["u1"] = &models.User{ID: "u1", Username: "hafiz", TokenQuota: 100}
	svc := NewBillingService(store)

	tx := &models.Transaction{ID: "tx1", UserID: "u1", PackageID: "p1", Tokens: 1000, Status: "pending"}
	store.transactions["tx1"] = tx

	if err := svc.MarkPaid(context.Background(), tx, "bank_transfer", "mt1"); err != nil {
		t.Fatalf("MarkPaid failed: %v", err)
	}
	if store.users["u1"].TokenQuota != 1100 {
		t.Errorf("expected quota 1100, got %d", store.users["u1"].TokenQuota)
	}

	// Second call (duplicate webhook) must not double-credit
	if err := svc.MarkPaid(context.Background(), tx, "bank_transfer", "mt1"); err != nil {
		t.Fatalf("MarkPaid second call failed: %v", err)
	}
	if store.users["u1"].TokenQuota != 1100 {
		t.Errorf("expected still 1100 (no double credit), got %d", store.users["u1"].TokenQuota)
	}
}

func TestSettingsService_UpdateUpstream(t *testing.T) {
	store := newFakeStore()
	cfg := &fakeConfig{}
	svc := NewSettingsService(store, cfg, nil)

	err := svc.UpdateUpstream(context.Background(), "http://10.0.0.88:20128/", "", "key123")
	if err != nil {
		t.Fatalf("UpdateUpstream failed: %v", err)
	}
	if cfg.upstreamURL != "http://10.0.0.88:20128" {
		t.Errorf("expected trimmed URL, got %q", cfg.upstreamURL)
	}
	if v, _ := store.GetSetting(context.Background(), "upstream_api_key"); v != "key123" {
		t.Errorf("expected key123 saved, got %q", v)
	}

	if err := svc.UpdateUpstream(context.Background(), "", "", ""); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

type fakeConfig struct {
	upstreamURL string
}

func (f *fakeConfig) SetUpstreamURL(u string)          { f.upstreamURL = u }
func (f *fakeConfig) GetUpstreamURL() string           { return f.upstreamURL }
func (f *fakeConfig) SetUpstreamAPIKey(k string)       {}
func (f *fakeConfig) SetNineRouterDBPath(p string)     {}
func (f *fakeConfig) GetNineRouterDBPath() string      { return "" }
func (f *fakeConfig) GetUpstreamAPIKey() string        { return "" }
func (f *fakeConfig) MidtransServerKey() string        { return "" }
func (f *fakeConfig) SetMidtransServerKey(k string)    {}
func (f *fakeConfig) MidtransClientKey() string        { return "" }
func (f *fakeConfig) SetMidtransClientKey(k string)    {}
func (f *fakeConfig) MidtransMerchantID() string       { return "" }
func (f *fakeConfig) SetMidtransMerchantID(id string)  {}
func (f *fakeConfig) MidtransIsProduction() bool       { return false }
func (f *fakeConfig) SetMidtransIsProduction(v bool)   {}