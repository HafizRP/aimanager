package handlers

import (
	"encoding/json"
	"net/http"
)

func (h *Handler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := h.repo.GetDashboardStats(ctx)
	if err != nil {
		stats = nil
	}

	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, "dashboard.html", "base.html", map[string]interface{}{
		"ActivePage": "dashboard",
		"Stats":      stats,
		"SuccessMsg": successMsg,
		"ErrorMsg":   errorMsg,
	})
}

func (h *Handler) APIStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := h.repo.GetDashboardStats(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}
