// Package repository provides the persistence layer backed by SQLite.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"9router-gateway/internal/entity"
)

// Repository defines the data-access contract for all gateway entities.
type Repository interface {
	// Users
	GetUserByID(ctx context.Context, id string) (*entity.User, error)
	GetUserByUsername(ctx context.Context, username string) (*entity.User, error)
	GetAllUsers(ctx context.Context) ([]entity.User, error)
	CreateUser(ctx context.Context, u *entity.User) error
	UpdateUser(ctx context.Context, u *entity.User) error
	UpdateUserPassword(ctx context.Context, id, passwordHash string) error
	ResetUserUsage(ctx context.Context, id string) error
	UpdateUserLastLogin(ctx context.Context, id string) error
	ToggleUserStatus(ctx context.Context, id string, isActive bool) error
	DeleteUser(ctx context.Context, id string) error
	DeductTokens(ctx context.Context, userID string, tokens int) error

	// API Keys
	GetAPIKeyByKey(ctx context.Context, key string) (*entity.APIKey, error)
	GetAPIKeysByUserID(ctx context.Context, userID string) ([]entity.APIKey, error)
	GetAllAPIKeys(ctx context.Context) ([]entity.APIKey, error)
	CreateAPIKey(ctx context.Context, k *entity.APIKey) error
	ToggleAPIKeyStatus(ctx context.Context, id string, isActive bool) error
	DeleteAPIKey(ctx context.Context, id string) error
	UpdateKeyLastUsed(ctx context.Context, id string) error
	UpdateKeyTokenUsage(ctx context.Context, id string, tokens int) error
	GetAPIKeyTokenUsage(ctx context.Context, id string) (int64, error)
	UpdateKeyBudgets(ctx context.Context, id string, maxTokensLimit, dailyQuota int) error
	GetTodayTokenUsageByUser(ctx context.Context, userID string) (int64, error)
	GetTodayTokenUsageByKey(ctx context.Context, keyID string) (int64, error)
	GetKeyWindowStats(ctx context.Context, since time.Time) ([]KeyWindowStats, error)
	GetKeyBaselineTokens(ctx context.Context, days int) (map[string]float64, error)

	// Logs
	CreateRequestLog(ctx context.Context, log *entity.RequestLog) error
	CreateSecurityEvent(ctx context.Context, ev *entity.SecurityEvent) error
	ListSecurityEvents(ctx context.Context, limit int) ([]entity.SecurityEvent, error)
	GetRequestLogs(ctx context.Context, limit, offset int, userID, modelFilter string, statusFilter int) ([]entity.RequestLog, int, error)
	GetRequestLogsCursor(ctx context.Context, limit int, cursor, direction, userID, modelFilter string, statusFilter int) ([]entity.RequestLog, *entity.CursorPageInfo, error)

	// Stats
	GetDashboardStats(ctx context.Context, timeframe string) (*entity.DashboardStats, error)
	GetUserDashboardStats(ctx context.Context, userID string, timeframe string) (*entity.DashboardStats, error)

	// Billing & Midtrans Transactions
	GetActivePackages(ctx context.Context) ([]entity.TokenPackage, error)
	GetPackageByID(ctx context.Context, id string) (*entity.TokenPackage, error)
	CreateTransaction(ctx context.Context, tx *entity.Transaction) error
	GetTransactionByID(ctx context.Context, id string) (*entity.Transaction, error)
	UpdateTransactionStatus(ctx context.Context, id, status, paymentType, midtransTxID string) error
	GetTransactionsByUserID(ctx context.Context, userID string, limit, offset int) ([]entity.Transaction, int, error)
	GetAllTransactions(ctx context.Context, limit, offset int) ([]entity.Transaction, int, error)
	CreditUserTokens(ctx context.Context, userID string, tokens int64) error

	// Settings
	SaveSetting(ctx context.Context, key, value string) error
	GetSetting(ctx context.Context, key string) (string, error)

	// Server-Side Sessions
	CreateSession(ctx context.Context, token, userID string, expiresAt time.Time) error
	GetSessionUser(ctx context.Context, token string) (*entity.User, error)
	DeleteSession(ctx context.Context, token string) error
	CleanExpiredSessions(ctx context.Context) error

	// Login Rate Limiting
	RecordLoginAttempt(ctx context.Context, ip string) error
	GetRecentLoginAttempts(ctx context.Context, ip string, windowMinutes int) (int, error)
	ClearLoginAttempts(ctx context.Context, ip string) error
	CleanOldLoginAttempts(ctx context.Context) error
}

// SQLiteRepo implements Repository using a *sql.DB.
type SQLiteRepo struct {
	db *sql.DB
}

// NewSQLiteRepo wraps an open database connection into an SQLiteRepo.
func NewSQLiteRepo(db *sql.DB) *SQLiteRepo {
	return &SQLiteRepo{db: db}
}

func parseTimeFlexible(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// User methods

// GetUserByID returns a user by its unique ID.
func (r *SQLiteRepo) GetUserByID(ctx context.Context, id string) (*entity.User, error) {
	query := `SELECT id, COALESCE(username, ''), name, COALESCE(password_hash, ''), role, token_quota, tokens_used, COALESCE(daily_token_quota, 0), allowed_models, 
	                 COALESCE(rate_limit_rpm, 0), COALESCE(rate_limit_tpm, 0), is_active, created_at, updated_at, last_login_at 
	          FROM users WHERE id = ?`
	var u entity.User
	var createdAt, updatedAt string
	var lastLogin sql.NullString
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&u.ID, &u.Username, &u.Name, &u.PasswordHash, &u.Role, &u.TokenQuota, &u.TokensUsed, &u.DailyTokenQuota, &u.AllowedModels,
		&u.RateLimitRPM, &u.RateLimitTPM, &u.IsActive, &createdAt, &updatedAt, &lastLogin,
	)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseTimeFlexible(createdAt)
	u.UpdatedAt = parseTimeFlexible(updatedAt)
	if lastLogin.Valid {
		t := parseTimeFlexible(lastLogin.String)
		if !t.IsZero() {
			u.LastLoginAt = &t
		}
	}
	return &u, nil
}

