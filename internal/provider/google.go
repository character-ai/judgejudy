package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/character-ai/judgejudy/internal/models"
	"google.golang.org/genai"
)

func init() {
	Register("google", func(apiKey string) (Provider, error) {
		return NewGoogleProvider(apiKey)
	})
}

// GoogleProvider implements Provider using the Google Gemini API.
type GoogleProvider struct {
	client *genai.Client
}

// NewGoogleProvider creates a new Google Gemini provider.
func NewGoogleProvider(apiKey string) (*GoogleProvider, error) {
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, &models.ProviderError{
			Provider: "google",
			Message:  "failed to create client",
			Err:      err,
		}
	}
	return &GoogleProvider{client: client}, nil
}

func (p *GoogleProvider) Name() string { return "google" }

func (p *GoogleProvider) SupportsModality(m models.Modality) bool {
	switch m {
	case models.ModalityText, models.ModalityImage, models.ModalityAudio:
		// Text generation, plus image/audio inputs (vision, audio understanding)
		return true
	default:
		return false
	}
}

func (p *GoogleProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	switch req.Modality {
	case models.ModalityText:
		return p.generateText(ctx, req)
	case models.ModalityImage:
		return p.generateImageVision(ctx, req)
	case models.ModalityAudio:
		return p.generateAudioInput(ctx, req)
	default:
		return nil, &models.ProviderError{
			Provider: "google",
			Message:  fmt.Sprintf("unsupported modality: %s", req.Modality),
		}
	}
}

func (p *GoogleProvider) generateText(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	model := getParam(req.Params, "model", "gemini-2.0-flash")
	temperature := float32(getParamFloat(req.Params, "temperature", 0.7))
	maxTokens := int32(getParamInt(req.Params, "max_tokens", 2048))

	parts := []*genai.Part{genai.NewPartFromText(req.Prompt)}

	// Add reference images for vision
	for _, ref := range req.ReferenceInputs {
		data, err := base64.StdEncoding.DecodeString(ref)
		if err != nil {
			return nil, &models.ProviderError{
				Provider: "google",
				Message:  "failed to decode base64 image",
				Err:      err,
			}
		}
		parts = append(parts, genai.NewPartFromBytes(data, "image/png"))
	}

	contents := []*genai.Content{
		genai.NewContentFromParts(parts, "user"),
	}

	config := &genai.GenerateContentConfig{
		Temperature:     &temperature,
		MaxOutputTokens: maxTokens,
	}

	start := time.Now()
	resp, err := p.client.Models.GenerateContent(ctx, model, contents, config)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "google",
			Message:   "content generation failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}

	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, &models.ProviderError{
			Provider: "google",
			Message:  "empty response from model (possibly content filtered)",
		}
	}

	text := resp.Text()

	var inputTokens, outputTokens int
	if resp.UsageMetadata != nil {
		inputTokens = int(resp.UsageMetadata.PromptTokenCount)
		outputTokens = int(resp.UsageMetadata.CandidatesTokenCount)
	}

	return &models.GenerateResponse{
		Content:     text,
		ContentType: "text/plain",
		LatencyMs:   latency,
		CostUSD:     CalculateCost(model, inputTokens, outputTokens),
		TokensUsed:  inputTokens + outputTokens,
		ModelUsed:   model,
		Raw:         resp,
	}, nil
}

func (p *GoogleProvider) generateImageVision(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	// Gemini handles image inputs natively via vision - same as text but with image parts
	return p.generateText(ctx, req)
}

func (p *GoogleProvider) generateAudioInput(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	model := getParam(req.Params, "model", "gemini-2.0-flash")
	temperature := float32(getParamFloat(req.Params, "temperature", 0.7))
	maxTokens := int32(getParamInt(req.Params, "max_tokens", 2048))

	parts := []*genai.Part{genai.NewPartFromText(req.Prompt)}

	// Add reference audio inputs
	for _, ref := range req.ReferenceInputs {
		data, err := base64.StdEncoding.DecodeString(ref)
		if err != nil {
			return nil, &models.ProviderError{
				Provider: "google",
				Message:  "failed to decode base64 audio",
				Err:      err,
			}
		}
		mimeType := getParam(req.Params, "audio_mime_type", "audio/mpeg")
		parts = append(parts, genai.NewPartFromBytes(data, mimeType))
	}

	contents := []*genai.Content{
		genai.NewContentFromParts(parts, "user"),
	}

	config := &genai.GenerateContentConfig{
		Temperature:     &temperature,
		MaxOutputTokens: maxTokens,
	}

	start := time.Now()
	resp, err := p.client.Models.GenerateContent(ctx, model, contents, config)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "google",
			Message:   "audio content generation failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}

	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, &models.ProviderError{
			Provider: "google",
			Message:  "empty response from model (possibly content filtered)",
		}
	}

	text := resp.Text()

	var inputTokens, outputTokens int
	if resp.UsageMetadata != nil {
		inputTokens = int(resp.UsageMetadata.PromptTokenCount)
		outputTokens = int(resp.UsageMetadata.CandidatesTokenCount)
	}

	return &models.GenerateResponse{
		Content:     text,
		ContentType: "text/plain",
		LatencyMs:   latency,
		CostUSD:     CalculateCost(model, inputTokens, outputTokens),
		TokensUsed:  inputTokens + outputTokens,
		ModelUsed:   model,
		Raw:         resp,
	}, nil
}
