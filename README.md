# Velum — Product Healing Agent

Detects hidden UX friction and product gaps from real user behavior, then tells your team what to fix first.

Amplitude, PostHog, Mixpanel, Segment — they're exceptional at collecting events and rendering charts. But they stop at the data layer. When 40% of your users drop off at checkout, they'll show you the number. They won't tell you it's a retry storm, or a confusion loop, or silent abandonment — they won't tell you it got 18% worse this week — and they certainly won't tell you what to fix first.

Velum is the **Product Healing Agent** that sits **on top of your existing analytics stack**. Feed it the same events your tools already collect. Get back named, quantified friction patterns — with severity, trend impact, and prioritized recommendations for what to heal first.

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

**This repository is the open-source core engine** — self-hostable, auditable, MIT licensed. A cloud version with an analysis dashboard, scheduled baseline jobs, and native source connectors for major analytics platforms is in development.

---

## What Velum Heals

The friction patterns that get buried in raw event tables — detected, diagnosed, and prioritized:

| Pattern | What it means |
|---------|--------------|
| **Retry Storm** | A large share of users are repeatedly hammering the same action — usually because it's failing silently |
| **Confusion Loop** | Users are cycling through the same events without progressing — a sign of broken UX or unclear state |
| **Silent Abandonment** | Users land, look around, and leave without a single meaningful interaction |
| **Early Dropoff** | Users are bouncing immediately after entering a flow — before they even engage with it |
| **Masked Failure** | Users eventually succeed, but only after hitting failures — hidden friction that looks fine in conversion metrics |
| **Bypass Behavior** | Users are skipping expected steps — either finding shortcuts or working around broken flows |
| **Funnel Dropoff** | Statistically significant user loss between specific funnel steps you define |

Each pattern comes with severity, confidence, affected user count, baseline comparison showing whether it's getting better or worse, and — when AI is enabled — prioritized recommendations for what to fix first.

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

Velum exposes two core endpoints — one for **building baselines** from historical data, and one for **analyzing** new events against those baselines.

#### 1. Build a baseline

Feed historical events to create a baseline snapshot. Do this first (or on a schedule) so Velum has some data to compare against.
> **Note:** The baseline step is optional. Without it, `/api/v1/analyze` still detects all patterns in your batch — you just won't get trend comparisons ("increasing", "decreasing") since there's no historical data to compare against.

```bash
curl -X POST http://localhost:8080/api/v1/baseline \
  -H "Content-Type: application/json" \
  -H "X-Project-ID: my-app" \
  -d @test_cases/a1_retry_storm.json
```

Response:

```json
{
  "success": true,
  "message": "Baseline snapshot stored",
  "request_id": "...",
  "data": {
    "patterns_stored": 1,
    "snapshot_date": "2026-03-25",
    "patterns": [...]
  }
}
```

#### 2. Analyze events

Send a batch of events to detect friction patterns and get healing recommendations. The analysis compares against stored baselines (read-only — no baseline writes happen here).

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
| `POST` | `/api/v1/analyze` | `X-Infra-Key` (if security enabled) | Analyze events against stored baselines (read-only, no baseline writes) |
| `POST` | `/api/v1/baseline` | `X-Infra-Key` (if security enabled) | Ingest events and store baseline snapshots (no AI summary) |

**How the two endpoints work together:**

- **`/api/v1/baseline`** — Feed historical event batches (e.g., yesterday's data, last week's data) to build up a baseline. Each call runs pattern detection and stores a snapshot keyed by date + pattern + flow. Call this on a schedule (daily, hourly) or as a one-time backfill.
- **`/api/v1/analyze`** — Send a batch of events for real-time analysis. Velum detects patterns, compares them against stored baselines, and returns trends ("retry storms increased 21% vs. 28-day average"). This endpoint **never** writes to baseline storage — it's purely read + analyze.

Both endpoints accept the same request format (`events` array + optional `analysis_context`). Both require the `X-Project-ID` header.

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
      "recommendations": [
        "[HIGH] Specific, actionable product/UX fix for the highest-severity pattern.",
        "[MEDIUM] Fix for the next pattern, citing data points.",
        "[MONITOR] What to track and when to revisit."
      ],
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
│  7. AI Analyzer         ─ diagnose & recommend fixes     │
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
| 7 | **AI Analyzer** | Diagnoses friction, generates summary, and recommends prioritized product/UX fixes. | Yes |

