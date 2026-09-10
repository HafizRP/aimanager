package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"9router-gateway/internal/models"
)

type Repository interface {
	// Users
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	GetAllUsers(ctx context.Context) ([]models.User, error)
	CreateUser(ctx context.Context, u *models.User) error
	UpdateUser(ctx context.Context, u *models.User) error
	ToggleUserStatus(ctx context.Context, id string, isActive bool) error
	DeleteUser(ctx context.Context, id string) error
	DeductTokens(ctx context.Context, userID string, tokens int) error

	// API Keys
	GetAPIKeyByKey(ctx context.Context, key string) (*models.APIKey, error)
	GetAPIKeysByUserID(ctx context.Context, userID string) ([]models.APIKey, error)
	GetAllAPIKeys(ctx context.Context) ([]models.APIKey, error)
	CreateAPIKey(ctx context.Context, k *models.APIKey) error
	ToggleAPIKeyStatus(ctx context.Context, id string, isActive bool) error
	DeleteAPIKey(ctx context.Context, id string) error
	UpdateKeyLastUsed(ctx context.Context, id string) error

	// Logs
	CreateRequestLog(ctx context.Context, log *models.RequestLog) error
	GetRequestLogs(ctx context.Context, limit, offset int, userID, modelFilter string, statusFilter int) ([]models.RequestLog, int, error)

	// Stats
	GetDashboardStats(ctx context.Context) (*models.DashboardStats, error)
}

type SQLiteRepo struct {
	db *sql.DB
}

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

func (r *SQLiteRepo) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	query := `SELECT id, name, role, token_quota, tokens_used, allowed_models, is_active, created_at, updated_at 
	          FROM users WHERE id = ?`
	var u models.User
	var createdAt, updatedAt string
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&u.ID, &u.Name, &u.Role, &u.TokenQuota, &u.TokensUsed, &u.AllowedModels, &u.IsActive, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseTimeFlexible(createdAt)
	u.UpdatedAt = parseTimeFlexible(updatedAt)
	return &u, nil
}

func (r *SQLiteRepo) GetAllUsers(ctx context.Context) ([]models.User, error) {
	query := `
		SELECT u.id, u.name, u.role, u.token_quota, u.tokens_used, u.allowed_models, u.is_active, u.created_at, u.updated_at,
		       (SELECT COUNT(*) FROM api_keys WHERE user_id = u.id) as key_count
		FROM users u
		ORDER BY u.created_at DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		var createdAt, updatedAt string
		if err := rows.Scan(&u.ID, &u.Name, &u.Role, &u.TokenQuota, &u.TokensUsed, &u.AllowedModels, &u.IsActive, &createdAt, &updatedAt, &u.KeyCount); err != nil {
			return nil, err
		}
		u.CreatedAt = parseTimeFlexible(createdAt)
		u.UpdatedAt = parseTimeFlexible(updatedAt)
		users = append(users, u)
	}
	return users, nil
}

func (r *SQLiteRepo) CreateUser(ctx context.Context, u *models.User) error {
	query := `INSERT INTO users (id, name, role, token_quota, tokens_used, allowed_models, is_active, created_at, updated_at) 
	          VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`
	_, err := r.db.ExecContext(ctx, query, u.ID, u.Name, u.Role, u.TokenQuota, u.TokensUsed, u.AllowedModels, u.IsActive)
	return err
}

func (r *SQLiteRepo) UpdateUser(ctx context.Context, u *models.User) error {
	query := `UPDATE users 
	          SET name = ?, token_quota = ?, allowed_models = ?, is_active = ?, updated_at = datetime('now')
	          WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, u.Name, u.TokenQuota, u.AllowedModels, u.IsActive, u.ID)
	return err
}

func (r *SQLiteRepo) ToggleUserStatus(ctx context.Context, id string, isActive bool) error {
	query := `UPDATE users SET is_active = ?, updated_at = datetime('now') WHERE id = ?`
	val := 0
	if isActive {
		val = 1
	}
	_, err := r.db.ExecContext(ctx, query, val, id)
	return err
}

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

