package behavior

import (
	"testing"
	"time"

	"github.com/velum/internal/layers/sessionflow"
)

func TestBehaviorExplore(t *testing.T) {
	a := New()

	flow := &sessionflow.FlowInstance{
		FlowInstanceID: "flow_1",
		UserID:         "user_a",
		Flow:           "checkout",
		Events: []sessionflow.FlowEvent{
			{Status: "view", RawEventName: "checkout_view"},
			{Status: "view", RawEventName: "product_view"},
		},
		IsComplete: false,
	}

	result := a.analyzeFlow(flow)

	if result.Outcome != BehaviorExplore {
		t.Errorf("Outcome = %v, want explore", result.Outcome)
	}
	if !containsBehavior(result.Behaviors, BehaviorExplore) {
		t.Error("Expected explore behavior")
	}
}

func TestBehaviorAttempt(t *testing.T) {
	a := New()

	flow := &sessionflow.FlowInstance{
		FlowInstanceID: "flow_1",
		UserID:         "user_a",
		Flow:           "payment",
		Events: []sessionflow.FlowEvent{
			{Status: "view", RawEventName: "checkout_view"},
			{Status: "click", RawEventName: "payment_click"},
		},
		IsComplete: false,
	}

	result := a.analyzeFlow(flow)

	if !containsBehavior(result.Behaviors, BehaviorAttempt) {
		t.Error("Expected attempt behavior")
	}
}

func TestBehaviorRetry(t *testing.T) {
	a := New()

	flow := &sessionflow.FlowInstance{
		FlowInstanceID: "flow_1",
		UserID:         "user_a",
		Flow:           "payment",
		Events: []sessionflow.FlowEvent{
			{Status: "view", RawEventName: "checkout_view"},
			{Status: "click", RawEventName: "payment_click"},
			{Status: "error", RawEventName: "payment_error"},
			{Status: "click", RawEventName: "payment_click"},
		},
		IsComplete: false,
	}

	result := a.analyzeFlow(flow)

	if !containsBehavior(result.Behaviors, BehaviorRetry) {
		t.Error("Expected retry behavior")
	}
	if !containsBehavior(result.Behaviors, BehaviorAttempt) {
		t.Error("Expected attempt behavior")
	}
}

func TestBehaviorSucceed(t *testing.T) {
	a := New()

	flow := &sessionflow.FlowInstance{
		FlowInstanceID: "flow_1",
		UserID:         "user_a",
		Flow:           "payment",
		Events: []sessionflow.FlowEvent{
			{Status: "view", RawEventName: "checkout_view"},
			{Status: "click", RawEventName: "payment_click"},
			{Status: "success", RawEventName: "payment_success"},
		},
		IsComplete: true,
	}

	result := a.analyzeFlow(flow)

	if result.Outcome != BehaviorSucceed {
		t.Errorf("Outcome = %v, want succeed", result.Outcome)
	}
	if !containsBehavior(result.Behaviors, BehaviorSucceed) {
		t.Error("Expected succeed behavior")
	}
}

func TestBehaviorAbandon(t *testing.T) {
	a := New()

	flow := &sessionflow.FlowInstance{
		FlowInstanceID: "flow_1",
		UserID:         "user_a",
		Flow:           "checkout",
		Events: []sessionflow.FlowEvent{
			{Status: "view", RawEventName: "checkout_view"},
			{Status: "click", RawEventName: "payment_click"},
			{Status: "exit", RawEventName: "checkout_exit"},
		},
		IsComplete: false,
	}

	result := a.analyzeFlow(flow)

	if !containsBehavior(result.Behaviors, BehaviorAbandon) {
		t.Error("Expected abandon behavior")
	}
}

