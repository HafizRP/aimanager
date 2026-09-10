package v1

import (
	"encoding/json"
	"net/http"
	"strings"

	"9router-gateway/internal/upstream"
)

// DashboardPage renders the stats dashboard for the current user.
func (h *Handler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := GetUserFromContext(ctx)

	timeframe := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("timeframe")))
	if timeframe == "" {
		timeframe = "1d"
	}

	stats, err := h.dash.GetStats(ctx, user, timeframe)
	if err != nil {
		stats = nil
	}

	var quotaReport *upstream.UpstreamQuotaReport
	if h.quotaManager != nil {
		quotaReport, _ = h.quotaManager.FetchAllQuotas(ctx, r.URL.Query().Get("refresh") == "true")
	}

	successMsg := r.URL.Query().Get("msg")
	errorMsg := r.URL.Query().Get("error")

	h.render(w, r, "dashboard.html", "base.html", map[string]interface{}{
		"ActivePage":  "dashboard",
		"Stats":       stats,
		"Timeframe":   timeframe,
		"QuotaReport": quotaReport,
		"SuccessMsg":  successMsg,
		"ErrorMsg":    errorMsg,
	})
}

// APIStats returns dashboard statistics as JSON (role-scoped).
func (h *Handler) APIStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := GetUserFromContext(ctx)

	timeframe := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("timeframe")))
	if timeframe == "" {
		timeframe = "1d"
	}

	stats, err := h.dash.GetStats(ctx, user, timeframe)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// APICacheStats returns or clears response cache analytics.
func (h *Handler) APICacheStats(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		http.Error(w, "Cache not initialized", http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h.cache.Stats())
	case http.MethodDelete:
		h.cache.ResetStats()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "message": "Cache analytics reset"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// CacheAnalyticsPage renders the cache analytics page.
func (h *Handler) CacheAnalyticsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "cache_analytics.html", "base.html", map[string]interface{}{
		"ActivePage": "cache-analytics",
	})
}

// APIUpstreamQuotas returns upstream quota summaries as JSON.
func (h *Handler) APIUpstreamQuotas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	force := r.URL.Query().Get("refresh") == "true"
	if h.quotaManager == nil {
		http.Error(w, "Quota manager not initialized", http.StatusInternalServerError)
		return
	}
	report, err := h.quotaManager.FetchAllQuotas(ctx, force)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}