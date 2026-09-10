package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"9router-gateway/internal/models"
)

func (h *Handler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := GetUserFromContext(ctx)

	timeframe := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("timeframe")))
	if timeframe == "" {
		timeframe = "1d"
	}

	var stats *models.DashboardStats
	var err error

	if user != nil && user.IsAdmin() {
		stats, err = h.repo.GetDashboardStats(ctx, timeframe)
	} else if user != nil {
		stats, err = h.repo.GetUserDashboardStats(ctx, user.ID, timeframe)
	}

	if err != nil {
		stats = nil
	}

	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, r, "dashboard.html", "base.html", map[string]interface{}{
		"ActivePage": "dashboard",
		"Stats":      stats,
		"Timeframe":  timeframe,
		"SuccessMsg": successMsg,
		"ErrorMsg":   errorMsg,
	})
}

func (h *Handler) APIStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := GetUserFromContext(ctx)

	timeframe := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("timeframe")))
	if timeframe == "" {
		timeframe = "1d"
	}

	var stats *models.DashboardStats
	var err error

	if user != nil && user.IsAdmin() {
		stats, err = h.repo.GetDashboardStats(ctx, timeframe)
	} else if user != nil {
		stats, err = h.repo.GetUserDashboardStats(ctx, user.ID, timeframe)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}