func (r *SQLiteRepo) DeductTokens(ctx context.Context, userID string, tokens int) error {
	if tokens <= 0 {
		return nil
	}
	query := `UPDATE users SET tokens_used = tokens_used + ?, updated_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, tokens, userID)
	return err
}

// API Key methods

func (r *SQLiteRepo) GetAPIKeyByKey(ctx context.Context, key string) (*models.APIKey, error) {
	query := `SELECT k.id, k.user_id, k.key, k.name, k.is_active, k.created_at, k.last_used_at, u.name 
	          FROM api_keys k 
	          JOIN users u ON k.user_id = u.id 
	          WHERE k.key = ?`
	var k models.APIKey
	var createdAt string
	var lastUsed sql.NullString
	err := r.db.QueryRowContext(ctx, query, key).Scan(
		&k.ID, &k.UserID, &k.Key, &k.Name, &k.IsActive, &createdAt, &lastUsed, &k.UserName,
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
	return &k, nil
}

func (r *SQLiteRepo) GetAPIKeysByUserID(ctx context.Context, userID string) ([]models.APIKey, error) {
	query := `SELECT id, user_id, key, name, is_active, created_at, last_used_at 
	          FROM api_keys WHERE user_id = ? ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		var createdAt string
		var lastUsed sql.NullString
		if err := rows.Scan(&k.ID, &k.UserID, &k.Key, &k.Name, &k.IsActive, &createdAt, &lastUsed); err != nil {
			return nil, err
		}
		k.CreatedAt = parseTimeFlexible(createdAt)
		if lastUsed.Valid {
			t := parseTimeFlexible(lastUsed.String)
			if !t.IsZero() {
				k.LastUsedAt = &t
			}
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (r *SQLiteRepo) GetAllAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	query := `SELECT k.id, k.user_id, k.key, k.name, k.is_active, k.created_at, k.last_used_at, u.name 
	          FROM api_keys k 
	          JOIN users u ON k.user_id = u.id 
	          ORDER BY k.created_at DESC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		var createdAt string
		var lastUsed sql.NullString
		if err := rows.Scan(&k.ID, &k.UserID, &k.Key, &k.Name, &k.IsActive, &createdAt, &lastUsed, &k.UserName); err != nil {
			return nil, err
		}
		k.CreatedAt = parseTimeFlexible(createdAt)
		if lastUsed.Valid {
			t := parseTimeFlexible(lastUsed.String)
			if !t.IsZero() {
				k.LastUsedAt = &t
			}
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (r *SQLiteRepo) CreateAPIKey(ctx context.Context, k *models.APIKey) error {
	query := `INSERT INTO api_keys (id, user_id, key, name, is_active, created_at) 
	          VALUES (?, ?, ?, ?, ?, datetime('now'))`
	_, err := r.db.ExecContext(ctx, query, k.ID, k.UserID, k.Key, k.Name, k.IsActive)
	return err
}

func (r *SQLiteRepo) ToggleAPIKeyStatus(ctx context.Context, id string, isActive bool) error {
	val := 0
	if isActive {
		val = 1
	}
	query := `UPDATE api_keys SET is_active = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, val, id)
	return err
}

func (r *SQLiteRepo) DeleteAPIKey(ctx context.Context, id string) error {
	query := `DELETE FROM api_keys WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *SQLiteRepo) UpdateKeyLastUsed(ctx context.Context, id string) error {
	query := `UPDATE api_keys SET last_used_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// Request logs

func (r *SQLiteRepo) CreateRequestLog(ctx context.Context, log *models.RequestLog) error {
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

func (r *SQLiteRepo) GetRequestLogs(ctx context.Context, limit, offset int, userID, modelFilter string, statusFilter int) ([]models.RequestLog, int, error) {
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

	var logs []models.RequestLog
	for rows.Next() {
		var l models.RequestLog
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

// Dashboard statistics

func (r *SQLiteRepo) GetDashboardStats(ctx context.Context) (*models.DashboardStats, error) {
	stats := &models.DashboardStats{}

	// Totals
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(total_tokens), 0) FROM request_logs").Scan(&stats.TotalRequests, &stats.TotalTokens)
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&stats.TotalUsers)
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE is_active = 1").Scan(&stats.ActiveUsers)
	_ = r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM api_keys WHERE is_active = 1").Scan(&stats.ActiveKeys)

	// Daily Usage (last 14 days)
	dailyQuery := `
		SELECT strftime('%Y-%m-%d', created_at) as log_date, 
		       COALESCE(SUM(total_tokens), 0) as tokens, 
		       COUNT(*) as reqs
		FROM request_logs
		WHERE created_at >= datetime('now', '-14 days')
		GROUP BY log_date
		ORDER BY log_date ASC`
	dailyRows, err := r.db.QueryContext(ctx, dailyQuery)
	if err == nil {
		defer dailyRows.Close()
		for dailyRows.Next() {
			var du models.DailyUsage
			if err := dailyRows.Scan(&du.Date, &du.TotalTokens, &du.Requests); err == nil {
				stats.DailyUsage = append(stats.DailyUsage, du)
			}
		}
	}

	// Top Users
	topUsersQuery := `
		SELECT u.id, u.name, COALESCE(SUM(l.total_tokens), 0) as total_tok, COUNT(l.id) as req_count
		FROM users u
		LEFT JOIN request_logs l ON u.id = l.user_id
		GROUP BY u.id, u.name
		ORDER BY total_tok DESC
		LIMIT 5`
	tuRows, err := r.db.QueryContext(ctx, topUsersQuery)
	if err == nil {
		defer tuRows.Close()
		for tuRows.Next() {
			var tu models.TopUserStat
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
			var tm models.TopModelStat
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
