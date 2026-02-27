package pattern

import (
	"testing"

	"github.com/velum/internal/layers/behavior"
)

func TestDetectRetryStorm(t *testing.T) {
	d := New()

	// Create flows where 50% have retry behavior (above 30% threshold)
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
	}

	result, _ := d.Process(flows)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternRetryStorm)
	if found == nil {
		t.Error("Expected retry_storm pattern to be detected")
	} else {
		if found.Flow != "payment" {
			t.Errorf("Flow = %v, want payment", found.Flow)
		}
		if found.Evidence.MatchingFlows != 2 {
			t.Errorf("MatchingFlows = %v, want 2", found.Evidence.MatchingFlows)
		}
	}
}

func TestDetectSilentAbandonment(t *testing.T) {
	d := New()

	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorAbandon}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorAbandon}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorAbandon, behavior.BehaviorRetry}}, // Not silent
		{FlowInstanceID: "4", UserID: "user_d", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
	}

	result, _ := d.Process(flows)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternSilentAbandonment)
	if found == nil {
		t.Error("Expected silent_abandonment pattern to be detected")
	} else {
		if found.Evidence.MatchingFlows != 2 {
			t.Errorf("MatchingFlows = %v, want 2 (excluding retry case)", found.Evidence.MatchingFlows)
		}
	}
}

func TestDetectEarlyDropoff(t *testing.T) {
	d := New()

	// 3 out of 5 flows (60%) are early dropoffs - above 40% threshold
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore, behavior.BehaviorAbandon}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorAbandon}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorSucceed}},
		{FlowInstanceID: "5", UserID: "user_e", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt}},
	}

	result, _ := d.Process(flows)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternEarlyDropoff)
	if found == nil {
		t.Error("Expected early_dropoff pattern to be detected")
	} else {
		if found.Evidence.MatchingFlows != 3 {
			t.Errorf("MatchingFlows = %v, want 3", found.Evidence.MatchingFlows)
		}
	}
}

func TestDetectMaskedFailure(t *testing.T) {
	d := New()

	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry, behavior.BehaviorSucceed}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry, behavior.BehaviorSucceed}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},                         // No retry, not masked
		{FlowInstanceID: "4", UserID: "user_d", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry, behavior.BehaviorAbandon}}, // Retry but failed
	}

	result, _ := d.Process(flows)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternMaskedFailure)
	if found == nil {
		t.Error("Expected masked_failure pattern to be detected")
	} else {
		if found.Evidence.MatchingFlows != 2 {
			t.Errorf("MatchingFlows = %v, want 2", found.Evidence.MatchingFlows)
		}
	}
}

func TestDetectConfusionLoop(t *testing.T) {
	d := New()

	// 2 out of 4 flows (50%) have hesitation - above 30% threshold
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorHesitate}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorHesitate}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt}},
	}

	result, _ := d.Process(flows)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternConfusionLoop)
	if found == nil {
		t.Error("Expected confusion_loop pattern to be detected")
	}
}

func TestNoPatternBelowThreshold(t *testing.T) {
	d := New()

	// Only 1 retry out of 4 (25%) - below 30% threshold
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
	}

	result, _ := d.Process(flows)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternRetryStorm)
	if found != nil {
		t.Error("Should not detect retry_storm when below threshold")
	}
}

func TestMinSampleSize(t *testing.T) {
	config := DefaultConfig()
	config.MinSampleSize = 5
	d := NewWithConfig(config)

	// Only 3 flows - below min sample size of 5
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry}},
	}

	result, _ := d.Process(flows)
	patternResult := result.(*PatternResult)

	if len(patternResult.DetectedPatterns) > 0 {
		t.Error("Should not detect patterns when below min sample size")
	}
}

func TestSeverityComputation(t *testing.T) {
	d := New()

	// Test high severity (100+ users)
	highGroup := make([]*behavior.AnalyzedFlow, 100)
	for i := 0; i < 100; i++ {
		highGroup[i] = &behavior.AnalyzedFlow{UserID: string(rune(i))}
	}
	if d.computeSeverity(d.countUniqueUsers(highGroup)) != SeverityHigh {
		t.Error("Expected high severity for 100+ users")
	}

	// Test medium severity
	if d.computeSeverity(50) != SeverityMedium {
		t.Error("Expected medium severity for 50 users")
	}

	// Test low severity
	if d.computeSeverity(10) != SeverityLow {
		t.Error("Expected low severity for 10 users")
	}
}

func TestConfidenceComputation(t *testing.T) {
	d := New()

	// All medium confidence flows -> high pattern confidence
	allMedium := []*behavior.AnalyzedFlow{
		{Confidence: "medium"},
		{Confidence: "medium"},
		{Confidence: "medium"},
	}
	if d.computeConfidence(allMedium) != ConfidenceHigh {
		t.Error("Expected high confidence when all flows have medium confidence")
	}

	// Mixed confidence -> medium pattern confidence
	mixed := []*behavior.AnalyzedFlow{
		{Confidence: "medium"},
		{Confidence: "low"},
		{Confidence: "medium"},
	}
	if d.computeConfidence(mixed) != ConfidenceMedium {
		t.Error("Expected medium confidence for mixed flows")
	}

	// All low -> low pattern confidence
	allLow := []*behavior.AnalyzedFlow{
		{Confidence: "low"},
		{Confidence: "low"},
		{Confidence: "low"},
	}
	if d.computeConfidence(allLow) != ConfidenceLow {
		t.Error("Expected low confidence when all flows have low confidence")
	}
}

