package v1

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"9router-gateway/internal/config"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/proxy"
	"9router-gateway/internal/repository"
	"9router-gateway/internal/syncer"
	"9router-gateway/internal/upstream"
	"9router-gateway/internal/usecase"
	"9router-gateway/web"
)

type contextKey string

const userContextKey = contextKey("current_user")

// GetUserFromContext returns the authenticated user stored in the request context.
func GetUserFromContext(ctx context.Context) *entity.User {
	if u, ok := ctx.Value(userContextKey).(*entity.User); ok {
		return u
	}
	return nil
}

// UpstreamModelItem is a lightweight model descriptor from the upstream catalog.
type UpstreamModelItem struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

// Handler is the thin HTTP layer. Domain logic lives in usecase.* services.
type Handler struct {
	cfg          *config.Config
	repo         repository.Repository
	syncer       *syncer.Syncer
	quotaManager *upstream.QuotaManager
	coreClient   *upstream.CoreClient
	cache        *proxy.ResponseCache
	templates    map[string]*template.Template
	httpClient   *http.Client

	// Upstream model list cache (60s TTL): /v1/models rarely changes and
	// every ModelsPage render used to block on it.
	modelsMu    sync.RWMutex
	modelsCache []UpstreamModelItem
	modelsTime  time.Time

	// Use case services
	auth     *usecase.AuthService
	users    *usecase.UserService
	keys     *usecase.KeyService
	dash     *usecase.DashboardService
	billing  *usecase.BillingService
	logs     *usecase.LogService
	settings *usecase.SettingsService
}

