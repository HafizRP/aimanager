package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"9router-gateway/internal/models"
)

func (h *Handler) UsersPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	users, err := h.repo.GetAllUsers(ctx)
	if err != nil {
		users = []models.User{}
	}

	availableModels := h.FetchUpstreamModels(ctx)

	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, "users.html", "base.html", map[string]interface{}{
		"ActivePage":      "users",
		"Users":           users,
		"AvailableModels": availableModels,
		"SuccessMsg":      successMsg,
		"ErrorMsg":        errorMsg,
	})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/users?error=User+name+cannot+be+empty", http.StatusSeeOther)
		return
	}

	quotaStr := r.FormValue("token_quota")
	quota, _ := strconv.ParseInt(quotaStr, 10, 64)

	var allowedModelsJSON string
	if r.FormValue("allow_all") == "true" {
		allowedModelsJSON = "[\"*\"]"
	} else {
		selectedModels := r.Form["models"]
		if len(selectedModels) == 0 {
			allowedModelsJSON = "[\"*\"]"
		} else {
			bytes, _ := json.Marshal(selectedModels)
			allowedModelsJSON = string(bytes)
		}
	}

	userID := uuid.New().String()
	user := &models.User{
		ID:            userID,
		Name:          name,
		Role:          "user",
		TokenQuota:    quota,
		TokensUsed:    0,
		AllowedModels: allowedModelsJSON,
		IsActive:      true,
	}

	ctx := r.Context()
	if err := h.repo.CreateUser(ctx, user); err != nil {
		http.Redirect(w, r, "/users?error="+err.Error(), http.StatusSeeOther)
		return
	}

	// Auto-generate key if checked
	if r.FormValue("create_key") == "true" {
		generatedKey := GenerateSecureAPIKey("sk-gw-")
		apiKey := &models.APIKey{
			ID:       uuid.New().String(),
			UserID:   userID,
			Key:      generatedKey,
			Name:     "Initial Key",
			IsActive: true,
		}
		_ = h.repo.CreateAPIKey(ctx, apiKey)
		http.Redirect(w, r, "/users?msg=User+and+API+key+created+successfully!+Key:+"+generatedKey, http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/users?msg=User+created+successfully", http.StatusSeeOther)
}

func (h *Handler) EditUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	userID := chi.URLParam(r, "id")
	if userID == "" {
		userID = r.FormValue("id")
	}

	ctx := r.Context()
	user, err := h.repo.GetUserByID(ctx, userID)
	if err != nil {
		http.Redirect(w, r, "/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name != "" {
		user.Name = name
	}

	quotaStr := r.FormValue("token_quota")
	quota, _ := strconv.ParseInt(quotaStr, 10, 64)
	user.TokenQuota = quota

	if r.FormValue("allow_all") == "true" {
		user.AllowedModels = "[\"*\"]"
	} else {
		selectedModels := r.Form["models"]
		if len(selectedModels) == 0 {
			user.AllowedModels = "[\"*\"]"
		} else {
			bytes, _ := json.Marshal(selectedModels)
			user.AllowedModels = string(bytes)
		}
	}

	user.IsActive = (r.FormValue("is_active") == "true")

	if err := h.repo.UpdateUser(ctx, user); err != nil {
		http.Redirect(w, r, "/users?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/users?msg=User+updated+successfully", http.StatusSeeOther)
}

func (h *Handler) ToggleUserStatus(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	ctx := r.Context()
	user, err := h.repo.GetUserByID(ctx, userID)
	if err != nil {
		http.Redirect(w, r, "/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	newStatus := !user.IsActive
	if err := h.repo.ToggleUserStatus(ctx, userID, newStatus); err != nil {
		http.Redirect(w, r, "/users?error="+err.Error(), http.StatusSeeOther)
		return
	}

	msg := "User suspended"
	if newStatus {
		msg = "User reactivated"
	}
	http.Redirect(w, r, "/users?msg="+msg, http.StatusSeeOther)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	ctx := r.Context()
	if err := h.repo.DeleteUser(ctx, userID); err != nil {
		http.Redirect(w, r, "/users?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/users?msg=User+and+keys+deleted+successfully", http.StatusSeeOther)
}
