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

	"9router-gateway/embeds"
	"9router-gateway/internal/config"
	"9router-gateway/internal/models"
	"9router-gateway/internal/repository"
)

type UpstreamModelItem struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

type Handler struct {
	cfg       *config.Config
	repo      repository.Repository
	templates map[string]*template.Template
}

func NewHandler(cfg *config.Config, repo repository.Repository) (*Handler, error) {
	h := &Handler{
		cfg:       cfg,
		repo:      repo,
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
		"formatDate": func(v interface{}) string {
			if v == nil {
				return "Never"
			}
			switch t := v.(type) {
			case time.Time:
				if t.IsZero() {
					return "-"
				}
				return t.Format("2006-01-02 15:04:05")
			case *time.Time:
				if t == nil || t.IsZero() {
					return "Never"
				}
				return t.Format("2006-01-02 15:04:05")
			}
			return "-"
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
		"sub": func(a, b int) int { return a - b },
	}

	pages := []string{"dashboard.html", "users.html", "keys.html", "logs.html", "models.html", "settings.html"}
	for _, page := range pages {
		tmpl, err := template.New("").Funcs(funcMap).ParseFS(embeds.FS, "templates/base.html", "templates/"+page)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", page, err)
		}
		h.templates[page] = tmpl
	}

	// Login template (no base layout)
	loginTmpl, err := template.New("login.html").Funcs(funcMap).ParseFS(embeds.FS, "templates/login.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse login template: %w", err)
	}
	h.templates["login.html"] = loginTmpl

	return h, nil
}

func (h *Handler) render(w http.ResponseWriter, tmplName, layoutName string, data map[string]interface{}) {
	tmpl, ok := h.templates[tmplName]
	if !ok {
		http.Error(w, "Template not found: "+tmplName, http.StatusInternalServerError)
		return
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

func (h *Handler) setSessionCookie(w http.ResponseWriter, username string) {
	sig := h.signSession(username)
	cookieVal := username + ":" + sig
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

func (h *Handler) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	parts := strings.SplitN(cookie.Value, ":", 2)
	if len(parts) != 2 {
		return false
	}
	expectedSig := h.signSession(parts[0])
	return hmac.Equal([]byte(parts[1]), []byte(expectedSig))
}

func (h *Handler) signSession(data string) string {
	mac := hmac.New(sha256.New, []byte(h.cfg.SessionSecret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// RequireAuth Middleware
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.isAuthenticated(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Auth Handlers

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.isAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	errMsg := r.URL.Query().Get("error")
	h.render(w, "login.html", "", map[string]interface{}{
		"ErrorMsg": errMsg,
	})
}

func (h *Handler) LoginPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))

	if (username == h.cfg.AdminUsername || username == "admin") && password == h.cfg.AdminPassword {
		h.setSessionCookie(w, username)
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
