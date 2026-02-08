package pattern

// PatternType represents the type of pattern detected
type PatternType string

const (
	PatternRetryStorm         PatternType = "retry_storm"
	PatternConfusionLoop      PatternType = "confusion_loop"
	PatternSilentAbandonment  PatternType = "silent_abandonment"
	PatternEarlyDropoff       PatternType = "early_dropoff"
	PatternBypassBehavior     PatternType = "bypass_behavior"
	PatternMaskedFailure      PatternType = "masked_failure"
)

// Severity represents the severity level of a pattern
type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
)

// Confidence represents confidence level
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// DetectedPattern represents a pattern found in the behavioral data
type DetectedPattern struct {
	Pattern       PatternType       `json:"pattern"`
	Flow          string            `json:"flow"`
	AffectedUsers int               `json:"affected_users"`
	TotalFlows    int               `json:"total_flows"`
	Severity      Severity          `json:"severity"`
	Confidence    Confidence        `json:"confidence"`
	Evidence      PatternEvidence   `json:"evidence"`
}

// PatternEvidence provides supporting data for the detected pattern
type PatternEvidence struct {
	MatchingFlows   int      `json:"matching_flows"`
	Ratio           float64  `json:"ratio"`
	Description     string   `json:"description"`
	SampleFlowIDs   []string `json:"sample_flow_ids,omitempty"`
}

// Config holds configuration for pattern detection
type Config struct {
	// MinSampleSize is the minimum number of flows required to detect patterns
	MinSampleSize int

	// RetryStormThreshold is the ratio of retry flows to trigger retry_storm
	RetryStormThreshold float64

	// SilentAbandonmentThreshold is the minimum count for silent abandonment
	SilentAbandonmentThreshold int

	// EarlyDropoffThreshold is the ratio for early dropoff detection
	EarlyDropoffThreshold float64

	// BypassThreshold is the minimum count for bypass behavior
	BypassThreshold int

	// MaskedFailureThreshold is the minimum count for masked failure
	MaskedFailureThreshold int

	// ConfusionLoopThreshold is the ratio of hesitation for confusion loop
	ConfusionLoopThreshold float64

	// HighSeverityUserCount is the user count threshold for high severity
	HighSeverityUserCount int

	// MediumSeverityUserCount is the user count threshold for medium severity
	MediumSeverityUserCount int
}

// DefaultConfig returns the default pattern detection configuration
func DefaultConfig() *Config {
	return &Config{
		MinSampleSize:              3,
		RetryStormThreshold:        0.3,
		SilentAbandonmentThreshold: 2,
		EarlyDropoffThreshold:      0.4,
		BypassThreshold:            2,
		MaskedFailureThreshold:     2,
		ConfusionLoopThreshold:     0.3,
		HighSeverityUserCount:      100,
		MediumSeverityUserCount:    20,
	}
}
