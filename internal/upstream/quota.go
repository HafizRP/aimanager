package upstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"9router-gateway/internal/config"

	_ "modernc.org/sqlite"
)

type UpstreamQuotaInfo struct {
	Used                float64   `json:"used"`
	Total               float64   `json:"total"`
	Remaining           float64   `json:"remaining"`
	RemainingPercentage float64   `json:"remaining_percentage"`
	ResetAt             time.Time `json:"reset_at"`
	ResetAtWIB          string    `json:"reset_at_wib"`
	ResetIn             string    `json:"reset_in"`
	DisplayName         string    `json:"display_name"`
	Unlimited           bool      `json:"unlimited"`
}

type ProviderAccount struct {
	ID        string                       `json:"id"`
	Provider  string                       `json:"provider"`
	Name      string                       `json:"name"`
	Email     string                       `json:"email"`
	Plan      string                       `json:"plan"`
	Status    string                       `json:"status"` // "ready", "exhausted", "error"
	Quotas    map[string]UpstreamQuotaInfo `json:"quotas"`
	ErrorMsg  string                       `json:"error_msg,omitempty"`
	FetchedAt time.Time                    `json:"fetched_at"`
}

func (p ProviderAccount) FlashQuota() *UpstreamQuotaInfo {
	for _, k := range []string{"gemini-3.8-flash-high", "gemini-3.8-flash-medium", "gemini-3.8-flash-low", "gemini-3.7-flash-high", "gemini-3.7-flash-medium"} {
		if q, ok := p.Quotas[k]; ok {
			return &q
		}
	}
	return nil
}

func (p ProviderAccount) ClaudeQuota() *UpstreamQuotaInfo {
	for _, k := range []string{"claude-sonnet-4-6", "claude-opus-4-6-thinking"} {
		if q, ok := p.Quotas[k]; ok {
			return &q
		}
	}
	return nil
}

func (p ProviderAccount) KiroCredit() *UpstreamQuotaInfo {
	if q, ok := p.Quotas["credit"]; ok {
		return &q
	}
	return nil
}

func (p ProviderAccount) DisplayProvider() string {
	if p.Provider == "antigravity" {
		return "Antigravity (Google)"
	}
	if p.Provider == "kiro" {
		return "Kiro AI"
	}
	if strings.HasPrefix(p.Provider, "openai-compatible") {
		return "OpenAI Compatible"
	}
	return p.Provider
}

type ModelAccountDetail struct {
	AccountName string  `json:"account_name"`
	Email       string  `json:"email"`
	Remaining   float64 `json:"remaining"`
	Total       float64 `json:"total"`
	Percentage  float64 `json:"percentage"`
	ResetAtWIB  string  `json:"reset_at_wib"`
	ResetIn     string  `json:"reset_in"`
	IsReady     bool    `json:"is_ready"`
}

type ModelQuotaSummary struct {
	ModelID          string               `json:"model_id"`
	Provider         string               `json:"provider"`
	HasQuota         bool                 `json:"has_quota"`
	BestPercentage   float64              `json:"best_percentage"`
	BestRemaining    float64              `json:"best_remaining"`
	BestTotal        float64              `json:"best_total"`
	TotalAccounts    int                  `json:"total_accounts"`
	ReadyAccounts    int                  `json:"ready_accounts"`
	Status           string               `json:"status"` // "ready", "partial", "exhausted", "unlimited"
	NearestResetWIB  string               `json:"nearest_reset_wib"`
	NearestResetIn   string               `json:"nearest_reset_in"`
	AccountDetails   []ModelAccountDetail `json:"account_details"`
	DescriptionLabel string               `json:"description_label"`
}

type UpstreamQuotaReport struct {
	Accounts       []ProviderAccount            `json:"accounts"`
	FetchedAt      time.Time                    `json:"fetched_at"`
	FetchedAtWIB   string                       `json:"fetched_at_wib"`
	ModelSummaries map[string]ModelQuotaSummary `json:"model_summaries"`
}

type QuotaManager struct {
	cfg        *config.Config
	httpClient *http.Client
	mu         sync.RWMutex
	cache      *UpstreamQuotaReport
	cacheTime  time.Time
	cacheTTL   time.Duration
}

func NewQuotaManager(cfg *config.Config) *QuotaManager {
	return &QuotaManager{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 6 * time.Second,
		},
		cacheTTL: 25 * time.Second,
	}
}

func (m *QuotaManager) deriveCLIToken() string {
	return DeriveCLIToken(m.cfg)
}

