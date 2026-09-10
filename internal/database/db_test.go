package database

import (
	"path/filepath"
	"testing"
)

func TestInitDB(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "sub", "test.db")

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	// Verify database connection works
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping failed: %v", err)
	}

	// Verify tables were created by migration
	tables := []string{"users", "api_keys", "request_logs", "sessions", "settings", "login_attempts", "transactions", "token_packages"}
	for _, table := range tables {
		var count int
		query := "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?"
		err := db.QueryRow(query, table).Scan(&count)
		if err != nil {
			t.Fatalf("failed to query table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("expected table %s to exist, but count was %d", table, count)
		}
	}

	// Verify journal_mode is WAL
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode 'wal', got '%s'", journalMode)
	}
}
