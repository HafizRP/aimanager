// Package proxy implements the OpenAI/Anthropic-compatible reverse proxy gateway.
package proxy

import (
	"fmt"
	"net/http"
	"time"

	"9router-gateway/internal/entity"
	"9router-gateway/internal/repository"
)

// AuthMiddleware validates API keys and user active status.
type AuthMiddleware struct {
	repo repository.Repository
}

func NewAuthMiddleware(repo repository.Repository) *AuthMiddleware {
	return &AuthMiddleware{repo: repo}
}

func (m *AuthMiddleware) Name() string {
	return "auth"
}

func (m *AuthMiddleware) Handle(ctx *PipelineContext, next NextFunc) error {
	rawKey := ctx.RawAPIKey
	if rawKey == "" {
		ctx.WriteJSONError(http.StatusUnauthorized, "API key required. Provide 'Authorization: Bearer ***' or 'x-api-key: ***' header.", "invalid_request_error")
		return nil
	}

	key, err := m.repo.GetAPIKeyByKey(ctx.Context, rawKey)
	if err != nil || !key.IsActive {
		ctx.WriteJSONError(http.StatusUnauthorized, "Invalid, revoked, or inactive API key", "invalid_request_error")
		return nil
	}

	if key.IsExpired() {
		ctx.WriteJSONError(http.StatusUnauthorized, "API key has expired. Please contact administrator.", "invalid_request_error")
		return nil
	}

	user, err := m.repo.GetUserByID(ctx.Context, key.UserID)
	if err != nil || !user.IsActive {
		ctx.WriteJSONError(http.StatusForbidden, "User account is suspended or inactive", "permission_denied")
		return nil
	}

	ctx.APIKey = key
	ctx.User = user
	return next(ctx)
}

// RateLimitMiddleware enforces key and user RPM/TPM limits.
type RateLimitMiddleware struct {
	limiter *RateLimiter
}

func NewRateLimitMiddleware(limiter *RateLimiter) *RateLimitMiddleware {
	return &RateLimitMiddleware{limiter: limiter}
}

func (m *RateLimitMiddleware) Name() string {
	return "rate_limit"
}

func (m *RateLimitMiddleware) Handle(ctx *PipelineContext, next NextFunc) error {
	if ctx.APIKey != nil && ctx.APIKey.RateLimitRPM > 0 {
		allowed, msg := m.limiter.Allow("key:"+ctx.APIKey.ID, ctx.APIKey.RateLimitRPM, 0, 0)
		if !allowed {
			ctx.Writer.Header().Set("Retry-After", "60")
			ctx.WriteJSONError(http.StatusTooManyRequests, msg, "rate_limit_exceeded")
			return nil
		}
	}

	if ctx.User != nil && (ctx.User.RateLimitRPM > 0 || ctx.User.RateLimitTPM > 0) {
		allowed, msg := m.limiter.Allow("user:"+ctx.User.ID, ctx.User.RateLimitRPM, ctx.User.RateLimitTPM, 200)
		if !allowed {
			ctx.Writer.Header().Set("Retry-After", "60")
			ctx.WriteJSONError(http.StatusTooManyRequests, msg, "rate_limit_exceeded")
			return nil
		}
	}

	return next(ctx)
}

// QuotaGuardMiddleware enforces lifetime and daily WIB token quotas.
type QuotaGuardMiddleware struct {
	repo repository.Repository
}

func NewQuotaGuardMiddleware(repo repository.Repository) *QuotaGuardMiddleware {
	return &QuotaGuardMiddleware{repo: repo}
}

func (m *QuotaGuardMiddleware) Name() string {
	return "quota_guard"
}

