# Velum — Product Healing Agent

Detects hidden UX friction from real user behavior and tells your team what to fix first.

---

## Try it now

No database. No API key. No config.

```bash
git clone https://github.com/velum-oss/velum.git
cd velum
go run cmd/velum/main.go --demo
```

Or with Docker:

```bash
docker run --rm -p 8080:8080 ghcr.io/velum-oss/velum:demo
```

Then send it events:

```bash
curl -X POST http://localhost:8080/api/v1/analyze \
  -H "Content-Type: application/json" \
  -H "X-Project-ID: my-app" \
  -d @test_cases/a1_retry_storm.json
```

```json
{
  "success": true,
  "message": "Behavioral analysis complete",
  "data": {
    "patterns": [
      {
        "type": "retry_storm",
        "severity": "medium",
        "confidence": "high",
        "flow": "checkout",
        "affected_users": 3,
        "impact_ratio": 0.5,
        "evidence": "High frequency of retry attempts detected"
      }
    ]
  }
}
```

No fake data. `test_cases/` contains real event shapes for all seven patterns — or send your own.

---

## What Velum detects

The friction patterns that get buried in raw event tables — named, measured, and prioritized:

| Pattern | What it means |
|---------|--------------|
| **Retry Storm** | A large share of users are repeatedly hammering the same action — usually because it's failing silently |
| **Confusion Loop** | Users are cycling through the same events without progressing — a sign of broken UX or unclear state |
| **Silent Abandonment** | Users showed intent (clicked, interacted) but left without an error — quiet churn |
| **Early Dropoff** | Users are bouncing immediately after entering a flow — before they even engage with it |
| **Masked Failure** | Users eventually succeed, but only after hitting failures — hidden friction that looks fine in conversion metrics |
| **Bypass Behavior** | Users are skipping expected steps — either finding shortcuts or working around broken flows |
| **Funnel Dropoff** | Statistically significant user loss between specific funnel steps you define |

Each pattern comes with: severity, confidence, affected user count, impact ratio, baseline comparison (getting better or worse?), and — when AI is enabled — prioritized fix recommendations.

---

## The problem

Amplitude, PostHog, Mixpanel, Segment — they're exceptional at collecting events and rendering charts. But they stop at the data layer. When 40% of your users drop off at checkout, they'll show you the number. They won't tell you it's a retry storm, or a confusion loop, or silent abandonment. They won't tell you it got 18% worse this week. And they certainly won't tell you what to fix first.

```
Your App → Amplitude / PostHog / Segment / Mixpanel
                        │
                        └──→ Velum (Product Healing Agent)
                                │
                                ├──→ "Retry storm at checkout, affecting 38% of users.
                                │     Up 21% vs. last 28 days. High significance."
                                │
                                └──→ "[HIGH] Add inline error messaging on payment
                                      failure explaining why the card was declined
                                      and suggest alternative payment methods."
```

Velum is the **Product Healing Agent** that sits **on top of your existing analytics stack**. Feed it the same events your tools already collect. Get back named, quantified friction patterns — with severity, trend impact, and prioritized recommendations for what to heal first.

**This repository is the open-source core engine** — self-hostable, auditable, MIT licensed. A cloud version with an analysis dashboard, scheduled baseline jobs, and native connectors for major analytics platforms is in development.

---

## Production setup

**Prerequisites:** Go 1.23+, PostgreSQL, LLM API key (optional)

