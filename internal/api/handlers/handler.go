package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	pipeline         *layers.Pipeline // Full pipeline including AI (for /analyze)
	baselinePipeline *layers.Pipeline // Layers 0–6 only, no AI (for /baseline)
	reportBuilder    *aggregation.ReportBuilder
	storage          storage.Storage
	vocabStorage     io.Closer
	propertyStorage  io.Closer
	environment      string
	cleanupCancel    context.CancelFunc // cancels the background cleanup goroutine
}

// NewHandler creates a new handler instance with the provided configuration
func NewHandler(cfg *config.Config) *Handler {
	// Initialize storage using factory (Postgres based on config)
	storageInstance, err := storage.NewStorage(&cfg.Storage)

	if err != nil {
		slog.Error("storage initialization failed",
			"backend", "postgresql",
			"host", cfg.Storage.Postgres.Host,
			"port", cfg.Storage.Postgres.Port,
			"database", cfg.Storage.Postgres.Database,
			"error", err,
		)
		os.Exit(1)
	} else {
		slog.Info("storage connection successful",
			"backend", "postgresql",
			"host", cfg.Storage.Postgres.Host,
			"port", cfg.Storage.Postgres.Port,
			"database", cfg.Storage.Postgres.Database,
			"user", cfg.Storage.Postgres.User,
			"ssl_mode", cfg.Storage.Postgres.SSLMode,
			"max_connections", cfg.Storage.Postgres.MaxConnections,
			"retention_days", cfg.Storage.RetentionDays,
		)
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
		slog.Warn("failed to initialize vocab storage", "error", err)
	} else {
		// Seed built-in vocabulary (only runs if storage is empty)
		ctx := context.Background()
		if err := vocabagent.SeedBuiltinVocabulary(ctx, vocabStorage); err != nil {
			slog.Warn("failed to seed vocabulary", "error", err)
		}
	}

	// Initialize the pipeline with layers
	// Shared layers (0–6) are registered on both pipelines.
	// The AI analyzer (layer 7) is only added to the analysis pipeline.
	pipeline := layers.NewPipeline()
	baselinePipeline := layers.NewPipeline()

	// registerShared adds a layer to both pipelines.
	registerShared := func(layer layers.Layer) {
		pipeline.Register(layer)
		baselinePipeline.Register(layer)
	}

	// Initialize property storage for context agent (same PostgreSQL database)
	var propertyStorage *propertyagent.PostgresPropertyStorage
	propertyStorage, err = propertyagent.NewPostgresPropertyStorage(&cfg.Storage.Postgres)
	if err != nil {
		slog.Warn("failed to initialize property storage", "error", err)
	} else {
		// Seed built-in dimensions (only runs if storage is empty)
		ctx := context.Background()
		if err := propertyagent.SeedBuiltinDimensions(ctx, propertyStorage); err != nil {
			slog.Warn("failed to seed property registry", "error", err)
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
			Enabled:  cfg.ContextAgent.Enabled,
			Provider: cfg.ContextAgent.Provider,
			BaseURL:  cfg.ContextAgent.BaseURL,
			APIKey:   cfg.ContextAgent.APIKey,
			Model:    cfg.ContextAgent.Model,
			Debug:    cfg.Server.Environment == "development",
			CircuitBreaker: propertyagent.CircuitBreakerConfig{
				Enabled:          cfg.Resiliency.CircuitBreaker.Enabled,
				FailureThreshold: cfg.Resiliency.CircuitBreaker.FailureThreshold,
				ResetTimeout:     resetTimeout,
			},
		}
		contextAgent := propertyagent.NewAgentWithConfig(contextAgentConfig)
		contextEnricher := propertyagent.NewContextEnricher(propertyStorage, contextAgent, cfg.Server.Environment == "development")
		registerShared(contextEnricher)
		slog.Info("context enricher layer enabled", "model", cfg.ContextAgent.Model)
	} else {
		slog.Info("context enricher layer disabled", "enabled", cfg.ContextAgent.Enabled, "api_key_set", cfg.ContextAgent.APIKey != "", "storage_available", propertyStorage != nil)
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
			Enabled:  cfg.VocabAgent.Enabled,
			Provider: cfg.VocabAgent.Provider,
			BaseURL:  cfg.VocabAgent.BaseURL,
			APIKey:   cfg.VocabAgent.APIKey,
			Model:    cfg.VocabAgent.Model,
			Debug:    cfg.Server.Environment == "development",
			CircuitBreaker: vocabagent.CircuitBreakerConfig{
				Enabled:          cfg.Resiliency.CircuitBreaker.Enabled,
				FailureThreshold: cfg.Resiliency.CircuitBreaker.FailureThreshold,
				ResetTimeout:     resetTimeout,
			},
		}
		vocabAgentInstance := vocabagent.NewWithConfig(vocabAgentConfig)
		enricher := vocabagent.NewVocabEnricher(vocabStorage, vocabAgentInstance, cfg.Server.Environment == "development")
		registerShared(enricher)
		slog.Info("vocab enricher layer enabled", "model", cfg.VocabAgent.Model)
	} else {
		slog.Info("vocab enricher layer disabled", "enabled", cfg.VocabAgent.Enabled, "api_key_set", cfg.VocabAgent.APIKey != "", "storage_available", vocabStorage != nil)
	}

	// Layer 2: Event Adapter - normalizes raw events and builds canonical context
	// Uses vocab storage for word categorization and property storage for context building
	if vocabStorage != nil && propertyStorage != nil {
		registerShared(eventadapter.NewWithLookups(vocabStorage, propertyStorage))
		slog.Info("event adapter using postgresql vocabulary + property registry lookup")
	} else if vocabStorage != nil {
		registerShared(eventadapter.NewWithVocabLookup(vocabStorage))
		slog.Info("event adapter using postgresql vocabulary lookup")
	} else {
		registerShared(eventadapter.New())
	}

	// Layer 3: Session & Flow Reconstructor - groups events into flow instances
	registerShared(sessionflow.New())

	// Layer 4: Behavior Analyzer - detects behavioral patterns
	registerShared(behavior.New())

	// Layer 5: Pattern Detector - recognizes higher-level patterns
	registerShared(pattern.New())

	// Layer 5: Baseline & Change Detector - compares against historical baseline
	baselineConfig := &baseline.Config{
		BaselineWindowDays:          cfg.Baseline.WindowDays,
		MinBaselineDays:             cfg.Baseline.MinDays,
		MinAffectedUsers:            cfg.Baseline.MinAffectedUsers,
		ComputationMode:             cfg.Baseline.ComputationMode,
		TrendThreshold:              cfg.Baseline.TrendThreshold,
		HighSignificanceThreshold:   cfg.Baseline.HighSignificanceThreshold,
		StandardDeviationMultiplier: cfg.Baseline.StdDeviationMultiplier,
	}

	if storageInstance != nil {
		registerShared(baseline.NewWithConfig(baselineConfig, storageInstance))
	} else {
		registerShared(baseline.NewWithConfig(baselineConfig, nil))
	}

	// Layer 6: AI Analyzer - provides AI-powered insights (optional)
	// Only registered if AI is enabled in config and API key is provided
	if cfg.AIAnalyzer.Enabled && cfg.AIAnalyzer.APIKey != "" {
		// Parse circuit breaker reset timeout
		resetTimeout, err := time.ParseDuration(cfg.Resiliency.CircuitBreaker.ResetTimeout)
		if err != nil {
			slog.Warn("invalid circuit breaker reset_timeout, using default", "value", cfg.Resiliency.CircuitBreaker.ResetTimeout, "default", "30s")
			resetTimeout = 30 * time.Second
		}

		aiConfig := &ai.Config{
			Enabled:  cfg.AIAnalyzer.Enabled,
			Provider: cfg.AIAnalyzer.Provider,
			BaseURL:  cfg.AIAnalyzer.BaseURL,
			APIKey:   cfg.AIAnalyzer.APIKey,
			Model:    cfg.AIAnalyzer.Model,
			Debug:    cfg.Server.Environment == "development",
			CircuitBreaker: ai.CircuitBreakerConfig{
				Enabled:          cfg.Resiliency.CircuitBreaker.Enabled,
				FailureThreshold: cfg.Resiliency.CircuitBreaker.FailureThreshold,
				ResetTimeout:     resetTimeout,
			},
		}
		pipeline.Register(ai.NewWithConfig(aiConfig))
		slog.Info("AI analyzer layer enabled", "model", cfg.AIAnalyzer.Model)
		if cfg.Resiliency.CircuitBreaker.Enabled {
			slog.Info("circuit breaker configured", "threshold", cfg.Resiliency.CircuitBreaker.FailureThreshold, "reset_timeout", resetTimeout)
		}
	} else {
		slog.Info("AI analyzer layer disabled", "enabled", cfg.AIAnalyzer.Enabled, "api_key_set", cfg.AIAnalyzer.APIKey != "")
	}

	// Only assign to io.Closer fields when the concrete pointer is non-nil,
	// to avoid a non-nil interface wrapping a nil pointer.
	var vocabCloser io.Closer
	if vocabStorage != nil {
		vocabCloser = vocabStorage
	}
	var propCloser io.Closer
	if propertyStorage != nil {
		propCloser = propertyStorage
	}

	return &Handler{
		pipeline:         pipeline,
		baselinePipeline: baselinePipeline,
		reportBuilder:    aggregation.NewReportBuilder(),
		storage:          storageInstance,
		vocabStorage:     vocabCloser,
		propertyStorage:  propCloser,
		environment:      cfg.Server.Environment,
		cleanupCancel:    cleanupCancel,
	}
}

