package handlers

import (
	"encoding/json"
	"net/http"

	"9router-gateway/internal/models"
)

func (h *Handler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := GetUserFromContext(ctx)

	var stats *models.DashboardStats
	var err error

	if user != nil && user.IsAdmin() {
		stats, err = h.repo.GetDashboardStats(ctx)
	} else if user != nil {
		stats, err = h.repo.GetUserDashboardStats(ctx, user.ID)
	}

	if err != nil {
		stats = nil
	}

	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, r, "dashboard.html", "base.html", map[string]interface{}{
		"ActivePage": "dashboard",
		"Stats":      stats,
		"SuccessMsg": successMsg,
		"ErrorMsg":   errorMsg,
	})
}

func (h *Handler) APIStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := GetUserFromContext(ctx)

	var stats *models.DashboardStats
	var err error

	if user != nil && user.IsAdmin() {
		stats, err = h.repo.GetDashboardStats(ctx)
	} else if user != nil {
		stats, err = h.repo.GetUserDashboardStats(ctx, user.ID)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}
