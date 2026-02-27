package datamapper

import (
	"testing"

	"github.com/velum/internal/config"
)

func TestDataMapper_BasicMapping(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"id": {
				Paths:    []string{"data.id", "meta.id"},
				Required: true,
			},
			"event": {
				Paths:    []string{"payload.action"},
				Required: true,
			},
		},
	}

	mapper := New(cfg)

	input := map[string]interface{}{
		"data": map[string]interface{}{
			"id": "event-123",
		},
		"payload": map[string]interface{}{
			"action": "button_click",
		},
	}

	result, err := mapper.Process(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mapped, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}

	if mapped["id"] != "event-123" {
		t.Errorf("expected id='event-123', got '%v'", mapped["id"])
	}
	if mapped["event"] != "button_click" {
		t.Errorf("expected event='button_click', got '%v'", mapped["event"])
	}
}

func TestDataMapper_FallbackPaths(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"user_id": {
				Paths:    []string{"context.user.id", "actor.user_id", "user"},
				Required: true,
			},
		},
	}

	mapper := New(cfg)

	tests := []struct {
		name     string
		input    map[string]interface{}
		expected string
	}{
		{
			name: "first path matches",
			input: map[string]interface{}{
				"context": map[string]interface{}{
					"user": map[string]interface{}{
						"id": "user-1",
					},
				},
			},
			expected: "user-1",
		},
		{
			name: "second path matches",
			input: map[string]interface{}{
				"actor": map[string]interface{}{
					"user_id": "user-2",
				},
			},
			expected: "user-2",
		},
		{
			name: "third path matches",
			input: map[string]interface{}{
				"user": "user-3",
			},
			expected: "user-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mapper.Process(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			mapped := result.(map[string]interface{})
			if mapped["user_id"] != tt.expected {
				t.Errorf("expected user_id='%s', got '%v'", tt.expected, mapped["user_id"])
			}
		})
	}
}

func TestDataMapper_MissingMandatoryField(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"id": {
				Paths:    []string{"data.id"},
				Required: true,
			},
			"ts": {
				Paths:    []string{"timestamp"},
				Required: true,
			},
		},
	}

	mapper := New(cfg)

	input := map[string]interface{}{
		"data": map[string]interface{}{
			"id": "event-123",
		},
		// missing timestamp
	}

	_, err := mapper.Process(input)
	if err == nil {
		t.Fatal("expected error for missing mandatory field, got nil")
	}

	if !contains(err.Error(), "missing mandatory field: ts") {
		t.Errorf("expected error message about missing 'ts', got: %v", err)
	}
}

func TestDataMapper_OptionalFieldOmitted(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"id": {
				Paths:    []string{"data.id"},
				Required: true,
			},
			"session_id": {
				Paths:    []string{"context.session.id"},
				Required: false, // optional
			},
		},
	}

	mapper := New(cfg)

	input := map[string]interface{}{
		"data": map[string]interface{}{
			"id": "event-123",
		},
		// no session info
	}

	result, err := mapper.Process(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mapped := result.(map[string]interface{})

	if mapped["id"] != "event-123" {
		t.Errorf("expected id='event-123', got '%v'", mapped["id"])
	}

	// session_id should be omitted (not present in map)
	if _, exists := mapped["session_id"]; exists {
		t.Error("expected session_id to be omitted, but it exists in result")
	}
}

func TestDataMapper_TimestampEpochMS(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"ts": {
				Paths:    []string{"meta.time"},
				Format:   "epoch_ms",
				Required: true,
			},
		},
	}

	mapper := New(cfg)

	input := map[string]interface{}{
		"meta": map[string]interface{}{
			"time": float64(1707500000000), // JSON numbers come as float64
		},
	}

	result, err := mapper.Process(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mapped := result.(map[string]interface{})
	ts := mapped["ts"].(int64)

	if ts != 1707500000000 {
		t.Errorf("expected ts=1707500000000, got %d", ts)
	}
}

