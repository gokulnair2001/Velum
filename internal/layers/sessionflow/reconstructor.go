package sessionflow

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/velum/internal/canonical"
	"github.com/velum/internal/layers/eventadapter"
)

// Reconstructor handles session and flow reconstruction
type Reconstructor struct {
	config      *Config
	idGenerator func() string
	idCounter   int
}

// New creates a new Reconstructor with default config
func New() *Reconstructor {
	r := &Reconstructor{
		config:    DefaultConfig(),
		idCounter: 0,
	}
	r.idGenerator = r.defaultIDGenerator
	return r
}

// NewWithConfig creates a Reconstructor with custom config
func NewWithConfig(config *Config) *Reconstructor {
	r := &Reconstructor{
		config:    config,
		idCounter: 0,
	}
	r.idGenerator = r.defaultIDGenerator
	return r
}

// Name returns the layer identifier
func (r *Reconstructor) Name() string {
	return "session_flow_reconstructor"
}

// Process implements the Layer interface
func (r *Reconstructor) Process(input interface{}) (interface{}, error) {
	// Handle input from event adapter layer
	switch v := input.(type) {
	case []*eventadapter.ProcessedEvent:
		return r.reconstructFromProcessedEvents(v)
	case []interface{}:
		return r.reconstructFromInterfaceSlice(v)
	default:
		return input, nil
	}
}

// reconstructFromProcessedEvents handles ProcessedEvent slice from Layer 1
func (r *Reconstructor) reconstructFromProcessedEvents(events []*eventadapter.ProcessedEvent) ([]*FlowInstance, error) {
	// Convert to internal format
	normalizedEvents := make([]*NormalizedEventInput, 0, len(events))

	for _, pe := range events {
		if pe == nil || pe.Normalized == nil {
			continue
		}

		input := &NormalizedEventInput{
			Event:    pe.Event,
			Original: pe.OriginalFields,
			Context:  pe.Context,
			Normalized: &NormalizedData{
				Original:      pe.Normalized.Original,
				Tokens:        pe.Normalized.Tokens,
				Status:        pe.Normalized.Status,
				Surface:       pe.Normalized.Surface,
				Flow:          pe.Normalized.Flow,
				Uncategorized: pe.Normalized.Uncategorized,
			},
		}

		// Extract fields from original
		if id, ok := pe.OriginalFields["id"]; ok {
			input.ID = fmt.Sprintf("%v", id)
		}
		if userID, ok := pe.OriginalFields["user_id"]; ok {
			input.UserID = userID
		}
		if ts, ok := pe.OriginalFields["ts"]; ok {
			input.Timestamp = fmt.Sprintf("%v", ts)
		}
		if sessionID, ok := pe.OriginalFields["session_id"]; ok {
			input.SessionID = fmt.Sprintf("%v", sessionID)
		}

		normalizedEvents = append(normalizedEvents, input)
	}

	return r.reconstruct(normalizedEvents)
}

// reconstructFromInterfaceSlice handles generic interface slice
func (r *Reconstructor) reconstructFromInterfaceSlice(events []interface{}) ([]*FlowInstance, error) {
	normalizedEvents := make([]*NormalizedEventInput, 0, len(events))

	for _, e := range events {
		if m, ok := e.(map[string]interface{}); ok {
			input := r.parseEventMap(m)
			if input != nil {
				normalizedEvents = append(normalizedEvents, input)
			}
		}
	}

	return r.reconstruct(normalizedEvents)
}

// parseEventMap converts a map to NormalizedEventInput
func (r *Reconstructor) parseEventMap(m map[string]interface{}) *NormalizedEventInput {
	input := &NormalizedEventInput{
		Original: m,
	}

	if id, ok := m["id"]; ok {
		input.ID = fmt.Sprintf("%v", id)
	}
	if userID, ok := m["user_id"]; ok {
		input.UserID = userID
	}
	if ts, ok := m["ts"]; ok {
		input.Timestamp = fmt.Sprintf("%v", ts)
	}
	if sessionID, ok := m["session_id"]; ok {
		input.SessionID = fmt.Sprintf("%v", sessionID)
	}
	if event, ok := m["event"]; ok {
		input.Event = fmt.Sprintf("%v", event)
	}

	// Parse normalized data
	if norm, ok := m["normalized"].(map[string]interface{}); ok {
		input.Normalized = &NormalizedData{}
		if orig, ok := norm["original"].(string); ok {
			input.Normalized.Original = orig
		}
		if tokens, ok := norm["tokens"].([]interface{}); ok {
			input.Normalized.Tokens = toStringSlice(tokens)
		}
		if status, ok := norm["status"].([]interface{}); ok {
			input.Normalized.Status = toStringSlice(status)
		}
		if surface, ok := norm["surface"].([]interface{}); ok {
			input.Normalized.Surface = toStringSlice(surface)
		}
		if flow, ok := norm["flow"].([]interface{}); ok {
			input.Normalized.Flow = toStringSlice(flow)
		}
	}

	return input
}

