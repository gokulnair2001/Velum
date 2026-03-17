package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/velum/internal/config"
	"github.com/velum/internal/layers/baseline"
	"github.com/velum/internal/layers/behavior"
	"github.com/velum/internal/layers/pattern"
)

const (
	systemPrompt = `You are a product analytics assistant.

You are a product analytics interpretation assistant.

You are given detected behavioral patterns and optional baseline comparison
results. The input JSON is the ONLY source of truth.

Patterns may include a "context_key" field which describes the specific
conditions under which the pattern was observed (e.g.,
"error_code=card_declined,plan_name=premium"). When present, you MUST
reference the context in your analysis. Context-keyed patterns represent
baselines that are tracked separately — for example, a retry storm on
checkout for error_code=card_declined is a different baseline from a retry
storm caused by error_code=timeout.

Your task is to describe WHAT was observed using ONLY the information
explicitly present in the input. You must not infer, assume, rename, or
reinterpret any data.

STRICT RULES (NON-NEGOTIABLE):

Data usage
- You may ONLY reference fields explicitly present in the input JSON.
- You MUST NOT invent, infer, rename, paraphrase, or introduce new pattern
  names, behavioral labels, flows, domains, or terminology.
- You MUST reference pattern_type values exactly as provided in the input.
- You may ONLY reference flow values exactly as they appear in the input.
- If a flow is missing, null, or unclassified, refer to it ONLY as
  "unclassified behavior" or omit flow mention entirely.
- You MUST NOT generalize or rephrase pattern names (e.g., do NOT convert
  "early_dropoff" into "silent abandonment" or similar terms).
- When context_key is present, you MUST include the context conditions in
  your analysis (e.g., "retry pattern in payment flow where
  error_code=card_declined").
- When multiple context_key variants exist for the same pattern+flow, compare
  them if baseline data is available.

Metrics & baselines
- Do NOT invent metrics, percentages, counts, ratios, trends, or baselines.
- You may ONLY use numeric values that appear verbatim in the input JSON.
- If numeric values are missing, describe observations qualitatively WITHOUT
  numbers.
- If baseline comparison fields are present, describe behavior using ONLY those
  fields (e.g., increasing, decreasing, stable).
- If baseline comparison is missing or baseline_available is false, you MUST
  explicitly state that baseline comparison is not available.
- You MUST NOT calculate, infer, or assume baseline values.
- When ALL patterns have "Baseline Available: false" (first observation), this
  means the system is seeing these flows FOR THE FIRST TIME. Do NOT interpret
  first observations as anomalies, problems, or dropoffs. Instead describe
  them as newly observed behavioral patterns that will serve as the initial
  baseline for future comparison.
- First observations with no baseline should focus on DESCRIBING the observed
  flow structure (what flows exist, how they relate) rather than diagnosing
  issues.

Language & interpretation constraints
- The summary MUST describe WHAT was observed, not WHY.
- Do NOT claim causality, intent, faults, issues, bugs, usability problems, or
  design problems.
- Avoid diagnostic, evaluative, or judgmental language such as:
  "problem", "issue", "difficulty", "confusion", "frustration",
  "poor", "bad", "failure", "broken".
- Hypotheses are allowed ONLY as possibilities and must remain high-level,
  neutral, and non-diagnostic.
- When uncertain, prefer stating uncertainty over adding detail.

Output constraints
- You MUST respond with valid JSON only.
- Do NOT include any text outside the JSON object.
- You MUST use the exact output structure defined below.
- If only one pattern_type is present, the summary MUST mention only that
  pattern_type.
- If multiple pattern_type values are present, the summary MAY mention them
  collectively without renaming them.
- EVERY detail MUST include at least one numeric value from the input
  (affected_users, total_flows, ratio, event counts, etc.).
- When error codes, device types, or country values appear in the
  "Event-Level Evidence" or "Context Breakdown" sections, you MUST reference
  them in the relevant detail. Do NOT omit error codes that are present.
- When "Sample User Journeys" are provided, use them to describe WHAT
  specific users experienced (e.g., "User u1 hit buffer_timeout twice
  before succeeding at lower quality"). Do NOT ignore user-level evidence.
- Each hypothesis MUST cite a specific data point from the input
  (e.g., "2 of 3 affected users were on mobile in IN, suggesting a
  region-specific issue").
- Generate one detail per detected pattern. Each detail should cover:
  the pattern type, affected users/total, and any error codes or
  conditions observed.

REQUIRED OUTPUT FORMAT:

{
  "summary": "A single sentence describing the observed pattern(s) using only provided pattern_type, flow, and context_key values, including affected user counts.",
  "details": [
    "Pattern X in flow Y: N affected users out of M total (Z%). Error codes observed: ... Devices/regions: ...",
    "Baseline comparison status for the above patterns.",
    "Additional detail about specific user journeys or conditions when evidence is present."
  ],
  "hypotheses": [
    "Possible explanation citing specific data points from the input (error codes, user counts, device/country distributions).",
    "Possible explanation citing specific data points from the input."
  ],
  "confidence_note": "These are hypotheses based on observed behavioral changes."
}`
)

