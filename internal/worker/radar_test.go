package worker

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"9router-gateway/internal/repository"

	_ "modernc.org/sqlite"
)

func setupRadarTestDB(t *testing.T) (repository.Repository, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_radar.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
	}

	schema := `
	CREATE TABLE users (
		id TEXT PRIMARY KEY,
		username TEXT,
		name TEXT,
		role TEXT,
		password_hash TEXT,
		token_quota INTEGER DEFAULT 0,
		tokens_used INTEGER DEFAULT 0,
		daily_token_quota INTEGER DEFAULT 0,
		allowed_models TEXT,
		rate_limit_rpm INTEGER DEFAULT 0,
		rate_limit_tpm INTEGER DEFAULT 0,
		is_active INTEGER DEFAULT 1,
		created_at TEXT,
		updated_at TEXT
	);

	CREATE TABLE api_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		name TEXT,
		key TEXT,
		allowed_models TEXT,
		allowed_ips TEXT,
		rate_limit_rpm INTEGER DEFAULT 0,
		max_tokens_limit INTEGER DEFAULT 0,
		daily_token_quota INTEGER DEFAULT 0,
		is_active INTEGER DEFAULT 1,
		created_at TEXT,
		updated_at TEXT
	);

	CREATE TABLE request_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key_id TEXT,
		user_id TEXT,
		model TEXT,
		path TEXT,
		status_code INTEGER,
		duration_ms INTEGER,
		total_tokens INTEGER,
		client_ip TEXT,
		created_at TEXT
	);

	CREATE TABLE security_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT,
		user_id TEXT,
		api_key_id TEXT,
		detail TEXT,
		action TEXT,
		created_at TEXT
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	repo := repository.NewSQLiteRepo(db)
	return repo, db
}

func TestRadar_Scan_WhitelistAdminAndHermes(t *testing.T) {
	repo, db := setupRadarTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 1. Insert admin user and 'hermes' key
	_, err := db.Exec(`INSERT INTO users (id, username, name, role, is_active) VALUES ('u-admin', 'admin', 'Admin', 'admin', 1)`)
	if err != nil {
		t.Fatalf("insert admin failed: %v", err)
	}
	_, err = db.Exec(`INSERT INTO api_keys (id, user_id, name, key, is_active) VALUES ('k-hermes', 'u-admin', 'hermes', 'sk-hermes', 1)`)
	if err != nil {
		t.Fatalf("insert hermes key failed: %v", err)
	}

	// 2. Insert normal user and normal key
	_, err = db.Exec(`INSERT INTO users (id, username, name, role, is_active) VALUES ('u-normal', 'john', 'John Doe', 'user', 1)`)
	if err != nil {
		t.Fatalf("insert normal user failed: %v", err)
	}
	_, err = db.Exec(`INSERT INTO api_keys (id, user_id, name, key, is_active) VALUES ('k-normal', 'u-normal', 'regular-key', 'sk-regular', 1)`)
	if err != nil {
		t.Fatalf("insert regular key failed: %v", err)
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	// 3. Inject failing request logs for hermes key (25 failed requests out of 25 = 100% error rate)
	for i := 0; i < 25; i++ {
		_, err = db.Exec(`INSERT INTO request_logs (api_key_id, user_id, model, path, status_code, duration_ms, total_tokens, client_ip, created_at)
			VALUES ('k-hermes', 'u-admin', 'm', '/v1', 504, 10, 10, '127.0.0.1', ?)`, now)
		if err != nil {
			t.Fatalf("insert log failed: %v", err)
		}
	}

	// 4. Inject failing request logs for normal key (25 failed requests out of 25)
	for i := 0; i < 25; i++ {
		_, err = db.Exec(`INSERT INTO request_logs (api_key_id, user_id, model, path, status_code, duration_ms, total_tokens, client_ip, created_at)
			VALUES ('k-normal', 'u-normal', 'm', '/v1', 504, 10, 10, '127.0.0.1', ?)`, now)
		if err != nil {
			t.Fatalf("insert log failed: %v", err)
		}
	}

	radar := NewRadar(repo, nil)

	// Run radar scan
	radar.scan()

	// 5. Verify hermes key is STILL ACTIVE (whitelisted)
	var hermesActive int
	err = db.QueryRow(`SELECT is_active FROM api_keys WHERE id = 'k-hermes'`).Scan(&hermesActive)
	if err != nil || hermesActive != 1 {
		t.Fatalf("expected k-hermes to remain active (1), got %d (err: %v)", hermesActive, err)
	}

	// 6. Verify normal key was AUTO-DISABLED
	var normalActive int
	err = db.QueryRow(`SELECT is_active FROM api_keys WHERE id = 'k-normal'`).Scan(&normalActive)
	if err != nil || normalActive != 0 {
		t.Fatalf("expected k-normal to be disabled (0), got %d (err: %v)", normalActive, err)
	}

	// 7. Verify security event actions
	events, err := repo.ListSecurityEvents(ctx, 10)
	if err != nil {
		t.Fatalf("list security events failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 security events, got %d", len(events))
	}

	for _, ev := range events {
		if ev.APIKeyID == "k-hermes" {
			if ev.Action != "alerted" || !strings.Contains(ev.Detail, "whitelisted") {
				t.Fatalf("expected k-hermes security event to be alerted/whitelisted, got: %+v", ev)
			}
		}
		if ev.APIKeyID == "k-normal" {
			if ev.Action != "key auto-disabled" {
				t.Fatalf("expected k-normal security event to be 'key auto-disabled', got: %+v", ev)
			}
		}
	}
}
