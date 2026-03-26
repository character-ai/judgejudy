package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/character-ai/judgejudy/internal/models"
)

const defaultOllamaHost = "http://localhost:11434"

func init() {
	Register("ollama", func(_ string) (Provider, error) {
		return NewOllamaProvider()
	})
}

// OllamaProvider implements Provider using the Ollama REST API.
type OllamaProvider struct {
	host       string
	httpClient *http.Client
}

// NewOllamaProvider creates a new Ollama provider.
func NewOllamaProvider() (*OllamaProvider, error) {
	host := os.Getenv("JUDGEJUDY_OLLAMA_HOST")
	if host == "" {
		host = defaultOllamaHost
	}
	return &OllamaProvider{
		host: host,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}, nil
}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) SupportsModality(m models.Modality) bool {
	switch m {
	case models.ModalityText, models.ModalityImage:
		// Image modality is vision-only (sending images to the model for analysis)
		return true
	default:
		return false
	}
}

func (p *OllamaProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	switch req.Modality {
	case models.ModalityText, models.ModalityImage:
		return p.generateChat(ctx, req)
	default:
		return nil, &models.ProviderError{
			Provider: "ollama",
			Message:  fmt.Sprintf("unsupported modality: %s", req.Modality),
		}
	}
}

// ollamaChatRequest matches the Ollama /api/chat REST API.
type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Options  map[string]any      `json:"options,omitempty"`
}

type ollamaChatMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

type ollamaChatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done               bool  `json:"done"`
	TotalDuration      int64 `json:"total_duration"`
	PromptEvalCount    int   `json:"prompt_eval_count"`
	EvalCount          int   `json:"eval_count"`
}

func (p *OllamaProvider) generateChat(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	model := getParam(req.Params, "model", "llama3")
	temperature := getParamFloat(req.Params, "temperature", 0.7)

	msg := ollamaChatMessage{
		Role:    "user",
		Content: req.Prompt,
	}

	// Add base64 images for vision
	if len(req.ReferenceInputs) > 0 {
		msg.Images = req.ReferenceInputs
	}

	options := map[string]any{
		"temperature": temperature,
	}
	if maxTokens := getParamInt(req.Params, "max_tokens", 0); maxTokens > 0 {
		options["num_predict"] = maxTokens
	}

	chatReq := ollamaChatRequest{
		Model:    model,
		Messages: []ollamaChatMessage{msg},
		Stream:   false,
		Options:  options,
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, &models.ProviderError{
			Provider: "ollama",
			Message:  "failed to marshal request",
			Err:      err,
		}
	}

	url := p.host + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &models.ProviderError{
			Provider: "ollama",
			Message:  "failed to create request",
			Err:      err,
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := p.httpClient.Do(httpReq)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "ollama",
			Message:   "request failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		return nil, &models.ProviderError{
			Provider:   "ollama",
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error (status %d): %s", resp.StatusCode, string(respBody)),
			Retryable:  resp.StatusCode >= 500,
		}
	}

	var chatResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, &models.ProviderError{
			Provider: "ollama",
			Message:  "failed to decode response",
			Err:      err,
		}
	}

	return &models.GenerateResponse{
		Content:     chatResp.Message.Content,
		ContentType: "text/plain",
		LatencyMs:   latency,
		CostUSD:     0, // Local model, no cost
		TokensUsed:  chatResp.PromptEvalCount + chatResp.EvalCount,
		ModelUsed:   model,
		Raw:         chatResp,
	}, nil
}
