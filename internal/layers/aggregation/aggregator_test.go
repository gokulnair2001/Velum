package aggregation

import (
	"testing"
	"time"

	"github.com/velum/internal/canonical"
	"github.com/velum/internal/layers/ai"
	"github.com/velum/internal/layers/baseline"
	"github.com/velum/internal/layers/behavior"
	"github.com/velum/internal/layers/pattern"
	"github.com/velum/internal/layers/sessionflow"
)

func makeFlow(userID, flow string, outcome behavior.BehaviorType, behaviors []behavior.BehaviorType, eventCount int) *behavior.AnalyzedFlow {
	events := make([]sessionflow.FlowEvent, eventCount)
	for i := 0; i < eventCount; i++ {
		events[i] = sessionflow.FlowEvent{Timestamp: time.Now()}
	}
	return &behavior.AnalyzedFlow{
		FlowInstanceID: userID + "_" + flow,
		UserID:         userID,
		Flow:           flow,
		Outcome:        outcome,
		Behaviors:      behaviors,
		EventCount:     eventCount,
		Events:         events,
		FlowIntent:     behavior.FlowIntentUnknown,
		StartTime:      time.Now(),
	}
}

func makeFlowWithContext(userID, flow string, outcome behavior.BehaviorType, ctx *canonical.EventContext) *behavior.AnalyzedFlow {
	f := makeFlow(userID, flow, outcome, []behavior.BehaviorType{outcome}, 3)
	f.Context = ctx
	return f
}

func makePattern(pt pattern.PatternType, flow string, sev pattern.Severity, affected, total int, ratio float64) *pattern.DetectedPattern {
	return &pattern.DetectedPattern{
		Pattern:       pt,
		Flow:          flow,
		AffectedUsers: affected,
		TotalFlows:    total,
		Severity:      sev,
		Confidence:    pattern.ConfidenceMedium,
		Evidence: pattern.PatternEvidence{
			MatchingFlows: affected,
			Ratio:         ratio,
			Description:   "Test pattern detected",
		},
	}
}

func makeChange(pt, flow string, trend baseline.Trend, delta float64, status baseline.BaselineStatus) *baseline.ChangeResult {
	return &baseline.ChangeResult{
		PatternType:         pt,
		Flow:                flow,
		CurrentImpactRatio:  0.4,
		BaselineImpactRatio: 0.25,
		Delta:               0.15,
		DeltaPercentage:     delta,
		Trend:               trend,
		ChangeSignificance:  baseline.SignificanceHigh,
		BaselineStatus:      status,
	}
}

func TestBuildEmptyReport(t *testing.T) {
	rb := NewReportBuilder()
	report := rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		AnalyzedFlows:    nil,
		DetectedPatterns: nil,
		InputEventsCount: 0,
	})
	if report == nil {
		t.Fatal("expected report, got nil")
	}
	if report.Summary == nil {
		t.Fatal("expected summary")
	}
	if report.Summary.TotalFlowInstances != 0 {
		t.Errorf("expected 0 flow instances, got %d", report.Summary.TotalFlowInstances)
	}
	if report.Summary.OverallHealth != "healthy" {
		t.Errorf("expected healthy, got %s", report.Summary.OverallHealth)
	}
	if report.DataQuality == nil {
		t.Fatal("expected data quality")
	}
	if report.Metadata == nil {
		t.Fatal("expected metadata")
	}
	if report.Metadata.EngineVersion != engineVersion {
		t.Errorf("expected version %s, got %s", engineVersion, report.Metadata.EngineVersion)
	}
}

