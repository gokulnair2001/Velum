package sessionflow

import (
	"testing"
	"time"

	"github.com/velum/internal/layers/eventadapter"
)

func TestGroupByUser(t *testing.T) {
	r := New()

	events := []*NormalizedEventInput{
		{ID: "1", UserID: "user_a", Timestamp: "2026-02-04T10:00:00Z"},
		{ID: "2", UserID: "user_b", Timestamp: "2026-02-04T10:01:00Z"},
		{ID: "3", UserID: "user_a", Timestamp: "2026-02-04T10:02:00Z"},
		{ID: "4", UserID: float64(123), Timestamp: "2026-02-04T10:03:00Z"},
	}

	grouped := r.groupByUser(events)

	if len(grouped["user_a"]) != 2 {
		t.Errorf("Expected 2 events for user_a, got %d", len(grouped["user_a"]))
	}
	if len(grouped["user_b"]) != 1 {
		t.Errorf("Expected 1 event for user_b, got %d", len(grouped["user_b"]))
	}
	if len(grouped["123"]) != 1 {
		t.Errorf("Expected 1 event for user 123, got %d", len(grouped["123"]))
	}
}

func TestSortByTimestamp(t *testing.T) {
	r := New()

	events := []*NormalizedEventInput{
		{ID: "3", Timestamp: "2026-02-04T10:02:00Z"},
		{ID: "1", Timestamp: "2026-02-04T10:00:00Z"},
		{ID: "2", Timestamp: "2026-02-04T10:01:00Z"},
	}

	r.sortByTimestamp(events)

	if events[0].ID != "1" || events[1].ID != "2" || events[2].ID != "3" {
		t.Errorf("Events not sorted correctly: %v", events)
	}
}

func TestReconstructFlowInstance(t *testing.T) {
	r := New()

	events := []*NormalizedEventInput{
		{
			ID:        "1",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:00:00Z",
			SessionID: "sess_1",
			Event:     "checkout_view",
			Normalized: &NormalizedData{
				Original: "checkout_view",
				Status:   []string{"view"},
				Surface:  []string{"checkout"},
				Flow:     []string{"payment"},
			},
		},
		{
			ID:        "2",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:01:00Z",
			SessionID: "sess_1",
			Event:     "payment_click",
			Normalized: &NormalizedData{
				Original: "payment_click",
				Status:   []string{"click"},
				Surface:  []string{"button"},
				Flow:     []string{"payment"},
			},
		},
		{
			ID:        "3",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:02:00Z",
			SessionID: "sess_1",
			Event:     "payment_success",
			Normalized: &NormalizedData{
				Original: "payment_success",
				Status:   []string{"success"},
				Surface:  []string{"modal"},
				Flow:     []string{"payment"},
			},
		},
	}

	flowInstances, err := r.reconstruct(events)
	if err != nil {
		t.Fatalf("Reconstruct failed: %v", err)
	}

	if len(flowInstances) != 1 {
		t.Fatalf("Expected 1 flow instance, got %d", len(flowInstances))
	}

	flow := flowInstances[0]
	if flow.UserID != "user_a" {
		t.Errorf("UserID = %v, want user_a", flow.UserID)
	}
	if flow.Flow != "payment" {
		t.Errorf("Flow = %v, want payment", flow.Flow)
	}
	if flow.ContextType != "explicit_session" {
		t.Errorf("ContextType = %v, want explicit_session", flow.ContextType)
	}
	if !flow.IsComplete {
		t.Error("Expected flow to be complete")
	}
	if len(flow.Events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(flow.Events))
	}
}

func TestContextTypeWindowed(t *testing.T) {
	r := New()

	events := []*NormalizedEventInput{
		{
			ID:        "1",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:00:00Z",
			SessionID: "sess_1",
			Event:     "checkout_view",
			Normalized: &NormalizedData{
				Status: []string{"view"},
				Flow:   []string{"payment"},
			},
		},
		{
			ID:        "2",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:01:00Z",
			SessionID: "", // Missing session_id
			Event:     "payment_success",
			Normalized: &NormalizedData{
				Status: []string{"success"},
				Flow:   []string{"payment"},
			},
		},
	}

	flowInstances, _ := r.reconstruct(events)

	if len(flowInstances) != 1 {
		t.Fatalf("Expected 1 flow instance, got %d", len(flowInstances))
	}

	if flowInstances[0].ContextType != "windowed" {
		t.Errorf("ContextType = %v, want windowed", flowInstances[0].ContextType)
	}
}

