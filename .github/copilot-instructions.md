# Copilot Instructions for Velum

## Project Overview

Velum is a **zero-config behavioral pattern detection engine** for product analytics, written in Go. It accepts raw JSON events from any domain (e-commerce, ride-hailing, fintech, streaming) and returns detected anti-patterns — retry storms, confusion loops, silent abandonment, early dropoff, masked failures, bypass behavior, and funnel dropoff — with optional AI-powered natural language summaries.

The entire analysis is driven by a single `POST /api/v1/analyze` endpoint.

---

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.23 |
| HTTP Router | `github.com/go-chi/chi/v5` |
| CORS | `github.com/go-chi/cors` |
| Rate Limiting | `github.com/go-chi/httprate` |
| Database | PostgreSQL (`github.com/lib/pq`) |
| Config | YAML (`gopkg.in/yaml.v3`) + `VELUM_*` env vars |
| AI / LLM | Groq, OpenAI, or any OpenAI-compatible provider (HTTP) |
| Logging | `log/slog` (stdlib — structured, JSON in production) |
| Containerization | Docker (multi-stage) + Docker Compose |

---

## Repository Layout

```
cmd/velum/main.go              # Entrypoint: HTTP server lifecycle, signal handling
internal/
  api/
    server.go                  # Chi router, middleware registration
    handlers/handler.go        # HTTP handlers (Health, Analyze), pipeline wiring
    middleware/security.go     # X-Infra-Key SHA-256 validation
  config/config.go             # Config structs, YAML load, VELUM_* env overrides
  canonical/
    context.go                 # EventContext — the normalized event model
    registry.go                # Built-in dimension/property registry
  storage/
    storage.go                 # Storage interface (FetchBaselineSnapshots, StoreSnapshot, …)
    postgres.go                # PostgreSQL implementation
    memory.go                  # In-memory implementation (tests)
    factory.go                 # Instantiates backend from config
  layers/
    layer.go                   # Layer, ContextAwareLayer, Pipeline interfaces
    propertyagent/             # Layer 0 — Context Enricher (property classification)
    vocabagent/                # Layer 1 — Vocab Enricher (word classification)
    eventadapter/              # Layer 2 — Event Adapter (raw → canonical)
    sessionflow/               # Layer 3 — Session & Flow Reconstructor
    behavior/                  # Layer 4 — Behavior Analyzer
    pattern/                   # Layer 5 — Pattern Detector
    baseline/                  # Layer 6 — Baseline Comparator
    ai/                        # Layer 7 — AI Analyzer (NL summaries)
    aggregation/               # Report assembly (ReportBuilder, ExtractFromPipelineOutput)
    datamapper/                # Optional declarative field mapping
config.yaml                    # Default configuration (secrets via env vars)
test_cases/                    # JSON fixtures for integration/manual tests
```

---

## Core Architecture: The 8-Layer Pipeline

Every analysis request flows through a sequential `Pipeline` (defined in `internal/layers/layer.go`). Each layer implements the `Layer` interface:

```go
type Layer interface {
    Name() string
    Process(input interface{}) (interface{}, error)
}
```

Layers that need batch-level configuration (e.g., `AnalysisContext`) also implement `ContextAwareLayer` and receive metadata via `ProcessWithContext`. The pipeline's `ExecuteWithContext` method dispatches correctly at runtime.

| # | Layer | Package | AI? |
|---|---|---|---|
| 0 | Context Enricher | `propertyagent` | Yes (optional) |
| 1 | Vocab Enricher | `vocabagent` | Yes (optional) |
| 2 | Event Adapter | `eventadapter` | No |
| 3 | Session & Flow Reconstructor | `sessionflow` | No |
| 4 | Behavior Analyzer | `behavior` | No |
| 5 | Pattern Detector | `pattern` | No |
| 6 | Baseline Comparator | `baseline` | No |
| 7 | AI Analyzer | `ai` | Yes (optional) |

