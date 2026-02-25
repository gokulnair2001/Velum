package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/velum/internal/canonical"
	"github.com/velum/internal/config"
	"github.com/velum/internal/layers"
	"github.com/velum/internal/layers/aggregation"
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
	reportBuilder   *aggregation.ReportBuilder
	storage         storage.Storage
	vocabStorage    *vocabagent.PostgresVocabStorage
	propertyStorage *propertyagent.PostgresPropertyStorage
	environment     string
	cleanupCancel   context.CancelFunc // cancels the background cleanup goroutine
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
		fmt.Println("\n🛑 Cannot start without a working database. Fix the connection and retry.")
		os.Exit(1)
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
	var cleanupCancel context.CancelFunc
	if storageInstance != nil {
		var cleanupCtx context.Context
		cleanupCtx, cleanupCancel = context.WithCancel(context.Background())
		go startStorageCleanup(cleanupCtx, storageInstance, cfg.Storage.RetentionDays)
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
		reportBuilder:   aggregation.NewReportBuilder(),
		storage:         storageInstance,
		vocabStorage:    vocabStorage,
		propertyStorage: propertyStorage,
		environment:     cfg.Server.Environment,
		cleanupCancel:   cleanupCancel,
	}
}

// startStorageCleanup runs periodic cleanup of old snapshots.
// It stops when ctx is cancelled (server shutdown).
func startStorageCleanup(ctx context.Context, store storage.Storage, retentionDays int) {
	// Run cleanup immediately on startup
	if deleted, err := store.Cleanup(ctx); err != nil {
		fmt.Printf("Warning: Storage cleanup failed: %v\n", err)
	} else if deleted > 0 {
		fmt.Printf("Storage cleanup: removed %d old snapshots\n", deleted)
	}

	// Then run every 24 hours, stopping when context is cancelled
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if deleted, err := store.Cleanup(ctx); err != nil {
				if ctx.Err() != nil {
					return // shutting down
				}
				fmt.Printf("Warning: Storage cleanup failed: %v\n", err)
			} else if deleted > 0 {
				fmt.Printf("Storage cleanup: removed %d old snapshots\n", deleted)
			}
		}
	}
}

