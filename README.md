# Velum

### Transform Analytics Events into Behavioral Intelligence

Velum is a behavioral infrastructure layer that sits between your raw analytics data and your decision-making systems. It automatically detects patterns like user hesitation, retry storms, silent abandonments, and confused navigation — insights that traditional metrics dashboards completely miss.

**Stop asking "what happened?" and start understanding "why users behave this way."**

---

## Why Velum?

Traditional analytics tells you *what* happened:
- 500 button clicks
- 12% drop-off rate
- 3.2s average load time

Velum tells you *why* it matters:
- "Users are hesitating before checkout — 40% pause for 5+ seconds"
- "Retry storm detected in payment flow — users clicking submit 3+ times"
- "Silent abandonment pattern — users scroll but never interact"

---

## 🚀 5-Minute Quick Start

### Step 1: Install

```bash
git clone https://github.com/your-org/velum.git
cd velum
go mod tidy
```

### Step 2: Configure

```bash
cp example.config.yaml config.yaml
```

**Minimal config for local testing** (edit `config.yaml`):
```yaml
server:
  port: "8080"

security:
  enabled: false  # Disable auth for quick testing
```

### Step 3: Run

```bash
go run cmd/velum/main.go
```

### Step 4: Test

```bash
# Health check
curl http://localhost:8080/health

# Send test events
curl -X POST http://localhost:8080/api/v1/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {"id": "1", "event": "checkout_page_view", "ts": 1707500000000},
      {"id": "2", "event": "checkout_scroll", "ts": 1707500015000},
      {"id": "3", "event": "checkout_abandoned", "ts": 1707500045000}
    ]
  }'
```

🎉 **That's it!** You should see behavioral analysis detecting a silent abandonment pattern.

---

## 📋 Choose Your Setup

| Scenario | Security | AI Features | Data Mapping |
|----------|----------|-------------|--------------|
| **Local Development** | `enabled: false` | Optional | Optional |
| **Internal API** | `enabled: true` | Recommended | As needed |
| **Production** | `enabled: true` | Recommended | Recommended |

---

## 🔧 Configuration Guide

Velum is configured via `config.yaml`. Here's everything you need to know:

### Security (API Authentication)

```yaml
security:
  enabled: true
  api_key_hash: "your-sha256-hash"
```

**Generate your API key hash:**
```bash
# Choose a secret key (keep this safe!)
echo -n "my-secret-api-key" | shasum -a 256
# Output: e57de863c9fb355e971216e8e5ccb6ca3c63a51d4a017ac55f15f23e78f4e92c

# Use this hash in config.yaml, then include the original key in requests:
curl -H "X-Infra-Key: my-secret-api-key" http://localhost:8080/api/v1/analyze
```

**To disable auth** (local dev only):
```yaml
security:
  enabled: false
```

---

### Data Mapping (Transform Your Event Format)

If your events don't match Velum's expected format, use data mapping to transform them:

**Expected format:**
```json
{"id": "...", "event": "...", "ts": 1234567890, "user_id": "...", "session_id": "..."}
```

**Your format might be:**
```json
{
  "data": {"id": "evt-123"},
  "meta": {"time": 1707500000000},
  "payload": {"event": {"action": "button_click"}},
  "context": {"user": {"id": "usr-456"}}
}
```

**Solution — Enable data mapping:**
```yaml
data_mapping:
  enabled: true
  mapping:
    id:
      paths: ["data.id", "payload.id", "meta.id"]  # Try each path in order
      required: true                                # Error if all paths fail
    ts:
      paths: ["meta.time", "timestamp", "created_at"]
      format: "epoch_ms"  # Options: epoch_ms, epoch_s, iso8601
      required: true
    event:
      paths: ["payload.event.action", "event_name", "action"]
      required: true
    user_id:
      paths: ["context.user.id", "actor.user_id", "user_id"]
      required: false  # Optional - anonymous users allowed
    session_id:
      paths: ["context.session.id", "session_id"]
      required: false
```

**Key concepts:**
- **paths**: Fallback paths tried in order (dot notation for nested fields)
- **required**: `true` = error if missing, `false` = field omitted if missing
- **format**: Timestamp conversion (`epoch_ms`, `epoch_s`, `iso8601` → all convert to epoch_ms)

