package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"9router-gateway/internal/models"
)

func (h *Handler) ModelsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	// Fetch detailed models from 9router
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.UpstreamURL+"/v1/models", nil)
	var allModels []UpstreamModelItem
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+h.cfg.UpstreamAPIKey)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			var res struct {
				Data []UpstreamModelItem `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&res)
			allModels = res.Data
		}
	}

	// Filter for non-admin user
	displayModels := allModels
	if currentUser != nil && !currentUser.IsAdmin() {
		allowedList := parseAllowedModels(currentUser.AllowedModels)
		if !hasWildcard(allowedList) {
			allowedMap := make(map[string]bool)
			for _, m := range allowedList {
				allowedMap[strings.TrimSpace(m)] = true
			}
			displayModels = []UpstreamModelItem{}
			for _, m := range allModels {
				if allowedMap[m.ID] {
					displayModels = append(displayModels, m)
				}
			}
		}
	}

	var users []models.User
	if currentUser != nil && currentUser.IsAdmin() {
		users, _ = h.repo.GetAllUsers(ctx)
	}

	h.render(w, r, "models.html", "base.html", map[string]interface{}{
		"ActivePage": "models",
		"Models":     displayModels,
		"Users":      users,
	})
}

func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	currentBaseURL := fmt.Sprintf("%s://%s/v1", scheme, host)

	var userKey string
	if currentUser != nil {
		keys, err := h.repo.GetAPIKeysByUserID(ctx, currentUser.ID)
		if err == nil && len(keys) > 0 {
			for _, k := range keys {
				if k.IsActive {
					userKey = k.Key
					break
				}
			}
			if userKey == "" {
				userKey = keys[0].Key
			}
		}
		// If user has no key, create one automatically
		if userKey == "" {
			prefix := "sk-gw-"
			if currentUser.IsAdmin() {
				prefix = "sk-gw-admin-"
			}
			newKey := GenerateSecureAPIKey(prefix)
			apiKey := &models.APIKey{
				ID:       uuid.New().String(),
				UserID:   currentUser.ID,
				Key:      newKey,
				Name:     "Default Key",
				IsActive: true,
			}
			_ = h.repo.CreateAPIKey(ctx, apiKey)
			if h.syncer != nil {
				_ = h.syncer.SyncKey(apiKey, currentUser.Name)
			}
			userKey = newKey
		}
	}
	if userKey == "" {
		userKey = "sk-gw-your-api-key"
	}

	userModel := "ag/gemini-3.8-flash-high"
	if currentUser != nil {
		allowed := parseAllowedModels(currentUser.AllowedModels)
		if len(allowed) > 0 && allowed[0] != "*" {
			userModel = allowed[0]
		}
	}

	h.render(w, r, "settings.html", "base.html", map[string]interface{}{
		"ActivePage":     "settings",
		"Config":         h.cfg,
		"CurrentBaseURL": currentBaseURL,
		"UserKey":        userKey,
		"UserModel":      userModel,
		"SuccessMsg":     successMsg,
		"ErrorMsg":       errorMsg,
	})
}

func (h *Handler) UpdatePasswordPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	if currentUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	currentPass := strings.TrimSpace(r.FormValue("current_password"))
	newPass := strings.TrimSpace(r.FormValue("new_password"))

	if len(newPass) < 6 {
		http.Redirect(w, r, "/settings?error=New+password+must+be+at+least+6+characters", http.StatusSeeOther)
		return
	}

	// Verify current password
	valid := CheckPasswordHash(currentPass, currentUser.PasswordHash)
	if !valid && (currentUser.Username == "admin" || currentUser.Username == h.cfg.AdminUsername) {
		valid = (currentPass == h.cfg.AdminPassword)
	}

	if !valid {
		http.Redirect(w, r, "/settings?error=Current+password+does+not+match", http.StatusSeeOther)
		return
	}

	newHash, err := HashPassword(newPass)
	if err != nil {
		http.Redirect(w, r, "/settings?error=Failed+to+encrypt+password", http.StatusSeeOther)
		return
	}

	if currentUser.IsAdmin() && (currentUser.Username == "admin" || currentUser.Username == h.cfg.AdminUsername) {
		h.cfg.AdminPassword = newPass
	}

	_ = h.repo.UpdateUserPassword(ctx, currentUser.ID, newHash)
	http.Redirect(w, r, "/settings?msg=Password+updated+successfully", http.StatusSeeOther)
}

func parseAllowedModels(raw string) []string {
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

func hasWildcard(list []string) bool {
	for _, m := range list {
		if strings.TrimSpace(m) == "*" {
			return true
		}
	}
	return false
}
