package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/velum/internal/config"
	"github.com/velum/internal/layers"
	"github.com/velum/internal/layers/ai"
	"github.com/velum/internal/layers/baseline"
	"github.com/velum/internal/layers/behavior"
	"github.com/velum/internal/layers/eventadapter"
	"github.com/velum/internal/layers/pattern"
	"github.com/velum/internal/layers/sessionflow"
	"github.com/velum/internal/layers/vocabagent"
	"github.com/velum/internal/storage"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	pipeline     *layers.Pipeline
	storage      storage.Storage
	vocabStorage *vocabagent.SQLiteVocabStorage
	environment  string
}

// NewHandler creates a new handler instance with the provided configuration
func NewHandler(cfg *config.Config) *Handler {
	// Initialize SQLite storage with retention config
	storageConfig := &storage.SQLiteConfig{
		RetentionDays: cfg.Storage.RetentionDays,
	}
	sqliteStorage, err := storage.NewSQLiteStorage(storageConfig)
	if err != nil {
		fmt.Printf("Warning: Failed to initialize SQLite storage, using in-memory: %v\n", err)
		sqliteStorage = nil
	}

	// Start background cleanup goroutine for storage retention
	if sqliteStorage != nil {
		go startStorageCleanup(sqliteStorage, cfg.Storage.RetentionDays)
	}

	// Initialize vocabulary storage and seed with built-in vocabulary
	var vocabStorage *vocabagent.SQLiteVocabStorage
	vocabStorageConfig := vocabagent.DefaultSQLiteVocabConfig()
	vocabStorage, err = vocabagent.NewSQLiteVocabStorage(vocabStorageConfig)
	if err != nil {
		fmt.Printf("Warning: Failed to initialize vocab storage: %v\n", err)
	} else {
		// Seed built-in vocabulary (only runs if storage is empty)
		ctx := context.Background()
		if err := vocabagent.SeedBuiltinVocabulary(ctx, vocabStorage); err != nil {
			fmt.Printf("Warning: Failed to seed vocabulary: %v\n", err)
		}
	}

	// Initialize the pipeline with layers
	pipeline := layers.NewPipeline()

	// Layer 0: Vocab Enricher - discovers unknown words and classifies via AI (optional)
	// Only registered if vocab agent is enabled in config and API key is provided
	if cfg.VocabAgent.Enabled && cfg.VocabAgent.APIKey != "" && vocabStorage != nil {
		// Parse circuit breaker reset timeout
		resetTimeout, err := time.ParseDuration(cfg.Resiliency.CircuitBreaker.ResetTimeout)
		if err != nil {
			resetTimeout = 30 * time.Second
		}

		vocabAgentConfig := &vocabagent.Config{
			Enabled: cfg.VocabAgent.Enabled,
			APIKey:  cfg.VocabAgent.APIKey,
			Model:   cfg.VocabAgent.Model,
			Debug:   cfg.Server.Environment == "development",
			CircuitBreaker: vocabagent.CircuitBreakerConfig{
				Enabled:          cfg.Resiliency.CircuitBreaker.Enabled,
				FailureThreshold: cfg.Resiliency.CircuitBreaker.FailureThreshold,
				ResetTimeout:     resetTimeout,
			},
		}
		vocabAgentInstance := vocabagent.NewWithConfig(vocabAgentConfig)
		enricher := vocabagent.NewVocabEnricher(vocabStorage, vocabAgentInstance, cfg.Server.Environment == "development")
		pipeline.Register(enricher)
		fmt.Println("Vocab Enricher layer enabled with model:", cfg.VocabAgent.Model)
	} else {
		fmt.Println("Vocab Enricher layer disabled (enabled:", cfg.VocabAgent.Enabled, ", api_key set:", cfg.VocabAgent.APIKey != "", ", storage:", vocabStorage != nil, ")")
	}

	// Layer 1: Event Adapter - normalizes raw events
	// Use vocab storage for external lookup if available
	if vocabStorage != nil {
		pipeline.Register(eventadapter.NewWithVocabLookup(vocabStorage))
		fmt.Println("Event Adapter using SQLite vocabulary lookup")
	} else {
		pipeline.Register(eventadapter.New())
	}

	// Layer 2: Session & Flow Reconstructor - groups events into flow instances
	pipeline.Register(sessionflow.New())

	// Layer 3: Behavior Analyzer - detects behavioral patterns
	pipeline.Register(behavior.New())

	// Layer 4: Pattern Detector - recognizes higher-level patterns
	pipeline.Register(pattern.New())

	// Layer 5: Baseline & Change Detector - compares against historical baseline
	baselineConfig := &baseline.Config{
		BaselineWindowDays:          cfg.Baseline.WindowDays,
		MinBaselineDays:             cfg.Baseline.MinDays,
		ComputationMode:             cfg.Baseline.ComputationMode,
		TrendThreshold:              cfg.Baseline.TrendThreshold,
		HighSignificanceThreshold:   cfg.Baseline.HighSignificanceThreshold,
		StandardDeviationMultiplier: cfg.Baseline.StdDeviationMultiplier,
	}

	if sqliteStorage != nil {
		pipeline.Register(baseline.NewWithConfig(baselineConfig, sqliteStorage))
	} else {
		pipeline.Register(baseline.NewWithConfig(baselineConfig, nil))
	}

	// Layer 6: AI Analyzer - provides AI-powered insights (optional)
	// Only registered if AI is enabled in config and API key is provided
	if cfg.AIAnalyzer.Enabled && cfg.AIAnalyzer.APIKey != "" {
		// Parse circuit breaker reset timeout
		resetTimeout, err := time.ParseDuration(cfg.Resiliency.CircuitBreaker.ResetTimeout)
		if err != nil {
			fmt.Printf("Warning: Invalid circuit breaker reset_timeout '%s', using 30s\n", cfg.Resiliency.CircuitBreaker.ResetTimeout)
			resetTimeout = 30 * time.Second
		}

		aiConfig := &ai.Config{
			Enabled: cfg.AIAnalyzer.Enabled,
			APIKey:  cfg.AIAnalyzer.APIKey,
			Model:   cfg.AIAnalyzer.Model,
			Debug:   cfg.Server.Environment == "development",
			CircuitBreaker: ai.CircuitBreakerConfig{
				Enabled:          cfg.Resiliency.CircuitBreaker.Enabled,
				FailureThreshold: cfg.Resiliency.CircuitBreaker.FailureThreshold,
				ResetTimeout:     resetTimeout,
			},
		}
		pipeline.Register(ai.NewWithConfig(aiConfig))
		fmt.Println("AI Analyzer layer enabled with model:", cfg.AIAnalyzer.Model)
		if cfg.Resiliency.CircuitBreaker.Enabled {
			fmt.Printf("   Circuit breaker: threshold=%d, reset=%s\n",
				cfg.Resiliency.CircuitBreaker.FailureThreshold, resetTimeout)
		}
	} else {
		fmt.Println("AI Analyzer layer disabled (enabled:", cfg.AIAnalyzer.Enabled, ", api_key set:", cfg.AIAnalyzer.APIKey != "", ")")
	}

	return &Handler{
		pipeline:     pipeline,
		storage:      sqliteStorage,
		vocabStorage: vocabStorage,
		environment:  cfg.Server.Environment,
	}
}

