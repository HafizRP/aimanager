package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

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
	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, r, "settings.html", "base.html", map[string]interface{}{
		"ActivePage": "settings",
		"Config":     h.cfg,
		"SuccessMsg": successMsg,
		"ErrorMsg":   errorMsg,
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
