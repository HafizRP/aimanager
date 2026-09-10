package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func InitDB(dbPath string) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Optimize connection pool for SQLite
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE,
		name TEXT NOT NULL,
		password_hash TEXT,
		role TEXT NOT NULL DEFAULT 'user',
		token_quota INTEGER NOT NULL DEFAULT 0,
		tokens_used INTEGER NOT NULL DEFAULT 0,
		allowed_models TEXT NOT NULL DEFAULT '["*"]',
		is_active INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_login_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS api_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		key TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		is_active INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_api_keys_key ON api_keys(key);
	CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);

	CREATE TABLE IF NOT EXISTS request_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		api_key_id TEXT NOT NULL,
		path TEXT NOT NULL,
		method TEXT NOT NULL,
		model TEXT NOT NULL,
		is_stream INTEGER NOT NULL DEFAULT 0,
		prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		status_code INTEGER NOT NULL,
		duration_ms INTEGER NOT NULL,
		client_ip TEXT,
		error_message TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at);
	CREATE INDEX IF NOT EXISTS idx_request_logs_user_id ON request_logs(user_id);
	CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS token_packages (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		tokens INTEGER NOT NULL,
		price_idr INTEGER NOT NULL,
		description TEXT,
		is_popular INTEGER NOT NULL DEFAULT 0,
		is_active INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS transactions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		package_id TEXT,
		tokens INTEGER NOT NULL,
		amount_idr INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		payment_type TEXT,
		snap_token TEXT,
		snap_url TEXT,
		midtrans_tx_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		paid_at DATETIME,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_tx_user_id ON transactions(user_id);
	CREATE INDEX IF NOT EXISTS idx_tx_status ON transactions(status);
	CREATE INDEX IF NOT EXISTS idx_tx_created_at ON transactions(created_at);
	`

	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Idempotent column additions for existing tables
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN username TEXT;`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN password_hash TEXT;`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN last_login_at DATETIME;`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username);`)

	// Seed default packages if none exist
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM token_packages").Scan(&count)
	if count == 0 {
		seedPackages := `
		INSERT INTO token_packages (id, name, tokens, price_idr, description, is_popular, is_active) VALUES
		('pkg-starter', 'Starter Pack', 500000, 25000, 'Ideal for testing Cursor & Cline with high-speed models', 0, 1),
		('pkg-pro', 'Pro Developer', 2000000, 80000, 'Best value for active developers & daily coding workflows', 1, 1),
		('pkg-power', 'Power Builder', 10000000, 350000, 'Heavy coding, multi-agent runs, & long contexts', 0, 1),
		('pkg-enterprise', 'Enterprise Tier', 50000000, 1500000, 'Large project migrations, teams & production workloads', 0, 1);
		`
		_, _ = db.Exec(seedPackages)
	}

	return nil
}