// GetUserByUsername returns a user matched by username or name (case-insensitive).
func (r *SQLiteRepo) GetUserByUsername(ctx context.Context, username string) (*entity.User, error) {
	query := `SELECT id, COALESCE(username, ''), name, COALESCE(password_hash, ''), role, token_quota, tokens_used, COALESCE(daily_token_quota, 0), allowed_models, 
	                 COALESCE(rate_limit_rpm, 0), COALESCE(rate_limit_tpm, 0), is_active, created_at, updated_at, last_login_at 
	          FROM users WHERE LOWER(username) = LOWER(?) OR LOWER(name) = LOWER(?) LIMIT 1`
	var u entity.User
	var createdAt, updatedAt string
	var lastLogin sql.NullString
	err := r.db.QueryRowContext(ctx, query, username, username).Scan(
		&u.ID, &u.Username, &u.Name, &u.PasswordHash, &u.Role, &u.TokenQuota, &u.TokensUsed, &u.DailyTokenQuota, &u.AllowedModels,
		&u.RateLimitRPM, &u.RateLimitTPM, &u.IsActive, &createdAt, &updatedAt, &lastLogin,
	)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseTimeFlexible(createdAt)
	u.UpdatedAt = parseTimeFlexible(updatedAt)
	if lastLogin.Valid {
		t := parseTimeFlexible(lastLogin.String)
		if !t.IsZero() {
			u.LastLoginAt = &t
		}
	}
	return &u, nil
}

// GetAllUsers returns every user ordered by creation time descending.
func (r *SQLiteRepo) GetAllUsers(ctx context.Context) ([]entity.User, error) {
	query := `
		SELECT u.id, COALESCE(u.username, ''), u.name, COALESCE(u.password_hash, ''), u.role, u.token_quota, u.tokens_used, COALESCE(u.daily_token_quota, 0), u.allowed_models,
		       COALESCE(u.rate_limit_rpm, 0), COALESCE(u.rate_limit_tpm, 0), u.is_active, u.created_at, u.updated_at, u.last_login_at,
		       (SELECT COUNT(*) FROM api_keys WHERE user_id = u.id) as key_count
		FROM users u
		ORDER BY u.created_at DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []entity.User
	for rows.Next() {
		var u entity.User
		var createdAt, updatedAt string
		var lastLogin sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.PasswordHash, &u.Role, &u.TokenQuota, &u.TokensUsed, &u.DailyTokenQuota, &u.AllowedModels,
			&u.RateLimitRPM, &u.RateLimitTPM, &u.IsActive, &createdAt, &updatedAt, &lastLogin, &u.KeyCount); err != nil {
			return nil, err
		}
		u.CreatedAt = parseTimeFlexible(createdAt)
		u.UpdatedAt = parseTimeFlexible(updatedAt)
		if lastLogin.Valid {
			t := parseTimeFlexible(lastLogin.String)
			if !t.IsZero() {
				u.LastLoginAt = &t
			}
		}
		users = append(users, u)
	}
	return users, nil
}

// CreateUser inserts a new user record.
func (r *SQLiteRepo) CreateUser(ctx context.Context, u *entity.User) error {
	query := `INSERT INTO users (id, username, name, password_hash, role, token_quota, tokens_used, allowed_models, is_active, created_at, updated_at) 
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`
	_, err := r.db.ExecContext(ctx, query, u.ID, u.Username, u.Name, u.PasswordHash, u.Role, u.TokenQuota, u.TokensUsed, u.AllowedModels, u.IsActive)
	return err
}

// UpdateUser overwrites mutable fields of an existing user.
func (r *SQLiteRepo) UpdateUser(ctx context.Context, u *entity.User) error {
	query := `UPDATE users 
	          SET username = ?, name = ?, role = ?, token_quota = ?, daily_token_quota = ?, allowed_models = ?, is_active = ?, updated_at = datetime('now')
	          WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, u.Username, u.Name, u.Role, u.TokenQuota, u.DailyTokenQuota, u.AllowedModels, u.IsActive, u.ID)
	return err
}

// UpdateUserPassword replaces the password hash for the specified user.
func (r *SQLiteRepo) UpdateUserPassword(ctx context.Context, id, passwordHash string) error {
	query := `UPDATE users SET password_hash = ?, updated_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, passwordHash, id)
	return err
}

// ResetUserUsage zeroes the user's token usage counter.
func (r *SQLiteRepo) ResetUserUsage(ctx context.Context, id string) error {
	query := `UPDATE users SET tokens_used = 0, updated_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// UpdateUserLastLogin sets the last-login timestamp to now.
func (r *SQLiteRepo) UpdateUserLastLogin(ctx context.Context, id string) error {
	query := `UPDATE users SET last_login_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// ToggleUserStatus enables or disables a user account.
func (r *SQLiteRepo) ToggleUserStatus(ctx context.Context, id string, isActive bool) error {
	query := `UPDATE users SET is_active = ?, updated_at = datetime('now') WHERE id = ?`
	val := 0
	if isActive {
		val = 1
	}
	_, err := r.db.ExecContext(ctx, query, val, id)
	return err
}

// DeleteUser removes a user and cascades to their API keys.
func (r *SQLiteRepo) DeleteUser(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM api_keys WHERE user_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id); err != nil {
		return err
	}

	return tx.Commit()
}

// DeductTokens increments the user's token usage counter.
func (r *SQLiteRepo) DeductTokens(ctx context.Context, userID string, tokens int) error {
	if tokens <= 0 {
		return nil
	}
	query := `UPDATE users SET tokens_used = tokens_used + ?, updated_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, tokens, userID)
	return err
}

// API Key methods

// GetAPIKeyByKey looks up an API key by its raw key string.
func (r *SQLiteRepo) GetAPIKeyByKey(ctx context.Context, key string) (*entity.APIKey, error) {
	query := `SELECT k.id, k.user_id, k.key, k.name, COALESCE(k.allowed_models, ''), COALESCE(k.rate_limit_rpm, 0), COALESCE(k.max_tokens_limit, 0), COALESCE(k.daily_token_quota, 0), k.is_active, k.created_at, k.last_used_at, k.expires_at, COALESCE(k.tokens_used, 0), u.name 
	          FROM api_keys k 
	          JOIN users u ON k.user_id = u.id 
	          WHERE k.key = ?`
	var k entity.APIKey
	var createdAt string
	var lastUsed sql.NullString
	var expiresAt sql.NullString
	err := r.db.QueryRowContext(ctx, query, key).Scan(
		&k.ID, &k.UserID, &k.Key, &k.Name, &k.AllowedModels, &k.RateLimitRPM, &k.MaxTokensLimit, &k.DailyTokenQuota, &k.IsActive, &createdAt, &lastUsed, &expiresAt, &k.TokenUsage, &k.UserName,
	)
	if err != nil {
		return nil, err
	}
	k.CreatedAt = parseTimeFlexible(createdAt)
	if lastUsed.Valid {
		t := parseTimeFlexible(lastUsed.String)
		if !t.IsZero() {
			k.LastUsedAt = &t
		}
	}
	if expiresAt.Valid {
		t := parseTimeFlexible(expiresAt.String)
		if !t.IsZero() {
			k.ExpiresAt = &t
		}
	}
	return &k, nil
}

