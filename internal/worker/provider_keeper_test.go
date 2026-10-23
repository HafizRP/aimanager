package worker

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	"9router-gateway/internal/config"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/upstream"

	_ "modernc.org/sqlite"
)

type mockLogRepo struct {
	mu     sync.Mutex
	events []*entity.SecurityEvent
}

func (m *mockLogRepo) CreateSecurityEvent(ctx context.Context, ev *entity.SecurityEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

func (m *mockLogRepo) getEvents() []*entity.SecurityEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]*entity.SecurityEvent, len(m.events))
	copy(res, m.events)
	return res
}

func setupTestDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_keeper.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
	}

	createTable := `CREATE TABLE providerConnections (
		id TEXT PRIMARY KEY,
		provider TEXT,
		name TEXT,
		email TEXT,
		isActive INTEGER,
		createdAt TEXT
	);`
	if _, err := db.Exec(createTable); err != nil {
		t.Fatalf("failed to create providerConnections table: %v", err)
	}
	return dbPath, db
}

func TestProviderKeeper_AutoDisableOnConsecutiveFailures(t *testing.T) {
	dbPath, db := setupTestDB(t)
	defer db.Close()

	// Insert active connection
	_, err := db.Exec(`INSERT INTO providerConnections (id, provider, name, email, isActive)
		VALUES ('conn-1', 'antigravity', 'Main AG', 'ag@example.com', 1)`)
	if err != nil {
		t.Fatalf("failed to insert conn: %v", err)
	}

	var mu sync.Mutex
	toggledActive := -1
	toggledID := ""

	mockCore := &upstream.MockCoreClient{
		TestProviderFunc: func(ctx context.Context, id string) (map[string]interface{}, error) {
			// Simulates failing upstream connection
			return map[string]interface{}{"valid": false, "error": "upstream timeout 504"}, nil
		},
		ToggleProviderFunc: func(ctx context.Context, id string, isActive bool) error {
			mu.Lock()
			defer mu.Unlock()
			toggledID = id
			if isActive {
				toggledActive = 1
			} else {
				toggledActive = 0
			}
			return nil
		},
	}

	cfg := &config.Config{NineRouterDBPath: dbPath}
	keeper := NewProviderKeeper(cfg, mockCore, nil)
	keeper.SetFailureThreshold(3)

	repo := &mockLogRepo{}
	keeper.SetLogRepository(repo)

	// Failure 1: not disabled yet
	keeper.checkAndAutoDisable()
	if count := keeper.GetFailureCount("conn-1"); count != 1 {
		t.Fatalf("expected failure count 1, got %d", count)
	}
	mu.Lock()
	if toggledActive != -1 {
		t.Fatalf("expected not toggled yet, got %d", toggledActive)
	}
	mu.Unlock()

	// Failure 2: not disabled yet
	keeper.checkAndAutoDisable()
	if count := keeper.GetFailureCount("conn-1"); count != 2 {
		t.Fatalf("expected failure count 2, got %d", count)
	}

	// Failure 3: reaches threshold -> trips circuit breaker
	keeper.checkAndAutoDisable()
	if count := keeper.GetFailureCount("conn-1"); count != 3 {
		t.Fatalf("expected failure count 3, got %d", count)
	}

	mu.Lock()
	if toggledActive != 0 || toggledID != "conn-1" {
		t.Fatalf("expected conn-1 auto-disabled (isActive=0), got id=%s active=%d", toggledID, toggledActive)
	}
	mu.Unlock()

	// Verify SecurityEvent recorded
	evs := repo.getEvents()
	if len(evs) != 1 {
		t.Fatalf("expected 1 security event, got %d", len(evs))
	}
	if evs[0].Kind != "circuit_breaker" || evs[0].Action != "provider auto-disabled" {
		t.Fatalf("unexpected security event: %+v", evs[0])
	}
}

func TestProviderKeeper_AutoReactivateWhenValid(t *testing.T) {
	dbPath, db := setupTestDB(t)
	defer db.Close()

	// Insert inactive connection
	_, err := db.Exec(`INSERT INTO providerConnections (id, provider, name, email, isActive)
		VALUES ('conn-2', 'kiro', 'Kiro Pro', 'kiro@example.com', 0)`)
	if err != nil {
		t.Fatalf("failed to insert conn: %v", err)
	}

	var mu sync.Mutex
	toggledActive := -1
	toggledID := ""

	mockCore := &upstream.MockCoreClient{
		TestProviderFunc: func(ctx context.Context, id string) (map[string]interface{}, error) {
			// Simulates recovered connection
			return map[string]interface{}{"valid": true}, nil
		},
		ToggleProviderFunc: func(ctx context.Context, id string, isActive bool) error {
			mu.Lock()
			defer mu.Unlock()
			toggledID = id
			if isActive {
				toggledActive = 1
			} else {
				toggledActive = 0
			}
			return nil
		},
	}

	cfg := &config.Config{NineRouterDBPath: dbPath}
	keeper := NewProviderKeeper(cfg, mockCore, nil)
	keeper.RecordProviderFailure(context.Background(), "conn-2", "kiro", "Kiro Pro")

	repo := &mockLogRepo{}
	keeper.SetLogRepository(repo)

	// Run reactivate check
	keeper.checkAndReactivate()

	mu.Lock()
	if toggledActive != 1 || toggledID != "conn-2" {
		t.Fatalf("expected conn-2 reactivated (isActive=1), got id=%s active=%d", toggledID, toggledActive)
	}
	mu.Unlock()

	// Failure count should be reset
	if count := keeper.GetFailureCount("conn-2"); count != 0 {
		t.Fatalf("expected failure count reset to 0, got %d", count)
	}

	// Verify self_heal security event recorded
	evs := repo.getEvents()
	if len(evs) != 1 {
		t.Fatalf("expected 1 security event, got %d", len(evs))
	}
	if evs[0].Kind != "self_heal" || evs[0].Action != "provider auto-reactivated" {
		t.Fatalf("unexpected security event: %+v", evs[0])
	}
}