// Analyzer performs AI-powered analysis of baseline results
type Analyzer struct {
	config         *Config
	httpClient     *http.Client
	circuitBreaker *CircuitBreaker
}

// New creates a new AI Analyzer with default config (disabled)
func New() *Analyzer {
	cfg := DefaultConfig()
	return &Analyzer{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		circuitBreaker: NewCircuitBreaker(cfg.CircuitBreaker, false),
	}
}

// NewWithConfig creates an AI Analyzer with custom config
func NewWithConfig(config *Config) *Analyzer {
	return &Analyzer{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		circuitBreaker: NewCircuitBreaker(config.CircuitBreaker, config.Debug),
	}
}

// Name returns the layer identifier
func (a *Analyzer) Name() string {
	return "ai_analyzer"
}

// apiEndpoint returns the configured API endpoint, resolving from provider if base_url is not set.
func (a *Analyzer) apiEndpoint() string {
	return config.ResolveProviderURL(a.config.Provider, a.config.BaseURL)
}

// Process implements the Layer interface
func (a *Analyzer) Process(input interface{}) (interface{}, error) {
	return a.processWithCtx(context.Background(), input)
}

// ProcessWithContext implements the ContextAwareLayer interface.
// Extracts the request context from AnalysisContext so HTTP calls to the
// Groq API are cancelled when the client disconnects.
func (a *Analyzer) ProcessWithContext(input interface{}, metadata interface{}) (interface{}, error) {
	ctx := context.Background()
	if actx, ok := metadata.(*behavior.AnalysisContext); ok && actx != nil {
		ctx = actx.RequestContext()
	}
	return a.processWithCtx(ctx, input)
}

