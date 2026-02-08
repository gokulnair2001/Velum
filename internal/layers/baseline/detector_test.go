package baseline

import (
	"context"
	"testing"
	"time"

	"github.com/velum/internal/layers/pattern"
	"github.com/velum/internal/storage"
)

func TestDetectorName(t *testing.T) {
	d := New()
	if d.Name() != "baseline_detector" {
		t.Errorf("Expected name 'baseline_detector', got '%s'", d.Name())
	}
}

func TestFirstObservation(t *testing.T) {
	d := New()

	// Create a pattern result with one pattern
	patternResult := &pattern.PatternResult{
		DetectedPatterns: []*pattern.DetectedPattern{
			{
				Pattern:       pattern.PatternRetryStorm,
				Flow:          "checkout",
				AffectedUsers: 50,
				TotalFlows:    100,
				Severity:      pattern.SeverityHigh,
				Evidence: pattern.PatternEvidence{
					Ratio: 0.35,
				},
			},
		},
	}

	result, err := d.Process(patternResult)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	baselineResult, ok := result.(*BaselineResult)
	if !ok {
		t.Fatal("Expected *BaselineResult")
	}

	if len(baselineResult.ChangeResults) != 1 {
		t.Fatalf("Expected 1 change result, got %d", len(baselineResult.ChangeResults))
	}

	// First observation should have unknown trend
	cr := baselineResult.ChangeResults[0]
	if cr.BaselineStatus != BaselineStatusFirstObservation {
		t.Errorf("Expected status 'first_observation', got '%s'", cr.BaselineStatus)
	}
	if cr.Trend != TrendUnknown {
		t.Errorf("Expected trend 'unknown', got '%s'", cr.Trend)
	}
}

func TestInsufficientBaseline(t *testing.T) {
	store := storage.NewInMemoryStorage()
	config := DefaultConfig()
	config.MinBaselineDays = 7

	d := NewWithConfig(config, store)

	// Add only 3 days of baseline data (less than 7 required)
	baseDate := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 1; i <= 3; i++ {
		store.StoreSnapshot(context.Background(), &storage.PatternSnapshot{
			PatternType: "retry_storm",
			Flow:        "checkout",
			ImpactRatio: 0.30,
			Date:        baseDate.AddDate(0, 0, -i),
		})
	}

	patternResult := &pattern.PatternResult{
		DetectedPatterns: []*pattern.DetectedPattern{
			{
				Pattern: pattern.PatternRetryStorm,
				Flow:    "checkout",
				Evidence: pattern.PatternEvidence{
					Ratio: 0.35,
				},
			},
		},
	}

	result, err := d.Process(patternResult)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	baselineResult := result.(*BaselineResult)
	cr := baselineResult.ChangeResults[0]

	if cr.BaselineStatus != BaselineStatusInsufficient {
		t.Errorf("Expected status 'insufficient_data', got '%s'", cr.BaselineStatus)
	}
}

func TestSufficientBaseline(t *testing.T) {
	store := storage.NewInMemoryStorage()
	config := DefaultConfig()
	config.MinBaselineDays = 7

	d := NewWithConfig(config, store)

	// Add 10 days of baseline data
	baseDate := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 1; i <= 10; i++ {
		store.StoreSnapshot(context.Background(), &storage.PatternSnapshot{
			PatternType:    "retry_storm",
			Flow:           "checkout",
			ImpactRatio:    0.30,
			Date:           baseDate.AddDate(0, 0, -i),
			PatternVersion: "v1",
		})
	}

	patternResult := &pattern.PatternResult{
		DetectedPatterns: []*pattern.DetectedPattern{
			{
				Pattern: pattern.PatternRetryStorm,
				Flow:    "checkout",
				Evidence: pattern.PatternEvidence{
					Ratio: 0.35, // 0.05 increase from 0.30 baseline
				},
			},
		},
	}

	result, err := d.Process(patternResult)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	baselineResult := result.(*BaselineResult)
	cr := baselineResult.ChangeResults[0]

	if cr.BaselineStatus != BaselineStatusSufficient {
		t.Errorf("Expected status 'sufficient', got '%s'", cr.BaselineStatus)
	}

	// Baseline should be ~0.30
	if cr.BaselineImpactRatio < 0.29 || cr.BaselineImpactRatio > 0.31 {
		t.Errorf("Expected baseline ratio ~0.30, got %f", cr.BaselineImpactRatio)
	}

	// Delta should be ~0.05
	expectedDelta := 0.05
	if cr.Delta < expectedDelta-0.001 || cr.Delta > expectedDelta+0.001 {
		t.Errorf("Expected delta ~0.05, got %f", cr.Delta)
	}
}

