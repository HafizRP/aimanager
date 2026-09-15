// Package eventbus provides an in-memory publish-subscribe event system for domain events.
package eventbus

import (
	"context"

	"github.com/rs/zerolog/log"

	"9router-gateway/internal/entity"
)

// KeySyncer is the decoupled interface for synchronizing keys with 9router Core.
type KeySyncer interface {
	SyncKey(key *entity.APIKey, userName string) error
	ToggleKey(keyID string, isActive bool) error
	DeleteKey(keyID string) error
}

// LogRepository is the decoupled interface for persisting request logs and security alerts.
type LogRepository interface {
	CreateRequestLog(ctx context.Context, log *entity.RequestLog) error
	CreateSecurityEvent(ctx context.Context, ev *entity.SecurityEvent) error
}

// Reactivator is the decoupled interface for immediate quota reactivation.
type Reactivator interface {
	CheckAndReactivateNow()
}

// KeyPayload carries key event details.
type KeyPayload struct {
	Key      *entity.APIKey
	UserName string
	KeyID    string
	IsActive bool
}

// RegisterKeySyncerSubscriber attaches key syncer observers to key events.
func RegisterKeySyncerSubscriber(bus EventBus, syncer KeySyncer) {
	if syncer == nil || bus == nil {
		return
	}

	bus.Subscribe(EventKeyCreated, func(ctx context.Context, e Event) {
		if p, ok := e.Payload.(KeyPayload); ok && p.Key != nil {
			if err := syncer.SyncKey(p.Key, p.UserName); err != nil {
				log.Warn().Err(err).Str("key_id", p.Key.ID).Msg("Observer failed to sync key on create")
			}
		}
	})

	bus.Subscribe(EventKeyToggled, func(ctx context.Context, e Event) {
		if p, ok := e.Payload.(KeyPayload); ok && p.KeyID != "" {
			if err := syncer.ToggleKey(p.KeyID, p.IsActive); err != nil {
				log.Warn().Err(err).Str("key_id", p.KeyID).Msg("Observer failed to toggle key")
			}
		}
	})

	bus.Subscribe(EventKeyDeleted, func(ctx context.Context, e Event) {
		if p, ok := e.Payload.(KeyPayload); ok && p.KeyID != "" {
			if err := syncer.DeleteKey(p.KeyID); err != nil {
				log.Warn().Err(err).Str("key_id", p.KeyID).Msg("Observer failed to delete key")
			}
		}
	})
}

// RegisterAuditLogSubscriber attaches async persistence observers for request logs and alerts.
func RegisterAuditLogSubscriber(bus EventBus, repo LogRepository) {
	if repo == nil || bus == nil {
		return
	}

	bus.Subscribe(EventRequestLogged, func(ctx context.Context, e Event) {
		if l, ok := e.Payload.(*entity.RequestLog); ok && l != nil {
			_ = repo.CreateRequestLog(ctx, l)
		}
	})

	bus.Subscribe(EventSecurityAlert, func(ctx context.Context, e Event) {
		if ev, ok := e.Payload.(*entity.SecurityEvent); ok && ev != nil {
			_ = repo.CreateSecurityEvent(ctx, ev)
		}
	})
}

// RegisterQuotaReactivatorSubscriber triggers immediate provider reactivation upon quota reset events.
func RegisterQuotaReactivatorSubscriber(bus EventBus, keeper Reactivator) {
	if keeper == nil || bus == nil {
		return
	}

	bus.Subscribe(EventQuotaResetDetected, func(ctx context.Context, e Event) {
		log.Info().Msg("Quota reset detected event received — triggering immediate provider check")
		keeper.CheckAndReactivateNow()
	})
}
