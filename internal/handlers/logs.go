package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"9router-gateway/internal/models"
)

func (h *Handler) LogsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	filterUser := r.URL.Query().Get("user_id")
	if currentUser != nil && !currentUser.IsAdmin() {
		filterUser = currentUser.ID
	}

	filterModel := r.URL.Query().Get("model")
	statusStr := r.URL.Query().Get("status")
	filterStatus, _ := strconv.Atoi(statusStr)

	cursor := r.URL.Query().Get("cursor")
	dir := r.URL.Query().Get("dir")
	if dir != "prev" && dir != "next" {
		dir = "next"
	}

	limit := 25

	logs, pageInfo, err := h.repo.GetRequestLogsCursor(ctx, limit, cursor, dir, filterUser, filterModel, filterStatus)
	if err != nil {
		logs = nil
		pageInfo = &models.CursorPageInfo{Limit: limit}
	}

	var users []models.User
	if currentUser != nil && currentUser.IsAdmin() {
		users, _ = h.repo.GetAllUsers(ctx)
	}

	h.render(w, r, "logs.html", "base.html", map[string]interface{}{
		"ActivePage":   "logs",
		"Logs":         logs,
		"Users":        users,
		"FilterUser":   filterUser,
		"FilterModel":  filterModel,
		"FilterStatus": filterStatus,
		"PageInfo":     pageInfo,
	})
}

// APILogs returns the latest logs as JSON for AJAX polling
func (h *Handler) APILogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	filterUser := r.URL.Query().Get("user_id")
	if currentUser != nil && !currentUser.IsAdmin() {
		filterUser = currentUser.ID
	}

	filterModel := r.URL.Query().Get("model")
	statusStr := r.URL.Query().Get("status")
	filterStatus, _ := strconv.Atoi(statusStr)

	cursor := r.URL.Query().Get("cursor")
	dir := r.URL.Query().Get("dir")
	if dir != "prev" && dir != "next" {
		dir = "next"
	}

	limit := 25

	logs, pageInfo, err := h.repo.GetRequestLogsCursor(ctx, limit, cursor, dir, filterUser, filterModel, filterStatus)
	if err != nil {
		logs = nil
		pageInfo = &models.CursorPageInfo{Limit: limit}
	}

	type logRow struct {
		ID               int64   `json:"id"`
		UserName         string  `json:"user_name"`
		KeyName          string  `json:"key_name"`
		Model            string  `json:"model"`
		Method           string  `json:"method"`
		Path             string  `json:"path"`
		IsStream         bool    `json:"is_stream"`
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalTokens      int     `json:"total_tokens"`
		StatusCode       int     `json:"status_code"`
		DurationMs       int64   `json:"duration_ms"`
		ClientIP         string  `json:"client_ip"`
		ErrorMessage     string  `json:"error_message"`
		CreatedAt        string  `json:"created_at"`
	}

	rows := make([]logRow, 0, len(logs))
	for _, l := range logs {
		rows = append(rows, logRow{
			ID:               l.ID,
			UserName:         l.UserName,
			KeyName:          l.KeyName,
			Model:            l.Model,
			Method:           l.Method,
			Path:             l.Path,
			IsStream:         l.IsStream,
			PromptTokens:     l.PromptTokens,
			CompletionTokens: l.CompletionTokens,
			TotalTokens:      l.TotalTokens,
			StatusCode:       l.StatusCode,
			DurationMs:       l.DurationMs,
			ClientIP:         l.ClientIP,
			ErrorMessage:     l.ErrorMessage,
			CreatedAt:        l.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"logs":      rows,
		"page_info": pageInfo,
	})
}