func TestSummaryComputation(t *testing.T) {
	rb := NewReportBuilder()
	flows := []*behavior.AnalyzedFlow{
		makeFlow("user1", "checkout", behavior.BehaviorSucceed, []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorSucceed}, 5),
		makeFlow("user2", "checkout", behavior.BehaviorAbandon, []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorAbandon}, 3),
		makeFlow("user1", "payment", behavior.BehaviorSucceed, []behavior.BehaviorType{behavior.BehaviorSucceed}, 4),
		makeFlow("user3", "search", behavior.BehaviorExplore, []behavior.BehaviorType{behavior.BehaviorExplore}, 1),
	}
	patterns := []*pattern.DetectedPattern{
		makePattern(pattern.PatternRetryStorm, "checkout", pattern.SeverityHigh, 5, 20, 0.25),
		makePattern(pattern.PatternEarlyDropoff, "payment", pattern.SeverityMedium, 3, 15, 0.2),
	}
	report := rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		AnalyzedFlows:    flows,
		DetectedPatterns: patterns,
		InputEventsCount: 100,
	})
	s := report.Summary
	if s.TotalFlowInstances != 4 {
		t.Errorf("expected 4 flow instances, got %d", s.TotalFlowInstances)
	}
	if s.TotalUsers != 3 {
		t.Errorf("expected 3 users, got %d", s.TotalUsers)
	}
	if s.UniqueFlowTypes != 3 {
		t.Errorf("expected 3 flow types, got %d", s.UniqueFlowTypes)
	}
	if s.PatternsDetected != 2 {
		t.Errorf("expected 2 patterns, got %d", s.PatternsDetected)
	}
	if s.CriticalPatterns != 1 {
		t.Errorf("expected 1 critical, got %d", s.CriticalPatterns)
	}
	if s.OverallHealth != "critical" {
		t.Errorf("expected critical health, got %s", s.OverallHealth)
	}
}

func TestFlowMetrics(t *testing.T) {
	rb := NewReportBuilder()
	flows := []*behavior.AnalyzedFlow{
		makeFlow("user1", "checkout", behavior.BehaviorSucceed, []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorSucceed}, 5),
		makeFlow("user2", "checkout", behavior.BehaviorAbandon, []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorAbandon}, 3),
		makeFlow("user3", "checkout", behavior.BehaviorSucceed, []behavior.BehaviorType{behavior.BehaviorSucceed}, 4),
	}
	report := rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		AnalyzedFlows:    flows,
		InputEventsCount: 50,
	})
	fm, ok := report.Flows["checkout"]
	if !ok {
		t.Fatal("expected checkout flow metrics")
	}
	if fm.Instances != 3 {
		t.Errorf("expected 3 instances, got %d", fm.Instances)
	}
	if fm.UniqueUsers != 3 {
		t.Errorf("expected 3 users, got %d", fm.UniqueUsers)
	}
	expectedRate := 2.0 / 3.0
	if fm.CompletionRate < expectedRate-0.01 || fm.CompletionRate > expectedRate+0.01 {
		t.Errorf("expected completion rate ~%.2f, got %.2f", expectedRate, fm.CompletionRate)
	}
	if fm.AvgEventsPerFlow < 3.99 || fm.AvgEventsPerFlow > 4.01 {
		t.Errorf("expected avg events ~4.0, got %.2f", fm.AvgEventsPerFlow)
	}
	if fm.BehaviorDistribution["succeed"] != 2 {
		t.Errorf("expected 2 succeed behaviors, got %d", fm.BehaviorDistribution["succeed"])
	}
	if fm.PrimaryOutcome != "succeed" {
		t.Errorf("expected primary outcome succeed, got %s", fm.PrimaryOutcome)
	}
}

func TestFlowMetricsWithContext(t *testing.T) {
	rb := NewReportBuilder()
	ctx1 := &canonical.EventContext{
		Dimensions: map[string]string{"device": "mobile", "platform": "ios"},
	}
	ctx2 := &canonical.EventContext{
		Dimensions: map[string]string{"device": "desktop", "platform": "web"},
	}
	flows := []*behavior.AnalyzedFlow{
		makeFlowWithContext("user1", "checkout", behavior.BehaviorSucceed, ctx1),
		makeFlowWithContext("user2", "checkout", behavior.BehaviorSucceed, ctx1),
		makeFlowWithContext("user3", "checkout", behavior.BehaviorAbandon, ctx2),
	}
	report := rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		AnalyzedFlows:    flows,
		InputEventsCount: 30,
	})
	fm := report.Flows["checkout"]
	if fm.Context == nil {
		t.Fatal("expected context aggregation")
	}
	if fm.Context["device"]["mobile"] != 2 {
		t.Errorf("expected 2 mobile, got %d", fm.Context["device"]["mobile"])
	}
	if fm.Context["device"]["desktop"] != 1 {
		t.Errorf("expected 1 desktop, got %d", fm.Context["device"]["desktop"])
	}
}

