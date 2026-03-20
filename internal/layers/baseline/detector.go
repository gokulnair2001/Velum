package baseline

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/velum/internal/layers/behavior"
	"github.com/velum/internal/layers/pattern"
	"github.com/velum/internal/storage"
)

// cachedBaselineStats holds cached baseline statistics with a date stamp
type cachedBaselineStats struct {
	stats     *BaselineStats
	cachedFor time.Time // The date this cache is valid for
}

// Detector performs baseline comparison and change detection
type Detector struct {
	config  *Config
	storage storage.Storage

	// Cache for baseline stats, keyed by "projectID:patternType:flow:contextKey"
	statsCache map[string]*cachedBaselineStats
	cacheMu    sync.RWMutex
}

// New creates a new Baseline Detector with default config and in-memory storage
func New() *Detector {
	return &Detector{
		config:     DefaultConfig(),
		storage:    storage.NewInMemoryStorage(),
		statsCache: make(map[string]*cachedBaselineStats),
	}
}

// NewWithStorage creates a Baseline Detector with custom storage
func NewWithStorage(store storage.Storage) *Detector {
	return &Detector{
		config:     DefaultConfig(),
		storage:    store,
		statsCache: make(map[string]*cachedBaselineStats),
	}
}

// NewWithConfig creates a Baseline Detector with custom config and storage
func NewWithConfig(config *Config, store storage.Storage) *Detector {
	if store == nil {
		store = storage.NewInMemoryStorage()
	}
	return &Detector{
		config:     config,
		storage:    store,
		statsCache: make(map[string]*cachedBaselineStats),
	}
}

// Name returns the layer identifier
func (d *Detector) Name() string {
	return "baseline_detector"
}

// Process implements the Layer interface
// Processes data for today's date with no project scoping (default project)
func (d *Detector) Process(input interface{}) (interface{}, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return d.processWithDates(context.Background(), input, today, today, "default", true)
}