func TestBehaviorHesitate(t *testing.T) {
	a := New()

	flow := &sessionflow.FlowInstance{
		FlowInstanceID: "flow_1",
		UserID:         "user_a",
		Flow:           "payment",
		Events: []sessionflow.FlowEvent{
			{Status: "view", RawEventName: "checkout_view"},
			{Status: "click", RawEventName: "payment_click"},
			{Status: "view", RawEventName: "checkout_view"}, // Back to view after click
		},
		IsComplete: false,
	}

	result := a.analyzeFlow(flow)

	if !containsBehavior(result.Behaviors, BehaviorHesitate) {
		t.Error("Expected hesitate behavior")
	}
}

func TestBehaviorPriority(t *testing.T) {
	a := New()

	// Flow with retry then success - succeed should be primary
	flow := &sessionflow.FlowInstance{
		FlowInstanceID: "flow_1",
		UserID:         "user_a",
		Flow:           "payment",
		Events: []sessionflow.FlowEvent{
			{Status: "view", RawEventName: "checkout_view"},
			{Status: "click", RawEventName: "payment_click"},
			{Status: "error", RawEventName: "payment_error"},
			{Status: "click", RawEventName: "payment_click"},
			{Status: "success", RawEventName: "payment_success"},
		},
		IsComplete: true,
	}

	result := a.analyzeFlow(flow)

	if result.Outcome != BehaviorSucceed {
		t.Errorf("Outcome = %v, want succeed (highest priority)", result.Outcome)
	}

	// Should have multiple behaviors
	if !containsBehavior(result.Behaviors, BehaviorSucceed) {
		t.Error("Expected succeed behavior")
	}
	if !containsBehavior(result.Behaviors, BehaviorRetry) {
		t.Error("Expected retry behavior")
	}
	if !containsBehavior(result.Behaviors, BehaviorAttempt) {
		t.Error("Expected attempt behavior")
	}
}

func TestProcessFromSessionFlow(t *testing.T) {
	a := New()

	flows := []*sessionflow.FlowInstance{
		{
			FlowInstanceID: "flow_1",
			UserID:         "user_a",
			Flow:           "payment",
			ContextType:    "explicit_session",
			Confidence:     "medium",
			Events: []sessionflow.FlowEvent{
				{Timestamp: time.Now(), Status: "view", RawEventName: "checkout_view"},
				{Timestamp: time.Now(), Status: "click", RawEventName: "payment_click"},
				{Timestamp: time.Now(), Status: "success", RawEventName: "payment_success"},
			},
			IsComplete: true,
		},
	}

	result, err := a.Process(flows)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	analyzed, ok := result.([]*AnalyzedFlow)
	if !ok {
		t.Fatalf("Expected []*AnalyzedFlow, got %T", result)
	}

	if len(analyzed) != 1 {
		t.Fatalf("Expected 1 analyzed flow, got %d", len(analyzed))
	}

	if analyzed[0].Outcome != BehaviorSucceed {
		t.Errorf("Outcome = %v, want succeed", analyzed[0].Outcome)
	}

	// Original fields should be preserved
	if analyzed[0].ContextType != "explicit_session" {
		t.Errorf("ContextType = %v, want explicit_session", analyzed[0].ContextType)
	}
}

func containsBehavior(behaviors []BehaviorType, target BehaviorType) bool {
	for _, b := range behaviors {
		if b == target {
			return true
		}
	}
	return false
}

// ==================== Context-Aware Tests ====================

