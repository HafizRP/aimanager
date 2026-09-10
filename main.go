package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
)

func main() {
	// 1. Logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting 9router Gateway Middleware...")

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

	// 4. Seed initial user and API key if table is empty
	seedInitialData(repo)

	// 5. Init Handlers & Proxy
	h, err := handlers.NewHandler(cfg, repo)
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

	// 8. Public Auth Routes for Dashboard
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.LoginPost)
	r.Post("/logout", h.LogoutPost)

	// 9. Protected Web Admin Dashboard Routes
	r.Group(func(admin chi.Router) {
		admin.Use(h.RequireAuth)

		admin.Get("/", h.DashboardPage)
		admin.Get("/api/stats", h.APIStats)

		// Users
		admin.Get("/users", h.UsersPage)
		admin.Post("/users", h.CreateUser)
		admin.Post("/users/{id}/edit", h.EditUser)
		admin.Post("/users/{id}/toggle", h.ToggleUserStatus)
		admin.Post("/users/{id}/delete", h.DeleteUser)

		// Keys
		admin.Get("/keys", h.KeysPage)
		admin.Post("/keys", h.CreateKey)
		admin.Post("/keys/{id}/toggle", h.ToggleKeyStatus)
		admin.Post("/keys/{id}/delete", h.DeleteKey)

		// Logs, Models, Settings
		admin.Get("/logs", h.LogsPage)
		admin.Get("/models", h.ModelsPage)
		admin.Get("/settings", h.SettingsPage)
		admin.Post("/settings/password", h.UpdatePasswordPost)
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
		slog.Info("9router Gateway listening", "address", "http://"+addr)
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

func seedInitialData(repo repository.Repository) {
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
	_ = repo.CreateAPIKey(ctx, &models.APIKey{
		ID:       uuid.New().String(),
		UserID:   adminID,
		Key:      adminKey,
		Name:     "Admin Master Key",
		IsActive: true,
	})

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
	_ = repo.CreateAPIKey(ctx, &models.APIKey{
		ID:       uuid.New().String(),
		UserID:   demoID,
		Key:      demoKey,
		Name:     "Cursor Key",
		IsActive: true,
	})

	slog.Info("Initial data seeded", "admin_key", adminKey, "demo_key", demoKey)
}
