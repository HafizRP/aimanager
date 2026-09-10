// Package app configures and runs the 9router Gateway application.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stdhttp "net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"9router-gateway/internal/config"
	controllerhttp "9router-gateway/internal/controller/http"
	"9router-gateway/internal/controller/http/v1"
	"9router-gateway/internal/database"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/proxy"
	"9router-gateway/internal/repository"
	"9router-gateway/internal/syncer"
	"9router-gateway/internal/upstream"
	"9router-gateway/internal/usecase"
	"9router-gateway/internal/worker"
)

// Run is the application composition root: configures logger, loads config,
// initializes infrastructure, wires dependencies, and runs the HTTP server
// until SIGINT/SIGTERM, then shuts down gracefully.
func Run() error {
	// 1. Logger Setup (Zerolog)
	zerolog.TimeFieldFormat = time.RFC3339
	if os.Getenv("LOG_FORMAT") != "json" {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "2006-01-02 15:04:05",
		})
	}

	log.Info().Msg("Starting AI Manager Gateway Middleware...")

	// 2. Load Config
	cfg := config.LoadConfig()
	log.Info().
		Int("port", cfg.Port).
		Str("upstream_url", cfg.UpstreamURL).
		Str("db_path", cfg.DBPath).
		Msg("Configuration loaded")

	// 3. Init Database
	db, err := database.InitDB(cfg.DBPath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize database")
		return err
	}
	defer db.Close()
	log.Info().Msg("SQLite database initialized successfully")

	repo := repository.NewSQLiteRepo(db)

	// Load Upstream 9router Core settings directly from database
	if uURL, err := repo.GetSetting(context.Background(), "upstream_url"); err == nil && strings.TrimSpace(uURL) != "" {
		cfg.SetUpstreamURL(uURL)
		log.Info().Str("upstream_url", cfg.GetUpstreamURL()).Msg("Loaded upstream URL from database")
	} else {
		cfg.SetUpstreamURL("http://127.0.0.1:20128")
		_ = repo.SaveSetting(context.Background(), "upstream_url", cfg.GetUpstreamURL())
	}

	if uKey, err := repo.GetSetting(context.Background(), "upstream_api_key"); err == nil {
		cfg.SetUpstreamAPIKey(uKey)
	}

	if nrDB, err := repo.GetSetting(context.Background(), "ninerouter_db_path"); err == nil && strings.TrimSpace(nrDB) != "" {
		cfg.SetNineRouterDBPath(nrDB)
	} else {
		cfg.SetNineRouterDBPath("./data/core/db/data.sqlite")
		_ = repo.SaveSetting(context.Background(), "ninerouter_db_path", cfg.GetNineRouterDBPath())
	}

	if nrDataDir, err := repo.GetSetting(context.Background(), "ninerouter_data_dir"); err == nil && nrDataDir != "" {
		cfg.NineRouterDataDir = nrDataDir
	}
	if mServer, err := repo.GetSetting(context.Background(), "midtrans_server_key"); err == nil && mServer != "" {
		cfg.MidtransServerKey = mServer
	}
	if mClient, err := repo.GetSetting(context.Background(), "midtrans_client_key"); err == nil && mClient != "" {
		cfg.MidtransClientKey = mClient
	}
	if mMerchant, err := repo.GetSetting(context.Background(), "midtrans_merchant_id"); err == nil && mMerchant != "" {
		cfg.MidtransMerchantID = mMerchant
	}
	if mProd, err := repo.GetSetting(context.Background(), "midtrans_is_production"); err == nil && mProd != "" {
		cfg.MidtransIsProduction = (mProd == "true")
	}

	// Ensure strong, persistent session secret
	if cfg.SessionSecret == "" || cfg.SessionSecret == "9router-secret-token-key-change-me" {
		if storedSecret, err := repo.GetSetting(context.Background(), "session_secret"); err == nil && storedSecret != "" {
			cfg.SessionSecret = storedSecret
		} else {
			secretBytes := make([]byte, 32)
			_, _ = rand.Read(secretBytes)
			cfg.SessionSecret = hex.EncodeToString(secretBytes)
			_ = repo.SaveSetting(context.Background(), "session_secret", cfg.SessionSecret)
			log.Info().Msg("Generated and persisted secure random session secret")
		}
	}

	keySyncer := syncer.NewSyncer(cfg.GetNineRouterDBPath())

	// 4. Seed initial user and API key if table is empty
	seedInitialData(repo, keySyncer, cfg)

	// Reconcile username and password hashes for existing users
	reconcileExistingUsers(repo, cfg)

	// 5. Backfill/Sync all keys to 9router Core
	if existingKeys, err := repo.GetAllAPIKeys(context.Background()); err == nil && len(existingKeys) > 0 {
		if err := keySyncer.BackfillAll(existingKeys); err != nil {
			log.Warn().Err(err).Msg("Failed to backfill keys to 9router")
		} else {
			log.Info().Int("count", len(existingKeys)).Msg("Successfully synchronized API keys with 9router core")
		}
	}

	// 6. Init Handlers & Proxy
	quotaMgr := upstream.NewQuotaManager(cfg)
	h, err := v1.NewHandler(cfg, repo, keySyncer, quotaMgr)
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize web handlers")
		return err
	}

	gwProxy := proxy.NewGatewayProxy(cfg, repo)

	// 7. Assemble Router (composition root)
	r := controllerhttp.NewRouter(cfg, db, repo, h, gwProxy)

	// 8. Background Provider Keeper (Auto-reactivate quota-reset accounts)
	coreClient := upstream.NewCoreClient(cfg)
	keeper := worker.NewProviderKeeper(cfg, coreClient, quotaMgr)
	go keeper.Start()

	// 9. Background Cleanup Daemon (expired sessions and old login attempts)
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_ = repo.CleanExpiredSessions(cleanCtx)
			_ = repo.CleanOldLoginAttempts(cleanCtx)
			cancel()
		}
	}()

	// 10. Start Server with Graceful Shutdown
	srv := controllerhttp.NewServer(cfg, r)

	go func() {
		log.Info().Str("address", "http://"+srv.Addr).Msg("AI Manager Gateway listening")
		if err := srv.ListenAndServe(); err != nil && err != stdhttp.ErrServerClosed {
			log.Error().Err(err).Msg("HTTP server failed")
			os.Exit(1)
		}
	}()

	// Listen for shutdown signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info().Msg("Shutting down server gracefully...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server forced to shutdown")
	}

	log.Info().Msg("9router Gateway exited cleanly")
	return nil
}

