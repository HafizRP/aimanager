# 9router Gateway Middleware & Reverse Proxy

High-performance API Gateway and Reverse Proxy Middleware deployed in front of **9router Core** (`127.0.0.1:20128`), providing User & API Key Management, Model Whitelist / RBAC, Token Quota Limiting (Streaming SSE & Non-Streaming), and a Modern Web Admin Dashboard.

---

## 🚀 Key Features

1. **User & API Key Management**:
   - Create unlimited users with custom token allowances.
   - Issue multiple API keys per user (`sk-gw-...`).
   - Instant 1-click Revoke / Suspend / Reactivate buttons.

2. **Model Whitelist (Role & Permission Matrix)**:
   - Define exact model access per user (e.g. User A only allowed `ag/gemini-3.8-flash-low`, VIP allowed `ag/gemini-3.8-flash-high`).
   - Requests outside whitelist are rejected immediately with `403 Forbidden: Model not allowed`.
   - Dynamic `/v1/models` filtering: Users querying available models only see models they are permitted to use!

3. **Token Quota & Usage Limiting**:
   - Real-time token tracking for both **Streaming (SSE)** and **Non-Streaming** requests.
   - Automatically parses `usage` metadata from OpenAI/Anthropic responses and streamed chunks.
   - Blocks requests with `429 Too Many Requests` when user quota is exhausted.

4. **Modern Web Admin Dashboard (Bootstrap 5.3 + Dark Theme)**:
   - **Dashboard**: KPI metric cards, 14-day daily token usage interactive Chart.js, Top Users, Top Models.
   - **Users & Quotas**: Visual quota progress bars, model checklist selector with quick filters (Gemini Only, Claude Only, All).
   - **API Keys**: Masked key view, quick-copy, toggle status.
   - **Request Logs**: Real-time transparent audit trail (user, model, stream type, prompt/completion tokens, duration ms, client IP, status code).
   - **Models**: Live catalog synced from upstream 9router.
   - **Quick Setup**: Interactive connection guides for Cursor, Cline, Hermes Agent, and Python.

5. **Ultra Low-Resource Go Architecture**:
   - Written in Go with Chi router and SQLite WAL mode (`data/gateway.db`).
   - Sub-millisecond proxy latency (<1ms overhead).
   - Only ~5MB - 15MB RAM consumption.

---

## 🌐 Endpoints & Ports

- **Gateway Port**: `http://127.0.0.1:20129` (and Tailscale `http://100.108.204.127:20129`)
- **Upstream Target**: `http://127.0.0.1:20128` (9router Core)
- **Web Admin Dashboard**: `http://100.108.204.127:20129` or `http://localhost:20129`
  - **Default Username**: `admin`
  - **Default Password**: `admin` (can be changed in Settings or `.env`)

---

## 🛠️ Client Configuration Examples

### 1. Cursor AI
- Go to **Cursor Settings > Models > OpenAI API Key**.
- Set **Override OpenAI Base URL**: `http://127.0.0.1:20129/v1` (or Tailscale IP `http://100.108.204.127:20129/v1`)
- Set **API Key**: `sk-gw-...` (your user key generated from Dashboard)
- Model: `ag/gemini-3.8-flash-high`

### 2. Cline / Roo-Code
- **API Provider**: `OpenAI Compatible`
- **Base URL**: `http://127.0.0.1:20129/v1`
- **API Key**: `sk-gw-...`
- **Model ID**: Select any model in your allowed whitelist

### 3. Hermes Agent
Add to `~/.hermes/config.yaml`:
```yaml
custom_providers:
  - name: 9router Gateway
    base_url: http://localhost:20129/v1
    key_env: GATEWAY_API_KEY
    model: ag/gemini-3.8-flash-high
    api_mode: chat_completions
```

### 4. Curl Test
```bash
curl -X POST http://127.0.0.1:20129/v1/chat/completions \
  -H "Authorization: Bearer sk-gw-..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "ag/gemini-3.8-flash-low",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

---

## ⚙️ Service Management

The gateway runs as a persistent systemd service:

```bash
# Check status
sudo systemctl status 9router-gateway

# Restart
sudo systemctl restart 9router-gateway

# Logs
journalctl -u 9router-gateway -f
```