func TestMultipleFlowInstances(t *testing.T) {
	r := New()

	events := []*NormalizedEventInput{
		{
			ID:        "1",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:00:00Z",
			Event:     "login_view",
			Normalized: &NormalizedData{
				Status: []string{"view"},
				Flow:   []string{"authentication"},
			},
		},
		{
			ID:        "2",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:01:00Z",
			Event:     "login_success",
			Normalized: &NormalizedData{
				Status: []string{"success"},
				Flow:   []string{"authentication"},
			},
		},
		{
			ID:        "3",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:02:00Z",
			Event:     "login_view", // New flow instance starts
			Normalized: &NormalizedData{
				Status: []string{"view"},
				Flow:   []string{"authentication"},
			},
		},
	}

	flowInstances, _ := r.reconstruct(events)

	if len(flowInstances) != 2 {
		t.Fatalf("Expected 2 flow instances, got %d", len(flowInstances))
	}

	// First should be complete
	if !flowInstances[0].IsComplete {
		t.Error("First flow instance should be complete")
	}
	// Second should not be complete (no success/exit)
	if flowInstances[1].IsComplete {
		t.Error("Second flow instance should not be complete")
	}
}

func TestMultipleUsers(t *testing.T) {
	r := New()

	events := []*NormalizedEventInput{
		{
			ID:        "1",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:00:00Z",
			Event:     "checkout_view",
			Normalized: &NormalizedData{
				Status: []string{"view"},
				Flow:   []string{"payment"},
			},
		},
		{
			ID:        "2",
			UserID:    "user_b",
			Timestamp: "2026-02-04T10:00:30Z",
			Event:     "checkout_view",
			Normalized: &NormalizedData{
				Status: []string{"view"},
				Flow:   []string{"payment"},
			},
		},
		{
			ID:        "3",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:01:00Z",
			Event:     "payment_success",
			Normalized: &NormalizedData{
				Status: []string{"success"},
				Flow:   []string{"payment"},
			},
		},
	}

	flowInstances, _ := r.reconstruct(events)

	if len(flowInstances) != 2 {
		t.Fatalf("Expected 2 flow instances (one per user), got %d", len(flowInstances))
	}

	// Find user_a's flow
	var userAFlow *FlowInstance
	for _, f := range flowInstances {
		if f.UserID == "user_a" {
			userAFlow = f
			break
		}
	}

	if userAFlow == nil {
		t.Fatal("Could not find user_a's flow")
	}
	if len(userAFlow.Events) != 2 {
		t.Errorf("user_a should have 2 events, got %d", len(userAFlow.Events))
	}
	if !userAFlow.IsComplete {
		t.Error("user_a's flow should be complete")
	}
}

func TestProcessFromEventAdapter(t *testing.T) {
	r := New()

	// Simulate input from event adapter layer
	events := []*eventadapter.ProcessedEvent{
		{
			Event: "checkout_view",
			OriginalFields: map[string]interface{}{
				"id":         "evt_1",
				"user_id":    "user_a",
				"ts":         "2026-02-04T10:00:00Z",
				"session_id": "sess_1",
			},
			Normalized: &eventadapter.NormalizedEvent{
				Original: "checkout_view",
				Tokens:   []string{"checkout", "view"},
				Status:   []string{"view"},
				Surface:  []string{"checkout"},
				Flow:     []string{"payment"},
			},
		},
		{
			Event: "payment_success",
			OriginalFields: map[string]interface{}{
				"id":         "evt_2",
				"user_id":    "user_a",
				"ts":         "2026-02-04T10:01:00Z",
				"session_id": "sess_1",
			},
			Normalized: &eventadapter.NormalizedEvent{
				Original: "payment_success",
				Tokens:   []string{"payment", "success"},
				Status:   []string{"success"},
				Surface:  []string{},
				Flow:     []string{"payment"},
			},
		},
	}

	result, err := r.Process(events)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	flowInstances, ok := result.([]*FlowInstance)
	if !ok {
		t.Fatalf("Expected []*FlowInstance, got %T", result)
	}

	if len(flowInstances) != 1 {
		t.Fatalf("Expected 1 flow instance, got %d", len(flowInstances))
	}

	flow := flowInstances[0]
	if flow.Flow != "payment" {
		t.Errorf("Flow = %v, want payment", flow.Flow)
	}
	if flow.ContextType != "explicit_session" {
		t.Errorf("ContextType = %v, want explicit_session", flow.ContextType)
	}
}

