package v1

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// APIRadarEvents returns recent findings as JSON (optionally filtered by kind).
func (h *Handler) APIRadarEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	events, err := h.repo.ListFilteredSecurityEvents(r.Context(), kind, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, events)
}

// APIRadarExport exports security and anomaly findings as CSV or JSON.
func (h *Handler) APIRadarExport(w http.ResponseWriter, r *http.Request) {
	limit := 500
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format != "json" {
		format = "csv"
	}

	events, err := h.repo.ListFilteredSecurityEvents(r.Context(), kind, limit)
	if err != nil {
		http.Error(w, "Failed to retrieve security events: "+err.Error(), http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=radar_findings_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(events)
		return
	}

	// CSV format
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=radar_findings_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{
		"ID", "Timestamp_WIB", "Kind", "User_ID", "API_Key_ID", "Detail", "Action",
	})

	for _, ev := range events {
		timeWIB := ev.CreatedAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		_ = writer.Write([]string{
			strconv.FormatInt(ev.ID, 10),
			timeWIB,
			ev.Kind,
			ev.UserID,
			ev.APIKeyID,
			ev.Detail,
			ev.Action,
		})
	}
}

// APIKeyBudget updates a key's lifetime + daily budgets and allowed IP restrictions.
func (h *Handler) APIKeyBudget(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		MaxTokensLimit  int    `json:"max_tokens_limit"`
		DailyTokenQuota int    `json:"daily_token_quota"`
		AllowedIPs      string `json:"allowed_ips"`
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
	payload.AllowedIPs = strings.TrimSpace(payload.AllowedIPs)
	if err := h.repo.UpdateKeyRestrictions(r.Context(), chi.URLParam(r, "id"), payload.MaxTokensLimit, payload.DailyTokenQuota, payload.AllowedIPs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

// APICircuitBreakers returns all registered upstream circuit breaker snapshots.
func (h *Handler) APICircuitBreakers(w http.ResponseWriter, r *http.Request) {
	if h.gwProxy == nil || h.gwProxy.CircuitBreakers() == nil {
		writeJSON(w, map[string]interface{}{"breakers": map[string]interface{}{}, "total": 0})
		return
	}
	snaps := h.gwProxy.CircuitBreakers().Snapshots()
	writeJSON(w, map[string]interface{}{
		"breakers": snaps,
		"total":    len(snaps),
	})
}

// APICircuitBreakerReset resets a specific circuit breaker by name.
func (h *Handler) APICircuitBreakerReset(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "circuit breaker name required", http.StatusBadRequest)
		return
	}
	if h.gwProxy == nil || h.gwProxy.CircuitBreakers() == nil {
		http.Error(w, "proxy not attached", http.StatusServiceUnavailable)
		return
	}
	ok := h.gwProxy.CircuitBreakers().Reset(name)
	if !ok {
		http.Error(w, "circuit breaker not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "name": name, "message": "Circuit breaker reset to Closed state"})
}

// APICircuitBreakersResetAll resets all registered circuit breakers.
func (h *Handler) APICircuitBreakersResetAll(w http.ResponseWriter, r *http.Request) {
	if h.gwProxy == nil || h.gwProxy.CircuitBreakers() == nil {
		http.Error(w, "proxy not attached", http.StatusServiceUnavailable)
		return
	}
	h.gwProxy.CircuitBreakers().ResetAll()
	writeJSON(w, map[string]interface{}{"ok": true, "message": "All circuit breakers reset to Closed state"})
}

