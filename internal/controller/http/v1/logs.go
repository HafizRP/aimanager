package v1

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"9router-gateway/internal/entity"
	"9router-gateway/internal/usecase"
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

	logs, pageInfo, err := h.logs.List(ctx, usecase.LogQuery{
		Limit:        25,
		Cursor:       r.URL.Query().Get("cursor"),
		Direction:    r.URL.Query().Get("dir"),
		UserID:       filterUser,
		ModelFilter:  filterModel,
		StatusFilter: filterStatus,
	})
	if err != nil {
		logs = nil
		pageInfo = &entity.CursorPageInfo{Limit: 25}
	}

	var users []entity.User
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

	logs, pageInfo, err := h.logs.List(ctx, usecase.LogQuery{
		Limit:        25,
		Cursor:       r.URL.Query().Get("cursor"),
		Direction:    r.URL.Query().Get("dir"),
		UserID:       filterUser,
		ModelFilter:  filterModel,
		StatusFilter: filterStatus,
	})
	if err != nil {
		logs = nil
		pageInfo = &entity.CursorPageInfo{Limit: 25}
	}

	type logRow struct {
		ID               int64  `json:"id"`
		UserName         string `json:"user_name"`
		KeyName          string `json:"key_name"`
		Model            string `json:"model"`
		Method           string `json:"method"`
		Path             string `json:"path"`
		IsStream         bool   `json:"is_stream"`
		PromptTokens     int    `json:"prompt_tokens"`
		CompletionTokens int    `json:"completion_tokens"`
		TotalTokens      int    `json:"total_tokens"`
		StatusCode       int    `json:"status_code"`
		DurationMs       int64  `json:"duration_ms"`
		ClientIP         string `json:"client_ip"`
		ErrorMessage     string `json:"error_message"`
		CreatedAt        string `json:"created_at"`
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

// ExportLogs streams request logs as CSV or JSON file
func (h *Handler) ExportLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	filterUser := r.URL.Query().Get("user_id")
	if currentUser != nil && !currentUser.IsAdmin() {
		filterUser = currentUser.ID
	}

	filterModel := r.URL.Query().Get("model")
	statusStr := r.URL.Query().Get("status")
	filterStatus, _ := strconv.Atoi(statusStr)

	format := strings.ToLower(r.URL.Query().Get("format"))
	if format != "json" {
		format = "csv"
	}

	// Fetch up to 1000 logs for export
	logs, _, err := h.logs.List(ctx, usecase.LogQuery{
		Limit:        1000,
		Cursor:       "",
		Direction:    "next",
		UserID:       filterUser,
		ModelFilter:  filterModel,
		StatusFilter: filterStatus,
	})
	if err != nil {
		http.Error(w, "Failed to retrieve logs", http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=request_logs_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(logs)
		return
	}

	// CSV format
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=request_logs_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write CSV Header
	_ = writer.Write([]string{
		"ID", "Timestamp_WIB", "User", "API_Key", "Model", "Method", "Path",
		"Stream", "Prompt_Tokens", "Completion_Tokens", "Total_Tokens",
		"Status_Code", "Duration_MS", "Client_IP", "Error_Message",
	})

	for _, l := range logs {
		timeWIB := l.CreatedAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		_ = writer.Write([]string{
			strconv.FormatInt(l.ID, 10),
			timeWIB,
			l.UserName,
			l.KeyName,
			l.Model,
			l.Method,
			l.Path,
			strconv.FormatBool(l.IsStream),
			strconv.Itoa(l.PromptTokens),
			strconv.Itoa(l.CompletionTokens),
			strconv.Itoa(l.TotalTokens),
			strconv.Itoa(l.StatusCode),
			strconv.FormatInt(l.DurationMs, 10),
			l.ClientIP,
			l.ErrorMessage,
		})
	}
}