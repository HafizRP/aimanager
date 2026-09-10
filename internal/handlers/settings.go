package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"9router-gateway/internal/models"
	"9router-gateway/internal/upstream"
)

type ModelViewItem struct {
	UpstreamModelItem
	Quota upstream.ModelQuotaSummary `json:"quota"`
}

func (h *Handler) ModelsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	// Fetch detailed models from 9router
	allModels, _ := h.fetchUpstreamModels(ctx)

	// Filter for non-admin user
	displayModels := allModels
	if currentUser != nil && !currentUser.IsAdmin() {
		allowedList := models.ParseAllowedModels(currentUser.AllowedModels)
		if !models.HasWildcard(allowedList) {
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

	var users []models.User
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
		allowed := models.ParseAllowedModels(currentUser.AllowedModels)
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

func (h *Handler) UpdateMidtransPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil || !currentUser.IsAdmin() {
		http.Redirect(w, r, "/?error=Unauthorized", http.StatusSeeOther)
		return
	}

	serverKey := strings.TrimSpace(r.FormValue("server_key"))
	clientKey := strings.TrimSpace(r.FormValue("client_key"))
	merchantID := strings.TrimSpace(r.FormValue("merchant_id"))
	isProduction := (r.FormValue("is_production") == "true")

	if serverKey != "" {
		h.cfg.MidtransServerKey = serverKey
	}
	if clientKey != "" {
		h.cfg.MidtransClientKey = clientKey
	}
	h.cfg.MidtransMerchantID = merchantID
	h.cfg.MidtransIsProduction = isProduction

	_ = h.repo.SaveSetting(ctx, "midtrans_server_key", h.cfg.MidtransServerKey)
	_ = h.repo.SaveSetting(ctx, "midtrans_client_key", h.cfg.MidtransClientKey)
	_ = h.repo.SaveSetting(ctx, "midtrans_merchant_id", h.cfg.MidtransMerchantID)
	if isProduction {
		_ = h.repo.SaveSetting(ctx, "midtrans_is_production", "true")
	} else {
		_ = h.repo.SaveSetting(ctx, "midtrans_is_production", "false")
	}

	http.Redirect(w, r, "/settings?msg=Midtrans+configuration+saved+successfully", http.StatusSeeOther)
}

func (h *Handler) UpdateUpstreamPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil || !currentUser.IsAdmin() {
		http.Redirect(w, r, "/?error=Unauthorized", http.StatusSeeOther)
		return
	}

	upstreamURL := strings.TrimSpace(r.FormValue("upstream_url"))
	dbPath := strings.TrimSpace(r.FormValue("ninerouter_db_path"))
	apiKey := strings.TrimSpace(r.FormValue("upstream_api_key"))

	if upstreamURL == "" {
		http.Redirect(w, r, "/settings?error=Upstream+URL+cannot+be+empty", http.StatusSeeOther)
		return
	}
	if !strings.HasPrefix(upstreamURL, "http://") && !strings.HasPrefix(upstreamURL, "https://") {
		http.Redirect(w, r, "/settings?error=Invalid+upstream+URL+(must+start+with+http://+or+https://)", http.StatusSeeOther)
		return
	}
	upstreamURL = strings.TrimRight(upstreamURL, "/")

	h.cfg.SetUpstreamURL(upstreamURL)
	if err := h.repo.SaveSetting(ctx, "upstream_url", upstreamURL); err != nil {
		log.Error().Err(err).Msg("Failed to save upstream_url setting")
	}

	if dbPath != "" {
		h.cfg.SetNineRouterDBPath(dbPath)
		if err := h.repo.SaveSetting(ctx, "ninerouter_db_path", dbPath); err != nil {
			log.Error().Err(err).Msg("Failed to save ninerouter_db_path setting")
		}
		if h.syncer != nil {
			h.syncer.UpdateDBPath(dbPath)
			// Trigger re-sync of all active keys
			if allKeys, err := h.repo.GetAllAPIKeys(ctx); err == nil {
				_ = h.syncer.BackfillAll(allKeys)
			}
		}
	}

	h.cfg.SetUpstreamAPIKey(apiKey)
	if err := h.repo.SaveSetting(ctx, "upstream_api_key", apiKey); err != nil {
		log.Error().Err(err).Msg("Failed to save upstream_api_key setting")
	}

	if h.coreClient != nil {
		h.coreClient.ResetCLIToken()
	}

	log.Info().
		Str("upstream_url", upstreamURL).
		Str("db_path", dbPath).
		Msg("Updated upstream 9router Core settings in database and runtime config")

	http.Redirect(w, r, "/settings?msg=Upstream+9router+Core+configuration+updated+successfully", http.StatusSeeOther)
}

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