func TestFunnelMetrics(t *testing.T) {
	rb := NewReportBuilder()
	ctx := &behavior.AnalysisContext{
		Scope: behavior.ScopeFlowCohort,
		FunnelDefinitions: map[string][]string{
			"purchase": {"checkout", "payment", "confirmation"},
		},
	}
	flows := []*behavior.AnalyzedFlow{
		makeFlow("user1", "checkout", behavior.BehaviorSucceed, nil, 3),
		makeFlow("user2", "checkout", behavior.BehaviorSucceed, nil, 3),
		makeFlow("user3", "checkout", behavior.BehaviorSucceed, nil, 3),
		makeFlow("user4", "checkout", behavior.BehaviorAbandon, nil, 3),
		makeFlow("user5", "checkout", behavior.BehaviorAbandon, nil, 3),
		makeFlow("user1", "payment", behavior.BehaviorSucceed, nil, 4),
		makeFlow("user2", "payment", behavior.BehaviorSucceed, nil, 4),
		makeFlow("user3", "payment", behavior.BehaviorAbandon, nil, 2),
		makeFlow("user1", "confirmation", behavior.BehaviorSucceed, nil, 2),
		makeFlow("user2", "confirmation", behavior.BehaviorSucceed, nil, 2),
	}
	report := rb.Build(&ReportInput{
		AnalysisContext:  ctx,
		AnalyzedFlows:    flows,
		InputEventsCount: 100,
	})
	funnel, ok := report.Funnels["purchase"]
	if !ok {
		t.Fatal("expected purchase funnel")
	}
	if funnel.TotalEntries != 5 {
		t.Errorf("expected 5 entries, got %d", funnel.TotalEntries)
	}
	if funnel.TotalCompletions != 2 {
		t.Errorf("expected 2 completions, got %d", funnel.TotalCompletions)
	}
	if funnel.OverallConversion < 0.39 || funnel.OverallConversion > 0.41 {
		t.Errorf("expected overall conversion ~0.4, got %.2f", funnel.OverallConversion)
	}
	if len(funnel.StepConversions) != 2 {
		t.Fatalf("expected 2 step conversions, got %d", len(funnel.StepConversions))
	}
	sc1 := funnel.StepConversions[0]
	if sc1.From != "checkout" || sc1.To != "payment" {
		t.Errorf("expected checkout->payment, got %s->%s", sc1.From, sc1.To)
	}
	if sc1.UsersFrom != 5 || sc1.UsersTo != 3 {
		t.Errorf("expected 5->3, got %d->%d", sc1.UsersFrom, sc1.UsersTo)
	}
	if sc1.Rate < 0.59 || sc1.Rate > 0.61 {
		t.Errorf("expected rate ~0.6, got %.2f", sc1.Rate)
	}
	if sc1.DropCount != 2 {
		t.Errorf("expected 2 drops, got %d", sc1.DropCount)
	}
}

func TestPatternInsights(t *testing.T) {
	rb := NewReportBuilder()
	patterns := []*pattern.DetectedPattern{
		makePattern(pattern.PatternEarlyDropoff, "checkout", pattern.SeverityLow, 2, 10, 0.2),
		makePattern(pattern.PatternRetryStorm, "payment", pattern.SeverityHigh, 8, 20, 0.4),
		makePattern(pattern.PatternSilentAbandonment, "search", pattern.SeverityMedium, 5, 15, 0.33),
	}
	changes := []*baseline.ChangeResult{
		makeChange("retry_storm", "payment", baseline.TrendIncreasing, 0.25, baseline.BaselineStatusSufficient),
	}
	report := rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		AnalyzedFlows:    []*behavior.AnalyzedFlow{},
		DetectedPatterns: patterns,
		ChangeResults:    changes,
		InputEventsCount: 50,
	})
	if len(report.Patterns) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(report.Patterns))
	}
	if report.Patterns[0].Type != "retry_storm" {
		t.Errorf("expected retry_storm first (high severity), got %s", report.Patterns[0].Type)
	}
	if report.Patterns[1].Type != "silent_abandonment" {
		t.Errorf("expected silent_abandonment second (medium), got %s", report.Patterns[1].Type)
	}
	if report.Patterns[2].Type != "early_dropoff" {
		t.Errorf("expected early_dropoff last (low), got %s", report.Patterns[2].Type)
	}
	rs := report.Patterns[0]
	if rs.Baseline == nil {
		t.Fatal("expected baseline info for retry_storm")
	}
	if !rs.Baseline.Available {
		t.Error("expected baseline to be available")
	}
	if rs.Baseline.Trend != "increasing" {
		t.Errorf("expected increasing trend, got %s", rs.Baseline.Trend)
	}
	ed := report.Patterns[2]
	if ed.Baseline != nil {
		t.Error("expected no baseline for early_dropoff")
	}
}