```bash
go mod tidy
cp .env.example .env    # fill in Postgres credentials + optional Groq key
go run cmd/velum/main.go
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
| `GET` | `/health` | None | Health check |
| `POST` | `/api/v1/baseline` | `X-Infra-Key` (if enabled) | Ingest events and store baseline snapshots |
| `POST` | `/api/v1/analyze` | `X-Infra-Key` (if enabled) | Detect patterns, compare against stored baselines |

**How they work together:**

- **`/api/v1/baseline`** — Feed historical event batches to build up a baseline. Call on a schedule or as a one-time backfill.
- **`/api/v1/analyze`** — Send a batch of events for analysis. Detects patterns, compares against baselines, returns trends. Never writes to storage.

Both endpoints accept the same request body and require the `X-Project-ID` header.

### Headers

| Header | Required | Description |
|--------|----------|-------------|
| `X-Project-ID` | Always | Project identifier (1–64 chars, alphanumeric/hyphens/underscores) |
| `X-Infra-Key` | When `security.enabled: true` | API key for authentication |

### Event fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Event ID (unique per event) |
| `event` | string | Yes | Event name (e.g. `checkout_payment_click`) |
| `ts` | number | Yes | Timestamp in epoch milliseconds |
| `user_id` | string | Yes | User identifier |
| `session_id` | string | No | Session identifier |

Any additional properties (`device`, `error_code`, `cart_value`, etc.) are automatically classified into roles.

### Response (without AI)

```json
{
  "success": true,
  "message": "Behavioral analysis complete",
  "request_id": "...",
  "data": {
    "patterns": [...]
  }
}
```

### Response (with AI enabled)

```json
{
  "success": true,
  "message": "Behavioral analysis complete",
  "data": {
    "ai_analysis": {
      "summary": "...",
      "recommendations": [
        "[HIGH] Add inline error messaging on payment failure...",
        "[MEDIUM] Add a progress indicator during payment processing...",
        "[MONITOR] Track retry rate weekly against the 28-day baseline."
      ]
    }
  }
}
```

---

## Pipeline

Events flow through a sequential detection pipeline:

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
│  7. AI Analyzer         ─ diagnose & recommend fixes     │
└──────────────────────────────────────────────────────────┘
  │
  ▼
JSON Response
```

Layers 0, 1, and 7 require LLM API keys. Layers 2–6 are fully deterministic — no external calls. Demo mode runs layers 2–6.

| # | Layer | What it does | Needs LLM |
|---|-------|--------------|-----------|
| 0 | **Context Enricher** | Classifies event properties as dimension, target, condition, or measure | Yes |
| 1 | **Vocab Enricher** | Tokenizes event names, classifies unknown words as status/surface/flow | Yes |
| 2 | **Event Adapter** | Normalizes raw events into canonical form using vocab + property lookups | No |
| 3 | **Session Flow** | Groups events by `user_id` + `session_id` into journeys, splits at 30-min gaps | No |
| 4 | **Behavior Analyzer** | Tags flow instances: retry, abandon, hesitation, exploration, success, failure | No |
| 5 | **Pattern Detector** | Aggregates behaviors across users into named anti-patterns | No |
| 6 | **Baseline Comparator** | Compares current patterns against stored historical snapshots | No |
| 7 | **AI Analyzer** | Diagnoses friction, recommends prioritized product/UX fixes | Yes |

---

## Configuration

Config is loaded from `config.yaml`, then overridden by `VELUM_*` environment variables.

### Minimal config

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

### AI features (optional)

All three AI layers support any OpenAI-compatible API — Groq (default), OpenAI, Together, Mistral, Fireworks, etc.

```yaml
ai_analyzer:
  enabled: true
  provider: "groq"
  api_key: "gsk_..."       # or env: VELUM_AI_API_KEY
  model: "llama-3.1-8b-instant"

vocab_agent:
  enabled: true
  provider: "groq"
  api_key: "gsk_..."       # or env: VELUM_VOCAB_AGENT_API_KEY
  model: "llama-3.1-8b-instant"

context_agent:
  enabled: true
  provider: "groq"
  api_key: "gsk_..."       # or env: VELUM_CONTEXT_AGENT_API_KEY
  model: "llama-3.1-8b-instant"
```

### Security

```yaml
security:
  enabled: true
  api_key_hash: "<sha256-hash-of-your-key>"
```

Generate a hash: `printf "my-secret-key" | shasum -a 256`, then pass `X-Infra-Key: my-secret-key` on every request.

### Baseline detection

```yaml
baseline:
  window_days: 28
  min_days: 7
  min_affected_users: 5
  trend_threshold: 0.10
  high_significance_threshold: 0.15
```

### Environment variables

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

### Data mapping

If your events use a different schema:

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
```

---

## Multi-tenancy

Each `X-Project-ID` gets isolated baseline storage. Vocabulary and property registry are shared across projects.

---

## Testing

```bash
go test ./...
```

Test cases covering all patterns are in `test_cases/`. Named patterns (`a1_retry_storm.json`, `a2_confusion_loop.json`, etc.) can be sent directly to `/api/v1/analyze` for manual testing.

---

## Origin

This started from a recurring problem: a 35% checkout drop rate, a week of data analysis, and a root cause that turned out to be a payment gateway timing out on mobile in India — causing users to retry 3–4 times before giving up. The pattern was obvious in hindsight, invisible in a dashboard.

Velum is the tool that should have caught that in minutes.

---

## License

MIT
