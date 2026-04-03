package provider

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/character-ai/judgejudy/pkg/models"
	openai "github.com/sashabaranov/go-openai"
)

const vllmTextTimeout = 120 * time.Second

func init() {
	Register("vllm", func(apiKey string) (Provider, error) {
		return NewVLLMProvider(apiKey)
	})
}

// VLLMProvider implements Provider for vLLM-served models using the
// OpenAI-compatible chat completions API.
type VLLMProvider struct {
	client *openai.Client
}

// NewVLLMProvider creates a new vLLM provider. The base URL must be set via
// JUDGEJUDY_VLLM_URL (e.g. "https://host/vllm/models/model-name/v1").
func NewVLLMProvider(apiKey string) (*VLLMProvider, error) {
	baseURL := os.Getenv("JUDGEJUDY_VLLM_URL")
	if baseURL == "" {
		return nil, &models.ProviderError{
			Provider: "vllm",
			Message:  "JUDGEJUDY_VLLM_URL must be set",
		}
	}

	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL
	client := openai.NewClientWithConfig(cfg)

	return &VLLMProvider{client: client}, nil
}

func (p *VLLMProvider) Name() string { return "vllm" }

func (p *VLLMProvider) SupportsModality(m models.Modality) bool {
	return m == models.ModalityText
}

func (p *VLLMProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	if req.Modality != models.ModalityText {
		return nil, &models.ProviderError{
			Provider: "vllm",
			Message:  fmt.Sprintf("unsupported modality: %s", req.Modality),
		}
	}

	ctx, cancel := context.WithTimeout(ctx, vllmTextTimeout)
	defer cancel()

	model := getParam(req.Params, "model", "")
	temperature := getParamFloat(req.Params, "temperature", 0.7)
	maxTokens := getParamInt(req.Params, "max_tokens", 2048)

	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleUser,
			Content: req.Prompt,
		},
	}

	start := time.Now()
	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Temperature: float32(temperature),
		MaxTokens:   maxTokens,
	})
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "vllm",
			Message:   "chat completion failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}

	if len(resp.Choices) == 0 {
		return nil, &models.ProviderError{
			Provider: "vllm",
			Message:  "no choices returned",
		}
	}

	return &models.GenerateResponse{
		Content:     resp.Choices[0].Message.Content,
		ContentType: "text/plain",
		LatencyMs:   latency,
		CostUSD:     0, // vLLM is self-hosted, no standard cost
		TokensUsed:  resp.Usage.TotalTokens,
		ModelUsed:   model,
		Raw:         resp,
	}, nil
}