**When adding a new layer:**
1. Create a package under `internal/layers/<name>/`.
2. Implement `Layer` (and optionally `ContextAwareLayer`).
3. Register it in `handlers.NewHandler` in the correct position.
4. Add a `Name()` method that returns a stable kebab-case identifier.

---

## Key Domain Concepts

- **Event** — A raw JSON object with required fields `event` (string), `user_id` (string), `ts` (epoch ms), and any additional properties.
- **EventContext** — The normalized, enriched representation of an event (`internal/canonical/context.go`).
- **FlowInstance** — A group of events for a single `user_id + session_id` pair, split at 30-minute inactivity gaps.
- **BehaviorType** — A signal tagged on a flow: `retry`, `abandon`, `hesitation`, `exploration`, `success`, `failure`.
- **PatternType** — An aggregate pattern across users: `retry_storm`, `confusion_loop`, `silent_abandonment`, `early_dropoff`, `masked_failure`, `bypass_behavior`, `funnel_dropoff`.
- **PatternSnapshot** — The storage model persisted to PostgreSQL for baseline comparison.
- **AnalysisContext** — Batch-level metadata (project ID, funnel definitions, flow configs, scope) passed through the pipeline.

---

## Configuration

Config is resolved in this priority order (last wins):
1. Compiled defaults (`config.DefaultConfig()`)
2. `config.yaml` / `config.yml` / `/etc/velum/config.yaml`
3. `VELUM_*` environment variables

**Key env vars:**

| Variable | Purpose |
|---|---|
| `VELUM_DB_PASSWORD` | PostgreSQL password |
| `VELUM_AI_API_KEY` | AI Analyzer API key |
| `VELUM_VOCAB_AGENT_API_KEY` | Vocab Enricher API key |
| `VELUM_CONTEXT_AGENT_API_KEY` | Context Enricher API key |
| `VELUM_API_KEY_HASH` | SHA-256 hash of `X-Infra-Key` |
| `VELUM_ENV` | `development` / `staging` / `production` |
| `VELUM_PORT` | HTTP listen port (default `8080`) |

Secrets must **never** be committed to `config.yaml`. They belong in `.env` (gitignored) or injected at runtime.

---

## API

### Endpoints

| Method | Path | Auth Required | Description |
|---|---|---|---|
| `GET` | `/health` | No | Liveness + DB connectivity check |
| `POST` | `/api/v1/analyze` | `X-Infra-Key` (when security enabled) | Analyze events against stored baselines (read-only, no baseline writes) |
| `POST` | `/api/v1/baseline` | `X-Infra-Key` (when security enabled) | Ingest events and store baseline snapshots (no AI summary) |

### Important Headers

| Header | Required | Notes |
|---|---|---|
| `X-Project-ID` | Always | 1–64 alphanumeric chars, hyphens, underscores |
| `X-Infra-Key` | When `security.enabled: true` | Raw key (server hashes and compares) |

### Request Limits

- Max body size: **10 MB**
- Max events per request: **10,000**
- Required fields per event: `event`, `user_id`, `ts`

---

## Coding Conventions

### Go Style
- Follow standard Go idioms and `gofmt` formatting.
- Use `log/slog` for all logging. Never use `fmt.Println` or `log.Printf` in production paths.
- Use structured log attributes (key-value pairs) — never interpolate values into log messages.
- In `development` environment: `slog.LevelDebug` + text handler. In `production`: `slog.LevelInfo` + JSON handler.
- Prefer explicit error returns over panics. Only use `os.Exit(1)` in `main.go` for unrecoverable startup failures.

### Error Handling
- Return errors up the call stack; wrap with context using `fmt.Errorf("...: %w", err)`.
- HTTP handlers translate errors to JSON responses via `respondJSON()`. Never write raw error strings to `http.ResponseWriter`.
- Storage and AI calls use `context.Context` — always propagate the request context so in-flight calls are cancelled on client disconnect.

