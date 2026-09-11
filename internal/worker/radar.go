// Package worker runs background jobs such as periodic provider health checks.
package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"9router-gateway/internal/entity"
	"9router-gateway/internal/notify"
	"9router-gateway/internal/repository"
)

// Radar scans gateway traffic for anomalies: error spikes (auto-disable),
// usage spikes and new-IP bursts (alert). Runs every 5 minutes.
type Radar struct {
	repo     repository.Repository
	notifier *notify.Sender
	stopChan chan struct{}
}

// NewRadar builds the anomaly radar.
func NewRadar(repo repository.Repository, notifier *notify.Sender) *Radar {
	return &Radar{repo: repo, notifier: notifier, stopChan: make(chan struct{})}
}

// Start launches the background scan loop.
func (rd *Radar) Start() {
	log.Info().Msg("Starting Anomaly Radar...")
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rd.scan()
			case <-rd.stopChan:
				return
			}
		}
	}()
}

// Stop signals the background goroutine to shut down.
func (rd *Radar) Stop() {
	close(rd.stopChan)
}

func (rd *Radar) alert(dedup, msg string) {
	if rd.notifier == nil {
		return
	}
	rd.notifier.Send(context.Background(), dedup, time.Hour, msg)
}

// recentlyFired reports whether the same kind+key fired within the cooldown.
func (rd *Radar) recentlyFired(ctx context.Context, kind, keyID string, cooldown time.Duration) bool {
	events, err := rd.repo.ListSecurityEvents(ctx, 60)
	if err != nil {
		return false
	}
	cutoff := time.Now().Add(-cooldown)
	for _, ev := range events {
		if ev.Kind == kind && ev.APIKeyID == keyID && ev.CreatedAt.After(cutoff) {
			return true
		}
	}
	return false
}

func (rd *Radar) scan() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stats, err := rd.repo.GetKeyWindowStats(ctx, time.Now().Add(-30*time.Minute))
	if err != nil {
		log.Warn().Err(err).Msg("radar: window stats failed")
		return
	}
	baseline, err := rd.repo.GetKeyBaselineTokens(ctx, 7)
	if err != nil {
		log.Warn().Err(err).Msg("radar: baseline failed")
		baseline = map[string]float64{}
	}

	for _, s := range stats {
		label := s.KeyName
		if label == "" {
			label = s.KeyID
		}
		who := label
		if s.UserName != "" {
			who += " (" + s.UserName + ")"
		}

		// 1. Error spike → auto-disable (needs volume to avoid false positives).
		if s.Requests >= 20 && float64(s.Errors)/float64(s.Requests) >= 0.5 {
			if rd.recentlyFired(ctx, "error_spike", s.KeyID, 2*time.Hour) {
				continue
			}
			detail := fmt.Sprintf("key %s: %d/%d requests failed in 30m (%.0f%%)", who, s.Errors, s.Requests, float64(s.Errors)/float64(s.Requests)*100)
			if err := rd.repo.ToggleAPIKeyStatus(ctx, s.KeyID, false); err == nil {
				_ = rd.repo.CreateSecurityEvent(ctx, &entity.SecurityEvent{Kind: "error_spike", UserID: s.UserID, APIKeyID: s.KeyID, Detail: detail, Action: "key auto-disabled"})
				rd.alert("radar:error:"+s.KeyID, "🚨 Error spike — "+detail+". Key auto-disabled, re-enable dari Keys kalau false alarm.")
			}
			continue
		}

		// 2. Usage spike vs 7-day baseline (alert only, needs real volume).
		if avg := baseline[s.KeyID]; avg > 0 && s.Tokens >= 100000 && float64(s.Tokens) >= 5*avg {
			if rd.recentlyFired(ctx, "usage_spike", s.KeyID, 6*time.Hour) {
				continue
			}
			detail := fmt.Sprintf("key %s: %d tokens in 30m vs ~%.0f/day baseline", who, s.Tokens, avg)
			_ = rd.repo.CreateSecurityEvent(ctx, &entity.SecurityEvent{Kind: "usage_spike", UserID: s.UserID, APIKeyID: s.KeyID, Detail: detail, Action: "alerted"})
			rd.alert("radar:usage:"+s.KeyID, "📈 Usage spike — "+detail+". Cek Logs kalau bukan traffic lo.")
		}

		// 3. IP burst: key used from 4+ distinct IPs in 30m (possible leak).
		if s.IPCount >= 4 {
			if rd.recentlyFired(ctx, "new_ip", s.KeyID, 6*time.Hour) {
				continue
			}
			detail := fmt.Sprintf("key %s: %d distinct IPs in 30m", who, s.IPCount)
			_ = rd.repo.CreateSecurityEvent(ctx, &entity.SecurityEvent{Kind: "new_ip", UserID: s.UserID, APIKeyID: s.KeyID, Detail: detail, Action: "alerted"})
			rd.alert("radar:ip:"+s.KeyID, "🔑 IP burst — "+detail+". Kalau key bocor, revoke dari Keys.")
		}
	}
}