func seedInitialData(repo repository.Repository, sync *syncer.Syncer, cfg *config.Config) {
	ctx := context.Background()
	users, err := repo.GetAllUsers(ctx)
	if err != nil || len(users) > 0 {
		return
	}

	log.Info().Msg("No users found. Creating default admin and demo user...")

	adminUsername := strings.TrimSpace(cfg.AdminUsername)
	if adminUsername == "" {
		adminUsername = "admin"
	}
	adminPassword := strings.TrimSpace(cfg.AdminPassword)
	if adminPassword == "" {
		adminPassword = "admin123"
	}
	adminHash, err := usecase.HashPassword(adminPassword)
	if err != nil {
		log.Error().Err(err).Msg("Failed to hash admin password during seed")
	}

	// 1. Default Admin User
	adminID := uuid.New().String()
	adminUser := &entity.User{
		ID:            adminID,
		Username:      adminUsername,
		Name:          "Default Admin",
		PasswordHash:  adminHash,
		Role:          "admin",
		TokenQuota:    0, // Unlimited
		TokensUsed:    0,
		AllowedModels: `["*"]`,
		IsActive:      true,
	}
	_ = repo.CreateUser(ctx, adminUser)

	adminKey := usecase.GenerateSecureAPIKey("sk-gw-admin-")
	adminAPIKey := &entity.APIKey{
		ID:       uuid.New().String(),
		UserID:   adminID,
		Key:      adminKey,
		Name:     "Admin Master Key",
		IsActive: true,
	}
	_ = repo.CreateAPIKey(ctx, adminAPIKey)
	if sync != nil {
		_ = sync.SyncKey(adminAPIKey, "Default Admin")
	}

	// 2. Demo User with quota & whitelist
	demoHash, _ := usecase.HashPassword("user123")
	demoID := uuid.New().String()
	demoUser := &entity.User{
		ID:            demoID,
		Username:      "user",
		Name:          "Standard User",
		PasswordHash:  demoHash,
		Role:          "user",
		TokenQuota:    1000000, // 1M tokens
		TokensUsed:    0,
		AllowedModels: `["ag/gemini-3.8-flash-low", "ag/gemini-3.8-flash", "ag/gemini-3.7-flash-low"]`,
		IsActive:      true,
	}
	_ = repo.CreateUser(ctx, demoUser)

	demoKey := usecase.GenerateSecureAPIKey("sk-gw-user-")
	demoAPIKey := &entity.APIKey{
		ID:       uuid.New().String(),
		UserID:   demoID,
		Key:      demoKey,
		Name:     "Cursor Key",
		IsActive: true,
	}
	_ = repo.CreateAPIKey(ctx, demoAPIKey)
	if sync != nil {
		_ = sync.SyncKey(demoAPIKey, "Standard User")
	}

	log.Info().
		Str("admin_username", adminUsername).
		Str("admin_key", adminKey).
		Str("demo_key", demoKey).
		Msg("Initial data seeded successfully")
}

func reconcileExistingUsers(repo repository.Repository, cfg *config.Config) {
	ctx := context.Background()
	users, err := repo.GetAllUsers(ctx)
	if err != nil {
		return
	}

	for _, u := range users {
		updated := false
		if u.Username == "" {
			u.Username = strings.ToLower(strings.ReplaceAll(u.Name, " ", ""))
			updated = true
		}
		if u.PasswordHash == "" {
			var pass string
			if u.Username == cfg.AdminUsername || u.Role == "admin" {
				pass = cfg.AdminPassword
			} else {
				pass = "user123"
			}
			hash, err := usecase.HashPassword(pass)
			if err == nil {
				u.PasswordHash = hash
				_ = repo.UpdateUserPassword(ctx, u.ID, hash)
				log.Info().Str("username", u.Username).Msg("Set password hash for user during migration")
			}
		}
		if updated {
			_ = repo.UpdateUser(ctx, &u)
		}
	}
}