func TestFlowIntentFromConfig(t *testing.T) {
	a := New()

	flows := []*sessionflow.FlowInstance{
		{
			FlowInstanceID: "flow_1",
			UserID:         "user_a",
			Flow:           "restaurant_list",
			Events: []sessionflow.FlowEvent{
				{Status: "view", RawEventName: "restaurant_list_view"},
			},
			IsComplete: false,
		},
		{
			FlowInstanceID: "flow_2",
			UserID:         "user_a",
			Flow:           "checkout",
			Events: []sessionflow.FlowEvent{
				{Status: "view", RawEventName: "checkout_view"},
			},
			IsComplete: false,
		},
	}

	ctx := &AnalysisContext{
		Scope: ScopeUserSession,
		FlowConfigs: map[string]*FlowConfig{
			"restaurant_list": {
				Name:   "restaurant_list",
				Intent: FlowIntentBrowse,
			},
			"checkout": {
				Name:   "checkout",
				Intent: FlowIntentTransact,
			},
		},
	}

	result, err := a.ProcessWithContext(flows, ctx)
	if err != nil {
		t.Fatalf("ProcessWithContext failed: %v", err)
	}

	analyzed := result.([]*AnalyzedFlow)
	if len(analyzed) != 2 {
		t.Fatalf("Expected 2 flows, got %d", len(analyzed))
	}

	// restaurant_list should be browse
	if analyzed[0].FlowIntent != FlowIntentBrowse {
		t.Errorf("restaurant_list FlowIntent = %v, want browse", analyzed[0].FlowIntent)
	}
	// checkout should be transact
	if analyzed[1].FlowIntent != FlowIntentTransact {
		t.Errorf("checkout FlowIntent = %v, want transact", analyzed[1].FlowIntent)
	}
}

func TestFlowIntentDefaultsToUnknown(t *testing.T) {
	a := New()

	flows := []*sessionflow.FlowInstance{
		{
			FlowInstanceID: "flow_1",
			UserID:         "user_a",
			Flow:           "unknown_flow",
			Events: []sessionflow.FlowEvent{
				{Status: "view", RawEventName: "unknown_view"},
			},
		},
	}

	ctx := &AnalysisContext{
		Scope: ScopeRawBatch,
	}

	result, err := a.ProcessWithContext(flows, ctx)
	if err != nil {
		t.Fatalf("ProcessWithContext failed: %v", err)
	}

	analyzed := result.([]*AnalyzedFlow)
	if analyzed[0].FlowIntent != FlowIntentUnknown {
		t.Errorf("FlowIntent = %v, want unknown", analyzed[0].FlowIntent)
	}
}

func TestFunnelProgressionDetection(t *testing.T) {
	a := New()

	now := time.Now()

	flows := []*sessionflow.FlowInstance{
		{
			FlowInstanceID: "flow_1",
			UserID:         "user_a",
			Flow:           "checkout",
			StartTime:      now,
			Events: []sessionflow.FlowEvent{
				{Timestamp: now, Status: "view", RawEventName: "checkout_view"},
				{Timestamp: now.Add(1 * time.Minute), Status: "click", RawEventName: "checkout_click"},
			},
			IsComplete: false,
		},
		{
			FlowInstanceID: "flow_2",
			UserID:         "user_a",
			Flow:           "payment",
			StartTime:      now.Add(2 * time.Minute),
			Events: []sessionflow.FlowEvent{
				{Timestamp: now.Add(2 * time.Minute), Status: "view", RawEventName: "payment_view"},
				{Timestamp: now.Add(3 * time.Minute), Status: "click", RawEventName: "payment_click"},
				{Timestamp: now.Add(4 * time.Minute), Status: "success", RawEventName: "payment_success"},
			},
			IsComplete: true,
		},
	}

	ctx := &AnalysisContext{
		Scope: ScopeUserSession,
		FunnelDefinitions: map[string][]string{
			"purchase_funnel": {"checkout", "payment", "order"},
		},
	}

	result, err := a.ProcessWithContext(flows, ctx)
	if err != nil {
		t.Fatalf("ProcessWithContext failed: %v", err)
	}

	analyzed := result.([]*AnalyzedFlow)

	// checkout flow should have BehaviorProgress (user advanced to payment)
	var checkoutFlow *AnalyzedFlow
	var paymentFlow *AnalyzedFlow
	for _, f := range analyzed {
		if f.Flow == "checkout" {
			checkoutFlow = f
		}
		if f.Flow == "payment" {
			paymentFlow = f
		}
	}

	if checkoutFlow == nil || paymentFlow == nil {
		t.Fatal("Could not find checkout or payment flow")
	}

	if !containsBehavior(checkoutFlow.Behaviors, BehaviorProgress) {
		t.Error("Expected BehaviorProgress on checkout flow (user progressed to payment)")
	}

	// checkout should NOT have abandon (it was replaced by progress)
	if containsBehavior(checkoutFlow.Behaviors, BehaviorAbandon) {
		t.Error("checkout should not have abandon — user progressed to next step")
	}

	// checkout outcome should be progress
	if checkoutFlow.Outcome != BehaviorProgress {
		t.Errorf("checkout Outcome = %v, want progress", checkoutFlow.Outcome)
	}

	// payment should have succeed (unchanged by funnel logic)
	if paymentFlow.Outcome != BehaviorSucceed {
		t.Errorf("payment Outcome = %v, want succeed", paymentFlow.Outcome)
	}
}

