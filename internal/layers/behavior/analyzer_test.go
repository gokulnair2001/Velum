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