// NewDemoHandler creates a handler wired entirely with in-memory storage.
// No database, no LLM, no config file required. Layers 2–6 only.
func NewDemoHandler() *Handler {
	ctx := context.Background()

	// In-memory storage for baseline snapshots
	storageInstance := storage.NewInMemoryStorage()

	// In-memory vocab storage — seed built-in vocabulary
	vocabMem := vocabagent.NewInMemoryVocabStorage()
	if err := vocabagent.SeedBuiltinVocabulary(ctx, vocabMem); err != nil {
		slog.Warn("demo: failed to seed vocabulary", "error", err)
	}

	// In-memory property storage — seed built-in dimensions
	propMem := propertyagent.NewInMemoryPropertyStorage()
	if err := propertyagent.SeedBuiltinDimensions(ctx, propMem); err != nil {
		slog.Warn("demo: failed to seed property registry", "error", err)
	}

	pipeline := layers.NewPipeline()
	baselinePipeline := layers.NewPipeline()

	registerShared := func(layer layers.Layer) {
		pipeline.Register(layer)
		baselinePipeline.Register(layer)
	}

	// Layer 2: Event Adapter (no LLM agents in demo)
	registerShared(eventadapter.NewWithLookups(vocabMem, propMem))

	// Layer 3: Session & Flow Reconstructor
	registerShared(sessionflow.New())

	// Layer 4: Behavior Analyzer
	registerShared(behavior.New())

	// Layer 5: Pattern Detector — lower severity thresholds for small demo traffic
	demoCfg := pattern.DefaultConfig()
	demoCfg.HighSeverityUserCount = 5
	demoCfg.MediumSeverityUserCount = 3
	registerShared(pattern.NewWithConfig(demoCfg))

	// Layer 6: Baseline Comparator (shared in-memory store)
	registerShared(baseline.NewWithStorage(storageInstance))

	return &Handler{
		pipeline:         pipeline,
		baselinePipeline: baselinePipeline,
		reportBuilder:    aggregation.NewReportBuilder(),
		storage:          storageInstance,
		vocabStorage:     vocabMem,
		propertyStorage:  propMem,
		environment:      "demo",
	}
}

