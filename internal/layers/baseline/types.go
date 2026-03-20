package baseline

import "time"

// Trend represents the direction of change
type Trend string

const (
	TrendIncreasing Trend = "increasing"
	TrendDecreasing Trend = "decreasing"
	TrendStable     Trend = "stable"
	TrendUnknown    Trend = "unknown"
)

// Significance represents the significance level of a change
type Significance string

const (
	SignificanceHigh   Significance = "high"
	SignificanceMedium Significance = "medium"
	SignificanceLow    Significance = "low"
)

// BaselineStatus represents the status of baseline data
type BaselineStatus string

const (
	BaselineStatusSufficient       BaselineStatus = "sufficient"
	BaselineStatusInsufficient     BaselineStatus = "insufficient_data"
	BaselineStatusFirstObservation BaselineStatus = "first_observation"
	BaselineStatusOutOfWindow      BaselineStatus = "out_of_window"
)

// BaselineStats holds computed baseline statistics
type BaselineStats struct {
	Average           float64 `json:"average"`
	StandardDeviation float64 `json:"standard_deviation"`
	Count             int     `json:"count"`
}

// ChangeResult represents the result of comparing a pattern against baseline
type ChangeResult struct {
	PatternType         string         `json:"pattern_type"`
	Flow                string         `json:"flow"`
	ContextKey          string         `json:"context_key"`
	CurrentImpactRatio  float64        `json:"current_impact_ratio"`
	BaselineImpactRatio float64        `json:"baseline_impact_ratio"`
	Delta               float64        `json:"delta"`
	DeltaPercentage     float64        `json:"delta_percentage"`
	Trend               Trend          `json:"trend"`
	ChangeSignificance  Significance   `json:"change_significance"`
	LowVolume           bool           `json:"low_volume,omitempty"`
	BaselineStatus      BaselineStatus `json:"baseline_status"`
	BaselineWindow      string         `json:"baseline_window"`
	BaselineDays        int            `json:"baseline_days"`
}

// BaselineResult contains the output of the baseline layer
type BaselineResult struct {
	AnalyzedFlows    interface{}     `json:"analyzed_flows"`
	DetectedPatterns interface{}     `json:"detected_patterns"`
	ChangeResults    []*ChangeResult `json:"change_results"`
	SnapshotDate     time.Time       `json:"snapshot_date"`
}

// Config holds configuration for baseline detection
type Config struct {
	// BaselineWindowDays is the number of days to consider for baseline
	BaselineWindowDays int

	// MinBaselineDays is the minimum days required to compute baseline
	MinBaselineDays int

	// ComputationMode determines how often to compute baseline: "daily" or "always"
	ComputationMode string

	// TrendThreshold is the minimum delta percentage to classify as increasing/decreasing
	TrendThreshold float64

	// HighSignificanceThreshold is the delta threshold for high significance (without std)
	HighSignificanceThreshold float64

	// StandardDeviationMultiplier is used when std is available (default: 2.0)
	StandardDeviationMultiplier float64

	// MinAffectedUsers is the minimum user count for high/medium significance.
	// Patterns below this are still reported but capped at SignificanceLow.
	MinAffectedUsers int

	// PatternVersion for versioning pattern detection logic
	PatternVersion string
}

// DefaultConfig returns the default baseline configuration
func DefaultConfig() *Config {
	return &Config{
		BaselineWindowDays:          28,
		MinBaselineDays:             7,
		ComputationMode:             "daily",
		TrendThreshold:              0.10, // 10% change threshold
		HighSignificanceThreshold:   0.15, // 15% delta for high significance
		StandardDeviationMultiplier: 2.0,
		MinAffectedUsers:            5,
		PatternVersion:              "v1",
	}
}
