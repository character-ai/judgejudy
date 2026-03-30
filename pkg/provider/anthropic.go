package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/character-ai/judgejudy/pkg/models"
)

func init() {
	Register("anthropic", func(apiKey string) (Provider, error) {
		return NewAnthropicProvider(apiKey)
	})
}

// AnthropicProvider implements Provider using the Anthropic Messages API.
type AnthropicProvider struct {
	client anthropic.Client
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(apiKey string) (*AnthropicProvider, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicProvider{client: client}, nil
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) SupportsModality(m models.Modality) bool {
	// Anthropic supports text generation and image vision (for judging),
	// but does not generate images, audio, or video.
	return m == models.ModalityText
}

func (p *AnthropicProvider) Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	switch req.Modality {
	case models.ModalityText:
		return p.generateText(ctx, req)
	default:
		return nil, &models.ProviderError{
			Provider: "anthropic",
			Message:  fmt.Sprintf("unsupported modality: %s", req.Modality),
		}
	}
}

func (p *AnthropicProvider) generateText(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error) {
	model := getParam(req.Params, "model", "claude-sonnet-4-6")
	maxTokens := int64(getParamInt(req.Params, "max_tokens", 2048))
	temperature := getParamFloat(req.Params, "temperature", 0.7)

	// Build content blocks
	var blocks []anthropic.ContentBlockParamUnion

	// Add text prompt
	blocks = append(blocks, anthropic.NewTextBlock(req.Prompt))

	// Add reference images for vision (judging)
	for _, ref := range req.ReferenceInputs {
		blocks = append(blocks, anthropic.NewImageBlockBase64(detectMIMEType(ref), ref))
	}

	params := anthropic.MessageNewParams{
		Model:       model,
		MaxTokens:   maxTokens,
		Messages:    []anthropic.MessageParam{anthropic.NewUserMessage(blocks...)},
		Temperature: param.NewOpt(temperature),
	}

	// Add tools if configured
	var toolName string
	if toolsJSON, ok := req.Params["tools"]; ok {
		if toolsStr, ok := toolsJSON.(string); ok && toolsStr != "" {
			var toolDefs []struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				InputSchema map[string]any `json:"input_schema"`
			}
			if err := json.Unmarshal([]byte(toolsStr), &toolDefs); err == nil {
				for _, td := range toolDefs {
					toolName = td.Name
					params.Tools = append(params.Tools, anthropic.ToolUnionParam{
						OfTool: &anthropic.ToolParam{
							Name:        td.Name,
							Description: anthropic.String(td.Description),
							InputSchema: anthropic.ToolInputSchemaParam{
								Properties: td.InputSchema["properties"],
								ExtraFields: map[string]interface{}{
									"required": td.InputSchema["required"],
								},
							},
						},
					})
				}
			}
		}
	}

	// Use streaming for large max_tokens requests (required by Opus for long operations)
	useStreaming := maxTokens > 16384

	start := time.Now()
	var content string
	var inputTokens, outputTokens int
	var modelUsed string

	// Helper to extract content from message blocks (handles both text and tool_use)
	extractContent := func(blocks []anthropic.ContentBlockUnion) string {
		var result string
		for _, block := range blocks {
			switch v := block.AsAny().(type) {
			case anthropic.ToolUseBlock:
				if toolName != "" && v.Name == toolName {
					result = string(v.Input)
				}
			case anthropic.TextBlock:
				if toolName == "" { // Only use text if no tool expected
					result += v.Text
				}
			}
		}
		return result
	}

	if useStreaming {
		stream := p.client.Messages.NewStreaming(ctx, params)
		defer stream.Close()

		var message anthropic.Message
		for stream.Next() {
			event := stream.Current()
			if err := message.Accumulate(event); err != nil {
				return nil, &models.ProviderError{
					Provider:  "anthropic",
					Message:   "streaming accumulate failed",
					Retryable: false,
					Err:       err,
				}
			}
		}
		if err := stream.Err(); err != nil {
			return nil, &models.ProviderError{
				Provider:  "anthropic",
				Message:   "streaming message creation failed",
				Retryable: isTransientError(err),
				Err:       err,
			}
		}

		content = extractContent(message.Content)
		inputTokens = int(message.Usage.InputTokens)
		outputTokens = int(message.Usage.OutputTokens)
		modelUsed = string(message.Model)
	} else {
		resp, err := p.client.Messages.New(ctx, params)
		if err != nil {
			return nil, &models.ProviderError{
				Provider:  "anthropic",
				Message:   "message creation failed",
				Retryable: isTransientError(err),
				Err:       err,
			}
		}

		content = extractContent(resp.Content)
		inputTokens = int(resp.Usage.InputTokens)
		outputTokens = int(resp.Usage.OutputTokens)
		modelUsed = string(resp.Model)
	}

	latency := time.Since(start).Milliseconds()
	if modelUsed == "" {
		modelUsed = model
	}

	return &models.GenerateResponse{
		Content:     content,
		ContentType: "text/plain",
		LatencyMs:   latency,
		CostUSD:     CalculateCost(model, inputTokens, outputTokens),
		TokensUsed:  inputTokens + outputTokens,
		ModelUsed:   modelUsed,
		Raw:         nil,
	}, nil
}

// detectMIMEType inspects the first bytes of base64-encoded data to determine the image MIME type.
func detectMIMEType(data string) string {
	// Decode enough bytes to check the magic number (first 16 bytes is plenty)
	raw, err := base64.StdEncoding.DecodeString(data[:min(24, len(data))])
	if err != nil || len(raw) < 4 {
		return "image/png"
	}

	switch {
	case len(raw) >= 4 && raw[0] == 0x89 && raw[1] == 'P' && raw[2] == 'N' && raw[3] == 'G':
		return "image/png"
	case len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF:
		return "image/jpeg"
	case len(raw) >= 3 && raw[0] == 'G' && raw[1] == 'I' && raw[2] == 'F':
		return "image/gif"
	case len(raw) >= 12 && raw[0] == 'R' && raw[1] == 'I' && raw[2] == 'F' && raw[3] == 'F' &&
		raw[8] == 'W' && raw[9] == 'E' && raw[10] == 'B' && raw[11] == 'P':
		return "image/webp"
	default:
		return "image/png"
	}
}