// startStorageCleanup runs periodic cleanup of old snapshots.
// It stops when ctx is cancelled (server shutdown).
func startStorageCleanup(ctx context.Context, store storage.Storage, retentionDays int) {
	// Run cleanup immediately on startup
	if deleted, err := store.Cleanup(ctx); err != nil {
		slog.Warn("storage cleanup failed", "error", err)
	} else if deleted > 0 {
		slog.Info("storage cleanup completed", "deleted_snapshots", deleted)
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
				slog.Warn("storage cleanup failed", "error", err)
			} else if deleted > 0 {
				slog.Info("storage cleanup completed", "deleted_snapshots", deleted)
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
			slog.Warn("failed to close storage", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if h.vocabStorage != nil {
		if err := h.vocabStorage.Close(); err != nil {
			slog.Warn("failed to close vocab storage", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if h.propertyStorage != nil {
		if err := h.propertyStorage.Close(); err != nil {
			slog.Warn("failed to close property storage", "error", err)
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

// maxEventsPerRequest is the maximum number of events allowed in a single request
const maxEventsPerRequest = 10000

// ProjectIDHeader is the HTTP header that identifies the project.
const ProjectIDHeader = "X-Project-ID"

// validProjectID matches alphanumeric, hyphens, underscores (1-64 chars)
var validProjectID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Analyze handles the behavioral analysis request
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	req, projectID, ok := h.validateAndDecodeRequest(w, r)
	if !ok {
		return
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
	// Analyze never writes baseline snapshots — use POST /api/v1/baseline for that.
	analysisCtx.UpdateBaseline = false
	// Carry the HTTP request context through the pipeline so layers can
	// cancel LLM / DB calls when the client disconnects.
	analysisCtx.Ctx = r.Context()

	// Check recommended properties and collect warnings
	warnings := canonical.CheckRecommendedProperties(req.Events)

	if isDebugMode {
		logAttrs := []any{
			"env", h.environment,
			"event_count", len(req.Events),
			"scope", analysisCtx.Scope,
			"pipeline_layers", h.pipeline.LayerNames(),
		}
		if len(analysisCtx.FunnelDefinitions) > 0 {
			logAttrs = append(logAttrs, "funnel_definitions", len(analysisCtx.FunnelDefinitions))
		}
		if len(analysisCtx.FlowConfigs) > 0 {
			logAttrs = append(logAttrs, "flow_configs", len(analysisCtx.FlowConfigs))
		}
		if len(warnings) > 0 {
			logAttrs = append(logAttrs, "warnings", warnings)
		}
		slog.Debug("new analysis request", logAttrs...)
	}

	// Execute pipeline with analysis context
	// ExecuteWithContext passes the context to ContextAwareLayer implementations
	// (behavior analyzer, pattern detector) while other layers run normally.
	result, err := h.pipeline.ExecuteWithContext(req.Events, analysisCtx)
	if err != nil {
		if isDebugMode {
			slog.Debug("pipeline execution failed", "error", err)
		}
		respondJSON(w, http.StatusInternalServerError, AnalysisResponse{
			Success: false,
			Message: "Processing failed: " + err.Error(),
		})
		return
	}

	if isDebugMode {
		slog.Debug("pipeline execution complete")
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

// BaselineData is the response payload for the baseline ingestion endpoint.
type BaselineData struct {
	PatternsStored int                           `json:"patterns_stored"`
	SnapshotDate   string                        `json:"snapshot_date"`
	Patterns       []*aggregation.PatternInsight `json:"patterns,omitempty"`
}

// validateAndDecodeRequest performs shared input validation for both the
// analyze and baseline endpoints. It returns the decoded request body,
// project ID, and true on success. On validation failure it writes the
// error response and returns false.
func (h *Handler) validateAndDecodeRequest(w http.ResponseWriter, r *http.Request) (*EventRequest, string, bool) {
	// Limit request body size to prevent OOM
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	// Extract and validate project ID from header
	projectID := r.Header.Get(ProjectIDHeader)
	if projectID == "" {
		respondJSON(w, http.StatusBadRequest, AnalysisResponse{
			Success: false,
			Message: "Missing required header 'X-Project-ID'. Each request must identify the project.",
		})
		return nil, "", false
	}
	if !validProjectID.MatchString(projectID) {
		respondJSON(w, http.StatusBadRequest, AnalysisResponse{
			Success: false,
			Message: "Invalid 'X-Project-ID'. Must be 1-64 alphanumeric characters, hyphens, or underscores.",
		})
		return nil, "", false
	}

	var req EventRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			respondJSON(w, http.StatusRequestEntityTooLarge, AnalysisResponse{
				Success: false,
				Message: "Request body too large. Maximum size is 10MB.",
			})
			return nil, "", false
		}
		respondJSON(w, http.StatusBadRequest, AnalysisResponse{
			Success: false,
			Message: "Invalid request body: malformed JSON.",
		})
		return nil, "", false
	}

	// Validate minimum input: at least 1 event required
	if len(req.Events) == 0 {
		respondJSON(w, http.StatusBadRequest, AnalysisResponse{
			Success: false,
			Message: "At least 1 event is required in the 'events' array.",
		})
		return nil, "", false
	}

	// Validate maximum event count to prevent resource exhaustion
	if len(req.Events) > maxEventsPerRequest {
		respondJSON(w, http.StatusBadRequest, AnalysisResponse{
			Success: false,
			Message: fmt.Sprintf("Too many events. Maximum allowed is %d per request.", maxEventsPerRequest),
		})
		return nil, "", false
	}

	// Validate required fields on events
	for i, evt := range req.Events {
		if _, ok := evt["event"]; !ok {
			respondJSON(w, http.StatusBadRequest, AnalysisResponse{
				Success: false,
				Message: fmt.Sprintf("Event at index %d is missing required field 'event'.", i),
			})
			return nil, "", false
		}
		if _, ok := evt["user_id"]; !ok {
			respondJSON(w, http.StatusBadRequest, AnalysisResponse{
				Success: false,
				Message: fmt.Sprintf("Event at index %d is missing required field 'user_id'.", i),
			})
			return nil, "", false
		}
		if _, ok := evt["ts"]; !ok {
			respondJSON(w, http.StatusBadRequest, AnalysisResponse{
				Success: false,
				Message: fmt.Sprintf("Event at index %d is missing required field 'ts'.", i),
			})
			return nil, "", false
		}
	}

	return &req, projectID, true
}

// Baseline handles baseline ingestion requests.
// Runs pipeline layers 0–6 (no AI), stores baseline snapshots, and returns
// a confirmation with the number of patterns stored.
func (h *Handler) Baseline(w http.ResponseWriter, r *http.Request) {
	req, projectID, ok := h.validateAndDecodeRequest(w, r)
	if !ok {
		return
	}

	isDebugMode := h.environment == "development"

	// Resolve analysis context (use default if not provided)
	analysisCtx := req.AnalysisContext
	if analysisCtx == nil {
		analysisCtx = behavior.DefaultAnalysisContext()
	}
	analysisCtx.ProjectID = projectID
	analysisCtx.UpdateBaseline = true // baseline endpoint always stores
	analysisCtx.Ctx = r.Context()

	if isDebugMode {
		slog.Debug("new baseline ingestion request",
			"env", h.environment,
			"event_count", len(req.Events),
			"pipeline_layers", h.baselinePipeline.LayerNames(),
		)
	}

	// Execute baseline pipeline (layers 0–6, no AI)
	result, err := h.baselinePipeline.ExecuteWithContext(req.Events, analysisCtx)
	if err != nil {
		if isDebugMode {
			slog.Debug("baseline pipeline execution failed", "error", err)
		}
		respondJSON(w, http.StatusInternalServerError, AnalysisResponse{
			Success: false,
			Message: "Baseline processing failed: " + err.Error(),
		})
		return
	}

	if isDebugMode {
		slog.Debug("baseline pipeline execution complete")
	}

	// Extract pattern data from pipeline output
	_, detectedPatterns, changeResults, _, _ := aggregation.ExtractFromPipelineOutput(result)

	// Build report for pattern insights (reuse existing report builder)
	report := h.reportBuilder.Build(&aggregation.ReportInput{
		AnalysisContext:  analysisCtx,
		DetectedPatterns: detectedPatterns,
		ChangeResults:    changeResults,
		InputEventsCount: len(req.Events),
		RawEvents:        req.Events,
		PipelineLayers:   h.baselinePipeline.LayerNames(),
	})

	reqID := middleware.GetReqID(r.Context())

	respondJSON(w, http.StatusOK, AnalysisResponse{
		Success:   true,
		Message:   "Baseline snapshot stored",
		RequestID: reqID,
		Data: BaselineData{
			PatternsStored: len(detectedPatterns),
			SnapshotDate:   time.Now().UTC().Truncate(24 * time.Hour).Format("2006-01-02"),
			Patterns:       report.Patterns,
		},
	})
}
