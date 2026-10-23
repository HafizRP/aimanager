package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"9router-gateway/internal/entity"
)

// retentionSettingsKey is the gateway.db settings key holding the retention window in days.
const retentionSettingsKey = "log_retention_days"

// defaultRetentionDays is used when the setting is unset. 0 disables auto-purge.
const defaultRetentionDays = 7

// APILogRetention reports the current retention window, row counts, and storage estimate.
func (h *Handler) APILogRetention(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	days := h.retentionDays(ctx)
	count, _ := h.logs.Count(ctx)

	var dbSizeBytes int64
	if fi, err := os.Stat(h.cfg.DBPath); err == nil {
		dbSizeBytes = fi.Size()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"retention_days": days,
		"total_logs":     count,
		"db_size_bytes":  dbSizeBytes,
	})
}

// APILogRetentionSave persists the retention window (days; 0 disables auto-purge).
func (h *Handler) APILogRetentionSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var body struct {
		RetentionDays *int `json:"retention_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		// Fall back to form encoding for non-JS submissions.
		_ = r.ParseForm()
		if v := strings.TrimSpace(r.FormValue("retention_days")); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				body.RetentionDays = &n
			}
		}
	}

	if body.RetentionDays == nil {
		http.Error(w, `{"error":"retention_days is required"}`, http.StatusBadRequest)
		return
	}
	days := *body.RetentionDays
	if days < 0 || days > 3650 {
		http.Error(w, `{"error":"retention_days must be between 0 and 3650"}`, http.StatusBadRequest)
		return
	}

	if err := h.repo.SaveSetting(ctx, retentionSettingsKey, strconv.Itoa(days)); err != nil {
		http.Error(w, `{"error":"failed to persist retention setting"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "retention_days": days})
}

// APILogsPurge runs an immediate retention sweep. An optional `days` body/query
// value overrides the stored window for this run only.
func (h *Handler) APILogsPurge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	days := h.retentionDays(ctx)
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 3650 {
			days = n
		}
	}

	result, err := h.logs.PurgeBefore(ctx, days)
	if err != nil {
		http.Error(w, `{"error":"purge failed"}`, http.StatusInternalServerError)
		return
	}

	if result.RequestLogs > 0 || result.SecurityEvents > 0 || result.LoginAudits > 0 {
		_ = h.repo.CreateSecurityEvent(ctx, &entity.SecurityEvent{
			Kind:   "log_retention",
			Detail: "Manual retention sweep removed expired audit rows",
			Action: "purged request_logs/security_events/login_audits",
		})
	}

	remaining, _ := h.logs.Count(ctx)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":              true,
		"retention_days":  days,
		"request_logs":    result.RequestLogs,
		"security_events": result.SecurityEvents,
		"login_audits":    result.LoginAudits,
		"remaining_logs":  remaining,
	})
}

// retentionDays resolves the stored retention window with the default fallback.
func (h *Handler) retentionDays(ctx context.Context) int {
	raw := h.repo.GetSettingDefault(ctx, retentionSettingsKey, "")
	if raw == "" {
		return defaultRetentionDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultRetentionDays
	}
	return n
}
