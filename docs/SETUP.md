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
      - INITIAL_PASSWORD=Kepoloe#123
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

---

## 4. Hardening Checklist
- Ensure `127.0.0.1:20128` is NOT bound to `0.0.0.0`.
- Generate a strong `SESSION_SECRET` (at least 32 characters).
- Rotate default admin password on first login.
