package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
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

	"9router-gateway/embeds"
	"9router-gateway/internal/config"
	"9router-gateway/internal/database"
	"9router-gateway/internal/handlers"
	"9router-gateway/internal/models"
	"9router-gateway/internal/proxy"
	"9router-gateway/internal/repository"
	"9router-gateway/internal/syncer"
)

func main() {
	// 1. Logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting AI Manager Gateway Middleware...")

	// 2. Load Config
	cfg := config.LoadConfig()
	slog.Info("Configuration loaded",
		"port", cfg.Port,
		"upstream_url", cfg.UpstreamURL,
		"db_path", cfg.DBPath,
	)

	// 3. Init Database
	db, err := database.InitDB(cfg.DBPath)
	if err != nil {
		slog.Error("Failed to initialize database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("SQLite database initialized successfully")

	repo := repository.NewSQLiteRepo(db)

	// Load runtime settings from database if configured
	if uURL, err := repo.GetSetting(context.Background(), "upstream_url"); err == nil && uURL != "" {
		cfg.UpstreamURL = uURL
	}
	if uKey, err := repo.GetSetting(context.Background(), "upstream_api_key"); err == nil && uKey != "" {
		cfg.UpstreamAPIKey = uKey
	}
	if nrDB, err := repo.GetSetting(context.Background(), "ninerouter_db_path"); err == nil && nrDB != "" {
		cfg.NineRouterDBPath = nrDB
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

	keySyncer := syncer.NewSyncer(cfg.NineRouterDBPath)

	// 4. Seed initial user and API key if table is empty
	seedInitialData(repo, keySyncer)

	// Reconcile username and password hashes for existing users
	reconcileExistingUsers(repo, cfg)

	// 5. Backfill/Sync all keys to 9router Core
	if existingKeys, err := repo.GetAllAPIKeys(context.Background()); err == nil && len(existingKeys) > 0 {
		if err := keySyncer.BackfillAll(existingKeys); err != nil {
			slog.Warn("Failed to backfill keys to 9router", "err", err)
		} else {
			slog.Info("Successfully synchronized API keys with 9router core", "count", len(existingKeys))
		}
	}

	// 6. Init Handlers & Proxy
	h, err := handlers.NewHandler(cfg, repo, keySyncer)
	if err != nil {
		slog.Error("Failed to initialize web handlers", "err", err)
		os.Exit(1)
	}

	gwProxy := proxy.NewGatewayProxy(cfg, repo)

	// 6. Setup Chi Router
	r := chi.NewRouter()

	// Middlewares
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
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
		AllowCredentials: true,
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

	// Static Assets from embeds
	staticFS, err := fs.Sub(embeds.FS, "static")
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

		// Shared: Dashboard, Keys (self-scoped for user), Logs (self-scoped for user), Models (whitelist-scoped for user)
		authRouter.Get("/", h.DashboardPage)
		authRouter.Get("/api/stats", h.APIStats)

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
		authRouter.Get("/models", h.ModelsPage)
		authRouter.Get("/settings", h.SettingsPage)
		authRouter.Post("/settings/password", h.UpdatePasswordPost)

		// Admin-Only Routes (User Management & Administrative Overrides)
		authRouter.Group(func(adminOnly chi.Router) {
			adminOnly.Use(h.RequireAdmin)

			adminOnly.Get("/users", h.UsersPage)
			adminOnly.Post("/users", h.CreateUser)
			adminOnly.Post("/users/{id}/edit", h.EditUser)
			adminOnly.Post("/users/{id}/password", h.ResetPassword)
			adminOnly.Post("/users/{id}/reset-usage", h.ResetUsage)
			adminOnly.Post("/users/{id}/toggle", h.ToggleUserStatus)
			adminOnly.Post("/users/{id}/delete", h.DeleteUser)

			// Admin Billing Actions
			adminOnly.Post("/api/billing/manual-credit", h.ManualCreditTokens)
			adminOnly.Post("/settings/midtrans", h.UpdateMidtransPost)

			// Admin Upstream 9router Actions
			adminOnly.Post("/settings/upstream", h.UpdateUpstreamPost)
			adminOnly.Get("/api/upstream/test", h.TestUpstreamConnection)
		})
	})

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
		slog.Info("AI Manager Gateway listening", "address", "http://"+addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "err", err)
			os.Exit(1)
		}
	}()

	// Listen for shutdown signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("Shutting down server gracefully...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "err", err)
	}

	slog.Info("9router Gateway exited cleanly")
}

func seedInitialData(repo repository.Repository, sync *syncer.Syncer) {
	ctx := context.Background()
	users, err := repo.GetAllUsers(ctx)
	if err != nil || len(users) > 0 {
		return
	}

	slog.Info("No users found. Creating default admin and demo user...")

	// 1. Default Admin User
	adminID := uuid.New().String()
	adminUser := &models.User{
		ID:            adminID,
		Name:          "Default Admin",
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
	demoID := uuid.New().String()
	demoUser := &models.User{
		ID:            demoID,
		Name:          "Standard User",
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

	slog.Info("Initial data seeded", "admin_key", adminKey, "demo_key", demoKey)
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
			passBytes := make([]byte, 12)
			_, _ = rand.Read(passBytes)
			pass := hex.EncodeToString(passBytes)
			hash, err := handlers.HashPassword(pass)
			if err == nil {
				u.PasswordHash = hash
				_ = repo.UpdateUserPassword(ctx, u.ID, hash)
			}
		}
		// Security hardening: Force replacement of default admin:admin or weak passwords
		if u.Username == "admin" && (handlers.CheckPasswordHash("admin", u.PasswordHash) || handlers.CheckPasswordHash("admin123", u.PasswordHash)) {
			adminPassBytes := make([]byte, 16)
			_, _ = rand.Read(adminPassBytes)
			newAdminPass := hex.EncodeToString(adminPassBytes)
			newHash, _ := handlers.HashPassword(newAdminPass)
			_ = repo.UpdateUserPassword(ctx, u.ID, newHash)
			slog.Warn("SECURITY HARDENING: Default admin password was rotated to secure random key", "username", "admin", "new_secure_password", newAdminPass)
		}
		if updated {
			_ = repo.UpdateUser(ctx, &u)
		}
	}
}
