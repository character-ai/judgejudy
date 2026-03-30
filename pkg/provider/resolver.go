package provider

import (
	"context"

	"github.com/character-ai/judgejudy/pkg/evaluator"
	"github.com/character-ai/judgejudy/pkg/models"
)

// DefaultResolver returns a provider resolver that creates provider call
// functions on demand using the global provider registry and environment
// API keys.
func DefaultResolver() evaluator.ProviderResolver {
	return func(provName, model string) (evaluator.ProviderFunc, error) {
		p, err := NewProvider(provName, "")
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, req models.GenerateRequest) (*models.GenerateResponse, error) {
			if model != "" {
				if req.Params == nil {
					req.Params = make(map[string]any)
				}
				if _, ok := req.Params["model"]; !ok {
					req.Params["model"] = model
				}
			}
			return p.Generate(ctx, &req)
		}, nil
	}
}
