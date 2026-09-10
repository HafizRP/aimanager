package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"9router-gateway/internal/models"
)

func (h *Handler) KeysPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	keys, err := h.repo.GetAllAPIKeys(ctx)
	if err != nil {
		keys = []models.APIKey{}
	}

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

	users, _ := h.repo.GetAllUsers(ctx)

	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, "keys.html", "base.html", map[string]interface{}{
		"ActivePage": "keys",
		"Keys":       keys,
		"Users":      users,
		"SuccessMsg": successMsg,
		"ErrorMsg":   errorMsg,
	})
}

func (h *Handler) CreateKey(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	userID := strings.TrimSpace(r.FormValue("user_id"))
	name := strings.TrimSpace(r.FormValue("name"))
	customKey := strings.TrimSpace(r.FormValue("custom_key"))

	if userID == "" {
		http.Redirect(w, r, "/keys?error=User+must+be+selected", http.StatusSeeOther)
		return
	}
	if name == "" {
		name = "API Key"
	}

	finalKey := customKey
	if finalKey == "" {
		finalKey = GenerateSecureAPIKey("sk-gw-")
	}

	apiKey := &models.APIKey{
		ID:       uuid.New().String(),
		UserID:   userID,
		Key:      finalKey,
		Name:     name,
		IsActive: true,
	}

	ctx := r.Context()
	if err := h.repo.CreateAPIKey(ctx, apiKey); err != nil {
		http.Redirect(w, r, "/keys?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/keys?msg=Key+created+successfully!+Token:+"+finalKey, http.StatusSeeOther)
}

func (h *Handler) ToggleKeyStatus(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "id")
	ctx := r.Context()

	// Find key to toggle
	keys, err := h.repo.GetAllAPIKeys(ctx)
	if err != nil {
		http.Redirect(w, r, "/keys?error=Key+not+found", http.StatusSeeOther)
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
		http.Redirect(w, r, "/keys?error=Key+not+found", http.StatusSeeOther)
		return
	}

	newStatus := !targetKey.IsActive
	if err := h.repo.ToggleAPIKeyStatus(ctx, keyID, newStatus); err != nil {
		http.Redirect(w, r, "/keys?error="+err.Error(), http.StatusSeeOther)
		return
	}

	msg := "API key suspended"
	if newStatus {
		msg = "API key reactivated"
	}
	http.Redirect(w, r, "/keys?msg="+msg, http.StatusSeeOther)
}

func (h *Handler) DeleteKey(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "id")
	ctx := r.Context()
	if err := h.repo.DeleteAPIKey(ctx, keyID); err != nil {
		http.Redirect(w, r, "/keys?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/keys?msg=API+key+revoked+and+deleted", http.StatusSeeOther)
}
