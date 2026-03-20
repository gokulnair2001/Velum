package behavior

import "context"

// BehaviorType represents the type of user behavior
type BehaviorType string

const (
	BehaviorExplore  BehaviorType = "explore"
	BehaviorAttempt  BehaviorType = "attempt"
	BehaviorRetry    BehaviorType = "retry"
	BehaviorSucceed  BehaviorType = "succeed"
	BehaviorAbandon  BehaviorType = "abandon"
	BehaviorHesitate BehaviorType = "hesitate"
	BehaviorBypass   BehaviorType = "bypass"
	BehaviorProgress BehaviorType = "progress" // User advanced to next flow in funnel
)

// BehaviorPriority defines the priority order (higher = more important)
var BehaviorPriority = map[BehaviorType]int{
	BehaviorSucceed:  6, // highest
	BehaviorRetry:    5,
	BehaviorProgress: 4, // user advanced to next funnel step
	BehaviorAttempt:  3,
	BehaviorAbandon:  2,
	BehaviorHesitate: 1,
	BehaviorExplore:  0, // lowest
	BehaviorBypass:   0,
}

// BehaviorEvent represents a behavior occurrence within a flow
type BehaviorEvent struct {
	Behavior   BehaviorType `json:"behavior"`
	EventIndex int          `json:"event_index"` // Index of the event that triggered this behavior
	Reason     string       `json:"reason"`      // Explanation for the behavior
}

// FlowIntent classifies what a flow's expected outcome is.
// This determines whether a view-only flow is "complete" or a "dropoff".
type FlowIntent string

const (
	// FlowIntentBrowse — the expected outcome IS viewing (restaurant list, menu, status check).
	// A single view event is a completed flow, not a dropoff.
	FlowIntentBrowse FlowIntent = "browse"

	// FlowIntentTransact — the expected outcome is an action (checkout, payment, registration).
	// View-only events here ARE potential dropoffs.
	FlowIntentTransact FlowIntent = "transact"

	// FlowIntentUnknown — intent not specified; system uses default heuristics.
	FlowIntentUnknown FlowIntent = "unknown"
)

// FunnelPosition describes where a flow sits in a multi-flow journey.
type FunnelPosition struct {
	FunnelID string   `json:"funnel_id,omitempty"` // Groups related flows (e.g., "purchase_funnel")
	Step     int      `json:"step"`                // Position in funnel (0-indexed)
	NextFlow []string `json:"next_flow,omitempty"` // Expected subsequent flow names
	PrevFlow []string `json:"prev_flow,omitempty"` // Expected preceding flow names
}

// FlowConfig defines metadata about a specific flow that guides pattern detection.
// This is what makes detection accurate across different batch compositions.
type FlowConfig struct {
	Name   string     `json:"name"`
	Intent FlowIntent `json:"intent"`

	// MinEventsForDropoff: minimum events before a flow can be considered "dropped off".
	// Prevents single-view events from being flagged. 0 means use global default.
	MinEventsForDropoff int `json:"min_events_for_dropoff"`

	// Funnel links this flow to a larger journey.
	Funnel *FunnelPosition `json:"funnel,omitempty"`

	// TerminalFlow: if true, this flow is a valid endpoint (no expected next step).
	TerminalFlow bool `json:"terminal_flow"`
}

// AnalysisScope tells the engine what kind of batch it's processing.
// The same events produce different insights depending on scope.
type AnalysisScope string

const (
	// ScopeUserSession — batch represents one user's session/journey.
	// Enables: journey progression, funnel completion, session-level patterns.
	ScopeUserSession AnalysisScope = "user_session"

	// ScopeUserHistory — batch represents one user across multiple sessions.
	// Enables: behavioral trends, habit detection, churn signals.
	ScopeUserHistory AnalysisScope = "user_history"

	// ScopeFlowCohort — batch represents many users in the same flow(s).
	// Enables: funnel drop rates, conversion analysis, aggregate patterns.
	ScopeFlowCohort AnalysisScope = "flow_cohort"

	// ScopeRawBatch — batch composition is unknown or mixed.
	// Engine operates conservatively — only per-flow patterns, no cross-flow inference.
	ScopeRawBatch AnalysisScope = "raw_batch"
)