// reconstruct performs the core flow reconstruction logic
func (r *Reconstructor) reconstruct(events []*NormalizedEventInput) ([]*FlowInstance, error) {
	// Step 1: Group events by user_id
	userEvents := r.groupByUser(events)

	// Step 2: Process each user's events
	var allFlowInstances []*FlowInstance

	for userID, userEvts := range userEvents {
		// Sort events temporally
		r.sortByTimestamp(userEvts)

		// Reconstruct flow instances for this user
		flowInstances := r.reconstructUserFlows(userID, userEvts)
		allFlowInstances = append(allFlowInstances, flowInstances...)
	}

	return allFlowInstances, nil
}

// groupByUser groups events by user_id
func (r *Reconstructor) groupByUser(events []*NormalizedEventInput) map[string][]*NormalizedEventInput {
	grouped := make(map[string][]*NormalizedEventInput)

	for _, event := range events {
		userID := r.extractUserID(event)
		grouped[userID] = append(grouped[userID], event)
	}

	return grouped
}

// extractUserID extracts user_id as string
func (r *Reconstructor) extractUserID(event *NormalizedEventInput) string {
	if event.UserID == nil {
		return "anonymous"
	}

	switch v := event.UserID.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', 0, 64)
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// sortByTimestamp sorts events by timestamp
func (r *Reconstructor) sortByTimestamp(events []*NormalizedEventInput) {
	sort.Slice(events, func(i, j int) bool {
		ti := r.parseTimestamp(events[i].Timestamp)
		tj := r.parseTimestamp(events[j].Timestamp)
		return ti.Before(tj)
	})
}

// parseTimestamp parses various timestamp formats including epoch milliseconds,
// epoch seconds, and ISO 8601 strings.
func (r *Reconstructor) parseTimestamp(ts string) time.Time {
	// Try parsing as a numeric epoch timestamp first.
	// JSON numbers decoded into map[string]interface{} become float64,
	// which fmt.Sprintf("%v", ...) renders as scientific notation (e.g., "1.7075e+12").
	// strconv.ParseFloat handles both "1707500000000" and "1.7075e+12".
	if epochVal, err := strconv.ParseFloat(ts, 64); err == nil && epochVal > 0 {
		epochInt := int64(epochVal)
		if epochInt > 1e15 {
			// Microseconds (e.g., 1707500000000000)
			return time.Unix(0, epochInt*int64(time.Microsecond)).UTC()
		} else if epochInt > 1e12 {
			// Milliseconds (e.g., 1707500000000)
			return time.Unix(0, epochInt*int64(time.Millisecond)).UTC()
		} else if epochInt > 1e9 {
			// Seconds (e.g., 1707500000)
			return time.Unix(epochInt, 0).UTC()
		}
	}

	// Try common ISO 8601 string formats
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t
		}
	}

	return time.Time{}
}

