package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"9router-gateway/internal/entity"
	"9router-gateway/internal/proxy"
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
		"ClientIP":   proxy.GetClientIP(r),
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
		AllowedIPs:     strings.TrimSpace(r.FormValue("allowed_ips")),
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

// EditKey updates an existing API key's configuration.
func (h *Handler) EditKey(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "id")
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	redirectURL := safeRedirectURL(r.FormValue("redirect"), "")
	if redirectURL == "" {
		redirectURL = safeRedirectURL(r.URL.Query().Get("redirect"), "/keys")
	}

	isJSON := strings.Contains(r.Header.Get("Accept"), "application/json") || strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Content-Type"), "application/json")

	// Fetch existing key to check ownership
	key, err := h.repo.GetAPIKeyByID(ctx, keyID)
	if err != nil || key == nil {
		if isJSON {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "API key not found"})
			return
		}
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape("API key not found"), http.StatusSeeOther)
		return
	}

	// Non-admins can only edit their own keys
	if !currentUser.IsAdmin() && key.UserID != currentUser.ID {
		if isJSON {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Permission denied"})
			return
		}
		http.Redirect(w, r, redirectURL+"?error=Permission+denied", http.StatusSeeOther)
		return
	}

	var in usecase.UpdateKeyInput
	in.ID = keyID

	if isJSON && strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var payload struct {
			Name            string `json:"name"`
			AllowedModels   string `json:"allowed_models"`
			AllowedIPs      string `json:"allowed_ips"`
			RateLimitRPM    int    `json:"rate_limit_rpm"`
			MaxTokensLimit  int    `json:"max_tokens_limit"`
			DailyTokenQuota int    `json:"daily_token_quota"`
			ExpiresAt       string `json:"expires_at"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Invalid request body"})
			return
		}
		in.Name = payload.Name
		in.AllowedModels = payload.AllowedModels
		in.AllowedIPs = payload.AllowedIPs
		in.RateLimitRPM = payload.RateLimitRPM
		in.MaxTokensLimit = payload.MaxTokensLimit
		in.DailyTokenQuota = payload.DailyTokenQuota
		if strings.TrimSpace(payload.ExpiresAt) != "" {
			parsed, err := parseExpiryInput(payload.ExpiresAt)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Invalid expires_at format: " + err.Error()})
				return
			}
			in.ExpiresAt = &parsed
		}
	} else {
		_ = r.ParseForm()
		in.Name = r.FormValue("name")
		in.AllowedModels = r.FormValue("allowed_models")
		in.AllowedIPs = r.FormValue("allowed_ips")
		in.RateLimitRPM, _ = strconv.Atoi(r.FormValue("rate_limit_rpm"))
		in.MaxTokensLimit, _ = strconv.Atoi(r.FormValue("max_tokens_limit"))
		in.DailyTokenQuota, _ = strconv.Atoi(r.FormValue("daily_token_quota"))

		rawExp := strings.TrimSpace(r.FormValue("expires_at"))
		if rawExp != "" {
			parsed, err := parseExpiryInput(rawExp)
			if err != nil {
				http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape("Invalid expires_at: "+err.Error()), http.StatusSeeOther)
				return
			}
			in.ExpiresAt = &parsed
		}
	}

	updated, err := h.keys.UpdateKey(ctx, in)
	if err != nil {
		if isJSON {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
			return
		}
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if isJSON {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "API key updated successfully",
			"key":     updated,
		})
		return
	}

	http.Redirect(w, r, redirectURL+"?msg="+url.QueryEscape("API key '"+updated.Name+"' updated successfully!"), http.StatusSeeOther)
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

// ResetKeyUsage zeroes an API key's token usage counter.
func (h *Handler) ResetKeyUsage(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "id")
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	redirectURL := safeRedirectURL(r.URL.Query().Get("redirect"), "")
	if redirectURL == "" {
		redirectURL = safeRedirectURL(r.FormValue("redirect"), "/keys")
	}

	isJSON := strings.Contains(r.Header.Get("Accept"), "application/json") || strings.HasPrefix(r.URL.Path, "/api/")

	// Ownership enforcement (admin can reset any key, standard user can only reset own key)
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
			if isJSON {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "Permission denied",
				})
				return
			}
			http.Redirect(w, r, redirectURL+"?error=Permission+denied", http.StatusSeeOther)
			return
		}
	}

	if err := h.keys.ResetKeyUsage(ctx, keyID); err != nil {
		if isJSON {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if isJSON {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Key token usage counter reset to 0",
		})
		return
	}

	http.Redirect(w, r, redirectURL+"?msg="+url.QueryEscape("API key token usage reset to 0"), http.StatusSeeOther)
}

// APIKeyStats returns performance metrics, hourly token breakdown, top models, and recent requests for an API key.
func (h *Handler) APIKeyStats(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "id")
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	// Fetch all keys to check existence and ownership
	var targetKey *entity.APIKey
	allKeys, err := h.repo.GetAllAPIKeys(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to fetch keys"})
		return
	}
	for i := range allKeys {
		if allKeys[i].ID == keyID {
			targetKey = &allKeys[i]
			break
		}
	}

	if targetKey == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "API key not found"})
		return
	}

	// Security: Standard user can only inspect their own keys (HTTP 404 to avoid oracle)
	if currentUser != nil && !currentUser.IsAdmin() && targetKey.UserID != currentUser.ID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "API key not found"})
		return
	}

	// Enrich key user name if empty
	if targetKey.UserName == "" {
		if u, _ := h.repo.GetUserByID(ctx, targetKey.UserID); u != nil {
			targetKey.UserName = u.Name
		}
	}

	summary, hourly, topModels, recent, err := h.repo.GetKeyStats(ctx, keyID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	resp := entity.KeyStatsResponse{
		Key:            *targetKey,
		Summary:        *summary,
		HourlyUsage:    hourly,
		TopModels:      topModels,
		RecentRequests: recent,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}