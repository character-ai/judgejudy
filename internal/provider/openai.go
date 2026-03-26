package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/character-ai/judgejudy/internal/models"
	openai "github.com/sashabaranov/go-openai"
)

const (
	openaiTextTimeout  = 30 * time.Second
	openaiMediaTimeout = 120 * time.Second
)

func init() {
	Register("openai", func(apiKey string) (Provider, error) {
		return NewOpenAIProvider(apiKey)
	})
}

// OpenAIProvider implements Provider using the OpenAI API.
type OpenAIProvider struct {
	client *openai.Client
}

// NewOpenAIProvider creates a new OpenAI provider.
func NewOpenAIProvider(apiKey string) (*OpenAIProvider, error) {
	client := openai.NewClient(apiKey)
	return &OpenAIProvider{
		client: client,
	}, nil
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) SupportsModality(m models.Modality) bool {
	switch m {
	case models.ModalityText, models.ModalityImage, models.ModalityAudio:
		return true
	default:
		return false
	}
}

func (p *OpenAIProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	switch req.Modality {
	case models.ModalityText:
		return p.generateText(ctx, req)
	case models.ModalityImage:
		return p.generateImage(ctx, req)
	case models.ModalityAudio:
		return p.generateAudio(ctx, req)
	default:
		return nil, &models.ProviderError{
			Provider: "openai",
			Message:  fmt.Sprintf("unsupported modality: %s", req.Modality),
		}
	}
}

func (p *OpenAIProvider) generateText(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, openaiTextTimeout)
	defer cancel()

	model := getParam(req.Params, "model", "gpt-4o")
	temperature := getParamFloat(req.Params, "temperature", 0.7)
	maxTokens := getParamInt(req.Params, "max_tokens", 2048)

	messages := []openai.ChatCompletionMessage{}

	// If there are reference inputs (images for vision), build multi-content messages
	if len(req.ReferenceInputs) > 0 {
		parts := []openai.ChatMessagePart{
			{
				Type: openai.ChatMessagePartTypeText,
				Text: req.Prompt,
			},
		}
		for _, ref := range req.ReferenceInputs {
			mimeType := detectMIMEType(ref)
			parts = append(parts, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{
					URL:    "data:" + mimeType + ";base64," + ref,
					Detail: openai.ImageURLDetailAuto,
				},
			})
		}
		messages = append(messages, openai.ChatCompletionMessage{
			Role:         openai.ChatMessageRoleUser,
			MultiContent: parts,
		})
	} else {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: req.Prompt,
		})
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
			Provider:  "openai",
			Message:   "chat completion failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}

	if len(resp.Choices) == 0 {
		return nil, &models.ProviderError{
			Provider: "openai",
			Message:  "no choices returned",
		}
	}

	inputTokens := resp.Usage.PromptTokens
	outputTokens := resp.Usage.CompletionTokens

	return &models.GenerateResponse{
		Content:     resp.Choices[0].Message.Content,
		ContentType: "text/plain",
		LatencyMs:   latency,
		CostUSD:     CalculateCost(model, inputTokens, outputTokens),
		TokensUsed:  resp.Usage.TotalTokens,
		ModelUsed:   model,
		Raw:         resp,
	}, nil
}

func (p *OpenAIProvider) generateImage(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, openaiMediaTimeout)
	defer cancel()

	model := getParam(req.Params, "model", "dall-e-3")
	size := getParam(req.Params, "size", "1024x1024")
	quality := getParam(req.Params, "quality", "standard")

	start := time.Now()
	resp, err := p.client.CreateImage(ctx, openai.ImageRequest{
		Prompt:  req.Prompt,
		Model:   model,
		N:       1,
		Size:    size,
		Quality: quality,
	})
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "openai",
			Message:   "image generation failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}

	if len(resp.Data) == 0 {
		return nil, &models.ProviderError{
			Provider: "openai",
			Message:  "no image data returned",
		}
	}

	// Download the image URL and base64 encode it
	imageURL := resp.Data[0].URL
	b64, err := downloadAndEncode(ctx, imageURL)
	if err != nil {
		return nil, &models.ProviderError{
			Provider: "openai",
			Message:  "failed to download generated image",
			Err:      err,
		}
	}

	return &models.GenerateResponse{
		Content:     b64,
		ContentType: "image/png",
		LatencyMs:   latency,
		CostUSD:     CalculateImageCost(model, 1),
		ModelUsed:   model,
		Raw:         resp,
	}, nil
}