func (m *QuotaManager) formatWIB(t time.Time) (string, string) {
	if t.IsZero() {
		return "-", "-"
	}

	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.FixedZone("WIB", 7*3600)
	}

	now := time.Now().In(loc)
	tWIB := t.In(loc)

	diff := tWIB.Sub(now)
	var resetIn string
	if diff <= 0 {
		resetIn = "Sekarang"
	} else if diff < time.Minute {
		resetIn = "< 1m"
	} else if diff < time.Hour {
		resetIn = fmt.Sprintf("%dm", int(diff.Minutes()))
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		mins := int(diff.Minutes()) % 60
		if mins > 0 {
			resetIn = fmt.Sprintf("%dj %dm", hours, mins)
		} else {
			resetIn = fmt.Sprintf("%dj", hours)
		}
	} else {
		days := int(diff.Hours()) / 24
		hours := int(diff.Hours()) % 24
		resetIn = fmt.Sprintf("%dh %dj", days, hours)
	}

	// Format resetAt string
	var datePrefix string
	if tWIB.Year() == now.Year() && tWIB.YearDay() == now.YearDay() {
		datePrefix = "Hari ini "
	} else if tWIB.Year() == now.Year() && tWIB.YearDay() == now.YearDay()+1 {
		datePrefix = "Besok "
	} else {
		datePrefix = tWIB.Format("02 Jan ")
	}
	resetWIB := datePrefix + tWIB.Format("15:04 WIB")

	return resetWIB, resetIn
}

