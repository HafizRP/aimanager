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

	pageStr := r.URL.Query().Get("page")
	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}

	limit := 30
	offset := (page - 1) * limit

	logs, total, err := h.repo.GetRequestLogs(ctx, limit, offset, filterUser, filterModel, filterStatus)
	if err != nil {
		logs = nil
		total = 0
	}

	var users []models.User
	if currentUser != nil && currentUser.IsAdmin() {
		users, _ = h.repo.GetAllUsers(ctx)
	}

	hasNext := (offset + len(logs)) < total

	h.render(w, r, "logs.html", "base.html", map[string]interface{}{
		"ActivePage":   "logs",
		"Logs":         logs,
		"Users":        users,
		"FilterUser":   filterUser,
		"FilterModel":  filterModel,
		"FilterStatus": filterStatus,
		"CurrentPage":  page,
		"TotalCount":   total,
		"HasNextPage":  hasNext,
	})
}
