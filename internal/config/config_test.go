package config

import (
	"os"
	"sync"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	// Clean relevant env vars for clean test
	envVars := []string{
		"PORT", "HOST", "DB_PATH", "ADMIN_USERNAME",
		"ADMIN_PASSWORD", "SESSION_SECRET", "MIDTRANS_IS_PRODUCTION",
	}
	for _, v := range envVars {
		orig := os.Getenv(v)
		defer os.Setenv(v, orig)
		os.Unsetenv(v)
	}

	cfg := LoadConfig()

	if cfg.Port != 20129 {
		t.Errorf("expected default Port 20129, got %d", cfg.Port)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("expected default Host '0.0.0.0', got '%s'", cfg.Host)
	}
	if cfg.UpstreamURL != "http://127.0.0.1:20128" {
		t.Errorf("expected default UpstreamURL 'http://127.0.0.1:20128', got '%s'", cfg.UpstreamURL)
	}
	if cfg.AdminUsername != "admin" {
		t.Errorf("expected default AdminUsername 'admin', got '%s'", cfg.AdminUsername)
	}
	if cfg.MidtransIsProduction != false {
		t.Errorf("expected default MidtransIsProduction false, got true")
	}
}

func TestConfigEnvOverrides(t *testing.T) {
	os.Setenv("PORT", "29999")
	os.Setenv("HOST", "127.0.0.1")
	os.Setenv("ADMIN_USERNAME", "custom_admin")
	os.Setenv("MIDTRANS_IS_PRODUCTION", "true")

	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("HOST")
		os.Unsetenv("ADMIN_USERNAME")
		os.Unsetenv("MIDTRANS_IS_PRODUCTION")
	}()

	cfg := LoadConfig()

	if cfg.Port != 29999 {
		t.Errorf("expected overridden Port 29999, got %d", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("expected overridden Host '127.0.0.1', got '%s'", cfg.Host)
	}
	if cfg.AdminUsername != "custom_admin" {
		t.Errorf("expected overridden AdminUsername 'custom_admin', got '%s'", cfg.AdminUsername)
	}
	if !cfg.MidtransIsProduction {
		t.Errorf("expected MidtransIsProduction true, got false")
	}
}

func TestConfigDataDir(t *testing.T) {
	os.Setenv("NINEROUTER_DATA_DIR", "/custom/core/data")

	defer func() {
		os.Unsetenv("NINEROUTER_DATA_DIR")
	}()

	cfg := LoadConfig()

	if cfg.NineRouterDataDir != "/custom/core/data" {
		t.Errorf("expected NineRouterDataDir '/custom/core/data', got '%s'", cfg.NineRouterDataDir)
	}

	dirs := cfg.GetCoreDataDirs()
	if len(dirs) < 2 {
		t.Errorf("expected at least 2 directories, got %v", dirs)
	}
	if dirs[0] != "/custom/core/data" {
		t.Errorf("expected first dir to be '/custom/core/data', got %s", dirs[0])
	}
}

func TestConfigUpstreamGettersSettersConcurrent(t *testing.T) {
	cfg := &Config{
		UpstreamURL:      "http://127.0.0.1:20128",
		UpstreamAPIKey:   "init-key",
		NineRouterDBPath: "./data/core/db/data.sqlite",
	}

	// Verify initial getters
	if cfg.GetUpstreamURL() != "http://127.0.0.1:20128" {
		t.Errorf("unexpected UpstreamURL: %s", cfg.GetUpstreamURL())
	}
	if cfg.GetUpstreamAPIKey() != "init-key" {
		t.Errorf("unexpected UpstreamAPIKey: %s", cfg.GetUpstreamAPIKey())
	}
	if cfg.GetNineRouterDBPath() != "./data/core/db/data.sqlite" {
		t.Errorf("unexpected NineRouterDBPath: %s", cfg.GetNineRouterDBPath())
	}

	// Verify setters trim slashes and whitespace
	cfg.SetUpstreamURL("  http://192.168.1.100:20128/  ")
	if cfg.GetUpstreamURL() != "http://192.168.1.100:20128" {
		t.Errorf("expected trimmed URL, got: %s", cfg.GetUpstreamURL())
	}

	cfg.SetUpstreamAPIKey("  secret-key-123  ")
	if cfg.GetUpstreamAPIKey() != "secret-key-123" {
		t.Errorf("expected trimmed key, got: %s", cfg.GetUpstreamAPIKey())
	}

	cfg.SetNineRouterDBPath("  /custom/db.sqlite  ")
	if cfg.GetNineRouterDBPath() != "/custom/db.sqlite" {
		t.Errorf("expected trimmed db path, got: %s", cfg.GetNineRouterDBPath())
	}

	// Concurrency test for race detector
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cfg.SetUpstreamURL("http://127.0.0.1:20128")
			_ = cfg.GetUpstreamURL()
			cfg.SetUpstreamAPIKey("some-key")
			_ = cfg.GetUpstreamAPIKey()
			cfg.SetNineRouterDBPath("./test.sqlite")
			_ = cfg.GetNineRouterDBPath()
			_ = cfg.GetCoreDataDirs()
		}()
		go func() {
			defer wg.Done()
			_ = cfg.GetUpstreamURL()
			_ = cfg.GetUpstreamAPIKey()
			_ = cfg.GetNineRouterDBPath()
			_ = cfg.GetCoreDataDirs()
		}()
	}
	wg.Wait()
}