func TestPatternInsightsFunnelDropoff(t *testing.T) {
	rb := NewReportBuilder()
	patterns := []*pattern.DetectedPattern{
		{
			Pattern:       pattern.PatternFunnelDropoff,
			Flow:          "checkout_to_payment",
			AffectedUsers: 10,
			TotalFlows:    30,
			Severity:      pattern.SeverityHigh,
			Confidence:    pattern.ConfidenceHigh,
			Evidence: pattern.PatternEvidence{
				Ratio:          0.33,
				Description:    "Funnel dropoff detected",
				FunnelID:       "purchase",
				StepFrom:       "checkout",
				StepTo:         "payment",
				ConversionRate: 0.67,
			},
		},
	}
	report := rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		DetectedPatterns: patterns,
		InputEventsCount: 50,
	})
	pi := report.Patterns[0]
	if pi.FunnelInfo == nil {
		t.Fatal("expected funnel info for funnel_dropoff")
	}
	if pi.FunnelInfo.FunnelID != "purchase" {
		t.Errorf("expected funnel purchase, got %s", pi.FunnelInfo.FunnelID)
	}
	if pi.FunnelInfo.StepFrom != "checkout" {
		t.Errorf("expected step from checkout, got %s", pi.FunnelInfo.StepFrom)
	}
	if pi.FunnelInfo.ConversionRate < 0.66 || pi.FunnelInfo.ConversionRate > 0.68 {
		t.Errorf("expected conversion ~0.67, got %.2f", pi.FunnelInfo.ConversionRate)
	}
}

func TestEntityInsights(t *testing.T) {
	rb := NewReportBuilder()
	ctx := &behavior.AnalysisContext{
		Scope:      behavior.ScopeUserSession,
		EntityType: "user",
		FunnelDefinitions: map[string][]string{
			"purchase": {"checkout", "payment", "confirmation"},
		},
	}
	flows := []*behavior.AnalyzedFlow{
		makeFlow("user1", "checkout", behavior.BehaviorSucceed, []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorSucceed}, 5),
		makeFlow("user1", "payment", behavior.BehaviorSucceed, []behavior.BehaviorType{behavior.BehaviorSucceed}, 4),
		makeFlow("user1", "confirmation", behavior.BehaviorSucceed, []behavior.BehaviorType{behavior.BehaviorSucceed}, 2),
		makeFlow("user2", "checkout", behavior.BehaviorAbandon, []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorAbandon}, 3),
	}
	report := rb.Build(&ReportInput{
		AnalysisContext:  ctx,
		AnalyzedFlows:    flows,
		InputEventsCount: 50,
	})
	if report.EntityInsights == nil {
		t.Fatal("expected entity insights for user_session scope")
	}
	if len(report.EntityInsights) != 2 {
		t.Fatalf("expected 2 entity insights, got %d", len(report.EntityInsights))
	}
	user1 := report.EntityInsights[0]
	if user1.EntityID != "user1" {
		t.Errorf("expected user1 first, got %s", user1.EntityID)
	}
	if user1.FlowsCount != 3 {
		t.Errorf("expected 3 flows for user1, got %d", user1.FlowsCount)
	}
	if user1.FurthestFunnelStep != "confirmation" {
		t.Errorf("expected confirmation as furthest step, got %s", user1.FurthestFunnelStep)
	}
	if !user1.FunnelComplete {
		t.Error("expected user1 to have completed funnel")
	}
	if user1.EntityType != "user" {
		t.Errorf("expected entity type user, got %s", user1.EntityType)
	}
	user2 := report.EntityInsights[1]
	if user2.FlowsCount != 1 {
		t.Errorf("expected 1 flow for user2, got %d", user2.FlowsCount)
	}
	if user2.FurthestFunnelStep != "checkout" {
		t.Errorf("expected checkout as furthest step, got %s", user2.FurthestFunnelStep)
	}
	if user2.FunnelComplete {
		t.Error("expected user2 to NOT have completed funnel")
	}
}

