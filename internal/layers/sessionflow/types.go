package sessionflow

import (
	"time"

	"github.com/velum/internal/canonical"
)

// FlowInstance represents a single user intent attempt
type FlowInstance struct {
	FlowInstanceID string      `json:"flow_instance_id"`
	UserID         string      `json:"user_id"`
	Flow           string      `json:"flow"`
	ContextType    string      `json:"context_type"` // "explicit_session" | "windowed"
	Confidence     string      `json:"confidence"`   // "high" | "medium" | "low"
	Events         []FlowEvent `json:"events"`
	StartTime      time.Time   `json:"start_time"`
	EndTime        time.Time   `json:"end_time,omitempty"`
	IsComplete     bool        `json:"is_complete"`

	// Context is the merged canonical context across all events in this flow.
	// Dimensions use first-seen-wins, targets/conditions/measures use last-wins.
	Context *canonical.EventContext `json:"context,omitempty"`
}

// FlowEvent represents an event within a flow instance
type FlowEvent struct {
	Timestamp    time.Time `json:"timestamp"`
	Surface      string    `json:"surface"`
	Status       string    `json:"status"`
	RawEventName string    `json:"raw_event_name"`
	SessionID    string    `json:"session_id,omitempty"`

	// Context holds the canonical context for this individual event.
	Context *canonical.EventContext `json:"context,omitempty"`
}

// NormalizedEventInput represents the input from the event adapter layer
type NormalizedEventInput struct {
	ID         string                 `json:"id"`
	UserID     interface{}            `json:"user_id"`
	Timestamp  string                 `json:"ts"`
	SessionID  string                 `json:"session_id,omitempty"`
	Event      string                 `json:"event"`
	Normalized *NormalizedData        `json:"normalized"`
	Original   map[string]interface{} `json:"-"`

	// Context holds the canonical property classification from the event adapter.
	Context *canonical.EventContext `json:"-"`
}

// NormalizedData represents the normalized breakdown from Layer 1
type NormalizedData struct {
	Original      string   `json:"original"`
	Tokens        []string `json:"tokens"`
	Status        []string `json:"status"`
	Surface       []string `json:"surface"`
	Flow          []string `json:"flow"`
	Uncategorized []string `json:"uncategorized"`
}

// Config holds configuration for flow reconstruction
type Config struct {
	// FlowTimeout is the maximum duration between events before a flow is considered ended
	FlowTimeout time.Duration

	// EntryStatuses are statuses that can start a new flow instance
	EntryStatuses map[string]bool

	// ExitStatuses are statuses that end a flow instance
	ExitStatuses map[string]bool

	// SuccessStatuses indicate successful flow completion
	SuccessStatuses map[string]bool
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		FlowTimeout: 30 * time.Minute,
		EntryStatuses: map[string]bool{
			"view":  true,
			"start": true,
		},
		ExitStatuses: map[string]bool{
			"dismiss": true,
			"end":     true,
			"exit":    true,
		},
		SuccessStatuses: map[string]bool{
			"success": true,
		},
	}
}
