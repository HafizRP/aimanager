// Package entity defines the core domain entities of the gateway.
//
// These are the structures the business logic (internal/usecase) operates on.
// They hold no infrastructure concerns (no SQL, no HTTP) and can be
// serialized to JSON for the dashboard and API responses.
package entity

import (
	"encoding/json"
	"strings"
	"time"
)

// User is an application account that can own API keys and consume tokens.
//
// TokenQuota of 0 means unlimited. AllowedModels is a JSON array string
// (or comma separated) of model IDs, "*" grants access to all models.
type User struct {
	ID            string     `json:"id"`
	Username      string     `json:"username"`
	Name          string     `json:"name"`
	PasswordHash  string     `json:"-"`
	Role          string     `json:"role"`        // "admin" or "user"
	TokenQuota    int64      `json:"token_quota"` // 0 = unlimited
	TokensUsed    int64      `json:"tokens_used"`
	AllowedModels string     `json:"allowed_models"` // JSON array string e.g. ["*"] or ["ag/gemini-3.8-flash-low"]
	RateLimitRPM  int        `json:"rate_limit_rpm"`
	RateLimitTPM  int64      `json:"rate_limit_tpm"`
	IsActive      bool       `json:"is_active"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`

	// Virtual fields for UI
	KeyCount int `json:"key_count,omitempty"`
}

// IsAdmin reports whether the user has the admin role.
func (u *User) IsAdmin() bool {
	return u.Role == "admin" || u.Role == "superadmin"
}

// QuotaPercent returns the used quota ratio (0-100), clamped at 100.
func (u *User) QuotaPercent() float64 {
	if u.TokenQuota <= 0 {
		return 0
	}
	p := float64(u.TokensUsed) / float64(u.TokenQuota) * 100
	if p > 100 {
		return 100
	}
	return p
}

// GetAllowedModels returns the parsed allowed model list.
func (u *User) GetAllowedModels() []string {
	return ParseAllowedModels(u.AllowedModels)
}

// HasModelAccess reports whether a model is allowed for this user.
func (u *User) HasModelAccess(model string) bool {
	return IsModelAllowed(model, u.GetAllowedModels())
}

// APIKey is a credential used to call the gateway proxy endpoints.
// It belongs to a single user and may be scoped to a subset of models
// and expire after ExpiresAt.
type APIKey struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	Key            string     `json:"key"`
	Name           string     `json:"name"`
	AllowedModels  string     `json:"allowed_models"` // Optional model scope
	RateLimitRPM   int        `json:"rate_limit_rpm"`
	MaxTokensLimit int        `json:"max_tokens_limit"` // Lifetime token budget; 0 = unlimited
	IsActive       bool       `json:"is_active"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`

	// Virtual fields for UI
	UserName   string `json:"user_name,omitempty"`
	TokenUsage int64  `json:"token_usage,omitempty"`
}

// IsExpired reports whether the key has passed its expiry.
func (k *APIKey) IsExpired() bool {
	if k == nil || k.ExpiresAt == nil {
		return false
	}
	return !k.ExpiresAt.IsZero() && time.Now().After(*k.ExpiresAt)
}

// GetAllowedModels returns the parsed allowed model list; nil means unrestricted.
func (k *APIKey) GetAllowedModels() []string {
	if strings.TrimSpace(k.AllowedModels) == "" {
		return nil
	}
	return ParseAllowedModels(k.AllowedModels)
}

// HasModelAccess reports whether a model is allowed for this key.
func (k *APIKey) HasModelAccess(model string) bool {
	allowed := k.GetAllowedModels()
	if len(allowed) == 0 {
		return true
	}
	return IsModelAllowed(model, allowed)
}

// RequestLog records a single proxied LLM request for usage tracking.
type RequestLog struct {
	ID               int64     `json:"id"`
	UserID           string    `json:"user_id"`
	APIKeyID         string    `json:"api_key_id"`
	Path             string    `json:"path"`
	Method           string    `json:"method"`
	Model            string    `json:"model"`
	IsStream         bool      `json:"is_stream"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	StatusCode       int       `json:"status_code"`
	DurationMs       int64     `json:"duration_ms"`
	ClientIP         string    `json:"client_ip"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	CreatedAt        time.Time `json:"created_at"`

	// Virtual fields for UI
	UserName string `json:"user_name,omitempty"`
	KeyName  string `json:"key_name,omitempty"`
}

