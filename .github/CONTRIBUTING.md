# AI-Ready Repository Standards

This repository follows AI-ready conventions for seamless integration with coding agents and AI assistants.

## Repository Structure

```
aimanager/
├── README.md                 # Comprehensive documentation
├── docs/
│   ├── API.md               # API reference
│   └── SETUP.md             # Deployment guide
├── internal/                 # Go internal packages
│   ├── handlers/            # HTTP handlers
│   ├── models/              # Data models
│   ├── proxy/               # Reverse proxy engine
│   ├── repository/          # SQLite data layer
│   ├── syncer/              # Background key sync
│   ├── upstream/            # 9router Core client
│   └── worker/              # Background daemons
├── embeds/templates/        # HTML templates (Go embed)
├── main.go                  # Application entry point
├── docker-compose.yml       # Unified stack definition
├── Dockerfile               # Gateway container build
├── .env.example             # Configuration template
└── .gitignore               # Excluded files
```

## AI Agent Integration

### Claude Code CLI
```bash
export ANTHROPIC_BASE_URL="https://aimanager.b14.my.id/v1"
export ANTHROPIC_API_KEY="aim_your_key_here"
claude
```

### Cursor IDE
1. Settings > Models > OpenAI API Key
2. Base URL: `https://aimanager.b14.my.id/v1`
3. API Key: `aim_...`

### Hermes Agent
```yaml
# ~/.hermes/config.yaml
custom_providers:
  - name: AI Manager
    base_url: https://aimanager.b14.my.id/v1
    key_env: AIMANAGER_API_KEY
    model: main
    api_mode: chat_completions
```

## Code Conventions

- **Go version**: 1.22+
- **Router**: Chi v5
- **Database**: SQLite with WAL mode
- **Templates**: Go `embed.FS` with `html/template`
- **Style**: Standard Go formatting (`gofmt`)

## Quick Commands

```bash
# Build binary
go build -ldflags="-w -s" -o 9router-gateway .

# Run locally
./9router-gateway

# Docker deployment
docker compose up -d --build

# Run tests
go test ./...

# Database inspection
sqlite3 data/gateway.db ".schema"
```

## Key Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /healthz` | Health check |
| `POST /v1/chat/completions` | OpenAI-compatible chat |
| `GET /v1/models` | Available models |
| `GET /api/stats` | Dashboard metrics |
| `GET /api/logs` | Request audit logs |
| `POST /api/benchmark/run` | Speed benchmark |