// reconstructUserFlows reconstructs flow instances for a single user
func (r *Reconstructor) reconstructUserFlows(userID string, events []*NormalizedEventInput) []*FlowInstance {
	// Track active flow instances by flow name
	activeFlows := make(map[string]*FlowInstance)
	var completedFlows []*FlowInstance

	for _, event := range events {
		if event.Normalized == nil {
			continue
		}

		timestamp := r.parseTimestamp(event.Timestamp)
		flowNames := event.Normalized.Flow
		statuses := event.Normalized.Status
		surfaces := event.Normalized.Surface

		// If no flow detected, use "unknown"
		if len(flowNames) == 0 {
			flowNames = []string{"unknown"}
		}

		// Process each flow the event belongs to
		for _, flowName := range flowNames {
			// Check for timeout on active flow
			if active, exists := activeFlows[flowName]; exists {
				if !active.EndTime.IsZero() && timestamp.Sub(active.EndTime) > r.config.FlowTimeout {
					// Flow timed out, complete it
					active.IsComplete = false
					active.ContextType = r.determineContextType(active)
					active.Confidence = r.calculateConfidence(active)
					completedFlows = append(completedFlows, active)
					delete(activeFlows, flowName)
				}
			}

			// Determine if this is an entry, exit, or continuation
			isEntry := r.isEntryStatus(statuses)
			isExit := r.isExitStatus(statuses)
			isSuccess := r.isSuccessStatus(statuses)

			// Get or create flow instance
			active, exists := activeFlows[flowName]

			if !exists || isEntry {
				// Start new flow instance
				if exists && active != nil {
					// Complete the previous one first
					active.IsComplete = false
					active.ContextType = r.determineContextType(active)
					active.Confidence = r.calculateConfidence(active)
					completedFlows = append(completedFlows, active)
				}

				active = &FlowInstance{
					FlowInstanceID: r.idGenerator(),
					UserID:         userID,
					Flow:           flowName,
					Events:         make([]FlowEvent, 0),
					StartTime:      timestamp,
					IsComplete:     false,
				}
				activeFlows[flowName] = active
			}

			// Add event to flow instance
			flowEvent := FlowEvent{
				Timestamp:    timestamp,
				Surface:      r.firstOrEmpty(surfaces),
				Status:       r.firstOrEmpty(statuses),
				RawEventName: event.Event,
				SessionID:    event.SessionID,
				Context:      event.Context,
			}
			active.Events = append(active.Events, flowEvent)
			active.EndTime = timestamp

			// Merge event context into flow-level context
			if event.Context != nil {
				if active.Context == nil {
					active.Context = canonical.NewEventContext()
				}
				active.Context.Merge(event.Context)
			}

			// Check for flow completion
			if isSuccess || isExit {
				active.IsComplete = isSuccess
				active.ContextType = r.determineContextType(active)
				active.Confidence = r.calculateConfidence(active)
				completedFlows = append(completedFlows, active)
				delete(activeFlows, flowName)
			}
		}
	}

	// Close any remaining active flows
	for _, active := range activeFlows {
		active.ContextType = r.determineContextType(active)
		active.Confidence = r.calculateConfidence(active)
		completedFlows = append(completedFlows, active)
	}

	return completedFlows
}

// isEntryStatus checks if any status indicates flow entry
func (r *Reconstructor) isEntryStatus(statuses []string) bool {
	for _, s := range statuses {
		if r.config.EntryStatuses[s] {
			return true
		}
	}
	return false
}

// isExitStatus checks if any status indicates flow exit
func (r *Reconstructor) isExitStatus(statuses []string) bool {
	for _, s := range statuses {
		if r.config.ExitStatuses[s] {
			return true
		}
	}
	return false
}

// isSuccessStatus checks if any status indicates success
func (r *Reconstructor) isSuccessStatus(statuses []string) bool {
	for _, s := range statuses {
		if r.config.SuccessStatuses[s] {
			return true
		}
	}
	return false
}

// determineContextType determines if flow is explicit_session or windowed
func (r *Reconstructor) determineContextType(flow *FlowInstance) string {
	if len(flow.Events) == 0 {
		return "windowed"
	}

	// Check if all events have valid session_id
	for _, event := range flow.Events {
		if event.SessionID == "" {
			return "windowed"
		}
	}

	// Check if all session_ids are the same
	firstSessionID := flow.Events[0].SessionID
	for _, event := range flow.Events {
		if event.SessionID != firstSessionID {
			return "windowed"
		}
	}

	return "explicit_session"
}

// calculateConfidence calculates confidence level based on context type
func (r *Reconstructor) calculateConfidence(flow *FlowInstance) string {
	if flow.ContextType == "explicit_session" {
		return "medium"
	}
	return "low"
}

// firstOrEmpty returns the first element or empty string
func (r *Reconstructor) firstOrEmpty(slice []string) string {
	if len(slice) > 0 {
		return slice[0]
	}
	return ""
}

// defaultIDGenerator generates unique flow instance IDs
func (r *Reconstructor) defaultIDGenerator() string {
	r.idCounter++
	return fmt.Sprintf("flow_%d_%d", time.Now().UnixNano(), r.idCounter)
}

// Helper function to convert []interface{} to []string
func toStringSlice(input []interface{}) []string {
	result := make([]string, 0, len(input))
	for _, v := range input {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