func TestTrendClassification(t *testing.T) {
	d := New()

	tests := []struct {
		deltaPct float64
		expected Trend
	}{
		{0.05, TrendStable},   // 5% - below threshold
		{-0.05, TrendStable},  // -5% - below threshold
		{0.15, TrendIncreasing}, // 15% - above threshold
		{-0.15, TrendDecreasing}, // -15% - above threshold
		{0.0, TrendStable},    // No change
	}

	for _, tt := range tests {
		result := d.classifyTrend(tt.deltaPct)
		if result != tt.expected {
			t.Errorf("classifyTrend(%f): expected %s, got %s", tt.deltaPct, tt.expected, result)
		}
	}
}

func TestSignificanceClassification(t *testing.T) {
	d := New()

	// Test with standard deviation
	baselineWithStd := &BaselineStats{
		Average:           0.30,
		StandardDeviation: 0.05,
		Count:             10,
	}

	// Delta of 0.12 is > 2*0.05 = high significance
	sig := d.classifySignificance(0.12, baselineWithStd)
	if sig != SignificanceHigh {
		t.Errorf("Expected high significance for delta 0.12 with std 0.05, got %s", sig)
	}

	// Delta of 0.05 is < 2*0.05 = medium significance
	sig = d.classifySignificance(0.05, baselineWithStd)
	if sig != SignificanceMedium {
		t.Errorf("Expected medium significance for delta 0.05 with std 0.05, got %s", sig)
	}

	// Test without standard deviation (fallback)
	baselineNoStd := &BaselineStats{
		Average:           0.30,
		StandardDeviation: 0,
		Count:             10,
	}

	// Delta >= 0.15 = high significance
	sig = d.classifySignificance(0.20, baselineNoStd)
	if sig != SignificanceHigh {
		t.Errorf("Expected high significance for delta 0.20 without std, got %s", sig)
	}

	// Delta < 0.15 = medium significance
	sig = d.classifySignificance(0.10, baselineNoStd)
	if sig != SignificanceMedium {
		t.Errorf("Expected medium significance for delta 0.10 without std, got %s", sig)
	}
}

func TestIncreasingTrend(t *testing.T) {
	store := storage.NewInMemoryStorage()
	config := DefaultConfig()
	config.MinBaselineDays = 5

	d := NewWithConfig(config, store)

	// Add baseline with ratio 0.20
	baseDate := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 1; i <= 7; i++ {
		store.StoreSnapshot(context.Background(), &storage.PatternSnapshot{
			PatternType: "confusion_loop",
			Flow:        "onboarding",
			ImpactRatio: 0.20,
			Date:        baseDate.AddDate(0, 0, -i),
		})
	}

	// Current ratio is 0.35 (75% increase)
	patternResult := &pattern.PatternResult{
		DetectedPatterns: []*pattern.DetectedPattern{
			{
				Pattern: pattern.PatternConfusionLoop,
				Flow:    "onboarding",
				Evidence: pattern.PatternEvidence{
					Ratio: 0.35,
				},
			},
		},
	}

	result, err := d.Process(patternResult)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	baselineResult := result.(*BaselineResult)
	cr := baselineResult.ChangeResults[0]

	if cr.Trend != TrendIncreasing {
		t.Errorf("Expected trend 'increasing', got '%s'", cr.Trend)
	}

	// Delta percentage should be 0.75 (75%)
	expectedDeltaPct := 0.75
	if cr.DeltaPercentage < expectedDeltaPct-0.01 || cr.DeltaPercentage > expectedDeltaPct+0.01 {
		t.Errorf("Expected delta percentage ~0.75, got %f", cr.DeltaPercentage)
	}
}