func findPattern(patterns []*DetectedPattern, patternType PatternType) *DetectedPattern {
	for _, p := range patterns {
		if p.Pattern == patternType {
			return p
		}
	}
	return nil
}

// ==================== Context-Aware Pattern Tests ====================

// Problem 1: Single-event flows should NOT trigger early_dropoff
func TestEarlyDropoffRespectsMinEvents(t *testing.T) {
	d := New()

	// All flows have only 1 event — below min threshold of 2
	// These should NOT trigger early_dropoff
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "restaurant_list", EventCount: 1, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "restaurant_list", EventCount: 1, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "restaurant_list", EventCount: 1, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "restaurant_list", EventCount: 1, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
	}

	ctx := behavior.DefaultAnalysisContext()
	result, _ := d.ProcessWithContext(flows, ctx)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternEarlyDropoff)
	if found != nil {
		t.Error("Should NOT detect early_dropoff for single-event flows (below min threshold)")
	}
}

// Problem 1: Multi-event flows SHOULD still trigger early_dropoff
func TestEarlyDropoffStillWorksForMultiEventFlows(t *testing.T) {
	d := New()

	// 3 out of 4 flows have 3+ events but only explore — still should trigger
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore, behavior.BehaviorAbandon}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "checkout", EventCount: 4, Behaviors: []behavior.BehaviorType{behavior.BehaviorAbandon}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "checkout", EventCount: 5, Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorSucceed}},
	}

	ctx := behavior.DefaultAnalysisContext()
	result, _ := d.ProcessWithContext(flows, ctx)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternEarlyDropoff)
	if found == nil {
		t.Error("Expected early_dropoff for multi-event flows with explore/abandon")
	}
}

// Problem 3: Browse-intent flows should NOT trigger early_dropoff
func TestBrowseFlowsExcludedFromDropoff(t *testing.T) {
	d := New()

	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "restaurant_list", EventCount: 3, FlowIntent: behavior.FlowIntentBrowse, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "restaurant_list", EventCount: 3, FlowIntent: behavior.FlowIntentBrowse, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "restaurant_list", EventCount: 3, FlowIntent: behavior.FlowIntentBrowse, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "restaurant_list", EventCount: 3, FlowIntent: behavior.FlowIntentBrowse, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
	}

	ctx := &behavior.AnalysisContext{
		Scope: behavior.ScopeFlowCohort,
		FlowConfigs: map[string]*behavior.FlowConfig{
			"restaurant_list": {
				Name:   "restaurant_list",
				Intent: behavior.FlowIntentBrowse,
			},
		},
	}

	result, _ := d.ProcessWithContext(flows, ctx)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternEarlyDropoff)
	if found != nil {
		t.Error("Should NOT detect early_dropoff for browse-intent flows")
	}
}

// Problem 4: Flows with BehaviorProgress should NOT trigger early_dropoff
func TestProgressFlowsExcludedFromDropoff(t *testing.T) {
	d := New()

	// All checkout flows have progress (user advanced to payment)
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore, behavior.BehaviorProgress}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore, behavior.BehaviorProgress}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore, behavior.BehaviorProgress}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorSucceed}},
	}

	ctx := behavior.DefaultAnalysisContext()
	result, _ := d.ProcessWithContext(flows, ctx)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternEarlyDropoff)
	if found != nil {
		t.Error("Should NOT detect early_dropoff when users progressed to next funnel step")
	}
}

