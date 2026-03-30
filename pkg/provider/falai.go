package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/character-ai/judgejudy/pkg/models"
)

func init() {
	Register("falai", func(apiKey string) (Provider, error) {
		return &FalAIProvider{
			apiKey:     apiKey,
			httpClient: &http.Client{Timeout: 30 * time.Second},
		}, nil
	})
}

// FalAIProvider implements Provider using the fal.ai queue API (Kling3 video).
type FalAIProvider struct {
	apiKey     string
	httpClient *http.Client
}

func (p *FalAIProvider) Name() string                          { return "falai" }
func (p *FalAIProvider) SupportsModality(m models.Modality) bool { return m == models.ModalityVideo }

func (p *FalAIProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	if req.Modality != models.ModalityVideo {
		return nil, &models.ProviderError{Provider: "falai", Message: fmt.Sprintf("unsupported modality: %s", req.Modality)}
	}

	model := getParam(req.Params, "model", "fal-ai/kling-video/v3/pro/image-to-video")
	headers := map[string]string{"Authorization": "Key " + p.apiKey}

	payload := map[string]any{
		"prompt":       req.Prompt,
		"duration":     getParam(req.Params, "duration", "5"),
		"aspect_ratio": getParam(req.Params, "aspect_ratio", "16:9"),
	}
	if imageURL := getParam(req.Params, "start_image_url", ""); imageURL != "" {
		payload["start_image_url"] = imageURL
	}

	// Step 1: Submit to queue
	var queueResp struct {
		StatusURL   string `json:"status_url"`
		ResponseURL string `json:"response_url"`
	}

	start := time.Now()
	submitURL := fmt.Sprintf("https://queue.fal.run/%s", model)
	_, err := doJSON(ctx, p.httpClient, "POST", submitURL, headers, payload, &queueResp)
	if err != nil {
		return nil, &models.ProviderError{Provider: "falai", Message: "submit failed", Retryable: isTransientError(err), Err: err}
	}

	// Step 2: Poll for completion
	var videoURL string
	maxAttempts := getParamInt(req.Params, "max_poll_attempts", 600)
	err = pollForCompletion(ctx, p.httpClient, headers, queueResp.StatusURL, 5*time.Second, maxAttempts,
		func(body []byte) (bool, error) {
			var s struct{ Status string `json:"status"` }
			if err := json.Unmarshal(body, &s); err != nil {
				return false, fmt.Errorf("parse poll response: %w", err)
			}
			if s.Status == "FAILED" {
				return false, fmt.Errorf("fal.ai job failed")
			}
			return s.Status == "COMPLETED", nil
		},
	)
	if err != nil {
		return nil, &models.ProviderError{Provider: "falai", Message: fmt.Sprintf("polling: %v", err)}
	}

	// Step 3: Fetch result
	var result struct {
		Video struct{ URL string `json:"url"` } `json:"video"`
	}
	if _, err := doJSON(ctx, p.httpClient, "GET", queueResp.ResponseURL, headers, nil, &result); err != nil {
		return nil, &models.ProviderError{Provider: "falai", Message: "fetch result", Err: err}
	}
	videoURL = result.Video.URL

	content, err := downloadAndEncode(ctx, videoURL)
	if err != nil {
		return nil, &models.ProviderError{Provider: "falai", Message: "download video", Err: err}
	}

	dur := getParamFloat(req.Params, "duration", 5)
	return &models.GenerateResponse{
		Content:     content,
		ContentType: "video/mp4",
		LatencyMs:   time.Since(start).Milliseconds(),
		CostUSD:     CalculateVideoCost(model, dur),
		ModelUsed:   model,
	}, nil
}
