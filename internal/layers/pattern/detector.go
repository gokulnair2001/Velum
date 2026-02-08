package pattern

import (
	"github.com/velum/internal/layers/behavior"
)

// Detector detects patterns from behavioral data
type Detector struct {
	config *Config
}

// New creates a new Pattern Detector with default config
func New() *Detector {
	return &Detector{
		config: DefaultConfig(),
	}
}

// NewWithConfig creates a Pattern Detector with custom config
func NewWithConfig(config *Config) *Detector {
	return &Detector{
		config: config,
	}
}

// Name returns the layer identifier
func (d *Detector) Name() string {
	return "pattern_detector"
}

// PatternResult contains both analyzed flows and detected patterns
type PatternResult struct {
	AnalyzedFlows    []*behavior.AnalyzedFlow `json:"analyzed_flows"`
	DetectedPatterns []*DetectedPattern       `json:"detected_patterns"`
}

// Process implements the Layer interface
func (d *Detector) Process(input interface{}) (interface{}, error) {
	switch v := input.(type) {
	case []*behavior.AnalyzedFlow:
		patterns := d.detectPatterns(v)
		return &PatternResult{
			AnalyzedFlows:    v,
			DetectedPatterns: patterns,
		}, nil
	default:
		return input, nil
	}
}

// detectPatterns analyzes behavior summaries and detects patterns
func (d *Detector) detectPatterns(flows []*behavior.AnalyzedFlow) []*DetectedPattern {
	// Group flows by flow type
	grouped := d.groupByFlow(flows)

	var detectedPatterns []*DetectedPattern

	for flowName, group := range grouped {
		// Skip if below minimum sample size
		if len(group) < d.config.MinSampleSize {
			continue
		}

		// Check each pattern type
		if pattern := d.detectRetryStorm(flowName, group); pattern != nil {
			detectedPatterns = append(detectedPatterns, pattern)
		}

		if pattern := d.detectConfusionLoop(flowName, group); pattern != nil {
			detectedPatterns = append(detectedPatterns, pattern)
		}

		if pattern := d.detectSilentAbandonment(flowName, group); pattern != nil {
			detectedPatterns = append(detectedPatterns, pattern)
		}

		if pattern := d.detectEarlyDropoff(flowName, group); pattern != nil {
			detectedPatterns = append(detectedPatterns, pattern)
		}

		if pattern := d.detectBypassBehavior(flowName, group); pattern != nil {
			detectedPatterns = append(detectedPatterns, pattern)
		}

		if pattern := d.detectMaskedFailure(flowName, group); pattern != nil {
			detectedPatterns = append(detectedPatterns, pattern)
		}
	}

	return detectedPatterns
}

// groupByFlow groups analyzed flows by flow name
func (d *Detector) groupByFlow(flows []*behavior.AnalyzedFlow) map[string][]*behavior.AnalyzedFlow {
	grouped := make(map[string][]*behavior.AnalyzedFlow)
	for _, flow := range flows {
		grouped[flow.Flow] = append(grouped[flow.Flow], flow)
	}
	return grouped
}

// detectRetryStorm checks for high frequency of retries
// Pattern: >= 30% of flows have retry behavior
func (d *Detector) detectRetryStorm(flowName string, group []*behavior.AnalyzedFlow) *DetectedPattern {
	retryCount := 0
	var sampleIDs []string

	for _, flow := range group {
		if containsBehavior(flow.Behaviors, behavior.BehaviorRetry) {
			retryCount++
			if len(sampleIDs) < 3 {
				sampleIDs = append(sampleIDs, flow.FlowInstanceID)
			}
		}
	}

	ratio := float64(retryCount) / float64(len(group))

	if ratio >= d.config.RetryStormThreshold {
		return d.buildPattern(
			PatternRetryStorm,
			flowName,
			group,
			retryCount,
			ratio,
			"High frequency of retry attempts detected",
			sampleIDs,
		)
	}

	return nil
}

