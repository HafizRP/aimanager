package usecase

import (
	"context"
	"time"

	"9router-gateway/internal/entity"
)

// ---- Core gateway data source (users, keys, logs, sessions) ----
type (
	UserStore interface {
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
	}

	APIKeyStore interface {
		GetAPIKeyByKey(ctx context.Context, key string) (*entity.APIKey, error)
		GetAPIKeysByUserID(ctx context.Context, userID string) ([]entity.APIKey, error)
		GetAllAPIKeys(ctx context.Context) ([]entity.APIKey, error)
		CreateAPIKey(ctx context.Context, k *entity.APIKey) error
		ToggleAPIKeyStatus(ctx context.Context, id string, isActive bool) error
		DeleteAPIKey(ctx context.Context, id string) error
		UpdateKeyLastUsed(ctx context.Context, id string) error
	}

	RequestLogStore interface {
		CreateRequestLog(ctx context.Context, log *entity.RequestLog) error
		GetRequestLogs(ctx context.Context, limit, offset int, userID, modelFilter string, statusFilter int) ([]entity.RequestLog, int, error)
		GetRequestLogsCursor(ctx context.Context, limit int, cursor, direction, userID, modelFilter string, statusFilter int) ([]entity.RequestLog, *entity.CursorPageInfo, error)
	}

	StatsStore interface {
		GetDashboardStats(ctx context.Context, timeframe string) (*entity.DashboardStats, error)
		GetUserDashboardStats(ctx context.Context, userID string, timeframe string) (*entity.DashboardStats, error)
	}

	TransactionStore interface {
		GetActivePackages(ctx context.Context) ([]entity.TokenPackage, error)
		GetPackageByID(ctx context.Context, id string) (*entity.TokenPackage, error)
		CreateTransaction(ctx context.Context, tx *entity.Transaction) error
		GetTransactionByID(ctx context.Context, id string) (*entity.Transaction, error)
		UpdateTransactionStatus(ctx context.Context, id, status, paymentType, midtransTxID string) error
		GetTransactionsByUserID(ctx context.Context, userID string, limit, offset int) ([]entity.Transaction, int, error)
		GetAllTransactions(ctx context.Context, limit, offset int) ([]entity.Transaction, int, error)
		CreditUserTokens(ctx context.Context, userID string, tokens int64) error
	}

	SettingsStore interface {
		SaveSetting(ctx context.Context, key, value string) error
		GetSetting(ctx context.Context, key string) (string, error)
	}

	SessionStore interface {
		CreateSession(ctx context.Context, token, userID string, expiresAt time.Time) error
		GetSessionUser(ctx context.Context, token string) (*entity.User, error)
		DeleteSession(ctx context.Context, token string) error
		CleanExpiredSessions(ctx context.Context) error
	}

	LoginAttemptStore interface {
		RecordLoginAttempt(ctx context.Context, ip string) error
		GetRecentLoginAttempts(ctx context.Context, ip string, windowMinutes int) (int, error)
		ClearLoginAttempts(ctx context.Context, ip string) error
		CleanOldLoginAttempts(ctx context.Context) error
	}

	// Store is the composition of every persistence capability used by use cases.
	Store interface {
		UserStore
		APIKeyStore
		RequestLogStore
		StatsStore
		TransactionStore
		SettingsStore
		SessionStore
		LoginAttemptStore
	}
)