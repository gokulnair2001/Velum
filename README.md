# Velum

**Behavioral Infrastructure Layer for Analytics Data**

Velum sits on top of your analytics event data and extracts behavioral insights. It transforms analytical/metric-focused data into behavioral/story-rich insights, shifting the focus from "why did this error occur?" to "why is there a gap in user behavior?"

---

## Getting Started

### Prerequisites
- Go 1.21 or higher

### Installation
```bash
go mod tidy
```

### Running the Server
```bash
go run cmd/velum/main.go
```

The server starts on port `8080` by default. Configure with environment variables:
- `VELUM_PORT` - Server port (default: `8080`)
- `VELUM_ENV` - Environment (default: `development`)

---

## API Endpoints

### Health Check
```
GET /health
```

**Response:**
```json
{
  "status": "healthy",
  "service": "velum"
}
```

### Analyze Events
```
POST /api/v1/analyze
```

Processes raw analytics events through Velum's behavioral layers.

---

## Raw Event Format

Velum expects events in JSON format with the following structure:

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `event` | `string` | The event name/identifier to be normalized |
| `id` | `string` | Unique event identifier |
| `ts` | `string` (ISO 8601) | Timestamp of the event |

> **Note:** Events missing `id` or `ts` fields will be skipped and excluded from processing.

### Recommended Fields

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | `string` | Session identifier |

> **💡 Tip:** Including `session_id` with each event enables stronger behavioral analysis. Session data allows Velum to group events into user journeys, detect patterns across interactions, and identify behavioral gaps within a single session context.

### Optional Fields

All other fields are passed through unchanged. Common fields include:

| Field | Type | Description |
|-------|------|-------------|
| `user_id` | `string` or `number` | User identifier |
| `properties` | `object` | Additional event properties |

### Event Naming Conventions

Velum's event adapter can parse events in multiple formats:

| Format | Example |
|--------|---------|
| Snake case | `button_click_success` |
| Kebab case | `home-page-view` |
| Camel case | `userLoginSuccess` |
| Dot notation | `checkout.payment.failed` |

---

## Request Example

```bash
curl -X POST http://localhost:8080/api/v1/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "event": "checkout_payment_click",
        "id": "evt_123",
        "user_id": 222,
        "ts": "2026-02-04T10:12:01Z",
        "session_id": "sess_abc",
        "properties": {
          "amount": 99.99,
          "currency": "USD"
        }
      },
      {
        "event": "login_modal_view",
        "id": "evt_124",
        "user_id": 222,
        "ts": "2026-02-04T10:12:05Z"
      }
    ]
  }'
```

---

## Response Example

```json
{
  "success": true,
  "message": "Behavioral analysis complete",
  "data": {
    "events_received": 2,
    "layers_processed": ["event_adapter"],
    "normalized_events": [
      {
        "event": "checkout_payment_click",
        "id": "evt_123",
        "user_id": 222,
        "ts": "2026-02-04T10:12:01Z",
        "session_id": "sess_abc",
        "properties": {
          "amount": 99.99,
          "currency": "USD"
        },
        "normalized": {
          "original": "checkout_payment_click",
          "tokens": ["checkout", "payment", "click"],
          "status": ["click"],
          "surface": ["checkout"],
          "flow": ["payment"]
        }
      },
      {
        "event": "login_modal_view",
        "id": "evt_124",
        "user_id": 222,
        "ts": "2026-02-04T10:12:05Z",
        "normalized": {
          "original": "login_modal_view",
          "tokens": ["login", "modal", "view"],
          "status": ["view"],
          "surface": ["modal", "view"],
          "flow": ["authentication"]
        }
      }
    ]
  }
}
```

---

## Normalized Event Structure

The `normalized` object contains:

| Field | Type | Description |
|-------|------|-------------|
| `original` | `string` | The original event string |
| `tokens` | `string[]` | Individual tokens extracted from the event |
| `status` | `string[]` | Event states (e.g., `click`, `view`, `success`, `failed`) |
| `surface` | `string[]` | UI locations (e.g., `modal`, `checkout`, `sidebar`) |
| `flow` | `string[]` | User intents (e.g., `authentication`, `payment`, `creation`) |
| `uncategorized` | `string[]` | Tokens not matching any vocabulary category |

