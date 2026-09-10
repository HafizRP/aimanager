package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"9router-gateway/internal/models"
	"9router-gateway/internal/usecase"
)

var (
	nonAlphaNumericRegex = regexp.MustCompile(`[^a-z0-9]+`)
	validUsernameRegex   = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)
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

	h.render(w, r, "users.html", "base.html", map[string]interface{}{
		"ActivePage":      "users",
		"Users":           users,
		"AvailableModels": availableModels,
		"SuccessMsg":      successMsg,
		"ErrorMsg":        errorMsg,
	})
}

func (h *Handler) UserDetailPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := chi.URLParam(r, "id")

	user, err := h.repo.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		http.Redirect(w, r, "/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	keys, err := h.repo.GetAPIKeysByUserID(ctx, userID)
	if err != nil {
		keys = []models.APIKey{}
	}
	for i := range keys {
		keys[i].UserName = user.Name
	}

	logs, totalLogs, err := h.repo.GetRequestLogs(ctx, 15, 0, userID, "", 0)
	if err != nil {
		logs = []models.RequestLog{}
		totalLogs = 0
	}

	stats, err := h.dash.GetStats(ctx, user, "30d")
	if err != nil {
		stats = &models.DashboardStats{}
	}

	availableModels := h.FetchUpstreamModels(ctx)

	var allowedModelsList []string
	isAllModels := false
	trimmed := strings.TrimSpace(user.AllowedModels)
	if trimmed == `["*"]` || trimmed == "*" || strings.Contains(trimmed, `*`) {
		isAllModels = true
	} else {
		_ = json.Unmarshal([]byte(trimmed), &allowedModelsList)
	}

	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, r, "user_detail.html", "base.html", map[string]interface{}{
		"ActivePage":        "users",
		"TargetUser":        user,
		"APIKeys":           keys,
		"Logs":              logs,
		"TotalLogs":         totalLogs,
		"Stats":             stats,
		"AvailableModels":   availableModels,
		"AllowedModelsList": allowedModelsList,
		"IsAllModels":       isAllModels,
		"SuccessMsg":        successMsg,
		"ErrorMsg":          errorMsg,
	})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	quotaStr := r.FormValue("token_quota")
	quota, _ := strconv.ParseInt(quotaStr, 10, 64)

	var allowedModelsJSON string
	if r.FormValue("allow_all") == "true" {
		allowedModelsJSON = `["*"]`
	} else {
		selectedModels := r.Form["models"]
		if len(selectedModels) == 0 {
			allowedModelsJSON = `["*"]`
		} else {
			b, _ := json.Marshal(selectedModels)
			allowedModelsJSON = string(b)
		}
	}

	_, password, generatedKey, err := h.users.CreateUser(r.Context(), usecase.CreateUserInput{
		Name:          strings.TrimSpace(r.FormValue("name")),
		Username:      strings.TrimSpace(r.FormValue("username")),
		Password:      strings.TrimSpace(r.FormValue("password")),
		Role:          strings.TrimSpace(r.FormValue("role")),
		TokenQuota:    quota,
		AllowedModels: allowedModelsJSON,
		CreateKey:     r.FormValue("create_key") == "true",
	})
	if err != nil {
		http.Redirect(w, r, "/users?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if generatedKey != "" {
		msg := fmt.Sprintf("User created! Password: %s | Key: %s", password, generatedKey)
		http.Redirect(w, r, "/users?msg="+url.QueryEscape(msg), http.StatusSeeOther)
		return
	}
	msg := fmt.Sprintf("User created successfully! Login Password: %s", password)
	http.Redirect(w, r, "/users?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (h *Handler) EditUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	userID := chi.URLParam(r, "id")
	if userID == "" {
		userID = r.FormValue("id")
	}
	redirectURL := safeRedirectURL(r.FormValue("redirect"), "/users")

	quotaStr := r.FormValue("token_quota")
	quota, _ := strconv.ParseInt(quotaStr, 10, 64)

	var allowedModelsJSON string
	if r.FormValue("allow_all") == "true" {
		allowedModelsJSON = `["*"]`
	} else {
		selectedModels := r.Form["models"]
		if len(selectedModels) == 0 {
			allowedModelsJSON = `["*"]`
		} else {
			b, _ := json.Marshal(selectedModels)
			allowedModelsJSON = string(b)
		}
	}

	_, err := h.users.UpdateUser(r.Context(), usecase.UpdateUserInput{
		ID:            userID,
		Name:          strings.TrimSpace(r.FormValue("name")),
		Username:      strings.TrimSpace(r.FormValue("username")),
		Role:          strings.TrimSpace(r.FormValue("role")),
		TokenQuota:    quota,
		AllowedModels: allowedModelsJSON,
		IsActive:      r.FormValue("is_active") == "true",
	})
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redirectURL+"?msg=User+updated+successfully", http.StatusSeeOther)
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	userID := chi.URLParam(r, "id")
	ctx := r.Context()
	redirectURL := safeRedirectURL(r.FormValue("redirect"), "/users")

	user, err := h.repo.GetUserByID(ctx, userID)
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error=User+not+found", http.StatusSeeOther)
		return
	}

	newPassword, err := h.users.ResetPassword(ctx, userID, r.FormValue("new_password"))
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Password for '%s' reset to: %s", user.Username, newPassword)
	http.Redirect(w, r, redirectURL+"?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (h *Handler) ResetUsage(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	ctx := r.Context()
	redirectURL := safeRedirectURL(r.URL.Query().Get("redirect"), "")
	if redirectURL == "" {
		redirectURL = safeRedirectURL(r.FormValue("redirect"), "/users")
	}

	if err := h.users.ResetUsage(ctx, userID); err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redirectURL+"?msg=Token+usage+counter+reset+to+0", http.StatusSeeOther)
}

func (h *Handler) ToggleUserStatus(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	ctx := r.Context()
	redirectURL := safeRedirectURL(r.URL.Query().Get("redirect"), "")
	if redirectURL == "" {
		redirectURL = safeRedirectURL(r.FormValue("redirect"), "/users")
	}

	newStatus, err := h.users.ToggleUserStatus(ctx, userID)
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	msg := "User suspended"
	if newStatus {
		msg = "User reactivated"
	}
	http.Redirect(w, r, redirectURL+"?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	ctx := r.Context()

	if err := h.users.DeleteUser(ctx, userID); err != nil {
		http.Redirect(w, r, "/users?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/users?msg=User+and+keys+deleted+successfully", http.StatusSeeOther)
}