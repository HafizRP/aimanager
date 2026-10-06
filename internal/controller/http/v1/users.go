package v1

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"9router-gateway/internal/entity"
	"9router-gateway/internal/proxy"
	"9router-gateway/internal/usecase"
)

var (
	nonAlphaNumericRegex = regexp.MustCompile(`[^a-z0-9]+`)
	validUsernameRegex   = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)
)

// UsersPage renders the user management page (admin).
func (h *Handler) UsersPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	users, err := h.repo.GetAllUsers(ctx)
	if err != nil {
		users = []entity.User{}
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

// UserDetailPage renders a single user's detail page (admin).
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
		keys = []entity.APIKey{}
	}
	for i := range keys {
		keys[i].UserName = user.Name
	}

	logs, totalLogs, err := h.repo.GetRequestLogs(ctx, 15, 0, userID, "", 0)
	if err != nil {
		logs = []entity.RequestLog{}
		totalLogs = 0
	}

	loginAudits, err := h.repo.GetUserLoginAudits(ctx, userID, 15)
	if err != nil {
		loginAudits = []entity.LoginAudit{}
	}

	stats, err := h.dash.GetStats(ctx, user, "30d")
	if err != nil {
		stats = &entity.DashboardStats{}
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
		"LoginAudits":       loginAudits,
		"Stats":             stats,
		"AvailableModels":   availableModels,
		"AllowedModelsList": allowedModelsList,
		"IsAllModels":       isAllModels,
		"SuccessMsg":        successMsg,
		"ErrorMsg":          errorMsg,
		"ClientIP":          proxy.GetClientIP(r),
	})
}

// CreateUser creates a user and optionally an API key (admin).
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

// EditUser updates a user's profile fields (admin).
func (h *Handler) EditUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	userID := chi.URLParam(r, "id")
	if userID == "" {
		userID = r.FormValue("id")
	}
	redirectURL := safeRedirectURL(r.FormValue("redirect"), "/users")

	quotaStr := r.FormValue("token_quota")
	quota, _ := strconv.ParseInt(quotaStr, 10, 64)
	dailyStr := r.FormValue("daily_token_quota")
	dailyQuota, _ := strconv.ParseInt(dailyStr, 10, 64)

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
		ID:              userID,
		Name:            strings.TrimSpace(r.FormValue("name")),
		Username:        strings.TrimSpace(r.FormValue("username")),
		Role:            strings.TrimSpace(r.FormValue("role")),
		TokenQuota:      quota,
		DailyTokenQuota: dailyQuota,
		AllowedModels:   allowedModelsJSON,
		IsActive:        r.FormValue("is_active") == "true",
	})
	if err != nil {
		http.Redirect(w, r, redirectURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redirectURL+"?msg=User+updated+successfully", http.StatusSeeOther)
}

// ResetPassword assigns a new password to a user (admin).
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

// ResetUsage zeroes a user's token usage counters (admin).
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

// ToggleUserStatus activates/deactivates a user account (admin).
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

// DeleteUser removes a user and their API keys (admin).
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	ctx := r.Context()

	if err := h.users.DeleteUser(ctx, userID); err != nil {
		http.Redirect(w, r, "/users?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/users?msg=User+and+keys+deleted+successfully", http.StatusSeeOther)
}

