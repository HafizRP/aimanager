package v1

import (
	"encoding/csv"
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

// ExportKeys streams API keys as CSV or JSON file
func (h *Handler) ExportKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	var keys []entity.APIKey
	var err error

	filterUserID := r.URL.Query().Get("user_id")
	if currentUser != nil && currentUser.IsAdmin() {
		keys, err = h.repo.GetAllAPIKeys(ctx)
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
		http.Error(w, "Failed to retrieve API keys", http.StatusInternalServerError)
		return
	}

	// Status filter if requested
	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if statusFilter != "" && statusFilter != "all" {
		filtered := []entity.APIKey{}
		for _, k := range keys {
			switch statusFilter {
			case "active":
				if k.IsActive && !k.IsExpired() {
					filtered = append(filtered, k)
				}
			case "revoked", "suspended", "inactive":
				if !k.IsActive {
					filtered = append(filtered, k)
				}
			case "expired":
				if k.IsExpired() {
					filtered = append(filtered, k)
				}
			}
		}
		keys = filtered
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	if format != "json" {
		format = "csv"
	}

	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=api_keys_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(keys)
		return
	}

	// CSV format
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=api_keys_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write CSV Header
	_ = writer.Write([]string{
		"ID", "Name", "User", "Key", "Model_Scope", "Allowed_IPs",
		"Rate_Limit_RPM", "Max_Tokens_Limit", "Daily_Token_Quota",
		"Tokens_Used", "Status", "Expires_At_WIB", "Created_At_WIB", "Last_Used_At_WIB", "Last_Used_IP",
	})

	for _, k := range keys {
		status := "Active"
		if !k.IsActive {
			status = "Revoked"
		} else if k.IsExpired() {
			status = "Expired"
		}

		expiresAtWIB := "-"
		if k.ExpiresAt != nil {
			expiresAtWIB = k.ExpiresAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		}

		lastUsedAtWIB := "-"
		if k.LastUsedAt != nil {
			lastUsedAtWIB = k.LastUsedAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		}

		createdAtWIB := k.CreatedAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")

		_ = writer.Write([]string{
			k.ID,
			k.Name,
			k.UserName,
			k.Key,
			k.AllowedModels,
			k.AllowedIPs,
			strconv.Itoa(k.RateLimitRPM),
			strconv.Itoa(k.MaxTokensLimit),
			strconv.Itoa(k.DailyTokenQuota),
			strconv.FormatInt(k.TokenUsage, 10),
			status,
			expiresAtWIB,
			createdAtWIB,
			lastUsedAtWIB,
			k.LastUsedIP,
		})
	}
}

// CloneKey duplicates an existing API key configuration with a newly generated token.
func (h *Handler) CloneKey(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	keyID := chi.URLParam(r, "id")
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	redirectURL := safeRedirectURL(r.FormValue("redirect"), "/keys")
	isJSON := strings.Contains(r.Header.Get("Accept"), "application/json") || strings.HasPrefix(r.URL.Path, "/api/")

	// Fetch source key
	sourceKey, err := h.repo.GetAPIKeyByID(ctx, keyID)
	if err != nil {
		if isJSON {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Source key not found"})
			return
		}
		http.Redirect(w, r, redirectURL+"?error=Source+key+not+found", http.StatusSeeOther)
		return
	}

	// Ownership enforcement
	if currentUser != nil && !currentUser.IsAdmin() && sourceKey.UserID != currentUser.ID {
		if isJSON {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Permission denied"})
			return
		}
		http.Redirect(w, r, redirectURL+"?error=Permission+denied", http.StatusSeeOther)
		return
	}

	// Custom clone name or fallback to "Original (Copy)"
	cloneName := strings.TrimSpace(r.FormValue("name"))
	if cloneName == "" {
		cloneName = sourceKey.Name + " (Copy)"
	}

	newKey, err := h.keys.CreateKey(ctx, usecase.CreateKeyInput{
		UserID:          sourceKey.UserID,
		Name:            cloneName,
		AllowedModels:   sourceKey.AllowedModels,
		AllowedIPs:     sourceKey.AllowedIPs,
		RateLimitRPM:    sourceKey.RateLimitRPM,
		MaxTokensLimit:  sourceKey.MaxTokensLimit,
		DailyTokenQuota: sourceKey.DailyTokenQuota,
		ExpiresAt:       sourceKey.ExpiresAt,
	})
	if err != nil {
		if isJSON {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
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
			"key":     newKey,
			"message": "Key cloned successfully",
		})
		return
	}

	http.Redirect(w, r, redirectURL+"?msg="+url.QueryEscape("Key cloned successfully! Token: "+newKey.Key), http.StatusSeeOther)
}