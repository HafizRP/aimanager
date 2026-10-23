package worker

import (
	"context"
	"testing"
	"time"

	"9router-gateway/internal/entity"
)

type fakeRetentionStore struct {
	retentionSetting string
	purgedRequest    int64
	purgedSecurity   int64
	purgedAudits     int64
	lastCutoff       time.Time
	events           []entity.SecurityEvent
	lastCount        int64
}

func newFakeRetentionStore() *fakeRetentionStore {
	return &fakeRetentionStore{}
}

func (f *fakeRetentionStore) GetSettingDefault(ctx context.Context, key, fallback string) string {
	if f.retentionSetting == "" {
		return fallback
	}
	return f.retentionSetting
}

func (f *fakeRetentionStore) CountRequestLogs(ctx context.Context) (int64, error) {
	return f.lastCount, nil
}

func (f *fakeRetentionStore) PurgeRequestLogsBefore(ctx context.Context, before time.Time) (int64, error) {
	f.lastCutoff = before
	return f.purgedRequest, nil
}

func (f *fakeRetentionStore) PurgeSecurityEventsBefore(ctx context.Context, before time.Time) (int64, error) {
	return f.purgedSecurity, nil
}

func (f *fakeRetentionStore) PurgeLoginAuditsBefore(ctx context.Context, before time.Time) (int64, error) {
	return f.purgedAudits, nil
}

func (f *fakeRetentionStore) CreateSecurityEvent(ctx context.Context, ev *entity.SecurityEvent) error {
	f.events = append(f.events, *ev)
	return nil
}

func TestLogRetention_DefaultDays(t *testing.T) {
	store := newFakeRetentionStore()
	lr := NewLogRetention(store)

	if got := lr.RetentionDays(context.Background()); got != 7 {
		t.Fatalf("expected default retention 7, got %d", got)
	}
}

func TestLogRetention_CustomDays(t *testing.T) {
	store := newFakeRetentionStore()
	store.retentionSetting = "30"
	lr := NewLogRetention(store)

	if got := lr.RetentionDays(context.Background()); got != 30 {
		t.Fatalf("expected retention 30, got %d", got)
	}
}

func TestLogRetention_InvalidSettingFallsBack(t *testing.T) {
	store := newFakeRetentionStore()
	store.retentionSetting = "not-a-number"
	lr := NewLogRetention(store)

	if got := lr.RetentionDays(context.Background()); got != 7 {
		t.Fatalf("expected fallback 7 for invalid setting, got %d", got)
	}
}

func TestLogRetention_RunOncePurgesAllTables(t *testing.T) {
	store := newFakeRetentionStore()
	store.retentionSetting = "14"
	store.purgedRequest = 100
	store.purgedSecurity = 5
	store.purgedAudits = 3
	lr := NewLogRetention(store)

	res, err := lr.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RequestLogs != 100 || res.SecurityEvents != 5 || res.LoginAudits != 3 {
		t.Fatalf("unexpected purge result: %+v", res)
	}
	// Cutoff must be ~14 days in the past.
	expected := time.Now().UTC().AddDate(0, 0, -14)
	if diff := store.lastCutoff.Sub(expected); diff > 2*time.Minute || diff < -2*time.Minute {
		t.Fatalf("cutoff drift too large: %v", diff)
	}
}

func TestLogRetention_ZeroDisables(t *testing.T) {
	store := newFakeRetentionStore()
	store.retentionSetting = "0"
	store.purgedRequest = 999
	lr := NewLogRetention(store)

	res, err := lr.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RequestLogs != 0 {
		t.Fatalf("expected no purge when retention is 0, got %+v", res)
	}
	if !store.lastCutoff.IsZero() {
		t.Fatal("purge should not have been called when retention is 0")
	}
}

func TestLogRetention_StartStop(t *testing.T) {
	store := newFakeRetentionStore()
	lr := NewLogRetention(store)
	lr.SetInterval(10 * time.Millisecond)
	lr.Start()
	time.Sleep(30 * time.Millisecond)
	lr.Stop()
}
