package http

import (
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog/log"

	"9router-gateway/internal/config"
	"9router-gateway/internal/controller/http/v1"
	"9router-gateway/internal/proxy"
	"9router-gateway/internal/repository"
	"9router-gateway/web"
)

// NewRouter assembles the HTTP router (composition root for handlers/proxy).
func NewRouter(cfg *config.Config, db *sql.DB, repo repository.Repository, h *v1.Handler, gwProxy *proxy.GatewayProxy) *chi.Mux {
	r := chi.NewRouter()

	// Middlewares
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			t1 := time.Now()
			defer func() {
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
		_, _ = w.Write([]byte("OK"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			http.Error(w, "Database down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("READY"))
	})

	// Static Assets from web/static
	staticFS, err := fs.Sub(web.FS, "static")
	if err == nil {
		r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
		// PWA: service worker must be served from root scope, manifest canonical URL.
		r.Handle("/sw.js", http.StripPrefix("/", http.FileServer(http.FS(staticFS))))
		r.Handle("/manifest.webmanifest", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/manifest+json")
			http.ServeFileFS(w, r, staticFS, "manifest.webmanifest")
		}))
	}

	// Reverse Proxy / Gateway routes (OpenAI & Anthropic compatible API)
	r.HandleFunc("/v1", gwProxy.ServeHTTP)
	r.HandleFunc("/v1/*", gwProxy.ServeHTTP)

	// Public Auth & Webhook Routes
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.LoginPost)
	r.Post("/logout", h.LogoutPost)
	r.Post("/api/webhook/midtrans", h.MidtransWebhook)

	// Protected Web Dashboard Routes (Accessible by Authenticated Users)
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

	return r
}

// NewServer builds an http.Server bound to the config address with the assembled router.
func NewServer(cfg *config.Config, handler http.Handler) *http.Server {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // Streaming SSE requires no write timeout
		IdleTimeout:  120 * time.Second,
	}
}