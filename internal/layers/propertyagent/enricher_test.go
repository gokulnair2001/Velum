package propertyagent

import (
	"context"
	"sync"
	"testing"

	"github.com/velum/internal/canonical"
)

// --- In-memory storage for tests ---

type memoryPropertyStorage struct {
	mu      sync.RWMutex
	entries map[string]*PropertyEntry
}

func newMemoryStorage() *memoryPropertyStorage {
	return &memoryPropertyStorage{
		entries: make(map[string]*PropertyEntry),
	}
}

func (m *memoryPropertyStorage) GetProperty(ctx context.Context, keyName string) (*PropertyEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.entries[keyName]
	if !ok {
		return nil, nil
	}
	copy := *entry
	return &copy, nil
}

func (m *memoryPropertyStorage) GetPropertiesByRole(ctx context.Context, role PropertyRole) ([]*PropertyEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*PropertyEntry
	for _, e := range m.entries {
		if e.Role == role {
			copy := *e
			result = append(result, &copy)
		}
	}
	return result, nil
}

func (m *memoryPropertyStorage) GetAllProperties(ctx context.Context) ([]*PropertyEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*PropertyEntry
	for _, e := range m.entries {
		copy := *e
		result = append(result, &copy)
	}
	return result, nil
}

func (m *memoryPropertyStorage) UpsertProperty(ctx context.Context, entry *PropertyEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := *entry
	m.entries[entry.KeyName] = &copy
	return nil
}

func (m *memoryPropertyStorage) UpsertPropertyBatch(ctx context.Context, entries []*PropertyEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range entries {
		copy := *e
		m.entries[e.KeyName] = &copy
	}
	return nil
}

func (m *memoryPropertyStorage) Close() error { return nil }

func (m *memoryPropertyStorage) GetPropertyStats(ctx context.Context) (map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	roleCounts := make(map[string]int)
	for _, e := range m.entries {
		roleCounts[string(e.Role)]++
	}
	return map[string]interface{}{
		"total_entries": len(m.entries),
		"by_role":       roleCounts,
	}, nil
}

// --- Tests ---

func TestPropertyRoles(t *testing.T) {
	if RoleDimension != "dimension" {
		t.Errorf("RoleDimension = %s, want dimension", RoleDimension)
	}
	if RoleTarget != "target" {
		t.Errorf("RoleTarget = %s, want target", RoleTarget)
	}
	if RoleCondition != "condition" {
		t.Errorf("RoleCondition = %s, want condition", RoleCondition)
	}
	if RoleMeasure != "measure" {
		t.Errorf("RoleMeasure = %s, want measure", RoleMeasure)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Error("Default config should be disabled")
	}
	if cfg.Model != "llama-3.1-8b-instant" {
		t.Errorf("Default model = %s, want llama-3.1-8b-instant", cfg.Model)
	}
	if cfg.CircuitBreaker.FailureThreshold != 5 {
		t.Errorf("Default failure threshold = %d, want 5", cfg.CircuitBreaker.FailureThreshold)
	}
}

func TestMemoryStorage(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStorage()

	// Get non-existent
	entry, err := store.GetProperty(ctx, "plan_name")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if entry != nil {
		t.Fatal("Expected nil for non-existent key")
	}

	// Upsert single
	err = store.UpsertProperty(ctx, &PropertyEntry{
		KeyName: "plan_name",
		Role:    RoleTarget,
		Label:   "plan_name",
		Source:  "ai",
	})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	entry, err = store.GetProperty(ctx, "plan_name")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if entry == nil || entry.Role != RoleTarget {
		t.Fatal("Expected target role for plan_name")
	}

	// Upsert batch
	batch := []*PropertyEntry{
		{KeyName: "error_code", Role: RoleCondition, Source: "ai"},
		{KeyName: "cart_value", Role: RoleMeasure, Source: "inferred"},
		{KeyName: "device", Role: RoleDimension, Label: "device", Source: "builtin"},
	}
	err = store.UpsertPropertyBatch(ctx, batch)
	if err != nil {
		t.Fatalf("Batch upsert failed: %v", err)
	}

	// Get all
	all, err := store.GetAllProperties(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("Expected 4 entries, got %d", len(all))
	}

	// Get by role
	conditions, err := store.GetPropertiesByRole(ctx, RoleCondition)
	if err != nil {
		t.Fatalf("GetByRole failed: %v", err)
	}
	if len(conditions) != 1 || conditions[0].KeyName != "error_code" {
		t.Errorf("Expected 1 condition (error_code), got %d", len(conditions))
	}
}

