package aggregation

import (
	"fmt"
	"sort"
	"time"

	"github.com/velum/internal/layers/ai"
	"github.com/velum/internal/layers/baseline"
	"github.com/velum/internal/layers/behavior"
	"github.com/velum/internal/layers/pattern"
)

const engineVersion = "0.1.0"

// ReportBuilder constructs a structured AnalysisReport from pipeline outputs.
// It is NOT a pipeline layer — it runs after the pipeline completes, transforming
// internal representations into a clean, documented external format.
type ReportBuilder struct{}

// NewReportBuilder creates a new ReportBuilder.
func NewReportBuilder() *ReportBuilder {
	return &ReportBuilder{}
}

// ReportInput holds all the data needed to build a report.
// The handler collects these from the pipeline's final output.
type ReportInput struct {
	// AnalysisContext is the context that guided the analysis.
	AnalysisContext *behavior.AnalysisContext

	// AnalyzedFlows from the behavior analyzer.
	AnalyzedFlows []*behavior.AnalyzedFlow

	// DetectedPatterns from the pattern detector.
	DetectedPatterns []*pattern.DetectedPattern

	// ChangeResults from the baseline detector (optional).
	ChangeResults []*baseline.ChangeResult

	// AIAnalysis from the AI layer (optional).
	AIAnalysis *ai.AnalysisResponse
	AIEnabled  bool

	// InputEventsCount is the number of raw events received.
	InputEventsCount int

	// Warnings from input validation.
	Warnings []string

	// PipelineLayers lists the layers that executed.
	PipelineLayers []string

	// RawEvents for field coverage analysis.
	RawEvents []map[string]interface{}
}

// Build constructs the full AnalysisReport from pipeline outputs.
func (rb *ReportBuilder) Build(input *ReportInput) *AnalysisReport {
	ctx := input.AnalysisContext
	if ctx == nil {
		ctx = behavior.DefaultAnalysisContext()
	}

	flows := input.AnalyzedFlows
	patterns := input.DetectedPatterns

	summary := rb.buildSummary(flows, patterns, ctx)
	summary.TotalEvents = input.InputEventsCount

	report := &AnalysisReport{
		Summary:     summary,
		Flows:       rb.buildFlowMetrics(flows),
		Patterns:    rb.buildPatternInsights(patterns, input.ChangeResults),
		Clusters:    rb.buildPatternClusters(patterns),
		DataQuality: rb.buildDataQuality(input),
		Metadata:    rb.buildMetadata(ctx, input.PipelineLayers),
	}

	// Link patterns to their clusters
	rb.linkPatternsToCluster(report.Patterns, report.Clusters)

	// Funnel metrics (only when definitions are provided)
	if ctx != nil && len(ctx.FunnelDefinitions) > 0 {
		report.Funnels = rb.buildFunnelMetrics(flows, ctx)
	}

	// Entity insights (only for user-level scopes)
	if ctx != nil && (ctx.Scope == behavior.ScopeUserSession || ctx.Scope == behavior.ScopeUserHistory) {
		report.EntityInsights = rb.buildEntityInsights(flows, ctx)
	}

	// AI analysis
	if input.AIEnabled && input.AIAnalysis != nil {
		report.AIAnalysis = &AIInsight{
			Enabled:        true,
			Summary:        input.AIAnalysis.Summary,
			Details:        input.AIAnalysis.Details,
			Hypotheses:     input.AIAnalysis.Hypotheses,
			ConfidenceNote: input.AIAnalysis.ConfidenceNote,
		}
	}

	return report
}

