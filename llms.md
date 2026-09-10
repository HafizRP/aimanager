# AI Manager - LLM Context Document

> This document provides structured context for LLMs and coding agents working with this codebase.

## Project Overview

**AI Manager** (`aimanager`) is a Go-based reverse proxy and multi-tenant management gateway for LLM APIs. It sits in front of **9router Core** and provides authentication, rate limiting, quota tracking, and a web admin dashboard.

### Primary Purpose
- Route LLM requests (OpenAI-compatible `/v1/chat/completions`) to upstream providers
- Manage users, API keys, and token quotas
- Track usage analytics and cost savings
- Provide web UI for administration

### Tech Stack
- **Language**: Go 1.22+
- **Router**: Chi v5 (`github.com/go-chi/chi/v5`)
- **Database**: SQLite with WAL mode (`modernc.org/sqlite`)
- **Templates**: Go `html/template` with `embed.FS`
- **Auth**: Server-side sessions with `bcrypt` password hashing
- **Architecture**: Monolithic Go binary, single-process

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  main.go (Entry)                    │
│  - Config loading                                   │
│  - Route registration                               │
│  - Middleware chain                                 │
└──────────────────┬──────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────┐
│            internal/handlers/                       │
│  - HTTP handlers for UI pages and API endpoints     │
│  - Template rendering                               │
│  - Request validation                               │
└──────────────────┬──────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────┐
│            internal/proxy/                          │
│  - GatewayProxy: Reverse proxy engine               │
│  - SSE streaming support                            │
│  - Token usage interception                         │
│  - Rate limiting & caching                          │
└──────────────────┬──────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────┐
│         internal/repository/                        │
│  - SQLite data access layer                         │
│  - User, API key, logs, sessions                    │
└─────────────────────────────────────────────────────┘
```

## Key Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point, route definitions, server startup |
| `internal/config/config.go` | Configuration struct and env loading |
| `internal/proxy/proxy.go` | Reverse proxy engine with auth/quotas |
| `internal/repository/repo.go` | SQLite data layer |
| `internal/handlers/handlers.go` | Handler struct and template rendering |
| `internal/models/models.go` | Data structs (User, APIKey, etc.) |

## Data Models

```go
type User struct {
    ID            string
    Username      string
    PasswordHash  string
    Role          string    // "admin" or "user"
    TokenQuota    int64     // 0 = unlimited
    TokensUsed    int64
    AllowedModels string    // JSON array or "*"
}

type APIKey struct {
    ID            string
    UserID        string
    Key           string    // "aim_" prefix
    Name          string
    IsActive      bool
    AllowedModels string    // Scoped models
    RateLimitRPM  int       // Requests per minute
}
```

## Environment Variables

| Variable | Required | Default |
|----------|----------|---------|
| `PORT` | No | `20129` |
| `HOST` | No | `0.0.0.0` |
| `UPSTREAM_URL` | No | `http://127.0.0.1:20128` |
| `DB_PATH` | No | `data/gateway.db` |
| `ADMIN_USERNAME` | No | `admin` |
| `ADMIN_PASSWORD` | No | `admin123` |
| `SESSION_SECRET` | Yes | - |

## API Endpoints

### OpenAI-Compatible
- `POST /v1/chat/completions` - Chat completion (streaming/non-streaming)
- `GET /v1/models` - List available models

### Dashboard
- `GET /` - Dashboard home
- `GET /login` - Login page
- `POST /login` - Authenticate
- `GET /logout` - Clear session

### Admin API
- `GET /api/stats` - Usage metrics
- `GET /api/logs` - Request audit logs
- `POST /api/benchmark/run` - Speed test models

## Common Tasks

### Add New API Endpoint
1. Create handler in `internal/handlers/`
2. Register route in `main.go` under `authRouter` or `publicRouter`
3. Add template if needed in `embeds/templates/`

### Modify Proxy Behavior
Edit `internal/proxy/proxy.go`:
- `HandleProxy()` - Main proxy logic
- `handleStreamingResponse()` - SSE streaming
- `handleNonStreamingResponse()` - Regular JSON

### Database Schema Changes
1. Add migration in `internal/database/db.go`
2. Update models in `internal/models/models.go`
3. Update repository methods

## Testing

```bash
# Run all tests
go test ./...

# Run specific package
go test ./internal/proxy

# With coverage
go test -cover ./...
```

## Deployment

```bash
# Build
go build -ldflags="-w -s" -o 9router-gateway .

# Docker
docker compose up -d --build

# Systemd
sudo systemctl restart 9router-gateway
```

## Known Constraints

1. **Port 20128** must remain localhost-only (security requirement)
2. **SQLite WAL mode** required for concurrent access
3. **Session tokens** are 64-char hex strings stored in DB
4. **API keys** must use `aim_` prefix for identification
