package models

import (
	"time"

	"github.com/google/uuid"
)

// Modality enum
type Modality string

const (
	ModalityText  Modality = "text"
	ModalityImage Modality = "image"
	ModalityAudio Modality = "audio"
	ModalityVideo Modality = "video"
)

// EvalType enum
type EvalType string

const (
	EvalTypeMetric    EvalType = "metric"
	EvalTypeAIJudge   EvalType = "ai_judge"
	EvalTypeComposite EvalType = "composite"
)

// JudgeMode enum
type JudgeMode string

const (
	JudgeModePointwise JudgeMode = "pointwise"
	JudgeModePairwise  JudgeMode = "pairwise"
)

// GenerateRequest — input to a provider
type GenerateRequest struct {
	Prompt          string         `json:"prompt"`
	Params          map[string]any `json:"params,omitempty"`
	Modality        Modality       `json:"modality"`
	ReferenceInputs []string      `json:"reference_inputs,omitempty"`
}

// GenerateResponse — output from a provider
type GenerateResponse struct {
	Content     string  `json:"content"`
	ContentType string  `json:"content_type"`
	FilePath    string  `json:"file_path,omitempty"`
	LatencyMs   int64   `json:"latency_ms"`
	CostUSD     float64 `json:"cost_usd"`
	TokensUsed  int     `json:"tokens_used,omitempty"`
	ModelUsed   string  `json:"model_used"`
	Raw         any     `json:"raw,omitempty"`
}

// TestCase — a single evaluation test case
type TestCase struct {
	ID             string         `json:"id" yaml:"id"`
	Input          string         `json:"input" yaml:"input"`
	ExpectedOutput string         `json:"expected_output,omitempty" yaml:"expected_output,omitempty"`
	ReferenceFiles []string       `json:"reference_files,omitempty" yaml:"reference_files,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Tags           []string       `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// Dataset — a collection of test cases
type Dataset struct {
	ID          string     `json:"id" yaml:"id"`
	Name        string     `json:"name" yaml:"name"`
	Version     string     `json:"version" yaml:"version"`
	Modality    Modality   `json:"modality" yaml:"modality"`
	Description string     `json:"description,omitempty" yaml:"description,omitempty"`
	TestCases   []TestCase `json:"test_cases" yaml:"test_cases"`
}

// Score — result from a single evaluator on a single test case
type Score struct {
	Value     float64 `json:"value"`
	RawValue  any     `json:"raw_value,omitempty"`
	Reasoning string  `json:"reasoning,omitempty"`
	Passed    *bool   `json:"passed,omitempty"`
}

// TestResult — all scores for a single test case
type TestResult struct {
	TestCaseID      string           `json:"test_case_id"`
	Input           string           `json:"input"`
	GeneratedOutput GenerateResponse `json:"generated_output"`
	Scores          map[string]Score `json:"scores"`
}

// AggregateMetrics — summary stats for a run
type AggregateMetrics struct {
	MeanScores    map[string]float64 `json:"mean_scores"`
	MedianScores  map[string]float64 `json:"median_scores"`
	P5Scores      map[string]float64 `json:"p5_scores"`
	P95Scores     map[string]float64 `json:"p95_scores"`
	PassRates     map[string]float64 `json:"pass_rates"`
	TotalPassRate float64            `json:"total_pass_rate"`
}

// Run — a complete evaluation run
type Run struct {
	ID               string            `json:"id"`
	Timestamp        time.Time         `json:"timestamp"`
	DatasetID        string            `json:"dataset_id"`
	DatasetVersion   string            `json:"dataset_version"`
	GeneratorConfig  ProviderConfig    `json:"generator_config"`
	EvaluatorNames   []string          `json:"evaluator_names"`
	EvaluatorConfigs []EvaluatorConfig `json:"evaluator_configs,omitempty"`
	Results          []TestResult      `json:"results"`
	Aggregate        AggregateMetrics  `json:"aggregate"`
	TotalCostUSD     float64           `json:"total_cost_usd"`
	DurationSeconds  float64           `json:"duration_seconds"`
	Metadata         map[string]any    `json:"metadata,omitempty"`
	IsBaseline       bool              `json:"is_baseline"`
}

// ProviderConfig — identifies a model provider + model
type ProviderConfig struct {
	Provider string         `json:"provider" yaml:"provider"`
	Model    string         `json:"model" yaml:"model"`
	Params   map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
}

// EvaluatorConfig — configuration for one evaluator in a pipeline
type EvaluatorConfig struct {
	Name       string            `json:"name" yaml:"name"`
	Type       EvalType          `json:"type" yaml:"type"`
	Provider   string            `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model      string            `json:"model,omitempty" yaml:"model,omitempty"`
	Metric     string            `json:"metric,omitempty" yaml:"metric,omitempty"`
	Rubric     string            `json:"rubric,omitempty" yaml:"rubric,omitempty"`
	Dimensions []string          `json:"dimensions,omitempty" yaml:"dimensions,omitempty"`
	Scale      [2]int            `json:"scale,omitempty" yaml:"scale,omitempty"`
	Mode       JudgeMode         `json:"mode,omitempty" yaml:"mode,omitempty"`
	Threshold  *float64          `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	Weight     float64           `json:"weight,omitempty" yaml:"weight,omitempty"`
	Children   []EvaluatorConfig `json:"children,omitempty" yaml:"children,omitempty"`
	Params     map[string]any    `json:"params,omitempty" yaml:"params,omitempty"`
}

// Comparison — result of comparing two runs
type Comparison struct {
	BaselineRunID  string                 `json:"baseline_run_id"`
	CandidateRunID string                 `json:"candidate_run_id"`
	Deltas         map[string]MetricDelta `json:"deltas"`
	Regressions    []string               `json:"regressions"`
	Improvements   []string               `json:"improvements"`
}

// MetricDelta — delta between two runs for a single evaluator
type MetricDelta struct {
	BaselineMean  float64 `json:"baseline_mean"`
	CandidateMean float64 `json:"candidate_mean"`
	AbsoluteDelta float64 `json:"absolute_delta"`
	PercentDelta  float64 `json:"percent_delta"`
	Improved      bool    `json:"improved"`
}

// ProviderError wraps provider-specific errors
type ProviderError struct {
	Provider   string `json:"provider"`
	StatusCode int    `json:"status_code,omitempty"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable"`
	Err        error  `json:"-"`
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return e.Provider + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.Provider + ": " + e.Message
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// NewID generates a new UUID v4 string
func NewID() string {
	return uuid.New().String()
}