// ProcessWithContext implements the ContextAwareLayer interface.
// Extracts ProjectID from analysis context and scopes all storage/cache access.
func (d *Detector) ProcessWithContext(input interface{}, metadata interface{}) (interface{}, error) {
	projectID := "default"
	reqCtx := context.Background()
	updateBaseline := true
	if actx, ok := metadata.(*behavior.AnalysisContext); ok && actx != nil {
		if actx.ProjectID != "" {
			projectID = actx.ProjectID
		}
		reqCtx = actx.RequestContext()
		updateBaseline = actx.UpdateBaseline
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return d.processWithDates(reqCtx, input, today, today, projectID, updateBaseline)
}

// ProcessWithDataDate processes data with a specific data date
// The window is evaluated against the actual current date
// This is useful for processing historical/backfill data
func (d *Detector) ProcessWithDataDate(input interface{}, dataDate time.Time) (interface{}, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return d.processWithDates(context.Background(), input, dataDate.UTC().Truncate(24*time.Hour), today, "default", true)
}

// ProcessWithDate processes input with a specific date (for testing)
// Both data date and "today" are set to the provided date
func (det *Detector) ProcessWithDate(input interface{}, date time.Time) (interface{}, error) {
	d := date.UTC().Truncate(24 * time.Hour)
	return det.processWithDates(context.Background(), input, d, d, "default", true)
}

// processWithDates is the internal method that handles processing with explicit dates
func (d *Detector) processWithDates(ctx context.Context, input interface{}, dataDate time.Time, today time.Time, projectID string, updateBaseline bool) (interface{}, error) {
	switch v := input.(type) {
	case *pattern.PatternResult:
		return d.analyzePatternChanges(ctx, v, dataDate, today, projectID, updateBaseline)
	default:
		return input, nil
	}
}

// analyzePatternChanges processes pattern results and detects changes
func (d *Detector) analyzePatternChanges(ctx context.Context, patternResult *pattern.PatternResult, dataDate time.Time, today time.Time, projectID string, updateBaseline bool) (*BaselineResult, error) {
	var changeResults []*ChangeResult

	for _, detectedPattern := range patternResult.DetectedPatterns {
		// Create current snapshot with the data date and project scope
		currentSnapshot := d.createSnapshot(detectedPattern, dataDate, projectID)

		// Check if snapshot date is within baseline window (relative to today)
		windowStatus := d.checkBaselineWindow(currentSnapshot.Date, today)

		var changeResult *ChangeResult

		switch windowStatus {
		case windowStatusTooOld:
			// Data is older than baseline window - skip comparison and storage
			changeResult = d.markOutOfWindow(currentSnapshot)

		case windowStatusWithinWindow, windowStatusToday:
			// Data is within baseline window (including today) - compare and optionally store
			changeResult = d.analyzePatternChange(ctx, currentSnapshot, projectID)
			if updateBaseline {
				if err := d.storage.StoreSnapshot(ctx, currentSnapshot); err != nil {
					slog.Warn("failed to store snapshot", "error", err)
				}
			}
		}

		changeResults = append(changeResults, changeResult)
	}

	return &BaselineResult{
		AnalyzedFlows:    patternResult.AnalyzedFlows,
		DetectedPatterns: patternResult.DetectedPatterns,
		ChangeResults:    changeResults,
		SnapshotDate:     dataDate,
	}, nil
}

// windowStatus represents where a date falls relative to baseline window
type windowStatus int

const (
	windowStatusTooOld       windowStatus = iota // Older than baseline window
	windowStatusWithinWindow                     // Within baseline window (not today)
	windowStatusToday                            // Today's date
)

// checkBaselineWindow determines if a date falls within the baseline window
func (d *Detector) checkBaselineWindow(snapshotDate time.Time, today time.Time) windowStatus {
	windowStart := today.AddDate(0, 0, -d.config.BaselineWindowDays)

	snapshotDay := snapshotDate.Truncate(24 * time.Hour)

	// Check if it's today
	if snapshotDay.Equal(today) {
		return windowStatusToday
	}

	// Check if it's too old (before window start)
	if snapshotDay.Before(windowStart) {
		return windowStatusTooOld
	}

	// Within window (between windowStart and yesterday)
	return windowStatusWithinWindow
}

// markOutOfWindow returns a result for data that's older than baseline window
func (d *Detector) markOutOfWindow(snapshot *storage.PatternSnapshot) *ChangeResult {
	return &ChangeResult{
		PatternType:         snapshot.PatternType,
		Flow:                snapshot.Flow,
		ContextKey:          snapshot.ContextKey,
		CurrentImpactRatio:  snapshot.ImpactRatio,
		BaselineImpactRatio: 0,
		Delta:               0,
		DeltaPercentage:     0,
		Trend:               TrendUnknown,
		ChangeSignificance:  SignificanceLow,
		BaselineStatus:      BaselineStatusOutOfWindow,
		BaselineWindow:      fmt.Sprintf("last_%d_days", d.config.BaselineWindowDays),
		BaselineDays:        0,
	}
}

// createSnapshot converts a detected pattern to a storable snapshot
func (d *Detector) createSnapshot(dp *pattern.DetectedPattern, date time.Time, projectID string) *storage.PatternSnapshot {
	return &storage.PatternSnapshot{
		ProjectID:      projectID,
		PatternType:    string(dp.Pattern),
		Flow:           dp.Flow,
		ContextKey:     dp.ContextKey,
		ImpactRatio:    dp.Evidence.Ratio,
		AffectedUsers:  dp.AffectedUsers,
		TotalFlows:     dp.TotalFlows,
		Severity:       string(dp.Severity),
		Date:           date,
		PatternVersion: d.config.PatternVersion,
	}
}

// cacheKey generates a cache key for a project+pattern+flow+context combination
func cacheKey(projectID, patternType, flow, contextKey string) string {
	return projectID + ":" + patternType + ":" + flow + ":" + contextKey
}

// getCachedStats returns cached baseline stats if valid for today, otherwise nil
func (d *Detector) getCachedStats(projectID, patternType, flow, contextKey string, today time.Time) *BaselineStats {
	d.cacheMu.RLock()
	defer d.cacheMu.RUnlock()

	key := cacheKey(projectID, patternType, flow, contextKey)
	cached, exists := d.statsCache[key]
	if !exists {
		return nil
	}

	// Check if cache is still valid (same day)
	if cached.cachedFor.Equal(today) {
		return cached.stats
	}

	return nil
}

// setCachedStats stores baseline stats in cache
func (d *Detector) setCachedStats(projectID, patternType, flow, contextKey string, stats *BaselineStats, today time.Time) {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()

	key := cacheKey(projectID, patternType, flow, contextKey)
	d.statsCache[key] = &cachedBaselineStats{
		stats:     stats,
		cachedFor: today,
	}
}

// analyzePatternChange compares a pattern snapshot against baseline
func (d *Detector) analyzePatternChange(ctx context.Context, currentSnapshot *storage.PatternSnapshot, projectID string) *ChangeResult {
	today := currentSnapshot.Date.Truncate(24 * time.Hour)

	// Check computation mode - use cache only in "daily" mode
	if d.config.ComputationMode == "daily" {
		// Try to get cached baseline stats first
		if cachedStats := d.getCachedStats(projectID, currentSnapshot.PatternType, currentSnapshot.Flow, currentSnapshot.ContextKey, today); cachedStats != nil {
			// Use cached stats - no need to fetch from storage
			if cachedStats.Count < d.config.MinBaselineDays {
				status := BaselineStatusInsufficient
				if cachedStats.Count == 0 {
					status = BaselineStatusFirstObservation
				}
				return d.markInsufficientBaseline(currentSnapshot, status)
			}
			return d.compareWithBaseline(currentSnapshot, cachedStats)
		}
	}

	// Fetch baseline snapshots from storage (always in "always" mode, or cache miss in "daily" mode)
	baselineSnapshots, err := d.storage.FetchBaselineSnapshots(
		ctx,
		projectID,
		currentSnapshot.PatternType,
		currentSnapshot.Flow,
		currentSnapshot.ContextKey,
		currentSnapshot.Date,
		d.config.BaselineWindowDays,
	)

	if err != nil {
		// On error, return unknown state
		return d.markInsufficientBaseline(currentSnapshot, BaselineStatusInsufficient)
	}

	// Compute baseline statistics
	baselineStats := d.computeBaselineStats(baselineSnapshots)

	// Cache the computed stats for today (only in "daily" mode)
	if d.config.ComputationMode == "daily" {
		d.setCachedStats(projectID, currentSnapshot.PatternType, currentSnapshot.Flow, currentSnapshot.ContextKey, baselineStats, today)
	}

	// Check for cold start / insufficient data
	if baselineStats.Count < d.config.MinBaselineDays {
		status := BaselineStatusInsufficient
		if baselineStats.Count == 0 {
			status = BaselineStatusFirstObservation
		}
		return d.markInsufficientBaseline(currentSnapshot, status)
	}

	// Compare current vs baseline
	return d.compareWithBaseline(currentSnapshot, baselineStats)
}

// markInsufficientBaseline returns a result when baseline data is insufficient
func (d *Detector) markInsufficientBaseline(snapshot *storage.PatternSnapshot, status BaselineStatus) *ChangeResult {
	return &ChangeResult{
		PatternType:         snapshot.PatternType,
		Flow:                snapshot.Flow,
		ContextKey:          snapshot.ContextKey,
		CurrentImpactRatio:  snapshot.ImpactRatio,
		BaselineImpactRatio: 0,
		Delta:               0,
		DeltaPercentage:     0,
		Trend:               TrendUnknown,
		ChangeSignificance:  SignificanceLow,
		BaselineStatus:      status,
		BaselineWindow:      fmt.Sprintf("last_%d_days", d.config.BaselineWindowDays),
		BaselineDays:        0,
	}
}

// computeBaselineStats calculates average and standard deviation from snapshots
func (d *Detector) computeBaselineStats(snapshots []*storage.PatternSnapshot) *BaselineStats {
	if len(snapshots) == 0 {
		return &BaselineStats{Average: 0, StandardDeviation: 0, Count: 0}
	}

	// Extract impact ratios
	values := make([]float64, len(snapshots))
	for i, snap := range snapshots {
		values[i] = snap.ImpactRatio
	}

	// Calculate average
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	avg := sum / float64(len(values))

	// Calculate standard deviation
	varianceSum := 0.0
	for _, v := range values {
		diff := v - avg
		varianceSum += diff * diff
	}
	std := math.Sqrt(varianceSum / float64(len(values)))

	return &BaselineStats{
		Average:           avg,
		StandardDeviation: std,
		Count:             len(values),
	}
}

// compareWithBaseline compares current snapshot against baseline statistics
func (d *Detector) compareWithBaseline(current *storage.PatternSnapshot, baseline *BaselineStats) *ChangeResult {
	delta := current.ImpactRatio - baseline.Average

	// Avoid division by zero
	var deltaPct float64
	if baseline.Average > 0 {
		deltaPct = delta / baseline.Average
	} else if current.ImpactRatio > 0 {
		deltaPct = 1.0 // 100% increase from zero baseline
	}

	trend := d.classifyTrend(deltaPct)
	significance := d.classifySignificance(delta, baseline)

	// Cap significance at low when affected user count is below threshold.
	// The pattern is still reported (the signal is real) but shouldn't
	// trigger high-priority alerts on statistically thin data.
	lowVolume := d.config.MinAffectedUsers > 0 && current.AffectedUsers < d.config.MinAffectedUsers
	if lowVolume && significance != SignificanceLow {
		significance = SignificanceLow
	}

	return &ChangeResult{
		PatternType:         current.PatternType,
		Flow:                current.Flow,
		ContextKey:          current.ContextKey,
		CurrentImpactRatio:  current.ImpactRatio,
		BaselineImpactRatio: baseline.Average,
		Delta:               delta,
		DeltaPercentage:     deltaPct,
		Trend:               trend,
		ChangeSignificance:  significance,
		LowVolume:           lowVolume,
		BaselineStatus:      BaselineStatusSufficient,
		BaselineWindow:      fmt.Sprintf("last_%d_days", d.config.BaselineWindowDays),
		BaselineDays:        baseline.Count,
	}
}

// classifyTrend determines the direction of change
func (d *Detector) classifyTrend(deltaPct float64) Trend {
	if math.Abs(deltaPct) < d.config.TrendThreshold {
		return TrendStable
	}

	if deltaPct > 0 {
		return TrendIncreasing
	}

	return TrendDecreasing
}

// classifySignificance determines how significant the change is
func (d *Detector) classifySignificance(delta float64, baseline *BaselineStats) Significance {
	// If we have standard deviation, use it
	if baseline.StandardDeviation > 0 {
		if math.Abs(delta) >= d.config.StandardDeviationMultiplier*baseline.StandardDeviation {
			return SignificanceHigh
		}
		return SignificanceMedium
	}

	// Fallback without standard deviation
	if math.Abs(delta) >= d.config.HighSignificanceThreshold {
		return SignificanceHigh
	}

	return SignificanceMedium
}

// GetConfig returns the current configuration
func (d *Detector) GetConfig() *Config {
	return d.config
}

// GetStorage returns the storage instance
func (d *Detector) GetStorage() storage.Storage {
	return d.storage
}