---

## Flow Instance Structure

After Layer 2 processing, events are grouped into flow instances:

```json
{
  "flow_instance_id": "flow_1738749600000000000_1",
  "user_id": "222",
  "flow": "payment",
  "context_type": "explicit_session",
  "confidence": "high",
  "start_time": "2026-02-04T10:00:00Z",
  "end_time": "2026-02-04T10:02:00Z",
  "is_complete": true,
  "events": [
    {
      "timestamp": "2026-02-04T10:00:00Z",
      "surface": "checkout",
      "status": "view",
      "raw_event_name": "checkout_view",
      "session_id": "sess_abc"
    },
    {
      "timestamp": "2026-02-04T10:01:00Z",
      "surface": "button",
      "status": "click",
      "raw_event_name": "payment_click",
      "session_id": "sess_abc"
    },
    {
      "timestamp": "2026-02-04T10:02:00Z",
      "surface": "modal",
      "status": "success",
      "raw_event_name": "payment_success",
      "session_id": "sess_abc"
    }
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `flow_instance_id` | `string` | Unique identifier for this flow attempt |
| `user_id` | `string` | User who performed this flow |
| `flow` | `string` | Flow type (e.g., `payment`, `authentication`) |
| `context_type` | `string` | `explicit_session` if all events share session_id, otherwise `windowed` |
| `confidence` | `string` | `high`, `medium`, or `low` based on context completeness |
| `is_complete` | `boolean` | Whether the flow ended with success |
| `events` | `array` | Ordered list of events in this flow instance |

### Flow Reconstruction Rules

- **Grouping:** Events are grouped by `user_id` - events from different users are never mixed
- **Ordering:** Events are sorted strictly by timestamp within each user
- **Flow Start:** A new flow instance starts when a `view` or `start` status is seen
- **Flow End:** A flow instance ends on `success`, `dismiss`, or timeout (default: 30 minutes)
- **Multiple Instances:** The same user can have multiple instances of the same flow
- **No Inference:** The layer does NOT infer behavior (retries, hesitation, abandonment)

---

## Vocabulary Categories

### Status Keywords
Words denoting the result or state of an event:
- **Success states:** `success`, `complete`, `done`, `ok`
- **Failure states:** `failed`, `error`
- **Pending states:** `pending`, `loading`, `processing`
- **Interaction states:** `click`, `tap`, `press`, `view`, `dismiss`

### Surface Keywords
Words denoting UI components or locations:
- **Navigation:** `home`, `nav`, `sidebar`, `header`, `footer`
- **Commerce:** `checkout`, `cart`, `product`, `catalog`
- **Components:** `modal`, `button`, `form`, `input`, `dropdown`, `card`
- **Screens:** `dashboard`, `settings`, `profile`, `search`

### Flow Keywords
Words denoting user intent or actions:
- **Authentication:** `login`, `logout`, `signin`, `signup`
- **CRUD:** `create`, `add`, `update`, `edit`, `delete`, `remove`
- **Commerce:** `payment`, `purchase`, `order`, `refund`
- **Navigation:** `search`, `filter`, `scroll`, `navigate`

---

## Project Structure

```
Velum/
├── cmd/
│   └── velum/
│       └── main.go              # Application entry point
├── internal/
│   ├── api/
│   │   ├── server.go            # HTTP server & routing
│   │   └── handlers/
│   │       └── handler.go       # Request handlers
│   ├── config/
│   │   └── config.go            # Configuration management
│   └── layers/
│       ├── layer.go             # Layer interface & pipeline
│       ├── eventadapter/        # Layer 1: Event normalization
│       │   ├── adapter.go
│       │   ├── vocabulary.go
│       │   └── adapter_test.go
│       └── sessionflow/         # Layer 2: Session & Flow Reconstruction
│           ├── types.go
│           ├── reconstructor.go
│           └── reconstructor_test.go
├── go.mod
└── README.md
```

---

## License

MIT
