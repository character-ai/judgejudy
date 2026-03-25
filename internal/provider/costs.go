package provider

// ModelCost holds pricing information for a model.
// Costs are per 1K tokens for text models, or per unit for image/audio.
type ModelCost struct {
	InputPer1K  float64 // cost per 1K input tokens
	OutputPer1K float64 // cost per 1K output tokens
	PerImage    float64 // cost per image generated
	PerAudioSec float64 // cost per second of audio
}

// modelCosts maps model names to their cost information.
var modelCosts = map[string]ModelCost{
	// OpenAI text models
	"gpt-4.1":      {InputPer1K: 0.002, OutputPer1K: 0.008},
	"gpt-4.1-mini": {InputPer1K: 0.0004, OutputPer1K: 0.0016},
	"gpt-4.1-nano": {InputPer1K: 0.0001, OutputPer1K: 0.0004},
	"gpt-4o":       {InputPer1K: 0.0025, OutputPer1K: 0.01},
	"gpt-4o-mini":  {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"o3":           {InputPer1K: 0.01, OutputPer1K: 0.04},
	"o3-mini":      {InputPer1K: 0.00115, OutputPer1K: 0.0044},
	"o4-mini":      {InputPer1K: 0.00115, OutputPer1K: 0.0044},

	// Anthropic models
	"claude-opus-4-6":          {InputPer1K: 0.015, OutputPer1K: 0.075},
	"claude-sonnet-4-6":        {InputPer1K: 0.003, OutputPer1K: 0.015},
	"claude-haiku-4-5":         {InputPer1K: 0.0008, OutputPer1K: 0.004},

	// Google Gemini models
	"gemini-2.5-pro":   {InputPer1K: 0.00125, OutputPer1K: 0.01},
	"gemini-2.5-flash": {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"gemini-2.0-flash": {InputPer1K: 0.0001, OutputPer1K: 0.0004},

	// OpenAI image models
	"gpt-image-1": {PerImage: 0.04},
	"dall-e-3":    {PerImage: 0.04},

	// OpenAI audio models
	"tts-1":    {PerAudioSec: 0.000015},
	"tts-1-hd": {PerAudioSec: 0.00003},
	"whisper-1": {PerAudioSec: 0.0001},
	"gpt-4o-transcribe": {PerAudioSec: 0.0001},
}

// GetModelCost returns the cost info for a model and whether it was found.
func GetModelCost(model string) (ModelCost, bool) {
	cost, ok := modelCosts[model]
	return cost, ok
}

// CalculateCost returns the estimated cost for a generation.
func CalculateCost(model string, inputTokens, outputTokens int) float64 {
	cost, ok := GetModelCost(model)
	if !ok {
		return 0
	}
	return (float64(inputTokens)/1000)*cost.InputPer1K +
		(float64(outputTokens)/1000)*cost.OutputPer1K
}

// CalculateImageCost returns the cost for image generation.
func CalculateImageCost(model string, count int) float64 {
	cost, ok := GetModelCost(model)
	if !ok {
		return 0
	}
	return cost.PerImage * float64(count)
}

// CalculateAudioCost returns the cost for audio processing.
func CalculateAudioCost(model string, durationSec float64) float64 {
	cost, ok := GetModelCost(model)
	if !ok {
		return 0
	}
	return cost.PerAudioSec * durationSec
}