// GetAPIKeysByUserID returns all API keys belonging to a user.
func (r *SQLiteRepo) GetAPIKeysByUserID(ctx context.Context, userID string) ([]entity.APIKey, error) {
	query := `SELECT id, user_id, key, name, COALESCE(allowed_models, ''), COALESCE(rate_limit_rpm, 0), COALESCE(max_tokens_limit, 0), COALESCE(daily_token_quota, 0), is_active, created_at, last_used_at, expires_at, COALESCE(tokens_used, 0) 
	          FROM api_keys WHERE user_id = ? ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []entity.APIKey
	for rows.Next() {
		var k entity.APIKey
		var createdAt string
		var lastUsed sql.NullString
		var expiresAt sql.NullString
		if err := rows.Scan(&k.ID, &k.UserID, &k.Key, &k.Name, &k.AllowedModels, &k.RateLimitRPM, &k.MaxTokensLimit, &k.DailyTokenQuota, &k.IsActive, &createdAt, &lastUsed, &expiresAt, &k.TokenUsage); err != nil {
			return nil, err
		}
		k.CreatedAt = parseTimeFlexible(createdAt)
		if lastUsed.Valid {
			t := parseTimeFlexible(lastUsed.String)
			if !t.IsZero() {
				k.LastUsedAt = &t
			}
		}
		if expiresAt.Valid {
			t := parseTimeFlexible(expiresAt.String)
			if !t.IsZero() {
				k.ExpiresAt = &t
			}
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// GetAllAPIKeys returns every API key across all users.
func (r *SQLiteRepo) GetAllAPIKeys(ctx context.Context) ([]entity.APIKey, error) {
	query := `SELECT k.id, k.user_id, k.key, k.name, COALESCE(k.allowed_models, ''), COALESCE(k.rate_limit_rpm, 0), COALESCE(k.max_tokens_limit, 0), COALESCE(k.daily_token_quota, 0), k.is_active, k.created_at, k.last_used_at, k.expires_at, COALESCE(k.tokens_used, 0), u.name 
	          FROM api_keys k 
	          JOIN users u ON k.user_id = u.id 
	          ORDER BY k.created_at DESC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []entity.APIKey
	for rows.Next() {
		var k entity.APIKey
		var createdAt string
		var lastUsed sql.NullString
		var expiresAt sql.NullString
		if err := rows.Scan(&k.ID, &k.UserID, &k.Key, &k.Name, &k.AllowedModels, &k.RateLimitRPM, &k.MaxTokensLimit, &k.DailyTokenQuota, &k.IsActive, &createdAt, &lastUsed, &expiresAt, &k.TokenUsage, &k.UserName); err != nil {
			return nil, err
		}
		k.CreatedAt = parseTimeFlexible(createdAt)
		if lastUsed.Valid {
			t := parseTimeFlexible(lastUsed.String)
			if !t.IsZero() {
				k.LastUsedAt = &t
			}
		}
		if expiresAt.Valid {
			t := parseTimeFlexible(expiresAt.String)
			if !t.IsZero() {
				k.ExpiresAt = &t
			}
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// CreateAPIKey inserts a new API key record.
func (r *SQLiteRepo) CreateAPIKey(ctx context.Context, k *entity.APIKey) error {
	query := `INSERT INTO api_keys (id, user_id, key, name, allowed_models, rate_limit_rpm, max_tokens_limit, daily_token_quota, is_active, expires_at, created_at) 
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`
	_, err := r.db.ExecContext(ctx, query, k.ID, k.UserID, k.Key, k.Name, k.AllowedModels, k.RateLimitRPM, k.MaxTokensLimit, k.DailyTokenQuota, k.IsActive, formatNullableTime(k.ExpiresAt))
	return err
}

func formatNullableTime(t *time.Time) interface{} {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

// ToggleAPIKeyStatus enables or disables an API key.
func (r *SQLiteRepo) ToggleAPIKeyStatus(ctx context.Context, id string, isActive bool) error {
	val := 0
	if isActive {
		val = 1
	}
	query := `UPDATE api_keys SET is_active = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, val, id)
	return err
}

// DeleteAPIKey removes an API key by its ID.
func (r *SQLiteRepo) DeleteAPIKey(ctx context.Context, id string) error {
	query := `DELETE FROM api_keys WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// UpdateKeyLastUsed sets the last-used timestamp of an API key to now.
func (r *SQLiteRepo) UpdateKeyLastUsed(ctx context.Context, id string) error {
	query := `UPDATE api_keys SET last_used_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// UpdateKeyTokenUsage adds tokens to a key's lifetime spent counter.
func (r *SQLiteRepo) UpdateKeyTokenUsage(ctx context.Context, id string, tokens int) error {
	if tokens <= 0 {
		return nil
	}
	query := `UPDATE api_keys SET tokens_used = COALESCE(tokens_used, 0) + ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, tokens, id)
	return err
}

// GetAPIKeyTokenUsage returns the lifetime tokens spent on a key.
func (r *SQLiteRepo) GetAPIKeyTokenUsage(ctx context.Context, id string) (int64, error) {
	var used int64
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(tokens_used, 0) FROM api_keys WHERE id = ?`, id).Scan(&used)
	return used, err
}

// UpdateKeyBudgets sets a key's lifetime and daily token budgets.
func (r *SQLiteRepo) UpdateKeyBudgets(ctx context.Context, id string, maxTokensLimit, dailyQuota int) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET max_tokens_limit = ?, daily_token_quota = ? WHERE id = ?`, maxTokensLimit, dailyQuota, id)
	return err
}

// wibDayExpr matches request_logs rows created during the current WIB day.
const wibDayExpr = `date(created_at, '+7 hours') = date('now', '+7 hours')`

// GetTodayTokenUsageByUser sums tokens spent by a user during the current WIB day.
func (r *SQLiteRepo) GetTodayTokenUsageByUser(ctx context.Context, userID string) (int64, error) {
	var total int64
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_tokens), 0) FROM request_logs WHERE user_id = ? AND `+wibDayExpr, userID).Scan(&total)
	return total, err
}

// GetTodayTokenUsageByKey sums tokens spent on a key during the current WIB day.
func (r *SQLiteRepo) GetTodayTokenUsageByKey(ctx context.Context, keyID string) (int64, error) {
	var total int64
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_tokens), 0) FROM request_logs WHERE api_key_id = ? AND `+wibDayExpr, keyID).Scan(&total)
	return total, err
}

