package config

import (
	"fmt"
	"strings"

	"github.com/character-ai/judgejudy/internal/models"
)

var knownProviders = map[string]bool{
	"openai":     true,
	"anthropic":  true,
	"google":     true,
	"replicate":  true,
	"together":   true,
	"ollama":     true,
	"elevenlabs": true,
	"cartesia":   true,
	"falai":      true,
	"wavespeed":    true,
	"passthrough": true,
}

// ValidationErrors collects multiple validation errors
type ValidationErrors struct {
	Errors []string
}

func (e *ValidationErrors) Error() string {
	return "validation errors:\n  - " + strings.Join(e.Errors, "\n  - ")
}

func (e *ValidationErrors) add(msg string) {
	e.Errors = append(e.Errors, msg)
}

// Validate checks the config for errors, collecting all issues
func Validate(cfg *EvalConfig) error {
	errs := &ValidationErrors{}

	if cfg.Name == "" {
		errs.add("name is required")
	}

	// Dataset validation
	if cfg.Dataset.Path == "" && cfg.Dataset.Inline == nil {
		errs.add("dataset: either path or inline must be set")
	}
	if cfg.Dataset.Path != "" && cfg.Dataset.Inline != nil {
		errs.add("dataset: path and inline are mutually exclusive")
	}
	if cfg.Dataset.Sample < 0 {
		errs.add("dataset.sample must be non-negative")
	}

	// Generator validation
	if !knownProviders[cfg.Generator.Provider] {
		errs.add(fmt.Sprintf("generator.provider: unknown provider %q", cfg.Generator.Provider))
	}
	if cfg.Generator.Model == "" {
		errs.add("generator.model is required")
	}

	// Evaluator validation
	if len(cfg.Evaluators) == 0 {
		errs.add("at least one evaluator is required")
	}
	for i, ev := range cfg.Evaluators {
		validateEvaluator(errs, ev, fmt.Sprintf("evaluators[%d]", i))
	}

	if len(errs.Errors) > 0 {
		return errs
	}
	return nil
}

func validateEvaluator(errs *ValidationErrors, ev models.EvaluatorConfig, prefix string) {
	if ev.Name == "" {
		errs.add(fmt.Sprintf("%s: name is required", prefix))
	}

	switch ev.Type {
	case models.EvalTypeAIJudge:
		if ev.Provider == "" {
			errs.add(fmt.Sprintf("%s: ai_judge requires provider", prefix))
		}
		if ev.Model == "" {
			errs.add(fmt.Sprintf("%s: ai_judge requires model", prefix))
		}
		if ev.Rubric == "" {
			errs.add(fmt.Sprintf("%s: ai_judge requires rubric", prefix))
		}
	case models.EvalTypeMetric:
		if ev.Metric == "" {
			errs.add(fmt.Sprintf("%s: metric evaluator requires metric name", prefix))
		}
	case models.EvalTypeComposite:
		if len(ev.Children) == 0 {
			errs.add(fmt.Sprintf("%s: composite evaluator requires children", prefix))
		}
		for j, child := range ev.Children {
			validateEvaluator(errs, child, fmt.Sprintf("%s.children[%d]", prefix, j))
		}
	default:
		errs.add(fmt.Sprintf("%s: unknown evaluator type %q", prefix, ev.Type))
	}

	if ev.Threshold != nil && (*ev.Threshold < 0.0 || *ev.Threshold > 1.0) {
		errs.add(fmt.Sprintf("%s: threshold must be between 0.0 and 1.0", prefix))
	}
}