func (a *Analyzer) processWithCtx(ctx context.Context, input interface{}) (interface{}, error) {
	// If AI is disabled, pass through the baseline result as AIResult
	if !a.config.Enabled || a.config.APIKey == "" {
		if a.config.Debug {
			slog.Debug("AI layer disabled, passing through data", "layer", "ai_analyzer")
		}
		return a.passThrough(input)
	}

	// Only process baseline results
	baselineResult, ok := input.(*baseline.BaselineResult)
	if !ok {
		if a.config.Debug {
			slog.Debug("input is not BaselineResult, passing through", "layer", "ai_analyzer", "input_type", fmt.Sprintf("%T", input))
		}
		return a.passThrough(input)
	}

	if a.config.Debug {
		slog.Debug("starting AI analysis", "layer", "ai_analyzer", "model", a.config.Model, "change_results", len(baselineResult.ChangeResults))
	}

	// Short-circuit: don't call LLM when there are no patterns to analyze
	if len(baselineResult.ChangeResults) == 0 {
		if a.config.Debug {
			slog.Debug("no patterns detected, skipping LLM call", "layer", "ai_analyzer")
		}
		return &AIResult{
			ChangeResults:    baselineResult.ChangeResults,
			DetectedPatterns: baselineResult.DetectedPatterns,
			AnalyzedFlows:    baselineResult.AnalyzedFlows,
			AIAnalysis: &AnalysisResponse{
				Summary:        "No significant behavioral patterns detected in this batch.",
				Details:        []string{},
				Hypotheses:     []string{},
				ConfidenceNote: "Insufficient pattern data to generate hypotheses. This is normal for small batches or first-time observations.",
			},
			AIEnabled: true,
		}, nil
	}

	// Check circuit breaker before making AI request
	if err := a.circuitBreaker.Allow(); err != nil {
		if a.config.Debug {
			slog.Debug("circuit breaker is open, skipping AI analysis", "layer", "ai_analyzer")
		}
		return &AIResult{
			ChangeResults:    baselineResult.ChangeResults,
			DetectedPatterns: baselineResult.DetectedPatterns,
			AnalyzedFlows:    baselineResult.AnalyzedFlows,
			AIAnalysis: &AnalysisResponse{
				Summary:        "AI analysis temporarily unavailable",
				Details:        []string{"Circuit breaker is open due to repeated failures"},
				Hypotheses:     []string{},
				ConfidenceNote: "AI analysis was skipped to prevent cascading failures.",
			},
			AIEnabled: true,
		}, nil
	}

	// Perform AI analysis
	analysis, err := a.analyze(ctx, baselineResult)
	if err != nil {
		a.circuitBreaker.RecordFailure()
		if a.config.Debug {
			slog.Debug("AI analysis failed", "layer", "ai_analyzer", "error", err)
		}
		// On error, return result without AI analysis but with error info
		return &AIResult{
			ChangeResults:    baselineResult.ChangeResults,
			DetectedPatterns: baselineResult.DetectedPatterns,
			AnalyzedFlows:    baselineResult.AnalyzedFlows,
			AIAnalysis: &AnalysisResponse{
				Summary:        "AI analysis failed",
				Details:        []string{fmt.Sprintf("Error: %v", err)},
				Hypotheses:     []string{},
				ConfidenceNote: "AI analysis was not completed due to an error.",
			},
			AIEnabled: true,
		}, nil
	}

	a.circuitBreaker.RecordSuccess()

	if a.config.Debug {
		slog.Debug("AI analysis completed", "layer", "ai_analyzer", "summary", analysis.Summary)
	}

	return &AIResult{
		ChangeResults:    baselineResult.ChangeResults,
		DetectedPatterns: baselineResult.DetectedPatterns,
		AnalyzedFlows:    baselineResult.AnalyzedFlows,
		AIAnalysis:       analysis,
		AIEnabled:        true,
	}, nil
}

// passThrough creates an AIResult from the input without AI analysis
// When AI is disabled, only return change_results
func (a *Analyzer) passThrough(input interface{}) (*AIResult, error) {
	switch v := input.(type) {
	case *baseline.BaselineResult:
		return &AIResult{
			ChangeResults:    v.ChangeResults,
			DetectedPatterns: v.DetectedPatterns,
			AnalyzedFlows:    v.AnalyzedFlows,
			AIAnalysis:       nil,
			AIEnabled:        false,
		}, nil
	default:
		// For other types, wrap minimally
		return &AIResult{
			AIEnabled: false,
		}, nil
	}
}