### Severity & Significance

Pattern **severity** is weighted by pattern type and flow intent:

| Pattern | Weight | Notes |
|---------|--------|-------|
| Retry Storm | 1.0 | Most impactful |
| Masked Failure | 0.9 | Hidden friction |
| Funnel Dropoff | 0.8 | Revenue impact |
| Silent Abandonment | 0.7 | Lost engagement |
| Early Dropoff | 0.6 | May be expected |
| Confusion Loop | 0.5 | UX friction |
| Bypass Behavior | 0.4 | Least impactful |

Transactional flows get a 1.5× multiplier; browse flows get 0.7×.

Baseline **significance** is capped at `low` when affected users are below `min_affected_users` (default: 5). The pattern is still reported with `low_volume: true` so dashboards can filter or display it, but it won't trigger high-priority alerts on statistically thin data.

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

### Baseline Detection

```yaml
baseline:
  window_days: 28               # Days of history for baseline computation
  min_days: 7                   # Minimum days before baseline is valid
  min_affected_users: 5         # Below this, significance is capped at "low"
  computation_mode: "daily"     # "daily" (cached) or "always" (per-request)
  trend_threshold: 0.10         # 10% delta to flag increasing/decreasing
  high_significance_threshold: 0.15  # 15% delta for high significance
  std_deviation_multiplier: 2.0      # Multiplier for std-based significance
```

`min_affected_users` prevents low-volume patterns (e.g., 1 user with 100% impact ratio) from being flagged as high significance. Set to `1` to disable the guard.

#### How Baseline Works

Every analysis request:
1. **Detects patterns** in the current batch (stateless, works for any time window)
2. **Compares** each pattern's impact ratio against the stored historical average (last `window_days`)
3. **Stores** the current snapshot via upsert — keyed on `(date, pattern_type, flow, context_key)`

| Baseline status | Condition | Behavior |
|---|---|---|
| `first_observation` | 0 historical snapshots | Stores snapshot, returns unknown trend |
| `insufficient_data` | 1–6 days of history | Stores snapshot, returns unknown trend |
| `sufficient` | ≥7 days of history | Computes avg + stddev, returns trend + significance |
| `out_of_window` | Data older than 28 days | Skips storage and comparison entirely |

**Trend** is classified by delta percentage: ≥10% increase → `increasing`, ≥10% decrease → `decreasing`, otherwise `stable`.

**Significance** uses standard deviation when available (`delta ≥ 2×stddev` → `high`), falls back to absolute threshold (`delta ≥ 0.15` → `high`). Capped at `low` when affected users < `min_affected_users`.

#### Data Ingestion Guidelines

- **Consistent windows**: For meaningful baseline comparisons, send the same time window each ingestion (e.g., always a full day). Inconsistent window sizes produce different denominators, making ratio comparisons noisy.
- **No overlap**: Avoid sending overlapping event batches for the same day. The last batch overwrites the snapshot (upsert), so overlapping batches cause the stored ratio to reflect only the last batch.
- **Re-processing**: Sending the same complete batch again is safe — the upsert overwrites with identical values.
- **Ad-hoc analysis**: The `/api/v1/analyze` endpoint never writes to baseline history, so investigative queries with non-standard windows are always safe.

#### Retention & Cleanup

Snapshots are auto-deleted after `retention_days` (default: **90 days**). A background goroutine runs cleanup on startup and every 24 hours.

```yaml
storage:
  retention_days: 90    # Snapshots older than this are deleted
```

| Time boundary | Default | Purpose |
|---|---|---|
| `baseline.window_days` | 28 days | How far back to look for comparison |
| `baseline.min_days` | 7 days | Minimum history before comparison is valid |
| `storage.retention_days` | 90 days | When data is permanently deleted |

The 62-day gap between `window_days` and `retention_days` means historical snapshots are preserved in case you widen the baseline window later.

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
