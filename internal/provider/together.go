package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/character-ai/judgejudy/internal/models"
)

const (
	togetherBaseURL    = "https://api.together.xyz/v1"
	togetherMaxRetries = 3
)

func init() {
	Register("together", func(apiKey string) (Provider, error) {
		return NewTogetherProvider(apiKey)
	})
}

// TogetherProvider implements Provider using the Together AI API.
type TogetherProvider struct {
	apiKey     string
	httpClient *http.Client
}

// NewTogetherProvider creates a new Together AI provider.
func NewTogetherProvider(apiKey string) (*TogetherProvider, error) {
	return &TogetherProvider{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}, nil
}

func (p *TogetherProvider) Name() string { return "together" }

func (p *TogetherProvider) SupportsModality(m models.Modality) bool {
	switch m {
	case models.ModalityText, models.ModalityImage:
		return true
	default:
		return false
	}
}

func (p *TogetherProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	switch req.Modality {
	case models.ModalityText:
		return p.generateText(ctx, req)
	case models.ModalityImage:
		return p.generateImage(ctx, req)
	default:
		return nil, &models.ProviderError{
			Provider: "together",
			Message:  fmt.Sprintf("unsupported modality: %s", req.Modality),
		}
	}
}

// togetherChatRequest is the OpenAI-compatible chat request.
type togetherChatRequest struct {
	Model       string                   `json:"model"`
	Messages    []togetherChatMessage    `json:"messages"`
	Temperature float64                  `json:"temperature,omitempty"`
	MaxTokens   int                      `json:"max_tokens,omitempty"`
}

type togetherChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type togetherChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (p *TogetherProvider) generateText(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	model := getParam(req.Params, "model", "meta-llama/Llama-3-70b-chat-hf")
	temperature := getParamFloat(req.Params, "temperature", 0.7)
	maxTokens := getParamInt(req.Params, "max_tokens", 2048)

	chatReq := togetherChatRequest{
		Model: model,
		Messages: []togetherChatMessage{
			{Role: "user", Content: req.Prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, &models.ProviderError{
			Provider: "together",
			Message:  "failed to marshal request",
			Err:      err,
		}
	}

	start := time.Now()
	respBody, err := p.doRequestWithRetry(ctx, "POST", togetherBaseURL+"/chat/completions", body)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, err
	}

	var chatResp togetherChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, &models.ProviderError{
			Provider: "together",
			Message:  "failed to unmarshal response",
			Err:      err,
		}
	}

	if len(chatResp.Choices) == 0 {
		return nil, &models.ProviderError{
			Provider: "together",
			Message:  "no choices returned",
		}
	}

	return &models.GenerateResponse{
		Content:     chatResp.Choices[0].Message.Content,
		ContentType: "text/plain",
		LatencyMs:   latency,
		CostUSD:     CalculateCost(model, chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens),
		TokensUsed:  chatResp.Usage.TotalTokens,
		ModelUsed:   model,
		Raw:         chatResp,
	}, nil
}

type togetherImageRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Steps  int    `json:"steps,omitempty"`
	N      int    `json:"n,omitempty"`
}

type togetherImageResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
		URL     string `json:"url"`
	} `json:"data"`
}

func (p *TogetherProvider) generateImage(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	model := getParam(req.Params, "model", "stabilityai/stable-diffusion-xl-base-1.0")
	width := getParamInt(req.Params, "width", 1024)
	height := getParamInt(req.Params, "height", 1024)
	steps := getParamInt(req.Params, "steps", 20)

	imageReq := togetherImageRequest{
		Model:  model,
		Prompt: req.Prompt,
		Width:  width,
		Height: height,
		Steps:  steps,
		N:      1,
	}

	body, err := json.Marshal(imageReq)
	if err != nil {
		return nil, &models.ProviderError{
			Provider: "together",
			Message:  "failed to marshal image request",
			Err:      err,
		}
	}

	start := time.Now()
	respBody, err := p.doRequestWithRetry(ctx, "POST", togetherBaseURL+"/images/generations", body)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, err
	}

	var imageResp togetherImageResponse
	if err := json.Unmarshal(respBody, &imageResp); err != nil {
		return nil, &models.ProviderError{
			Provider: "together",
			Message:  "failed to unmarshal image response",
			Err:      err,
		}
	}

	if len(imageResp.Data) == 0 {
		return nil, &models.ProviderError{
			Provider: "together",
			Message:  "no image data returned",
		}
	}

	content := imageResp.Data[0].B64JSON
	if content == "" && imageResp.Data[0].URL != "" {
		content, err = downloadAndEncode(ctx, imageResp.Data[0].URL)
		if err != nil {
			return nil, &models.ProviderError{
				Provider: "together",
				Message:  "failed to download image",
				Err:      err,
			}
		}
	}

	return &models.GenerateResponse{
		Content:     content,
		ContentType: "image/png",
		LatencyMs:   latency,
		ModelUsed:   model,
		Raw:         imageResp,
	}, nil
}

// doRequestWithRetry performs an HTTP request with exponential backoff on 429 errors.
func (p *TogetherProvider) doRequestWithRetry(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	backoff := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	for attempt := 0; attempt < togetherMaxRetries; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, &models.ProviderError{
				Provider: "together",
				Message:  "failed to create request",
				Err:      err,
			}
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			// Retry transient network errors (timeout, connection reset, etc.)
			if isTransientError(err) && attempt < togetherMaxRetries-1 {
				select {
				case <-ctx.Done():
					return nil, &models.ProviderError{
						Provider: "together",
						Message:  "context cancelled during retry backoff",
						Err:      ctx.Err(),
					}
				case <-time.After(backoff[attempt]):
					continue
				}
			}
			return nil, &models.ProviderError{
				Provider:  "together",
				Message:   "request failed",
				Retryable: isTransientError(err),
				Err:       err,
			}
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, &models.ProviderError{
				Provider: "together",
				Message:  "failed to read response body",
				Err:      err,
			}
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < togetherMaxRetries-1 {
			select {
			case <-ctx.Done():
				return nil, &models.ProviderError{
					Provider: "together",
					Message:  "context cancelled during retry backoff",
					Err:      ctx.Err(),
				}
			case <-time.After(backoff[attempt]):
				continue
			}
		}

		if resp.StatusCode >= 400 {
			return nil, &models.ProviderError{
				Provider:   "together",
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("API error (status %d): %s", resp.StatusCode, string(respBody)),
				Retryable:  resp.StatusCode >= 500,
			}
		}

		return respBody, nil
	}

	return nil, &models.ProviderError{
		Provider:  "together",
		Message:   "max retries exceeded",
		Retryable: true,
	}
}