// buildSummary computes top-level statistics.
func (rb *ReportBuilder) buildSummary(
	flows []*behavior.AnalyzedFlow,
	patterns []*pattern.DetectedPattern,
	ctx *behavior.AnalysisContext,
) *Summary {
	users := make(map[string]bool)
	flowTypes := make(map[string]bool)

	for _, f := range flows {
		users[f.UserID] = true
		flowTypes[f.Flow] = true
	}

	criticalCount := 0
	for _, p := range patterns {
		if p.Severity == pattern.SeverityHigh {
			criticalCount++
		}
	}

	health := "healthy"
	if criticalCount > 0 {
		health = "critical"
	} else if len(patterns) > 0 {
		health = "warning"
	}

	scope := string(behavior.ScopeRawBatch)
	if ctx != nil {
		scope = string(ctx.Scope)
	}

	return &Summary{
		TotalFlowInstances: len(flows),
		TotalUsers:         len(users),
		UniqueFlowTypes:    len(flowTypes),
		AnalysisScope:      scope,
		PatternsDetected:   len(patterns),
		CriticalPatterns:   criticalCount,
		OverallHealth:      health,
	}
}

// buildFlowMetrics computes per-flow aggregated metrics.
func (rb *ReportBuilder) buildFlowMetrics(flows []*behavior.AnalyzedFlow) map[string]*FlowMetrics {
	// Group flows by flow name
	grouped := make(map[string][]*behavior.AnalyzedFlow)
	for _, f := range flows {
		grouped[f.Flow] = append(grouped[f.Flow], f)
	}

	result := make(map[string]*FlowMetrics, len(grouped))

	for flowName, group := range grouped {
		users := make(map[string]bool)
		totalEvents := 0
		completedCount := 0
		behaviorDist := make(map[string]int)
		outcomeDist := make(map[string]int)
		contextAgg := make(map[string]map[string]int)

		var intent string

		for _, f := range group {
			users[f.UserID] = true
			if f.EventCount > 0 {
				totalEvents += f.EventCount
			} else {
				totalEvents += len(f.Events)
			}

			if f.Outcome == behavior.BehaviorSucceed {
				completedCount++
			}

			outcomeDist[string(f.Outcome)]++

			for _, b := range f.Behaviors {
				behaviorDist[string(b)]++
			}

			if intent == "" && f.FlowIntent != "" {
				intent = string(f.FlowIntent)
			}

			// Aggregate context
			if f.Context != nil {
				for k, v := range f.Context.Dimensions {
					if contextAgg[k] == nil {
						contextAgg[k] = make(map[string]int)
					}
					contextAgg[k][v]++
				}
				for k, v := range f.Context.Conditions {
					if contextAgg[k] == nil {
						contextAgg[k] = make(map[string]int)
					}
					contextAgg[k][fmt.Sprintf("%v", v)]++
				}
				for k, v := range f.Context.Targets {
					if contextAgg[k] == nil {
						contextAgg[k] = make(map[string]int)
					}
					contextAgg[k][fmt.Sprintf("%v", v)]++
				}
			}
		}

		if intent == "" {
			intent = string(behavior.FlowIntentUnknown)
		}

		// Find primary outcome (most common)
		primaryOutcome := "explore"
		maxCount := 0
		for outcome, count := range outcomeDist {
			if count > maxCount {
				primaryOutcome = outcome
				maxCount = count
			}
		}

		avgEvents := 0.0
		if len(group) > 0 {
			avgEvents = float64(totalEvents) / float64(len(group))
		}

		completionRate := 0.0
		if len(group) > 0 {
			completionRate = float64(completedCount) / float64(len(group))
		}

		fm := &FlowMetrics{
			FlowName:             flowName,
			Intent:               intent,
			Instances:            len(group),
			UniqueUsers:          len(users),
			CompletionRate:       completionRate,
			AvgEventsPerFlow:     avgEvents,
			BehaviorDistribution: behaviorDist,
			OutcomeDistribution:  outcomeDist,
			PrimaryOutcome:       primaryOutcome,
		}

		if len(contextAgg) > 0 {
			fm.Context = contextAgg
		}

		result[flowName] = fm
	}

	return result
}

