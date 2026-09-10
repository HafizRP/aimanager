package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

type TelegramNotifier struct {
	botToken   string
	chatID     string
	httpClient *http.Client
	mu         sync.Mutex
	lastAlerts map[string]time.Time
}

func NewTelegramNotifier() *TelegramNotifier {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if chatID == "" {
		chatID = "5145884481" // Configured user ID
	}

	return &TelegramNotifier{
		botToken: botToken,
		chatID:   chatID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		lastAlerts: make(map[string]time.Time),
	}
}

func (tn *TelegramNotifier) SendAlert(alertKey, message string) error {
	tn.mu.Lock()
	last, exists := tn.lastAlerts[alertKey]
	// Throttle identical alerts to at most once per 15 minutes
	if exists && time.Since(last) < 15*time.Minute {
		tn.mu.Unlock()
		return nil
	}
	tn.lastAlerts[alertKey] = time.Now()
	tn.mu.Unlock()

	if tn.botToken == "" {
		// Log locally if bot token not configured
		return nil
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tn.botToken)
	payload := map[string]interface{}{
		"chat_id":    tn.chatID,
		"text":       message,
		"parse_mode": "HTML",
	}

	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := tn.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
