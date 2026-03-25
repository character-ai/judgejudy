package evaluator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/character-ai/judgejudy/internal/models"
)

// CompositeEvaluator runs multiple child evaluators and returns a weighted average.
type CompositeEvaluator struct {
	name     string
	children []Evaluator
	weights  []float64
}

// NewCompositeEvaluator creates a composite evaluator from children and their weights.
// Weights do not need to sum to 1; they are normalized internally.
func NewCompositeEvaluator(name string, children []Evaluator, weights []float64) *CompositeEvaluator {
	if len(weights) != len(children) {
		// Default to equal weights if mismatch.
		weights = make([]float64, len(children))
		for i := range weights {
			weights[i] = 1.0
		}
	}
	return &CompositeEvaluator{
		name:     name,
		children: children,
		weights:  weights,
	}
}

func (c *CompositeEvaluator) Name() string           { return c.name }
func (c *CompositeEvaluator) Type() models.EvalType   { return models.EvalTypeComposite }

// Evaluate runs all children and returns the weighted average score.
// Individual child scores are stored in Score.RawValue.
func (c *CompositeEvaluator) Evaluate(ctx context.Context, input models.TestCase, output models.GenerateResponse) (*models.Score, error) {
	if len(c.children) == 0 {
		return nil, fmt.Errorf("composite evaluator %q: no children configured", c.name)
	}

	childScores := make(map[string]models.Score, len(c.children))
	var weightedSum float64
	var totalWeight float64
	var allPassed = true
	hasPassed := false

	failedCount := 0
	for i, child := range c.children {
		score, err := child.Evaluate(ctx, input, output)
		if err != nil {
			slog.Warn("composite evaluator: child failed, skipping",
				"composite", c.name, "child", child.Name(), "error", err)
			failedCount++
			continue
		}

		childScores[child.Name()] = *score
		w := c.weights[i]
		weightedSum += score.Value * w
		totalWeight += w

		if score.Passed != nil {
			hasPassed = true
			if !*score.Passed {
				allPassed = false
			}
		}
	}

	if failedCount == len(c.children) {
		return nil, fmt.Errorf("composite evaluator %q: all children failed", c.name)
	}

	avg := 0.0
	if totalWeight > 0 {
		avg = weightedSum / totalWeight
	}

	result := &models.Score{
		Value:    avg,
		RawValue: childScores,
	}

	if hasPassed {
		result.Passed = &allPassed
	}

	return result, nil
}