func TestEntityInsightsNotForCohort(t *testing.T) {
	rb := NewReportBuilder()
	report := rb.Build(&ReportInput{
		AnalysisContext:  &behavior.AnalysisContext{Scope: behavior.ScopeFlowCohort},
		AnalyzedFlows:    []*behavior.AnalyzedFlow{makeFlow("u1", "checkout", behavior.BehaviorSucceed, nil, 3)},
		InputEventsCount: 10,
	})
	if report.EntityInsights != nil {
		t.Error("expected no entity insights for flow_cohort scope")
	}
}

func TestDataQuality(t *testing.T) {
	rb := NewReportBuilder()
	rawEvents := []map[string]interface{}{
		{"id": "1", "ts": "2024-01-01", "event": "checkout_view", "user_id": "u1"},
		{"id": "2", "ts": "2024-01-01", "event": "checkout_click", "user_id": "u2", "session_id": "s1"},
		{"id": "3", "ts": "2024-01-01", "event_name": "payment_view"},
	}
	report := rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		AnalyzedFlows:    []*behavior.AnalyzedFlow{makeFlow("u1", "checkout", behavior.BehaviorSucceed, nil, 3)},
		InputEventsCount: 3,
		RawEvents:        rawEvents,
		Warnings:         []string{"Missing session_id on some events"},
	})
	dq := report.DataQuality
	if dq.EventsReceived != 3 {
		t.Errorf("expected 3 events received, got %d", dq.EventsReceived)
	}
	if dq.FlowInstancesCreated != 1 {
		t.Errorf("expected 1 flow instance, got %d", dq.FlowInstancesCreated)
	}
	if dq.FieldCoverage["id"] < 0.99 {
		t.Errorf("expected id coverage 1.0, got %.2f", dq.FieldCoverage["id"])
	}
	if dq.FieldCoverage["user_id"] < 0.65 || dq.FieldCoverage["user_id"] > 0.68 {
		t.Errorf("expected user_id coverage ~0.67, got %.2f", dq.FieldCoverage["user_id"])
	}
	if len(dq.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(dq.Warnings))
	}
}

func TestAIAnalysisPopulated(t *testing.T) {
	rb := NewReportBuilder()
	report := rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		InputEventsCount: 10,
		AIEnabled:        true,
		AIAnalysis: &ai.AnalysisResponse{
			Summary:        "Users are experiencing checkout friction",
			Details:        []string{"High retry rate in payment"},
			Hypotheses:     []string{"Payment gateway timeout"},
			ConfidenceNote: "Medium confidence based on 100 events",
		},
	})
	if report.AIAnalysis == nil {
		t.Fatal("expected AI analysis")
	}
	if !report.AIAnalysis.Enabled {
		t.Error("expected AI enabled")
	}
	if report.AIAnalysis.Summary != "Users are experiencing checkout friction" {
		t.Errorf("unexpected summary: %s", report.AIAnalysis.Summary)
	}
}

func TestExtractFromAIResult(t *testing.T) {
	flows := []*behavior.AnalyzedFlow{makeFlow("u1", "checkout", behavior.BehaviorSucceed, nil, 3)}
	patterns := []*pattern.DetectedPattern{makePattern(pattern.PatternRetryStorm, "checkout", pattern.SeverityHigh, 5, 10, 0.5)}
	changes := []*baseline.ChangeResult{makeChange("retry_storm", "checkout", baseline.TrendIncreasing, 0.2, baseline.BaselineStatusSufficient)}
	aiResult := &ai.AIResult{
		AnalyzedFlows:    flows,
		DetectedPatterns: patterns,
		ChangeResults:    changes,
		AIAnalysis:       &ai.AnalysisResponse{Summary: "test"},
		AIEnabled:        true,
	}
	af, dp, cr, aa, enabled := ExtractFromPipelineOutput(aiResult)
	if len(af) != 1 {
		t.Errorf("expected 1 flow, got %d", len(af))
	}
	if len(dp) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(dp))
	}
	if len(cr) != 1 {
		t.Errorf("expected 1 change result, got %d", len(cr))
	}
	if aa == nil {
		t.Error("expected AI analysis")
	}
	if !enabled {
		t.Error("expected AI enabled")
	}
}

