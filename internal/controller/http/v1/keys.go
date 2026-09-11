package v1

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"9router-gateway/internal/entity"
	"9router-gateway/internal/usecase"
)

// parseExpiryInput parses a datetime-local / RFC3339 value into a time.Time.
// It assumes the browser submits a local datetime in WIB (Asia/Jakarta).
func parseExpiryInput(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}

	layouts := []string{
		"2006-01-02T15:04",
		time.RFC3339,
		"2006-01-02 15:04",
		"2006-01-02",
	}

	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.FixedZone("WIB", 7*3600)
	}

	var parsed time.Time
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, raw, loc); err == nil {
			parsed = t
			break
		}
	}
	if parsed.IsZero() {
		return time.Time{}, fmt.Errorf("unrecognized datetime format: %s", raw)
	}
	return parsed, nil
}

// KeysPage renders the API key management page.
func (h *Handler) KeysPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	var keys []entity.APIKey
	var err error

	if currentUser != nil && currentUser.IsAdmin() {
		keys, err = h.repo.GetAllAPIKeys(ctx)
		filterUserID := r.URL.Query().Get("user_id")
		if filterUserID != "" {
			filtered := []entity.APIKey{}
			for _, k := range keys {
				if k.UserID == filterUserID {
					filtered = append(filtered, k)
				}
			}
			keys = filtered
		}
	} else if currentUser != nil {
		keys, err = h.repo.GetAPIKeysByUserID(ctx, currentUser.ID)
		for i := range keys {
			keys[i].UserName = currentUser.Name
		}
	}

	if err != nil {
		keys = []entity.APIKey{}
	}

	var users []entity.User
	if currentUser != nil && currentUser.IsAdmin() {
		users, _ = h.repo.GetAllUsers(ctx)
	} else if currentUser != nil {
		users = []entity.User{*currentUser}
	}

	allModels, _ := h.fetchUpstreamModels(ctx)

	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, r, "keys.html", "base.html", map[string]interface{}{
		"ActivePage": "keys",
		"Keys":       keys,
		"Users":      users,
		"Models":     allModels,
		"SuccessMsg": successMsg,
		"ErrorMsg":   errorMsg,
	})
}

// CreateKey creates a new API key for the current or target user.
func (h *Handler) CreateKey(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	userID := strings.TrimSpace(r.FormValue("user_id"))
	// Enforce self-only key creation for non-admin
	if currentUser != nil && !currentUser.IsAdmin() {
		userID = currentUser.ID
	}

	redirectURL := safeRedirectURL(r.FormValue("redirect"), "/keys")

	// Optional expiry datetime (RFC3339 or "YYYY-MM-DDTHH:MM")
	var expiresAt *time.Time
	if rawExp := strings.TrimSpace(r.FormValue("expires_at")); rawExp != "" {
		parsed, err := parseExpiryInput(rawExp)
		if err != nil {
			http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape("Invalid expires_at: "+err.Error()), http.StatusSeeOther)
			return
		}
		expiresAt = &parsed
	}

	if userID == "" {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape("User must be selected"), http.StatusSeeOther)
		return
	}

	rateLimitRPM, _ := strconv.Atoi(r.FormValue("rate_limit_rpm"))
	maxTokensLimit, _ := strconv.Atoi(r.FormValue("max_tokens_limit"))
	dailyQuota, _ := strconv.Atoi(r.FormValue("daily_token_quota"))

	key, err := h.keys.CreateKey(ctx, usecase.CreateKeyInput{
		UserID:          userID,
		Name:            strings.TrimSpace(r.FormValue("name")),
		CustomKey:       strings.TrimSpace(r.FormValue("custom_key")),
		AllowedModels:   strings.TrimSpace(r.FormValue("allowed_models")),
		RateLimitRPM:    rateLimitRPM,
		MaxTokensLimit:  maxTokensLimit,
		DailyTokenQuota: dailyQuota,
		ExpiresAt:       expiresAt,
	})
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, redirectURL+"?msg="+url.QueryEscape("Key created successfully! Token: "+key.Key), http.StatusSeeOther)
}

// ToggleKeyStatus activates/deactivates an API key.
func (h *Handler) ToggleKeyStatus(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "id")
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	redirectURL := safeRedirectURL(r.URL.Query().Get("redirect"), "")
	if redirectURL == "" {
		redirectURL = safeRedirectURL(r.FormValue("redirect"), "/keys")
	}

	// Ownership enforcement (admin can toggle any key)
	if currentUser != nil && !currentUser.IsAdmin() {
		keys, _ := h.repo.GetAllAPIKeys(ctx)
		owned := false
		for _, k := range keys {
			if k.ID == keyID && k.UserID == currentUser.ID {
				owned = true
				break
			}
		}
		if !owned {
			http.Redirect(w, r, redirectURL+"?error=Permission+denied", http.StatusSeeOther)
			return
		}
	}

	newStatus, err := h.keys.ToggleKeyStatus(ctx, keyID)
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	msg := "API key suspended"
	if newStatus {
		msg = "API key reactivated"
	}
	http.Redirect(w, r, redirectURL+"?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

// DeleteKey deletes an API key.
func (h *Handler) DeleteKey(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "id")
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	redirectURL := safeRedirectURL(r.URL.Query().Get("redirect"), "")
	if redirectURL == "" {
		redirectURL = safeRedirectURL(r.FormValue("redirect"), "/keys")
	}

	// Ownership enforcement (admin can delete any key)
	if currentUser != nil && !currentUser.IsAdmin() {
		keys, _ := h.repo.GetAllAPIKeys(ctx)
		owned := false
		for _, k := range keys {
			if k.ID == keyID && k.UserID == currentUser.ID {
				owned = true
				break
			}
		}
		if !owned {
			http.Redirect(w, r, redirectURL+"?error=Permission+denied", http.StatusSeeOther)
			return
		}
	}

	if err := h.keys.DeleteKey(ctx, keyID); err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redirectURL+"?msg=API+key+revoked+and+deleted", http.StatusSeeOther)
}