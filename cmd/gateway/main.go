package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"9router-gateway/internal/config"
	"9router-gateway/internal/database"
	"9router-gateway/internal/handlers"
	"9router-gateway/internal/models"
	"9router-gateway/internal/proxy"
	"9router-gateway/internal/repository"
	"9router-gateway/internal/syncer"
	"9router-gateway/internal/upstream"
	"9router-gateway/internal/worker"
	"9router-gateway/web"
)

func main() {
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
		os.Exit(1)
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
	h, err := handlers.NewHandler(cfg, repo, keySyncer, quotaMgr)
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize web handlers")
		os.Exit(1)
	}

	gwProxy := proxy.NewGatewayProxy(cfg, repo)

	// 6. Setup Chi Router
	r := chi.NewRouter()

	// Middlewares
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			t1 := time.Now()
			defer func() {
				// Don't clutter logs with frequent health probes unless verbose
				if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
					return
				}
				status := ww.Status()
				event := log.Info()
				if status >= 500 {
					event = log.Error()
				} else if status >= 400 {
					event = log.Warn()
				}
				event.
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Int("status", status).
					Int("bytes", ww.BytesWritten()).
					Dur("latency", time.Since(t1)).
					Str("ip", r.RemoteAddr).
					Msg("HTTP Request")
			}()
			next.ServeHTTP(ww, r)
		})
	})
	r.Use(middleware.Recoverer)
	r.Use(middleware.GetHead)

	// Security Headers Middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://app.midtrans.com https://app.sandbox.midtrans.com; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com https://cdn.jsdelivr.net; img-src 'self' data: https:; connect-src 'self' https://app.midtrans.com https://app.sandbox.midtrans.com; frame-src https://app.midtrans.com https://app.sandbox.midtrans.com;")

			proto := r.Header.Get("X-Forwarded-Proto")
			host := r.Host
			isLocal := strings.HasPrefix(host, "localhost") ||
				strings.HasPrefix(host, "127.0.0.1") ||
				strings.HasPrefix(host, "100.") ||
				strings.HasPrefix(host, "192.168.") ||
				strings.HasPrefix(host, "10.")

			// If accessed via plain HTTP on public domain -> permanently redirect to HTTPS
			if proto == "http" && !isLocal {
				target := "https://" + host + r.URL.RequestURI()
				http.Redirect(w, r, target, http.StatusMovedPermanently)
				return
			}

			// Enforce HSTS for HTTPS connections
			if proto == "https" || r.TLS != nil {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
			}

			next.ServeHTTP(w, r)
		})
	})

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "x-api-key"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health Probes
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			http.Error(w, "Database down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("READY"))
	})

	// Static Assets from web/static
	staticFS, err := fs.Sub(web.FS, "static")
	if err == nil {
		r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	}

	// 7. Reverse Proxy / Gateway routes (OpenAI & Anthropic compatible API)
	r.HandleFunc("/v1", gwProxy.ServeHTTP)
	r.HandleFunc("/v1/*", gwProxy.ServeHTTP)

	// 8. Public Auth & Webhook Routes
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.LoginPost)
	r.Post("/logout", h.LogoutPost)
	r.Post("/api/webhook/midtrans", h.MidtransWebhook)

	// 9. Protected Web Dashboard Routes (Accessible by Authenticated Users)
	r.Group(func(authRouter chi.Router) {
		authRouter.Use(h.RequireAuth)
		authRouter.Use(h.ValidateCSRF)

		// Shared: Dashboard, Keys (self-scoped for user), Logs (self-scoped for user), Models (whitelist-scoped for user)
		authRouter.Get("/", h.DashboardPage)
		authRouter.Get("/api/stats", h.APIStats)
		authRouter.Get("/api/upstream/quotas", h.APIUpstreamQuotas)

		// Billing & Top-Up
		authRouter.Get("/billing", h.BillingPage)
		authRouter.Post("/api/billing/checkout", h.CheckoutSnap)

		// Keys (Scoped: Admin can manage all, Standard users manage their own)
		authRouter.Get("/keys", h.KeysPage)
		authRouter.Post("/keys", h.CreateKey)
		authRouter.Post("/keys/{id}/toggle", h.ToggleKeyStatus)
		authRouter.Post("/keys/{id}/delete", h.DeleteKey)

		// Logs, Models, Settings
		authRouter.Get("/logs", h.LogsPage)
		authRouter.Get("/api/logs", h.APILogs)
		authRouter.Get("/api/logs/export", h.ExportLogs)
		authRouter.Get("/models", h.ModelsPage)
		authRouter.Get("/api/models/alias", h.APIModelAliasesGet)
		authRouter.Get("/settings", h.SettingsPage)
		authRouter.Post("/settings/password", h.UpdatePasswordPost)

		// Workspace: Chat Playground & CLI Tools Setup Hub & Speed Benchmark
		authRouter.Get("/chat", h.ChatPage)
		authRouter.Get("/playground", h.ChatPage)
		authRouter.Get("/cli-tools", h.CLIToolsPage)
		authRouter.Get("/benchmark", h.BenchmarkPage)
		authRouter.Post("/api/benchmark/run", h.APIBenchmarkRun)

		// Cache Analytics (self-service for users, full stats for admin)
		authRouter.Get("/cache-analytics", h.CacheAnalyticsPage)
		authRouter.Get("/api/cache/stats", h.APICacheStats)
		authRouter.Delete("/api/cache/stats", h.APICacheStats)

		// Admin-Only Routes (User Management & Administrative Overrides)
		authRouter.Group(func(adminOnly chi.Router) {
			adminOnly.Use(h.RequireAdmin)

			adminOnly.Get("/users", h.UsersPage)
			adminOnly.Get("/users/{id}", h.UserDetailPage)
			adminOnly.Post("/users", h.CreateUser)
			adminOnly.Post("/users/{id}/edit", h.EditUser)
			adminOnly.Post("/users/{id}/password", h.ResetPassword)
			adminOnly.Post("/users/{id}/reset-usage", h.ResetUsage)
			adminOnly.Post("/users/{id}/toggle", h.ToggleUserStatus)
			adminOnly.Post("/users/{id}/delete", h.DeleteUser)

			// 9router Core: Upstream Providers Management
			adminOnly.Get("/providers", h.ProvidersPage)
			adminOnly.Post("/api/providers/toggle", h.APIProvidersToggle)
			adminOnly.Post("/api/providers/priority", h.APIProvidersPriority)
			adminOnly.Get("/api/providers/test", h.APIProvidersTest)
			adminOnly.Post("/api/providers/test", h.APIProvidersTest)
			adminOnly.Post("/api/providers/delete", h.APIProvidersDelete)
			adminOnly.Post("/api/providers/create", h.APIProvidersCreate)

			// 9router Core: Combos & Fallbacks
			adminOnly.Get("/combos", h.CombosPage)
			adminOnly.Post("/api/combos/create", h.APICombosCreate)
			adminOnly.Post("/api/combos/update", h.APICombosUpdate)
			adminOnly.Post("/api/combos/delete", h.APICombosDelete)

			// 9router Core: Token Saver, RTK, Provider Thinking, Caveman
			adminOnly.Get("/token-saver", h.TokenSaverPage)
			adminOnly.Post("/api/token-saver/save", h.APITokenSaverSave)

			// 9router Core: Proxy Pools
			adminOnly.Get("/proxy-pools", h.ProxyPoolsPage)
			adminOnly.Post("/api/proxy-pools/create", h.APIProxyPoolsCreate)
			adminOnly.Post("/api/proxy-pools/delete", h.APIProxyPoolsDelete)
			adminOnly.Post("/api/proxy-pools/test", h.APIProxyPoolsTest)

			// 9router Core: Model Aliases CRUD
			adminOnly.Post("/api/models/alias", h.APIModelAliasSet)
			adminOnly.Delete("/api/models/alias", h.APIModelAliasDelete)
			adminOnly.Post("/api/models/alias/delete", h.APIModelAliasDelete)

			// Admin Billing Actions
			adminOnly.Post("/api/billing/manual-credit", h.ManualCreditTokens)
			adminOnly.Post("/settings/midtrans", h.UpdateMidtransPost)

			// Admin Upstream 9router Actions
			adminOnly.Post("/settings/upstream", h.UpdateUpstreamPost)
			adminOnly.Get("/api/upstream/test", h.TestUpstreamConnection)
		})
	})

	// 9.5 Background Provider Keeper (Auto-reactivate quota-reset accounts)
	coreClient := upstream.NewCoreClient(cfg)
	keeper := worker.NewProviderKeeper(cfg, coreClient, quotaMgr)
	go keeper.Start()

	// 9.6 Background Cleanup Daemon (expired sessions and old login attempts)
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
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // Streaming SSE requires no write timeout
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info().Str("address", "http://"+addr).Msg("AI Manager Gateway listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server forced to shutdown")
	}

	log.Info().Msg("9router Gateway exited cleanly")
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
	adminHash, err := handlers.HashPassword(adminPassword)
	if err != nil {
		log.Error().Err(err).Msg("Failed to hash admin password during seed")
	}

	// 1. Default Admin User
	adminID := uuid.New().String()
	adminUser := &models.User{
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

	adminKey := handlers.GenerateSecureAPIKey("sk-gw-admin-")
	adminAPIKey := &models.APIKey{
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
	demoHash, _ := handlers.HashPassword("user123")
	demoID := uuid.New().String()
	demoUser := &models.User{
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

	demoKey := handlers.GenerateSecureAPIKey("sk-gw-user-")
	demoAPIKey := &models.APIKey{
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
			hash, err := handlers.HashPassword(pass)
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