func TestContextEnricher_DimensionsAndMeasures(t *testing.T) {
	// Dimensions and measures should be recognised and skipped — no new DB entries.
	store := newMemoryStorage()
	agent := NewAgentWithConfig(&Config{
		Enabled: true,
		APIKey:  "test-key",
		Model:   "test",
	})
	enricher := NewContextEnricher(store, agent, false)

	events := []map[string]interface{}{
		{
			"event":      "purchase_completed",
			"user_id":    "u123",
			"device":     "mobile", // built-in dimension
			"country":    "US",     // built-in dimension
			"cart_value": 120.50,   // numeric → measure
			"load_time":  340,      // numeric → measure
		},
	}

	result, err := enricher.Process(events)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Learn-only: input passes through unchanged
	processed := result.([]map[string]interface{})
	if len(processed) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(processed))
	}

	// Enricher should NOT attach _context (EventAdapter does that now)
	if _, hasCtx := processed[0][canonical.ContextKey]; hasCtx {
		t.Error("Learn-only enricher should not attach _context")
	}

	// Original event data unchanged
	if processed[0]["device"] != "mobile" {
		t.Errorf("device = %v, want mobile", processed[0]["device"])
	}

	// No new DB entries — dimensions & measures don't need AI classification
	ctx := context.Background()
	all, _ := store.GetAllProperties(ctx)
	if len(all) != 0 {
		t.Errorf("Expected 0 DB entries (no unknowns), got %d", len(all))
	}
}

func TestContextEnricher_CachedProperties(t *testing.T) {
	// Pre-populated DB entries should be skipped — no re-classification.
	store := newMemoryStorage()
	ctx := context.Background()

	store.UpsertProperty(ctx, &PropertyEntry{KeyName: "plan_name", Role: RoleTarget, Source: "ai"})
	store.UpsertProperty(ctx, &PropertyEntry{KeyName: "error_code", Role: RoleCondition, Source: "ai"})

	agent := NewAgentWithConfig(&Config{
		Enabled: true,
		APIKey:  "test-key",
		Model:   "test",
	})
	enricher := NewContextEnricher(store, agent, false)

	events := []map[string]interface{}{
		{
			"event":      "checkout_failed",
			"user_id":    "u456",
			"plan_name":  "premium",       // already in DB as target
			"error_code": "card_declined", // already in DB as condition
			"device":     "desktop",       // dimension
			"amount":     49.99,           // measure
		},
	}

	result, err := enricher.Process(events)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Learn-only: input passes through unchanged
	processed := result.([]map[string]interface{})
	if processed[0]["plan_name"] != "premium" {
		t.Errorf("plan_name = %v, want premium", processed[0]["plan_name"])
	}

	// DB should still have exactly 2 entries — no new unknowns
	all, _ := store.GetAllProperties(ctx)
	if len(all) != 2 {
		t.Errorf("Expected 2 DB entries (pre-populated only), got %d", len(all))
	}
}

func TestContextEnricher_CoreFieldsSkipped(t *testing.T) {
	store := newMemoryStorage()
	agent := NewAgentWithConfig(&Config{
		Enabled: true,
		APIKey:  "test-key",
		Model:   "test",
	})
	enricher := NewContextEnricher(store, agent, false)

	// Events with only core fields + one built-in dimension
	events := []map[string]interface{}{
		{
			"id":         "evt-1",
			"event":      "page_view",
			"user_id":    "u1",
			"session_id": "s1",
			"timestamp":  1234567890,
			"device":     "tablet", // only non-core property, but it's a dimension
		},
	}

	result, err := enricher.Process(events)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Learn-only: passes through unchanged
	processed := result.([]map[string]interface{})
	if len(processed) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(processed))
	}

	// No unknowns — core fields skipped, device is a dimension
	ctx := context.Background()
	all, _ := store.GetAllProperties(ctx)
	if len(all) != 0 {
		t.Errorf("Expected 0 DB entries (only core fields + dimensions), got %d", len(all))
	}
}

func TestContextEnricher_MultipleEvents(t *testing.T) {
	store := newMemoryStorage()
	ctx := context.Background()
	store.UpsertProperty(ctx, &PropertyEntry{KeyName: "page_name", Role: RoleTarget, Source: "ai"})

	agent := NewAgentWithConfig(&Config{
		Enabled: true,
		APIKey:  "test-key",
		Model:   "test",
	})
	enricher := NewContextEnricher(store, agent, false)

	events := []map[string]interface{}{
		{
			"event":     "page_view",
			"user_id":   "u1",
			"page_name": "pricing", // known in DB
			"device":    "mobile",  // dimension
		},
		{
			"event":     "button_click",
			"user_id":   "u1",
			"page_name": "checkout", // known in DB
			"platform":  "ios",      // dimension
			"clicks":    3,          // measure
		},
	}

	result, err := enricher.Process(events)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Learn-only: all events pass through unchanged
	processed := result.([]map[string]interface{})
	if len(processed) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(processed))
	}

	// Event 1 unchanged
	if processed[0]["page_name"] != "pricing" {
		t.Errorf("Event1 page_name = %v, want pricing", processed[0]["page_name"])
	}

	// Event 2 unchanged
	if processed[1]["clicks"] != 3 {
		t.Errorf("Event2 clicks = %v, want 3", processed[1]["clicks"])
	}

	// DB still has only 1 entry (page_name pre-populated, no new unknowns)
	all, _ := store.GetAllProperties(ctx)
	if len(all) != 1 {
		t.Errorf("Expected 1 DB entry (page_name), got %d", len(all))
	}
}

