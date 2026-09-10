# ⚡ AI Manager (`9router-gateway`)

> **Unified Enterprise LLM Gateway, FinOps Analytics, Multi-Tenant Management & Reverse Proxy Stack**

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![SQLite](https://img.shields.io/badge/SQLite-WAL_Mode-003B57?style=flat&logo=sqlite)](https://www.sqlite.org/)
[![Docker](https://img.shields.io/badge/Docker-Compose_Ready-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**AI Manager** is a high-performance reverse proxy, unified administration platform, and multi-tenant management gateway deployed in front of **9router Core** (`127.0.0.1:20128`). It wraps complex multi-provider LLM routing into a single, cohesive pane of glass—adding enterprise authentication, granular RBAC, FinOps cost-savings tracking, sub-10ms response caching, automated token billing, and developer tooling.

---

## 🏛️ System Architecture

```
                                [ Client Requests ]
       (Cursor IDE, Claude Code, Cline, Hermes Agent, Python, Custom Apps)
                                       │
                                       ▼
                       ┌───────────────────────────────┐
                       │   Cloudflare Tunnel / Ingress │
                       │    (aimanager.b14.my.id)      │
                       └───────────────┬───────────────┘
                                       │
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          AI MANAGER GATEWAY (Port 20129)                     │
│                                                                             │
│  ┌───────────────────────┐ ┌──────────────────────┐ ┌────────────────────┐  │
│  │   Auth & RBAC Filter  │ │  Rate Limiter / RPM  │ │ Response Cache     │  │
│  │   (Session / Bearer)  │ │  (Per-key & Per-user)│ │ (SHA-256, 15m TTL) │  │
│  └───────────┬───────────┘ └──────────┬───────────┘ └─────────┬──────────┘  │
│              │                        │                       │             │
│  ┌───────────▼────────────────────────▼───────────────────────▼──────────┐  │
│  │               Reverse Proxy & SSE Streaming Engine                    │  │
│  │      - Scoped Models Whitelisting                                     │  │
│  │      - Token Usage Interceptor (Prompt + Completion + Reasoning)       │  │
│  │      - Async Audit Logger (WAL SQLite)                                │  │
│  └────────────────────────────────────┬──────────────────────────────────┘  │
│                                       │                                     │
│  ┌────────────────────────────────────▼──────────────────────────────────┐  │
│  │                       Internal Background Daemons                     │  │
│  │   • Key Syncer (SQLite to Core)   • Quota Auto-Reactivator Worker     │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────┬─────────────────────────────────────┘
                                        │ (Loopback 127.0.0.1 only)
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           9ROUTER CORE (Port 20128)                         │
│                                                                             │
│   • Multi-Provider Pooling (Antigravity, Kiro AI, Gemini, Anthropic, etc.)  │
│   • Model Combos & Failover Fallback Chains (`main`, `free-only`)           │
│   • RTK Prompt Compression (-40% token usage) & Caveman Mode                │
│   • Model Route Rewriting & Aliases                                         │
└───────────────────────────────────────┬─────────────────────────────────────┘
                                        │
                                        ▼
                         [ Upstream Cloud Providers ]
          (Google Gemini, Anthropic Claude, OpenAI, OpenRouter, etc.)
```

---

## ✨ Features & Capabilities

### 1. 100% Parity with 9router Core
- **Upstream Provider Management**: Connect, test ping latency, toggle, and prioritize upstream LLM accounts (Google Gemini, Anthropic, OpenAI, OpenRouter, Kiro AI, Antigravity) directly from the Web UI.
- **Model Combos & Auto-Failover Chains**: Define resilient combo chains (e.g. `main`, `free-only`) that automatically fall back to secondary models if an upstream provider experiences rate limits or downtime.
- **Token Saver & RTK Prompt Compression**: Real-time switchable prompt compression (-40% tokens), Caveman mode, Ponytail chunk streaming, and Provider Thinking intensity controls.
- **Model Aliases**: Dynamically rewrite requested model names to internal targets without changing client configuration.
- **Outbound Proxy Pools**: Support for egress HTTP and SOCKS5 proxy routing.

### 2. Multi-Tenant RBAC & Security
- **Role-Based Access Control**: Separate **Admin** (system configuration, users, billing, provider nodes) and **Standard User** (keys, personal logs, playground, token top-up) views.
- **Server-Side Crypto Sessions**: 64-character high-entropy hexadecimal tokens stored in SQLite with TTL expiry.
- **Brute-Force Protection**: IP-based sliding window rate limiter on authentication endpoints.
- **Enterprise Security Headers**: Strict Content Security Policy (CSP), HTTP Strict Transport Security (HSTS), `X-Frame-Options: DENY`, and `X-Content-Type-Options: nosniff`.
- **Localhost Lockdown**: Port `20128` (9router Core) is hardened to `127.0.0.1`—all incoming traffic must pass through the AI Manager gateway (`20129`).

### 3. FinOps & Cost Analytics
- **Live Cost Savings Tracker**: Automatically computes total money saved (in **USD** and **IDR**) compared against commercial frontier model rates ($5/1M tokens) plus savings realized from RTK token compression.
- **TradingView-Style Candlestick Charts**: Adaptive timeframe grouping (**1H, 4H, 1D, 1W, 1M, ALL**) for token usage trends with volume metrics.
- **Top Users & Models Leaderboard**: Ranks consumers by total token volume with rank badges.

### 4. Advanced Traffic & Cache Controls
- **Exact Response Caching**: In-memory cache for identical non-streaming prompts with a 15-minute TTL. Returns cached completions in **<10ms** with `X-Cache: HIT`, saving 100% of upstream tokens.
- **Scoped API Keys**: Restrict specific keys to designated models only (e.g. `["main"]` or `["ag/gemini-3.8-flash-high"]`). Unauthorized model requests receive an immediate `403 Forbidden`.
- **Per-Key & Per-User Rate Limiting**: Enforce Request Per Minute (RPM) and Token Per Minute (TPM) caps with RFC-compliant `429 Too Many Requests` and `Retry-After` headers.
- **Background Quota Auto-Reactivator**: Periodically checks accounts that encountered daily quota exhaustion. As soon as the reset window passes, the daemon tests connectivity and reactivates the connection automatically.

### 5. Developer Experience (DX)
- **Interactive AI Playground (`/chat`)**: Multi-model chat console supporting SSE streaming, Markdown formatting, code block copy, multi-session history saved in browser `localStorage`, and 1-click **Export to Markdown**.
- **Model Speed Benchmark (`/benchmark`)**: Concurrently fires test payloads to multiple upstream models and combos to measure round-trip latency, TTFT, and throughput with a real-time leaderboard.
- **CLI Tools Setup Hub (`/cli-tools`)**: Pre-filled, copy-ready configuration snippets for:
  - Claude Code CLI
  - Cursor IDE
  - Cline / Roo-Code
  - OpenAI Codex CLI
  - GitHub Copilot
  - Hermes Agent
- **Request Audit Logs (`/logs`)**: Real-time audit logs with **Go Live** auto-polling (every 3 seconds), in-place AJAX refresh, and 1-click **CSV / JSON Export**.

### 6. Billing & Monetization
- **Midtrans Payment Gateway**: Integrated Snap checkout supporting QRIS, GoPay, and Virtual Accounts.
- **Automated Webhook Fulfillment**: Verifies signature hashes and automatically credits purchased token allowances to user accounts.

---

## 🚀 Quick Start (Docker Compose)

The easiest way to run the complete AI Manager stack (both 9router Core and Gateway) is via Docker Compose.

### 1. Clone & Configure
```bash
git clone https://github.com/HafizRP/aimanager.git
cd aimanager

cp .env.example .env
# Edit .env to set your ADMIN_PASSWORD and SESSION_SECRET
nano .env
```

### 2. Launch Unified Stack
```bash
docker compose up -d --build
```

### 3. Verify Containers
```bash
docker compose ps
```
- **9router Core**: Listening internally on `127.0.0.1:20128`
- **AI Manager Gateway**: Listening on `http://0.0.0.0:20129`

---

## 🖥️ Systemd Deployment (Bare-Metal / Linux Server)

For production Linux servers running without Docker overhead:

### 1. Build Gateway Binary
```bash
go build -ldflags="-w -s" -o bin/9router-gateway ./cmd/gateway
# Or using Makefile
make build
```

### 2. Configure Service Unit
Use the provided service unit template in `init/systemd/9router-gateway.service`:
```bash
sudo cp init/systemd/9router-gateway.service /etc/systemd/system/
```
Or create `/etc/systemd/system/9router-gateway.service`:
```ini
[Unit]
Description=AI Manager Gateway & Reverse Proxy
After=network.target docker.service
Wants=docker.service

[Service]
Type=simple
User=b14
Group=b14
WorkingDirectory=/home/b14/9router-gateway
ExecStart=/home/b14/9router-gateway/bin/9router-gateway
Restart=always
RestartSec=3
EnvironmentFile=/home/b14/9router-gateway/.env
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

### 3. Start & Enable Service
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now 9router-gateway
sudo systemctl status 9router-gateway
```

---

## ⚙️ Configuration Reference (`.env`)

| Variable | Default | Description |
|---|---|---|
| `PORT` | `20129` | HTTP port for AI Manager Gateway |
| `HOST` | `0.0.0.0` | Bind address |
| `DB_PATH` | `./data/gateway.db` | Path to gateway SQLite database |
| `NINEROUTER_DATA_DIR` | (auto-derived) | Optional path to 9router Core data directory |
| `ADMIN_USERNAME` | `admin` | Default admin username |
| `ADMIN_PASSWORD` | `admin123` | Default admin password |
| `SESSION_SECRET` | `change_me` | Secret key used for signing session cookies |
| `MIDTRANS_SERVER_KEY` | - | Midtrans Server Key for payment handling |
| `MIDTRANS_CLIENT_KEY` | - | Midtrans Client Key for Snap UI popup |
| `MIDTRANS_IS_PRODUCTION`| `false` | Set to `true` for live payments |

> **Note**: Upstream 9router Core settings (Upstream URL, API Key, and Core DB Path) are configured and stored directly in the SQLite database via the Admin Settings UI.

---

## 📡 API Reference

AI Manager exposes an OpenAI-compatible API interface:

### 1. Chat Completions
```http
POST /v1/chat/completions
Authorization: Bearer <YOUR_API_KEY>
Content-Type: application/json
```
```json
{
  "model": "main",
  "messages": [
    {"role": "user", "content": "Explain quantum computing in one sentence."}
  ],
  "temperature": 0.7,
  "stream": false
}
```

### 2. Models Catalog
```http
GET /v1/models
Authorization: Bearer <YOUR_API_KEY>
```
*Note: Returns only models explicitly allowed by the user's whitelist and key scope.*

### 3. Export Request Logs
```http
GET /api/logs/export?format=csv
Cookie: gw_session=<ADMIN_SESSION_TOKEN>
```
*(Supports `format=csv` and `format=json`)*

### 4. Speed Benchmark Run
```http
POST /api/benchmark/run
Cookie: gw_session=<ADMIN_SESSION_TOKEN>
Content-Type: application/json
```
```json
{
  "models": ["main", "free-only", "ag/gemini-3.8-flash-high"]
}
```

---

## 🔌 Client Setup Guides

### Cursor IDE
1. Open **Cursor Settings > Models > OpenAI API Key**.
2. Set **Override OpenAI Base URL**: `https://aimanager.b14.my.id/v1` (or `http://127.0.0.1:20129/v1`).
3. Set **API Key**: `aim_...` (generated from `/keys`).
4. Configure model: `main` or your allowed combo.

### Claude Code CLI
Run in terminal:
```bash
export ANTHROPIC_BASE_URL="https://aimanager.b14.my.id/v1"
export ANTHROPIC_API_KEY="aim_your_key_here"
claude
```

### Hermes Agent
Add custom provider to `~/.hermes/config.yaml`:
```yaml
custom_providers:
  - name: AI Manager
    base_url: https://aimanager.b14.my.id/v1
    key_env: AIMANAGER_API_KEY
    model: main
    api_mode: chat_completions
```

---

## � Changelog

See [CHANGELOG.md](CHANGELOG.md) for detailed release notes, version history, and architectural milestones.

---

## �📄 License

This project is licensed under the **MIT License**.
