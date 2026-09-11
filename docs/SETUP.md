# AI Manager Setup & Deployment Guide

This guide covers setting up AI Manager on Linux servers, configuring Cloudflare Tunnel, and integrating client coding agents.

## 1. Prerequisites
- Docker & Docker Compose v2+
- Go 1.22+ (for bare-metal execution)
- SQLite 3 with WAL support

## 2. Docker Compose Deployment

The stack is configured as a single unified compose file (`docker-compose.yml`):

```yaml
version: '3.8'

services:
  core:
    image: decolua/9router:latest
    container_name: 9router
    restart: unless-stopped
    ports:
      - "127.0.0.1:20128:20128"
    environment:
      - DATA_DIR=/app/data
      - PORT=20128
      - HOSTNAME=0.0.0.0
      - BASE_URL=${BASE_URL}
      - NEXT_PUBLIC_BASE_URL=${NEXT_PUBLIC_BASE_URL}
      - NEXT_TELEMETRY_DISABLED=1
    volumes:
      - ./data/core:/app/data

  gateway:
    build: .
    container_name: 9router_gateway
    restart: unless-stopped
    network_mode: host
    environment:
      - PORT=20129
      - HOST=0.0.0.0
      - UPSTREAM_URL=http://127.0.0.1:20128
      - DB_PATH=/app/data/gateway.db
      - NINEROUTER_DB_PATH=/app/data/core/db/data.sqlite
    env_file:
      - .env
    volumes:
      - ./data:/app/data
    depends_on:
      - core
```

### Starting the Stack
```bash
docker compose up -d
```

---

## 3. Cloudflare Tunnel Ingress

To expose AI Manager securely over HTTPS without opening firewall ports:

1. In Cloudflare Zero Trust dashboard, create a tunnel pointing `aimanager.yourdomain.com` to `http://localhost:20129`.
2. AI Manager includes built-in middleware to enforce HTTPS redirects and HSTS headers.
3. Configure `UPSTREAM_URL` to point to `http://127.0.0.1:20128` internally.

### Core domain & OAuth redirect

9router Core builds its OAuth `redirect_uri` from `BASE_URL` (fallback: request `x-forwarded-*` headers). If you serve Core on its own public domain (e.g. for provider OAuth logins):

1. Point `core.yourdomain.com` to `http://localhost:20128` in the same tunnel.
2. Set in `.env` (values here are placeholders — never commit real domains):
   ```bash
   BASE_URL=https://core.yourdomain.com
   NEXT_PUBLIC_BASE_URL=https://core.yourdomain.com
   ```
3. Recreate core: `docker compose up -d --force-recreate core`.
4. Register the callback URL (e.g. `https://core.yourdomain.com/api/auth/oidc/callback`) in your OAuth provider's authorized redirect URIs — otherwise the provider rejects the login even when `BASE_URL` is correct.

---

## 4. Hardening Checklist
- Ensure `127.0.0.1:20128` is NOT bound to `0.0.0.0`.
- Generate a strong `SESSION_SECRET` (at least 32 characters).
- Rotate default admin password on first login.

## 5. Telegram Alerts (optional)
Budget cutoffs, anomaly radar, and self-heal events notify via Telegram when configured in `.env`:
```bash
TG_BOT_TOKEN=<same token as the deploy workflow secret>
TG_CHAT_ID=-1003823512531
TG_THREAD_ID=39
```
Empty = silent (events still logged on the Radar page).
