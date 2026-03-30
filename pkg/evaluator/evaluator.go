package evaluator

import (
	"context"
	"fmt"

	"github.com/character-ai/judgejudy/pkg/models"
)

// ProviderFunc is a function type that calls an AI provider.
// This decouples the evaluator package from any concrete provider implementation.
type ProviderFunc func(ctx context.Context, req models.GenerateRequest) (*models.GenerateResponse, error)

// ProviderResolver returns a ProviderFunc for a given provider name and model.
type ProviderResolver func(provider, model string) (ProviderFunc, error)

// Evaluator is the interface every evaluator must implement.
type Evaluator interface {
	// Name returns the evaluator's unique name.
	Name() string
	// Type returns the evaluator type (metric, ai_judge, composite).
	Type() models.EvalType
	// Evaluate scores a single test case against the generated output.
	Evaluate(ctx context.Context, input models.TestCase, output models.GenerateResponse) (*models.Score, error)
}

// NewEvaluator constructs an Evaluator from config. The resolver is used to
// obtain provider functions for AI judge evaluators. It may be nil if no
// ai_judge evaluators are expected.
func NewEvaluator(cfg models.EvaluatorConfig, resolver ProviderResolver) (Evaluator, error) {
	switch cfg.Type {
	case models.EvalTypeAIJudge:
		if resolver == nil {
			return nil, fmt.Errorf("evaluator %q: ai_judge requires a provider resolver", cfg.Name)
		}
		provFn, err := resolver(cfg.Provider, cfg.Model)
		if err != nil {
			return nil, fmt.Errorf("evaluator %q: resolve provider: %w", cfg.Name, err)
		}
		return NewJudgeEvaluator(cfg, provFn)

	case models.EvalTypeMetric:
		return NewMetricsEvaluator(cfg)

	case models.EvalTypeComposite:
		children := make([]Evaluator, 0, len(cfg.Children))
		for _, childCfg := range cfg.Children {
			child, err := NewEvaluator(childCfg, resolver)
			if err != nil {
				return nil, fmt.Errorf("evaluator %q: child %q: %w", cfg.Name, childCfg.Name, err)
			}
			children = append(children, child)
		}
		weights := make([]float64, len(cfg.Children))
		for i, childCfg := range cfg.Children {
			w := childCfg.Weight
			if w == 0 {
				w = 1.0
			}
			weights[i] = w
		}
		return NewCompositeEvaluator(cfg.Name, children, weights), nil

	default:
		return nil, fmt.Errorf("evaluator %q: unknown type %q", cfg.Name, cfg.Type)
	}
}