func TestDataMapper_TimestampEpochSeconds(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"ts": {
				Paths:    []string{"created_at"},
				Format:   "epoch_s",
				Required: true,
			},
		},
	}

	mapper := New(cfg)

	input := map[string]interface{}{
		"created_at": float64(1707500000),
	}

	result, err := mapper.Process(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mapped := result.(map[string]interface{})
	ts := mapped["ts"].(int64)

	// epoch_s should be multiplied by 1000 to get ms
	if ts != 1707500000000 {
		t.Errorf("expected ts=1707500000000, got %d", ts)
	}
}

func TestDataMapper_TimestampISO8601(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"ts": {
				Paths:    []string{"timestamp"},
				Format:   "iso8601",
				Required: true,
			},
		},
	}

	mapper := New(cfg)

	input := map[string]interface{}{
		"timestamp": "2024-02-09T15:30:00Z",
	}

	result, err := mapper.Process(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mapped := result.(map[string]interface{})
	ts := mapped["ts"].(int64)

	// Should be converted to epoch milliseconds
	if ts <= 0 {
		t.Errorf("expected positive timestamp, got %d", ts)
	}
}

func TestDataMapper_BatchProcessing(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"id": {
				Paths:    []string{"data.id"},
				Required: true,
			},
		},
	}

	mapper := New(cfg)

	input := []map[string]interface{}{
		{"data": map[string]interface{}{"id": "event-1"}},
		{"data": map[string]interface{}{"id": "event-2"}},
		{"data": map[string]interface{}{"id": "event-3"}},
	}

	result, err := mapper.Process(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	results := result.([]map[string]interface{})
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, r := range results {
		expected := "event-" + string('1'+rune(i))
		if r["id"] != expected {
			t.Errorf("result[%d]: expected id='%s', got '%v'", i, expected, r["id"])
		}
	}
}

func TestDataMapper_DisabledPassthrough(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: false, // disabled
		Mapping: map[string]config.FieldMappingSpec{
			"id": {
				Paths:    []string{"data.id"},
				Required: true,
			},
		},
	}

	mapper := New(cfg)

	input := map[string]interface{}{
		"original": "data",
	}

	result, err := mapper.Process(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When disabled, should pass through unchanged
	mapped := result.(map[string]interface{})
	if mapped["original"] != "data" {
		t.Error("expected passthrough when disabled")
	}
}

func TestDataMapper_NestedPaths(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"action": {
				Paths:    []string{"deeply.nested.payload.event.action"},
				Required: true,
			},
		},
	}

	mapper := New(cfg)

	input := map[string]interface{}{
		"deeply": map[string]interface{}{
			"nested": map[string]interface{}{
				"payload": map[string]interface{}{
					"event": map[string]interface{}{
						"action": "deep_click",
					},
				},
			},
		},
	}

	result, err := mapper.Process(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mapped := result.(map[string]interface{})
	if mapped["action"] != "deep_click" {
		t.Errorf("expected action='deep_click', got '%v'", mapped["action"])
	}
}