// buildFunnelMetrics computes funnel-level conversion analysis.
func (rb *ReportBuilder) buildFunnelMetrics(
	flows []*behavior.AnalyzedFlow,
	ctx *behavior.AnalysisContext,
) map[string]*FunnelMetrics {
	if ctx == nil || len(ctx.FunnelDefinitions) == 0 {
		return nil
	}

	result := make(map[string]*FunnelMetrics, len(ctx.FunnelDefinitions))

	for funnelID, steps := range ctx.FunnelDefinitions {
		if len(steps) < 2 {
			continue
		}

		// Build step index
		stepIndex := make(map[string]int)
		for i, step := range steps {
			stepIndex[step] = i
		}

		// Count unique users at each step
		stepUsers := make(map[int]map[string]bool)
		for i := range steps {
			stepUsers[i] = make(map[string]bool)
		}

		for _, flow := range flows {
			if idx, ok := stepIndex[flow.Flow]; ok {
				stepUsers[idx][flow.UserID] = true
			}
		}

		// Compute step conversions
		var conversions []*StepConversion
		for i := 0; i < len(steps)-1; i++ {
			fromCount := len(stepUsers[i])
			toCount := len(stepUsers[i+1])

			rate := 0.0
			if fromCount > 0 {
				rate = float64(toCount) / float64(fromCount)
			}

			conversions = append(conversions, &StepConversion{
				From:      steps[i],
				To:        steps[i+1],
				UsersFrom: fromCount,
				UsersTo:   toCount,
				Rate:      rate,
				DropCount: fromCount - toCount,
				DropRate:  1.0 - rate,
			})
		}

		firstStepUsers := len(stepUsers[0])
		lastStepUsers := len(stepUsers[len(steps)-1])
		overallConversion := 0.0
		if firstStepUsers > 0 {
			overallConversion = float64(lastStepUsers) / float64(firstStepUsers)
		}

		result[funnelID] = &FunnelMetrics{
			FunnelID:          funnelID,
			Steps:             steps,
			StepConversions:   conversions,
			OverallConversion: overallConversion,
			TotalEntries:      firstStepUsers,
			TotalCompletions:  lastStepUsers,
		}
	}

	return result
}

// buildPatternInsights merges detected patterns with baseline data into
// a clean, sorted list for API consumers.
func (rb *ReportBuilder) buildPatternInsights(
	patterns []*pattern.DetectedPattern,
	changes []*baseline.ChangeResult,
) []*PatternInsight {
	// Build baseline lookup: "patternType:flow:contextKey" → ChangeResult
	baselineLookup := make(map[string]*baseline.ChangeResult)
	for _, c := range changes {
		key := c.PatternType + ":" + c.Flow + ":" + c.ContextKey
		baselineLookup[key] = c
	}

	insights := make([]*PatternInsight, 0, len(patterns))

	for _, p := range patterns {
		// Count unique users in the affected user set
		uniqueAffected := len(p.AffectedUserIDs)
		if uniqueAffected == 0 {
			uniqueAffected = p.AffectedUsers // fallback for patterns without ID tracking
		}

		// Count unique users in the full flow group (from TotalFlows as approximation,
		// but we can't recover unique users without the original flows here)
		uniqueInGroup := p.TotalFlows

		insight := &PatternInsight{
			Type:                string(p.Pattern),
			Severity:            string(p.Severity),
			Confidence:          string(p.Confidence),
			Flow:                p.Flow,
			ContextKey:          p.ContextKey,
			AffectedUsers:       p.AffectedUsers,
			UniqueAffectedUsers: uniqueAffected,
			TotalFlows:          p.TotalFlows,
			UniqueUsersInGroup:  uniqueInGroup,
			ImpactRatio:         p.Evidence.Ratio,
			Evidence:            p.Evidence.Description,
			SampleFlowIDs:       p.Evidence.SampleFlowIDs,
		}

		// Add funnel info for funnel_dropoff patterns
		if p.Pattern == pattern.PatternFunnelDropoff && p.Evidence.FunnelID != "" {
			insight.FunnelInfo = &FunnelPatternInfo{
				FunnelID:       p.Evidence.FunnelID,
				StepFrom:       p.Evidence.StepFrom,
				StepTo:         p.Evidence.StepTo,
				ConversionRate: p.Evidence.ConversionRate,
			}
		}

		// Merge baseline data
		key := string(p.Pattern) + ":" + p.Flow + ":" + p.ContextKey
		if change, ok := baselineLookup[key]; ok {
			insight.Baseline = &BaselineInfo{
				Available:     change.BaselineStatus == baseline.BaselineStatusSufficient,
				Status:        string(change.BaselineStatus),
				Trend:         string(change.Trend),
				CurrentRatio:  change.CurrentImpactRatio,
				BaselineRatio: change.BaselineImpactRatio,
				DeltaPct:      change.DeltaPercentage,
				Significance:  string(change.ChangeSignificance),
			}
		}

		insights = append(insights, insight)
	}

	// Sort by severity: high → medium → low, then by impact ratio descending
	severityOrder := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.Slice(insights, func(i, j int) bool {
		si := severityOrder[insights[i].Severity]
		sj := severityOrder[insights[j].Severity]
		if si != sj {
			return si < sj
		}
		return insights[i].ImpactRatio > insights[j].ImpactRatio
	})

	return insights
}

