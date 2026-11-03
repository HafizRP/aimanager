// Package worker runs background jobs such as periodic provider health checks, circuit breaker monitoring, and log retention.
package worker

import (
	"context"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"9router-gateway/internal/entity"
	"9router-gateway/internal/usecase"
)

// LogRetentionStore is the persistence surface the retention worker needs.
type LogRetentionStore interface {
	GetSettingDefault(ctx context.Context, key, fallback string) string
	CountRequestLogs(ctx context.Context) (int64, error)
	PurgeRequestLogsBefore(ctx context.Context, before time.Time) (int64, error)
	PurgeSecurityEventsBefore(ctx context.Context, before time.Time) (int64, error)
	PurgeLoginAuditsBefore(ctx context.Context, before time.Time) (int64, error)
	CreateSecurityEvent(ctx context.Context, ev *entity.SecurityEvent) error
}

// LogRetention periodically prunes request logs, security events, and login audits
// older than the configured retention window, keeping gateway.db from growing unbounded.
type LogRetention struct {
	store         LogRetentionStore
	interval      time.Duration
	defaultDays   int
	stopCh        chan struct{}
	lastPurgeInfo usecase.PurgeResult
}

// NewLogRetention builds a retention worker with sane defaults (7-day retention, 6h sweep).
func NewLogRetention(store LogRetentionStore) *LogRetention {
	return &LogRetention{
		store:       store,
		interval:    6 * time.Hour,
		defaultDays: 7,
		stopCh:      make(chan struct{}),
	}
}

// SetInterval overrides the sweep interval (mainly for tests).
func (lr *LogRetention) SetInterval(d time.Duration) {
	if d > 0 {
		lr.interval = d
	}
}

// RetentionDays reads the configured window, falling back to the worker default.
func (lr *LogRetention) RetentionDays(ctx context.Context) int {
	raw := lr.store.GetSettingDefault(ctx, "log_retention_days", "")
	if raw == "" {
		return lr.defaultDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return lr.defaultDays
	}
	return n
}

// RunOnce performs a single retention sweep and returns rows removed.
func (lr *LogRetention) RunOnce(ctx context.Context) (usecase.PurgeResult, error) {
	var out usecase.PurgeResult
	days := lr.RetentionDays(ctx)
	if days <= 0 {
		return out, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)

	var err error
	if out.RequestLogs, err = lr.store.PurgeRequestLogsBefore(ctx, cutoff); err != nil {
		return out, err
	}
	out.SecurityEvents, _ = lr.store.PurgeSecurityEventsBefore(ctx, cutoff)
	out.LoginAudits, _ = lr.store.PurgeLoginAuditsBefore(ctx, cutoff)
	lr.lastPurgeInfo = out
	return out, nil
}

// Start launches the background retention loop.
func (lr *LogRetention) Start() {
	log.Info().Dur("interval", lr.interval).Int("default_retention_days", lr.defaultDays).
		Msg("Starting request log retention worker...")
	go func() {
		ticker := time.NewTicker(lr.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				res, err := lr.RunOnce(ctx)
				if err != nil {
					log.Warn().Err(err).Msg("Log retention sweep failed")
				} else if res.RequestLogs > 0 || res.SecurityEvents > 0 || res.LoginAudits > 0 {
					log.Info().
						Int64("request_logs", res.RequestLogs).
						Int64("security_events", res.SecurityEvents).
						Int64("login_audits", res.LoginAudits).
						Msg("Log retention sweep removed expired rows")
					if lr.store != nil {
						_ = lr.store.CreateSecurityEvent(context.Background(), &entity.SecurityEvent{
							Kind:   "log_retention",
							Detail: "Retention sweep removed expired audit rows",
							Action: "purged request_logs/security_events/login_audits",
						})
					}
				}
				cancel()
			case <-lr.stopCh:
				return
			}
		}
	}()
}

// Stop signals the retention loop to shut down.
func (lr *LogRetention) Stop() {
	close(lr.stopCh)
}