// CreateSecurityEvent records an anomaly/self-heal/budget finding.
func (r *SQLiteRepo) CreateSecurityEvent(ctx context.Context, ev *entity.SecurityEvent) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO security_events (kind, user_id, api_key_id, detail, action, created_at) VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		ev.Kind, ev.UserID, ev.APIKeyID, ev.Detail, ev.Action)
	return err
}

// ListSecurityEvents returns recent findings, newest first.
func (r *SQLiteRepo) ListSecurityEvents(ctx context.Context, limit int) ([]entity.SecurityEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, kind, user_id, api_key_id, detail, action, created_at FROM security_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.SecurityEvent
	for rows.Next() {
		var ev entity.SecurityEvent
		var createdAt string
		if err := rows.Scan(&ev.ID, &ev.Kind, &ev.UserID, &ev.APIKeyID, &ev.Detail, &ev.Action, &createdAt); err != nil {
			return nil, err
		}
		ev.CreatedAt = parseTimeFlexible(createdAt)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// KeyWindowStats aggregates per-key request/error counts over a time window.
type KeyWindowStats struct {
	KeyID    string
	UserID   string
	UserName string
	KeyName  string
	Requests int64
	Errors   int64
	Tokens   int64
	IPCount  int64
}

// GetKeyWindowStats aggregates per-key traffic since the given UTC time.
func (r *SQLiteRepo) GetKeyWindowStats(ctx context.Context, since time.Time) ([]KeyWindowStats, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT l.api_key_id, l.user_id, COALESCE(u.name, ''), COALESCE(k.name, ''),
		COUNT(*), SUM(CASE WHEN l.status_code >= 400 THEN 1 ELSE 0 END), COALESCE(SUM(l.total_tokens), 0), COUNT(DISTINCT l.client_ip)
		FROM request_logs l LEFT JOIN users u ON l.user_id = u.id LEFT JOIN api_keys k ON l.api_key_id = k.id
		WHERE l.created_at >= ? GROUP BY l.api_key_id`, since.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KeyWindowStats
	for rows.Next() {
		var s KeyWindowStats
		if err := rows.Scan(&s.KeyID, &s.UserID, &s.UserName, &s.KeyName, &s.Requests, &s.Errors, &s.Tokens, &s.IPCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetKeyBaselineTokens returns avg daily tokens per key over the past N days (excl. today).
func (r *SQLiteRepo) GetKeyBaselineTokens(ctx context.Context, days int) (map[string]float64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT api_key_id, COALESCE(SUM(total_tokens), 0) * 1.0 / ? FROM request_logs
		WHERE date(created_at, '+7 hours') < date('now', '+7 hours')
		AND date(created_at, '+7 hours') >= date('now', '+7 hours', ?)
		GROUP BY api_key_id`, days, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]float64)
	for rows.Next() {
		var id string
		var avg float64
		if err := rows.Scan(&id, &avg); err != nil {
			return nil, err
		}
		out[id] = avg
	}
	return out, rows.Err()
}

// Request logs

// CreateRequestLog inserts a new request log entry.
func (r *SQLiteRepo) CreateRequestLog(ctx context.Context, log *entity.RequestLog) error {
	query := `INSERT INTO request_logs 
	          (user_id, api_key_id, path, method, model, is_stream, prompt_tokens, completion_tokens, total_tokens, status_code, duration_ms, client_ip, error_message, created_at) 
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`
	isStream := 0
	if log.IsStream {
		isStream = 1
	}
	_, err := r.db.ExecContext(ctx, query,
		log.UserID, log.APIKeyID, log.Path, log.Method, log.Model, isStream,
		log.PromptTokens, log.CompletionTokens, log.TotalTokens, log.StatusCode,
		log.DurationMs, log.ClientIP, log.ErrorMessage,
	)
	return err
}