// Close stops the background cleanup goroutine and closes all storage
// connections. It should be called during graceful shutdown.
func (h *Handler) Close() error {
	// Stop cleanup goroutine
	if h.cleanupCancel != nil {
		h.cleanupCancel()
	}

	var firstErr error
	if h.storage != nil {
		if err := h.storage.Close(); err != nil {
			fmt.Printf("Warning: Failed to close storage: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if h.vocabStorage != nil {
		if err := h.vocabStorage.Close(); err != nil {
			fmt.Printf("Warning: Failed to close vocab storage: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if h.propertyStorage != nil {
		if err := h.propertyStorage.Close(); err != nil {
			fmt.Printf("Warning: Failed to close property storage: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Health handles the health check request.
// Checks database connectivity and returns degraded status if DB is unreachable.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	result := map[string]interface{}{
		"service": "velum",
	}

	// Check database connectivity
	if h.storage != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := h.storage.Ping(ctx); err != nil {
			result["status"] = "degraded"
			result["db"] = "unreachable"
			respondJSON(w, http.StatusServiceUnavailable, result)
			return
		}
		result["db"] = "connected"
	}

	result["status"] = "healthy"
	respondJSON(w, http.StatusOK, result)
}

// EventRequest represents the incoming raw event data
type EventRequest struct {
	Events          []map[string]interface{}  `json:"events"`
	AnalysisContext *behavior.AnalysisContext `json:"analysis_context,omitempty"`
}

// AnalysisResponse represents the behavioral analysis response
type AnalysisResponse struct {
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	RequestID string      `json:"request_id,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

// maxRequestBodySize is the maximum allowed request body size (10MB)
const maxRequestBodySize = 10 * 1024 * 1024

// ProjectIDHeader is the HTTP header that identifies the project.
const ProjectIDHeader = "X-Project-ID"

// validProjectID matches alphanumeric, hyphens, underscores (1-64 chars)
var validProjectID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Analyze handles the behavioral analysis request
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent OOM
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	// Extract and validate project ID from header
	projectID := r.Header.Get(ProjectIDHeader)
	if projectID == "" {
		respondJSON(w, http.StatusBadRequest, AnalysisResponse{
			Success: false,
			Message: "Missing required header 'X-Project-ID'. Each request must identify the project.",
		})
		return
	}
	if !validProjectID.MatchString(projectID) {
		respondJSON(w, http.StatusBadRequest, AnalysisResponse{
			Success: false,
			Message: "Invalid 'X-Project-ID'. Must be 1-64 alphanumeric characters, hyphens, or underscores.",
		})
		return
	}

	var req EventRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			respondJSON(w, http.StatusRequestEntityTooLarge, AnalysisResponse{
				Success: false,
				Message: "Request body too large. Maximum size is 10MB.",
			})
			return
		}
		respondJSON(w, http.StatusBadRequest, AnalysisResponse{
			Success: false,
			Message: "Invalid request body: malformed JSON.",
		})
		return
	}

	// Validate minimum input: at least 1 event required
	if len(req.Events) == 0 {
		respondJSON(w, http.StatusBadRequest, AnalysisResponse{
			Success: false,
			Message: "At least 1 event is required in the 'events' array.",
		})
		return
	}

	// Validate required fields on events
	for i, evt := range req.Events {
		if _, ok := evt["event"]; !ok {
			respondJSON(w, http.StatusBadRequest, AnalysisResponse{
				Success: false,
				Message: fmt.Sprintf("Event at index %d is missing required field 'event'.", i),
			})
			return
		}
		if _, ok := evt["user_id"]; !ok {
			respondJSON(w, http.StatusBadRequest, AnalysisResponse{
				Success: false,
				Message: fmt.Sprintf("Event at index %d is missing required field 'user_id'.", i),
			})
			return
		}
		if _, ok := evt["ts"]; !ok {
			respondJSON(w, http.StatusBadRequest, AnalysisResponse{
				Success: false,
				Message: fmt.Sprintf("Event at index %d is missing required field 'ts'.", i),
			})
			return
		}
	}

	// Debug mode is enabled in development environment (only affects console logging)
	isDebugMode := h.environment == "development"

	// Resolve analysis context (use default if not provided)
	analysisCtx := req.AnalysisContext
	if analysisCtx == nil {
		analysisCtx = behavior.DefaultAnalysisContext()
	}
	// Always set ProjectID from the HTTP header (overrides any body value)
	analysisCtx.ProjectID = projectID
	// Carry the HTTP request context through the pipeline so layers can
	// cancel LLM / DB calls when the client disconnects.
	analysisCtx.Ctx = r.Context()

	// Check recommended properties and collect warnings
	warnings := canonical.CheckRecommendedProperties(req.Events)

	if isDebugMode {
		fmt.Println("[DEBUG] ======= New Analysis Request =======")
		fmt.Printf("[DEBUG] Environment: %s\n", h.environment)
		fmt.Printf("[DEBUG] Events received: %d\n", len(req.Events))
		fmt.Printf("[DEBUG] Analysis scope: %s\n", analysisCtx.Scope)
		if len(analysisCtx.FunnelDefinitions) > 0 {
			fmt.Printf("[DEBUG] Funnel definitions: %d\n", len(analysisCtx.FunnelDefinitions))
		}
		if len(analysisCtx.FlowConfigs) > 0 {
			fmt.Printf("[DEBUG] Flow configs: %d\n", len(analysisCtx.FlowConfigs))
		}
		fmt.Printf("[DEBUG] Pipeline layers: %v\n", h.pipeline.LayerNames())
		if len(warnings) > 0 {
			for _, w := range warnings {
				fmt.Printf("[DEBUG] [WARNING] %s\n", w)
			}
		}
		fmt.Println("[DEBUG] Starting pipeline execution...")
	}

	// Execute pipeline with analysis context
	// ExecuteWithContext passes the context to ContextAwareLayer implementations
	// (behavior analyzer, pattern detector) while other layers run normally.
	result, err := h.pipeline.ExecuteWithContext(req.Events, analysisCtx)
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

	// Extract typed data from pipeline output
	analyzedFlows, detectedPatterns, changeResults, aiAnalysis, aiEnabled := aggregation.ExtractFromPipelineOutput(result)

	// Build structured report
	report := h.reportBuilder.Build(&aggregation.ReportInput{
		AnalysisContext:  analysisCtx,
		AnalyzedFlows:    analyzedFlows,
		DetectedPatterns: detectedPatterns,
		ChangeResults:    changeResults,
		AIAnalysis:       aiAnalysis,
		AIEnabled:        aiEnabled,
		InputEventsCount: len(req.Events),
		RawEvents:        req.Events,
		Warnings:         warnings,
		PipelineLayers:   h.pipeline.LayerNames(),
	})

	// Slim response: when AI is enabled, send only ai_analysis.
	// When AI is off, send the raw patterns (what the AI would have analyzed).
	var responseData interface{}
	if aiEnabled && report.AIAnalysis != nil {
		responseData = map[string]interface{}{
			"ai_analysis": report.AIAnalysis,
		}
	} else {
		responseData = map[string]interface{}{
			"patterns": report.Patterns,
		}
	}

	// Include request ID for tracing
	reqID := middleware.GetReqID(r.Context())

	respondJSON(w, http.StatusOK, AnalysisResponse{
		Success:   true,
		Message:   "Behavioral analysis complete",
		RequestID: reqID,
		Data:      responseData,
	})
}

// respondJSON writes a JSON response
func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
