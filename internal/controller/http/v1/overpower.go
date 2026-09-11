package v1

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Overpower endpoints: upstream health, anomaly radar, key budget management.
// All admin-only.

// APIUpstreamHealth returns the rolling upstream reliability snapshot.
func (h *Handler) APIUpstreamHealth(w http.ResponseWriter, r *http.Request) {
	if h.gwProxy == nil {
		http.Error(w, "proxy not attached", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, h.gwProxy.Health().Snapshot())
}

// RadarPage renders recent anomaly/self-heal/budget findings.
func (h *Handler) RadarPage(w http.ResponseWriter, r *http.Request) {
	events, err := h.repo.ListSecurityEvents(r.Context(), 100)
	if err != nil {
		events = nil
	}
	var health interface{}
	if h.gwProxy != nil {
		health = h.gwProxy.Health().Snapshot()
	}
	h.render(w, r, "radar.html", "base.html", map[string]interface{}{
		"ActivePage": "radar",
		"Events":     events,
		"Health":     health,
	})
}

// APIRadarEvents returns recent findings as JSON.
func (h *Handler) APIRadarEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	events, err := h.repo.ListSecurityEvents(r.Context(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, events)
}

// APIKeyBudget updates a key's lifetime + daily budgets.
func (h *Handler) APIKeyBudget(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		MaxTokensLimit  int `json:"max_tokens_limit"`
		DailyTokenQuota int `json:"daily_token_quota"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if payload.MaxTokensLimit < 0 {
		payload.MaxTokensLimit = 0
	}
	if payload.DailyTokenQuota < 0 {
		payload.DailyTokenQuota = 0
	}
	if err := h.repo.UpdateKeyBudgets(r.Context(), chi.URLParam(r, "id"), payload.MaxTokensLimit, payload.DailyTokenQuota); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}
