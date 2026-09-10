// Package config loads application configuration from environment variables and .env files.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Config holds all runtime configuration for the 9router Gateway.
type Config struct {
	mu                   sync.RWMutex
	Port                 int
	Host                 string
	UpstreamURL          string
	UpstreamAPIKey       string
	DBPath               string
	NineRouterDBPath     string
	NineRouterDataDir    string
	AdminUsername        string
	AdminPassword        string
	SessionSecret        string
	MidtransServerKey    string
	MidtransClientKey    string
	MidtransIsProduction bool
	MidtransMerchantID   string
}

// LoadConfig reads environment variables and returns a populated Config.
func LoadConfig() *Config {
	// Try loading from .env if present
	loadDotEnv(".env")

	port := getEnvAsInt("PORT", 20129)
	host := getEnv("HOST", "0.0.0.0")

	// Upstream 9router Core default settings (managed via SQLite database)
	upstreamURL := "http://127.0.0.1:20128"
	upstreamAPIKey := ""
	dbPath := getEnv("DB_PATH", "./data/gateway.db")
	nineRouterDBPath := "./data/core/db/data.sqlite"
	nineRouterDataDir := getEnv("NINEROUTER_DATA_DIR", "")

	adminUsername := getEnv("ADMIN_USERNAME", "admin")
	adminPassword := getEnv("ADMIN_PASSWORD", "admin123")
	sessionSecret := getEnv("SESSION_SECRET", "9router-secret-token-key-change-me")

	midtransServerKey := getEnv("MIDTRANS_SERVER_KEY", "SB-Mid-server-demo-key")
	midtransClientKey := getEnv("MIDTRANS_CLIENT_KEY", "SB-Mid-client-demo-key")
	midtransIsProduction := getEnvAsBool("MIDTRANS_IS_PRODUCTION", false)
	midtransMerchantID := getEnv("MIDTRANS_MERCHANT_ID", "")

	return &Config{
		Port:                 port,
		Host:                 host,
		UpstreamURL:          upstreamURL,
		UpstreamAPIKey:       upstreamAPIKey,
		DBPath:               dbPath,
		NineRouterDBPath:     nineRouterDBPath,
		NineRouterDataDir:    nineRouterDataDir,
		AdminUsername:        adminUsername,
		AdminPassword:        adminPassword,
		SessionSecret:        sessionSecret,
		MidtransServerKey:    midtransServerKey,
		MidtransClientKey:    midtransClientKey,
		MidtransIsProduction: midtransIsProduction,
		MidtransMerchantID:   midtransMerchantID,
	}
}

// GetUpstreamURL returns the configured upstream URL in a thread-safe manner.
func (c *Config) GetUpstreamURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.UpstreamURL
}

// SetUpstreamURL sets the upstream URL in a thread-safe manner and strips trailing slashes.
func (c *Config) SetUpstreamURL(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.UpstreamURL = strings.TrimRight(strings.TrimSpace(url), "/")
}

// GetUpstreamAPIKey returns the master upstream API key in a thread-safe manner.
func (c *Config) GetUpstreamAPIKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.UpstreamAPIKey
}

// SetUpstreamAPIKey sets the master upstream API key in a thread-safe manner.
func (c *Config) SetUpstreamAPIKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.UpstreamAPIKey = strings.TrimSpace(key)
}

// GetNineRouterDBPath returns the 9router SQLite database path in a thread-safe manner.
func (c *Config) GetNineRouterDBPath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.NineRouterDBPath
}

// SetNineRouterDBPath sets the 9router SQLite database path in a thread-safe manner.
func (c *Config) SetNineRouterDBPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.NineRouterDBPath = strings.TrimSpace(path)
}

// GetCoreDataDirs returns candidate directories to search for 9router Core files
// (e.g. machine-id, auth/cli-secret), ordered by priority and deduplicated.
func (c *Config) GetCoreDataDirs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var dirs []string
	seen := make(map[string]bool)

	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir != "" && !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}

	if c.NineRouterDataDir != "" {
		add(c.NineRouterDataDir)
	}

	if c.NineRouterDBPath != "" {
		add(filepath.Dir(filepath.Dir(c.NineRouterDBPath)))
		add(filepath.Dir(c.NineRouterDBPath))
	}

	add("./data/core")

	return dirs
}

func getEnvAsBool(key string, defaultVal bool) bool {
	valStr := getEnv(key, "")
	if valStr == "" {
		return defaultVal
	}
	valStr = strings.ToLower(valStr)
	return valStr == "true" || valStr == "1" || valStr == "yes"
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valStr := getEnv(key, "")
	if val, err := strconv.Atoi(valStr); err == nil {
		return val
	}
	return defaultVal
}

func loadDotEnv(filepath string) {
	file, err := os.Open(filepath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			// remove quotes if present
			if (strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"")) ||
				(strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")) {
				v = v[1 : len(v)-1]
			}
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
}