func TestProviderKeeper_RecordProviderFailureAndSuccess(t *testing.T) {
	var mu sync.Mutex
	toggledActive := -1
	toggledID := ""

	mockCore := &upstream.MockCoreClient{
		ToggleProviderFunc: func(ctx context.Context, id string, isActive bool) error {
			mu.Lock()
			defer mu.Unlock()
			toggledID = id
			if isActive {
				toggledActive = 1
			} else {
				toggledActive = 0
			}
			return nil
		},
	}

	cfg := &config.Config{}
	keeper := NewProviderKeeper(cfg, mockCore, nil)
	keeper.SetFailureThreshold(2)

	// 1st failure
	keeper.RecordProviderFailure(context.Background(), "p-1", "openai", "Primary Node")
	if count := keeper.GetFailureCount("p-1"); count != 1 {
		t.Fatalf("expected failure count 1, got %d", count)
	}
	mu.Lock()
	if toggledActive != -1 {
		t.Fatalf("expected not toggled yet")
	}
	mu.Unlock()

	// 2nd failure -> threshold reached
	keeper.RecordProviderFailure(context.Background(), "p-1", "openai", "Primary Node")
	if count := keeper.GetFailureCount("p-1"); count != 2 {
		t.Fatalf("expected failure count 2, got %d", count)
	}
	mu.Lock()
	if toggledActive != 0 || toggledID != "p-1" {
		t.Fatalf("expected auto-disabled, got active=%d id=%s", toggledActive, toggledID)
	}
	mu.Unlock()

	// Success resets counter
	keeper.RecordProviderSuccess("p-1")
	if count := keeper.GetFailureCount("p-1"); count != 0 {
		t.Fatalf("expected reset to 0, got %d", count)
	}
}

func TestProviderKeeper_Concurrency(t *testing.T) {
	mockCore := &upstream.MockCoreClient{
		ToggleProviderFunc: func(ctx context.Context, id string, isActive bool) error {
			return nil
		},
	}
	cfg := &config.Config{}
	keeper := NewProviderKeeper(cfg, mockCore, nil)
	keeper.SetFailureThreshold(100)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				keeper.RecordProviderFailure(context.Background(), "p-conc", "ag", "AG Conc")
				_ = keeper.GetFailureCount("p-conc")
				if j%10 == 0 {
					keeper.RecordProviderSuccess("p-conc")
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestProviderKeeper_SkipUnsupportedProviderTest(t *testing.T) {
	dbPath, db := setupTestDB(t)
	defer db.Close()

	// Insert active connection for provider like commandcode
	_, err := db.Exec(`INSERT INTO providerConnections (id, provider, name, email, isActive)
		VALUES ('conn-cmc', 'commandcode', 'Personal Key', '', 1)`)
	if err != nil {
		t.Fatalf("failed to insert conn: %v", err)
	}

	var mu sync.Mutex
	toggled := false

	mockCore := &upstream.MockCoreClient{
		TestProviderFunc: func(ctx context.Context, id string) (map[string]interface{}, error) {
			// Simulates 9router Core's response for un-testable providers
			return map[string]interface{}{
				"valid": false,
				"error": "Provider test not supported",
			}, nil
		},
		ToggleProviderFunc: func(ctx context.Context, id string, isActive bool) error {
			mu.Lock()
			defer mu.Unlock()
			toggled = true
			return nil
		},
	}

	cfg := &config.Config{NineRouterDBPath: dbPath}
	keeper := NewProviderKeeper(cfg, mockCore, nil)
	keeper.SetFailureThreshold(1) // would trip immediately if treated as failure

	// Run auto-disable check
	keeper.checkAndAutoDisable()

	mu.Lock()
	if toggled {
		t.Fatalf("expected unsupported provider to NOT be toggled/disabled")
	}
	mu.Unlock()

	if count := keeper.GetFailureCount("conn-cmc"); count != 0 {
		t.Fatalf("expected failure count to be 0 for unsupported test, got %d", count)
	}
}
