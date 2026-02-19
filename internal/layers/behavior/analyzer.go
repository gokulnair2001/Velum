package behavior

import (
	"time"

	"github.com/velum/internal/canonical"
	"github.com/velum/internal/layers/sessionflow"
)

// Analyzer detects behavioral patterns in flow instances
type Analyzer struct {
	config *Config
}

// New creates a new Behavior Analyzer with default config
func New() *Analyzer {
	return &Analyzer{
		config: DefaultConfig(),
	}
}

// NewWithConfig creates a Behavior Analyzer with custom config
func NewWithConfig(config *Config) *Analyzer {
	return &Analyzer{
		config: config,
	}
}

// Name returns the layer identifier
func (a *Analyzer) Name() string {
	return "behavior_analyzer"
}

// AnalyzedFlow represents a flow instance with behavior analysis
type AnalyzedFlow struct {
	FlowInstanceID string                  `json:"flow_instance_id"`
	UserID         string                  `json:"user_id"`
	Flow           string                  `json:"flow"`
	ContextType    string                  `json:"context_type"`
	Confidence     string                  `json:"confidence"`
	Behaviors      []BehaviorType          `json:"behaviors"`       // Ordered list of detected behaviors
	BehaviorDetail []BehaviorEvent         `json:"behavior_detail"` // Detailed behavior events
	Outcome        BehaviorType            `json:"outcome"`         // Highest priority behavior (final outcome)
	Events         []sessionflow.FlowEvent `json:"events"`
	StartTime      time.Time               `json:"start_time"`
	EndTime        time.Time               `json:"end_time,omitempty"`
	IsComplete     bool                    `json:"is_complete"`

	// Context is the merged canonical context from the flow instance.
	Context *canonical.EventContext `json:"context,omitempty"`
}

// Process implements the Layer interface
func (a *Analyzer) Process(input interface{}) (interface{}, error) {
	switch v := input.(type) {
	case []*sessionflow.FlowInstance:
		return a.analyzeFlows(v), nil
	default:
		return input, nil
	}
}

// analyzeFlows processes multiple flow instances
func (a *Analyzer) analyzeFlows(flows []*sessionflow.FlowInstance) []*AnalyzedFlow {
	results := make([]*AnalyzedFlow, len(flows))
	for i, flow := range flows {
		results[i] = a.analyzeFlow(flow)
	}
	return results
}

// analyzeFlow detects behaviors in a single flow instance
func (a *Analyzer) analyzeFlow(flow *sessionflow.FlowInstance) *AnalyzedFlow {
	analyzed := &AnalyzedFlow{
		FlowInstanceID: flow.FlowInstanceID,
		UserID:         flow.UserID,
		Flow:           flow.Flow,
		ContextType:    flow.ContextType,
		Confidence:     flow.Confidence,
		Events:         flow.Events,
		StartTime:      flow.StartTime,
		EndTime:        flow.EndTime,
		IsComplete:     flow.IsComplete,
		Context:        flow.Context,
		Behaviors:      make([]BehaviorType, 0),
		BehaviorDetail: make([]BehaviorEvent, 0),
	}

	// Detect behaviors in order of occurrence
	a.detectBehaviors(analyzed)

	// Determine outcome (highest priority behavior)
	analyzed.Outcome = a.getOutcome(analyzed.Behaviors)

	return analyzed
}