// buildEntityInsights creates per-entity behavioral summaries.
func (rb *ReportBuilder) buildEntityInsights(
	flows []*behavior.AnalyzedFlow,
	ctx *behavior.AnalysisContext,
) []*EntityInsight {
	// Group flows by user
	userFlows := make(map[string][]*behavior.AnalyzedFlow)
	for _, f := range flows {
		userFlows[f.UserID] = append(userFlows[f.UserID], f)
	}

	// Build funnel step index for furthest step tracking
	type funnelInfo struct {
		funnelID  string
		stepIndex map[string]int
		steps     []string
	}
	var funnels []funnelInfo
	if ctx != nil {
		for funnelID, steps := range ctx.FunnelDefinitions {
			si := make(map[string]int)
			for i, s := range steps {
				si[s] = i
			}
			funnels = append(funnels, funnelInfo{funnelID: funnelID, stepIndex: si, steps: steps})
		}
	}

	entityType := "user"
	if ctx != nil && ctx.EntityType != "" {
		entityType = ctx.EntityType
	}

	insights := make([]*EntityInsight, 0, len(userFlows))

	for userID, uf := range userFlows {
		flowNames := make(map[string]bool)
		behaviorsSet := make(map[string]bool)
		outcomeCounts := make(map[string]int)

		for _, f := range uf {
			flowNames[f.Flow] = true
			for _, b := range f.Behaviors {
				behaviorsSet[string(b)] = true
			}
			outcomeCounts[string(f.Outcome)]++
		}

		// Find primary outcome
		primaryOutcome := "explore"
		maxCount := 0
		for outcome, count := range outcomeCounts {
			if count > maxCount {
				primaryOutcome = outcome
				maxCount = count
			}
		}

		// Convert sets to sorted slices
		names := make([]string, 0, len(flowNames))
		for n := range flowNames {
			names = append(names, n)
		}
		sort.Strings(names)

		behaviorsList := make([]string, 0, len(behaviorsSet))
		for b := range behaviorsSet {
			behaviorsList = append(behaviorsList, b)
		}
		sort.Strings(behaviorsList)

		insight := &EntityInsight{
			EntityID:       userID,
			EntityType:     entityType,
			FlowsCount:     len(uf),
			FlowNames:      names,
			PrimaryOutcome: primaryOutcome,
			Behaviors:      behaviorsList,
		}

		// Find furthest funnel step
		for _, fi := range funnels {
			maxStep := -1
			for _, f := range uf {
				if idx, ok := fi.stepIndex[f.Flow]; ok && idx > maxStep {
					maxStep = idx
				}
			}
			if maxStep >= 0 {
				insight.FurthestFunnelStep = fi.steps[maxStep]
				insight.FunnelComplete = maxStep == len(fi.steps)-1
			}
		}

		insights = append(insights, insight)
	}

	// Sort by flows count descending (most active entities first)
	sort.Slice(insights, func(i, j int) bool {
		return insights[i].FlowsCount > insights[j].FlowsCount
	})

	return insights
}