func TestExtractFromPatternResult(t *testing.T) {
	flows := []*behavior.AnalyzedFlow{makeFlow("u1", "checkout", behavior.BehaviorSucceed, nil, 3)}
	patterns := []*pattern.DetectedPattern{makePattern(pattern.PatternEarlyDropoff, "checkout", pattern.SeverityLow, 2, 10, 0.2)}
	result := &pattern.PatternResult{AnalyzedFlows: flows, DetectedPatterns: patterns}
	af, dp, cr, _, enabled := ExtractFromPipelineOutput(result)
	if len(af) != 1 {
		t.Errorf("expected 1 flow, got %d", len(af))
	}
	if len(dp) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(dp))
	}
	if cr != nil {
		t.Error("expected no change results")
	}
	if enabled {
		t.Error("expected AI not enabled")
	}
}

func TestHealthLevels(t *testing.T) {
	rb := NewReportBuilder()
	report := rb.Build(&ReportInput{AnalysisContext: behavior.DefaultAnalysisContext(), InputEventsCount: 10})
	if report.Summary.OverallHealth != "healthy" {
		t.Errorf("expected healthy, got %s", report.Summary.OverallHealth)
	}
	report = rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		DetectedPatterns: []*pattern.DetectedPattern{makePattern(pattern.PatternEarlyDropoff, "checkout", pattern.SeverityMedium, 3, 10, 0.3)},
		InputEventsCount: 10,
	})
	if report.Summary.OverallHealth != "warning" {
		t.Errorf("expected warning, got %s", report.Summary.OverallHealth)
	}
	report = rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		DetectedPatterns: []*pattern.DetectedPattern{makePattern(pattern.PatternRetryStorm, "payment", pattern.SeverityHigh, 8, 20, 0.4)},
		InputEventsCount: 10,
	})
	if report.Summary.OverallHealth != "critical" {
		t.Errorf("expected critical, got %s", report.Summary.OverallHealth)
	}
}

func TestMetadata(t *testing.T) {
	rb := NewReportBuilder()
	report := rb.Build(&ReportInput{
		AnalysisContext:  &behavior.AnalysisContext{Scope: behavior.ScopeUserSession, EntityID: "user_123", EntityType: "user"},
		PipelineLayers:   []string{"event_adapter", "session_flow", "behavior", "pattern", "baseline"},
		InputEventsCount: 10,
	})
	m := report.Metadata
	if m.AnalysisScope != "user_session" {
		t.Errorf("expected user_session scope, got %s", m.AnalysisScope)
	}
	if m.EntityID != "user_123" {
		t.Errorf("expected entity user_123, got %s", m.EntityID)
	}
	if len(m.PipelineLayers) != 5 {
		t.Errorf("expected 5 layers, got %d", len(m.PipelineLayers))
	}
	if m.ProcessedAt == "" {
		t.Error("expected processed_at timestamp")
	}
}

func TestNilContextDefaultsToRawBatch(t *testing.T) {
	rb := NewReportBuilder()
	report := rb.Build(&ReportInput{AnalysisContext: nil, InputEventsCount: 10})
	if report.Summary.AnalysisScope != "raw_batch" {
		t.Errorf("expected raw_batch scope, got %s", report.Summary.AnalysisScope)
	}
}

func TestFlowIntentInMetrics(t *testing.T) {
	rb := NewReportBuilder()
	flow := makeFlow("u1", "menu", behavior.BehaviorExplore, []behavior.BehaviorType{behavior.BehaviorExplore}, 1)
	flow.FlowIntent = behavior.FlowIntentBrowse
	report := rb.Build(&ReportInput{
		AnalysisContext:  behavior.DefaultAnalysisContext(),
		AnalyzedFlows:    []*behavior.AnalyzedFlow{flow},
		InputEventsCount: 1,
	})
	fm := report.Flows["menu"]
	if fm.Intent != "browse" {
		t.Errorf("expected browse intent, got %s", fm.Intent)
	}
}
