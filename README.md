# Velum

Behavioral analytics pipeline for product events. Send raw JSON events, get back detected patterns like retry storms, silent abandonment, confusion loops, and hesitation — with optional AI-powered natural language summaries.

---

## Quick Start

### Prerequisites

- Go 1.23+
- PostgreSQL

### Install & Run

```bash
git clone https://github.com/your-org/velum.git
cd velum
go mod tidy
cp example.config.yaml config.yaml   # edit with your Postgres credentials
go run cmd/velum/main.go
```

### Send Events

Every request must include the `X-Project-ID` header. Each project gets isolated storage (per-project tables).

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
        "device": "mobile",
        "country": "US"
      },
      {
        "event": "checkout_payment_click",
        "ts": 1707500015000,
        "user_id": "usr-101",
        "session_id": "sess-abc",
        "device": "mobile",
        "error_code": "card_declined"
      },
      {
        "event": "checkout_payment_click",
        "ts": 1707500045000,
        "user_id": "usr-101",
        "session_id": "sess-abc",
        "device": "mobile",
        "error_code": "card_declined"
      }
    ]
  }'
```

### Health Check

```bash
curl http://localhost:8080/health
```

---

## API

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| `GET` | `/health` | No | Health check (DB connectivity) |
| `POST` | `/api/v1/analyze` | Yes (if enabled) | Analyze events |

### Required Headers

| Header | Required | Description |
|--------|----------|-------------|
| `X-Project-ID` | Always | Project identifier (1–64 alphanumeric, hyphens, underscores) |
| `X-Infra-Key` | When `security.enabled: true` | API key for authentication |

### Request Body

```json
{
  "events": [
    {
      "event": "checkout_payment_click",
      "ts": 1707500000000,
      "user_id": "usr-123",
      "session_id": "sess-abc",
      "device": "mobile",
      "country": "US",
      "error_code": "card_declined",
      "cart_value": 120.50
    }
  ]
}
```

### Event Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `event` | string | Yes | Event name (e.g. `checkout_payment_click`) |
| `ts` | number | Yes | Timestamp in epoch milliseconds |
| `user_id` | string | Yes | User identifier |
| `session_id` | string | No | Session identifier |

Any additional properties (`device`, `error_code`, `cart_value`, etc.) are automatically classified into roles — see [Property Roles](#property-roles).

### Event Naming

Velum parses multiple naming conventions:

| Format | Example | Parsed Tokens |
|--------|---------|---------------|
| Snake case | `checkout_payment_success` | checkout, payment, success |
| Kebab case | `home-page-view` | home, page, view |
| Camel case | `userLoginFailed` | user, login, failed |
| Dot notation | `cart.item.added` | cart, item, added |

---

## Pipeline Layers

Events flow through an 8-layer pipeline. Each layer enriches or analyzes the data before passing it to the next.

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

### Layer Details

| # | Layer | Purpose | AI Required | Storage |
|---|-------|---------|-------------|---------|
| 0 | **Context Enricher** | Discovers unknown event properties (e.g. `error_code`, `plan_name`) and classifies them as **target** or **condition** via AI. Results stored to DB for future requests. Built-in dimensions (`device`, `country`) and numeric measures are resolved without AI. | Yes | PostgreSQL (`property_registry`) |
| 1 | **Vocab Enricher** | Tokenizes event names, finds unknown words, and classifies them as **status**, **surface**, or **flow** via AI. Learned words stored to DB. | Yes | PostgreSQL (`vocabulary`) |
| 2 | **Event Adapter** | Normalizes raw events into canonical form. Looks up each token from the vocabulary DB and each property from the property registry. Builds structured context (dimensions, targets, conditions, measures). | No | Reads from DB |
| 3 | **Session Flow Reconstructor** | Groups events by `user_id` + `session_id` into chronological user journeys. Identifies flow boundaries and transitions. | No | None |
| 4 | **Behavior Analyzer** | Analyzes flows for behavioral signals: hesitation (long pauses), retries (repeated actions), abandonment (incomplete flows), exploration (non-linear navigation). | No | None |
| 5 | **Pattern Detector** | Aggregates individual behavioral signals across users into named patterns (retry storm, silent abandonment, confusion loop, etc.). Patterns are keyed by context (e.g. retry storm in payment where `error_code=card_declined`). | No | None |
| 6 | **Baseline Comparator** | Compares detected patterns against historical snapshots. Identifies trends (increasing, decreasing, stable), significance levels, and first observations. Snapshots stored per project. | No | PostgreSQL (`pattern_snapshots_{project_id}`) |
| 7 | **AI Analyzer** | Takes baseline comparison results and generates a natural language summary with hypotheses. Only called when patterns exist. Strict prompt rules prevent hallucination. | Yes | None |

### Detected Patterns

| Pattern | Description |
|---------|-------------|
| Retry Storm | >30% of users retry the same action repeatedly |
| Silent Abandonment | Users view content but never interact |
| Confusion Loop | Repeated navigation without progress |
| Early Dropoff | Users abandon immediately after starting a flow |
| Hesitation | Long pauses before taking action |

---

## Property Roles

Extra properties on events are automatically classified into four roles:

| Role | How Resolved | Splits Baselines | Examples |
|------|-------------|-------------------|----------|
| **Dimension** | Built-in list | No | `device`, `country`, `platform`, `browser` |
| **Target** | AI classification | Yes | `plan_name`, `product_id`, `feature_name` |
| **Condition** | AI classification | Yes | `error_code`, `ab_variant`, `retry_reason` |
| **Measure** | Type inference (numeric) | No | `cart_value`, `load_time_ms` |

Targets and conditions form the **context key** for baselines. A retry storm on `checkout` where `error_code=card_declined` is tracked separately from `error_code=timeout`.

### Built-in Dimensions

These field names are recognized automatically without AI:

| Category | Accepted Field Names |
|----------|---------------------|
| Device | `device`, `device_type`, `deviceType`, `device_model`, `deviceModel` |
| Platform/OS | `platform`, `os`, `os_name`, `osName`, `os_version`, `osVersion` |
| Browser | `browser`, `browser_name`, `browserName`, `browser_version`, `browserVersion` |
| Geography | `country`, `region`, `city`, `locale`, `timezone`, `tz` |
| App Version | `app_version`, `appVersion`, `build`, `build_version`, `version` |
| Attribution | `channel`, `source`, `medium`, `utm_source`, `utm_medium`, `utm_campaign`, `referrer` |
| Network | `network_type`, `networkType`, `connection_type`, `connectionType` |
| Language | `language`, `lang` |
| User Segment | `user_type`, `userType`, `user_role`, `userRole`, `user_segment`, `userSegment` |

---

## Vocabulary System

Event name tokens are classified into three categories:

| Category | Examples | Meaning |
|----------|----------|---------|
| **Status** | click, success, failed, error, retry | What happened |
| **Surface** | button, modal, sidebar, checkout, cart | Where it happened |
| **Flow** | payment, auth, registration, search | User intent/journey |

Example: `checkout_payment_failed` → `{surface: checkout, flow: payment, status: failed}`

Unknown words are marked as "uncategorized" by default. Enable `vocab_agent` to classify them via AI.

---

## Configuration

Velum is configured via `config.yaml`. Environment variables override file values.

```bash
cp example.config.yaml config.yaml
```

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

Generate hash: `printf "my-secret-key" | shasum -a 256`

Then pass `X-Infra-Key: my-secret-key` on every request.

### AI Features

All three AI layers use [Groq](https://console.groq.com/keys) (free tier available).

```yaml
context_agent:
  enabled: true
  api_key: "gsk_..."      # or env: VELUM_CONTEXT_AGENT_API_KEY
  model: "llama-3.1-8b-instant"

