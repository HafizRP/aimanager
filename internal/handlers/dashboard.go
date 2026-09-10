package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"9router-gateway/internal/models"
)

func parseTimeframeDays(tf string) int {
	switch strings.ToLower(strings.TrimSpace(tf)) {
	case "7d", "7":
		return 7
	case "30d", "30":
		return 30
	case "90d", "90":
		return 90
	default:
		return 14
	}
}

func (h *Handler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := GetUserFromContext(ctx)

	timeframe := r.URL.Query().Get("timeframe")
	days := parseTimeframeDays(timeframe)

	var stats *models.DashboardStats
	var err error

	if user != nil && user.IsAdmin() {
		stats, err = h.repo.GetDashboardStats(ctx, days)
	} else if user != nil {
		stats, err = h.repo.GetUserDashboardStats(ctx, user.ID, days)
	}

	if err != nil {
		stats = nil
	}

	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, r, "dashboard.html", "base.html", map[string]interface{}{
		"ActivePage": "dashboard",
		"Stats":      stats,
		"Timeframe":  days,
		"SuccessMsg": successMsg,
		"ErrorMsg":   errorMsg,
	})
}

func (h *Handler) APIStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := GetUserFromContext(ctx)

	timeframe := r.URL.Query().Get("timeframe")
	days := parseTimeframeDays(timeframe)

	var stats *models.DashboardStats
	var err error

	if user != nil && user.IsAdmin() {
		stats, err = h.repo.GetDashboardStats(ctx, days)
	} else if user != nil {
		stats, err = h.repo.GetUserDashboardStats(ctx, user.ID, days)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}