// detectConfusionLoop checks for high frequency of hesitation
// Pattern: >= 30% of flows have hesitate behavior
func (d *Detector) detectConfusionLoop(flowName string, group []*behavior.AnalyzedFlow) *DetectedPattern {
	hesitateCount := 0
	var sampleIDs []string

	for _, flow := range group {
		if containsBehavior(flow.Behaviors, behavior.BehaviorHesitate) {
			hesitateCount++
			if len(sampleIDs) < 3 {
				sampleIDs = append(sampleIDs, flow.FlowInstanceID)
			}
		}
	}

	ratio := float64(hesitateCount) / float64(len(group))

	if ratio >= d.config.ConfusionLoopThreshold {
		return d.buildPattern(
			PatternConfusionLoop,
			flowName,
			group,
			hesitateCount,
			ratio,
			"Users showing confusion with repeated back-and-forth navigation",
			sampleIDs,
		)
	}

	return nil
}

// detectSilentAbandonment checks for abandons without errors or retries
// Pattern: Abandon without any retry attempts
func (d *Detector) detectSilentAbandonment(flowName string, group []*behavior.AnalyzedFlow) *DetectedPattern {
	silentAbandonCount := 0
	var sampleIDs []string

	for _, flow := range group {
		hasAbandon := containsBehavior(flow.Behaviors, behavior.BehaviorAbandon)
		hasRetry := containsBehavior(flow.Behaviors, behavior.BehaviorRetry)

		if hasAbandon && !hasRetry {
			silentAbandonCount++
			if len(sampleIDs) < 3 {
				sampleIDs = append(sampleIDs, flow.FlowInstanceID)
			}
		}
	}

	if silentAbandonCount >= d.config.SilentAbandonmentThreshold {
		ratio := float64(silentAbandonCount) / float64(len(group))
		return d.buildPattern(
			PatternSilentAbandonment,
			flowName,
			group,
			silentAbandonCount,
			ratio,
			"Users abandoning flow without encountering errors or retrying",
			sampleIDs,
		)
	}

	return nil
}

// detectEarlyDropoff checks for users who explore but immediately abandon
// Pattern: >= 40% of flows have only explore + abandon behaviors
func (d *Detector) detectEarlyDropoff(flowName string, group []*behavior.AnalyzedFlow) *DetectedPattern {
	earlyDropoffCount := 0
	var sampleIDs []string

	for _, flow := range group {
		// Check if flow only has explore and/or abandon (no attempt, retry, succeed)
		hasExplore := containsBehavior(flow.Behaviors, behavior.BehaviorExplore)
		hasAbandon := containsBehavior(flow.Behaviors, behavior.BehaviorAbandon)
		hasAttempt := containsBehavior(flow.Behaviors, behavior.BehaviorAttempt)
		hasRetry := containsBehavior(flow.Behaviors, behavior.BehaviorRetry)
		hasSucceed := containsBehavior(flow.Behaviors, behavior.BehaviorSucceed)

		// Early dropoff: explored but never attempted action, or abandoned immediately
		if (hasExplore || hasAbandon) && !hasAttempt && !hasRetry && !hasSucceed {
			earlyDropoffCount++
			if len(sampleIDs) < 3 {
				sampleIDs = append(sampleIDs, flow.FlowInstanceID)
			}
		}
	}

	ratio := float64(earlyDropoffCount) / float64(len(group))

	if ratio >= d.config.EarlyDropoffThreshold {
		return d.buildPattern(
			PatternEarlyDropoff,
			flowName,
			group,
			earlyDropoffCount,
			ratio,
			"Users dropping off early without attempting any action",
			sampleIDs,
		)
	}

	return nil
}