// buildDataQuality computes data quality metrics from input.
func (rb *ReportBuilder) buildDataQuality(input *ReportInput) *DataQuality {
	dq := &DataQuality{
		EventsReceived: input.InputEventsCount,
		Warnings:       input.Warnings,
		FieldCoverage:  make(map[string]float64),
	}

	// Count events that made it into flow instances
	totalFlowEvents := 0
	for _, f := range input.AnalyzedFlows {
		totalFlowEvents += len(f.Events)
	}
	dq.EventsProcessed = totalFlowEvents
	dq.FlowInstancesCreated = len(input.AnalyzedFlows)

	// Compute field coverage from raw events
	if len(input.RawEvents) > 0 {
		fieldPresence := map[string]int{
			"id":         0,
			"user_id":    0,
			"ts":         0,
			"event":      0,
			"session_id": 0,
		}

		for _, evt := range input.RawEvents {
			for field := range fieldPresence {
				if _, ok := evt[field]; ok {
					fieldPresence[field]++
				}
			}
			// Also check event_name as alias for event
			if _, ok := evt["event_name"]; ok {
				fieldPresence["event"]++
			}
		}

		total := len(input.RawEvents)
		for field, count := range fieldPresence {
			coverage := float64(count) / float64(total)
			if coverage > 1.0 {
				coverage = 1.0 // Can exceed 1.0 if event + event_name both present
			}
			dq.FieldCoverage[field] = coverage
		}
	}

	return dq
}

// buildMetadata creates processing metadata.
func (rb *ReportBuilder) buildMetadata(
	ctx *behavior.AnalysisContext,
	pipelineLayers []string,
) *AnalysisMetadata {
	meta := &AnalysisMetadata{
		ProcessedAt:    time.Now().UTC().Format(time.RFC3339),
		EngineVersion:  engineVersion,
		PipelineLayers: pipelineLayers,
	}

	if ctx != nil {
		meta.AnalysisScope = string(ctx.Scope)
		meta.EntityID = ctx.EntityID
		meta.EntityType = ctx.EntityType
	}

	return meta
}

// buildPatternClusters groups co-occurring patterns on the same flow into
// root cause clusters. Patterns sharing >50% affected users (Jaccard similarity)
// on the same flow are merged into a single cluster.
func (rb *ReportBuilder) buildPatternClusters(patterns []*pattern.DetectedPattern) []*PatternCluster {
	// Group patterns by flow (only global context key — skip context-keyed variants)
	byFlow := make(map[string][]*pattern.DetectedPattern)
	for _, p := range patterns {
		if p.ContextKey == "" {
			byFlow[p.Flow] = append(byFlow[p.Flow], p)
		}
	}

	var clusters []*PatternCluster
	severityOrder := map[pattern.Severity]int{pattern.SeverityHigh: 0, pattern.SeverityMedium: 1, pattern.SeverityLow: 2}

	for flow, group := range byFlow {
		if len(group) < 2 {
			continue // need at least 2 patterns to form a cluster
		}

		// Check user-set overlap between all pairs
		// Build an adjacency list of patterns with Jaccard > 0.5
		n := len(group)
		clustered := make([]bool, n)
		clusterAssignment := make([]int, n) // -1 = unassigned
		for i := range clusterAssignment {
			clusterAssignment[i] = -1
		}

		nextClusterID := 0
		for i := 0; i < n; i++ {
			if len(group[i].AffectedUserIDs) == 0 {
				continue
			}
			for j := i + 1; j < n; j++ {
				if len(group[j].AffectedUserIDs) == 0 {
					continue
				}
				jaccard := jaccardSimilarity(group[i].AffectedUserIDs, group[j].AffectedUserIDs)
				if jaccard >= 0.5 {
					// Merge into same cluster
					if clusterAssignment[i] >= 0 {
						clusterAssignment[j] = clusterAssignment[i]
					} else if clusterAssignment[j] >= 0 {
						clusterAssignment[i] = clusterAssignment[j]
					} else {
						clusterAssignment[i] = nextClusterID
						clusterAssignment[j] = nextClusterID
						nextClusterID++
					}
					clustered[i] = true
					clustered[j] = true
				}
			}
		}

		// Build clusters from assignments
		clusterMembers := make(map[int][]*pattern.DetectedPattern)
		for i, cid := range clusterAssignment {
			if cid >= 0 {
				clusterMembers[cid] = append(clusterMembers[cid], group[i])
			}
		}

		for cid, members := range clusterMembers {
			if len(members) < 2 {
				continue
			}

			// Sort by severity (highest first), then by pattern type weight
			sort.Slice(members, func(i, j int) bool {
				si := severityOrder[members[i].Severity]
				sj := severityOrder[members[j].Severity]
				if si != sj {
					return si < sj
				}
				wi := pattern.PatternTypeWeight[members[i].Pattern]
				wj := pattern.PatternTypeWeight[members[j].Pattern]
				return wi > wj
			})

			root := members[0]
			var effects []string
			for _, m := range members[1:] {
				effects = append(effects, string(m.Pattern))
			}

			// Compute union of all affected users
			union := make(map[string]bool)
			for _, m := range members {
				for uid := range m.AffectedUserIDs {
					union[uid] = true
				}
			}

			// Compute overall overlap ratio (average pairwise Jaccard)
			var totalJaccard float64
			pairs := 0
			for i := 0; i < len(members); i++ {
				for j := i + 1; j < len(members); j++ {
					totalJaccard += jaccardSimilarity(members[i].AffectedUserIDs, members[j].AffectedUserIDs)
					pairs++
				}
			}
			avgJaccard := 0.0
			if pairs > 0 {
				avgJaccard = totalJaccard / float64(pairs)
			}

			clusterID := fmt.Sprintf("%s_cluster_%d", flow, cid)
			clusters = append(clusters, &PatternCluster{
				ClusterID:      clusterID,
				RootPattern:    string(root.Pattern),
				EffectPatterns: effects,
				Flow:           flow,
				AffectedUsers:  len(union),
				OverlapRatio:   avgJaccard,
				Severity:       string(root.Severity),
			})
		}
	}

	return clusters
}

