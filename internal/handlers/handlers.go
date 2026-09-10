package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"9router-gateway/embeds"
	"9router-gateway/internal/config"
	"9router-gateway/internal/models"
	"9router-gateway/internal/repository"
	"9router-gateway/internal/syncer"
	"9router-gateway/internal/upstream"
)

type contextKey string

const userContextKey = contextKey("current_user")

func GetUserFromContext(ctx context.Context) *models.User {
	if u, ok := ctx.Value(userContextKey).(*models.User); ok {
		return u
	}
	return nil
}

type UpstreamModelItem struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

type Handler struct {
	cfg          *config.Config
	repo         repository.Repository
	syncer       *syncer.Syncer
	quotaManager *upstream.QuotaManager
	coreClient   *upstream.CoreClient
	templates    map[string]*template.Template
}

func NewHandler(cfg *config.Config, repo repository.Repository, sync *syncer.Syncer, quotaMgr *upstream.QuotaManager) (*Handler, error) {
	h := &Handler{
		cfg:          cfg,
		repo:         repo,
		syncer:       sync,
		quotaManager: quotaMgr,
		coreClient:   upstream.NewCoreClient(cfg),
		templates:    make(map[string]*template.Template),
	}

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
		"countUsersAllowed": func(users []models.User, modelID string) int {
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
		"dashboard.html", "users.html", "user_detail.html", "keys.html", "logs.html", "models.html",
		"settings.html", "billing.html", "providers.html", "combos.html", "token_saver.html",
		"chat.html", "cli_tools.html", "proxy_pools.html", "benchmark.html",
	}
	for _, page := range pages {
		tmpl, err := template.New("").Funcs(funcMap).ParseFS(embeds.FS, "templates/base.html", "templates/"+page)
		if err != nil {
			// Profile might be optional if merged into settings/users
			if page != "profile.html" {
				return nil, fmt.Errorf("failed to parse template %s: %w", page, err)
			}
			continue
		}
		h.templates[page] = tmpl
	}

	// Login template (standalone)
	loginTmpl, err := template.New("login.html").Funcs(funcMap).ParseFS(embeds.FS, "templates/login.html")
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

	// Inject CurrentUser into data
	currentUser := GetUserFromContext(r.Context())
	data["CurrentUser"] = currentUser
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
		slog.Error("Template execution failed", "tmpl", tmplName, "err", err)
		http.Error(w, "Internal rendering error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// Session Auth Helper

const sessionCookieName = "gw_session"

func (h *Handler) setSessionCookie(w http.ResponseWriter, r *http.Request, user *models.User) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		slog.Error("Failed to generate session token", "err", err)
		return
	}
	token := hex.EncodeToString(tokenBytes)

	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	ctx := r.Context()
	if err := h.repo.CreateSession(ctx, token, user.ID, expiresAt); err != nil {
		slog.Error("Failed to store session in database", "err", err)
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
		_ = h.repo.DeleteSession(r.Context(), cookie.Value)
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

func (h *Handler) getSessionUser(r *http.Request) *models.User {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}

	user, err := h.repo.GetSessionUser(r.Context(), cookie.Value)
	if err != nil || user == nil || !user.IsActive {
		return nil
	}

	return user
}

func (h *Handler) signSession(data string) string {
	mac := hmac.New(sha256.New, []byte(h.cfg.SessionSecret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
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

func (h *Handler) LoginPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))

	clientIP := r.RemoteAddr
	if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
		clientIP = cfIP
	} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		clientIP = strings.Split(xff, ",")[0]
	}
	clientIP = strings.TrimSpace(strings.Split(clientIP, ":")[0])

	ctx := r.Context()

	// Rate limiting: max 5 failed attempts in 15 minutes
	recentFails, _ := h.repo.GetRecentLoginAttempts(ctx, clientIP, 15)
	if recentFails >= 5 {
		http.Redirect(w, r, "/login?error=Too+many+failed+login+attempts.+Please+wait+15+minutes.", http.StatusSeeOther)
		return
	}

	if username == "" || password == "" {
		http.Redirect(w, r, "/login?error=Username+and+password+are+required", http.StatusSeeOther)
		return
	}

	// 1. Authenticate strictly via database user and bcrypt hash
	user, err := h.repo.GetUserByUsername(ctx, username)
	if err != nil || user == nil {
		_ = h.repo.RecordLoginAttempt(ctx, clientIP)
		http.Redirect(w, r, "/login?error=Invalid+username+or+password", http.StatusSeeOther)
		return
	}

	if !user.IsActive {
		http.Redirect(w, r, "/login?error=Account+is+suspended.+Please+contact+administrator.", http.StatusSeeOther)
		return
	}

	valid := CheckPasswordHash(password, user.PasswordHash)
	if !valid {
		_ = h.repo.RecordLoginAttempt(ctx, clientIP)
		http.Redirect(w, r, "/login?error=Invalid+username+or+password", http.StatusSeeOther)
		return
	}

	// Clear failed login attempts and create session
	_ = h.repo.ClearLoginAttempts(ctx, clientIP)
	_ = h.repo.UpdateUserLastLogin(ctx, user.ID)
	h.setSessionCookie(w, r, user)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) LogoutPost(w http.ResponseWriter, r *http.Request) {
	h.clearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) FetchUpstreamModels(ctx context.Context) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.UpstreamURL+"/v1/models", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+h.cfg.UpstreamAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var res struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil
	}

	var modelIDs []string
	for _, m := range res.Data {
		modelIDs = append(modelIDs, m.ID)
	}
	return modelIDs
}

func GenerateSecureAPIKey(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	if prefix == "" {
		prefix = "sk-gw-"
	}
	return prefix + hex.EncodeToString(b)
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	if hash == "" {
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func (h *Handler) deriveCurrentBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	return fmt.Sprintf("%s://%s/v1", scheme, host)
}

func (h *Handler) fetchUpstreamModels(ctx context.Context) ([]UpstreamModelItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.UpstreamURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if h.cfg.UpstreamAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.cfg.UpstreamAPIKey)
	}
	resp, err := http.DefaultClient.Do(req)
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
	return res.Data, nil
}