// GetRequestLogs returns paginated request logs with optional filters.
func (r *SQLiteRepo) GetRequestLogs(ctx context.Context, limit, offset int, userID, modelFilter string, statusFilter int) ([]entity.RequestLog, int, error) {
	whereClauses := []string{"1=1"}
	args := []interface{}{}

	if userID != "" {
		whereClauses = append(whereClauses, "l.user_id = ?")
		args = append(args, userID)
	}
	if modelFilter != "" {
		whereClauses = append(whereClauses, "l.model LIKE ?")
		args = append(args, "%"+modelFilter+"%")
	}
	if statusFilter > 0 {
		whereClauses = append(whereClauses, "l.status_code = ?")
		args = append(args, statusFilter)
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM request_logs l WHERE %s", whereSQL)
	var total int
	err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Select rows
	dataQuery := fmt.Sprintf(`
		SELECT l.id, l.user_id, l.api_key_id, l.path, l.method, l.model, l.is_stream, 
		       l.prompt_tokens, l.completion_tokens, l.total_tokens, l.status_code, 
		       l.duration_ms, l.client_ip, COALESCE(l.error_message, ''), l.created_at,
		       COALESCE(u.name, 'Unknown') as user_name,
		       COALESCE(k.name, 'Deleted Key') as key_name
		FROM request_logs l
		LEFT JOIN users u ON l.user_id = u.id
		LEFT JOIN api_keys k ON l.api_key_id = k.id
		WHERE %s
		ORDER BY l.created_at DESC
		LIMIT ? OFFSET ?`, whereSQL)

	queryArgs := append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx, dataQuery, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []entity.RequestLog
	for rows.Next() {
		var l entity.RequestLog
		var createdAt string
		var isStream int
		if err := rows.Scan(
			&l.ID, &l.UserID, &l.APIKeyID, &l.Path, &l.Method, &l.Model, &isStream,
			&l.PromptTokens, &l.CompletionTokens, &l.TotalTokens, &l.StatusCode,
			&l.DurationMs, &l.ClientIP, &l.ErrorMessage, &createdAt,
			&l.UserName, &l.KeyName,
		); err != nil {
			return nil, 0, err
		}
		l.IsStream = (isStream == 1)
		l.CreatedAt = parseTimeFlexible(createdAt)
		logs = append(logs, l)
	}
	return logs, total, nil
}

// GetRequestLogsCursor returns a cursor-paginated page of request logs.
func (r *SQLiteRepo) GetRequestLogsCursor(ctx context.Context, limit int, cursor, direction, userID, modelFilter string, statusFilter int) ([]entity.RequestLog, *entity.CursorPageInfo, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	whereClauses := []string{"1=1"}
	args := []interface{}{}

	if userID != "" {
		whereClauses = append(whereClauses, "l.user_id = ?")
		args = append(args, userID)
	}
	if modelFilter != "" {
		whereClauses = append(whereClauses, "l.model LIKE ?")
		args = append(args, "%"+modelFilter+"%")
	}
	if statusFilter > 0 {
		whereClauses = append(whereClauses, "l.status_code = ?")
		args = append(args, statusFilter)
	}

	whereSQL := strings.Join(whereClauses, " AND ")
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM request_logs l WHERE %s", whereSQL)
	var total int
	_ = r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)

	cursorTime, cursorID, hasCursor := DecodeCursor(cursor)
	isPrev := (direction == "prev" && hasCursor)

	cursorWhere := ""
	cursorArgs := []interface{}{}
	orderDir := "DESC"

	if hasCursor {
		if isPrev {
			cursorWhere = " AND (l.created_at > ? OR (l.created_at = ? AND l.id > ?))"
			cursorArgs = append(cursorArgs, cursorTime, cursorTime, cursorID)
			orderDir = "ASC"
		} else {
			cursorWhere = " AND (l.created_at < ? OR (l.created_at = ? AND l.id < ?))"
			cursorArgs = append(cursorArgs, cursorTime, cursorTime, cursorID)
			orderDir = "DESC"
		}
	}

	dataQuery := fmt.Sprintf(`
		SELECT l.id, l.user_id, l.api_key_id, l.path, l.method, l.model, l.is_stream, 
		       l.prompt_tokens, l.completion_tokens, l.total_tokens, l.status_code, 
		       l.duration_ms, l.client_ip, COALESCE(l.error_message, ''), l.created_at,
		       COALESCE(u.name, 'Unknown') as user_name,
		       COALESCE(k.name, 'Deleted Key') as key_name
		FROM request_logs l
		LEFT JOIN users u ON l.user_id = u.id
		LEFT JOIN api_keys k ON l.api_key_id = k.id
		WHERE %s%s
		ORDER BY l.created_at %s, l.id %s
		LIMIT ?`, whereSQL, cursorWhere, orderDir, orderDir)

	allArgs := append(args, cursorArgs...)
	allArgs = append(allArgs, limit+1)

	rows, err := r.db.QueryContext(ctx, dataQuery, allArgs...)
	if err != nil {
		return nil, &entity.CursorPageInfo{Limit: limit, TotalCount: total}, err
	}
	defer rows.Close()

	var logs []entity.RequestLog
	for rows.Next() {
		var l entity.RequestLog
		var createdAt string
		var isStream int
		if err := rows.Scan(
			&l.ID, &l.UserID, &l.APIKeyID, &l.Path, &l.Method, &l.Model, &isStream,
			&l.PromptTokens, &l.CompletionTokens, &l.TotalTokens, &l.StatusCode,
			&l.DurationMs, &l.ClientIP, &l.ErrorMessage, &createdAt,
			&l.UserName, &l.KeyName,
		); err != nil {
			return nil, &entity.CursorPageInfo{Limit: limit, TotalCount: total}, err
		}
		l.IsStream = (isStream == 1)
		l.CreatedAt = parseTimeFlexible(createdAt)
		logs = append(logs, l)
	}

	pageInfo := &entity.CursorPageInfo{
		Limit:      limit,
		TotalCount: total,
	}

	if isPrev {
		if len(logs) > limit {
			pageInfo.HasPrev = true
			logs = logs[:limit]
		}
		pageInfo.HasNext = true

		// Reverse logs to restore DESC order
		for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
			logs[i], logs[j] = logs[j], logs[i]
		}
	} else {
		if hasCursor {
			pageInfo.HasPrev = true
		}
		if len(logs) > limit {
			pageInfo.HasNext = true
			logs = logs[:limit]
		}
	}

	if len(logs) > 0 {
		first := logs[0]
		last := logs[len(logs)-1]
		pageInfo.PrevCursor = EncodeCursor(first.CreatedAt, first.ID)
		pageInfo.NextCursor = EncodeCursor(last.CreatedAt, last.ID)
	}

	return logs, pageInfo, nil
}

func getTimeframeConfig(tf string) (bucketExpr, whereClause, candleSize, normTf string) {
	normTf = strings.ToLower(strings.TrimSpace(tf))
	switch normTf {
	case "1h":
		// Last 1 hour, 5-minute candles in WIB (+7h)
		bucket := "strftime('%H:', datetime(created_at, '+7 hours')) || printf('%02d', (CAST(strftime('%M', datetime(created_at, '+7 hours')) AS INTEGER) / 5) * 5)"
		return bucket, "created_at >= datetime('now', '-1 hour')", "5m", "1H"
	case "4h":
		// Last 4 hours, 15-minute candles in WIB (+7h)
		bucket := "strftime('%H:', datetime(created_at, '+7 hours')) || printf('%02d', (CAST(strftime('%M', datetime(created_at, '+7 hours')) AS INTEGER) / 15) * 15)"
		return bucket, "created_at >= datetime('now', '-4 hours')", "15m", "4H"
	case "1d", "24h":
		// Last 24 hours, 1-hour candles in WIB (+7h)
		bucket := "strftime('%H:00', datetime(created_at, '+7 hours'))"
		return bucket, "created_at >= datetime('now', '-24 hours')", "1h", "1D"
	case "1w", "7d":
		// Last 7 days, daily candles in WIB (+7h)
		bucket := "strftime('%Y-%m-%d', datetime(created_at, '+7 hours'))"
		return bucket, "created_at >= datetime('now', '-7 days')", "1d", "1W"
	case "1m", "30d":
		// Last 30 days, daily candles in WIB (+7h)
		bucket := "strftime('%Y-%m-%d', datetime(created_at, '+7 hours'))"
		return bucket, "created_at >= datetime('now', '-30 days')", "1d", "1M"
	case "all":
		// All time, daily candles
		bucket := "strftime('%Y-%m-%d', datetime(created_at, '+7 hours'))"
		return bucket, "1=1", "1d", "ALL"
	default:
		// Default to 1D (24h) for trading feel
		bucket := "strftime('%H:00', datetime(created_at, '+7 hours'))"
		return bucket, "created_at >= datetime('now', '-24 hours')", "1h", "1D"
	}
}