func (m *QuotaManager) FetchAllQuotas(ctx context.Context, force bool) (*UpstreamQuotaReport, error) {
	m.mu.RLock()
	if !force && m.cache != nil && time.Since(m.cacheTime) < m.cacheTTL {
		cached := m.cache
		m.mu.RUnlock()
		return cached, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double check cache
	if !force && m.cache != nil && time.Since(m.cacheTime) < m.cacheTTL {
		return m.cache, nil
	}

	// 1. Open 9router SQLite to query active provider connections
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", m.cfg.GetNineRouterDBPath())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open 9router db: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, provider, COALESCE(name,''), COALESCE(email,'') FROM providerConnections WHERE isActive = 1 ORDER BY priority ASC, createdAt ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to query provider connections: %w", err)
	}
	defer rows.Close()

	type connRow struct {
		id       string
		provider string
		name     string
		email    string
	}

	var conns []connRow
	for rows.Next() {
		var c connRow
		if err := rows.Scan(&c.id, &c.provider, &c.name, &c.email); err == nil {
			conns = append(conns, c)
		}
	}

	cliToken := m.deriveCLIToken()

	// 2. Fetch usage in parallel
	var wg sync.WaitGroup
	var accMu sync.Mutex
	accounts := make([]ProviderAccount, 0, len(conns))

	for _, c := range conns {
		wg.Add(1)
		go func(conn connRow) {
			defer wg.Done()

			acc := ProviderAccount{
				ID:        conn.id,
				Provider:  conn.provider,
				Name:      conn.name,
				Email:     conn.email,
				Status:    "ready",
				Quotas:    make(map[string]UpstreamQuotaInfo),
				FetchedAt: time.Now(),
			}

			reqURL := fmt.Sprintf("%s/api/usage/%s", m.cfg.GetUpstreamURL(), conn.id)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
			if err != nil {
				acc.Status = "error"
				acc.ErrorMsg = err.Error()
				accMu.Lock()
				accounts = append(accounts, acc)
				accMu.Unlock()
				return
			}

			if cliToken != "" {
				req.Header.Set("x-9r-cli-token", cliToken)
			}
			if apiKey := m.cfg.GetUpstreamAPIKey(); apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}

			resp, err := m.httpClient.Do(req)
			if err != nil {
				acc.Status = "error"
				acc.ErrorMsg = "Connection timed out"
				accMu.Lock()
				accounts = append(accounts, acc)
				accMu.Unlock()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				acc.Status = "error"
				acc.ErrorMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
				accMu.Lock()
				accounts = append(accounts, acc)
				accMu.Unlock()
				return
			}

			var rawResp struct {
				Plan   string `json:"plan"`
				Quotas map[string]struct {
					Used                float64     `json:"used"`
					Total               float64     `json:"total"`
					Remaining           interface{} `json:"remaining"`
					RemainingPercentage interface{} `json:"remainingPercentage"`
					ResetAt             string      `json:"resetAt"`
					DisplayName         string      `json:"displayName"`
					Unlimited           bool        `json:"unlimited"`
				} `json:"quotas"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&rawResp); err != nil {
				acc.Status = "error"
				acc.ErrorMsg = "Failed to decode response"
				accMu.Lock()
				accounts = append(accounts, acc)
				accMu.Unlock()
				return
			}

			acc.Plan = rawResp.Plan

			for k, q := range rawResp.Quotas {
				info := UpstreamQuotaInfo{
					Used:        q.Used,
					Total:       q.Total,
					DisplayName: q.DisplayName,
					Unlimited:   q.Unlimited,
				}

				if q.Remaining != nil {
					switch v := q.Remaining.(type) {
					case float64:
						info.Remaining = v
					case int:
						info.Remaining = float64(v)
					}
				} else {
					info.Remaining = math.Max(0, info.Total-info.Used)
				}

				if q.RemainingPercentage != nil {
					switch v := q.RemainingPercentage.(type) {
					case float64:
						info.RemainingPercentage = v
					case int:
						info.RemainingPercentage = float64(v)
					}
				} else if info.Total > 0 {
					info.RemainingPercentage = (info.Remaining / info.Total) * 100.0
				}

				if q.ResetAt != "" {
					if t, err := time.Parse(time.RFC3339, q.ResetAt); err == nil {
						info.ResetAt = t
						info.ResetAtWIB, info.ResetIn = m.formatWIB(t)
					}
				}

				acc.Quotas[k] = info
			}

			// Determine overall account status
			if len(acc.Quotas) > 0 {
				hasReady := false
				for _, q := range acc.Quotas {
					if q.RemainingPercentage > 0.5 || q.Unlimited {
						hasReady = true
						break
					}
				}
				if !hasReady {
					acc.Status = "exhausted"
				}
			}

			accMu.Lock()
			accounts = append(accounts, acc)
			accMu.Unlock()
		}(c)
	}

	wg.Wait()

	// Sort accounts: Antigravity first, then Kiro, then others; Ready before Exhausted
	sort.Slice(accounts, func(i, j int) bool {
		rank := func(p ProviderAccount) int {
			if p.Provider == "antigravity" {
				if p.Status == "ready" {
					return 1
				}
				return 2
			}
			if p.Provider == "kiro" {
				if p.Status == "ready" {
					return 3
				}
				return 4
			}
			return 5
		}
		rI, rJ := rank(accounts[i]), rank(accounts[j])
		if rI != rJ {
			return rI < rJ
		}
		if accounts[i].Email != "" && accounts[j].Email != "" {
			return accounts[i].Email < accounts[j].Email
		}
		return accounts[i].Name < accounts[j].Name
	})

	loc, _ := time.LoadLocation("Asia/Jakarta")
	if loc == nil {
		loc = time.FixedZone("WIB", 7*3600)
	}
	now := time.Now().In(loc)

	report := &UpstreamQuotaReport{
		Accounts:       accounts,
		FetchedAt:      now,
		FetchedAtWIB:   now.Format("15:04:05 WIB"),
		ModelSummaries: make(map[string]ModelQuotaSummary),
	}

	m.cache = report
	m.cacheTime = time.Now()

	log.Info().Int("accounts_count", len(accounts)).Msg("Fetched upstream model quotas successfully")
	return report, nil
}

func (m *QuotaManager) GetModelSummary(modelID string, report *UpstreamQuotaReport) ModelQuotaSummary {
	summary := ModelQuotaSummary{
		ModelID:        modelID,
		HasQuota:       false,
		Status:         "unlimited",
		AccountDetails: []ModelAccountDetail{},
	}

	if report == nil || len(report.Accounts) == 0 {
		return summary
	}

	// 1. Google Antigravity Models (ag/...)
	if strings.HasPrefix(modelID, "ag/") {
		summary.Provider = "antigravity"
		rawKey := strings.TrimPrefix(modelID, "ag/")

		var matchingAccounts []ModelAccountDetail
		bestPct := 0.0
		bestRemaining := 0.0
		bestTotal := 0.0
		readyCount := 0
		totalAntigravity := 0
		var nearestResetTime time.Time
		var nearestResetWIB, nearestResetIn string

		for _, acc := range report.Accounts {
			if acc.Provider != "antigravity" {
				continue
			}
			totalAntigravity++

			var qInfo *UpstreamQuotaInfo
			// Direct key match
			if q, ok := acc.Quotas[rawKey]; ok {
				qInfo = &q
			} else {
				// Fuzzy key match (e.g. gemini-3.8-flash -> gemini-3.8-flash-high/low)
				for k, q := range acc.Quotas {
					if strings.HasPrefix(k, rawKey) || strings.Contains(k, rawKey) {
						qCopy := q
						qInfo = &qCopy
						break
					}
				}
			}

			if qInfo != nil {
				summary.HasQuota = true
				isReady := qInfo.RemainingPercentage > 0.5

				name := acc.Email
				if name == "" {
					name = acc.Name
				}
				if name == "" {
					name = acc.ID[:8]
				}

				detail := ModelAccountDetail{
					AccountName: name,
					Email:       acc.Email,
					Remaining:   qInfo.Remaining,
					Total:       qInfo.Total,
					Percentage:  qInfo.RemainingPercentage,
					ResetAtWIB:  qInfo.ResetAtWIB,
					ResetIn:     qInfo.ResetIn,
					IsReady:     isReady,
				}
				matchingAccounts = append(matchingAccounts, detail)

				if isReady {
					readyCount++
				}
				if qInfo.RemainingPercentage > bestPct {
					bestPct = qInfo.RemainingPercentage
					bestRemaining = qInfo.Remaining
					bestTotal = qInfo.Total
				}

				if !qInfo.ResetAt.IsZero() {
					if nearestResetTime.IsZero() || qInfo.ResetAt.Before(nearestResetTime) {
						nearestResetTime = qInfo.ResetAt
						nearestResetWIB = qInfo.ResetAtWIB
						nearestResetIn = qInfo.ResetIn
					}
				}
			}
		}

		if summary.HasQuota {
			summary.BestPercentage = bestPct
			summary.BestRemaining = bestRemaining
			summary.BestTotal = bestTotal
			summary.TotalAccounts = totalAntigravity
			summary.ReadyAccounts = readyCount
			summary.AccountDetails = matchingAccounts
			summary.NearestResetWIB = nearestResetWIB
			summary.NearestResetIn = nearestResetIn

			if readyCount == totalAntigravity && totalAntigravity > 0 {
				summary.Status = "ready"
				summary.DescriptionLabel = fmt.Sprintf("%.1f%% (Semua %d Akun Aktif)", bestPct, totalAntigravity)
			} else if readyCount > 0 {
				summary.Status = "partial"
				summary.DescriptionLabel = fmt.Sprintf("%.1f%% (%d/%d Akun Aktif)", bestPct, readyCount, totalAntigravity)
			} else {
				summary.Status = "exhausted"
				summary.DescriptionLabel = fmt.Sprintf("0%% (Limit tercapai, reset %s)", nearestResetWIB)
			}
		}

		return summary
	}

	// 2. Kiro AI Models (kr/...)
	if strings.HasPrefix(modelID, "kr/") {
		summary.Provider = "kiro"
		var matchingAccounts []ModelAccountDetail
		bestPct := 0.0
		bestRemaining := 0.0
		bestTotal := 0.0
		readyCount := 0
		totalKiro := 0
		var nearestResetTime time.Time
		var nearestResetWIB, nearestResetIn string

		for _, acc := range report.Accounts {
			if acc.Provider != "kiro" {
				continue
			}
			totalKiro++

			if q, ok := acc.Quotas["credit"]; ok {
				summary.HasQuota = true
				isReady := q.Remaining > 0.01

				name := acc.Name
				if name == "" {
					name = acc.Email
				}
				if name == "" {
					name = acc.ID[:8]
				}

				detail := ModelAccountDetail{
					AccountName: name,
					Email:       acc.Email,
					Remaining:   q.Remaining,
					Total:       q.Total,
					Percentage:  q.RemainingPercentage,
					ResetAtWIB:  q.ResetAtWIB,
					ResetIn:     q.ResetIn,
					IsReady:     isReady,
				}
				matchingAccounts = append(matchingAccounts, detail)

				if isReady {
					readyCount++
				}
				if q.RemainingPercentage > bestPct {
					bestPct = q.RemainingPercentage
					bestRemaining = q.Remaining
					bestTotal = q.Total
				}

				if !q.ResetAt.IsZero() {
					if nearestResetTime.IsZero() || q.ResetAt.Before(nearestResetTime) {
						nearestResetTime = q.ResetAt
						nearestResetWIB = q.ResetAtWIB
						nearestResetIn = q.ResetIn
					}
				}
			}
		}

		if summary.HasQuota {
			summary.BestPercentage = bestPct
			summary.BestRemaining = bestRemaining
			summary.BestTotal = bestTotal
			summary.TotalAccounts = totalKiro
			summary.ReadyAccounts = readyCount
			summary.AccountDetails = matchingAccounts
			summary.NearestResetWIB = nearestResetWIB
			summary.NearestResetIn = nearestResetIn

			if readyCount == totalKiro && totalKiro > 0 {
				summary.Status = "ready"
				summary.DescriptionLabel = fmt.Sprintf("%.1f/%.0f Credits (Semua Akun)", bestRemaining, bestTotal)
			} else if readyCount > 0 {
				summary.Status = "partial"
				summary.DescriptionLabel = fmt.Sprintf("%.1f/%.0f Credits (%d/%d Akun Aktif)", bestRemaining, bestTotal, readyCount, totalKiro)
			} else {
				summary.Status = "exhausted"
				summary.DescriptionLabel = fmt.Sprintf("0 Credits (Habis, reset %s)", nearestResetWIB)
			}
		}

		return summary
	}

	// Default / Passthrough provider models
	summary.HasQuota = false
	summary.Status = "unlimited"
	summary.DescriptionLabel = "Uncapped / Standard"
	return summary
}