func (p *OpenAIProvider) generateAudio(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, openaiMediaTimeout)
	defer cancel()

	model := getParam(req.Params, "model", "tts-1")

	// Check if this is a transcription request
	if getParam(req.Params, "task", "") == "transcribe" {
		return p.transcribeAudio(ctx, req)
	}

	voice := openai.VoiceAlloy
	if v := getParam(req.Params, "voice", ""); v != "" {
		voice = openai.SpeechVoice(v)
	}

	start := time.Now()
	resp, err := p.client.CreateSpeech(ctx, openai.CreateSpeechRequest{
		Model: openai.SpeechModel(model),
		Input: req.Prompt,
		Voice: voice,
	})
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "openai",
			Message:   "speech generation failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}
	defer resp.Close()

	data, err := io.ReadAll(io.LimitReader(resp, maxResponseBytes))
	if err != nil {
		return nil, &models.ProviderError{
			Provider: "openai",
			Message:  "failed to read speech response",
			Err:      err,
		}
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	// Rough estimate: ~150 words/min at average speech rate, ~5 chars/word
	durationSec := float64(len(req.Prompt)) / (150.0 * 5.0 / 60.0)

	return &models.GenerateResponse{
		Content:     b64,
		ContentType: "audio/mpeg",
		LatencyMs:   latency,
		CostUSD:     CalculateAudioCost(model, durationSec),
		ModelUsed:   model,
		Raw:         nil,
	}, nil
}

func (p *OpenAIProvider) transcribeAudio(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	model := getParam(req.Params, "model", "whisper-1")
	filePath := getParam(req.Params, "file_path", "")

	if filePath == "" && len(req.ReferenceInputs) == 0 {
		return nil, &models.ProviderError{
			Provider: "openai",
			Message:  "transcription requires file_path param or reference_inputs",
		}
	}

	// If filePath is empty but we have ReferenceInputs, decode the first one to a temp file
	if filePath == "" && len(req.ReferenceInputs) > 0 {
		audioData, decErr := base64.StdEncoding.DecodeString(req.ReferenceInputs[0])
		if decErr != nil {
			return nil, &models.ProviderError{
				Provider: "openai",
				Message:  "failed to decode base64 audio from reference_inputs",
				Err:      decErr,
			}
		}
		tmpFile, tmpErr := os.CreateTemp("", "judgejudy-audio-*.wav")
		if tmpErr != nil {
			return nil, &models.ProviderError{
				Provider: "openai",
				Message:  "failed to create temp file for audio",
				Err:      tmpErr,
			}
		}
		defer os.Remove(tmpFile.Name())
		if _, writeErr := tmpFile.Write(audioData); writeErr != nil {
			tmpFile.Close()
			return nil, &models.ProviderError{
				Provider: "openai",
				Message:  "failed to write temp audio file",
				Err:      writeErr,
			}
		}
		tmpFile.Close()
		filePath = tmpFile.Name()
	}

	start := time.Now()
	resp, err := p.client.CreateTranscription(ctx, openai.AudioRequest{
		Model:    model,
		FilePath: filePath,
	})
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "openai",
			Message:   "transcription failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}

	return &models.GenerateResponse{
		Content:     resp.Text,
		ContentType: "text/plain",
		LatencyMs:   latency,
		CostUSD:     0, // Duration not easily available from the API response
		ModelUsed:   model,
		Raw:         resp,
	}, nil
}

// Shared helpers (getParam*, isTransientError, downloadAndEncode) are in helpers.go