### Layer Development
- Each layer package must have a `types.go` (domain types), and the main logic in `<name>.go` or `analyzer.go`/`detector.go`.
- Layer `Process` methods must accept and return `interface{}` — use a type switch or assertion at the start.
- Layers must not import other layers (no circular dependencies). Shared types live in `internal/canonical/`.
- AI-optional layers register conditionally in `NewHandler` (check `Enabled && APIKey != ""`). Always log whether the layer is enabled/disabled at startup.

### Circuit Breaker Pattern
All AI/external HTTP calls use a circuit breaker (`CircuitBreakerConfig`): after `FailureThreshold` consecutive failures, calls short-circuit for `ResetTimeout`. This pattern exists in `ai/`, `vocabagent/`, and `propertyagent/`. Apply the same pattern to any new external dependency.

### Storage
- The `storage.Storage` interface is the only contract — never import `storage/postgres.go` directly from outside the `storage` package.
- Use `Cleanup()` for retention; call it at startup and every 24 hours (see `startStorageCleanup` in `handlers/handler.go`).
- Baseline snapshots use upsert semantics (`StoreSnapshot`) — all writes must be idempotent.

### Security
- API key auth: the raw key is never stored. Only its SHA-256 hash (`VELUM_API_KEY_HASH`) is stored. Comparison uses `crypto/subtle.ConstantTimeCompare` to prevent timing attacks.
- The `/health` endpoint is intentionally **outside** the security middleware group so k8s probes and load balancers can reach it unauthenticated.
- CORS: use explicit domains in production — never leave `allowed_origins: ["*"]` in a production config.

### Testing
- Test files live alongside the code they test (`*_test.go` in the same package).
- Use table-driven tests with `t.Run` subtests.
- The `storage/memory.go` backend is the preferred storage stub for unit tests — avoid spinning up a real DB in unit tests.
- Integration/manual test fixtures live in `test_cases/` as JSON files and are exercised by `test_cases/validate.sh`.
- Run tests with: `go test ./...`
- Check coverage with: `go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out`

### Docker / Deployment
- The Dockerfile is multi-stage: `golang:1.23-alpine` builder → `alpine:3.20` runtime.
- The container runs as a non-root user (`velum`).
- Secrets are injected via `env_file: .env` in docker-compose — never bake secrets into the image.
- The AWS RDS CA bundle is pre-installed in the image to support SSL connections to RDS out of the box.
- `docker compose up --build` requires a pre-existing `velum-network` (`docker network create velum-network`).

---

## What to Avoid

- **Do not** add new AI layers without a corresponding circuit breaker.
- **Do not** store secrets in `config.yaml` or commit `.env`.
- **Do not** bypass the `Storage` interface by using the Postgres driver directly outside the `storage` package.
- **Do not** add global mutable state — all config and dependencies flow through `Config` and `Handler`.
- **Do not** use `panic` in layer `Process` methods — return an error instead.
- **Do not** change the pipeline layer order without updating the layer index comments in `handlers/handler.go` and this document.
- **Do not** use `allowed_origins: ["*"]` in production CORS config.
- **Do not** log raw event data (may contain PII) at `Info` level or above — restrict to `Debug` and only in `development` environment.

---

## Adding a New Anti-Pattern

1. Add a new `PatternType` constant in `internal/layers/pattern/types.go`.
2. Add a weight entry in `PatternTypeWeight`.
3. Implement detection logic in `internal/layers/pattern/detector.go`.
4. Add a corresponding `BehaviorType` signal in `internal/layers/behavior/types.go` if a new behavioral signal is needed.
5. Wire the behavior signal → pattern in the behavior analyzer if required.
6. Add a test fixture in `test_cases/` with an appropriate prefix (`a` for anti-patterns).
7. Update `README.md` pattern table.

---

## Quick Reference: Running Locally

```bash
# Run directly
go run cmd/velum/main.go

# Run with Docker
docker network create velum-network
docker compose up --build

# Test
go test ./...

# Smoke test
curl -X POST http://localhost:8080/api/v1/analyze \
  -H "Content-Type: application/json" \
  -H "X-Project-ID: my-app" \
  -d @test_cases/a1_retry_storm.json
```
