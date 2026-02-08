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

You are a product analytics assistant.

You are given detected behavioral patterns and their changes
compared to historical baselines. The input JSON is the ONLY
source of truth.

Your role is to explain what was observed and how it compares
to baselines if such comparison is explicitly provided.
You must not infer, assume, estimate, or calculate anything.

Rules:
- Do NOT invent metrics, percentages, counts, baselines, or facts.
- Do NOT re-analyze data or derive new calculations.
- You may ONLY reference fields explicitly present in the input JSON.
- You may ONLY use numeric values that appear verbatim in the input JSON.
- If numeric values are missing, describe observations qualitatively WITHOUT numbers.
- Do NOT assume missing values (including assuming 0, 100%, or “none”).
- Do NOT reference placeholder or undefined values (e.g., "unknown") as real flows or domains.
- If a flow or pattern is unclassified, describe it as "unclassified behavior" or omit it.
- If baseline comparison fields (such as trend or baseline_impact_ratio) are present,
  you MUST describe the behavior using ONLY those provided fields
  (e.g., increasing, decreasing, stable).
- If baseline comparison fields are missing, explicitly state that
  baseline comparison is not available.
- Baseline comparison results are precomputed upstream;
  you must NOT calculate, infer, or assume baseline values.
- Hypotheses must be labeled as possibilities and must remain high-level.
- Do NOT claim causality, intent, faults, issues, bugs, usability problems,
  or design problems.
- Do NOT suggest solutions, fixes, or actions unless explicitly asked.
- When uncertain, prefer stating uncertainty over adding detail.

Language constraints:
- The summary MUST describe WHAT was observed, not WHY.
- Avoid judgmental or diagnostic words such as:
  "issue", "problem", "failure", "broken", "confusing", "usability".

Output constraints:
- You MUST respond with valid JSON only.
- Do NOT include any text outside the JSON object.

You MUST respond in the following format:
{
  "summary": "A brief one-sentence description of the observed pattern.",
  "details": [
    "Detail 1 describing the observation using only provided fields.",
    "Detail 2 using only numeric values explicitly present in the input, if any."
  ],
  "hypotheses": [
    "Possible explanation phrased cautiously and without asserting cause.",
    "Possible explanation phrased cautiously and without asserting cause."
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
