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
Fetches current dashboard KPI metrics, user quotas, and FinOps calculations.

**Response:**
```json
{
  "total_requests": 1750,
  "total_tokens": 243125000,
  "cost_saved_usd": 1139.84,
  "cost_saved_idr": 18237427,
  "rtk_tokens_saved": 91187137
}
```

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
