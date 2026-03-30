package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/character-ai/judgejudy/pkg/models"
	replicate "github.com/replicate/replicate-go"
)

func init() {
	Register("replicate", func(apiKey string) (Provider, error) {
		return NewReplicateProvider(apiKey)
	})
}

// ReplicateProvider implements Provider using the Replicate API.
type ReplicateProvider struct {
	client *replicate.Client
}

// NewReplicateProvider creates a new Replicate provider.
func NewReplicateProvider(apiKey string) (*ReplicateProvider, error) {
	client, err := replicate.NewClient(replicate.WithToken(apiKey))
	if err != nil {
		return nil, &models.ProviderError{
			Provider: "replicate",
			Message:  "failed to create client",
			Err:      err,
		}
	}
	return &ReplicateProvider{client: client}, nil
}

func (p *ReplicateProvider) Name() string { return "replicate" }

func (p *ReplicateProvider) SupportsModality(m models.Modality) bool {
	switch m {
	case models.ModalityImage, models.ModalityVideo, models.ModalityAudio:
		return true
	default:
		return false
	}
}

func (p *ReplicateProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	switch req.Modality {
	case models.ModalityImage:
		return p.generateSync(ctx, req)
	case models.ModalityVideo, models.ModalityAudio:
		return p.generateAsync(ctx, req)
	default:
		return nil, &models.ProviderError{
			Provider: "replicate",
			Message:  fmt.Sprintf("unsupported modality: %s", req.Modality),
		}
	}
}

func (p *ReplicateProvider) generateSync(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	model := getParam(req.Params, "model", "stability-ai/sdxl")

	input := replicate.PredictionInput{
		"prompt": req.Prompt,
	}
	// Merge additional params (skip framework-level keys)
	for k, v := range req.Params {
		if k == "model" || k == "version" || k == "max_poll_attempts" {
			continue
		}
		input[k] = v
	}

	start := time.Now()
	output, err := p.client.Run(ctx, model, input, nil)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "replicate",
			Message:   "run failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}

	// Output can be a string URL, a list of URLs, or other types
	content, contentType, err := extractReplicateOutput(ctx, output)
	if err != nil {
		return nil, &models.ProviderError{
			Provider: "replicate",
			Message:  "failed to process output",
			Err:      err,
		}
	}

	return &models.GenerateResponse{
		Content:     content,
		ContentType: contentType,
		LatencyMs:   latency,
		ModelUsed:   model,
		Raw:         output,
	}, nil
}

func (p *ReplicateProvider) generateAsync(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	model := getParam(req.Params, "model", "")
	version := getParam(req.Params, "version", "")

	if model == "" && version == "" {
		return nil, &models.ProviderError{
			Provider: "replicate",
			Message:  "model or version must be specified for async generation",
		}
	}

	// For async, if we have a model identifier (owner/name), use Run which handles polling
	if model != "" {
		return p.generateSync(ctx, req)
	}

	input := replicate.PredictionInput{
		"prompt": req.Prompt,
	}
	for k, v := range req.Params {
		if k == "model" || k == "version" || k == "max_poll_attempts" {
			continue
		}
		input[k] = v
	}

	start := time.Now()

	prediction, err := p.client.CreatePrediction(ctx, version, input, nil, false)
	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "replicate",
			Message:   "create prediction failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}

	// Poll with timeout
	pollCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	err = p.client.Wait(pollCtx, prediction)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, &models.ProviderError{
			Provider:  "replicate",
			Message:   "prediction wait failed",
			Retryable: isTransientError(err),
			Err:       err,
		}
	}

	if prediction.Status == replicate.Failed {
		errMsg := "prediction failed"
		if prediction.Error != nil {
			errMsg = fmt.Sprintf("prediction failed: %v", prediction.Error)
		}
		return nil, &models.ProviderError{
			Provider: "replicate",
			Message:  errMsg,
		}
	}

	content, contentType, err := extractReplicateOutput(ctx, prediction.Output)
	if err != nil {
		return nil, &models.ProviderError{
			Provider: "replicate",
			Message:  "failed to process output",
			Err:      err,
		}
	}

	modelUsed := model
	if modelUsed == "" {
		modelUsed = version
	}

	return &models.GenerateResponse{
		Content:     content,
		ContentType: contentType,
		LatencyMs:   latency,
		ModelUsed:   modelUsed,
		Raw:         prediction,
	}, nil
}

// extractReplicateOutput processes Replicate output into base64 content.
func extractReplicateOutput(ctx context.Context, output replicate.PredictionOutput) (string, string, error) {
	switch v := output.(type) {
	case string:
		// Single URL - download and encode
		b64, err := downloadAndEncode(ctx, v)
		if err != nil {
			return "", "", err
		}
		return b64, "application/octet-stream", nil

	case []interface{}:
		// List of URLs - take the first one
		if len(v) == 0 {
			return "", "", fmt.Errorf("empty output list")
		}
		if url, ok := v[0].(string); ok {
			b64, err := downloadAndEncode(ctx, url)
			if err != nil {
				return "", "", err
			}
			return b64, "application/octet-stream", nil
		}
		return fmt.Sprintf("%v", v[0]), "text/plain", nil

	default:
		return fmt.Sprintf("%v", output), "text/plain", nil
	}
}