// APILoginAudits returns paginated login audits (admin sees all, regular user sees own).
func (h *Handler) APILoginAudits(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	filterUser := r.URL.Query().Get("user_id")
	if !currentUser.IsAdmin() {
		filterUser = currentUser.ID
	}
	filterStatus := r.URL.Query().Get("status")

	limitStr := r.URL.Query().Get("limit")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 50
	}
	offsetStr := r.URL.Query().Get("offset")
	offset, _ := strconv.Atoi(offsetStr)
	if offset < 0 {
		offset = 0
	}

	audits, total, err := h.repo.GetFilteredLoginAudits(ctx, filterUser, filterStatus, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"audits": audits,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ExportLoginAudits exports login audit logs as CSV or JSON.
func (h *Handler) ExportLoginAudits(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	filterUser := r.URL.Query().Get("user_id")
	if !currentUser.IsAdmin() {
		filterUser = currentUser.ID
	}
	filterStatus := r.URL.Query().Get("status")
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format != "json" {
		format = "csv"
	}

	// Fetch up to 5000 records for export
	audits, _, err := h.repo.GetFilteredLoginAudits(ctx, filterUser, filterStatus, 5000, 0)
	if err != nil {
		http.Error(w, "Failed to retrieve login audits", http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=login_audits_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(audits)
		return
	}

	// CSV format
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=login_audits_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{
		"ID", "Timestamp_WIB", "Username", "Account_Name", "Status", "Client_IP", "User_Agent", "Reason",
	})

	for _, a := range audits {
		timeWIB := a.CreatedAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		_ = writer.Write([]string{
			strconv.FormatInt(a.ID, 10),
			timeWIB,
			a.Username,
			a.UserName,
			a.Status,
			a.IP,
			a.UserAgent,
			a.Reason,
		})
	}
}

// APIUserLoginAudits returns recent login audits for a specific user (admin only).
func (h *Handler) APIUserLoginAudits(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	limitStr := r.URL.Query().Get("limit")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 15
	}

	audits, err := h.repo.GetUserLoginAudits(r.Context(), userID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, audits)
}

// ExportUsers exports users as CSV or JSON file (admin only).
func (h *Handler) ExportUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil || !currentUser.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	filterRole := strings.TrimSpace(r.URL.Query().Get("role"))
	filterStatus := strings.TrimSpace(r.URL.Query().Get("status"))
	filterQuota := strings.TrimSpace(r.URL.Query().Get("quota"))
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format != "json" {
		format = "csv"
	}

	users, err := h.repo.GetAllUsers(ctx)
	if err != nil {
		http.Error(w, "Failed to retrieve users", http.StatusInternalServerError)
		return
	}

	var filtered []entity.User
	for _, u := range users {
		if filterRole != "" && filterRole != "all" && !strings.EqualFold(u.Role, filterRole) {
			continue
		}
		if filterStatus == "active" && !u.IsActive {
			continue
		}
		if filterStatus == "suspended" && u.IsActive {
			continue
		}
		if filterQuota == "unlimited" && u.TokenQuota > 0 {
			continue
		}
		if filterQuota == "limited" && u.TokenQuota == 0 {
			continue
		}
		if filterQuota == "exhausted" && (u.TokenQuota == 0 || u.TokensUsed < u.TokenQuota) {
			continue
		}
		if filterQuota == "near_limit" && (u.TokenQuota == 0 || u.QuotaPercent() < 80.0 || (u.TokenQuota > 0 && u.TokensUsed >= u.TokenQuota)) {
			continue
		}
		filtered = append(filtered, u)
	}

	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=users_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(filtered)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=users_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{
		"ID", "Username", "Name", "Role", "Token_Quota", "Tokens_Used", "Daily_Token_Quota",
		"Rate_Limit_RPM", "Rate_Limit_TPM", "Key_Count", "Is_Active", "Allowed_Models",
		"Created_At_WIB", "Last_Login_At_WIB",
	})

	for _, u := range filtered {
		createdAtWIB := u.CreatedAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		lastLoginWIB := "-"
		if u.LastLoginAt != nil && !u.LastLoginAt.IsZero() {
			lastLoginWIB = u.LastLoginAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		}

		_ = writer.Write([]string{
			u.ID,
			u.Username,
			u.Name,
			u.Role,
			strconv.FormatInt(u.TokenQuota, 10),
			strconv.FormatInt(u.TokensUsed, 10),
			strconv.FormatInt(u.DailyTokenQuota, 10),
			strconv.Itoa(u.RateLimitRPM),
			strconv.FormatInt(u.RateLimitTPM, 10),
			strconv.Itoa(u.KeyCount),
			strconv.FormatBool(u.IsActive),
			u.AllowedModels,
			createdAtWIB,
			lastLoginWIB,
		})
	}
}