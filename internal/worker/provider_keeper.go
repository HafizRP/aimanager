// Package worker runs background jobs such as periodic provider health checks and circuit breaker monitoring.
package worker

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"9router-gateway/internal/config"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/notify"
	"9router-gateway/internal/upstream"
)

// LogRepository defines the interface for recording security and health events.
type LogRepository interface {
	CreateSecurityEvent(ctx context.Context, ev *entity.SecurityEvent) error
}

// ProviderKeeper periodically tests provider accounts, auto-reactivates quota-reset accounts,
// and gracefully disables providers after consecutive 5xx or health check failures (circuit breaker).
type ProviderKeeper struct {
	cfg              *config.Config
	coreClient       upstream.CoreClient
	quotaMgr         *upstream.QuotaManager
	notifier         *notify.Sender
	repo             LogRepository
	failureThreshold int
	failureCounts    map[string]int
	mu               sync.Mutex
	stopChan         chan struct{}
}

// NewProviderKeeper creates a ProviderKeeper wired to the given config and core client.
func NewProviderKeeper(cfg *config.Config, coreClient upstream.CoreClient, quotaMgr *upstream.QuotaManager) *ProviderKeeper {
	return &ProviderKeeper{
		cfg:              cfg,
		coreClient:       coreClient,
		quotaMgr:         quotaMgr,
		failureThreshold: 3,
		failureCounts:    make(map[string]int),
		stopChan:         make(chan struct{}),
	}
}

// SetNotifier attaches the Telegram alert sender (nil-safe, optional).
func (pk *ProviderKeeper) SetNotifier(s *notify.Sender) {
	pk.notifier = s
}

// SetLogRepository attaches the audit log repository for security event recording.
func (pk *ProviderKeeper) SetLogRepository(r LogRepository) {
	pk.repo = r
}

// SetFailureThreshold sets the consecutive failure threshold before auto-disabling (circuit breaker).
func (pk *ProviderKeeper) SetFailureThreshold(threshold int) {
	pk.mu.Lock()
	defer pk.mu.Unlock()
	if threshold > 0 {
		pk.failureThreshold = threshold
	}
}

// GetFailureCount returns the current consecutive failure count for a provider connection.
func (pk *ProviderKeeper) GetFailureCount(id string) int {
	pk.mu.Lock()
	defer pk.mu.Unlock()
	return pk.failureCounts[id]
}

// RecordProviderFailure increments failure count and auto-disables the provider if threshold is reached.
func (pk *ProviderKeeper) RecordProviderFailure(ctx context.Context, id, provider, name string) {
	pk.mu.Lock()
	pk.failureCounts[id]++
	failures := pk.failureCounts[id]
	threshold := pk.failureThreshold
	pk.mu.Unlock()

	dispName := name
	if dispName == "" {
		dispName = id
	}

	log.Warn().
		Str("id", id).
		Str("provider", provider).
		Str("name", dispName).
		Int("consecutive_failures", failures).
		Int("threshold", threshold).
		Msg("Provider health check failure recorded")

	if failures >= threshold {
		if err := pk.coreClient.ToggleProvider(ctx, id, false); err == nil {
			log.Warn().
				Str("id", id).
				Str("provider", provider).
				Str("name", dispName).
				Int("failures", failures).
				Msg("Graceful circuit breaker: auto-disabled provider connection after consecutive failures")

			if pk.notifier != nil {
				pk.notifier.Send(context.Background(), "keeper:disable:"+id, 6*time.Hour,
					fmt.Sprintf("🚨 Circuit breaker tripped — provider %s/%s auto-disabled after %d consecutive failures. Self-heal keeper will monitor and reactivate once healthy.", provider, dispName, failures))
			}

			if pk.repo != nil {
				_ = pk.repo.CreateSecurityEvent(context.Background(), &entity.SecurityEvent{
					Kind:   "circuit_breaker",
					Detail: fmt.Sprintf("Provider connection %s/%s auto-disabled after %d consecutive failures", provider, dispName, failures),
					Action: "provider auto-disabled",
				})
			}
		}
	}
}

// RecordProviderSuccess resets the consecutive failure count for a provider connection.
func (pk *ProviderKeeper) RecordProviderSuccess(id string) {
	pk.mu.Lock()
	pk.failureCounts[id] = 0
	pk.mu.Unlock()
}

// Start launches the background goroutine that periodically checks provider health.
func (pk *ProviderKeeper) Start() {
	log.Info().Msg("Starting Provider Auto-Reactivation and Circuit Breaker Worker...")
	go func() {
		reactivateTicker := time.NewTicker(45 * time.Second)
		defer reactivateTicker.Stop()
		autoDisableTicker := time.NewTicker(90 * time.Second)
		defer autoDisableTicker.Stop()

		for {
			select {
			case <-reactivateTicker.C:
				pk.checkAndReactivate()
			case <-autoDisableTicker.C:
				pk.checkAndAutoDisable()
			case <-pk.stopChan:
				return
			}
		}
	}()
}