// startStorageCleanup runs periodic cleanup of old snapshots
func startStorageCleanup(store *storage.SQLiteStorage, retentionDays int) {
	// Run cleanup immediately on startup
	ctx := context.Background()
	if deleted, err := store.Cleanup(ctx); err != nil {
		fmt.Printf("Warning: Storage cleanup failed: %v\n", err)
	} else if deleted > 0 {
		fmt.Printf("Storage cleanup: removed %d old snapshots\n", deleted)
	}

	// Then run every 24 hours
	ticker := time.NewTicker(24 * time.Hour)
	for range ticker.C {
		if deleted, err := store.Cleanup(ctx); err != nil {
			fmt.Printf("Warning: Storage cleanup failed: %v\n", err)
		} else if deleted > 0 {
			fmt.Printf("Storage cleanup: removed %d old snapshots\n", deleted)
		}
	}
}

// Health handles the health check request
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "healthy",
		"service": "velum",
	})
}

// EventRequest represents the incoming raw event data
type EventRequest struct {
	Events []map[string]interface{} `json:"events"`
}

// AnalysisResponse represents the behavioral analysis response
type AnalysisResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Analyze handles the behavioral analysis request
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	var req EventRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, AnalysisResponse{
			Success: false,
			Message: "Invalid request body",
		})
		return
	}

	// Debug mode is enabled in development environment (only affects console logging)
	isDebugMode := h.environment == "development"

	if isDebugMode {
		fmt.Println("[DEBUG] ======= New Analysis Request =======")
		fmt.Printf("[DEBUG] Environment: %s\n", h.environment)
		fmt.Printf("[DEBUG] Events received: %d\n", len(req.Events))
		fmt.Printf("[DEBUG] Pipeline layers: %v\n", h.pipeline.LayerNames())
		fmt.Println("[DEBUG] Starting pipeline execution...")
	}

	// Execute pipeline
	result, err := h.pipeline.Execute(req.Events)
	if err != nil {
		if isDebugMode {
			fmt.Printf("[DEBUG] Pipeline execution failed: %v\n", err)
		}
		respondJSON(w, http.StatusInternalServerError, AnalysisResponse{
			Success: false,
			Message: "Processing failed: " + err.Error(),
		})
		return
	}

	if isDebugMode {
		fmt.Println("[DEBUG] Pipeline execution complete")
		fmt.Println("[DEBUG] =======================================")
	}

	// Build response based on final output type
	responseData := map[string]interface{}{}

	// Check result type and expose appropriate fields
	switch v := result.(type) {
	case *ai.AIResult:
		responseData["ai_enabled"] = v.AIEnabled
		if v.AIEnabled && v.AIAnalysis != nil {
			// AI enabled: show ai_analysis only
			responseData["ai_analysis"] = v.AIAnalysis
		} else {
			// AI disabled: show only change_results with minimal format for first observations
			responseData["data"] = map[string]interface{}{
				"change_results": formatChangeResults(v.ChangeResults),
			}
		}
	case *baseline.BaselineResult:
		// Baseline result without AI: show only change_results with minimal format for first observations
		responseData["ai_enabled"] = false
		responseData["data"] = map[string]interface{}{
			"change_results": formatChangeResults(v.ChangeResults),
		}
	case *pattern.PatternResult:
		responseData["ai_enabled"] = false
		responseData["data"] = map[string]interface{}{
			"analyzed_flows":    v.AnalyzedFlows,
			"detected_patterns": v.DetectedPatterns,
		}
	default:
		responseData["ai_enabled"] = false
		responseData["data"] = result
	}

	respondJSON(w, http.StatusOK, AnalysisResponse{
		Success: true,
		Message: "Behavioral analysis complete",
		Data:    responseData,
	})
}

