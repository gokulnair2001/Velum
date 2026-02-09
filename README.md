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

## Quick Start

### Prerequisites

| Requirement | Version | Notes |
|-------------|---------|-------|
| Go | 1.21+ | [Download Go](https://go.dev/dl/) |
| SQLite | 3.x | Usually pre-installed on macOS/Linux |

### Installation

```bash
# Clone the repository
git clone https://github.com/your-org/velum.git
cd velum

# Install dependencies
go mod tidy

# Copy the example configuration
cp example.config.yaml config.yaml
```

### Configuration

Edit `config.yaml` to customize Velum for your environment:

```yaml
# Server settings
server:
  port: "8080"
  host: "0.0.0.0"
  environment: "development"

# Security - generate your own API key hash
# Run: echo -n "your-secret-key" | shasum -a 256
security:
  enabled: true
  api_key_hash: "your-sha256-hash-here"

# AI-powered vocabulary expansion (optional)
vocab_agent:
  enabled: false
  api_key: ""  # Your Groq API key
  model: "llama-3.1-8b-instant"

# AI-powered analysis summaries (optional)
ai_analyzer:
  enabled: false
  api_key: ""  # Your Groq API key
```

### Generate API Key

Velum uses SHA-256 hashed API keys for authentication:

```bash
# Generate hash for your API key
echo -n "my-secret-api-key" | shasum -a 256
# Output: 3e84b4a0d4bc2a9dfc3d6e5f0b1c8a7d...

# Add this hash to config.yaml under security.api_key_hash
```

### Start the Server

```bash
# Run the server
go run cmd/velum/main.go

# Or build and run
go build -o velum cmd/velum/main.go
./velum
```

You should see:
```
Loaded configuration from config.yaml
Vocabulary already seeded (180 entries), skipping
🚀 Velum server starting on 0.0.0.0:8080
   Environment: development
```

### Verify Installation

```bash
# Health check
curl http://localhost:8080/health

# Expected response:
# {"status":"healthy","service":"velum"}
```

---

## Your First Analysis

Send analytics events to Velum and receive behavioral insights:

```bash
curl -X POST http://localhost:8080/api/v1/analyze \
  -H "Content-Type: application/json" \
  -H "X-Infra-Key: my-secret-api-key" \
  -d '{
    "events": [
      {"id": "1", "event": "checkout_view", "ts": "2026-02-04T10:00:00Z", "user_id": 101},
      {"id": "2", "event": "checkout_scroll", "ts": "2026-02-04T10:00:15Z", "user_id": 101},
      {"id": "3", "event": "checkout_scroll", "ts": "2026-02-04T10:00:30Z", "user_id": 101},
      {"id": "4", "event": "checkout_close", "ts": "2026-02-04T10:00:45Z", "user_id": 101}
    ]
  }'
```

**Response:**
```json
{
  "success": true,
  "message": "Behavioral analysis complete",
  "data": {
    "change_results": [
      {
        "flow": "checkout",
        "pattern_type": "silent_abandonment",
        "baseline_available": false
      }
    ]
  }
}
```

Velum detected a **silent abandonment** — the user viewed and scrolled but left without interacting.

---

## API Reference

### Authentication

All API requests require the `X-Infra-Key` header:

```bash
curl -H "X-Infra-Key: your-api-key" http://localhost:8080/api/v1/analyze
```

### Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check (no auth required) |
| POST | `/api/v1/analyze` | Analyze analytics events |

### Event Format

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | ✅ | Unique event identifier |
| `event` | string | ✅ | Event name (e.g., `button_click_success`) |
| `ts` | string | ✅ | ISO 8601 timestamp |
| `user_id` | string/number | Recommended | User identifier for flow grouping |
| `session_id` | string | Optional | Session identifier for higher confidence |

### Event Naming

Velum automatically parses multiple naming conventions:

| Format | Example |
|--------|---------|
| Snake case | `button_click_success` |
| Kebab case | `home-page-view` |
| Camel case | `userLoginSuccess` |
| Dot notation | `checkout.payment.failed` |

---

## Processing Pipeline

Velum processes events through a layered pipeline:

```
Raw Events → Event Adapter → Session Flow → Behavior Analyzer → Pattern Detector → Baseline Detector
```

### Layer 1: Event Adapter
Normalizes event names into structured vocabulary:
- `checkout_payment_click` → `{surface: [checkout], flow: [payment], status: [click]}`

### Layer 2: Session Flow Reconstructor
Groups events into user journeys:
- Groups by `user_id` and `session_id`
- Detects flow boundaries (start/end conditions)

### Layer 3: Behavior Analyzer
Identifies individual behaviors:
- Hesitation (long pauses between events)
- Retries (repeated actions)
- Abandonment (incomplete flows)

### Layer 4: Pattern Detector
Aggregates behaviors into patterns:
- **Retry Storm:** >30% of users retry the same action
- **Silent Abandonment:** Users view but never interact
- **Confusion Loop:** Repeated navigation without progress
- **Early Dropoff:** Users abandon immediately after starting

### Layer 5: Baseline Detector
Compares current patterns against historical baselines to detect anomalies.

---

## Vocabulary System

Velum classifies event tokens into three categories:

### Status (What happened)
Action states and results:
- `click`, `view`, `success`, `failed`, `error`, `pending`, `open`, `close`

### Surface (Where it happened)
UI components and locations:
- `button`, `modal`, `form`, `checkout`, `sidebar`, `header`

### Flow (User intent)
Business processes and goals:
- `payment`, `authentication`, `registration`, `sharing`, `search`

### Dynamic Vocabulary (Optional)

When `vocab_agent` is enabled, Velum uses AI to classify unknown words:

```yaml
vocab_agent:
  enabled: true
  api_key: "your-groq-api-key"
```

Unknown words are automatically classified and stored in SQLite for future use.

---

## Configuration Reference

### Server Settings

```yaml
server:
  port: "8080"           # Server port
  host: "0.0.0.0"        # Bind address
  environment: "development"  # development, staging, production
  read_timeout: "10s"    # Request read timeout
  write_timeout: "30s"   # Response write timeout
  shutdown_timeout: "15s" # Graceful shutdown timeout
```

### Storage

```yaml
storage:
  retention_days: 90     # How long to keep historical data
```

### Baseline Detection

```yaml
baseline:
  window_days: 28        # Days for baseline computation
  min_days: 7            # Minimum days before computing baseline
  trend_threshold: 0.10  # 10% change = trend
```

### Security

```yaml
security:
  enabled: true
  api_key_hash: "sha256-hash"  # echo -n "key" | shasum -a 256
```

### Environment Variables

Configuration can be overridden with environment variables:

| Variable | Description |
|----------|-------------|
| `VELUM_AI_API_KEY` | AI Analyzer API key |
| `VELUM_VOCAB_AGENT_API_KEY` | Vocab Agent API key |

---

## Project Structure

```
velum/
├── cmd/velum/
│   └── main.go                 # Application entry point
├── config.yaml                 # Configuration file
├── data/                       # SQLite databases (auto-created)
│   ├── velum.db               # Event storage
│   └── velum_vocab.db         # Vocabulary storage
├── internal/
│   ├── api/
│   │   ├── server.go          # HTTP server & routing
│   │   ├── handlers/          # Request handlers
│   │   └── middleware/        # Auth & security middleware
│   ├── config/                # Configuration management
│   ├── layers/
│   │   ├── layer.go           # Layer interface & pipeline
│   │   ├── eventadapter/      # Layer 1: Event normalization
│   │   ├── sessionflow/       # Layer 2: Flow reconstruction
│   │   ├── behavior/          # Layer 3: Behavior analysis
│   │   ├── pattern/           # Layer 4: Pattern detection
│   │   ├── baseline/          # Layer 5: Baseline comparison
│   │   ├── ai/                # AI-powered analysis
│   │   └── vocabagent/        # AI vocabulary expansion
│   └── storage/               # SQLite storage layer
└── go.mod
```

---

## Running Tests

```bash
# Run all tests
go test ./...

# Run with verbose output
go test ./... -v

# Run specific package tests
go test ./internal/layers/vocabagent/... -v
```

---

## Troubleshooting

### "Invalid API key" error

Verify your API key hash:
```bash
echo -n "your-key" | shasum -a 256
```
Ensure the hash in `config.yaml` matches exactly (no trailing newline).

### "Address already in use"

Kill the existing process:
```bash
lsof -ti:8080 | xargs kill -9
```

### Events showing "unknown" flow

Your event vocabulary isn't recognized. Options:
1. Enable `vocab_agent` to auto-classify new words
2. Add words to `internal/layers/eventadapter/vocabulary.go`
3. Check the SQLite vocab database: `sqlite3 ./data/velum_vocab.db "SELECT * FROM vocabulary"`

---

## License

MIT