// Dashboard statistics

// GetDashboardStats aggregates global dashboard metrics for the given timeframe.
func (r *SQLiteRepo) GetDashboardStats(ctx context.Context, timeframe string) (*entity.DashboardStats, error) {
	stats := &entity.DashboardStats{}

	// Totals
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(total_tokens), 0) FROM request_logs").Scan(&stats.TotalRequests, &stats.TotalTokens)
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&stats.TotalUsers)
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE is_active = 1").Scan(&stats.ActiveUsers)
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM api_keys WHERE is_active = 1").Scan(&stats.ActiveKeys)

	// FinOps: Estimated Cost Saved ($5.00 / 1M tokens equivalent, Rp 80.000 / 1M tokens, 40% RTK savings)
	stats.CostSavedUSD = float64(stats.TotalTokens) * 0.000005
	stats.CostSavedIDR = int64(float64(stats.TotalTokens) * 0.08)
	stats.RTKTokensSaved = int64(float64(stats.TotalTokens) * 0.40)

	// Trading Timeframe Granular Aggregation
	bucketExpr, whereClause, candleSize, normTf := getTimeframeConfig(timeframe)
	stats.Timeframe = normTf
	stats.CandleSize = candleSize

	dailyQuery := fmt.Sprintf(`
		SELECT %s as log_date, 
		       COALESCE(SUM(total_tokens), 0) as tokens, 
		       COUNT(*) as reqs
		FROM request_logs
		WHERE %s
		GROUP BY log_date
		ORDER BY log_date ASC`, bucketExpr, whereClause)
	dailyRows, err := r.db.QueryContext(ctx, dailyQuery)
	if err == nil {
		defer dailyRows.Close()
		for dailyRows.Next() {
			var du entity.DailyUsage
			if err := dailyRows.Scan(&du.Date, &du.TotalTokens, &du.Requests); err == nil {
				stats.DailyUsage = append(stats.DailyUsage, du)
				stats.TimeframeVol += du.TotalTokens
				stats.TimeframeReqs += du.Requests
				if du.TotalTokens > stats.PeakTokens {
					stats.PeakTokens = du.TotalTokens
				}
			}
		}
	}

	// Top Users ordered by tokens_used DESC (per user request)
	topUsersQuery := `
		SELECT u.id, u.name, COALESCE(u.tokens_used, 0) as total_tok, COUNT(l.id) as req_count
		FROM users u
		LEFT JOIN request_logs l ON u.id = l.user_id
		GROUP BY u.id, u.name, u.tokens_used
		ORDER BY total_tok DESC
		LIMIT 10`
	tuRows, err := r.db.QueryContext(ctx, topUsersQuery)
	if err == nil {
		defer tuRows.Close()
		for tuRows.Next() {
			var tu entity.TopUserStat
			if err := tuRows.Scan(&tu.UserID, &tu.UserName, &tu.TokensUsed, &tu.Requests); err == nil {
				stats.TopUsers = append(stats.TopUsers, tu)
			}
		}
	}

	// Top Models
	topModelsQuery := `
		SELECT model, COUNT(*) as req_count, COALESCE(SUM(total_tokens), 0) as total_tok
		FROM request_logs
		WHERE model != ''
		GROUP BY model
		ORDER BY req_count DESC
		LIMIT 5`
	tmRows, err := r.db.QueryContext(ctx, topModelsQuery)
	if err == nil {
		defer tmRows.Close()
		for tmRows.Next() {
			var tm entity.TopModelStat
			if err := tmRows.Scan(&tm.Model, &tm.Requests, &tm.TotalTokens); err == nil {
				stats.TopModels = append(stats.TopModels, tm)
			}
		}
	}

	// Recent 10 logs
	recentLogs, _, _ := r.GetRequestLogs(ctx, 10, 0, "", "", 0)
	stats.RecentLogs = recentLogs

	return stats, nil
}

// GetUserDashboardStats aggregates per-user dashboard metrics for the given timeframe.
func (r *SQLiteRepo) GetUserDashboardStats(ctx context.Context, userID string, timeframe string) (*entity.DashboardStats, error) {
	stats := &entity.DashboardStats{}

	// User-specific Totals
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(total_tokens), 0) FROM request_logs WHERE user_id = ?", userID).Scan(&stats.TotalRequests, &stats.TotalTokens)
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND is_active = 1", userID).Scan(&stats.ActiveKeys)

	// FinOps: User Cost Saved
	stats.CostSavedUSD = float64(stats.TotalTokens) * 0.000005
	stats.CostSavedIDR = int64(float64(stats.TotalTokens) * 0.08)
	stats.RTKTokensSaved = int64(float64(stats.TotalTokens) * 0.40)

	// Trading Timeframe Granular Aggregation
	bucketExpr, whereClause, candleSize, normTf := getTimeframeConfig(timeframe)
	stats.Timeframe = normTf
	stats.CandleSize = candleSize

	userWhere := whereClause + " AND user_id = ?"
	if whereClause == "1=1" {
		userWhere = "user_id = ?"
	}

	dailyQuery := fmt.Sprintf(`
		SELECT %s as log_date, 
		       COALESCE(SUM(total_tokens), 0) as tokens, 
		       COUNT(*) as reqs
		FROM request_logs
		WHERE %s
		GROUP BY log_date
		ORDER BY log_date ASC`, bucketExpr, userWhere)
	dailyRows, err := r.db.QueryContext(ctx, dailyQuery, userID)
	if err == nil {
		defer dailyRows.Close()
		for dailyRows.Next() {
			var du entity.DailyUsage
			if err := dailyRows.Scan(&du.Date, &du.TotalTokens, &du.Requests); err == nil {
				stats.DailyUsage = append(stats.DailyUsage, du)
				stats.TimeframeVol += du.TotalTokens
				stats.TimeframeReqs += du.Requests
				if du.TotalTokens > stats.PeakTokens {
					stats.PeakTokens = du.TotalTokens
				}
			}
		}
	}

	// User Top Models
	topModelsQuery := `
		SELECT model, COUNT(*) as req_count, COALESCE(SUM(total_tokens), 0) as total_tok
		FROM request_logs
		WHERE user_id = ? AND model != ''
		GROUP BY model
		ORDER BY req_count DESC
		LIMIT 5`
	tmRows, err := r.db.QueryContext(ctx, topModelsQuery, userID)
	if err == nil {
		defer tmRows.Close()
		for tmRows.Next() {
			var tm entity.TopModelStat
			if err := tmRows.Scan(&tm.Model, &tm.Requests, &tm.TotalTokens); err == nil {
				stats.TopModels = append(stats.TopModels, tm)
			}
		}
	}

	// User Recent 10 logs
	recentLogs, _, _ := r.GetRequestLogs(ctx, 10, 0, userID, "", 0)
	stats.RecentLogs = recentLogs

	return stats, nil
}

