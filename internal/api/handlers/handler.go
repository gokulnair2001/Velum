package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/velum/internal/canonical"
	"github.com/velum/internal/config"
	"github.com/velum/internal/layers"
	"github.com/velum/internal/layers/ai"
	"github.com/velum/internal/layers/baseline"
	"github.com/velum/internal/layers/behavior"
	"github.com/velum/internal/layers/eventadapter"
	"github.com/velum/internal/layers/pattern"
	"github.com/velum/internal/layers/propertyagent"
	"github.com/velum/internal/layers/sessionflow"
	"github.com/velum/internal/layers/vocabagent"
	"github.com/velum/internal/storage"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	pipeline        *layers.Pipeline
	storage         storage.Storage
	vocabStorage    *vocabagent.PostgresVocabStorage
	propertyStorage *propertyagent.PostgresPropertyStorage
	environment     string
}

// NewHandler creates a new handler instance with the provided configuration
func NewHandler(cfg *config.Config) *Handler {
	// Initialize storage using factory (Postgres based on config)
	storageInstance, err := storage.NewStorage(&cfg.Storage)

	if err != nil {
		fmt.Printf("❌ Storage initialization failed\n")
		fmt.Printf("   Backend: PostgreSQL\n")
		fmt.Printf("   Host: %s:%d\n", cfg.Storage.Postgres.Host, cfg.Storage.Postgres.Port)
		fmt.Printf("   Database: %s\n", cfg.Storage.Postgres.Database)
		fmt.Printf("   Error: %v\n", err)
		storageInstance = nil
	} else {
		// Log which storage backend is being used
		fmt.Printf("✅ Storage connection successful\n")
		fmt.Printf("   Backend: PostgreSQL\n")
		fmt.Printf("   Host: %s:%d\n", cfg.Storage.Postgres.Host, cfg.Storage.Postgres.Port)
		fmt.Printf("   Database: %s\n", cfg.Storage.Postgres.Database)
		fmt.Printf("   User: %s\n", cfg.Storage.Postgres.User)
		fmt.Printf("   SSL Mode: %s\n", cfg.Storage.Postgres.SSLMode)
		fmt.Printf("   Max Connections: %d\n", cfg.Storage.Postgres.MaxConnections)
		fmt.Printf("   Retention: %d days\n", cfg.Storage.RetentionDays)
	}

	// Start background cleanup goroutine for storage retention
	if storageInstance != nil {
		go startStorageCleanup(storageInstance, cfg.Storage.RetentionDays)
	}

	// Initialize vocabulary storage using the same PostgreSQL database
	var vocabStorage *vocabagent.PostgresVocabStorage
	vocabStorage, err = vocabagent.NewPostgresVocabStorage(&cfg.Storage.Postgres)
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

	// Initialize property storage for context agent (same PostgreSQL database)
	var propertyStorage *propertyagent.PostgresPropertyStorage
	propertyStorage, err = propertyagent.NewPostgresPropertyStorage(&cfg.Storage.Postgres)
	if err != nil {
		fmt.Printf("Warning: Failed to initialize property storage: %v\n", err)
	} else {
		// Seed built-in dimensions (only runs if storage is empty)
		ctx := context.Background()
		if err := propertyagent.SeedBuiltinDimensions(ctx, propertyStorage); err != nil {
			fmt.Printf("Warning: Failed to seed property registry: %v\n", err)
		}
	}

	// Layer 0: Context Enricher - classifies event properties into dimensions/targets/conditions/measures
	// Dimensions resolved by built-in list, measures by type inference, target vs condition by AI
	if cfg.ContextAgent.Enabled && cfg.ContextAgent.APIKey != "" && propertyStorage != nil {
		resetTimeout, err := time.ParseDuration(cfg.Resiliency.CircuitBreaker.ResetTimeout)
		if err != nil {
			resetTimeout = 30 * time.Second
		}

		contextAgentConfig := &propertyagent.Config{
			Enabled: cfg.ContextAgent.Enabled,
			APIKey:  cfg.ContextAgent.APIKey,
			Model:   cfg.ContextAgent.Model,
			Debug:   cfg.Server.Environment == "development",
			CircuitBreaker: propertyagent.CircuitBreakerConfig{
				Enabled:          cfg.Resiliency.CircuitBreaker.Enabled,
				FailureThreshold: cfg.Resiliency.CircuitBreaker.FailureThreshold,
				ResetTimeout:     resetTimeout,
			},
		}
		contextAgent := propertyagent.NewAgentWithConfig(contextAgentConfig)
		contextEnricher := propertyagent.NewContextEnricher(propertyStorage, contextAgent, cfg.Server.Environment == "development")
		pipeline.Register(contextEnricher)
		fmt.Println("Context Enricher layer enabled with model:", cfg.ContextAgent.Model)
	} else {
		fmt.Println("Context Enricher layer disabled (enabled:", cfg.ContextAgent.Enabled, ", api_key set:", cfg.ContextAgent.APIKey != "", ", storage:", propertyStorage != nil, ")")
	}

	// Layer 1: Vocab Enricher - discovers unknown words and classifies via AI (optional)
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

	// Layer 2: Event Adapter - normalizes raw events and builds canonical context
	// Uses vocab storage for word categorization and property storage for context building
	if vocabStorage != nil && propertyStorage != nil {
		pipeline.Register(eventadapter.NewWithLookups(vocabStorage, propertyStorage))
		fmt.Println("Event Adapter using PostgreSQL vocabulary + property registry lookup")
	} else if vocabStorage != nil {
		pipeline.Register(eventadapter.NewWithVocabLookup(vocabStorage))
		fmt.Println("Event Adapter using PostgreSQL vocabulary lookup")
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

	if storageInstance != nil {
		pipeline.Register(baseline.NewWithConfig(baselineConfig, storageInstance))
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
		pipeline:        pipeline,
		storage:         storageInstance,
		vocabStorage:    vocabStorage,
		propertyStorage: propertyStorage,
		environment:     cfg.Server.Environment,
	}
}

// startStorageCleanup runs periodic cleanup of old snapshots
func startStorageCleanup(store storage.Storage, retentionDays int) {
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

	// Check recommended properties and collect warnings
	warnings := canonical.CheckRecommendedProperties(req.Events)

	if isDebugMode {
		fmt.Println("[DEBUG] ======= New Analysis Request =======")
		fmt.Printf("[DEBUG] Environment: %s\n", h.environment)
		fmt.Printf("[DEBUG] Events received: %d\n", len(req.Events))
		fmt.Printf("[DEBUG] Pipeline layers: %v\n", h.pipeline.LayerNames())
		if len(warnings) > 0 {
			for _, w := range warnings {
				fmt.Printf("[DEBUG] [WARNING] %s\n", w)
			}
		}
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
			// AI enabled: show ai_analysis only (no data object)
			responseData["ai_analysis"] = v.AIAnalysis
		} else {
			// AI disabled: show enriched change_results
			responseData["data"] = map[string]interface{}{
				"change_results": formatChangeResults(v.ChangeResults, v.DetectedPatterns, v.AnalyzedFlows),
			}
		}
	case *baseline.BaselineResult:
		// Baseline result without AI: show enriched change_results
		responseData["ai_enabled"] = false
		responseData["data"] = map[string]interface{}{
			"change_results": formatChangeResults(v.ChangeResults, v.DetectedPatterns, v.AnalyzedFlows),
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

	// Include warnings in response if any recommended properties are missing
	if len(warnings) > 0 {
		responseData["warnings"] = warnings
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

// formatChangeResults formats change results for response, enriched with
// pattern evidence (severity, confidence, affected_users) and a per-flow
// context breakdown (dimensional/conditional distribution across flows).
func formatChangeResults(changeResults []*baseline.ChangeResult, detectedPatterns interface{}, analyzedFlows interface{}) []map[string]interface{} {
	// Build lookup maps for enrichment
	evidenceMap := buildEvidenceMap(detectedPatterns)
	contextMap := buildFlowContextMap(analyzedFlows)

	formatted := make([]map[string]interface{}, 0, len(changeResults))

	for _, change := range changeResults {
		var entry map[string]interface{}

		if change.BaselineStatus == baseline.BaselineStatusFirstObservation {
			// First observation: minimal data only
			entry = map[string]interface{}{
				"pattern_type":       change.PatternType,
				"flow":               change.Flow,
				"baseline_available": false,
			}
		} else {
			// Baseline exists: full data
			entry = map[string]interface{}{
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
			}
		}

		if change.ContextKey != "" {
			entry["context_key"] = change.ContextKey
		}

		// Enrich with pattern evidence
		key := change.PatternType + ":" + change.Flow
		if ev, ok := evidenceMap[key]; ok {
			entry["severity"] = ev.Severity
			entry["confidence"] = ev.Confidence
			entry["affected_users"] = ev.AffectedUsers
			entry["total_flows"] = ev.TotalFlows
			entry["impact_ratio"] = ev.Evidence.Ratio
			if ev.Evidence.Description != "" {
				entry["evidence"] = ev.Evidence.Description
			}
		}

		// Enrich with context breakdown for this flow
		if ctx, ok := contextMap[change.Flow]; ok && len(ctx) > 0 {
			entry["context"] = ctx
		}

		formatted = append(formatted, entry)
	}

	return formatted
}

// buildEvidenceMap creates a lookup of "patternType:flow" -> DetectedPattern.
func buildEvidenceMap(detected interface{}) map[string]*pattern.DetectedPattern {
	result := make(map[string]*pattern.DetectedPattern)
	patterns, ok := detected.([]*pattern.DetectedPattern)
	if !ok {
		return result
	}
	for _, p := range patterns {
		key := string(p.Pattern) + ":" + p.Flow
		result[key] = p
	}
	return result
}

// buildFlowContextMap aggregates context properties per flow from analyzed flows.
// Returns flow -> property -> value -> count.
func buildFlowContextMap(analyzedFlows interface{}) map[string]map[string]map[string]int {
	result := make(map[string]map[string]map[string]int)
	flows, ok := analyzedFlows.([]*behavior.AnalyzedFlow)
	if !ok {
		return result
	}
	for _, f := range flows {
		if f.Context == nil {
			continue
		}
		props, exists := result[f.Flow]
		if !exists {
			props = make(map[string]map[string]int)
			result[f.Flow] = props
		}
		for k, v := range f.Context.Dimensions {
			if props[k] == nil {
				props[k] = make(map[string]int)
			}
			props[k][v]++
		}
		for k, v := range f.Context.Conditions {
			if props[k] == nil {
				props[k] = make(map[string]int)
			}
			props[k][fmt.Sprintf("%v", v)]++
		}
		for k, v := range f.Context.Targets {
			if props[k] == nil {
				props[k] = make(map[string]int)
			}
			props[k][fmt.Sprintf("%v", v)]++
		}
	}
	return result
}