// detectBehaviors analyzes the event sequence and detects behaviors
func (a *Analyzer) detectBehaviors(flow *AnalyzedFlow) {
	if len(flow.Events) == 0 {
		return
	}

	hasAction := false
	hasSuccess := false
	hasError := false
	hasExit := false
	actionCount := 0
	lastActionIndex := -1
	lastErrorIndex := -1

	// First pass: gather statistics
	for i, event := range flow.Events {
		status := event.Status

		if a.config.ActionStatuses[status] {
			hasAction = true
			actionCount++
			lastActionIndex = i
		}
		if a.config.SuccessStatuses[status] {
			hasSuccess = true
		}
		if a.config.ErrorStatuses[status] {
			hasError = true
			lastErrorIndex = i
		}
		if a.config.ExitStatuses[status] {
			hasExit = true
		}
	}

	// Apply behavior rules in sequence

	// Rule D — Succeed (check first due to highest priority)
	if hasSuccess {
		for i, event := range flow.Events {
			if a.config.SuccessStatuses[event.Status] {
				a.addBehavior(flow, BehaviorSucceed, i, "success event detected")
				break // Only record first success
			}
		}
	}

	// Rule C — Retry: error followed by another click
	if hasError && actionCount > 1 {
		for i := lastErrorIndex + 1; i < len(flow.Events); i++ {
			if a.config.ActionStatuses[flow.Events[i].Status] {
				a.addBehavior(flow, BehaviorRetry, i, "action after error")
				break
			}
		}
	}

	// Rule B — Attempt: first action in the flow
	if hasAction {
		for i, event := range flow.Events {
			if a.config.ActionStatuses[event.Status] {
				a.addBehavior(flow, BehaviorAttempt, i, "first action in flow")
				break
			}
		}
	}

	// Rule F — Hesitate: entry → action → entry again
	if a.config.EnableHesitation {
		a.detectHesitation(flow)
	}

	// Rule E — Abandon: flow ends without success, last signal is exit or timeout
	if !hasSuccess && !flow.IsComplete {
		if hasExit {
			a.addBehavior(flow, BehaviorAbandon, len(flow.Events)-1, "explicit exit without success")
		} else if lastActionIndex >= 0 && lastActionIndex < len(flow.Events)-1 {
			// Had action but flow didn't complete - possible abandon
			a.addBehavior(flow, BehaviorAbandon, len(flow.Events)-1, "flow ended without completion")
		}
	}

	// Rule A — Explore: only view/open events, no actions
	if !hasAction && !hasSuccess {
		onlyViews := true
		for _, event := range flow.Events {
			if !a.config.EntryStatuses[event.Status] && event.Status != "" {
				onlyViews = false
				break
			}
		}
		if onlyViews {
			a.addBehavior(flow, BehaviorExplore, 0, "only entry/view events, no actions")
		}
	}
}

// detectHesitation checks for entry → action → entry pattern
func (a *Analyzer) detectHesitation(flow *AnalyzedFlow) {
	// Need at least 3 events for hesitation pattern
	if len(flow.Events) < 3 {
		return
	}

	// Look for pattern: entry → action → entry
	for i := 0; i < len(flow.Events)-2; i++ {
		isEntry1 := a.config.EntryStatuses[flow.Events[i].Status]
		isAction := a.config.ActionStatuses[flow.Events[i+1].Status]
		isEntry2 := a.config.EntryStatuses[flow.Events[i+2].Status]

		if isEntry1 && isAction && isEntry2 {
			a.addBehavior(flow, BehaviorHesitate, i+2, "entry after action indicates hesitation")
			return // Only detect first hesitation
		}
	}
}

// addBehavior adds a behavior to the flow if not already present
func (a *Analyzer) addBehavior(flow *AnalyzedFlow, behavior BehaviorType, eventIndex int, reason string) {
	// Check if behavior already exists
	for _, b := range flow.Behaviors {
		if b == behavior {
			return
		}
	}

	flow.Behaviors = append(flow.Behaviors, behavior)
	flow.BehaviorDetail = append(flow.BehaviorDetail, BehaviorEvent{
		Behavior:   behavior,
		EventIndex: eventIndex,
		Reason:     reason,
	})
}

// getOutcome returns the highest priority behavior as the flow outcome
func (a *Analyzer) getOutcome(behaviors []BehaviorType) BehaviorType {
	if len(behaviors) == 0 {
		return BehaviorExplore // Default
	}

	primary := behaviors[0]
	highestPriority := BehaviorPriority[primary]

	for _, b := range behaviors[1:] {
		if BehaviorPriority[b] > highestPriority {
			primary = b
			highestPriority = BehaviorPriority[b]
		}
	}

	return primary
}

// GetConfig returns the current configuration
func (a *Analyzer) GetConfig() *Config {
	return a.config
}
