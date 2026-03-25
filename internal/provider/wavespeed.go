package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/character-ai/judgejudy/internal/models"
)

const wavespeedBaseURL = "https://api.wavespeed.ai/api"

func init() {
	Register("wavespeed", func(apiKey string) (Provider, error) {
		return &WaveSpeedProvider{
			apiKey:     apiKey,
			httpClient: &http.Client{Timeout: 30 * time.Second},
		}, nil
	})
}

// WaveSpeedProvider implements Provider using the WaveSpeed API.
type WaveSpeedProvider struct {
	apiKey     string
	httpClient *http.Client
}

func (p *WaveSpeedProvider) Name() string { return "wavespeed" }
func (p *WaveSpeedProvider) SupportsModality(m models.Modality) bool {
	return m == models.ModalityImage || m == models.ModalityVideo
}

func (p *WaveSpeedProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	if req.Modality != models.ModalityImage && req.Modality != models.ModalityVideo {
		return nil, &models.ProviderError{Provider: "wavespeed", Message: fmt.Sprintf("unsupported modality: %s", req.Modality)}
	}

	modelPath := getParam(req.Params, "model_path", getParam(req.Params, "model", ""))
	if modelPath == "" {
		return nil, &models.ProviderError{Provider: "wavespeed", Message: "model_path or model param is required"}
	}

	headers := map[string]string{"Authorization": "Bearer " + p.apiKey}

	payload := map[string]any{"prompt": req.Prompt}
	for _, key := range []string{"image", "last_image", "resolution", "negative_prompt", "seed", "size", "guidance_scale"} {
		if v := getParam(req.Params, key, ""); v != "" {
			payload[key] = v
		}
	}
	if dur := getParamInt(req.Params, "duration", 0); dur > 0 {
		payload["duration"] = dur
	}
	// Support images array for editing models (SD4 Edit, NanoBanana Edit, etc.)
	if images := req.Params["images"]; images != nil {
		payload["images"] = images
	}

	// Step 1: Submit
	var wsResp struct {
		Data struct {
			ID      string   `json:"id"`
			Status  string   `json:"status"`
			Outputs []string `json:"outputs"`
			Error   string   `json:"error"`
		} `json:"data"`
	}

	start := time.Now()
	_, err := doJSON(ctx, p.httpClient, "POST", wavespeedBaseURL+modelPath, headers, payload, &wsResp)
	if err != nil {
		return nil, &models.ProviderError{Provider: "wavespeed", Message: "submit failed", Retryable: isTransientError(err), Err: err}
	}

	// Step 2: Poll for completion
	if wsResp.Data.Status != "completed" && wsResp.Data.Status != "failed" {
		maxAttempts := getParamInt(req.Params, "max_poll_attempts", 300)
		statusURL := fmt.Sprintf("%s/v3/predictions/%s/result", wavespeedBaseURL, wsResp.Data.ID)
		err = pollForCompletion(ctx, p.httpClient, headers, statusURL, 5*time.Second, maxAttempts,
			func(body []byte) (bool, error) {
				if err := json.Unmarshal(body, &wsResp); err != nil {
					return false, nil // retry on decode errors
				}
				if wsResp.Data.Status == "failed" {
					return true, fmt.Errorf("generation failed: %s", wsResp.Data.Error)
				}
				return wsResp.Data.Status == "completed", nil
			},
		)
		if err != nil {
			return nil, &models.ProviderError{Provider: "wavespeed", Message: err.Error()}
		}
	}

	if len(wsResp.Data.Outputs) == 0 {
		return nil, &models.ProviderError{Provider: "wavespeed", Message: "no output URLs in completed response"}
	}

	content, err := downloadAndEncode(ctx, wsResp.Data.Outputs[0])
	if err != nil {
		return nil, &models.ProviderError{Provider: "wavespeed", Message: "download output", Err: err}
	}

	contentType := "video/mp4"
	var costUSD float64
	if req.Modality == models.ModalityImage {
		contentType = "image/png"
		costUSD = CalculateImageCost(modelPath, 1)
	} else {
		dur := float64(getParamInt(req.Params, "duration", 5))
		costUSD = CalculateVideoCost(modelPath, dur)
	}

	return &models.GenerateResponse{
		Content:     content,
		ContentType: contentType,
		LatencyMs:   time.Since(start).Milliseconds(),
		CostUSD:     costUSD,
		ModelUsed:   modelPath,
	}, nil
}
