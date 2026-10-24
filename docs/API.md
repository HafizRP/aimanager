# AI Manager API Documentation

This document outlines the API endpoints exposed by AI Manager.

## 1. LLM Client Endpoints (OpenAI-Compatible)

All LLM endpoints require a Bearer token issued via the AI Manager Dashboard (`/keys`).

```http
Authorization: Bearer <API_KEY>
```

### POST `/v1/chat/completions`
Send chat messages to upstream providers through the router.

**Request Body:**
```json
{
  "model": "main",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ],
  "temperature": 0.7,
  "max_tokens": 1000,
  "stream": true
}
```

**Response (Non-streaming):**
```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "created": 1789048766,
  "model": "main",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! How can I assist you today?"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 12,
    "completion_tokens": 9,
    "total_tokens": 21
  }
}
```

**Caching Header:**
- If the exact prompt was seen within the last 15 minutes, AI Manager returns:
  ```http
  X-Cache: HIT
  ```

---

### GET `/v1/models`
Returns list of available models and combos filtered according to the user's whitelist and key scope.

---

## 2. Metrics & Audit Endpoints

Requires session authentication (`gw_session` cookie).

### GET `/api/stats`
Fetches current dashboard KPI metrics, user quotas, FinOps calculations, and latency SLO percentiles.

**Query Parameters:**
- `timeframe`: `1h` | `4h` | `1d` | `1w` | `1m` | `all` (default `1d`). Scopes both the usage buckets and the latency window.

**Response:**
```json
{
  "total_requests": 1750,
  "total_tokens": 243125000,
  "cost_saved_usd": 1139.84,
  "cost_saved_idr": 18237427,
  "rtk_tokens_saved": 91187137,
  "latency_sample": 412,
  "latency_p50_ms": 4216,
  "latency_p95_ms": 22721,
  "latency_p99_ms": 48597,
  "latency_by_model": [
    { "model": "ag/gemini-3.8-flash-high", "requests": 253, "p50_ms": 4425, "p95_ms": 13908, "p99_ms": 48597, "max_ms": 61240 }
  ]
}
```

Latency percentiles use **nearest-rank** over successful (`status_code < 400`) requests with a
non-zero `duration_ms` in the selected window, so slow upstream failures never inflate the SLO.
`latency_by_model` is capped at 12 models, ordered by p95 descending. Grades are derived from
p95: `fast` (< 5s), `ok` (< 15s), `slow` (< 30s), `bad` (>= 30s).

---

### GET `/api/logs`
Returns paginated request logs.

**Query Parameters:**
- `cursor`: Base64 opaque cursor
- `limit`: Number of rows (default `50`)

---

### GET `/api/logs/export`
Exports audit logs directly as a file download.

**Query Parameters:**
- `format`: `csv` or `json`

---

### POST `/api/benchmark/run`
Triggers concurrent TTFT and latency tests against specified models.

**Request Body:**
```json
{
  "models": ["main", "free-only", "ag/gemini-3.8-flash-high"]
}
```