// detectBypassBehavior checks for flows that skip expected entry points
// Pattern: Flows with bypass behavior
func (d *Detector) detectBypassBehavior(flowName string, group []*behavior.AnalyzedFlow) *DetectedPattern {
	bypassCount := 0
	var sampleIDs []string

	for _, flow := range group {
		if containsBehavior(flow.Behaviors, behavior.BehaviorBypass) {
			bypassCount++
			if len(sampleIDs) < 3 {
				sampleIDs = append(sampleIDs, flow.FlowInstanceID)
			}
		}
	}

	if bypassCount >= d.config.BypassThreshold {
		ratio := float64(bypassCount) / float64(len(group))
		return d.buildPattern(
			PatternBypassBehavior,
			flowName,
			group,
			bypassCount,
			ratio,
			"Users bypassing expected flow entry points",
			sampleIDs,
		)
	}

	return nil
}

// detectMaskedFailure checks for flows with retries that eventually succeed
// Pattern: Retry + Success indicates hidden failures that users overcome
func (d *Detector) detectMaskedFailure(flowName string, group []*behavior.AnalyzedFlow) *DetectedPattern {
	maskedFailureCount := 0
	var sampleIDs []string

	for _, flow := range group {
		hasRetry := containsBehavior(flow.Behaviors, behavior.BehaviorRetry)
		hasSucceed := containsBehavior(flow.Behaviors, behavior.BehaviorSucceed)

		if hasRetry && hasSucceed {
			maskedFailureCount++
			if len(sampleIDs) < 3 {
				sampleIDs = append(sampleIDs, flow.FlowInstanceID)
			}
		}
	}

	if maskedFailureCount >= d.config.MaskedFailureThreshold {
		ratio := float64(maskedFailureCount) / float64(len(group))
		return d.buildPattern(
			PatternMaskedFailure,
			flowName,
			group,
			maskedFailureCount,
			ratio,
			"Users experiencing failures but eventually succeeding after retries",
			sampleIDs,
		)
	}

	return nil
}

// buildPattern constructs a DetectedPattern with computed severity and confidence
func (d *Detector) buildPattern(
	patternType PatternType,
	flowName string,
	group []*behavior.AnalyzedFlow,
	matchingFlows int,
	ratio float64,
	description string,
	sampleIDs []string,
) *DetectedPattern {
	affectedUsers := d.countUniqueUsers(group)
	
	return &DetectedPattern{
		Pattern:       patternType,
		Flow:          flowName,
		AffectedUsers: affectedUsers,
		TotalFlows:    len(group),
		Severity:      d.computeSeverity(affectedUsers),
		Confidence:    d.computeConfidence(group),
		Evidence: PatternEvidence{
			MatchingFlows: matchingFlows,
			Ratio:         ratio,
			Description:   description,
			SampleFlowIDs: sampleIDs,
		},
	}
}

// countUniqueUsers counts unique users in the flow group
func (d *Detector) countUniqueUsers(group []*behavior.AnalyzedFlow) int {
	users := make(map[string]bool)
	for _, flow := range group {
		users[flow.UserID] = true
	}
	return len(users)
}

// computeSeverity determines severity based on affected user count
func (d *Detector) computeSeverity(affectedUsers int) Severity {
	if affectedUsers >= d.config.HighSeverityUserCount {
		return SeverityHigh
	}
	if affectedUsers >= d.config.MediumSeverityUserCount {
		return SeverityMedium
	}
	return SeverityLow
}

// computeConfidence determines confidence based on flow context types
func (d *Detector) computeConfidence(group []*behavior.AnalyzedFlow) Confidence {
	mediumCount := 0
	for _, flow := range group {
		if flow.Confidence == "medium" {
			mediumCount++
		}
	}

	// If all flows have medium confidence (explicit session), pattern confidence is high
	if mediumCount == len(group) {
		return ConfidenceHigh
	}
	
	// If majority have medium confidence
	if float64(mediumCount)/float64(len(group)) >= 0.5 {
		return ConfidenceMedium
	}

	return ConfidenceLow
}

// containsBehavior checks if a behavior exists in the list
func containsBehavior(behaviors []behavior.BehaviorType, target behavior.BehaviorType) bool {
	for _, b := range behaviors {
		if b == target {
			return true
		}
	}
	return false
}

// GetConfig returns the current configuration
func (d *Detector) GetConfig() *Config {
	return d.config
}