// AnalysisContext is provided with every batch to guide pattern detection.
// It controls what level of analysis is valid and provides per-flow configuration.
type AnalysisContext struct {
	// Ctx carries the HTTP request context through the pipeline so that
	// long-running operations (LLM calls, DB queries) are cancelled when
	// the client disconnects or the server shuts down.
	Ctx context.Context `json:"-"`

	// ProjectID isolates all data (baselines, patterns) by project.
	// Extracted from the X-Project-ID HTTP header on every request.
	ProjectID string `json:"project_id"`

	// Scope tells the engine what level of analysis is valid for this batch.
	Scope AnalysisScope `json:"scope"`

	// EntityID is the primary entity this batch concerns (user_id, org_id, etc).
	// Empty for raw/mixed batches.
	EntityID string `json:"entity_id,omitempty"`

	// EntityType describes what EntityID represents (e.g., "user", "org", "device").
	EntityType string `json:"entity_type,omitempty"`

	// FlowConfigs provides per-flow metadata. Key is flow name.
	// If a flow isn't listed, defaults are used.
	FlowConfigs map[string]*FlowConfig `json:"flow_configs,omitempty"`

	// FunnelDefinitions defines multi-flow journeys.
	// Key is funnel ID, value is ordered list of flow names.
	FunnelDefinitions map[string][]string `json:"funnel_definitions,omitempty"`

	// PartialSession: if true, the engine knows it's seeing an incomplete picture.
	// Suppresses patterns that require full session visibility.
	PartialSession bool `json:"partial_session"`

	// UpdateBaseline controls whether the baseline snapshot is stored after analysis.
	// Default is true (header absent = store). Set to false via X-Update-Baseline: false
	// for ad-hoc analysis without polluting baseline history.
	UpdateBaseline bool `json:"-"`
}

// DefaultAnalysisContext returns a conservative default context (raw batch, no assumptions).
func DefaultAnalysisContext() *AnalysisContext {
	return &AnalysisContext{
		Ctx:            context.Background(),
		Scope:          ScopeRawBatch,
		PartialSession: true, // assume incomplete by default — safer
		UpdateBaseline: true, // always store baseline by default
	}
}

// RequestContext returns the request-scoped context, falling back to
// context.Background() if none was set. Layers should use this instead of
// creating their own context.Background().
func (ac *AnalysisContext) RequestContext() context.Context {
	if ac != nil && ac.Ctx != nil {
		return ac.Ctx
	}
	return context.Background()
}

// GetFlowConfig returns the FlowConfig for a flow, or nil if not configured.
func (ac *AnalysisContext) GetFlowConfig(flowName string) *FlowConfig {
	if ac == nil || ac.FlowConfigs == nil {
		return nil
	}
	return ac.FlowConfigs[flowName]
}

// GetMinEventsForDropoff returns the effective minimum events threshold for a flow.
// Checks flow-specific config first, then falls back to the global default.
func (ac *AnalysisContext) GetMinEventsForDropoff(flowName string, globalDefault int) int {
	if fc := ac.GetFlowConfig(flowName); fc != nil && fc.MinEventsForDropoff > 0 {
		return fc.MinEventsForDropoff
	}
	if globalDefault > 0 {
		return globalDefault
	}
	return 2 // absolute fallback
}

// Config holds configuration for behavior detection
type Config struct {
	// ActionStatuses are statuses that indicate user action (not just viewing)
	ActionStatuses map[string]bool

	// EntryStatuses are statuses that indicate flow entry
	EntryStatuses map[string]bool

	// ErrorStatuses are statuses that indicate errors
	ErrorStatuses map[string]bool

	// SuccessStatuses are statuses that indicate success
	SuccessStatuses map[string]bool

	// ExitStatuses are statuses that indicate explicit exit
	ExitStatuses map[string]bool

	// EnableHesitation controls whether to detect hesitation (conservative)
	EnableHesitation bool

	// DefaultMinEventsForDropoff is the minimum number of events a flow must have
	// before it can be considered a dropoff. Prevents single-view events from
	// being flagged as early_dropoff. Default: 2.
	DefaultMinEventsForDropoff int
}

// DefaultConfig returns the default behavior detection configuration
func DefaultConfig() *Config {
	return &Config{
		ActionStatuses: map[string]bool{
			"click":    true,
			"submit":   true,
			"confirm":  true,
			"select":   true,
			"attempt":  true,
			"apply":    true,
			"request":  true,
			"scroll":   true,
			"swipe":    true,
			"add":      true,
			"create":   true,
			"remove":   true,
			"update":   true,
			"save":     true,
			"upload":   true,
			"download": true,
			"share":    true,
			"export":   true,
		},
		EntryStatuses: map[string]bool{
			"view":  true,
			"start": true,
			"open":  true,
		},
		ErrorStatuses: map[string]bool{
			"error":  true,
			"failed": true,
		},
		SuccessStatuses: map[string]bool{
			"success": true,
		},
		ExitStatuses: map[string]bool{
			"exit":      true,
			"dismiss":   true,
			"end":       true,
			"cancelled": true,
		},
		EnableHesitation:           true,
		DefaultMinEventsForDropoff: 2,
	}
}
