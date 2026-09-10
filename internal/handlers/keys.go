package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"9router-gateway/internal/models"
)

func (h *Handler) KeysPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	var keys []models.APIKey
	var err error

	if currentUser != nil && currentUser.IsAdmin() {
		keys, err = h.repo.GetAllAPIKeys(ctx)
		filterUserID := r.URL.Query().Get("user_id")
		if filterUserID != "" {
			filtered := []models.APIKey{}
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
		keys = []models.APIKey{}
	}

	var users []models.User
	if currentUser != nil && currentUser.IsAdmin() {
		users, _ = h.repo.GetAllUsers(ctx)
	} else if currentUser != nil {
		users = []models.User{*currentUser}
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

func (h *Handler) CreateKey(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	userID := strings.TrimSpace(r.FormValue("user_id"))
	// Enforce self-only key creation for non-admin
	if currentUser != nil && !currentUser.IsAdmin() {
		userID = currentUser.ID
	}

	name := strings.TrimSpace(r.FormValue("name"))
	customKey := strings.TrimSpace(r.FormValue("custom_key"))
	allowedModelsRaw := strings.TrimSpace(r.FormValue("allowed_models"))
	rateLimitRPM, _ := strconv.Atoi(r.FormValue("rate_limit_rpm"))

	redirectURL := safeRedirectURL(r.FormValue("redirect"), "/keys")

	if userID == "" {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape("User must be selected"), http.StatusSeeOther)
		return
	}
	if name == "" {
		name = "API Key"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	if rateLimitRPM < 0 {
		rateLimitRPM = 0
	}

	finalKey := customKey
	if finalKey == "" {
		finalKey = GenerateSecureAPIKey("sk-gw-")
	} else {
		if len(finalKey) < 8 || len(finalKey) > 128 {
			http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape("Custom key must be between 8 and 128 characters"), http.StatusSeeOther)
			return
		}
		if existing, _ := h.repo.GetAPIKeyByKey(ctx, finalKey); existing != nil {
			http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape("API key already exists"), http.StatusSeeOther)
			return
		}
	}

	allowedModels := ""
	if allowedModelsRaw != "" && allowedModelsRaw != "*" {
		if strings.HasPrefix(allowedModelsRaw, "[") {
			allowedModels = allowedModelsRaw
		} else {
			parts := strings.Split(allowedModelsRaw, ",")
			var clean []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					clean = append(clean, p)
				}
			}
			b, _ := json.Marshal(clean)
			allowedModels = string(b)
		}
	}

	apiKey := &models.APIKey{
		ID:            uuid.New().String(),
		UserID:        userID,
		Key:           finalKey,
		Name:          name,
		AllowedModels: allowedModels,
		RateLimitRPM:  rateLimitRPM,
		IsActive:      true,
	}

	if err := h.repo.CreateAPIKey(ctx, apiKey); err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Sync to 9router Core
	if h.syncer != nil {
		u, _ := h.repo.GetUserByID(ctx, userID)
		uName := ""
		if u != nil {
			uName = u.Name
		}
		_ = h.syncer.SyncKey(apiKey, uName)
	}

	http.Redirect(w, r, redirectURL+"?msg="+url.QueryEscape("Key created successfully! Token: "+finalKey), http.StatusSeeOther)
}

func (h *Handler) ToggleKeyStatus(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "id")
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	redirectURL := safeRedirectURL(r.URL.Query().Get("redirect"), "")
	if redirectURL == "" {
		redirectURL = safeRedirectURL(r.FormValue("redirect"), "/keys")
	}

	// Find key to toggle
	keys, err := h.repo.GetAllAPIKeys(ctx)
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error=Key+not+found", http.StatusSeeOther)
		return
	}

	var targetKey *models.APIKey
	for _, k := range keys {
		if k.ID == keyID {
			targetKey = &k
			break
		}
	}

	if targetKey == nil {
		http.Redirect(w, r, redirectURL+"?error=Key+not+found", http.StatusSeeOther)
		return
	}

	// Non-admin can only toggle their own keys
	if currentUser != nil && !currentUser.IsAdmin() && targetKey.UserID != currentUser.ID {
		http.Redirect(w, r, redirectURL+"?error=Permission+denied", http.StatusSeeOther)
		return
	}

	newStatus := !targetKey.IsActive
	if err := h.repo.ToggleAPIKeyStatus(ctx, keyID, newStatus); err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Sync toggle to 9router Core
	if h.syncer != nil {
		_ = h.syncer.ToggleKey(keyID, newStatus)
	}

	msg := "API key suspended"
	if newStatus {
		msg = "API key reactivated"
	}
	http.Redirect(w, r, redirectURL+"?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (h *Handler) DeleteKey(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "id")
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	redirectURL := safeRedirectURL(r.URL.Query().Get("redirect"), "")
	if redirectURL == "" {
		redirectURL = safeRedirectURL(r.FormValue("redirect"), "/keys")
	}

	keys, err := h.repo.GetAllAPIKeys(ctx)
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error=Key+not+found", http.StatusSeeOther)
		return
	}

	var targetKey *models.APIKey
	for _, k := range keys {
		if k.ID == keyID {
			targetKey = &k
			break
		}
	}

	if targetKey == nil {
		http.Redirect(w, r, redirectURL+"?error=Key+not+found", http.StatusSeeOther)
		return
	}

	// Non-admin can only delete their own keys
	if currentUser != nil && !currentUser.IsAdmin() && targetKey.UserID != currentUser.ID {
		http.Redirect(w, r, redirectURL+"?error=Permission+denied", http.StatusSeeOther)
		return
	}

	if err := h.repo.DeleteAPIKey(ctx, keyID); err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Sync delete to 9router Core
	if h.syncer != nil {
		_ = h.syncer.DeleteKey(keyID)
	}

	http.Redirect(w, r, redirectURL+"?msg=API+key+revoked+and+deleted", http.StatusSeeOther)
}
