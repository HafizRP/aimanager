package v1

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"9router-gateway/internal/entity"
	"9router-gateway/internal/upstream"
	"9router-gateway/internal/usecase"
)

// ModelViewItem is a model descriptor for the models page.
type ModelViewItem struct {
	UpstreamModelItem
	Quota upstream.ModelQuotaSummary `json:"quota"`
}

// ModelsPage renders the model whitelist/alias settings page.
func (h *Handler) ModelsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	// Fetch detailed models from 9router
	allModels, _ := h.fetchUpstreamModels(ctx)

	// Filter for non-admin user
	displayModels := allModels
	if currentUser != nil && !currentUser.IsAdmin() {
		allowedList := entity.ParseAllowedModels(currentUser.AllowedModels)
		if !entity.HasWildcard(allowedList) {
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

	// Fetch upstream quota report
	var quotaReport *upstream.UpstreamQuotaReport
	if h.quotaManager != nil {
		quotaReport, _ = h.quotaManager.FetchAllQuotas(ctx, r.URL.Query().Get("refresh") == "true")
	}

	var modelViews []ModelViewItem
	for _, m := range displayModels {
		item := ModelViewItem{
			UpstreamModelItem: m,
		}
		if h.quotaManager != nil && quotaReport != nil {
			item.Quota = h.quotaManager.GetModelSummary(m.ID, quotaReport)
		}
		modelViews = append(modelViews, item)
	}

	var users []entity.User
	if currentUser != nil && currentUser.IsAdmin() {
		users, _ = h.repo.GetAllUsers(ctx)
	}

	aliases, _ := h.coreClient.GetModelAliases(ctx)

	h.render(w, r, "models.html", "base.html", map[string]interface{}{
		"ActivePage":  "models",
		"Models":      modelViews,
		"Users":       users,
		"QuotaReport": quotaReport,
		"Aliases":     aliases,
		"UpstreamURL": h.cfg.GetUpstreamURL(),
	})
}

// ExportModels streams the model catalog with real-time quota status as CSV or JSON file
func (h *Handler) ExportModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	// Fetch detailed models from 9router
	allModels, err := h.fetchUpstreamModels(ctx)
	if err != nil {
		allModels = []UpstreamModelItem{}
	}

	// Filter for non-admin user
	displayModels := allModels
	if currentUser != nil && !currentUser.IsAdmin() {
		allowedList := entity.ParseAllowedModels(currentUser.AllowedModels)
		if !entity.HasWildcard(allowedList) {
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

	// Fetch upstream quota report
	var quotaReport *upstream.UpstreamQuotaReport
	if h.quotaManager != nil {
		quotaReport, _ = h.quotaManager.FetchAllQuotas(ctx, false)
	}

	var users []entity.User
	if currentUser != nil && currentUser.IsAdmin() {
		users, _ = h.repo.GetAllUsers(ctx)
	}

	type modelExportRow struct {
		ID               string  `json:"id"`
		Provider         string  `json:"provider"`
		Status           string  `json:"status"`
		HasQuota         bool    `json:"has_quota"`
		AvailableValue   string  `json:"available_value"`
		AvailablePercent float64 `json:"available_percent"`
		ReadyAccounts    int     `json:"ready_accounts"`
		TotalAccounts    int     `json:"total_accounts"`
		NearestResetWIB  string  `json:"nearest_reset_wib"`
		NearestResetIn   string  `json:"nearest_reset_in"`
		WhitelistedUsers int     `json:"whitelisted_users"`
	}

	rows := make([]modelExportRow, 0, len(displayModels))
	for _, m := range displayModels {
		var q upstream.ModelQuotaSummary
		if h.quotaManager != nil && quotaReport != nil {
			q = h.quotaManager.GetModelSummary(m.ID, quotaReport)
		}

		status := "Ready"
		availVal := "Uncapped"
		if q.HasQuota {
			switch q.Status {
			case "exhausted":
				status = "Limit Reached"
			case "partial":
				status = "Partial Ready"
			default:
				status = "Ready"
			}
			if q.Provider == "kiro" {
				availVal = fmt.Sprintf("%.1f/%.1f Cr", q.BestRemaining, q.BestTotal)
			} else {
				availVal = fmt.Sprintf("%.1f%%", q.BestPercentage)
			}
		}

		whitelistedCount := 0
		if len(users) > 0 {
			for _, u := range users {
				if strings.Contains(u.AllowedModels, "*") || strings.Contains(u.AllowedModels, m.ID) {
					whitelistedCount++
				}
			}
		}

		rows = append(rows, modelExportRow{
			ID:               m.ID,
			Provider:         m.OwnedBy,
			Status:           status,
			HasQuota:         q.HasQuota,
			AvailableValue:   availVal,
			AvailablePercent: q.BestPercentage,
			ReadyAccounts:    q.ReadyAccounts,
			TotalAccounts:    q.TotalAccounts,
			NearestResetWIB:  q.NearestResetWIB,
			NearestResetIn:   q.NearestResetIn,
			WhitelistedUsers: whitelistedCount,
		})
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=models_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(rows)
		return
	}

	// CSV format default
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=models_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{
		"Model_ID", "Provider", "Status", "Available_Value", "Ready_Accounts", "Total_Accounts", "Nearest_Reset_WIB", "Nearest_Reset_In", "Whitelisted_Users",
	})

	for _, r := range rows {
		_ = writer.Write([]string{
			r.ID,
			r.Provider,
			r.Status,
			r.AvailableValue,
			strconv.Itoa(r.ReadyAccounts),
			strconv.Itoa(r.TotalAccounts),
			r.NearestResetWIB,
			r.NearestResetIn,
			strconv.Itoa(r.WhitelistedUsers),
		})
	}
}

// SettingsPage renders the gateway settings page.
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
			newKey := usecase.GenerateSecureAPIKey(prefix)
			apiKey := &entity.APIKey{
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
		userKey = "«redacted:sk-…»"
	}

	userModel := "ag/gemini-3.8-flash-high"
	if currentUser != nil {
		allowed := entity.ParseAllowedModels(currentUser.AllowedModels)
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

// UpdatePasswordPost changes the current user's password.
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
	if !h.auth.VerifySessionPassword(currentUser, currentPass) {
		http.Redirect(w, r, "/settings?error=Current+password+does+not+match", http.StatusSeeOther)
		return
	}

	newHash, err := usecase.HashPassword(newPass)
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

// UpdateMidtransPost saves Midtrans gateway keys (admin).
func (h *Handler) UpdateMidtransPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil || !currentUser.IsAdmin() {
		http.Redirect(w, r, "/?error=Unauthorized", http.StatusSeeOther)
		return
	}

	err := h.settings.UpdateMidtrans(
		ctx,
		strings.TrimSpace(r.FormValue("server_key")),
		strings.TrimSpace(r.FormValue("client_key")),
		strings.TrimSpace(r.FormValue("merchant_id")),
		r.FormValue("is_production") == "true",
	)
	if err != nil {
		log.Error().Err(err).Msg("Failed to save Midtrans settings")
		http.Redirect(w, r, "/settings?error=Failed+to+save+Midtrans+configuration", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?msg=Midtrans+configuration+saved+successfully", http.StatusSeeOther)
}

// UpdateUpstreamPost saves the 9router Core upstream settings (admin).
func (h *Handler) UpdateUpstreamPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil || !currentUser.IsAdmin() {
		http.Redirect(w, r, "/?error=Unauthorized", http.StatusSeeOther)
		return
	}

	err := h.settings.UpdateUpstream(
		ctx,
		strings.TrimSpace(r.FormValue("upstream_url")),
		strings.TrimSpace(r.FormValue("ninerouter_db_path")),
		strings.TrimSpace(r.FormValue("upstream_api_key")),
	)
	if err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if h.coreClient != nil {
		h.coreClient.ResetCLIToken()
	}

	log.Info().
		Str("upstream_url", h.cfg.GetUpstreamURL()).
		Str("db_path", h.cfg.GetNineRouterDBPath()).
		Msg("Updated upstream 9router Core settings in database and runtime config")

	http.Redirect(w, r, "/settings?msg=Upstream+9router+Core+configuration+updated+successfully", http.StatusSeeOther)
}

// TestUpstreamConnection pings the configured upstream and reports latency.
func (h *Handler) TestUpstreamConnection(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	currentUser := GetUserFromContext(ctx)
	if currentUser == nil || !currentUser.IsAdmin() {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Test provided candidate URL or fallback to configured UpstreamURL
	targetURL := h.cfg.GetUpstreamURL()
	if candidate := strings.TrimSpace(r.URL.Query().Get("url")); candidate != "" {
		if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
			targetURL = strings.TrimRight(candidate, "/")
		}
	}

	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid target URL scheme (must be http or https)",
			"target":  targetURL,
		})
		return
	}

	start := time.Now()

	// Test connection directly to /v1/models endpoint
	checkURL := targetURL + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"target":  targetURL,
		})
		return
	}

	resp, err := h.httpClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"target":  targetURL,
		})
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     resp.StatusCode < 500,
		"status_code": resp.StatusCode,
		"latency_ms":  latency,
		"endpoint":    checkURL,
		"target":      targetURL,
	})
}

// GetTheme returns the persisted UI theme (dark|light) for the current user.
func (h *Handler) GetTheme(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"theme": h.repo.GetSettingDefault(r.Context(), "ui_theme", "dark"),
	})
}

// SetTheme persists the UI theme preference (dark|light) for the current user.
func (h *Handler) SetTheme(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	theme := strings.TrimSpace(r.FormValue("theme"))
	if theme != "light" && theme != "dark" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "theme must be 'dark' or 'light'"})
		return
	}
	if err := h.repo.SaveSetting(r.Context(), "ui_theme", theme); err != nil {
		log.Error().Err(err).Msg("Failed to save UI theme setting")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "failed to save theme"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "theme": theme})
}