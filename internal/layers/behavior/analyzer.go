package behavior

import (
	"fmt"
	"sort"
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
	FlowIntent     FlowIntent              `json:"flow_intent"`     // Classified flow intent (browse/transact/unknown)
	EventCount     int                     `json:"event_count"`     // Number of events in flow (convenience for pattern detection)
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

// ProcessWithContext implements the ContextAwareLayer interface.
// When analysis context is provided, enables:
// - Flow intent classification (browse vs transact)
// - Funnel progression detection (cross-flow analysis)
// - Min-events threshold for dropoff prevention
func (a *Analyzer) ProcessWithContext(input interface{}, metadata interface{}) (interface{}, error) {
	var ctx *AnalysisContext
	if metadata != nil {
		ctx, _ = metadata.(*AnalysisContext)
	}
	if ctx == nil {
		return a.Process(input)
	}

	switch v := input.(type) {
	case []*sessionflow.FlowInstance:
		return a.analyzeFlowsWithContext(v, ctx), nil
	default:
		return a.Process(input)
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
		FlowIntent:     FlowIntentUnknown,
		EventCount:     len(flow.Events),
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
		} else if hasError {
			// Flow hit an error and never recovered — that's an abandon
			a.addBehavior(flow, BehaviorAbandon, lastErrorIndex, "flow ended with unrecovered error")
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

// analyzeFlowsWithContext processes flows with analysis context awareness.
// Adds flow intent classification and cross-flow funnel progression on top
// of standard per-flow behavior detection.
func (a *Analyzer) analyzeFlowsWithContext(flows []*sessionflow.FlowInstance, ctx *AnalysisContext) []*AnalyzedFlow {
	// Step 1: Analyze each flow individually (same as standard path)
	results := make([]*AnalyzedFlow, len(flows))
	for i, flow := range flows {
		results[i] = a.analyzeFlow(flow)
	}

	// Step 2: Resolve flow intents from config or inference
	a.resolveFlowIntents(results, ctx)

	// Step 3: Detect funnel progression (cross-flow analysis)
	// Only valid when we have funnel definitions and user session data
	if len(ctx.FunnelDefinitions) > 0 {
		a.detectFunnelProgression(results, ctx)
	}

	// Step 4: Recalculate outcomes after progression adjustments
	for _, r := range results {
		r.Outcome = a.getOutcome(r.Behaviors)
	}

	return results
}

// resolveFlowIntents classifies each flow's intent.
// Priority: explicit FlowConfig > inference from events.
func (a *Analyzer) resolveFlowIntents(flows []*AnalyzedFlow, ctx *AnalysisContext) {
	for _, f := range flows {
		// Check explicit config first
		if ctx.FlowConfigs != nil {
			if fc, ok := ctx.FlowConfigs[f.Flow]; ok {
				f.FlowIntent = fc.Intent
				continue
			}
		}
		// No explicit config — keep as unknown (conservative default).
		// Unknown flows use standard heuristics in pattern detection.
		f.FlowIntent = FlowIntentUnknown
	}
}

// detectFunnelProgression identifies flows where the user advanced to a
// subsequent funnel step. These flows should NOT be treated as dropoffs
// because the user continued their journey in the next flow.
func (a *Analyzer) detectFunnelProgression(flows []*AnalyzedFlow, ctx *AnalysisContext) {
	if ctx == nil || len(ctx.FunnelDefinitions) == 0 {
		return
	}

	// Build user → flows map
	userFlows := make(map[string][]*AnalyzedFlow)
	for _, f := range flows {
		userFlows[f.UserID] = append(userFlows[f.UserID], f)
	}

	// Sort each user's flows by start time
	for _, uf := range userFlows {
		sort.Slice(uf, func(i, j int) bool {
			return uf[i].StartTime.Before(uf[j].StartTime)
		})
	}

	// For each funnel definition, check user progression
	for funnelID, steps := range ctx.FunnelDefinitions {
		if len(steps) < 2 {
			continue
		}

		// Build step index: flow_name → step position
		stepIndex := make(map[string]int)
		for i, step := range steps {
			stepIndex[step] = i
		}

		// For each user, check if they progressed through funnel steps
		for _, uf := range userFlows {
			a.markFunnelProgression(uf, funnelID, steps, stepIndex)
		}
	}
}

// markFunnelProgression checks a single user's flows for advancement through
// funnel steps. If a user reached step N, all their flows at step < N are
// marked with BehaviorProgress instead of abandon/explore.
func (a *Analyzer) markFunnelProgression(userFlows []*AnalyzedFlow, funnelID string, steps []string, stepIndex map[string]int) {
	// Find the furthest funnel step this user reached
	maxStepReached := -1
	var flowsInFunnel []*AnalyzedFlow

	for _, f := range userFlows {
		if step, ok := stepIndex[f.Flow]; ok {
			flowsInFunnel = append(flowsInFunnel, f)
			if step > maxStepReached {
				maxStepReached = step
			}
		}
	}

	if len(flowsInFunnel) <= 1 || maxStepReached <= 0 {
		return // Need at least 2 flows in funnel with advancement
	}

	// For each flow that's NOT at the last reached step, mark as progressed
	for _, f := range flowsInFunnel {
		currentStep := stepIndex[f.Flow]
		if currentStep < maxStepReached {
			// User progressed beyond this step — not a dropoff
			a.removeBehavior(f, BehaviorAbandon)
			a.addBehavior(f, BehaviorProgress, len(f.Events)-1,
				fmt.Sprintf("user progressed to step %d (%s) in funnel %s",
					maxStepReached, steps[maxStepReached], funnelID))
		}
	}
}

// removeBehavior removes a specific behavior from an analyzed flow.
func (a *Analyzer) removeBehavior(flow *AnalyzedFlow, target BehaviorType) {
	for i, b := range flow.Behaviors {
		if b == target {
			flow.Behaviors = append(flow.Behaviors[:i], flow.Behaviors[i+1:]...)
			// Also remove from detail
			for j, d := range flow.BehaviorDetail {
				if d.Behavior == target {
					flow.BehaviorDetail = append(flow.BehaviorDetail[:j], flow.BehaviorDetail[j+1:]...)
					break
				}
			}
			return
		}
	}
}
