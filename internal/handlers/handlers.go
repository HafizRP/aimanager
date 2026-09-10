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

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"9router-gateway/embeds"
	"9router-gateway/internal/config"
	"9router-gateway/internal/models"
	"9router-gateway/internal/repository"
	"9router-gateway/internal/syncer"
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
	cfg       *config.Config
	repo      repository.Repository
	syncer    *syncer.Syncer
	templates map[string]*template.Template
}

func NewHandler(cfg *config.Config, repo repository.Repository, sync *syncer.Syncer) (*Handler, error) {
	h := &Handler{
		cfg:       cfg,
		repo:      repo,
		syncer:    sync,
		templates: make(map[string]*template.Template),
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
			fallbackStr := t.Format("2006-01-02 15:04:05")
			return template.HTML(fmt.Sprintf(`<span class="local-time" data-utc="%s">%s</span>`, utcStr, fallbackStr))
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
	}

	pages := []string{"dashboard.html", "users.html", "keys.html", "logs.html", "models.html", "settings.html", "profile.html"}
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

const sessionCookieName = "gw_admin_session"

func (h *Handler) setSessionCookie(w http.ResponseWriter, user *models.User) {
	payload := fmt.Sprintf("%s:%s:%s", user.ID, user.Username, user.Role)
	sig := h.signSession(payload)
	cookieVal := payload + ":" + sig
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    cookieVal,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7, // 7 days
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
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
	parts := strings.Split(cookie.Value, ":")
	if len(parts) < 2 {
		return nil
	}

	// Handle 4-part cookie (userID:username:role:signature)
	if len(parts) == 4 {
		userID := parts[0]
		payload := fmt.Sprintf("%s:%s:%s", parts[0], parts[1], parts[2])
		expectedSig := h.signSession(payload)
		if !hmac.Equal([]byte(parts[3]), []byte(expectedSig)) {
			return nil
		}
		u, err := h.repo.GetUserByID(r.Context(), userID)
		if err == nil && u != nil && u.IsActive {
			return u
		}
		return nil
	}

	// Legacy 2-part cookie (username:signature)
	if len(parts) == 2 {
		username := parts[0]
		expectedSig := h.signSession(username)
		if !hmac.Equal([]byte(parts[1]), []byte(expectedSig)) {
			return nil
		}
		u, err := h.repo.GetUserByUsername(r.Context(), username)
		if err == nil && u != nil && u.IsActive {
			return u
		}
		// Fallback admin
		if username == "admin" || username == h.cfg.AdminUsername {
			return &models.User{
				ID:       "admin",
				Username: "admin",
				Name:     "Administrator",
				Role:     "admin",
				IsActive: true,
			}
		}
	}

	return nil
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

	if username == "" || password == "" {
		http.Redirect(w, r, "/login?error=Username+and+password+are+required", http.StatusSeeOther)
		return
	}

	ctx := r.Context()

	// 1. Check user in database
	user, err := h.repo.GetUserByUsername(ctx, username)
	if err == nil && user != nil {
		if !user.IsActive {
			http.Redirect(w, r, "/login?error=Account+is+suspended.+Please+contact+administrator.", http.StatusSeeOther)
			return
		}

		valid := CheckPasswordHash(password, user.PasswordHash)
		// Auto-migrate blank password for existing users with standard initial password
		if !valid && user.PasswordHash == "" && (password == h.cfg.AdminPassword || password == "password123" || password == user.Username) {
			valid = true
			newHash, _ := HashPassword(password)
			_ = h.repo.UpdateUserPassword(ctx, user.ID, newHash)
		}

		if valid {
			_ = h.repo.UpdateUserLastLogin(ctx, user.ID)
			h.setSessionCookie(w, user)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	// 2. Check bootstrap admin fallback
	if (username == h.cfg.AdminUsername || username == "admin") && password == h.cfg.AdminPassword {
		adminUser, _ := h.repo.GetUserByUsername(ctx, "admin")
		if adminUser == nil {
			hash, _ := HashPassword(h.cfg.AdminPassword)
			adminUser = &models.User{
				ID:            uuid.New().String(),
				Username:      "admin",
				Name:          "System Administrator",
				PasswordHash:  hash,
				Role:          "admin",
				TokenQuota:    0,
				AllowedModels: `["*"]`,
				IsActive:      true,
			}
			_ = h.repo.CreateUser(ctx, adminUser)
		}
		_ = h.repo.UpdateUserLastLogin(ctx, adminUser.ID)
		h.setSessionCookie(w, adminUser)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/login?error=Invalid+username+or+password", http.StatusSeeOther)
}

func (h *Handler) LogoutPost(w http.ResponseWriter, r *http.Request) {
	h.clearSessionCookie(w)
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