// DailyUsage aggregates token and request totals for one day.
type DailyUsage struct {
	Date        string `json:"date"`
	TotalTokens int64  `json:"total_tokens"`
	Requests    int64  `json:"requests"`
}

// TopUserStat ranks a user by token usage over a timeframe.
type TopUserStat struct {
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	TokensUsed int64  `json:"tokens_used"`
	Requests   int64  `json:"requests"`
}

// TopModelStat ranks a model by request and token counts.
type TopModelStat struct {
	Model       string `json:"model"`
	Requests    int64  `json:"requests"`
	TotalTokens int64  `json:"total_tokens"`
}

// DashboardStats is the aggregate statistics payload for the dashboard.
type DashboardStats struct {
	TotalRequests  int64          `json:"total_requests"`
	TotalTokens    int64          `json:"total_tokens"`
	CostSavedUSD   float64        `json:"cost_saved_usd"`
	CostSavedIDR   int64          `json:"cost_saved_idr"`
	RTKTokensSaved int64          `json:"rtk_tokens_saved"`
	ActiveUsers    int64          `json:"active_users"`
	TotalUsers     int64          `json:"total_users"`
	ActiveKeys     int64          `json:"active_keys"`
	Timeframe      string         `json:"timeframe"`
	TimeframeVol   int64          `json:"timeframe_vol"`
	TimeframeReqs  int64          `json:"timeframe_reqs"`
	PeakTokens     int64          `json:"peak_tokens"`
	CandleSize     string         `json:"candle_size"`
	DailyUsage     []DailyUsage   `json:"daily_usage"`
	TopUsers       []TopUserStat  `json:"top_users"`
	TopModels      []TopModelStat `json:"top_models"`
	RecentLogs     []RequestLog   `json:"recent_logs"`
}

// CursorPageInfo carries pagination metadata for cursor-based log queries.
type CursorPageInfo struct {
	HasNext    bool   `json:"has_next"`
	HasPrev    bool   `json:"has_prev"`
	NextCursor string `json:"next_cursor"`
	PrevCursor string `json:"prev_cursor"`
	Limit      int    `json:"limit"`
	TotalCount int    `json:"total_count"`
}

// TokenPackage is a purchasable token bundle sold via Midtrans.
type TokenPackage struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Tokens      int64     `json:"tokens"`
	PriceIDR    int64     `json:"price_idr"`
	Description string    `json:"description"`
	IsPopular   bool      `json:"is_popular"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// Transaction is a Midtrans payment record (order) for a token package.
type Transaction struct {
	ID           string     `json:"id"` // order_id
	UserID       string     `json:"user_id"`
	PackageID    string     `json:"package_id"`
	Tokens       int64      `json:"tokens"`
	AmountIDR    int64      `json:"amount_idr"`
	Status       string     `json:"status"` // "pending", "settlement", "expire", "cancel"
	PaymentType  string     `json:"payment_type"`
	SnapToken    string     `json:"snap_token"`
	SnapURL      string     `json:"snap_url"`
	MidtransTxID string     `json:"midtrans_tx_id"`
	CreatedAt    time.Time  `json:"created_at"`
	PaidAt       *time.Time `json:"paid_at,omitempty"`

	// Virtual UI fields
	UserName string `json:"user_name,omitempty"`
}

// ParseAllowedModels parses a JSON array or comma-separated string of allowed entity.
func ParseAllowedModels(raw string) []string {
	var list []string
	if strings.TrimSpace(raw) == "" {
		return []string{"*"}
	}
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		return list
	}
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			list = append(list, p)
		}
	}
	if len(list) == 0 {
		return []string{"*"}
	}
	return list
}

// HasWildcard returns true if the list of allowed models contains "*".
func HasWildcard(list []string) bool {
	for _, m := range list {
		if strings.TrimSpace(m) == "*" {
			return true
		}
	}
	return false
}

// IsModelAllowed checks whether a requested model matches the allowed models list.
func IsModelAllowed(requested string, allowedList []string) bool {
	for _, a := range allowedList {
		if a == "*" || strings.EqualFold(a, requested) {
			return true
		}
	}
	return false
}
