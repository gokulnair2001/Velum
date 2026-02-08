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
		{FlowInstanceID: "3", UserID: "user_c", Flow: "payment", Behaviors: []behavior.BehaviorType{behavior.BehaviorSucceed}}, // No retry, not masked
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
