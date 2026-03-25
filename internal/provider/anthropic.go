package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/character-ai/judgejudy/internal/models"
)

func init() {
	Register("anthropic", func(apiKey string) (Provider, error) {
		return NewAnthropicProvider(apiKey)
	})
}

// AnthropicProvider implements Provider using the Anthropic Messages API.
type AnthropicProvider struct {
	client anthropic.Client
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(apiKey string) (*AnthropicProvider, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicProvider{client: client}, nil
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) SupportsModality(m models.Modality) bool {
	// Anthropic supports text generation and image vision (for judging),
	// but does not generate images, audio, or video.
	return m == models.ModalityText
}

func (p *AnthropicProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	switch req.Modality {
	case models.ModalityText:
		return p.generateText(ctx, req)
	default:
		return nil, &models.ProviderError{
			Provider: "anthropic",
			Message:  fmt.Sprintf("unsupported modality: %s", req.Modality),
		}
	}
}

func (p *AnthropicProvider) generateText(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	model := getParam(req.Params, "model", "claude-sonnet-4-6")
	maxTokens := int64(getParamInt(req.Params, "max_tokens", 2048))
	temperature := getParamFloat(req.Params, "temperature", 0.7)

	// Build content blocks
	var blocks []anthropic.ContentBlockParamUnion

	// Add text prompt
	blocks = append(blocks, anthropic.NewTextBlock(req.Prompt))

	// Add reference images for vision (judging)
	for _, ref := range req.ReferenceInputs {
		blocks = append(blocks, anthropic.NewImageBlockBase64(detectMIMEType(ref), ref))
	}

	params := anthropic.MessageNewParams{
		Model:       model,
		MaxTokens:   maxTokens,
		Messages:    []anthropic.MessageParam{anthropic.NewUserMessage(blocks...)},
		Temperature: param.NewOpt(temperature),
	}

	start := time.Now()
	resp, err := p.client.Messages.New(ctx, params)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "anthropic",
			Message:   "message creation failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}

	// Extract text content from response
	var content string
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			content += tb.Text
		}
	}

	inputTokens := int(resp.Usage.InputTokens)
	outputTokens := int(resp.Usage.OutputTokens)

	return &models.GenerateResponse{
		Content:     content,
		ContentType: "text/plain",
		LatencyMs:   latency,
		CostUSD:     CalculateCost(model, inputTokens, outputTokens),
		TokensUsed:  inputTokens + outputTokens,
		ModelUsed:   model,
		Raw:         resp,
	}, nil
}

// detectMIMEType inspects the first bytes of base64-encoded data to determine the image MIME type.
func detectMIMEType(data string) string {
	// Decode enough bytes to check the magic number (first 16 bytes is plenty)
	raw, err := base64.StdEncoding.DecodeString(data[:min(24, len(data))])
	if err != nil || len(raw) < 4 {
		return "image/png"
	}

	switch {
	case len(raw) >= 4 && raw[0] == 0x89 && raw[1] == 'P' && raw[2] == 'N' && raw[3] == 'G':
		return "image/png"
	case len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF:
		return "image/jpeg"
	case len(raw) >= 3 && raw[0] == 'G' && raw[1] == 'I' && raw[2] == 'F':
		return "image/gif"
	case len(raw) >= 12 && raw[0] == 'R' && raw[1] == 'I' && raw[2] == 'F' && raw[3] == 'F' &&
		raw[8] == 'W' && raw[9] == 'E' && raw[10] == 'B' && raw[11] == 'P':
		return "image/webp"
	default:
		return "image/png"
	}
}