// linkPatternsToCluster assigns ClusterID to pattern insights that belong to a cluster.
func (rb *ReportBuilder) linkPatternsToCluster(insights []*PatternInsight, clusters []*PatternCluster) {
	for _, cluster := range clusters {
		allTypes := append([]string{cluster.RootPattern}, cluster.EffectPatterns...)
		for _, insight := range insights {
			if insight.Flow == cluster.Flow && insight.ContextKey == "" {
				for _, pt := range allTypes {
					if insight.Type == pt {
						insight.ClusterID = cluster.ClusterID
					}
				}
			}
		}
	}
}

// jaccardSimilarity computes |A ∩ B| / |A ∪ B| for two user sets.
func jaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// ExtractFromPipelineOutput extracts typed data from the pipeline's final output.
// Handles all possible terminal types: *ai.AIResult, *baseline.BaselineResult,
// *pattern.PatternResult, or raw []*behavior.AnalyzedFlow.
func ExtractFromPipelineOutput(output interface{}) (
	analyzedFlows []*behavior.AnalyzedFlow,
	detectedPatterns []*pattern.DetectedPattern,
	changeResults []*baseline.ChangeResult,
	aiAnalysis *ai.AnalysisResponse,
	aiEnabled bool,
) {
	switch v := output.(type) {
	case *ai.AIResult:
		if af, ok := v.AnalyzedFlows.([]*behavior.AnalyzedFlow); ok {
			analyzedFlows = af
		}
		if dp, ok := v.DetectedPatterns.([]*pattern.DetectedPattern); ok {
			detectedPatterns = dp
		}
		changeResults = v.ChangeResults
		aiAnalysis = v.AIAnalysis
		aiEnabled = v.AIEnabled

	case *baseline.BaselineResult:
		if af, ok := v.AnalyzedFlows.([]*behavior.AnalyzedFlow); ok {
			analyzedFlows = af
		}
		if dp, ok := v.DetectedPatterns.([]*pattern.DetectedPattern); ok {
			detectedPatterns = dp
		}
		changeResults = v.ChangeResults

	case *pattern.PatternResult:
		analyzedFlows = v.AnalyzedFlows
		detectedPatterns = v.DetectedPatterns

	case []*behavior.AnalyzedFlow:
		analyzedFlows = v
	}

	return
}