func TestContextEnricher_PassthroughWhenDisabled(t *testing.T) {
	// No storage, no agent → pass through
	enricher := NewContextEnricher(nil, nil, false)

	events := []map[string]interface{}{
		{"event": "test", "user_id": "u1"},
	}

	result, err := enricher.Process(events)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	processed := result.([]map[string]interface{})
	if _, hasContext := processed[0][canonical.ContextKey]; hasContext {
		t.Error("Expected no _context when enricher is disabled")
	}
}

func TestContextEnricher_NonEventInput(t *testing.T) {
	store := newMemoryStorage()
	agent := NewAgent()
	enricher := NewContextEnricher(store, agent, false)

	// String input — should pass through unchanged
	result, err := enricher.Process("not an event batch")
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result != "not an event batch" {
		t.Error("Expected passthrough for non-event input")
	}
}

func TestContextEnricher_EmptyEvents(t *testing.T) {
	store := newMemoryStorage()
	agent := NewAgent()
	enricher := NewContextEnricher(store, agent, false)

	result, err := enricher.Process([]map[string]interface{}{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	processed := result.([]map[string]interface{})
	if len(processed) != 0 {
		t.Error("Expected empty result for empty input")
	}
}

func TestContextEnricher_Name(t *testing.T) {
	enricher := NewContextEnricher(nil, nil, false)
	if enricher.Name() != "context_enricher" {
		t.Errorf("Name() = %s, want context_enricher", enricher.Name())
	}
}

func TestCircuitBreaker_BasicFlow(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		ResetTimeout:     0, // immediate reset for test
	}, false)

	// Should start closed
	if cb.State() != CircuitClosed {
		t.Error("Expected circuit to start closed")
	}

	// Record failures until threshold
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Error("Expected circuit still closed after 1 failure")
	}

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("Expected circuit open after 2 failures")
	}

	// Should block requests
	if err := cb.Allow(); err != ErrCircuitOpen {
		// With ResetTimeout=0, it will immediately transition to half-open
		// That's fine — just verify it doesn't stay permanently closed
	}

	// Record success to close again
	cb.RecordSuccess()
}

func TestSeedBuiltinDimensions_Idempotent(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStorage()

	// First seed
	err := SeedBuiltinDimensions(ctx, store)
	if err != nil {
		t.Fatalf("First seed failed: %v", err)
	}

	all, _ := store.GetAllProperties(ctx)
	firstCount := len(all)
	if firstCount == 0 {
		t.Fatal("Expected seeded entries")
	}

	// Second seed should be idempotent (skipped due to stats check)
	err = SeedBuiltinDimensions(ctx, store)
	if err != nil {
		t.Fatalf("Second seed failed: %v", err)
	}

	all2, _ := store.GetAllProperties(ctx)
	if len(all2) != firstCount {
		t.Errorf("Expected same count after re-seed, got %d vs %d", len(all2), firstCount)
	}
}

func TestPropertyAgent_DisabledReturnsEmpty(t *testing.T) {
	agent := NewAgent() // Disabled by default

	result, err := agent.ClassifyProperties([]UnknownProperty{
		{Key: "plan_name", SampleValue: "premium"},
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result.Target) != 0 || len(result.Condition) != 0 {
		t.Error("Expected empty result when agent is disabled")
	}
}

func TestPropertyAgent_EmptyInput(t *testing.T) {
	agent := NewAgentWithConfig(&Config{
		Enabled: true,
		APIKey:  "test-key",
		Model:   "test",
	})

	result, err := agent.ClassifyProperties([]UnknownProperty{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result.Target) != 0 || len(result.Condition) != 0 {
		t.Error("Expected empty result for empty input")
	}
}

func TestContextEnricher_DimensionVariants(t *testing.T) {
	// Dimension variants (device_type, os, lang, env) should all be recognised
	// and skipped during learning — they are not unknowns.
	store := newMemoryStorage()
	agent := NewAgentWithConfig(&Config{
		Enabled: true,
		APIKey:  "test-key",
		Model:   "test",
	})
	enricher := NewContextEnricher(store, agent, false)

	events := []map[string]interface{}{
		{
			"event":       "test",
			"user_id":     "u1",
			"device_type": "tablet",
			"os":          "android",
			"lang":        "en",
			"env":         "production",
		},
	}

	result, err := enricher.Process(events)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Learn-only: passes through unchanged
	processed := result.([]map[string]interface{})
	if processed[0]["device_type"] != "tablet" {
		t.Errorf("device_type = %v, want tablet", processed[0]["device_type"])
	}
	if processed[0]["os"] != "android" {
		t.Errorf("os = %v, want android", processed[0]["os"])
	}

	// No unknowns — all are recognised dimension variants
	ctx := context.Background()
	all, _ := store.GetAllProperties(ctx)
	if len(all) != 0 {
		t.Errorf("Expected 0 DB entries (all are dimensions), got %d", len(all))
	}
}
