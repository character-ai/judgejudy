package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/character-ai/judgejudy/pkg/models"
)

func init() {
	Register("cartesia", func(apiKey string) (Provider, error) {
		return &CartesiaProvider{
			apiKey:     apiKey,
			httpClient: &http.Client{Timeout: 60 * time.Second},
		}, nil
	})
}

// CartesiaProvider implements Provider using the Cartesia TTS API.
type CartesiaProvider struct {
	apiKey     string
	httpClient *http.Client
}

func (p *CartesiaProvider) Name() string                          { return "cartesia" }
func (p *CartesiaProvider) SupportsModality(m models.Modality) bool { return m == models.ModalityAudio }

func (p *CartesiaProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	if req.Modality != models.ModalityAudio {
		return nil, &models.ProviderError{Provider: "cartesia", Message: fmt.Sprintf("unsupported modality: %s", req.Modality)}
	}

	model := getParam(req.Params, "model", "sonic-2")
	voiceID := getParam(req.Params, "voice_id", "a0e99841-438c-4a64-b679-ae501e7d6091")

	payload := map[string]any{
		"model_id":   model,
		"transcript": req.Prompt,
		"voice":      map[string]any{"mode": "id", "id": voiceID},
		"output_format": map[string]any{
			"container":   "mp3",
			"bit_rate":    getParamInt(req.Params, "bit_rate", 128000),
			"sample_rate": getParamInt(req.Params, "sample_rate", 44100),
		},
		"language": getParam(req.Params, "language", "en"),
	}

	headers := map[string]string{
		"Authorization":   "Bearer " + p.apiKey,
		"Cartesia-Version": "2025-04-16",
	}

	start := time.Now()
	resp, err := doJSON(ctx, p.httpClient, "POST", "https://api.cartesia.ai/tts/bytes", headers, payload, nil)
	if err != nil {
		return nil, &models.ProviderError{Provider: "cartesia", Message: "request failed", Retryable: isTransientError(err), Err: err}
	}
	defer resp.Body.Close()

	if e := httpError("cartesia", resp); e != nil {
		return nil, e
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &models.ProviderError{Provider: "cartesia", Message: "read response", Err: err}
	}

	durationSec := float64(len(req.Prompt)) / (150.0 * 5.0 / 60.0)

	return &models.GenerateResponse{
		Content:     base64.StdEncoding.EncodeToString(data),
		ContentType: "audio/mpeg",
		LatencyMs:   time.Since(start).Milliseconds(),
		CostUSD:     CalculateAudioCost(model, durationSec),
		ModelUsed:   model,
	}, nil
}