func TestFlowTimeout(t *testing.T) {
	config := DefaultConfig()
	config.FlowTimeout = 5 * time.Minute // Short timeout for testing
	r := NewWithConfig(config)

	events := []*NormalizedEventInput{
		{
			ID:        "1",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:00:00Z",
			Event:     "checkout_view",
			Normalized: &NormalizedData{
				Status: []string{"view"},
				Flow:   []string{"payment"},
			},
		},
		{
			ID:        "2",
			UserID:    "user_a",
			Timestamp: "2026-02-04T10:10:00Z", // 10 minutes later, exceeds timeout
			Event:     "checkout_click",
			Normalized: &NormalizedData{
				Status: []string{"click"},
				Flow:   []string{"payment"},
			},
		},
	}

	flowInstances, _ := r.reconstruct(events)

	// Should create 2 separate flow instances due to timeout
	if len(flowInstances) != 2 {
		t.Fatalf("Expected 2 flow instances due to timeout, got %d", len(flowInstances))
	}
}

func TestParseTimestampEpochFormats(t *testing.T) {
	r := New()

	tests := []struct {
		name     string
		input    string
		wantYear int
		wantZero bool
	}{
		{"epoch_ms", "1707500000000", 2024, false},
		{"epoch_ms_scientific", "1.7075e+12", 2024, false},
		{"epoch_ms_scientific_precise", "1.707500015e+12", 2024, false},
		{"epoch_s", "1707500000", 2024, false},
		{"iso8601", "2026-02-04T10:00:00Z", 2026, false},
		{"rfc3339", "2026-02-04T10:00:00+05:30", 2026, false},
		{"invalid", "not-a-timestamp", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.parseTimestamp(tt.input)
			if tt.wantZero {
				if !result.IsZero() {
					t.Errorf("expected zero time for %q, got %v", tt.input, result)
				}
			} else {
				if result.IsZero() {
					t.Errorf("expected non-zero time for %q, got zero", tt.input)
				}
				if result.Year() != tt.wantYear {
					t.Errorf("expected year %d for %q, got %d (%v)", tt.wantYear, tt.input, result.Year(), result)
				}
			}
		})
	}
}

func TestSortByTimestampEpochMs(t *testing.T) {
	r := New()

	// Simulate epoch ms timestamps as they come from JSON (float64 → %v → scientific notation)
	events := []*NormalizedEventInput{
		{ID: "3", Timestamp: "1.70750003e+12"},  // 30s later
		{ID: "1", Timestamp: "1.7075e+12"},      // earliest
		{ID: "2", Timestamp: "1.707500015e+12"}, // 15s later
	}

	r.sortByTimestamp(events)

	if events[0].ID != "1" || events[1].ID != "2" || events[2].ID != "3" {
		t.Errorf("Epoch ms events not sorted correctly: got %s, %s, %s",
			events[0].ID, events[1].ID, events[2].ID)
	}
}