// respondJSON writes a JSON response
func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// formatChangeResults formats change results for response
// For first_observation, only return minimal data (pattern_type, flow, baseline_available)
// For existing baseline, return full data
func formatChangeResults(changeResults []*baseline.ChangeResult) []map[string]interface{} {
	formatted := make([]map[string]interface{}, 0, len(changeResults))

	for _, change := range changeResults {
		if change.BaselineStatus == baseline.BaselineStatusFirstObservation {
			// First observation: minimal data only
			formatted = append(formatted, map[string]interface{}{
				"pattern_type":       change.PatternType,
				"flow":               change.Flow,
				"baseline_available": false,
			})
		} else {
			// Baseline exists: full data
			formatted = append(formatted, map[string]interface{}{
				"pattern_type":          change.PatternType,
				"flow":                  change.Flow,
				"baseline_available":    true,
				"current_impact_ratio":  change.CurrentImpactRatio,
				"baseline_impact_ratio": change.BaselineImpactRatio,
				"delta":                 change.Delta,
				"delta_percentage":      change.DeltaPercentage,
				"trend":                 change.Trend,
				"change_significance":   change.ChangeSignificance,
				"baseline_status":       change.BaselineStatus,
				"baseline_window":       change.BaselineWindow,
				"baseline_days":         change.BaselineDays,
			})
		}
	}

	return formatted
}