vocab_agent:
  enabled: true
  api_key: "gsk_..."      # or env: VELUM_VOCAB_AGENT_API_KEY
  model: "llama-3.1-8b-instant"

ai_analyzer:
  enabled: true
  api_key: "gsk_..."      # or env: VELUM_AI_API_KEY
  model: "llama-3.1-8b-instant"
```

### Baseline Detection

```yaml
baseline:
  window_days: 28           # Historical window
  min_days: 7               # Min data before computing baseline
  computation_mode: "daily" # "daily" (cached) or "always"
  trend_threshold: 0.10     # 10% change = significant
```

### Resiliency

```yaml
resiliency:
  rate_limit_requests: 100  # Max req/s
  circuit_breaker:
    enabled: true
    failure_threshold: 5    # Open after 5 AI failures
    reset_timeout: "30s"
```

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
      required: false
```

### Environment Variables

| Variable | Overrides |
|----------|-----------|
| `VELUM_PORT` | `server.port` |
| `VELUM_ENV` | `server.environment` |
| `VELUM_AI_API_KEY` | `ai_analyzer.api_key` |
| `VELUM_VOCAB_AGENT_API_KEY` | `vocab_agent.api_key` |
| `VELUM_CONTEXT_AGENT_API_KEY` | `context_agent.api_key` |

---

## Logging

Velum uses Go's `log/slog` structured logging.

| Environment | Format | Level | Output |
|-------------|--------|-------|--------|
| `development` | Text (human-readable) | Debug | stdout |
| `production` | JSON (machine-parseable) | Info | stdout |

Logs go to stdout. In containerized deployments, the orchestrator (Docker, k8s, ECS) captures them automatically.

---

## Multi-Tenancy

Each `X-Project-ID` gets isolated storage. Pattern snapshots are stored in per-project tables (`pattern_snapshots_{project_id}`). Vocabulary and property registry are shared across projects.

---

## Testing

```bash
go test ./...              # Run all tests
go test ./... -v           # Verbose
go test ./... -cover       # With coverage
```

---

## License

MIT
