package evaluator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/character-ai/judgejudy/internal/models"
)

// MetricsEvaluator shells out to a Python metrics script.
type MetricsEvaluator struct {
	name      string
	metric    string
	threshold *float64
	params    map[string]any
}

// metricsInput is the JSON sent to the Python script.
type metricsInput struct {
	Input          string         `json:"input"`
	ExpectedOutput string         `json:"expected_output,omitempty"`
	GeneratedOutput string        `json:"generated_output"`
	ContentType    string         `json:"content_type"`
	FilePath       string         `json:"file_path,omitempty"`
	Metric         string         `json:"metric"`
	Params         map[string]any `json:"params,omitempty"`
}

// metricsOutput is the JSON expected from the Python script.
type metricsOutput struct {
	Score float64 `json:"score"`
	Raw   any     `json:"raw,omitempty"`
	Error *string `json:"error"`
}

// NewMetricsEvaluator creates a new Python metrics bridge evaluator.
func NewMetricsEvaluator(cfg models.EvaluatorConfig) (*MetricsEvaluator, error) {
	if cfg.Metric == "" {
		return nil, fmt.Errorf("metric evaluator %q: metric name is required", cfg.Name)
	}
	return &MetricsEvaluator{
		name:      cfg.Name,
		metric:    cfg.Metric,
		threshold: cfg.Threshold,
		params:    cfg.Params,
	}, nil
}

func (m *MetricsEvaluator) Name() string           { return m.name }
func (m *MetricsEvaluator) Type() models.EvalType   { return models.EvalTypeMetric }

// Evaluate shells out to the appropriate Python metric script.
func (m *MetricsEvaluator) Evaluate(ctx context.Context, input models.TestCase, output models.GenerateResponse) (*models.Score, error) {
	// Determine modality from content type for script selection.
	modality := detectModality(output.ContentType)

	// For non-text modalities, if the content is base64 and no file path exists,
	// decode to a temp file so Python metrics can read it.
	filePath := output.FilePath
	if filePath == "" && modality != "text" && output.Content != "" {
		ext := extForContentType(output.ContentType)
		tmpFile, tmpErr := os.CreateTemp("", "jj_media_*"+ext)
		if tmpErr == nil {
			data, decErr := base64.StdEncoding.DecodeString(output.Content)
			if decErr == nil {
				tmpFile.Write(data)
				tmpFile.Close()
				filePath = tmpFile.Name()
				defer os.Remove(filePath)
			} else {
				tmpFile.Close()
				os.Remove(tmpFile.Name())
			}
		}
	}

	// Build input payload.
	payload := metricsInput{
		Input:           input.Input,
		ExpectedOutput:  input.ExpectedOutput,
		GeneratedOutput: output.Content,
		ContentType:     output.ContentType,
		FilePath:        filePath,
		Metric:          m.metric,
		Params:          m.params,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("metric %q: marshal input: %w", m.name, err)
	}

	// Write input to temp file.
	inputFile, err := os.CreateTemp("", "jj_input_*.json")
	if err != nil {
		return nil, fmt.Errorf("metric %q: create temp input: %w", m.name, err)
	}
	defer os.Remove(inputFile.Name())

	if _, err := inputFile.Write(payloadBytes); err != nil {
		inputFile.Close()
		return nil, fmt.Errorf("metric %q: write temp input: %w", m.name, err)
	}
	inputFile.Close()

	// Prepare output temp file.
	outputFile, err := os.CreateTemp("", "jj_output_*.json")
	if err != nil {
		return nil, fmt.Errorf("metric %q: create temp output: %w", m.name, err)
	}
	defer os.Remove(outputFile.Name())
	outputFile.Close()

	// Determine script path. Try multiple locations:
	// 1. Relative to CWD (for development)
	// 2. Relative to the executable (for installed binaries)
	scriptName := modality + "_metrics.py"
	scriptPath := filepath.Join("python", "metrics", scriptName)
	if _, err := os.Stat(scriptPath); err != nil {
		exePath, exeErr := os.Executable()
		if exeErr == nil {
			altPath := filepath.Join(filepath.Dir(exePath), "..", "python", "metrics", scriptName)
			if _, statErr := os.Stat(altPath); statErr == nil {
				scriptPath = altPath
			}
		}
	}

	// Execute Python script.
	cmd := exec.CommandContext(ctx, "python3", scriptPath,
		"--input", inputFile.Name(),
		"--output", outputFile.Name(),
	)
	cmdOutput, runErr := cmd.CombinedOutput()

	// Read output file first — the Python script writes structured errors there
	// even on non-zero exit.
	outputBytes, err := os.ReadFile(outputFile.Name())
	if err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("python metric %q failed: %v\noutput: %s",
				m.metric, runErr, string(cmdOutput))
		}
		return nil, fmt.Errorf("metric %q: read output: %w", m.name, err)
	}

	var result metricsOutput
	if err := json.Unmarshal(outputBytes, &result); err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("python metric %q failed: %v\noutput: %s",
				m.metric, runErr, string(cmdOutput))
		}
		return nil, fmt.Errorf("metric %q: parse output: %w\nraw: %s", m.name, err, string(outputBytes))
	}

	if result.Error != nil && *result.Error != "" {
		return nil, fmt.Errorf("metric %q: python error: %s", m.name, *result.Error)
	}

	score := &models.Score{
		Value:    result.Score,
		RawValue: result.Raw,
	}

	if m.threshold != nil {
		passed := result.Score >= *m.threshold
		score.Passed = &passed
	}

	return score, nil
}

// extForContentType returns a file extension for a content type.
func extForContentType(ct string) string {
	switch {
	case strings.Contains(ct, "wav"):
		return ".wav"
	case strings.Contains(ct, "mp3"), strings.Contains(ct, "mpeg"):
		return ".mp3"
	case strings.Contains(ct, "mp4"):
		return ".mp4"
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		return ".jpg"
	case strings.Contains(ct, "webp"):
		return ".webp"
	default:
		return ".bin"
	}
}

// detectModality infers the modality string from a content type.
func detectModality(contentType string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "image"):
		return "image"
	case strings.HasPrefix(ct, "audio"):
		return "audio"
	case strings.HasPrefix(ct, "video"):
		return "video"
	default:
		return "text"
	}
}