// Problem 2: Funnel dropoff detection between flow steps
func TestFunnelDropoffDetection(t *testing.T) {
	d := New()

	// 5 users at checkout, 2 at payment, 1 at order
	flows := []*behavior.AnalyzedFlow{
		// Checkout step: 5 users
		{FlowInstanceID: "c1", UserID: "user_a", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt}},
		{FlowInstanceID: "c2", UserID: "user_b", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt}},
		{FlowInstanceID: "c3", UserID: "user_c", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt}},
		{FlowInstanceID: "c4", UserID: "user_d", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt}},
		{FlowInstanceID: "c5", UserID: "user_e", Flow: "checkout", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt}},
		// Payment step: only 2 users made it
		{FlowInstanceID: "p1", UserID: "user_a", Flow: "payment", EventCount: 4, Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorSucceed}},
		{FlowInstanceID: "p2", UserID: "user_b", Flow: "payment", EventCount: 4, Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt, behavior.BehaviorSucceed}},
		// Order step: only 1 user
		{FlowInstanceID: "o1", UserID: "user_a", Flow: "order", EventCount: 2, Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
	}

	ctx := &behavior.AnalysisContext{
		Scope: behavior.ScopeFlowCohort,
		FunnelDefinitions: map[string][]string{
			"purchase": {"checkout", "payment", "order"},
		},
	}

	result, _ := d.ProcessWithContext(flows, ctx)
	patternResult := result.(*PatternResult)

	// Should detect funnel dropoff between checkout → payment (60% drop)
	var funnelPatterns []*DetectedPattern
	for _, p := range patternResult.DetectedPatterns {
		if p.Pattern == PatternFunnelDropoff {
			funnelPatterns = append(funnelPatterns, p)
		}
	}

	if len(funnelPatterns) == 0 {
		t.Fatal("Expected at least one funnel_dropoff pattern")
	}

	// Find the checkout → payment drop
	var checkoutDrop *DetectedPattern
	for _, p := range funnelPatterns {
		if p.Evidence.StepFrom == "checkout" && p.Evidence.StepTo == "payment" {
			checkoutDrop = p
		}
	}

	if checkoutDrop == nil {
		t.Fatal("Expected funnel_dropoff between checkout → payment")
	}

	if checkoutDrop.AffectedUsers != 3 {
		t.Errorf("AffectedUsers = %d, want 3 (5 at checkout, 2 at payment)", checkoutDrop.AffectedUsers)
	}

	if checkoutDrop.Evidence.ConversionRate < 0.39 || checkoutDrop.Evidence.ConversionRate > 0.41 {
		t.Errorf("ConversionRate = %f, want ~0.4", checkoutDrop.Evidence.ConversionRate)
	}

	if checkoutDrop.Evidence.FunnelID != "purchase" {
		t.Errorf("FunnelID = %s, want purchase", checkoutDrop.Evidence.FunnelID)
	}
}

// Test that funnel dropoff is not detected when not enough users
func TestFunnelDropoffBelowMinSample(t *testing.T) {
	config := DefaultConfig()
	config.MinSampleSize = 5
	d := NewWithConfig(config)

	// Only 2 users at checkout — below min sample size of 5
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "c1", UserID: "user_a", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt}},
		{FlowInstanceID: "c2", UserID: "user_b", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorAttempt}},
	}

	ctx := &behavior.AnalysisContext{
		Scope: behavior.ScopeFlowCohort,
		FunnelDefinitions: map[string][]string{
			"purchase": {"checkout", "payment"},
		},
	}

	result, _ := d.ProcessWithContext(flows, ctx)
	patternResult := result.(*PatternResult)

	for _, p := range patternResult.DetectedPatterns {
		if p.Pattern == PatternFunnelDropoff {
			t.Error("Should NOT detect funnel_dropoff when below min sample size")
		}
	}
}

// Problem 4: Silent abandonment should exclude progressed flows
func TestSilentAbandonmentExcludesProgressedFlows(t *testing.T) {
	d := New()

	// All checkout flows "abandoned" but actually progressed to payment
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorProgress}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorProgress}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorProgress}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "checkout", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
	}

	ctx := behavior.DefaultAnalysisContext()
	result, _ := d.ProcessWithContext(flows, ctx)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternSilentAbandonment)
	if found != nil {
		t.Error("Should NOT detect silent_abandonment for flows where user progressed")
	}
}

// Test ProcessWithContext backward compat — nil context uses Process
func TestProcessWithContextNilContext(t *testing.T) {
	d := New()

	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorRetry}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
	}

	result, _ := d.ProcessWithContext(flows, nil)
	patternResult := result.(*PatternResult)

	// Should still detect retry_storm (50% > 30% threshold)
	found := findPattern(patternResult.DetectedPatterns, PatternRetryStorm)
	if found == nil {
		t.Error("Expected retry_storm even with nil context (backward compat)")
	}
}

// Test custom MinEventsForDropoff per-flow via AnalysisContext
func TestPerFlowMinEventsOverride(t *testing.T) {
	d := New()

	// Flows with 3 events each. Global min is 2, but flow-specific is 5.
	flows := []*behavior.AnalyzedFlow{
		{FlowInstanceID: "1", UserID: "user_a", Flow: "registration", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "2", UserID: "user_b", Flow: "registration", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "3", UserID: "user_c", Flow: "registration", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorExplore}},
		{FlowInstanceID: "4", UserID: "user_d", Flow: "registration", EventCount: 3, Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}},
	}

	ctx := &behavior.AnalysisContext{
		Scope: behavior.ScopeFlowCohort,
		FlowConfigs: map[string]*behavior.FlowConfig{
			"registration": {
				Name:                "registration",
				Intent:              behavior.FlowIntentTransact,
				MinEventsForDropoff: 5, // requires 5 events before counting as dropoff
			},
		},
	}

	result, _ := d.ProcessWithContext(flows, ctx)
	patternResult := result.(*PatternResult)

	found := findPattern(patternResult.DetectedPatterns, PatternEarlyDropoff)
	if found != nil {
		t.Error("Should NOT detect early_dropoff — flows have 3 events but min is 5 for this flow")
	}
}