func (m *QuotaGuardMiddleware) Handle(ctx *PipelineContext, next NextFunc) error {
	user := ctx.User
	key := ctx.APIKey

	if user != nil {
		// Lifetime user quota (admins exempt)
		if !user.IsAdmin() && user.TokenQuota > 0 && user.TokensUsed >= user.TokenQuota {
			msg := fmt.Sprintf("Token quota limit reached (%d / %d tokens used). Please purchase tokens or contact admin.", user.TokensUsed, user.TokenQuota)
			ctx.WriteJSONError(http.StatusTooManyRequests, msg, "insufficient_quota")
			return nil
		}

		// Daily user budget (WIB)
		if !user.IsAdmin() && user.DailyTokenQuota > 0 {
			if today, err := m.repo.GetTodayTokenUsageByUser(ctx.Context, user.ID); err == nil && today >= user.DailyTokenQuota {
				msg := fmt.Sprintf("Daily token budget exhausted (%d / %d tokens today). Resets at midnight WIB.", today, user.DailyTokenQuota)
				ctx.WriteJSONError(http.StatusTooManyRequests, msg, "insufficient_quota")
				_ = m.repo.CreateSecurityEvent(ctx.Context, &entity.SecurityEvent{Kind: "budget_cutoff", UserID: user.ID, Detail: msg, Action: "blocked"})
				return nil
			}
		}
	}

	if key != nil {
		// Lifetime key budget
		if key.MaxTokensLimit > 0 && key.TokenUsage >= int64(key.MaxTokensLimit) {
			msg := fmt.Sprintf("API key token budget exhausted (%d / %d tokens used). Contact admin to increase the budget.", key.TokenUsage, key.MaxTokensLimit)
			ctx.WriteJSONError(http.StatusTooManyRequests, msg, "insufficient_quota")
			return nil
		}

		// Daily key budget (WIB)
		if key.DailyTokenQuota > 0 {
			if today, err := m.repo.GetTodayTokenUsageByKey(ctx.Context, key.ID); err == nil && int64(today) >= int64(key.DailyTokenQuota) {
				msg := fmt.Sprintf("API key daily budget exhausted (%d / %d tokens today). Resets at midnight WIB.", today, key.DailyTokenQuota)
				ctx.WriteJSONError(http.StatusTooManyRequests, msg, "insufficient_quota")
				_ = m.repo.CreateSecurityEvent(ctx.Context, &entity.SecurityEvent{Kind: "budget_cutoff", UserID: key.UserID, APIKeyID: key.ID, Detail: msg, Action: "blocked"})
				return nil
			}
		}
	}

	return next(ctx)
}

// ModelAccessMiddleware enforces model whitelist rules.
type ModelAccessMiddleware struct {
	repo repository.Repository
}

func NewModelAccessMiddleware(repo repository.Repository) *ModelAccessMiddleware {
	return &ModelAccessMiddleware{repo: repo}
}

func (m *ModelAccessMiddleware) Name() string {
	return "model_access"
}

func (m *ModelAccessMiddleware) Handle(ctx *PipelineContext, next NextFunc) error {
	requestedModel := ctx.RequestedModel
	if requestedModel == "" {
		return next(ctx)
	}

	if ctx.User != nil && !ctx.User.HasModelAccess(requestedModel) {
		allowedList := ctx.User.GetAllowedModels()
		errMsg := fmt.Sprintf("Model '%s' is not allowed for your account. Allowed models: %v", requestedModel, allowedList)
		ctx.WriteJSONError(http.StatusForbidden, errMsg, "permission_denied")
		_ = m.repo.CreateRequestLog(ctx.Context, &entity.RequestLog{
			UserID:       ctx.User.ID,
			Path:         ctx.Request.URL.Path,
			Method:       ctx.Request.Method,
			Model:        requestedModel,
			StatusCode:   http.StatusForbidden,
			DurationMs:   time.Since(ctx.StartTime).Milliseconds(),
			ClientIP:     ctx.ClientIP,
			ErrorMessage: errMsg,
		})
		return nil
	}

	if ctx.APIKey != nil && ctx.APIKey.AllowedModels != "" && ctx.APIKey.AllowedModels != "[]" && ctx.APIKey.AllowedModels != `["*"]` {
		if !ctx.APIKey.HasModelAccess(requestedModel) {
			allowedList := ctx.APIKey.GetAllowedModels()
			errMsg := fmt.Sprintf("Model '%s' is not allowed for this specific API key. Allowed key models: %v", requestedModel, allowedList)
			ctx.WriteJSONError(http.StatusForbidden, errMsg, "permission_denied")
			_ = m.repo.CreateRequestLog(ctx.Context, &entity.RequestLog{
				UserID:       ctx.User.ID,
				APIKeyID:     ctx.APIKey.ID,
				Path:         ctx.Request.URL.Path,
				Method:       ctx.Request.Method,
				Model:        requestedModel,
				StatusCode:   http.StatusForbidden,
				DurationMs:   time.Since(ctx.StartTime).Milliseconds(),
				ClientIP:     ctx.ClientIP,
				ErrorMessage: errMsg,
			})
			return nil
		}
	}

	return next(ctx)
}