// analyze performs the actual AI analysis using Groq API
func (a *Analyzer) analyze(ctx context.Context, baselineResult *baseline.BaselineResult) (*AnalysisResponse, error) {
	// Build the user prompt with baseline data
	userPrompt, err := a.buildUserPrompt(baselineResult)
	if err != nil {
		return nil, fmt.Errorf("failed to build prompt: %w", err)
	}

	if a.config.Debug {
		slog.Debug("built AI prompt", "layer", "ai_analyzer", "prompt_chars", len(userPrompt))
	}

	// Create the Groq request
	groqReq := GroqRequest{
		Model: a.config.Model,
		Messages: []GroqMessage{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: userPrompt,
			},
		},
		Temperature: 0.3, // Low temperature for consistent, factual output
		MaxTokens:   4096,
	}

	// Make the API request with retry for rate limits and server errors
	reqBody, err := json.Marshal(groqReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if a.config.Debug {
		slog.Debug("sending request to Groq API", "layer", "ai_analyzer")
	}

	var body []byte
	var statusCode int
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", a.apiEndpoint(), bytes.NewBuffer(reqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+a.config.APIKey)

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("API request failed: %w", err)
		}

		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
		statusCode = resp.StatusCode

		// Retry on 429 (rate limit) or 5xx (server error)
		if (statusCode == 429 || statusCode >= 500) && attempt < maxRetries {
			backoff := time.Duration(1<<uint(attempt)) * time.Second // 1s, 2s, 4s
			if a.config.Debug {
				slog.Debug("retrying Groq API request", "layer", "ai_analyzer",
					"status", statusCode, "attempt", attempt+1, "backoff", backoff)
			}
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return nil, fmt.Errorf("request cancelled during retry backoff: %w", ctx.Err())
			}
		}
		break
	}

	if a.config.Debug {
		slog.Debug("Groq API response received", "layer", "ai_analyzer", "status", statusCode)
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", statusCode, string(body))
	}

	// Parse the Groq response
	var groqResp GroqResponse
	if err := json.Unmarshal(body, &groqResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if groqResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", groqResp.Error.Message)
	}

	if len(groqResp.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned")
	}

	// Check if the response was truncated (hit max_tokens)
	content := groqResp.Choices[0].Message.Content
	finishReason := ""
	if len(groqResp.Choices) > 0 {
		finishReason = groqResp.Choices[0].FinishReason
	}

	if finishReason == "length" {
		if a.config.Debug {
			slog.Debug("response truncated, attempting JSON repair", "layer", "ai_analyzer", "finish_reason", finishReason)
		}
		// Try to repair the truncated JSON before parsing
		content = repairTruncatedJSON(content)
	}

	return a.parseAIResponse(content)
}