// NewHandler wires the HTTP layer to use case services and pre-parses templates.
func NewHandler(cfg *config.Config, repo repository.Repository, sync *syncer.Syncer, quotaMgr *upstream.QuotaManager) (*Handler, error) {
	h := &Handler{
		cfg:          cfg,
		repo:         repo,
		syncer:       sync,
		quotaManager: quotaMgr,
		coreClient:   upstream.NewCoreClient(cfg),
		cache:        proxy.NewResponseCache(),
		templates:    make(map[string]*template.Template),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	// Wire use case services (domain layer)
	h.auth = usecase.NewAuthService(repo, cfg.SessionSecret, cfg.AdminUsername, cfg.AdminPassword)
	h.users = usecase.NewUserService(repo, sync)
	h.keys = usecase.NewKeyService(repo, sync)
	h.dash = usecase.NewDashboardService(repo)
	h.billing = usecase.NewBillingService(repo)
	h.logs = usecase.NewLogService(repo)
	h.settings = usecase.NewSettingsService(repo, &configAdapter{cfg: cfg}, sync)

	funcMap := template.FuncMap{
		"formatNumber": func(v interface{}) string {
			var n int64
			switch val := v.(type) {
			case int64:
				n = val
			case int:
				n = int64(val)
			default:
				return "0"
			}
			if n >= 1000000 {
				return fmt.Sprintf("%.2fM", float64(n)/1000000.0)
			} else if n >= 1000 {
				return fmt.Sprintf("%.1fk", float64(n)/1000.0)
			}
			return strconv.FormatInt(n, 10)
		},
		"formatDate": func(v interface{}) template.HTML {
			if v == nil {
				return template.HTML(`<span class="text-secondary opacity-60">Never</span>`)
			}
			var t time.Time
			switch val := v.(type) {
			case time.Time:
				t = val
			case *time.Time:
				if val == nil || val.IsZero() {
					return template.HTML(`<span class="text-secondary opacity-60">Never</span>`)
				}
				t = *val
			default:
				return template.HTML("-")
			}
			if t.IsZero() {
				return template.HTML("-")
			}
			utcStr := t.UTC().Format(time.RFC3339)
			loc, err := time.LoadLocation("Asia/Jakarta")
			if err != nil {
				loc = time.FixedZone("WIB", 7*3600)
			}
			displayStr := t.In(loc).Format("2006-01-02 15:04:05")
			return template.HTML(fmt.Sprintf(`<span class="local-time font-monospace" data-utc="%s">%s</span>`, utcStr, displayStr))
		},
		"maskKey": func(k string) string {
			if len(k) <= 12 {
				return k
			}
			return k[:8] + "..." + k[len(k)-4:]
		},
		"toInt64": func(v int) int64 {
			return int64(v)
		},
		"calcPct": func(val, max int64) int {
			if max <= 0 {
				return 0
			}
			p := int(float64(val) / float64(max) * 100.0)
			if p > 100 {
				return 100
			}
			if p < 0 {
				return 0
			}
			return p
		},
		"containsWildcard": func(raw string) bool {
			return strings.Contains(raw, "*")
		},
		"countAllowed": func(raw string) int {
			var list []string
			if err := json.Unmarshal([]byte(raw), &list); err == nil {
				return len(list)
			}
			parts := strings.Split(raw, ",")
			return len(parts)
		},
		"countUsersAllowed": func(users []entity.User, modelID string) int {
			c := 0
			for _, u := range users {
				if strings.Contains(u.AllowedModels, "*") || strings.Contains(u.AllowedModels, modelID) {
					c++
				}
			}
			return c
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b interface{}) int64 {
			var na, nb int64
			switch v := a.(type) {
			case int64:
				na = v
			case int:
				na = int64(v)
			}
			switch v := b.(type) {
			case int64:
				nb = v
			case int:
				nb = int64(v)
			}
			return na - nb
		},
		"formatFloat1": func(v float64) string {
			return fmt.Sprintf("%.1f", v)
		},
		"pctColorClass": func(pct float64, status string) string {
			if status == "exhausted" || (pct <= 0.5 && status != "unlimited") {
				return "badge-rose text-danger"
			} else if pct < 30.0 || status == "partial" {
				return "badge-amber text-warning"
			}
			return "badge-emerald text-emerald"
		},
		"pctBarColor": func(pct float64, status string) string {
			if status == "exhausted" || (pct <= 0.5 && status != "unlimited") {
				return "bg-danger"
			} else if pct < 30.0 || status == "partial" {
				return "bg-warning"
			}
			return "bg-success"
		},
	}

	pages := []string{
		"dashboard.html", "users.html", "user_detail.html", "keys.html", "logs.html",
		"settings.html", "billing.html", "providers.html", "combos.html", "token_saver.html",
		"models.html", "chat.html", "cli_tools.html", "proxy_pools.html", "benchmark.html",
		"cache_analytics.html", "skills.html", "endpoint.html", "profile.html", "quota.html",
		"console_log.html", "usage.html", "nodes.html", "mitm.html", "mcp.html",
		"media.html", "pxpipe.html", "translator.html", "pricing.html",
		}
	for _, page := range pages {
		tmpl, err := template.New("").Funcs(funcMap).ParseFS(web.FS, "templates/base.html", "templates/"+page)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", page, err)
		}
		h.templates[page] = tmpl
	}

	// Login template (standalone)
	loginTmpl, err := template.New("login.html").Funcs(funcMap).ParseFS(web.FS, "templates/login.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse login template: %w", err)
	}
	h.templates["login.html"] = loginTmpl

	return h, nil
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, tmplName, layoutName string, data map[string]interface{}) {
	tmpl, ok := h.templates[tmplName]
	if !ok {
		http.Error(w, "Template not found: "+tmplName, http.StatusInternalServerError)
		return
	}

	// Inject CurrentUser and CSRF token into data
	currentUser := GetUserFromContext(r.Context())
	data["CurrentUser"] = currentUser
	data["CSRFToken"] = h.getCSRFToken(r)
	if currentUser != nil {
		data["IsAdmin"] = currentUser.IsAdmin()
		data["LoggedIn"] = true
	} else {
		data["IsAdmin"] = false
		data["LoggedIn"] = false
	}

	var buf bytes.Buffer
	var err error
	if layoutName != "" {
		err = tmpl.ExecuteTemplate(&buf, layoutName, data)
	} else {
		err = tmpl.Execute(&buf, data)
	}

	if err != nil {
		log.Error().Str("tmpl", tmplName).Err(err).Msg("Template execution failed")
		http.Error(w, "Internal rendering error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// Session Auth Helper

const sessionCookieName = "gw_session"

func (h *Handler) setSessionCookie(w http.ResponseWriter, r *http.Request, user *entity.User) {
	token, _, err := h.auth.StartSession(r.Context(), user.ID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to store session in database")
		return
	}

	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7, // 7 days
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		_ = h.auth.DeleteSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (h *Handler) getSessionUser(r *http.Request) *entity.User {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	user, err := h.auth.GetSessionUser(r.Context(), cookie.Value)
	if err != nil {
		return nil
	}
	return user
}

func (h *Handler) getCSRFToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	if h.auth != nil {
		return h.auth.SignSession("csrf:" + cookie.Value)
	}
	// Fallback for tests / uninitialized auth service
	return usecase.SignSession(h.cfg.SessionSecret, "csrf:"+cookie.Value)
}

// ValidateCSRF Middleware enforces CSRF validation on mutating HTTP methods for authenticated sessions
func (h *Handler) ValidateCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete || r.Method == http.MethodPatch {
			expectedToken := h.getCSRFToken(r)
			if expectedToken != "" {
				receivedToken := r.Header.Get("X-CSRF-Token")
				if receivedToken == "" {
					receivedToken = r.FormValue("csrf_token")
				}
				if receivedToken == "" || subtle.ConstantTimeCompare([]byte(expectedToken), []byte(receivedToken)) != 1 {
					log.Warn().Str("path", r.URL.Path).Str("method", r.Method).Msg("CSRF token validation failed")
					http.Error(w, "Invalid or missing CSRF token", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func safeRedirectURL(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.Contains(raw, `\`) {
		return fallback
	}
	return raw
}

// RequireAuth Middleware
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := h.getSessionUser(r)
		if user == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin Middleware
func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUserFromContext(r.Context())
		if user == nil || !user.IsAdmin() {
			http.Redirect(w, r, "/?error=Access+denied.+Administrator+privileges+required.", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Auth Handlers

// LoginPage renders the login form.
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if user := h.getSessionUser(r); user != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	errMsg := r.URL.Query().Get("error")
	h.render(w, r, "login.html", "", map[string]interface{}{
		"ErrorMsg": errMsg,
	})
}

// LoginPost authenticates the submitted credentials and starts a session.
func (h *Handler) LoginPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))

	clientIP := proxy.GetClientIP(r)

	user, err := h.auth.Authenticate(r.Context(), usecase.AuthInput{
		Username: username,
		Password: password,
	}, clientIP)
	if err != nil {
		http.Redirect(w, r, "/login?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.setSessionCookie(w, r, user)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// LogoutPost ends the current session and clears the cookie.
func (h *Handler) LogoutPost(w http.ResponseWriter, r *http.Request) {
	h.clearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// FetchUpstreamModels returns the cached list of upstream model IDs.
func (h *Handler) FetchUpstreamModels(ctx context.Context) []string {
	items, err := h.fetchUpstreamModels(ctx)
	if err != nil {
		return nil
	}
	var modelIDs []string
	for _, m := range items {
		modelIDs = append(modelIDs, m.ID)
	}
	return modelIDs
}

// GenerateSecureAPIKey delegates to the use case package.
func GenerateSecureAPIKey(prefix string) string {
	return usecase.GenerateSecureAPIKey(prefix)
}

// GenerateRandomPassword delegates to the use case package.
func GenerateRandomPassword(length int) string {
	return usecase.GenerateRandomPassword(length)
}

// HashPassword hashes a plaintext password with bcrypt.
func HashPassword(password string) (string, error) {
	return usecase.HashPassword(password)
}

// CheckPasswordHash compares a plaintext password against a bcrypt hash.
func CheckPasswordHash(password, hash string) bool {
	return usecase.CheckPasswordHash(password, hash)
}

func (h *Handler) deriveCurrentBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if xfh := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); xfh != "" {
		if parts := strings.Split(xfh, ","); len(parts) > 0 {
			host = strings.TrimSpace(parts[0])
		}
	}
	return fmt.Sprintf("%s://%s/v1", scheme, host)
}

func (h *Handler) fetchUpstreamModels(ctx context.Context) ([]UpstreamModelItem, error) {
	h.modelsMu.RLock()
	if time.Since(h.modelsTime) < 60*time.Second && h.modelsCache != nil {
		cached := h.modelsCache
		h.modelsMu.RUnlock()
		return cached, nil
	}
	h.modelsMu.RUnlock()

	items, err := h.refreshUpstreamModels(ctx)
	if err != nil {
		return nil, err
	}
	return items, nil
}

// WarmUpstreamModels forces a model list refresh (startup cache priming).
func (h *Handler) WarmUpstreamModels(ctx context.Context) ([]UpstreamModelItem, error) {
	return h.refreshUpstreamModels(ctx)
}

func (h *Handler) refreshUpstreamModels(ctx context.Context) ([]UpstreamModelItem, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.GetUpstreamURL()+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	authKey := h.cfg.GetUpstreamAPIKey()
	// Fallback to a synced gateway admin key if no master upstream key is configured
	if authKey == "" {
		if users, err := h.repo.GetAllUsers(ctx); err == nil {
			for _, u := range users {
				if u.IsAdmin() {
					if keys, err := h.repo.GetAPIKeysByUserID(ctx, u.ID); err == nil {
						for _, k := range keys {
							if k.IsActive && strings.HasPrefix(k.Key, "sk-gw-admin-") {
								authKey = k.Key
								break
							}
						}
					}
				}
				if authKey != "" {
					break
				}
			}
		}
	}
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res struct {
		Data []UpstreamModelItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	h.modelsMu.Lock()
	h.modelsCache = res.Data
	h.modelsTime = time.Now()
	h.modelsMu.Unlock()
	return res.Data, nil
}