# Velum

Zero-config behavioral pattern detection engine for product analytics. Send raw JSON events from any domain — e-commerce, ride-hailing, streaming, fintech — and get back detected anti-patterns (retry storms, silent abandonment, confusion loops, early dropoff, hesitation) with optional AI-powered natural language summaries.

Works with a single `POST` endpoint.

---

## Quick Start

### Prerequisites

- Go 1.23+
- PostgreSQL
- LLM API key (optional, for AI features) — [Groq](https://console.groq.com/keys), [OpenAI](https://platform.openai.com/api-keys), or any OpenAI-compatible provider

### Run

```bash
git clone https://github.com/velum/velum.git
cd velum
go mod tidy
cp config.yaml config.yaml        # edit with your Postgres credentials
go run cmd/velum/main.go
```

### Try It

```bash
curl -X POST http://localhost:8080/api/v1/analyze \
  -H "Content-Type: application/json" \
  -H "X-Project-ID: my-app" \
  -d '{
    "events": [
      {
        "event": "checkout_page_view",
        "ts": 1707500000000,
        "user_id": "usr-101",
        "session_id": "sess-abc",
        "device": "mobile"
      },
      {
        "event": "checkout_payment_click",
        "ts": 1707500015000,
        "user_id": "usr-101",
        "session_id": "sess-abc",
        "error_code": "card_declined"
      },
      {
        "event": "checkout_payment_click",
        "ts": 1707500045000,
        "user_id": "usr-101",
        "session_id": "sess-abc",
        "error_code": "card_declined"
      }
    ]
  }'
```

### Docker

```bash
docker network create velum-network
docker compose up --build
```

---

## API

### Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/health` | No | Health check (DB connectivity) |
| `POST` | `/api/v1/analyze` | `X-Infra-Key` (if security enabled) | Analyze events |

### Headers

| Header | Required | Description |
|--------|----------|-------------|
| `X-Project-ID` | Always | Project identifier (1–64 chars, alphanumeric/hyphens/underscores) |
| `X-Infra-Key` | When `security.enabled: true` | API key for authentication |

### Event Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `event` | string | Yes | Event name (e.g. `checkout_payment_click`) |
| `ts` | number | Yes | Timestamp in epoch milliseconds |
| `user_id` | string | Yes | User identifier |
| `session_id` | string | No | Session identifier |

Any additional properties (`device`, `error_code`, `cart_value`, etc.) are automatically classified into roles (dimension, target, condition, or measure).

### Event Naming

Velum parses `snake_case`, `camelCase`, `kebab-case`, and `dot.notation` — all are tokenized and classified automatically.

### Response

With AI enabled:

```json
{
  "success": true,
  "message": "Behavioral analysis complete",
  "request_id": "...",
  "data": {
    "ai_analysis": {
      "summary": "...",
      "details": ["..."],
      "hypotheses": ["..."],
      "confidence_note": "..."
    }
  }
}
```

Without AI, `data.patterns` is returned instead of `data.ai_analysis`.

---

## Pipeline

Events flow through an 8-layer sequential pipeline:

```
Raw Events
  │
  ▼
┌──────────────────────────────────────────────────────────┐
│  0. Context Enricher    ─ classify unknown properties    │
│  1. Vocab Enricher      ─ classify unknown words         │
│  2. Event Adapter       ─ normalize into canonical form  │
│  3. Session Flow        ─ reconstruct user journeys      │
│  4. Behavior Analyzer   ─ tag behavioral signals         │
│  5. Pattern Detector    ─ aggregate into patterns        │
│  6. Baseline Comparator ─ compare against history        │
│  7. AI Analyzer         ─ generate NL summaries          │
└──────────────────────────────────────────────────────────┘
  │
  ▼
JSON Response
```

| # | Layer | What It Does | AI |
|---|-------|--------------|----|
| 0 | **Context Enricher** | Classifies event properties as dimension, target, condition, or measure. Built-in dimensions (`device`, `country`) resolved without AI. | Yes |
| 1 | **Vocab Enricher** | Tokenizes event names, classifies unknown words as status/surface/flow via LLM. Stores learned words to DB. | Yes |
| 2 | **Event Adapter** | Normalizes raw events into canonical form using vocab + property lookups. | No |
| 3 | **Session Flow** | Groups events by `user_id` + `session_id` into chronological journeys. Splits at 30-min inactivity gaps. | No |
| 4 | **Behavior Analyzer** | Tags flows with behavioral signals: retry, abandon, hesitation, exploration, success, failure. | No |
| 5 | **Pattern Detector** | Aggregates behaviors across users into named anti-patterns. Patterns keyed by flow + context. | No |
| 6 | **Baseline Comparator** | Compares current patterns against historical snapshots. Detects trends and significance. | No |
| 7 | **AI Analyzer** | Generates natural language summary with hypotheses grounded in data. | Yes |

### Detected Patterns

| Pattern | Trigger |
|---------|---------|
| **Retry Storm** | ≥30% of users retry the same action repeatedly |
| **Silent Abandonment** | Users view content but never interact |
| **Confusion Loop** | Same event repeated ≥3 times without progress |
| **Early Dropoff** | Users bounce immediately after starting a flow |
| **Masked Failure** | Failures followed by eventual success (hidden friction) |
| **Hesitation** | Long pauses before taking action |

---

## Configuration

Config is loaded from `config.yaml`, then overridden by `VELUM_*` environment variables.

### Minimal Config

```yaml
server:
  port: "8080"
  environment: "development"

storage:
  type: "postgres"
  postgres:
    host: "localhost"
    port: 5432
    database: "velum"
    user: "velum_user"
    password: "your_password"

security:
  enabled: false
```

### Security

```yaml
security:
  enabled: true
  api_key_hash: "<sha256-hash-of-your-key>"
```

Generate a hash: `printf "my-secret-key" | shasum -a 256`, then pass `X-Infra-Key: my-secret-key` on every request.

### AI Features

All three AI layers support any OpenAI-compatible API — Groq (default), OpenAI, Together, Mistral, Fireworks, etc. The API URL is auto-resolved from the `provider` name.

**Groq (default — just set the key):**

```yaml
ai_analyzer:
  enabled: true
  provider: "groq"              # URL auto-resolved
  api_key: "gsk_..."             # or env: VELUM_AI_API_KEY
  model: "llama-3.1-8b-instant"
```

**OpenAI:**

```yaml
ai_analyzer:
  enabled: true
  provider: "openai"             # URL auto-resolved
  api_key: "sk-..."              # or env: VELUM_AI_API_KEY
  model: "gpt-4o-mini"
```

**Custom / self-hosted (Ollama, vLLM, LiteLLM, etc.):**

```yaml
ai_analyzer:
  enabled: true
  provider: "custom"
  base_url: "http://localhost:11434/v1/chat/completions"
  model: "llama3"
```

The same `provider`, `api_key`, and `model` fields are available on all three agents (`ai_analyzer`, `vocab_agent`, `context_agent`). `base_url` is only needed for custom endpoints.

If `provider` is omitted, it defaults to `"groq"`.

### Data Mapping

If your events use a different schema, map fields declaratively:

```yaml
data_mapping:
  enabled: true
  mapping:
    event:
      paths: ["payload.event.action", "event_name"]
      required: true
    ts:
      paths: ["meta.time", "timestamp"]
      format: "epoch_ms"
      required: true
    user_id:
      paths: ["context.user.id", "user_id"]
      required: true
    session_id:
      paths: ["context.session.id", "session_id"]
```

### Environment Variables

| Variable | Overrides |
|----------|-----------|
| `VELUM_PORT` | `server.port` |
| `VELUM_ENV` | `server.environment` |
| `VELUM_DB_HOST` | `storage.postgres.host` |
| `VELUM_DB_USER` | `storage.postgres.user` |
| `VELUM_DB_PASSWORD` | `storage.postgres.password` |
| `VELUM_DB_NAME` | `storage.postgres.database` |
| `VELUM_DB_PORT` | `storage.postgres.port` |
| `VELUM_DB_SSL_MODE` | `storage.postgres.ssl_mode` |
| `VELUM_AI_API_KEY` | `ai_analyzer.api_key` |
| `VELUM_VOCAB_AGENT_API_KEY` | `vocab_agent.api_key` |
| `VELUM_CONTEXT_AGENT_API_KEY` | `context_agent.api_key` |
| `VELUM_API_KEY_HASH` | `security.api_key_hash` |

---

## Multi-Tenancy

Each `X-Project-ID` gets isolated storage. Pattern baselines are stored in per-project tables. Vocabulary and property registry are shared across projects.

---

## Testing

```bash
go test ./...
```

---

## License

MIT
