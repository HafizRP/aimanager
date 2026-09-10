package handlers

import (
	"net/http"
	"strconv"

	"9router-gateway/internal/models"
)

func (h *Handler) LogsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	filterUser := r.URL.Query().Get("user_id")
	// If non-admin, force filter to their own ID only
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