// Stop signals the background goroutine to shut down.
func (pk *ProviderKeeper) Stop() {
	close(pk.stopChan)
}

// CheckAndReactivateNow triggers an immediate reactive check without waiting for the next timer interval.
func (pk *ProviderKeeper) CheckAndReactivateNow() {
	go pk.checkAndReactivate()
}

// CheckAndAutoDisableNow triggers an immediate reactive check of active providers.
func (pk *ProviderKeeper) CheckAndAutoDisableNow() {
	go pk.checkAndAutoDisable()
}

func (pk *ProviderKeeper) checkAndReactivate() {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	dbPath := pk.cfg.GetNineRouterDBPath()
	if dbPath == "" {
		dbPath = "./data/core/db/data.sqlite"
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return
	}
	defer db.Close()

	// Query inactive providers
	query := `SELECT id, provider, name, email, isActive FROM providerConnections WHERE isActive = 0`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return
	}
	defer rows.Close()

	type inactiveConn struct {
		ID       string
		Provider string
		Name     string
		Email    string
	}
	var toTest []inactiveConn

	for rows.Next() {
		var c inactiveConn
		var active int
		var name, email sql.NullString
		if err := rows.Scan(&c.ID, &c.Provider, &name, &email, &active); err == nil {
			if name.Valid {
				c.Name = name.String
			}
			if email.Valid {
				c.Email = email.String
			}
			toTest = append(toTest, c)
		}
	}

	for _, conn := range toTest {
		// Test connectivity with 9router Core
		testRes, err := pk.coreClient.TestProvider(ctx, conn.ID)
		valid := false
		if err == nil && testRes != nil {
			if v, ok := testRes["valid"].(bool); ok && v {
				valid = true
			}
		}
		if valid {
			// Account has recovered! Reactivate connection
			err := pk.coreClient.ToggleProvider(ctx, conn.ID, true)
			if err == nil {
				pk.RecordProviderSuccess(conn.ID)

				dispName := conn.Name
				if dispName == "" {
					dispName = conn.Email
				}
				log.Info().
					Str("id", conn.ID).
					Str("provider", conn.Provider).
					Str("name", conn.Name).
					Str("email", conn.Email).
					Msg("Auto-reactivated provider account after quota reset window")

				if pk.notifier != nil {
					pk.notifier.Send(context.Background(), "keeper:reactivate:"+conn.ID, 12*time.Hour,
						"✅ Self-heal — connection "+conn.Provider+"/"+dispName+" active again (quota reset), auto-reactivated.")
				}

				if pk.repo != nil {
					_ = pk.repo.CreateSecurityEvent(context.Background(), &entity.SecurityEvent{
						Kind:   "self_heal",
						Detail: fmt.Sprintf("Provider connection %s/%s recovered connectivity", conn.Provider, dispName),
						Action: "provider auto-reactivated",
					})
				}
			}
		}
	}
}

func (pk *ProviderKeeper) checkAndAutoDisable() {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	dbPath := pk.cfg.GetNineRouterDBPath()
	if dbPath == "" {
		dbPath = "./data/core/db/data.sqlite"
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return
	}
	defer db.Close()

	// Query active providers
	query := `SELECT id, provider, name, email, isActive FROM providerConnections WHERE isActive = 1`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return
	}
	defer rows.Close()

	type activeConn struct {
		ID       string
		Provider string
		Name     string
		Email    string
	}
	var toTest []activeConn

	for rows.Next() {
		var c activeConn
		var active int
		var name, email sql.NullString
		if err := rows.Scan(&c.ID, &c.Provider, &name, &email, &active); err == nil {
			if name.Valid {
				c.Name = name.String
			}
			if email.Valid {
				c.Email = email.String
			}
			toTest = append(toTest, c)
		}
	}

	for _, conn := range toTest {
		testRes, err := pk.coreClient.TestProvider(ctx, conn.ID)
		valid := false
		if err == nil && testRes != nil {
			if v, ok := testRes["valid"].(bool); ok && v {
				valid = true
			}
			// Skip providers that 9router Core does not support testing for (e.g. commandcode)
			if errMsg, ok := testRes["error"].(string); ok && strings.Contains(strings.ToLower(errMsg), "not supported") {
				continue
			}
		}

		dispName := conn.Name
		if dispName == "" {
			dispName = conn.Email
		}

		if valid {
			pk.RecordProviderSuccess(conn.ID)
		} else {
			pk.RecordProviderFailure(ctx, conn.ID, conn.Provider, dispName)
		}
	}
}
