package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/velum/internal/layers/baseline"
)

const (
	groqAPIEndpoint = "https://api.groq.com/openai/v1/chat/completions"
	systemPrompt    = `You are a product analytics assistant.

You are a product analytics interpretation assistant.

You are given detected behavioral patterns and optional baseline comparison
results. The input JSON is the ONLY source of truth.

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

REQUIRED OUTPUT FORMAT:

{
  "summary": "A single sentence describing the observed pattern(s) using only provided pattern_type and flow values.",
  "details": [
    "Detail describing the observation using only fields explicitly present in the input.",
    "Detail describing baseline availability or comparison status, if applicable."
  ],
  "hypotheses": [
    "Possible explanation phrased cautiously and without asserting cause or diagnosis.",
    "Possible explanation phrased cautiously and without asserting cause or diagnosis."
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

// Process implements the Layer interface
func (a *Analyzer) Process(input interface{}) (interface{}, error) {
	// If AI is disabled, pass through the baseline result as AIResult
	if !a.config.Enabled || a.config.APIKey == "" {
		if a.config.Debug {
			fmt.Println("[DEBUG] [AI] AI layer disabled, passing through data")
		}
		return a.passThrough(input)
	}

	// Only process baseline results
	baselineResult, ok := input.(*baseline.BaselineResult)
	if !ok {
		if a.config.Debug {
			fmt.Printf("[DEBUG] [AI] Input is not BaselineResult, passing through (type: %T)\n", input)
		}
		return a.passThrough(input)
	}

	if a.config.Debug {
		fmt.Println("[DEBUG] [AI] Starting AI analysis...")
		fmt.Printf("[DEBUG] [AI] Model: %s\n", a.config.Model)
		fmt.Printf("[DEBUG] [AI] Change results to analyze: %d\n", len(baselineResult.ChangeResults))
	}

	// Check circuit breaker before making AI request
	if err := a.circuitBreaker.Allow(); err != nil {
		if a.config.Debug {
			fmt.Println("[DEBUG] [AI] Circuit breaker is open, skipping AI analysis")
		}
		return &AIResult{
			ChangeResults: baselineResult.ChangeResults,
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
	analysis, err := a.analyze(baselineResult)
	if err != nil {
		a.circuitBreaker.RecordFailure()
		if a.config.Debug {
			fmt.Printf("[DEBUG] [AI] Analysis failed: %v\n", err)
		}
		// On error, return result without AI analysis but with error info
		return &AIResult{
			ChangeResults: baselineResult.ChangeResults,
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
		fmt.Println("[DEBUG] [AI] Analysis completed successfully")
		fmt.Printf("[DEBUG] [AI] Summary: %s\n", analysis.Summary)
	}

	return &AIResult{
		ChangeResults: baselineResult.ChangeResults,
		AIAnalysis:    analysis,
		AIEnabled:     true,
	}, nil
}

// passThrough creates an AIResult from the input without AI analysis
// When AI is disabled, only return change_results
func (a *Analyzer) passThrough(input interface{}) (*AIResult, error) {
	switch v := input.(type) {
	case *baseline.BaselineResult:
		return &AIResult{
			ChangeResults: v.ChangeResults,
			AIAnalysis:    nil,
			AIEnabled:     false,
		}, nil
	default:
		// For other types, wrap minimally
		return &AIResult{
			AIEnabled: false,
		}, nil
	}
}

// analyze performs the actual AI analysis using Groq API
func (a *Analyzer) analyze(baselineResult *baseline.BaselineResult) (*AnalysisResponse, error) {
	// Build the user prompt with baseline data
	userPrompt, err := a.buildUserPrompt(baselineResult)
	if err != nil {
		return nil, fmt.Errorf("failed to build prompt: %w", err)
	}

	if a.config.Debug {
		fmt.Printf("[DEBUG] [AI] Built prompt (%d chars)\n", len(userPrompt))
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
		MaxTokens:   1024,
	}

	// Make the API request
	reqBody, err := json.Marshal(groqReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if a.config.Debug {
		fmt.Println("[DEBUG] [AI] Sending request to Groq API...")
	}

	req, err := http.NewRequest("POST", groqAPIEndpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.APIKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if a.config.Debug {
		fmt.Printf("[DEBUG] [AI] Groq API response status: %d\n", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
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

	// Parse the AI response content as JSON
	content := groqResp.Choices[0].Message.Content
	return a.parseAIResponse(content)
}

// buildUserPrompt creates the prompt with baseline data
func (a *Analyzer) buildUserPrompt(baselineResult *baseline.BaselineResult) (string, error) {
	var sb strings.Builder

	sb.WriteString("Please analyze the following behavioral pattern data and provide insights:\n\n")

	// Add change results if present
	if len(baselineResult.ChangeResults) > 0 {
		sb.WriteString("## Detected Changes:\n")
		for _, change := range baselineResult.ChangeResults {
			// Check if baseline is available (not first observation)
			if change.BaselineStatus == baseline.BaselineStatusFirstObservation {
				// First observation: only send minimal data without baseline metrics
				sb.WriteString(fmt.Sprintf("- Pattern: %s, Flow: %s\n", change.PatternType, change.Flow))
				sb.WriteString("  Baseline Available: false\n\n")
			} else {
				// Baseline exists: send full data with comparison metrics
				sb.WriteString(fmt.Sprintf("- Pattern: %s, Flow: %s\n", change.PatternType, change.Flow))
				sb.WriteString(fmt.Sprintf("  Current Impact: %.2f%%, Baseline: %.2f%%\n",
					change.CurrentImpactRatio*100, change.BaselineImpactRatio*100))
				sb.WriteString(fmt.Sprintf("  Delta: %.2f%%, Trend: %s, Significance: %s\n",
					change.DeltaPercentage*100, change.Trend, change.ChangeSignificance))
				sb.WriteString(fmt.Sprintf("  Baseline Window: %s (%d days)\n\n",
					change.BaselineWindow, change.BaselineDays))
			}
		}
	} else {
		sb.WriteString("No significant changes detected compared to baseline.\n")
	}

	sb.WriteString("\nProvide your analysis in the required JSON format.")

	return sb.String(), nil
}

// parseAIResponse extracts the structured response from AI output
func (a *Analyzer) parseAIResponse(content string) (*AnalysisResponse, error) {
	// Clean up the content - remove markdown code blocks if present
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
	}
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
	}
	if strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(content, "```")
	}
	content = strings.TrimSpace(content)

	var response AnalysisResponse
	if err := json.Unmarshal([]byte(content), &response); err != nil {
		// If parsing fails, return a basic response with the raw content
		return &AnalysisResponse{
			Summary:        "Analysis complete",
			Details:        []string{content},
			Hypotheses:     []string{},
			ConfidenceNote: "Raw AI response (structured parsing failed)",
		}, nil
	}

	return &response, nil
}
