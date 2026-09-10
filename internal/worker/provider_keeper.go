// Package worker runs background jobs such as periodic provider health checks.
package worker

import (
	"context"
	"database/sql"
	"time"

	"github.com/rs/zerolog/log"

	"9router-gateway/internal/config"
	"9router-gateway/internal/upstream"
)

// ProviderKeeper periodically tests and reactivates idle upstream provider accounts.
type ProviderKeeper struct {
	cfg        *config.Config
	coreClient *upstream.CoreClient
	quotaMgr   *upstream.QuotaManager
	stopChan   chan struct{}
}

// NewProviderKeeper creates a ProviderKeeper wired to the given config and core client.
func NewProviderKeeper(cfg *config.Config, coreClient *upstream.CoreClient, quotaMgr *upstream.QuotaManager) *ProviderKeeper {
	return &ProviderKeeper{
		cfg:        cfg,
		coreClient: coreClient,
		quotaMgr:   quotaMgr,
		stopChan:   make(chan struct{}),
	}
}

// Start launches the background goroutine that periodically checks provider health.
func (pk *ProviderKeeper) Start() {
	log.Info().Msg("Starting Provider Auto-Reactivation Worker...")
	go func() {
		ticker := time.NewTicker(45 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				pk.checkAndReactivate()
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
				log.Info().
					Str("id", conn.ID).
					Str("provider", conn.Provider).
					Str("name", conn.Name).
					Str("email", conn.Email).
					Msg("Auto-reactivated provider account after quota reset window")
			}
		}
	}
}
