package pattern

import (
	"fmt"
	"sort"
	"strings"

	"github.com/velum/internal/canonical"
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
		patterns := d.detectPatterns(v, nil)
		return &PatternResult{
			AnalyzedFlows:    v,
			DetectedPatterns: patterns,
		}, nil
	default:
		return input, nil
	}
}

// ProcessWithContext implements the ContextAwareLayer interface.
// When analysis context is provided, enables:
// - Min-events threshold for early_dropoff (prevents false positives on browse flows)
// - Browse flow intent protection (browse flows are not dropoffs)
// - Funnel progression exclusion (progressed flows are not dropoffs)
// - Funnel dropoff detection (cross-flow conversion analysis)
func (d *Detector) ProcessWithContext(input interface{}, metadata interface{}) (interface{}, error) {
	var ctx *behavior.AnalysisContext
	if metadata != nil {
		ctx, _ = metadata.(*behavior.AnalysisContext)
	}

	switch v := input.(type) {
	case []*behavior.AnalyzedFlow:
		patterns := d.detectPatterns(v, ctx)
		return &PatternResult{
			AnalyzedFlows:    v,
			DetectedPatterns: patterns,
		}, nil
	default:
		return d.Process(input)
	}
}

// detectPatterns analyzes behavior summaries and detects patterns
func (d *Detector) detectPatterns(flows []*behavior.AnalyzedFlow, ctx *behavior.AnalysisContext) []*DetectedPattern {
	// Group flows by flow type
	grouped := d.groupByFlow(flows)

	var detectedPatterns []*DetectedPattern

	for flowName, group := range grouped {
		// Sub-group by context key for context-keyed baselines
		subGroups := d.groupByContextKey(group)

		for contextKey, subGroup := range subGroups {
			// Skip if below minimum sample size
			if len(subGroup) < d.config.MinSampleSize {
				continue
			}

			// Check each pattern type
			if pattern := d.detectRetryStorm(flowName, contextKey, subGroup); pattern != nil {
				detectedPatterns = append(detectedPatterns, pattern)
			}

			if pattern := d.detectConfusionLoop(flowName, contextKey, subGroup); pattern != nil {
				detectedPatterns = append(detectedPatterns, pattern)
			}

			if pattern := d.detectSilentAbandonment(flowName, contextKey, subGroup, ctx); pattern != nil {
				detectedPatterns = append(detectedPatterns, pattern)
			}

			if pattern := d.detectEarlyDropoff(flowName, contextKey, subGroup, ctx); pattern != nil {
				detectedPatterns = append(detectedPatterns, pattern)
			}

			if pattern := d.detectBypassBehavior(flowName, contextKey, subGroup); pattern != nil {
				detectedPatterns = append(detectedPatterns, pattern)
			}

			if pattern := d.detectMaskedFailure(flowName, contextKey, subGroup); pattern != nil {
				detectedPatterns = append(detectedPatterns, pattern)
			}
		}
	}

	// Funnel-level patterns (cross-flow analysis)
	if ctx != nil && len(ctx.FunnelDefinitions) > 0 {
		funnelPatterns := d.detectFunnelDropoff(flows, ctx)
		detectedPatterns = append(detectedPatterns, funnelPatterns...)
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

// groupByContextKey sub-groups flows by their derived context key.
// Always includes a global group (empty key "") containing ALL flows so that
// patterns are detectable even when context-specific sub-groups are too small.
// Context-keyed groups are only included when they have enough samples.
func (d *Detector) groupByContextKey(flows []*behavior.AnalyzedFlow) map[string][]*behavior.AnalyzedFlow {
	grouped := make(map[string][]*behavior.AnalyzedFlow)

	// Always include the global (empty key) group with ALL flows
	grouped[""] = flows

	// Also create per-context sub-groups
	for _, flow := range flows {
		key := deriveContextKey(flow.Context)
		if key != "" {
			grouped[key] = append(grouped[key], flow)
		}
	}
	return grouped
}

// deriveContextKey builds a context key from an EventContext.
// Priority: conditions first (sorted), then targets (sorted).
// Format: "key1=val1,key2=val2" — deterministic and human-readable.
// Returns empty string if no conditions or targets exist.
func deriveContextKey(ctx *canonical.EventContext) string {
	if ctx == nil {
		return ""
	}

	var parts []string

	// Conditions take priority — they explain "why" the pattern occurred
	if len(ctx.Conditions) > 0 {
		keys := make([]string, 0, len(ctx.Conditions))
		for k := range ctx.Conditions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", k, ctx.Conditions[k]))
		}
	}

	// Targets explain "what" the action was about
	if len(ctx.Targets) > 0 {
		keys := make([]string, 0, len(ctx.Targets))
		for k := range ctx.Targets {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", k, ctx.Targets[k]))
		}
	}

	return strings.Join(parts, ",")
}

// detectRetryStorm checks for high frequency of retries
// Pattern: >= 30% of flows have retry behavior OR users repeat the same flow
func (d *Detector) detectRetryStorm(flowName, contextKey string, group []*behavior.AnalyzedFlow) *DetectedPattern {
	retryCount := 0
	affectedUserIDs := make(map[string]bool)
	var sampleIDs []string

	// Check 1: Within-instance retries (error → action in same flow instance)
	for _, flow := range group {
		if containsBehavior(flow.Behaviors, behavior.BehaviorRetry) {
			retryCount++
			affectedUserIDs[flow.UserID] = true
			if len(sampleIDs) < 3 {
				sampleIDs = append(sampleIDs, flow.FlowInstanceID)
			}
		}
	}

	// Check 2: Cross-instance retries — when the reconstructor creates separate
	// flow instances per entry (e.g. booking_requested → cancelled → booking_requested),
	// each extra instance from the same user indicates a retry.
	// Only count cross-instance retries when a prior instance FAILED (had error,
	// abandon after attempt, or was incomplete). Normal re-entries (like browse_home
	// appearing multiple times) are NOT retries.
	type userFlowInfo struct {
		count     int
		hasFailed bool // at least one instance had error/failure/abandon-after-attempt
	}
	userFlows := make(map[string]*userFlowInfo)
	for _, flow := range group {
		uf, ok := userFlows[flow.UserID]
		if !ok {
			uf = &userFlowInfo{}
			userFlows[flow.UserID] = uf
		}
		uf.count++
		// A flow instance counts as "failed" if it has errors or was abandoned after
		// the user actually attempted something (not just explored).
		hasError := false
		for _, evt := range flow.Events {
			if evt.Status == "error" || evt.Status == "failed" || evt.Status == "cancelled" {
				hasError = true
				break
			}
		}
		hasAttempt := containsBehavior(flow.Behaviors, behavior.BehaviorAttempt)
		hasAbandon := containsBehavior(flow.Behaviors, behavior.BehaviorAbandon)
		if hasError || (hasAbandon && hasAttempt) || (!flow.IsComplete && hasError) {
			uf.hasFailed = true
		}
	}
	for userID, uf := range userFlows {
		if uf.count > 1 && uf.hasFailed && !affectedUserIDs[userID] {
			// Each instance beyond the first is a retry
			retryCount += uf.count - 1
			affectedUserIDs[userID] = true
		}
	}

	ratio := float64(retryCount) / float64(len(group))

	if ratio >= d.config.RetryStormThreshold {
		return d.buildPattern(
			PatternRetryStorm,
			flowName,
			contextKey,
			group,
			len(affectedUserIDs),
			len(group),
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
func (d *Detector) detectConfusionLoop(flowName, contextKey string, group []*behavior.AnalyzedFlow) *DetectedPattern {
	hesitateCount := 0
	affectedUserIDs := make(map[string]bool)
	var sampleIDs []string

	for _, flow := range group {
		if containsBehavior(flow.Behaviors, behavior.BehaviorHesitate) {
			hesitateCount++
			affectedUserIDs[flow.UserID] = true
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
			contextKey,
			group,
			len(affectedUserIDs),
			len(group),
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
// Context-aware: skips flows where user progressed to next funnel step
// Cross-instance aware: excludes users who retried (multiple instances) or eventually succeeded
func (d *Detector) detectSilentAbandonment(flowName, contextKey string, group []*behavior.AnalyzedFlow, ctx *behavior.AnalysisContext) *DetectedPattern {
	silentAbandonCount := 0
	affectedUserIDs := make(map[string]bool)
	var sampleIDs []string

	// Pre-compute which users retried (>1 instance with at least one failure) or eventually succeeded.
	// Those users are NOT silent abandoners — they took further action.
	usersWhoRetried := make(map[string]bool)
	userFlowCounts := make(map[string]int)
	userHasFailure := make(map[string]bool)
	for _, flow := range group {
		userFlowCounts[flow.UserID]++
		if containsBehavior(flow.Behaviors, behavior.BehaviorSucceed) {
			usersWhoRetried[flow.UserID] = true
		}
		// Check for errors in events
		for _, evt := range flow.Events {
			if evt.Status == "error" || evt.Status == "failed" || evt.Status == "cancelled" {
				userHasFailure[flow.UserID] = true
				break
			}
		}
	}
	for userID, count := range userFlowCounts {
		// Only consider multi-instance as retry if at least one instance failed
		if count > 1 && userHasFailure[userID] {
			usersWhoRetried[userID] = true
		}
	}

	for _, flow := range group {
		// Skip flows where user progressed to next funnel step
		if containsBehavior(flow.Behaviors, behavior.BehaviorProgress) {
			continue
		}

		// Skip users who retried or eventually succeeded
		if usersWhoRetried[flow.UserID] {
			continue
		}

		hasAbandon := containsBehavior(flow.Behaviors, behavior.BehaviorAbandon)
		hasRetry := containsBehavior(flow.Behaviors, behavior.BehaviorRetry)

		if hasAbandon && !hasRetry {
			silentAbandonCount++
			affectedUserIDs[flow.UserID] = true
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
			contextKey,
			group,
			len(affectedUserIDs),
			len(group),
			silentAbandonCount,
			ratio,
			"Users abandoning flow without encountering errors or retrying",
			sampleIDs,
		)
	}

	return nil
}

// detectEarlyDropoff checks for users who explore but immediately abandon.
// Context-aware improvements:
// - Respects MinEventsForDropoff: flows below the threshold are excluded (not dropoffs)
// - Respects FlowIntent: browse-intent flows are never early dropoffs
// - Respects BehaviorProgress: flows where user progressed to next funnel step are excluded
// Pattern: >= 40% of eligible flows have only explore + abandon behaviors
func (d *Detector) detectEarlyDropoff(flowName, contextKey string, group []*behavior.AnalyzedFlow, ctx *behavior.AnalysisContext) *DetectedPattern {
	// Resolve min events threshold: flow-specific > context global > config > default
	minEvents := d.config.MinEventsForDropoff
	if minEvents <= 0 {
		minEvents = 2 // absolute fallback
	}

	// Check context for flow-specific overrides
	flowIsBrowse := false
	if ctx != nil && ctx.FlowConfigs != nil {
		if fc, ok := ctx.FlowConfigs[flowName]; ok {
			if fc.Intent == behavior.FlowIntentBrowse {
				flowIsBrowse = true
			}
			if fc.MinEventsForDropoff > 0 {
				minEvents = fc.MinEventsForDropoff
			}
		}
	}

	// Browse-intent flows by definition don't have early dropoff
	if flowIsBrowse {
		return nil
	}

	earlyDropoffCount := 0
	eligibleCount := 0
	affectedUserIDs := make(map[string]bool)
	var sampleIDs []string

	for _, flow := range group {
		// Skip flows below minimum event threshold (single-event views are not dropoffs)
		// Use EventCount if set by analyzer, fall back to len(Events).
		// If both are 0 (synthetic test data), don't filter.
		eventCount := flow.EventCount
		if eventCount == 0 {
			eventCount = len(flow.Events)
		}
		if eventCount > 0 && eventCount < minEvents {
			continue
		}

		// Skip browse-intent flows (per-flow intent from behavior analyzer)
		if flow.FlowIntent == behavior.FlowIntentBrowse {
			continue
		}

		// Skip flows where user progressed to next funnel step
		if containsBehavior(flow.Behaviors, behavior.BehaviorProgress) {
			continue
		}

		eligibleCount++

		// Check if flow only has explore and/or abandon (no attempt, retry, succeed)
		hasExplore := containsBehavior(flow.Behaviors, behavior.BehaviorExplore)
		hasAbandon := containsBehavior(flow.Behaviors, behavior.BehaviorAbandon)
		hasAttempt := containsBehavior(flow.Behaviors, behavior.BehaviorAttempt)
		hasRetry := containsBehavior(flow.Behaviors, behavior.BehaviorRetry)
		hasSucceed := containsBehavior(flow.Behaviors, behavior.BehaviorSucceed)

		// Early dropoff: explored but never attempted action, or abandoned immediately
		if (hasExplore || hasAbandon) && !hasAttempt && !hasRetry && !hasSucceed {
			earlyDropoffCount++
			affectedUserIDs[flow.UserID] = true
			if len(sampleIDs) < 3 {
				sampleIDs = append(sampleIDs, flow.FlowInstanceID)
			}
		}
	}

	// Use eligible count as denominator (excludes below-threshold and browse flows)
	if eligibleCount == 0 {
		return nil
	}

	ratio := float64(earlyDropoffCount) / float64(eligibleCount)

	if ratio >= d.config.EarlyDropoffThreshold {
		return d.buildPattern(
			PatternEarlyDropoff,
			flowName,
			contextKey,
			group,
			len(affectedUserIDs),
			eligibleCount,
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
func (d *Detector) detectBypassBehavior(flowName, contextKey string, group []*behavior.AnalyzedFlow) *DetectedPattern {
	bypassCount := 0
	affectedUserIDs := make(map[string]bool)
	var sampleIDs []string

	for _, flow := range group {
		if containsBehavior(flow.Behaviors, behavior.BehaviorBypass) {
			bypassCount++
			affectedUserIDs[flow.UserID] = true
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
			contextKey,
			group,
			len(affectedUserIDs),
			len(group),
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
func (d *Detector) detectMaskedFailure(flowName, contextKey string, group []*behavior.AnalyzedFlow) *DetectedPattern {
	maskedFailureCount := 0
	affectedUserIDs := make(map[string]bool)
	var sampleIDs []string

	// Check 1: Within-instance masked failures (retry + succeed in same flow)
	for _, flow := range group {
		hasRetry := containsBehavior(flow.Behaviors, behavior.BehaviorRetry)
		hasSucceed := containsBehavior(flow.Behaviors, behavior.BehaviorSucceed)

		if hasRetry && hasSucceed {
			maskedFailureCount++
			affectedUserIDs[flow.UserID] = true
			if len(sampleIDs) < 3 {
				sampleIDs = append(sampleIDs, flow.FlowInstanceID)
			}
		}
	}

	// Check 2: Cross-instance masked failures — user had at least one failed/abandoned
	// instance of this flow AND at least one successful instance later.
	// This detects the pattern: booking_requested → driver_cancelled → booking_requested → confirmed
	type userOutcome struct {
		hasFailure bool
		hasSuccess bool
	}
	userOutcomes := make(map[string]*userOutcome)
	for _, flow := range group {
		uo, ok := userOutcomes[flow.UserID]
		if !ok {
			uo = &userOutcome{}
			userOutcomes[flow.UserID] = uo
		}
		if containsBehavior(flow.Behaviors, behavior.BehaviorSucceed) {
			uo.hasSuccess = true
		}
		if containsBehavior(flow.Behaviors, behavior.BehaviorAbandon) ||
			(!flow.IsComplete && !containsBehavior(flow.Behaviors, behavior.BehaviorSucceed)) {
			uo.hasFailure = true
		}
	}
	for userID, uo := range userOutcomes {
		if uo.hasFailure && uo.hasSuccess && !affectedUserIDs[userID] {
			maskedFailureCount++
			affectedUserIDs[userID] = true
		}
	}

	if maskedFailureCount >= d.config.MaskedFailureThreshold {
		ratio := float64(maskedFailureCount) / float64(len(group))
		return d.buildPattern(
			PatternMaskedFailure,
			flowName,
			contextKey,
			group,
			len(affectedUserIDs),
			len(group),
			maskedFailureCount,
			ratio,
			"Users experiencing failures but eventually succeeding after retries",
			sampleIDs,
		)
	}

	return nil
}

// buildPattern constructs a DetectedPattern with computed severity and confidence.
// affectedUsers: count of unique users who actually exhibited the pattern.
// totalFlows: the denominator (total eligible flows considered, not necessarily the full group).
func (d *Detector) buildPattern(
	patternType PatternType,
	flowName string,
	contextKey string,
	group []*behavior.AnalyzedFlow,
	affectedUsers int,
	totalFlows int,
	matchingFlows int,
	ratio float64,
	description string,
	sampleIDs []string,
) *DetectedPattern {
	return &DetectedPattern{
		Pattern:       patternType,
		Flow:          flowName,
		ContextKey:    contextKey,
		AffectedUsers: affectedUsers,
		TotalFlows:    totalFlows,
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

// detectFunnelDropoff identifies significant user drop-offs between consecutive
// funnel steps. This is a cross-flow pattern that requires funnel definitions
// in the analysis context.
//
// For each funnel, it counts unique users at each step and reports significant
// conversion drops between consecutive steps (e.g., 10 users at checkout but
// only 3 at payment = 70% drop rate).
func (d *Detector) detectFunnelDropoff(flows []*behavior.AnalyzedFlow, ctx *behavior.AnalysisContext) []*DetectedPattern {
	if ctx == nil || len(ctx.FunnelDefinitions) == 0 {
		return nil
	}

	var patterns []*DetectedPattern

	for funnelID, steps := range ctx.FunnelDefinitions {
		if len(steps) < 2 {
			continue
		}

		// Build step index: flow_name → step position
		stepIndex := make(map[string]int)
		for i, step := range steps {
			stepIndex[step] = i
		}

		// Count unique users at each funnel step
		stepUsers := make(map[int]map[string]bool)
		for i := range steps {
			stepUsers[i] = make(map[string]bool)
		}

		for _, flow := range flows {
			if idx, ok := stepIndex[flow.Flow]; ok {
				stepUsers[idx][flow.UserID] = true
			}
		}

		// Detect significant drops between consecutive steps
		for i := 0; i < len(steps)-1; i++ {
			fromCount := len(stepUsers[i])
			toCount := len(stepUsers[i+1])

			if fromCount < d.config.MinSampleSize {
				continue // Not enough data at this step
			}

			conversionRate := float64(toCount) / float64(fromCount)
			dropRate := 1.0 - conversionRate

			// Report if drop rate exceeds threshold
			if dropRate >= d.config.EarlyDropoffThreshold {
				droppedUsers := fromCount - toCount

				// Collect sample flow IDs from users who dropped at this step
				var sampleIDs []string
				for _, flow := range flows {
					if flow.Flow == steps[i] && !stepUsers[i+1][flow.UserID] {
						if len(sampleIDs) < 3 {
							sampleIDs = append(sampleIDs, flow.FlowInstanceID)
						}
					}
				}

				patterns = append(patterns, &DetectedPattern{
					Pattern:       PatternFunnelDropoff,
					Flow:          steps[i],
					ContextKey:    "",
					AffectedUsers: droppedUsers,
					TotalFlows:    fromCount,
					Severity:      d.computeSeverity(droppedUsers),
					Confidence:    ConfidenceMedium,
					Evidence: PatternEvidence{
						MatchingFlows:  droppedUsers,
						Ratio:          dropRate,
						Description:    fmt.Sprintf("Funnel '%s': %d/%d users dropped between %s → %s (%.0f%% conversion)", funnelID, droppedUsers, fromCount, steps[i], steps[i+1], conversionRate*100),
						SampleFlowIDs:  sampleIDs,
						FunnelID:       funnelID,
						StepFrom:       steps[i],
						StepTo:         steps[i+1],
						ConversionRate: conversionRate,
					},
				})
			}
		}
	}

	return patterns
}