// TestDataMapper_RealWorldConfig tests with the exact config structure from config.yaml
func TestDataMapper_RealWorldConfig(t *testing.T) {
	// This matches the config.yaml mapping structure exactly
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"id": {
				Paths:    []string{"data.id", "payload.id", "meta.id"},
				Required: true,
			},
			"ts": {
				Paths:    []string{"meta.time", "timestamp", "created_at"},
				Format:   "epoch_ms",
				Required: true,
			},
			"event": {
				Paths:    []string{"payload.event.action", "event_name", "action"},
				Required: true,
			},
			"user_id": {
				Paths:    []string{"context.user.id", "actor.user_id", "user_id"},
				Required: true,
			},
			"session_id": {
				Paths:    []string{"context.session.id", "session_id"},
				Required: false,
			},
		},
	}

	mapper := New(cfg)

	t.Run("complete event with all fields", func(t *testing.T) {
		input := map[string]interface{}{
			"data": map[string]interface{}{
				"id": "evt-12345",
			},
			"meta": map[string]interface{}{
				"time": float64(1707500000000),
			},
			"payload": map[string]interface{}{
				"event": map[string]interface{}{
					"action": "checkout_completed",
				},
			},
			"context": map[string]interface{}{
				"user": map[string]interface{}{
					"id": "usr-abc123",
				},
				"session": map[string]interface{}{
					"id": "sess-xyz789",
				},
			},
		}

		result, err := mapper.Process(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mapped := result.(map[string]interface{})

		// Verify all fields
		if mapped["id"] != "evt-12345" {
			t.Errorf("id: expected 'evt-12345', got '%v'", mapped["id"])
		}
		if mapped["ts"].(int64) != 1707500000000 {
			t.Errorf("ts: expected 1707500000000, got '%v'", mapped["ts"])
		}
		if mapped["event"] != "checkout_completed" {
			t.Errorf("event: expected 'checkout_completed', got '%v'", mapped["event"])
		}
		if mapped["user_id"] != "usr-abc123" {
			t.Errorf("user_id: expected 'usr-abc123', got '%v'", mapped["user_id"])
		}
		if mapped["session_id"] != "sess-xyz789" {
			t.Errorf("session_id: expected 'sess-xyz789', got '%v'", mapped["session_id"])
		}
	})

	t.Run("event without optional session_id", func(t *testing.T) {
		input := map[string]interface{}{
			"data": map[string]interface{}{
				"id": "evt-67890",
			},
			"timestamp": float64(1707600000000),
			"action":    "button_click",
			"user_id":   "usr-def456",
			// No session_id - should be omitted
		}

		result, err := mapper.Process(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mapped := result.(map[string]interface{})

		if mapped["id"] != "evt-67890" {
			t.Errorf("id: expected 'evt-67890', got '%v'", mapped["id"])
		}
		if mapped["event"] != "button_click" {
			t.Errorf("event: expected 'button_click', got '%v'", mapped["event"])
		}

		// session_id should NOT exist in the result
		if _, exists := mapped["session_id"]; exists {
			t.Error("session_id should be omitted when not found")
		}
	})

	t.Run("event using fallback paths", func(t *testing.T) {
		// Uses second/third fallback paths for each field
		input := map[string]interface{}{
			"payload": map[string]interface{}{
				"id": "evt-fallback",
			},
			"created_at": float64(1707700000000),
			"event_name": "page_view",
			"actor": map[string]interface{}{
				"user_id": "usr-fallback",
			},
			"session_id": "sess-fallback",
		}

		result, err := mapper.Process(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mapped := result.(map[string]interface{})

		if mapped["id"] != "evt-fallback" {
			t.Errorf("id: expected 'evt-fallback', got '%v'", mapped["id"])
		}
		if mapped["event"] != "page_view" {
			t.Errorf("event: expected 'page_view', got '%v'", mapped["event"])
		}
		if mapped["user_id"] != "usr-fallback" {
			t.Errorf("user_id: expected 'usr-fallback', got '%v'", mapped["user_id"])
		}
		if mapped["session_id"] != "sess-fallback" {
			t.Errorf("session_id: expected 'sess-fallback', got '%v'", mapped["session_id"])
		}
	})

	t.Run("missing mandatory field fails", func(t *testing.T) {
		input := map[string]interface{}{
			"data": map[string]interface{}{
				"id": "evt-incomplete",
			},
			"timestamp": float64(1707800000000),
			"action":    "some_action",
			// Missing user_id - should fail
		}

		_, err := mapper.Process(input)
		if err == nil {
			t.Fatal("expected error for missing user_id, got nil")
		}

		if !contains(err.Error(), "user_id") {
			t.Errorf("error should mention 'user_id', got: %v", err)
		}
	})

	t.Run("batch processing multiple events", func(t *testing.T) {
		input := []map[string]interface{}{
			{
				"meta":    map[string]interface{}{"id": "evt-batch-1", "time": float64(1707500000000)},
				"action":  "click",
				"user_id": "usr-1",
			},
			{
				"data":       map[string]interface{}{"id": "evt-batch-2"},
				"timestamp":  float64(1707500001000),
				"event_name": "scroll",
				"context":    map[string]interface{}{"user": map[string]interface{}{"id": "usr-2"}},
			},
		}

		result, err := mapper.Process(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		results := result.([]map[string]interface{})
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		if results[0]["id"] != "evt-batch-1" {
			t.Errorf("batch[0].id: expected 'evt-batch-1', got '%v'", results[0]["id"])
		}
		if results[1]["event"] != "scroll" {
			t.Errorf("batch[1].event: expected 'scroll', got '%v'", results[1]["event"])
		}
	})
}

// TestDataMapper_ExtraFieldsPassthrough verifies that unmapped top-level fields
// (device, country, error_code, etc.) survive mapping and reach downstream layers.
func TestDataMapper_ExtraFieldsPassthrough(t *testing.T) {
	cfg := config.DataMappingConfig{
		Enabled: true,
		Mapping: map[string]config.FieldMappingSpec{
			"id": {
				Paths:    []string{"data.id"},
				Required: true,
			},
			"event": {
				Paths:    []string{"payload.action", "action"},
				Required: true,
			},
			"user_id": {
				Paths:    []string{"user_id"},
				Required: true,
			},
		},
	}

	mapper := New(cfg)

	input := map[string]interface{}{
		"data": map[string]interface{}{
			"id": "evt-123",
		},
		"payload": map[string]interface{}{
			"action": "checkout",
		},
		"user_id":    "usr-1",
		"device":     "mobile",
		"country":    "US",
		"error_code": "ERR_TIMEOUT",
		"cart_value": 49.99,
		"plan_name":  "pro",
	}

	result, err := mapper.Process(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mapped := result.(map[string]interface{})

	// Mapped fields should be present
	if mapped["id"] != "evt-123" {
		t.Errorf("id: expected 'evt-123', got '%v'", mapped["id"])
	}
	if mapped["event"] != "checkout" {
		t.Errorf("event: expected 'checkout', got '%v'", mapped["event"])
	}
	if mapped["user_id"] != "usr-1" {
		t.Errorf("user_id: expected 'usr-1', got '%v'", mapped["user_id"])
	}

	// Extra properties should pass through
	extraFields := map[string]interface{}{
		"device":     "mobile",
		"country":    "US",
		"error_code": "ERR_TIMEOUT",
		"cart_value": 49.99,
		"plan_name":  "pro",
	}
	for key, expected := range extraFields {
		if mapped[key] != expected {
			t.Errorf("%s: expected '%v', got '%v'", key, expected, mapped[key])
		}
	}

	// Nested container objects should NOT pass through
	if _, exists := mapped["data"]; exists {
		t.Error("nested container 'data' should not pass through")
	}
	if _, exists := mapped["payload"]; exists {
		t.Error("nested container 'payload' should not pass through")
	}
}

// TestDataMapper_TimestampFormats tests all supported timestamp formats
func TestDataMapper_TimestampFormats(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		input    interface{}
		expected int64
	}{
		{"epoch_ms float64", "epoch_ms", float64(1707500000000), 1707500000000},
		{"epoch_ms string", "epoch_ms", "1707500000000", 1707500000000},
		{"epoch_s to ms", "epoch_s", float64(1707500000), 1707500000000},
		{"epoch_s string", "epoch_s", "1707500000", 1707500000000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DataMappingConfig{
				Enabled: true,
				Mapping: map[string]config.FieldMappingSpec{
					"ts": {Paths: []string{"time"}, Format: tt.format, Required: true},
				},
			}

			mapper := New(cfg)
			result, err := mapper.Process(map[string]interface{}{"time": tt.input})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			mapped := result.(map[string]interface{})
			if mapped["ts"].(int64) != tt.expected {
				t.Errorf("expected %d, got %v", tt.expected, mapped["ts"])
			}
		})
	}

	// ISO8601 tests
	iso8601Tests := []struct {
		name  string
		input string
	}{
		{"RFC3339", "2024-02-09T15:30:00Z"},
		{"RFC3339 with timezone", "2024-02-09T15:30:00+05:30"},
		{"date only", "2024-02-09"},
	}

	for _, tt := range iso8601Tests {
		t.Run("iso8601 "+tt.name, func(t *testing.T) {
			cfg := config.DataMappingConfig{
				Enabled: true,
				Mapping: map[string]config.FieldMappingSpec{
					"ts": {Paths: []string{"time"}, Format: "iso8601", Required: true},
				},
			}

			mapper := New(cfg)
			result, err := mapper.Process(map[string]interface{}{"time": tt.input})
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", tt.input, err)
			}

			mapped := result.(map[string]interface{})
			ts := mapped["ts"].(int64)
			if ts <= 0 {
				t.Errorf("expected positive timestamp, got %d", ts)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