func TestSurfaceFallbackFolding(t *testing.T) {
	r := New()

	// Simulate ride-hailing: booking_requested → driver_assigned → driver_cancelled → booking_requested → driver_assigned → ride_started → ride_completed
	// "driver" events should fold into the active "booking" flow, not create separate "driver" flows.
	events := []*NormalizedEventInput{
		{
			ID: "1", UserID: "user_1", Timestamp: "2026-02-04T10:00:00Z",
			Event: "booking_requested",
			Normalized: &NormalizedData{
				Original: "booking_requested", Tokens: []string{"booking", "requested"},
				Status: []string{"request"}, Surface: []string{"booking"}, Flow: []string{},
			},
		},
		{
			ID: "2", UserID: "user_1", Timestamp: "2026-02-04T10:00:30Z",
			Event: "driver_assigned",
			Normalized: &NormalizedData{
				Original: "driver_assigned", Tokens: []string{"driver", "assigned"},
				Status: []string{"assigned"}, Surface: []string{"driver"}, Flow: []string{},
			},
		},
		{
			ID: "3", UserID: "user_1", Timestamp: "2026-02-04T10:01:00Z",
			Event: "driver_cancelled",
			Normalized: &NormalizedData{
				Original: "driver_cancelled", Tokens: []string{"driver", "cancelled"},
				Status: []string{"cancelled"}, Surface: []string{"driver"}, Flow: []string{},
			},
		},
		{
			ID: "4", UserID: "user_1", Timestamp: "2026-02-04T10:02:00Z",
			Event: "booking_requested",
			Normalized: &NormalizedData{
				Original: "booking_requested", Tokens: []string{"booking", "requested"},
				Status: []string{"request"}, Surface: []string{"booking"}, Flow: []string{},
			},
		},
		{
			ID: "5", UserID: "user_1", Timestamp: "2026-02-04T10:02:30Z",
			Event: "driver_assigned",
			Normalized: &NormalizedData{
				Original: "driver_assigned", Tokens: []string{"driver", "assigned"},
				Status: []string{"assigned"}, Surface: []string{"driver"}, Flow: []string{},
			},
		},
		{
			ID: "6", UserID: "user_1", Timestamp: "2026-02-04T10:03:00Z",
			Event: "ride_started",
			Normalized: &NormalizedData{
				Original: "ride_started", Tokens: []string{"ride", "started"},
				Status: []string{"start"}, Surface: []string{"ride"}, Flow: []string{},
			},
		},
		{
			ID: "7", UserID: "user_1", Timestamp: "2026-02-04T10:10:00Z",
			Event: "ride_completed",
			Normalized: &NormalizedData{
				Original: "ride_completed", Tokens: []string{"ride", "completed"},
				Status: []string{"success"}, Surface: []string{"ride"}, Flow: []string{},
			},
		},
	}

	flowInstances, err := r.reconstruct(events)
	if err != nil {
		t.Fatalf("Reconstruct failed: %v", err)
	}

	// Count flows by name
	flowCounts := make(map[string]int)
	for _, f := range flowInstances {
		flowCounts[f.Flow]++
	}

	// There should be NO "driver" flows — driver events should fold into booking
	if flowCounts["driver"] > 0 {
		t.Errorf("Expected 0 driver flows (should fold into booking), got %d", flowCounts["driver"])
	}

	// Should have 2 booking flows (first cancelled, second still active)
	if flowCounts["booking"] != 2 {
		t.Errorf("Expected 2 booking flows, got %d", flowCounts["booking"])
	}

	// Should have 1 ride flow
	if flowCounts["ride"] != 1 {
		t.Errorf("Expected 1 ride flow, got %d", flowCounts["ride"])
	}

	// The first booking should contain the driver_assigned and driver_cancelled events
	var booking1Events int
	for _, f := range flowInstances {
		if f.Flow == "booking" && !f.IsComplete {
			if booking1Events == 0 || len(f.Events) > booking1Events {
				booking1Events = len(f.Events)
			}
		}
	}
	// First booking: booking_requested + driver_assigned + driver_cancelled = 3 events
	found3EventBooking := false
	for _, f := range flowInstances {
		if f.Flow == "booking" && len(f.Events) == 3 {
			found3EventBooking = true
			break
		}
	}
	if !found3EventBooking {
		t.Errorf("Expected a booking flow with 3 events (booking_requested + driver_assigned + driver_cancelled)")
		for _, f := range flowInstances {
			t.Logf("  Flow=%s Events=%d IsComplete=%v", f.Flow, len(f.Events), f.IsComplete)
		}
	}

	// ride flow should be complete (success)
	for _, f := range flowInstances {
		if f.Flow == "ride" {
			if !f.IsComplete {
				t.Error("ride flow should be complete (success)")
			}
			if len(f.Events) != 2 {
				t.Errorf("ride flow should have 2 events, got %d", len(f.Events))
			}
		}
	}
}

func TestLifecycleSurfaceSkip(t *testing.T) {
	r := New()

	events := []*NormalizedEventInput{
		{
			ID: "1", UserID: "user_1", Timestamp: "2026-02-04T10:00:00Z",
			Event: "session_start",
			Normalized: &NormalizedData{
				Status: []string{"start"}, Surface: []string{"session"}, Flow: []string{},
			},
		},
		{
			ID: "2", UserID: "user_1", Timestamp: "2026-02-04T10:01:00Z",
			Event: "checkout_view",
			Normalized: &NormalizedData{
				Status: []string{"view"}, Surface: []string{"checkout"}, Flow: []string{"payment"},
			},
		},
		{
			ID: "3", UserID: "user_1", Timestamp: "2026-02-04T10:10:00Z",
			Event: "session_end",
			Normalized: &NormalizedData{
				Status: []string{"end"}, Surface: []string{"session"}, Flow: []string{},
			},
		},
	}

	flowInstances, err := r.reconstruct(events)
	if err != nil {
		t.Fatalf("Reconstruct failed: %v", err)
	}

	// Should have exactly 1 flow (payment), no "session" flow
	if len(flowInstances) != 1 {
		t.Fatalf("Expected 1 flow instance, got %d", len(flowInstances))
	}
	if flowInstances[0].Flow != "payment" {
		t.Errorf("Flow = %v, want payment", flowInstances[0].Flow)
	}
}
