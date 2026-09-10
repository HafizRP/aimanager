package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (h *Handler) ModelsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Fetch detailed models from 9router
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.UpstreamURL+"/v1/models", nil)
	var models []UpstreamModelItem
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+h.cfg.UpstreamAPIKey)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			var res struct {
				Data []UpstreamModelItem `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&res)
			models = res.Data
		}
	}

	users, _ := h.repo.GetAllUsers(ctx)

	h.render(w, "models.html", "base.html", map[string]interface{}{
		"ActivePage": "models",
		"Models":     models,
		"Users":      users,
	})
}

func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, "settings.html", "base.html", map[string]interface{}{
		"ActivePage": "settings",
		"Config":     h.cfg,
		"SuccessMsg": successMsg,
		"ErrorMsg":   errorMsg,
	})
}

func (h *Handler) UpdatePasswordPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	currentPass := strings.TrimSpace(r.FormValue("current_password"))
	newPass := strings.TrimSpace(r.FormValue("new_password"))

	if currentPass != h.cfg.AdminPassword {
		http.Redirect(w, r, "/settings?error=Current+password+does+not+match", http.StatusSeeOther)
		return
	}

	if len(newPass) < 6 {
		http.Redirect(w, r, "/settings?error=New+password+must+be+at+least+6+characters", http.StatusSeeOther)
		return
	}

	h.cfg.AdminPassword = newPass
	http.Redirect(w, r, "/settings?msg=Admin+password+updated+successfully", http.StatusSeeOther)
}
