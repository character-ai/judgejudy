package config

import (
	"os"

	"github.com/character-ai/judgejudy/pkg/models"
	"gopkg.in/yaml.v3"
)

// EvalConfig is the top-level evaluation configuration
type EvalConfig struct {
	Name        string                  `yaml:"name"`
	Description string                  `yaml:"description,omitempty"`
	Dataset     DatasetRef              `yaml:"dataset"`
	Generator   models.ProviderConfig   `yaml:"generator"`
	Evaluators  []models.EvaluatorConfig `yaml:"evaluators"`
	Pipeline    PipelineConfig          `yaml:"pipeline,omitempty"`
	Report      ReportConfig            `yaml:"report,omitempty"`
}

// DatasetRef references a dataset either by path or inline
type DatasetRef struct {
	Path   string          `yaml:"path,omitempty"`
	Inline *models.Dataset `yaml:"inline,omitempty"`
	Sample int             `yaml:"sample,omitempty"`
}

// PipelineConfig controls pipeline execution
type PipelineConfig struct {
	Concurrency    int  `yaml:"concurrency,omitempty"`
	TimeoutSeconds int  `yaml:"timeout_seconds,omitempty"`
	RetryAttempts  int  `yaml:"retry_attempts,omitempty"`
	CacheEnabled   bool `yaml:"cache_enabled,omitempty"`
	FailFast       bool `yaml:"fail_fast,omitempty"`
}

// ReportConfig controls report generation
type ReportConfig struct {
	OutputPath string `yaml:"output_path,omitempty"`
	Title      string `yaml:"title,omitempty"`
}

// Defaults applies default values to the config
func (c *EvalConfig) Defaults() {
	if c.Pipeline.Concurrency <= 0 {
		c.Pipeline.Concurrency = 5
	}
	if c.Pipeline.TimeoutSeconds <= 0 {
		c.Pipeline.TimeoutSeconds = 120
	}
	if c.Pipeline.RetryAttempts < 0 {
		c.Pipeline.RetryAttempts = 2
	}
	// Note: Report.OutputPath intentionally not defaulted —
	// report generation is opt-in via --report flag or config.
}

// LoadConfig reads and validates a YAML config file
func LoadConfig(path string) (*EvalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg EvalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	cfg.Defaults()

	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