// Billing & Midtrans Transactions

// GetActivePackages returns all active token packages ordered by price.
func (r *SQLiteRepo) GetActivePackages(ctx context.Context) ([]entity.TokenPackage, error) {
	query := `SELECT id, name, tokens, price_idr, description, is_popular, is_active, created_at 
	          FROM token_packages WHERE is_active = 1 ORDER BY price_idr ASC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pkgs []entity.TokenPackage
	for rows.Next() {
		var p entity.TokenPackage
		var isPop, isActive int
		var createdAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Tokens, &p.PriceIDR, &p.Description, &isPop, &isActive, &createdAt); err != nil {
			return nil, err
		}
		p.IsPopular = (isPop == 1)
		p.IsActive = (isActive == 1)
		p.CreatedAt = parseTimeFlexible(createdAt)
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// GetPackageByID returns a single token package by its ID.
func (r *SQLiteRepo) GetPackageByID(ctx context.Context, id string) (*entity.TokenPackage, error) {
	query := `SELECT id, name, tokens, price_idr, description, is_popular, is_active, created_at 
	          FROM token_packages WHERE id = ?`
	var p entity.TokenPackage
	var isPop, isActive int
	var createdAt string
	err := r.db.QueryRowContext(ctx, query, id).Scan(&p.ID, &p.Name, &p.Tokens, &p.PriceIDR, &p.Description, &isPop, &isActive, &createdAt)
	if err != nil {
		return nil, err
	}
	p.IsPopular = (isPop == 1)
	p.IsActive = (isActive == 1)
	p.CreatedAt = parseTimeFlexible(createdAt)
	return &p, nil
}

// CreateTransaction inserts a new billing transaction.
func (r *SQLiteRepo) CreateTransaction(ctx context.Context, tx *entity.Transaction) error {
	query := `INSERT INTO transactions (id, user_id, package_id, tokens, amount_idr, status, payment_type, snap_token, snap_url, midtrans_tx_id, created_at)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`
	_, err := r.db.ExecContext(ctx, query, tx.ID, tx.UserID, tx.PackageID, tx.Tokens, tx.AmountIDR, tx.Status, tx.PaymentType, tx.SnapToken, tx.SnapURL, tx.MidtransTxID)
	return err
}

// GetTransactionByID returns a transaction by its ID.
func (r *SQLiteRepo) GetTransactionByID(ctx context.Context, id string) (*entity.Transaction, error) {
	query := `SELECT t.id, t.user_id, t.package_id, t.tokens, t.amount_idr, t.status, 
	                 COALESCE(t.payment_type, ''), COALESCE(t.snap_token, ''), COALESCE(t.snap_url, ''), 
	                 COALESCE(t.midtrans_tx_id, ''), t.created_at, t.paid_at,
	                 COALESCE(u.name, 'Unknown')
	          FROM transactions t
	          LEFT JOIN users u ON t.user_id = u.id
	          WHERE t.id = ?`
	var t entity.Transaction
	var createdAt string
	var paidAt sql.NullString
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&t.ID, &t.UserID, &t.PackageID, &t.Tokens, &t.AmountIDR, &t.Status,
		&t.PaymentType, &t.SnapToken, &t.SnapURL, &t.MidtransTxID,
		&createdAt, &paidAt, &t.UserName,
	)
	if err != nil {
		return nil, err
	}
	t.CreatedAt = parseTimeFlexible(createdAt)
	if paidAt.Valid {
		pt := parseTimeFlexible(paidAt.String)
		if !pt.IsZero() {
			t.PaidAt = &pt
		}
	}
	return &t, nil
}

// UpdateTransactionStatus updates the status and payment metadata of a transaction.
func (r *SQLiteRepo) UpdateTransactionStatus(ctx context.Context, id, status, paymentType, midtransTxID string) error {
	if status == "settlement" || status == "capture" || status == "paid" {
		query := `UPDATE transactions 
		          SET status = ?, payment_type = ?, midtrans_tx_id = ?, paid_at = datetime('now')
		          WHERE id = ?`
		_, err := r.db.ExecContext(ctx, query, status, paymentType, midtransTxID, id)
		return err
	}
	query := `UPDATE transactions 
	          SET status = ?, payment_type = ?, midtrans_tx_id = ?
	          WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, status, paymentType, midtransTxID, id)
	return err
}

