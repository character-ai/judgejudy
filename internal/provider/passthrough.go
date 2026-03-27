package provider

import (
	"context"
	"fmt"

	"github.com/character-ai/judgejudy/internal/models"
)

func init() {
	Register("passthrough", func(_ string) (Provider, error) {
		return &PassthroughProvider{}, nil
	})
}

// PassthroughProvider returns pre-generated content from test case metadata
// without calling any external API. This is useful for evaluating content
// that was generated outside the JudgeJudy pipeline (e.g., A/B testing
// two different systems).
//
// The provider reads from the "generated_output" key in request params
// (which comes from test case metadata) and returns it as the response content.
type PassthroughProvider struct{}

func (p *PassthroughProvider) Name() string { return "passthrough" }

func (p *PassthroughProvider) SupportsModality(_ models.Modality) bool { return true }

func (p *PassthroughProvider) Generate(_ context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	content, ok := req.Params["generated_output"]
	if !ok {
		// Fall back to using the prompt itself as the output
		return &models.GenerateResponse{
			Content:     req.Prompt,
			ContentType: "text/plain",
			ModelUsed:   "passthrough",
		}, nil
	}

	s, ok := content.(string)
	if !ok {
		return nil, fmt.Errorf("passthrough: generated_output must be a string, got %T", content)
	}

	return &models.GenerateResponse{
		Content:     s,
		ContentType: "text/plain",
		ModelUsed:   "passthrough",
	}, nil
}
