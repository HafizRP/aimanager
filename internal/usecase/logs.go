package usecase

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"9router-gateway/internal/entity"
)

// LogService provides scoped request log queries and pagination.
type LogService struct {
	store Store
}

// NewLogService builds a LogService.
func NewLogService(store Store) *LogService {
	return &LogService{store: store}
}

// LogQuery carries pagination and filter parameters.
type LogQuery struct {
	Limit        int
	Cursor       string
	Direction    string
	UserID       string
	ModelFilter  string
	StatusFilter int
	StartDate    *time.Time // Inclusive lower bound (UTC)
	EndDate      *time.Time // Inclusive upper bound (UTC)
}

// List returns logs and paging info for the query.
func (s *LogService) List(ctx context.Context, q LogQuery) ([]entity.RequestLog, *entity.CursorPageInfo, error) {
	if q.Limit <= 0 {
		q.Limit = 25
	}
	if q.Direction != "prev" && q.Direction != "next" {
		q.Direction = "next"
	}
	return s.store.GetRequestLogsCursor(ctx, q.Limit, q.Cursor, q.Direction, q.UserID, q.ModelFilter, q.StatusFilter, q.StartDate, q.EndDate)
}

// SettingsService handles runtime config persistence.
type SettingsService struct {
	store     Store
	config    SettingsConfig
	keySyncer KeySyncer
}

// SettingsConfig exposes mutable runtime settings (implemented by *config.Config).
type SettingsConfig interface {
	SetUpstreamURL(url string)
	GetUpstreamURL() string
	SetUpstreamAPIKey(key string)
	SetNineRouterDBPath(path string)
	GetNineRouterDBPath() string
	GetUpstreamAPIKey() string
	MidtransServerKey() string
	SetMidtransServerKey(key string)
	MidtransClientKey() string
	SetMidtransClientKey(key string)
	MidtransMerchantID() string
	SetMidtransMerchantID(id string)
	MidtransIsProduction() bool
	SetMidtransIsProduction(v bool)
}

// NewSettingsService builds a SettingsService.
func NewSettingsService(store Store, config SettingsConfig, keySyncer KeySyncer) *SettingsService {
	// Guard against typed-nil (a nil *syncer.Syncer passed as KeySyncer interface).
	if isNilInterface(keySyncer) {
		keySyncer = nil
	}
	return &SettingsService{store: store, config: config, keySyncer: keySyncer}
}

// isNilInterface reports whether v is nil or holds a nil pointer/interface value.
func isNilInterface(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}

// UpdateUpstream persists and applies the 9router Core connection settings.
func (s *SettingsService) UpdateUpstream(ctx context.Context, upstreamURL, dbPath, apiKey string) error {
	upstreamURL = strings.TrimSpace(upstreamURL)
	if upstreamURL == "" {
		return fmt.Errorf("Upstream URL cannot be empty")
	}
	if !strings.HasPrefix(upstreamURL, "http://") && !strings.HasPrefix(upstreamURL, "https://") {
		return fmt.Errorf("Invalid upstream URL (must start with http:// or https://)")
	}
	upstreamURL = strings.TrimRight(upstreamURL, "/")

	s.config.SetUpstreamURL(upstreamURL)
	if err := s.store.SaveSetting(ctx, "upstream_url", upstreamURL); err != nil {
		return err
	}

	if dbPath != "" {
		s.config.SetNineRouterDBPath(dbPath)
		if err := s.store.SaveSetting(ctx, "ninerouter_db_path", dbPath); err != nil {
			return err
		}
		if s.keySyncer != nil {
			s.keySyncer.UpdateDBPath(dbPath)
			if allKeys, err := s.store.GetAllAPIKeys(ctx); err == nil {
				_ = s.keySyncer.BackfillAll(allKeys)
			}
		}
	}

	s.config.SetUpstreamAPIKey(apiKey)
	return s.store.SaveSetting(ctx, "upstream_api_key", apiKey)
}

// UpdateMidtrans persists Midtrans gateway settings.
func (s *SettingsService) UpdateMidtrans(ctx context.Context, serverKey, clientKey, merchantID string, isProduction bool) error {
	if serverKey != "" {
		s.config.SetMidtransServerKey(serverKey)
	}
	if clientKey != "" {
		s.config.SetMidtransClientKey(clientKey)
	}
	s.config.SetMidtransMerchantID(merchantID)
	s.config.SetMidtransIsProduction(isProduction)

	_ = s.store.SaveSetting(ctx, "midtrans_server_key", s.config.MidtransServerKey())
	_ = s.store.SaveSetting(ctx, "midtrans_client_key", s.config.MidtransClientKey())
	_ = s.store.SaveSetting(ctx, "midtrans_merchant_id", s.config.MidtransMerchantID())
	if isProduction {
		_ = s.store.SaveSetting(ctx, "midtrans_is_production", "true")
	} else {
		_ = s.store.SaveSetting(ctx, "midtrans_is_production", "false")
	}
	return nil
}