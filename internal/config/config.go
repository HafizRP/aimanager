package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port             int
	Host             string
	UpstreamURL      string
	UpstreamAPIKey   string
	DBPath           string
	NineRouterDBPath string
	AdminUsername    string
	AdminPassword    string
	SessionSecret    string
}

func LoadConfig() *Config {
	// Try loading from .env if present
	loadDotEnv(".env")

	port := getEnvAsInt("PORT", 20129)
	host := getEnv("HOST", "0.0.0.0")
	upstreamURL := getEnv("UPSTREAM_URL", "http://127.0.0.1:20128")
	upstreamAPIKey := getEnv("UPSTREAM_API_KEY", "")
	dbPath := getEnv("DB_PATH", "/home/b14/9router-gateway/data/gateway.db")
	nineRouterDBPath := getEnv("NINEROUTER_DB_PATH", "/home/b14/9router/data/db/data.sqlite")
	adminUsername := getEnv("ADMIN_USERNAME", "admin")
	adminPassword := getEnv("ADMIN_PASSWORD", "admin123")
	sessionSecret := getEnv("SESSION_SECRET", "9router-secret-token-key-change-me")

	return &Config{
		Port:             port,
		Host:             host,
		UpstreamURL:      upstreamURL,
		UpstreamAPIKey:   upstreamAPIKey,
		DBPath:           dbPath,
		NineRouterDBPath: nineRouterDBPath,
		AdminUsername:    adminUsername,
		AdminPassword:    adminPassword,
		SessionSecret:    sessionSecret,
	}
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
