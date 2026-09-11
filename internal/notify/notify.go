// Package notify sends Telegram alerts for budget cutoffs, anomalies and
// self-healing actions. Disabled when TG_BOT_TOKEN/TG_CHAT_ID are unset.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"9router-gateway/internal/config"
)

// Sender delivers Telegram messages with per-key cooldown to avoid spam.
type Sender struct {
	cfg      *config.Config
	client   *http.Client
	mu       sync.Mutex
	lastSent map[string]time.Time
}

// NewSender builds a Sender bound to the gateway config.
func NewSender(cfg *config.Config) *Sender {
	return &Sender{
		cfg:      cfg,
		client:   &http.Client{Timeout: 10 * time.Second},
		lastSent: make(map[string]time.Time),
	}
}

// Enabled reports whether Telegram delivery is configured.
func (s *Sender) Enabled() bool {
	return s.cfg.TelegramBotToken != "" && s.cfg.TelegramChatID != ""
}

// Send delivers msg, throttled by cooldown per dedup key (0 = no throttle).
func (s *Sender) Send(ctx context.Context, dedup string, cooldown time.Duration, msg string) {
	if !s.Enabled() {
		return
	}
	if dedup != "" && cooldown > 0 {
		s.mu.Lock()
		if last, ok := s.lastSent[dedup]; ok && time.Since(last) < cooldown {
			s.mu.Unlock()
			return
		}
		s.lastSent[dedup] = time.Now()
		s.mu.Unlock()
	}
	payload := map[string]interface{}{
		"chat_id": s.cfg.TelegramChatID,
		"text":    msg,
	}
	if s.cfg.TelegramThreadID != "" {
		var tid int
		if _, err := fmt.Sscanf(s.cfg.TelegramThreadID, "%d", &tid); err == nil {
			payload["message_thread_id"] = tid
		}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+s.cfg.TelegramBotToken+"/sendMessage",
		bytes.NewBuffer(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
