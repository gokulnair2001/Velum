package aggregation

// AnalysisReport is the top-level structured output from Velum.
// This is what API consumers receive — designed for dashboards, alerting,
// and downstream systems. Every field is intentional and documented.
type AnalysisReport struct {
	// Summary provides high-level stats about the analyzed batch.
	Summary *Summary `json:"summary"`

	// Flows provides per-flow aggregated metrics.
	// Key is flow name. Every flow seen in the batch gets an entry.
	Flows map[string]*FlowMetrics `json:"flows"`

	// Funnels provides funnel-level conversion analysis.
	// Only populated when AnalysisContext has FunnelDefinitions.
	// Key is funnel ID.
	Funnels map[string]*FunnelMetrics `json:"funnels,omitempty"`

	// Patterns lists all detected patterns, sorted by severity (critical first).
	// Each pattern includes evidence, optional baseline comparison, and context.
	Patterns []*PatternInsight `json:"patterns"`

	// EntityInsights provides per-entity behavioral summaries.
	// Only populated when scope is user_session/user_history and entity data is available.
	EntityInsights []*EntityInsight `json:"entity_insights,omitempty"`

	// AIAnalysis contains LLM-generated insights (optional, only when AI layer is enabled).
	AIAnalysis *AIInsight `json:"ai_analysis,omitempty"`

	// DataQuality reports on input data completeness and processing outcomes.
	DataQuality *DataQuality `json:"data_quality"`

	// Metadata contains processing metadata for debugging and reproducibility.
	Metadata *AnalysisMetadata `json:"metadata"`
}

// Summary provides high-level operational stats about the analyzed batch.
type Summary struct {
	// TotalEvents is the number of raw events received in the request.
	TotalEvents int `json:"total_events"`

	// TotalUsers is the number of unique user/entity IDs found.
	TotalUsers int `json:"total_users"`

	// TotalFlowInstances is the count of flow instances reconstructed.
	TotalFlowInstances int `json:"total_flow_instances"`

	// UniqueFlowTypes is the number of distinct flow names (e.g., checkout, payment).
	UniqueFlowTypes int `json:"unique_flow_types"`

	// AnalysisScope is the scope used for this analysis (user_session, flow_cohort, etc.).
	AnalysisScope string `json:"analysis_scope"`

	// PatternsDetected is the total number of patterns found.
	PatternsDetected int `json:"patterns_detected"`

	// CriticalPatterns is the count of high-severity patterns that need attention.
	CriticalPatterns int `json:"critical_patterns"`

	// OverallHealth is a one-word health indicator derived from pattern severity.
	// Values: "healthy" (no high-severity patterns), "warning" (some medium patterns),
	// "critical" (at least one high-severity pattern).
	OverallHealth string `json:"overall_health"`
}

// FlowMetrics provides per-flow aggregated behavioral metrics.
type FlowMetrics struct {
	// FlowName is the flow identifier (e.g., "checkout", "payment").
	FlowName string `json:"flow_name"`

	// Intent is the classified flow intent: "browse", "transact", or "unknown".
	Intent string `json:"intent"`

	// Instances is the total number of flow instances for this flow.
	Instances int `json:"instances"`

	// UniqueUsers is the number of distinct users who entered this flow.
	UniqueUsers int `json:"unique_users"`

	// CompletionRate is the ratio of flows that completed successfully (0.0–1.0).
	CompletionRate float64 `json:"completion_rate"`

	// AvgEventsPerFlow is the mean number of events per flow instance.
	AvgEventsPerFlow float64 `json:"avg_events_per_flow"`

	// BehaviorDistribution maps behavior types to their occurrence count.
	// Key is behavior name (e.g., "succeed", "retry"), value is count.
	BehaviorDistribution map[string]int `json:"behavior_distribution"`

	// OutcomeDistribution maps outcome (highest-priority behavior) to count.
	// This shows the final disposition of each flow instance.
	OutcomeDistribution map[string]int `json:"outcome_distribution"`

	// PrimaryOutcome is the most common outcome across all instances of this flow.
	PrimaryOutcome string `json:"primary_outcome"`

	// Context provides dimensional/conditional distribution for this flow.
	// Key is property name, value is map of value → count.
	// Example: {"device": {"mobile": 30, "desktop": 12}, "error_code": {"timeout": 5}}
	Context map[string]map[string]int `json:"context,omitempty"`
}

// FunnelMetrics provides funnel-level conversion analysis.
type FunnelMetrics struct {
	// FunnelID is the funnel identifier (from AnalysisContext.FunnelDefinitions).
	FunnelID string `json:"funnel_id"`

	// Steps is the ordered list of flow names in this funnel.
	Steps []string `json:"steps"`

	// StepConversions provides conversion data between each consecutive pair of steps.
	StepConversions []*StepConversion `json:"step_conversions"`

	// OverallConversion is the ratio of users who completed the entire funnel.
	// (users at last step / users at first step)
	OverallConversion float64 `json:"overall_conversion"`

	// TotalEntries is the number of unique users who entered the first step.
	TotalEntries int `json:"total_entries"`

	// TotalCompletions is the number of unique users who reached the last step.
	TotalCompletions int `json:"total_completions"`
}

// StepConversion describes the conversion between two consecutive funnel steps.
type StepConversion struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	UsersFrom int     `json:"users_from"`
	UsersTo   int     `json:"users_to"`
	Rate      float64 `json:"rate"`       // Conversion rate (users_to / users_from)
	DropCount int     `json:"drop_count"` // Users who didn't make it to the next step
	DropRate  float64 `json:"drop_rate"`  // 1 - rate
}

