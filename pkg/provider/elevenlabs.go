package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/character-ai/judgejudy/pkg/models"
)

func init() {
	Register("elevenlabs", func(apiKey string) (Provider, error) {
		return &ElevenLabsProvider{
			apiKey:     apiKey,
			httpClient: &http.Client{Timeout: 60 * time.Second},
		}, nil
	})
}

// ElevenLabsProvider implements Provider using the ElevenLabs TTS API.
type ElevenLabsProvider struct {
	apiKey     string
	httpClient *http.Client
}

func (p *ElevenLabsProvider) Name() string                          { return "elevenlabs" }
func (p *ElevenLabsProvider) SupportsModality(m models.Modality) bool { return m == models.ModalityAudio }

func (p *ElevenLabsProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	if req.Modality != models.ModalityAudio {
		return nil, &models.ProviderError{Provider: "elevenlabs", Message: fmt.Sprintf("unsupported modality: %s", req.Modality)}
	}

	voiceID := getParam(req.Params, "voice_id", "21m00Tcm4TlvDq8ikWAM")
	model := getParam(req.Params, "model", "eleven_multilingual_v2")

	payload := map[string]any{
		"text":     req.Prompt,
		"model_id": model,
		"voice_settings": map[string]any{
			"stability":        getParamFloat(req.Params, "stability", 0.5),
			"similarity_boost": getParamFloat(req.Params, "similarity_boost", 0.75),
		},
	}

	headers := map[string]string{
		"xi-api-key": p.apiKey,
		"Accept":     "audio/mpeg",
	}

	apiURL := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", url.PathEscape(voiceID))

	start := time.Now()
	resp, err := doJSON(ctx, p.httpClient, "POST", apiURL, headers, payload, nil)
	if err != nil {
		return nil, &models.ProviderError{Provider: "elevenlabs", Message: "request failed", Retryable: isTransientError(err), Err: err}
	}
	defer resp.Body.Close()

	if e := httpError("elevenlabs", resp); e != nil {
		return nil, e
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &models.ProviderError{Provider: "elevenlabs", Message: "read response", Err: err}
	}

	// Estimate duration: ~150 words/min, ~5 chars/word
	durationSec := float64(len(req.Prompt)) / (150.0 * 5.0 / 60.0)

	return &models.GenerateResponse{
		Content:     base64.StdEncoding.EncodeToString(data),
		ContentType: "audio/mpeg",
		LatencyMs:   time.Since(start).Milliseconds(),
		CostUSD:     CalculateAudioCost(model, durationSec),
		ModelUsed:   model,
	}, nil
}