---

### AI Features (Optional but Powerful)

Velum has two AI-powered features. Both require a [Groq API key](https://console.groq.com/keys) (free tier available).

#### Vocab Agent — Auto-classify Unknown Words

When Velum encounters unknown tokens in your events (like `promo_banner_click`), it can automatically classify them:

```yaml
vocab_agent:
  enabled: true
  provider: "groq"
  api_key: "gsk_your_api_key_here"  # Or set VELUM_VOCAB_AGENT_API_KEY env var
  model: "llama-3.1-8b-instant"
```

#### AI Analyzer — Natural Language Summaries

Get human-readable explanations of detected patterns:

```yaml
ai_analyzer:
  enabled: true
  provider: "groq"
  api_key: "gsk_your_api_key_here"  # Or set VELUM_AI_API_KEY env var
  model: "llama-3.1-8b-instant"
```

---

### Server Settings

```yaml
server:
  port: "8080"              # Port to listen on
  host: "0.0.0.0"           # 0.0.0.0 = all interfaces, 127.0.0.1 = localhost only
  environment: "development" # development, staging, production
  
  # Timeouts (prevent hanging connections)
  read_timeout: "10s"
  write_timeout: "30s"
  idle_timeout: "60s"
  shutdown_timeout: "15s"   # Graceful shutdown wait time
```

### CORS (Cross-Origin Requests)

```yaml
cors:
  allowed_origins: ["*"]                    # Use specific domains in production
  allowed_methods: ["GET", "POST"]
  allowed_headers: ["Content-Type", "Authorization"]
```

### Baseline Detection

Track patterns over time and detect anomalies:

```yaml
baseline:
  window_days: 28              # Historical window for baseline
  min_days: 7                  # Minimum data before computing baseline
  computation_mode: "daily"    # "daily" (cached) or "always" (real-time)
  trend_threshold: 0.10        # 10% change = significant trend
  high_significance_threshold: 0.15
  std_deviation_multiplier: 2.0
```

### Resiliency (Rate Limits & Circuit Breaker)

Protect against overload and cascading AI failures:

```yaml
resiliency:
  rate_limit_requests: 100  # Max requests per second
  circuit_breaker:
    enabled: true
    failure_threshold: 5    # Open circuit after 5 AI failures
    reset_timeout: "30s"    # Retry AI after 30s
```

### Storage

```yaml
storage:
  retention_days: 90  # Auto-delete data older than this
```

---

## 📡 API Reference

### Authentication

When `security.enabled: true`, include your API key:
```bash
curl -H "X-Infra-Key: your-api-key" http://localhost:8080/api/v1/analyze
```

### Endpoints

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/health` | ❌ | Health check |
| POST | `/api/v1/analyze` | ✅ | Analyze events |

### Event Format

**Without data mapping:**
```json
{
  "events": [
    {
      "id": "evt-001",
      "event": "checkout_payment_click",
      "ts": 1707500000000,
      "user_id": "usr-123",
      "session_id": "sess-abc"
    }
  ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | ✅ | Unique event ID |
| `event` | string | ✅ | Event name |
| `ts` | number/string | ✅ | Timestamp (epoch ms or ISO 8601) |
| `user_id` | string | Recommended | User identifier |
| `session_id` | string | Optional | Session identifier |

**With data mapping enabled:** Send events in your own format — Velum transforms them automatically.

### Event Naming Conventions

Velum parses multiple formats:

| Format | Example | Parsed As |
|--------|---------|-----------|
| Snake case | `checkout_payment_success` | checkout, payment, success |
| Kebab case | `home-page-view` | home, page, view |
| Camel case | `userLoginFailed` | user, login, failed |
| Dot notation | `cart.item.added` | cart, item, added |

---

## 🔄 How It Works

Velum processes events through a layered pipeline:

```
Raw Events → [Data Mapper] → Event Adapter → Session Flow → Behavior Analyzer → Pattern Detector → Baseline
```

| Layer | What It Does |
|-------|--------------|
| **Data Mapper** | Transforms your event format to Velum's format (optional) |
| **Event Adapter** | Tokenizes event names → `{status, surface, flow}` |
| **Session Flow** | Groups events by user/session into journeys |
| **Behavior Analyzer** | Detects hesitation, retries, abandonment |
| **Pattern Detector** | Aggregates behaviors into patterns |
| **Baseline** | Compares against historical data |

### Detected Patterns

| Pattern | Description |
|---------|-------------|
| **Retry Storm** | >30% of users retry the same action |
| **Silent Abandonment** | Users view but never interact |
| **Confusion Loop** | Repeated navigation without progress |
| **Early Dropoff** | Users abandon immediately after starting |
| **Hesitation** | Long pauses before action |

---

## 🔤 Vocabulary System

Velum classifies event tokens into three categories:

| Category | Examples | What It Represents |
|----------|----------|-------------------|
| **Status** | click, success, failed, error, pending | What happened |
| **Surface** | button, modal, sidebar, checkout, cart | Where it happened |
| **Flow** | payment, auth, registration, search | User intent |

**Example:** `checkout_payment_failed` → `{surface: checkout, flow: payment, status: failed}`

### Handling Unknown Words

By default, unknown words go to "uncategorized". Enable `vocab_agent` for AI classification:

```yaml
vocab_agent:
  enabled: true
  api_key: "your-groq-api-key"
```

---

## 🌍 Environment Variables

Override config values with environment variables:

| Variable | Overrides | Description |
|----------|-----------|-------------|
| `VELUM_PORT` | `server.port` | Server port |
| `VELUM_ENV` | `server.environment` | Environment mode |
| `VELUM_AI_API_KEY` | `ai_analyzer.api_key` | AI analyzer API key |
| `VELUM_VOCAB_AGENT_API_KEY` | `vocab_agent.api_key` | Vocab agent API key |

```bash
# Example: Run with custom port and AI key
VELUM_PORT=3000 VELUM_AI_API_KEY=gsk_xxx go run cmd/velum/main.go
```

---

## 📁 Project Structure

```
velum/
├── cmd/velum/main.go           # Entry point
├── config.yaml                 # Your configuration
├── example.config.yaml         # Template with all options
├── data/                       # Auto-created databases
│   ├── velum.db               # Event storage
│   └── velum_vocab.db         # Vocabulary storage
└── internal/
    ├── api/                    # HTTP server
    ├── config/                 # Config loading
    ├── layers/                 # Processing pipeline
    │   ├── datamapper/        # Event format transformation
    │   ├── eventadapter/      # Event normalization
    │   ├── sessionflow/       # Session reconstruction
    │   ├── behavior/          # Behavior detection
    │   ├── pattern/           # Pattern aggregation
    │   ├── baseline/          # Historical comparison
    │   ├── ai/                # AI analysis
    │   └── vocabagent/        # AI vocabulary
    └── storage/               # SQLite persistence
```

---

## 🧪 Testing

```bash
# Run all tests
go test ./...

# Verbose output
go test ./... -v

# Specific package
go test ./internal/layers/datamapper/... -v

# With coverage
go test ./... -cover
```

---

## 🔧 Troubleshooting

### "Invalid API key" error

```bash
# Regenerate and verify your hash (no trailing newline!)
printf "your-key" | shasum -a 256
```

### "Address already in use"

```bash
# Kill process on port 8080
lsof -ti:8080 | xargs kill -9
```

### Events showing empty/unknown flow

1. **Check event naming** — Use descriptive names like `checkout_payment_click`
2. **Enable vocab_agent** — Auto-classify new words
3. **Inspect vocabulary database:**
   ```bash
   sqlite3 ./data/velum_vocab.db "SELECT * FROM vocabulary LIMIT 10"
   ```

### Data mapping not working

1. Ensure `data_mapping.enabled: true`
2. Check paths use dot notation: `context.user.id` not `context/user/id`
3. Verify required fields have valid fallback paths

### AI features not responding

1. Verify API key is valid at [console.groq.com](https://console.groq.com)
2. Check circuit breaker isn't open (wait 30s after failures)
3. Review logs for rate limiting errors

---

## 📄 License

MIT
