package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"9router-gateway/internal/models"
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

	stats, err := h.repo.GetUserDashboardStats(ctx, userID, "30d")
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
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/users?error=User+name+cannot+be+empty", http.StatusSeeOther)
		return
	}
	if len(name) > 100 {
		name = name[:100]
	}

	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	if username == "" {
		// Clean up name for username
		username = nonAlphaNumericRegex.ReplaceAllString(strings.ToLower(name), "")
		if username == "" {
			username = "user" + GenerateRandomPassword(4)
		}
	}

	if !validUsernameRegex.MatchString(username) {
		http.Redirect(w, r, "/users?error="+url.QueryEscape("Username must be 3-32 characters and contain only letters, numbers, hyphens, or underscores"), http.StatusSeeOther)
		return
	}

	// Check if username already exists
	ctx := r.Context()
	if existing, _ := h.repo.GetUserByUsername(ctx, username); existing != nil {
		http.Redirect(w, r, "/users?error="+url.QueryEscape("Username '"+username+"' is already taken"), http.StatusSeeOther)
		return
	}

	password := strings.TrimSpace(r.FormValue("password"))
	if password != "" && len(password) < 6 {
		http.Redirect(w, r, "/users?error="+url.QueryEscape("Password must be at least 6 characters"), http.StatusSeeOther)
		return
	}
	if password == "" {
		password = GenerateRandomPassword(8)
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		http.Redirect(w, r, "/users?error=Failed+to+hash+password", http.StatusSeeOther)
		return
	}

	role := strings.TrimSpace(r.FormValue("role"))
	if role != "admin" {
		role = "user"
	}

	quotaStr := r.FormValue("token_quota")
	quota, _ := strconv.ParseInt(quotaStr, 10, 64)
	if quota < 0 {
		quota = 0
	}

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
		Username:      username,
		Name:          name,
		PasswordHash:  passwordHash,
		Role:          role,
		TokenQuota:    quota,
		TokensUsed:    0,
		AllowedModels: allowedModelsJSON,
		IsActive:      true,
	}

	if err := h.repo.CreateUser(ctx, user); err != nil {
		http.Redirect(w, r, "/users?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
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
		if h.syncer != nil {
			_ = h.syncer.SyncKey(apiKey, user.Name)
		}
		msg := fmt.Sprintf("User '%s' created! Password: %s | Key: %s", username, password, generatedKey)
		http.Redirect(w, r, "/users?msg="+url.QueryEscape(msg), http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("User '%s' created successfully! Login Password: %s", username, password)
	http.Redirect(w, r, "/users?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (h *Handler) EditUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	userID := chi.URLParam(r, "id")
	if userID == "" {
		userID = r.FormValue("id")
	}
	redirectURL := safeRedirectURL(r.FormValue("redirect"), "/users")

	ctx := r.Context()
	user, err := h.repo.GetUserByID(ctx, userID)
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error=User+not+found", http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name != "" {
		if len(name) > 100 {
			name = name[:100]
		}
		user.Name = name
	}

	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	if username != "" && username != user.Username {
		if !validUsernameRegex.MatchString(username) {
			http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape("Username must be 3-32 characters and contain only letters, numbers, hyphens, or underscores"), http.StatusSeeOther)
			return
		}
		// Check uniqueness
		if existing, _ := h.repo.GetUserByUsername(ctx, username); existing != nil && existing.ID != user.ID {
			http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape("Username '"+username+"' is already taken"), http.StatusSeeOther)
			return
		}
		user.Username = username
	}

	role := strings.TrimSpace(r.FormValue("role"))
	if role == "admin" || role == "user" {
		user.Role = role
	}

	quotaStr := r.FormValue("token_quota")
	quota, _ := strconv.ParseInt(quotaStr, 10, 64)
	if quota < 0 {
		quota = 0
	}
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

	newPassword := strings.TrimSpace(r.FormValue("new_password"))
	if newPassword != "" && len(newPassword) < 6 {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape("New password must be at least 6 characters"), http.StatusSeeOther)
		return
	}
	if newPassword == "" {
		newPassword = GenerateRandomPassword(8)
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error=Failed+to+hash+password", http.StatusSeeOther)
		return
	}

	if err := h.repo.UpdateUserPassword(ctx, userID, hash); err != nil {
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

	if err := h.repo.ResetUserUsage(ctx, userID); err != nil {
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

	user, err := h.repo.GetUserByID(ctx, userID)
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error=User+not+found", http.StatusSeeOther)
		return
	}

	newStatus := !user.IsActive
	if err := h.repo.ToggleUserStatus(ctx, userID, newStatus); err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Also toggle status of user keys in 9router
	if h.syncer != nil {
		keys, _ := h.repo.GetAPIKeysByUserID(ctx, userID)
		for _, k := range keys {
			_ = h.syncer.ToggleKey(k.ID, newStatus)
		}
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

	// Get keys before deleting to remove from 9router
	keys, _ := h.repo.GetAPIKeysByUserID(ctx, userID)

	if err := h.repo.DeleteUser(ctx, userID); err != nil {
		http.Redirect(w, r, "/users?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if h.syncer != nil {
		for _, k := range keys {
			_ = h.syncer.DeleteKey(k.ID)
		}
	}

	http.Redirect(w, r, "/users?msg=User+and+keys+deleted+successfully", http.StatusSeeOther)
}

func GenerateRandomPassword(length int) string {
	b := make([]byte, length/2+1)
	_, _ = rand.Read(b)
	str := hex.EncodeToString(b)
	if len(str) > length {
		return str[:length]
	}
	return str
}