// PatternInsight is a production-ready pattern representation.
// Merges data from DetectedPattern + ChangeResult into a single consumable object.
type PatternInsight struct {
	// Type is the pattern identifier (e.g., "retry_storm", "early_dropoff").
	Type string `json:"type"`

	// Severity is "high", "medium", or "low".
	Severity string `json:"severity"`

	// Confidence is "high", "medium", or "low".
	Confidence string `json:"confidence"`

	// Flow is the flow this pattern applies to.
	Flow string `json:"flow"`

	// ContextKey describes specific conditions (e.g., "error_code=timeout").
	// Empty for global patterns.
	ContextKey string `json:"context_key,omitempty"`

	// AffectedUsers is the count of unique users affected by this pattern.
	AffectedUsers int `json:"affected_users"`

	// TotalFlows is the denominator — total flows in this group.
	TotalFlows int `json:"total_flows"`

	// ImpactRatio is the ratio of affected flows (0.0–1.0).
	ImpactRatio float64 `json:"impact_ratio"`

	// Evidence is a human-readable explanation of why this pattern was detected.
	Evidence string `json:"evidence"`

	// SampleFlowIDs provides example flow instance IDs for debugging.
	SampleFlowIDs []string `json:"sample_flow_ids,omitempty"`

	// FunnelInfo is populated only for funnel_dropoff patterns.
	FunnelInfo *FunnelPatternInfo `json:"funnel_info,omitempty"`

	// Baseline provides historical comparison if available.
	Baseline *BaselineInfo `json:"baseline,omitempty"`
}

// FunnelPatternInfo holds funnel-specific data for funnel_dropoff patterns.
type FunnelPatternInfo struct {
	FunnelID       string  `json:"funnel_id"`
	StepFrom       string  `json:"step_from"`
	StepTo         string  `json:"step_to"`
	ConversionRate float64 `json:"conversion_rate"`
}

// BaselineInfo provides historical context for a pattern.
type BaselineInfo struct {
	// Available is true when baseline comparison data exists.
	Available bool `json:"available"`

	// Status is the baseline data status: "sufficient", "insufficient_data",
	// "first_observation", or "out_of_window".
	Status string `json:"status"`

	// Trend is "increasing", "decreasing", "stable", or "unknown".
	Trend string `json:"trend,omitempty"`

	// CurrentRatio is the current impact ratio.
	CurrentRatio float64 `json:"current_ratio,omitempty"`

	// BaselineRatio is the historical average impact ratio.
	BaselineRatio float64 `json:"baseline_ratio,omitempty"`

	// DeltaPct is the percentage change from baseline.
	DeltaPct float64 `json:"delta_pct,omitempty"`

	// Significance is "high", "medium", or "low".
	Significance string `json:"significance,omitempty"`
}

// EntityInsight provides per-entity behavioral summary.
// Useful for understanding individual user journeys or organizational patterns.
type EntityInsight struct {
	// EntityID is the identifier (user_id, org_id, etc.).
	EntityID string `json:"entity_id"`

	// EntityType describes what EntityID represents (e.g., "user", "org").
	EntityType string `json:"entity_type"`

	// FlowsCount is the number of flow instances for this entity.
	FlowsCount int `json:"flows_count"`

	// FlowNames lists the distinct flows this entity participated in.
	FlowNames []string `json:"flow_names"`

	// PrimaryOutcome is the most common behavioral outcome across this entity's flows.
	PrimaryOutcome string `json:"primary_outcome"`

	// Behaviors lists all unique behaviors observed for this entity.
	Behaviors []string `json:"behaviors"`

	// FurthestFunnelStep is the deepest funnel step this entity reached.
	// Empty if no funnel definitions are provided.
	FurthestFunnelStep string `json:"furthest_funnel_step,omitempty"`

	// FunnelComplete is true if the entity completed the entire funnel.
	FunnelComplete bool `json:"funnel_complete,omitempty"`
}

// AIInsight wraps the AI analysis output.
type AIInsight struct {
	Enabled        bool     `json:"enabled"`
	Summary        string   `json:"summary,omitempty"`
	Details        []string `json:"details,omitempty"`
	Hypotheses     []string `json:"hypotheses,omitempty"`
	ConfidenceNote string   `json:"confidence_note,omitempty"`
}

// DataQuality reports on input data completeness and processing outcomes.
type DataQuality struct {
	// EventsReceived is the total number of raw events in the request.
	EventsReceived int `json:"events_received"`

	// EventsProcessed is the number successfully processed into flow instances.
	EventsProcessed int `json:"events_processed"`

	// FlowInstancesCreated is the number of flow instances reconstructed.
	FlowInstancesCreated int `json:"flow_instances_created"`

	// FieldCoverage reports the fraction of events that have each key field.
	// Key is field name, value is coverage ratio (0.0–1.0).
	FieldCoverage map[string]float64 `json:"field_coverage"`

	// Warnings are non-fatal issues detected in the input data.
	Warnings []string `json:"warnings,omitempty"`
}

// AnalysisMetadata contains processing context for debugging and reproducibility.
type AnalysisMetadata struct {
	// ProcessedAt is the ISO 8601 timestamp of when the analysis ran.
	ProcessedAt string `json:"processed_at"`

	// EngineVersion is the Velum engine version.
	EngineVersion string `json:"engine_version"`

	// PipelineLayers lists the layers that were executed (in order).
	PipelineLayers []string `json:"pipeline_layers"`

	// AnalysisScope is the scope that was used.
	AnalysisScope string `json:"analysis_scope"`

	// EntityID is the entity this analysis concerned (if applicable).
	EntityID string `json:"entity_id,omitempty"`

	// EntityType is the entity type (if applicable).
	EntityType string `json:"entity_type,omitempty"`
}