// buildUserPrompt creates the prompt with baseline data, pattern evidence, and context breakdown
func (a *Analyzer) buildUserPrompt(baselineResult *baseline.BaselineResult) (string, error) {
	var sb strings.Builder

	sb.WriteString("Please analyze the following behavioral pattern data and provide insights:\n\n")

	// Build lookup maps for pattern evidence and flow context
	patternEvidence := buildPatternEvidenceMap(baselineResult.DetectedPatterns)
	contextBreakdown := buildContextBreakdown(baselineResult.AnalyzedFlows)

	// Summarize baseline status upfront
	totalChanges := len(baselineResult.ChangeResults)
	firstObsCount := 0
	for _, change := range baselineResult.ChangeResults {
		if change.BaselineStatus == baseline.BaselineStatusFirstObservation {
			firstObsCount++
		}
	}
	if totalChanges > 0 && firstObsCount == totalChanges {
		sb.WriteString("**NOTE:** ALL patterns below are FIRST OBSERVATIONS — the system is seeing ")
		sb.WriteString("these flows for the first time. There is NO baseline to compare against. ")
		sb.WriteString("Do not interpret these as anomalies or problems. Describe the observed ")
		sb.WriteString("flow structure and note that these will become the baseline for future runs.\n\n")
	} else if firstObsCount > 0 {
		sb.WriteString(fmt.Sprintf("**NOTE:** %d of %d patterns are first observations (no baseline yet).\n\n",
			firstObsCount, totalChanges))
	}

	// Add change results if present — skip context-keyed duplicates to reduce noise.
	// When both retry_storm:transfer (global) and retry_storm:transfer (context=upi_timeout)
	// exist, only include the global one with a note about context variants.
	if len(baselineResult.ChangeResults) > 0 {
		sb.WriteString("## Detected Changes:\n")

		// Index: collect context variants per pattern+flow, and check which
		// pattern+flow combos have a global (empty context) entry.
		contextVariants := make(map[string][]string) // "pattern:flow" → [contextKey1, contextKey2]
		hasGlobal := make(map[string]bool)           // "pattern:flow" → true if global exists
		for _, change := range baselineResult.ChangeResults {
			baseKey := change.PatternType + ":" + change.Flow
			if change.ContextKey != "" {
				contextVariants[baseKey] = append(contextVariants[baseKey], change.ContextKey)
			} else {
				hasGlobal[baseKey] = true
			}
		}

		for _, change := range baselineResult.ChangeResults {
			// Skip context-keyed entries ONLY when a global entry exists for
			// the same pattern+flow — the global entry will reference variants.
			// If there's no global entry, keep the context-keyed one.
			if change.ContextKey != "" {
				baseKey := change.PatternType + ":" + change.Flow
				if hasGlobal[baseKey] {
					continue
				}
			}
			// Check if baseline is available (not first observation)
			if change.BaselineStatus == baseline.BaselineStatusFirstObservation {
				// First observation: only send minimal data without baseline metrics
				sb.WriteString(fmt.Sprintf("- Pattern: %s, Flow: %s\n", change.PatternType, change.Flow))
				if change.ContextKey != "" {
					sb.WriteString(fmt.Sprintf("  Context: %s\n", change.ContextKey))
				}
				sb.WriteString("  Baseline Available: false\n")
			} else {
				// Baseline exists: send full data with comparison metrics
				sb.WriteString(fmt.Sprintf("- Pattern: %s, Flow: %s\n", change.PatternType, change.Flow))
				if change.ContextKey != "" {
					sb.WriteString(fmt.Sprintf("  Context: %s\n", change.ContextKey))
				}
				sb.WriteString(fmt.Sprintf("  Current Impact: %.2f%%, Baseline: %.2f%%\n",
					change.CurrentImpactRatio*100, change.BaselineImpactRatio*100))
				sb.WriteString(fmt.Sprintf("  Delta: %.2f%%, Trend: %s, Significance: %s\n",
					change.DeltaPercentage*100, change.Trend, change.ChangeSignificance))
				sb.WriteString(fmt.Sprintf("  Baseline Window: %s (%d days)\n",
					change.BaselineWindow, change.BaselineDays))
			}

			// Append pattern evidence (severity, confidence, affected users)
			key := change.PatternType + ":" + change.Flow
			if ev, ok := patternEvidence[key]; ok {
				sb.WriteString(fmt.Sprintf("  Severity: %s, Confidence: %s\n", ev.Severity, ev.Confidence))
				pct := ev.Evidence.Ratio * 100
				sb.WriteString(fmt.Sprintf("  Affected Users: %d / %d eligible flows (%.0f%%)\n",
					ev.AffectedUsers, ev.TotalFlows, pct))
				if ev.Evidence.Description != "" {
					sb.WriteString(fmt.Sprintf("  Evidence: %s\n", ev.Evidence.Description))
				}
			}

			// Note context-keyed variants if any
			if variants, ok := contextVariants[key]; ok && len(variants) > 0 {
				sb.WriteString(fmt.Sprintf("  Context variants: %s\n", strings.Join(variants, "; ")))
			}

			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("No significant changes detected compared to baseline.\n")
	}

	// Append context breakdown if available
	if len(contextBreakdown) > 0 {
		sb.WriteString("## Context Breakdown:\n")
		sb.WriteString("Distribution of properties across analyzed flows:\n")
		for flowName, props := range contextBreakdown {
			sb.WriteString(fmt.Sprintf("\nFlow: %s\n", flowName))
			for propName, values := range props {
				sortedVals := sortedValueCounts(values)
				sb.WriteString(fmt.Sprintf("  %s: %s\n", propName, sortedVals))
			}
		}
		sb.WriteString("\n")
	}

	// Append event-level evidence per pattern (error distributions + sample user journeys)
	eventEvidence := buildEventLevelEvidence(baselineResult.DetectedPatterns, baselineResult.AnalyzedFlows)
	if len(eventEvidence) > 0 {
		sb.WriteString("## Event-Level Evidence:\n")
		sb.WriteString("Per-pattern drill-down with error distributions and sample user journeys.\n")
		sb.WriteString("Use this data to make your analysis SPECIFIC. Reference error codes and user journeys.\n\n")
		for key, ev := range eventEvidence {
			sb.WriteString(fmt.Sprintf("### %s\n", key))
			if len(ev.ErrorDistribution) > 0 {
				sb.WriteString("  Error distribution:\n")
				for code, count := range ev.ErrorDistribution {
					sb.WriteString(fmt.Sprintf("    %s: %d occurrences\n", code, count))
				}
			}
			if len(ev.StatusDistribution) > 0 {
				sb.WriteString("  Status distribution:\n")
				for status, count := range ev.StatusDistribution {
					sb.WriteString(fmt.Sprintf("    %s: %d occurrences\n", status, count))
				}
			}
			if len(ev.UserJourneys) > 0 {
				sb.WriteString("  Sample user journeys:\n")
				for _, uj := range ev.UserJourneys {
					sb.WriteString(fmt.Sprintf("    User %s: %s\n", uj.UserID, strings.Join(uj.Steps, " → ")))
				}
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("Provide your analysis in the required JSON format.")

	return sb.String(), nil
}

// buildPatternEvidenceMap creates a lookup of "patternType:flow" → DetectedPattern
func buildPatternEvidenceMap(detected interface{}) map[string]*pattern.DetectedPattern {
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

// buildContextBreakdown summarises the dimensional/conditional distribution
// per flow across all analyzed flows. Returns flow → property → value → count.
func buildContextBreakdown(analyzedFlows interface{}) map[string]map[string]map[string]int {
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
		// Dimensions (e.g. device=mobile)
		for k, v := range f.Context.Dimensions {
			if props[k] == nil {
				props[k] = make(map[string]int)
			}
			props[k][v]++
		}
		// Conditions (e.g. error_code=card_declined)
		for k, v := range f.Context.Conditions {
			if props[k] == nil {
				props[k] = make(map[string]int)
			}
			props[k][fmt.Sprintf("%v", v)]++
		}
		// Targets (e.g. plan_name=premium)
		for k, v := range f.Context.Targets {
			if props[k] == nil {
				props[k] = make(map[string]int)
			}
			props[k][fmt.Sprintf("%v", v)]++
		}
	}
	return result
}

// sortedValueCounts formats a value→count map as a sorted readable string
// e.g. "mobile(3), desktop(2)"
func sortedValueCounts(values map[string]int) string {
	type vc struct {
		Value string
		Count int
	}
	var pairs []vc
	for v, c := range values {
		pairs = append(pairs, vc{v, c})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Count != pairs[j].Count {
			return pairs[i].Count > pairs[j].Count
		}
		return pairs[i].Value < pairs[j].Value
	})
	var parts []string
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("%s(%d)", p.Value, p.Count))
	}
	return strings.Join(parts, ", ")
}

// parseAIResponse extracts the structured response from AI output
func (a *Analyzer) parseAIResponse(content string) (*AnalysisResponse, error) {
	content = strings.TrimSpace(content)

	// Strategy 1: Try direct JSON parse first (ideal case)
	var response AnalysisResponse
	if err := json.Unmarshal([]byte(content), &response); err == nil {
		return &response, nil
	}

	// Strategy 2: Extract JSON from markdown code block (```json ... ```)
	// The LLM often wraps JSON in code blocks and adds commentary after
	if idx := strings.Index(content, "```json"); idx != -1 {
		after := content[idx+len("```json"):]
		if endIdx := strings.Index(after, "```"); endIdx != -1 {
			extracted := strings.TrimSpace(after[:endIdx])
			if err := json.Unmarshal([]byte(extracted), &response); err == nil {
				return &response, nil
			}
		}
	}

	// Strategy 3: Extract JSON from generic code block (``` ... ```)
	if idx := strings.Index(content, "```"); idx != -1 {
		after := content[idx+len("```"):]
		if endIdx := strings.Index(after, "```"); endIdx != -1 {
			extracted := strings.TrimSpace(after[:endIdx])
			if err := json.Unmarshal([]byte(extracted), &response); err == nil {
				return &response, nil
			}
		}
	}

	// Strategy 4: Find the first { and last } — extract the outermost JSON object
	firstBrace := strings.Index(content, "{")
	lastBrace := strings.LastIndex(content, "}")
	if firstBrace != -1 && lastBrace > firstBrace {
		extracted := content[firstBrace : lastBrace+1]
		if err := json.Unmarshal([]byte(extracted), &response); err == nil {
			return &response, nil
		}
	}

	// All strategies failed: return raw content as fallback
	return &AnalysisResponse{
		Summary:        "Analysis complete",
		Details:        []string{content},
		Hypotheses:     []string{},
		ConfidenceNote: "Raw AI response (structured parsing failed)",
	}, nil
}

// repairTruncatedJSON attempts to fix JSON that was truncated mid-generation
// by the LLM hitting max_tokens. It closes open strings, arrays, and objects.
func repairTruncatedJSON(content string) string {
	content = strings.TrimSpace(content)

	// Strip markdown code fences if present
	if strings.HasPrefix(content, "```json") {
		content = content[len("```json"):]
	} else if strings.HasPrefix(content, "```") {
		content = content[len("```"):]
	}
	content = strings.TrimSpace(content)

	// Track nesting state
	inString := false
	escaped := false
	var stack []byte // '{' or '['

	for i := 0; i < len(content); i++ {
		c := content[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '{':
			stack = append(stack, '{')
		case '[':
			stack = append(stack, '[')
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
	}

	// Close open string
	if inString {
		content += "\""
	}

	// Remove trailing comma (invalid JSON)
	content = strings.TrimRight(content, " \t\n\r")
	content = strings.TrimRight(content, ",")

	// Close open brackets/braces in reverse order
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '[' {
			content += "]"
		} else if stack[i] == '{' {
			content += "}"
		}
	}

	return content
}

// --- Event-level evidence for AI prompt enrichment ---

// patternEvidence holds per-pattern drill-down data for the AI prompt.
type patternEventEvidence struct {
	ErrorDistribution  map[string]int // error_code → count
	StatusDistribution map[string]int // status → count (error/failed/cancelled only)
	UserJourneys       []userJourney  // sample user event sequences
}

// userJourney is a compact representation of one user's event sequence in a flow.
type userJourney struct {
	UserID string
	Steps  []string // e.g. ["playback_started(start)", "playback_error(error,error_code=drm_license_failed)"]
}

// buildEventLevelEvidence constructs per-pattern drill-down data from the
// analyzed flows. For each detected pattern, it finds the affected flows,
// extracts error code and status distributions, and builds sample user
// journeys with event-level detail.
//
// Returns: map of "pattern_type:flow" → evidence.
func buildEventLevelEvidence(detected interface{}, analyzedFlows interface{}) map[string]*patternEventEvidence {
	result := make(map[string]*patternEventEvidence)

	patterns, ok := detected.([]*pattern.DetectedPattern)
	if !ok || len(patterns) == 0 {
		return result
	}
	flows, ok := analyzedFlows.([]*behavior.AnalyzedFlow)
	if !ok || len(flows) == 0 {
		return result
	}

	// Index flows by flow name for fast lookup
	flowsByName := make(map[string][]*behavior.AnalyzedFlow)
	for _, f := range flows {
		flowsByName[f.Flow] = append(flowsByName[f.Flow], f)
	}

	for _, p := range patterns {
		key := string(p.Pattern) + ":" + p.Flow
		group := flowsByName[p.Flow]
		if len(group) == 0 {
			continue
		}

		ev := &patternEventEvidence{
			ErrorDistribution:  make(map[string]int),
			StatusDistribution: make(map[string]int),
		}

		// Track which users are affected (have the pattern's behavior)
		affectedUsers := findAffectedUsers(p, group)

		// Collect error/status distributions from ALL events in affected flows
		for _, flow := range group {
			if !affectedUsers[flow.UserID] {
				continue
			}
			for _, event := range flow.Events {
				// Count error/failure statuses
				if event.Status == "error" || event.Status == "failed" || event.Status == "cancelled" {
					ev.StatusDistribution[event.Status]++
				}
				// Extract error_code from event context conditions
				if event.Context != nil && len(event.Context.Conditions) > 0 {
					for condKey, condVal := range event.Context.Conditions {
						if strings.Contains(strings.ToLower(condKey), "error") ||
							strings.Contains(strings.ToLower(condKey), "reason") ||
							strings.Contains(strings.ToLower(condKey), "code") {
							ev.ErrorDistribution[fmt.Sprintf("%s=%v", condKey, condVal)]++
						}
					}
				}
			}
		}

		// Build sample user journeys (up to 3 affected users)
		journeyCount := 0
		for _, flow := range group {
			if journeyCount >= 3 {
				break
			}
			if !affectedUsers[flow.UserID] {
				continue
			}
			// Skip if we already have a journey for this user
			alreadyHave := false
			for _, uj := range ev.UserJourneys {
				if uj.UserID == flow.UserID {
					alreadyHave = true
					break
				}
			}
			if alreadyHave {
				continue
			}

			uj := userJourney{UserID: flow.UserID}
			for _, event := range flow.Events {
				step := event.RawEventName
				if event.Status != "" {
					step += "(" + event.Status
					// Append error_code inline if present
					if event.Context != nil {
						for condKey, condVal := range event.Context.Conditions {
							if strings.Contains(strings.ToLower(condKey), "error") ||
								strings.Contains(strings.ToLower(condKey), "code") {
								step += fmt.Sprintf(",%s=%v", condKey, condVal)
							}
						}
					}
					step += ")"
				}
				uj.Steps = append(uj.Steps, step)
			}
			ev.UserJourneys = append(ev.UserJourneys, uj)
			journeyCount++
		}

		result[key] = ev
	}

	return result
}

// findAffectedUsers identifies which users exhibit the given pattern's behavior.
func findAffectedUsers(p *pattern.DetectedPattern, group []*behavior.AnalyzedFlow) map[string]bool {
	affected := make(map[string]bool)

	targetBehavior := patternToBehavior(p.Pattern)

	for _, flow := range group {
		// Check if flow has the target behavior
		for _, b := range flow.Behaviors {
			if b == targetBehavior {
				affected[flow.UserID] = true
				break
			}
		}
		// Also check for error/failure evidence for retry and failure patterns
		if p.Pattern == pattern.PatternRetryStorm || p.Pattern == pattern.PatternMaskedFailure {
			for _, event := range flow.Events {
				if event.Status == "error" || event.Status == "failed" {
					affected[flow.UserID] = true
					break
				}
			}
		}
		// For silent abandonment, check for abandon or incomplete flows
		if p.Pattern == pattern.PatternSilentAbandonment {
			if !flow.IsComplete && containsBehaviorAI(flow.Behaviors, behavior.BehaviorAbandon) {
				affected[flow.UserID] = true
			}
		}
	}

	// If no specific behavior match, include all users (fallback)
	if len(affected) == 0 {
		for _, flow := range group {
			affected[flow.UserID] = true
		}
	}

	return affected
}

// patternToBehavior maps a pattern type to the primary behavior it detects.
func patternToBehavior(pt pattern.PatternType) behavior.BehaviorType {
	switch pt {
	case pattern.PatternRetryStorm:
		return behavior.BehaviorRetry
	case pattern.PatternConfusionLoop:
		return behavior.BehaviorHesitate
	case pattern.PatternSilentAbandonment:
		return behavior.BehaviorAbandon
	case pattern.PatternEarlyDropoff:
		return behavior.BehaviorExplore
	case pattern.PatternBypassBehavior:
		return behavior.BehaviorBypass
	case pattern.PatternMaskedFailure:
		return behavior.BehaviorRetry
	default:
		return ""
	}
}

// containsBehaviorAI is a local helper (same as pattern package's containsBehavior
// but accessible here without circular imports).
func containsBehaviorAI(behaviors []behavior.BehaviorType, target behavior.BehaviorType) bool {
	for _, b := range behaviors {
		if b == target {
			return true
		}
	}
	return false
}
