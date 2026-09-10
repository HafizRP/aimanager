package syncer

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"9router-gateway/internal/models"
	_ "modernc.org/sqlite"
)

type Syncer struct {
	dbPath    string
	machineID string
}

func NewSyncer(dbPath string) *Syncer {
	machineID := "33b8f86c23c91fec"
	// Try reading machineId from /home/b14/9router/data/machine-id
	if bytes, err := os.ReadFile("/home/b14/9router/data/machine-id"); err == nil {
		content := strings.TrimSpace(string(bytes))
		if len(content) >= 16 {
			machineID = content[:16]
		}
	}

	return &Syncer{
		dbPath:    dbPath,
		machineID: machineID,
	}
}

func (s *Syncer) UpdateDBPath(newPath string) {
	s.dbPath = newPath
}

func (s *Syncer) getDB() (*sql.DB, error) {
	if _, err := os.Stat(s.dbPath); err != nil {
		return nil, fmt.Errorf("9router db not found at %s: %w", s.dbPath, err)
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", s.dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return db, nil
}

// SyncKey inserts or updates an API key in 9router Core's database
func (s *Syncer) SyncKey(key *models.APIKey, userName string) error {
	db, err := s.getDB()
	if err != nil {
		slog.Warn("9router sync skipped (cannot open db)", "err", err)
		return err
	}
	defer db.Close()

	name := key.Name
	if userName != "" {
		name = fmt.Sprintf("[%s] %s", userName, key.Name)
	}

	activeInt := 0
	if key.IsActive {
		activeInt = 1
	}

	createdAtStr := key.CreatedAt.UTC().Format(time.RFC3339Nano)
	if key.CreatedAt.IsZero() {
		createdAtStr = time.Now().UTC().Format(time.RFC3339Nano)
	}

	query := `INSERT OR REPLACE INTO apiKeys (id, key, name, machineId, isActive, createdAt)
	          VALUES (?, ?, ?, ?, ?, ?)`
	_, err = db.Exec(query, key.ID, key.Key, name, s.machineID, activeInt, createdAtStr)
	if err != nil {
		slog.Error("Failed to sync key to 9router db", "key_id", key.ID, "err", err)
		return err
	}

	slog.Info("Synced API key to 9router core database", "key_id", key.ID, "name", name)
	return nil
}

// ToggleKey updates the isActive flag in 9router Core's database
func (s *Syncer) ToggleKey(keyID string, isActive bool) error {
	db, err := s.getDB()
	if err != nil {
		return err
	}
	defer db.Close()

	activeInt := 0
	if isActive {
		activeInt = 1
	}

	query := `UPDATE apiKeys SET isActive = ? WHERE id = ?`
	_, err = db.Exec(query, activeInt, keyID)
	return err
}

// DeleteKey removes an API key from 9router Core's database
func (s *Syncer) DeleteKey(keyID string) error {
	db, err := s.getDB()
	if err != nil {
		return err
	}
	defer db.Close()

	query := `DELETE FROM apiKeys WHERE id = ?`
	_, err = db.Exec(query, keyID)
	return err
}

// BackfillAll syncs all gateway keys into 9router Core's database
func (s *Syncer) BackfillAll(keys []models.APIKey) error {
	db, err := s.getDB()
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO apiKeys (id, key, name, machineId, isActive, createdAt) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, k := range keys {
		name := k.Name
		if k.UserName != "" {
			name = fmt.Sprintf("[%s] %s", k.UserName, k.Name)
		}
		activeInt := 0
		if k.IsActive {
			activeInt = 1
		}
		createdAtStr := k.CreatedAt.UTC().Format(time.RFC3339Nano)
		if k.CreatedAt.IsZero() {
			createdAtStr = time.Now().UTC().Format(time.RFC3339Nano)
		}
		_, _ = stmt.Exec(k.ID, k.Key, name, s.machineID, activeInt, createdAtStr)
	}

	return tx.Commit()
}