func TestFunnelProgressionNoAdvancement(t *testing.T) {
	a := New()

	now := time.Now()

	// User only reaches checkout, never progresses to payment
	flows := []*sessionflow.FlowInstance{
		{
			FlowInstanceID: "flow_1",
			UserID:         "user_a",
			Flow:           "checkout",
			StartTime:      now,
			Events: []sessionflow.FlowEvent{
				{Timestamp: now, Status: "view", RawEventName: "checkout_view"},
				{Timestamp: now.Add(1 * time.Minute), Status: "exit", RawEventName: "checkout_exit"},
			},
			IsComplete: false,
		},
	}

	ctx := &AnalysisContext{
		Scope: ScopeUserSession,
		FunnelDefinitions: map[string][]string{
			"purchase_funnel": {"checkout", "payment", "order"},
		},
	}

	result, err := a.ProcessWithContext(flows, ctx)
	if err != nil {
		t.Fatalf("ProcessWithContext failed: %v", err)
	}

	analyzed := result.([]*AnalyzedFlow)

	// Should NOT have progress — user didn't advance
	if containsBehavior(analyzed[0].Behaviors, BehaviorProgress) {
		t.Error("Should not have BehaviorProgress — user didn't reach next funnel step")
	}

	// Should have abandon (exited without success)
	if !containsBehavior(analyzed[0].Behaviors, BehaviorAbandon) {
		t.Error("Expected abandon behavior for user who exited checkout")
	}
}

func TestEventCountIsSet(t *testing.T) {
	a := New()

	flow := &sessionflow.FlowInstance{
		FlowInstanceID: "flow_1",
		UserID:         "user_a",
		Flow:           "checkout",
		Events: []sessionflow.FlowEvent{
			{Status: "view", RawEventName: "checkout_view"},
			{Status: "click", RawEventName: "checkout_click"},
			{Status: "success", RawEventName: "checkout_success"},
		},
		IsComplete: true,
	}

	result := a.analyzeFlow(flow)

	if result.EventCount != 3 {
		t.Errorf("EventCount = %d, want 3", result.EventCount)
	}
}

func TestProcessWithContextNilContextFallsBack(t *testing.T) {
	a := New()

	flows := []*sessionflow.FlowInstance{
		{
			FlowInstanceID: "flow_1",
			UserID:         "user_a",
			Flow:           "payment",
			Events: []sessionflow.FlowEvent{
				{Status: "view", RawEventName: "payment_view"},
				{Status: "click", RawEventName: "payment_click"},
				{Status: "success", RawEventName: "payment_success"},
			},
			IsComplete: true,
		},
	}

	// nil context should fall back to standard Process
	result, err := a.ProcessWithContext(flows, nil)
	if err != nil {
		t.Fatalf("ProcessWithContext with nil failed: %v", err)
	}

	analyzed := result.([]*AnalyzedFlow)
	if analyzed[0].Outcome != BehaviorSucceed {
		t.Errorf("Outcome = %v, want succeed", analyzed[0].Outcome)
	}
}
