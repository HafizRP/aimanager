package models

import (
	"time"
)

type User struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Role          string    `json:"role"`
	TokenQuota    int64     `json:"token_quota"` // 0 = unlimited
	TokensUsed    int64     `json:"tokens_used"`
	AllowedModels string    `json:"allowed_models"` // JSON array string e.g. ["*"] or ["ag/gemini-3.8-flash-low"]
	IsActive      bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Virtual fields for UI
	KeyCount int `json:"key_count,omitempty"`
}

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

type APIKey struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	IsActive   bool       `json:"is_active"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`

	// Virtual field for UI
	UserName string `json:"user_name,omitempty"`
}

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

type DailyUsage struct {
	Date        string `json:"date"`
	TotalTokens int64  `json:"total_tokens"`
	Requests    int64  `json:"requests"`
}

type TopUserStat struct {
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	TokensUsed int64  `json:"tokens_used"`
	Requests   int64  `json:"requests"`
}

type TopModelStat struct {
	Model       string `json:"model"`
	Requests    int64  `json:"requests"`
	TotalTokens int64  `json:"total_tokens"`
}

type DashboardStats struct {
	TotalRequests int64          `json:"total_requests"`
	TotalTokens   int64          `json:"total_tokens"`
	ActiveUsers   int64          `json:"active_users"`
	TotalUsers    int64          `json:"total_users"`
	ActiveKeys    int64          `json:"active_keys"`
	DailyUsage    []DailyUsage   `json:"daily_usage"`
	TopUsers      []TopUserStat  `json:"top_users"`
	TopModels     []TopModelStat `json:"top_models"`
	RecentLogs    []RequestLog   `json:"recent_logs"`
}