func TestDecreasingTrend(t *testing.T) {
	store := storage.NewInMemoryStorage()
	config := DefaultConfig()
	config.MinBaselineDays = 5

	d := NewWithConfig(config, store)

	// Add baseline with ratio 0.40
	baseDate := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 1; i <= 7; i++ {
		store.StoreSnapshot(context.Background(), &storage.PatternSnapshot{
			PatternType: "retry_storm",
			Flow:        "payment",
			ImpactRatio: 0.40,
			Date:        baseDate.AddDate(0, 0, -i),
		})
	}

	// Current ratio is 0.25 (37.5% decrease)
	patternResult := &pattern.PatternResult{
		DetectedPatterns: []*pattern.DetectedPattern{
			{
				Pattern: pattern.PatternRetryStorm,
				Flow:    "payment",
				Evidence: pattern.PatternEvidence{
					Ratio: 0.25,
				},
			},
		},
	}

	result, err := d.Process(patternResult)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	baselineResult := result.(*BaselineResult)
	cr := baselineResult.ChangeResults[0]

	if cr.Trend != TrendDecreasing {
		t.Errorf("Expected trend 'decreasing', got '%s'", cr.Trend)
	}
}

func TestSnapshotStorage(t *testing.T) {
	d := New()

	patternResult := &pattern.PatternResult{
		DetectedPatterns: []*pattern.DetectedPattern{
			{
				Pattern: pattern.PatternEarlyDropoff,
				Flow:    "signup",
				Evidence: pattern.PatternEvidence{
					Ratio: 0.45,
				},
			},
		},
	}

	// Process should store the snapshot (today's data is within the baseline window)
	_, err := d.Process(patternResult)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify snapshot was stored
	store := d.GetStorage().(*storage.InMemoryStorage)
	snapshots := store.GetAllSnapshots()

	if len(snapshots) != 1 {
		t.Fatalf("Expected 1 snapshot, got %d", len(snapshots))
	}

	snap := snapshots[0]
	if snap.PatternType != "early_dropoff" {
		t.Errorf("Expected pattern type 'early_dropoff', got '%s'", snap.PatternType)
	}
	if snap.Flow != "signup" {
		t.Errorf("Expected flow 'signup', got '%s'", snap.Flow)
	}
	if snap.ImpactRatio != 0.45 {
		t.Errorf("Expected impact ratio 0.45, got %f", snap.ImpactRatio)
	}
}
func TestOutOfWindowData(t *testing.T) {
	d := New()

	patternResult := &pattern.PatternResult{
		DetectedPatterns: []*pattern.DetectedPattern{
			{
				Pattern: pattern.PatternEarlyDropoff,
				Flow:    "signup",
				Evidence: pattern.PatternEvidence{
					Ratio: 0.35,
				},
			},
		},
	}

	// Simulate receiving data that's 60 days old (outside 28-day window)
	oldDate := time.Now().UTC().AddDate(0, 0, -60).Truncate(24 * time.Hour)

	result, err := d.ProcessWithDataDate(patternResult, oldDate)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	baselineResult := result.(*BaselineResult)

	// Verify no snapshot was stored (old data should not be stored)
	store := d.GetStorage().(*storage.InMemoryStorage)
	snapshots := store.GetAllSnapshots()
	if len(snapshots) != 0 {
		t.Fatalf("Expected 0 snapshots for old data, got %d", len(snapshots))
	}

	// Verify the result indicates out-of-window status
	if len(baselineResult.ChangeResults) != 1 {
		t.Fatalf("Expected 1 change result, got %d", len(baselineResult.ChangeResults))
	}

	changeResult := baselineResult.ChangeResults[0]
	if changeResult.BaselineStatus != BaselineStatusOutOfWindow {
		t.Errorf("Expected status 'out_of_window', got '%s'", changeResult.BaselineStatus)
	}
	if changeResult.Trend != TrendUnknown {
		t.Errorf("Expected trend 'unknown', got '%s'", changeResult.Trend)
	}
}