// GetTransactionsByUserID returns paginated transactions for a specific user.
func (r *SQLiteRepo) GetTransactionsByUserID(ctx context.Context, userID string, limit, offset int) ([]entity.Transaction, int, error) {
	var total int
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM transactions WHERE user_id = ?", userID).Scan(&total)

	query := `SELECT t.id, t.user_id, t.package_id, t.tokens, t.amount_idr, t.status, 
	                 COALESCE(t.payment_type, ''), COALESCE(t.snap_token, ''), COALESCE(t.snap_url, ''), 
	                 COALESCE(t.midtrans_tx_id, ''), t.created_at, t.paid_at,
	                 COALESCE(u.name, 'Unknown')
	          FROM transactions t
	          LEFT JOIN users u ON t.user_id = u.id
	          WHERE t.user_id = ?
	          ORDER BY t.created_at DESC
	          LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var txs []entity.Transaction
	for rows.Next() {
		var t entity.Transaction
		var createdAt string
		var paidAt sql.NullString
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.PackageID, &t.Tokens, &t.AmountIDR, &t.Status,
			&t.PaymentType, &t.SnapToken, &t.SnapURL, &t.MidtransTxID,
			&createdAt, &paidAt, &t.UserName,
		); err != nil {
			return nil, 0, err
		}
		t.CreatedAt = parseTimeFlexible(createdAt)
		if paidAt.Valid {
			pt := parseTimeFlexible(paidAt.String)
			if !pt.IsZero() {
				t.PaidAt = &pt
			}
		}
		txs = append(txs, t)
	}
	return txs, total, nil
}

// GetAllTransactions returns paginated transactions across all users.
func (r *SQLiteRepo) GetAllTransactions(ctx context.Context, limit, offset int) ([]entity.Transaction, int, error) {
	var total int
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM transactions").Scan(&total)

	query := `SELECT t.id, t.user_id, t.package_id, t.tokens, t.amount_idr, t.status, 
	                 COALESCE(t.payment_type, ''), COALESCE(t.snap_token, ''), COALESCE(t.snap_url, ''), 
	                 COALESCE(t.midtrans_tx_id, ''), t.created_at, t.paid_at,
	                 COALESCE(u.name, 'Unknown')
	          FROM transactions t
	          LEFT JOIN users u ON t.user_id = u.id
	          ORDER BY t.created_at DESC
	          LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var txs []entity.Transaction
	for rows.Next() {
		var t entity.Transaction
		var createdAt string
		var paidAt sql.NullString
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.PackageID, &t.Tokens, &t.AmountIDR, &t.Status,
			&t.PaymentType, &t.SnapToken, &t.SnapURL, &t.MidtransTxID,
			&createdAt, &paidAt, &t.UserName,
		); err != nil {
			return nil, 0, err
		}
		t.CreatedAt = parseTimeFlexible(createdAt)
		if paidAt.Valid {
			pt := parseTimeFlexible(paidAt.String)
			if !pt.IsZero() {
				t.PaidAt = &pt
			}
		}
		txs = append(txs, t)
	}
	return txs, total, nil
}

// CreditUserTokens increases a user's token quota balance.
func (r *SQLiteRepo) CreditUserTokens(ctx context.Context, userID string, tokens int64) error {
	if tokens <= 0 {
		return nil
	}
	// Increase token_quota by tokens
	query := `UPDATE users SET token_quota = token_quota + ?, updated_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, tokens, userID)
	return err
}

// SaveSetting upserts a key-value setting.
func (r *SQLiteRepo) SaveSetting(ctx context.Context, key, value string) error {
	query := `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now'))
	          ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`
	_, err := r.db.ExecContext(ctx, query, key, value)
	return err
}

// GetSetting retrieves a setting value by key.
func (r *SQLiteRepo) GetSetting(ctx context.Context, key string) (string, error) {
	var val string
	err := r.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	return val, err
}

// Server-Side Sessions Implementation

// CreateSession stores a new server-side session token.
func (r *SQLiteRepo) CreateSession(ctx context.Context, token, userID string, expiresAt time.Time) error {
	query := `INSERT INTO sessions (token, user_id, created_at, expires_at) VALUES (?, ?, datetime('now'), ?)`
	_, err := r.db.ExecContext(ctx, query, token, userID, expiresAt.UTC().Format("2006-01-02 15:04:05"))
	return err
}

// GetSessionUser returns the active user associated with a session token.
func (r *SQLiteRepo) GetSessionUser(ctx context.Context, token string) (*entity.User, error) {
	query := `
		SELECT u.id, u.name, COALESCE(u.username, ''), COALESCE(u.password_hash, ''), 
		       u.role, u.token_quota, u.tokens_used, u.allowed_models, u.is_active, 
		       u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.token = ? AND s.expires_at > datetime('now') AND u.is_active = 1`

	var u entity.User
	var createdAt, updatedAt string
	var isActive int
	err := r.db.QueryRowContext(ctx, query, token).Scan(
		&u.ID, &u.Name, &u.Username, &u.PasswordHash,
		&u.Role, &u.TokenQuota, &u.TokensUsed, &u.AllowedModels, &isActive,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.IsActive = (isActive == 1)
	u.CreatedAt = parseTimeFlexible(createdAt)
	u.UpdatedAt = parseTimeFlexible(updatedAt)
	return &u, nil
}

// DeleteSession removes a session by its token.
func (r *SQLiteRepo) DeleteSession(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM sessions WHERE token = ?", token)
	return err
}

// CleanExpiredSessions deletes all sessions whose expiry time has passed.
func (r *SQLiteRepo) CleanExpiredSessions(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= datetime('now')")
	return err
}

// Login Rate Limiting Implementation

// RecordLoginAttempt records a login attempt from the given IP.
func (r *SQLiteRepo) RecordLoginAttempt(ctx context.Context, ip string) error {
	query := `INSERT INTO login_attempts (ip, attempt_time) VALUES (?, datetime('now'))`
	_, err := r.db.ExecContext(ctx, query, ip)
	return err
}

// GetRecentLoginAttempts counts login attempts from the given IP within the time window.
func (r *SQLiteRepo) GetRecentLoginAttempts(ctx context.Context, ip string, windowMinutes int) (int, error) {
	modifier := fmt.Sprintf("-%d minutes", windowMinutes)
	query := `SELECT COUNT(*) FROM login_attempts WHERE ip = ? AND attempt_time >= datetime('now', ?)`
	var count int
	err := r.db.QueryRowContext(ctx, query, ip, modifier).Scan(&count)
	return count, err
}

// ClearLoginAttempts removes all recorded login attempts for an IP.
func (r *SQLiteRepo) ClearLoginAttempts(ctx context.Context, ip string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM login_attempts WHERE ip = ?", ip)
	return err
}

// CleanOldLoginAttempts removes login attempts older than 24 hours.
func (r *SQLiteRepo) CleanOldLoginAttempts(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM login_attempts WHERE attempt_time < datetime('now', '-24 hours')")
	return err
}
