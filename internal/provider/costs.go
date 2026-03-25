package provider

import "strings"

// ModelCost holds pricing information for a model.
// Costs are per 1K tokens for text models, or per unit for image/audio/video.
type ModelCost struct {
	InputPer1K  float64 // cost per 1K input tokens
	OutputPer1K float64 // cost per 1K output tokens
	PerImage    float64 // cost per image generated
	PerAudioSec float64 // cost per second of audio
	PerVideoSec float64 // cost per second of video
}

// modelCosts maps model names/paths to their cost information.
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
	"claude-opus-4-6":   {InputPer1K: 0.015, OutputPer1K: 0.075},
	"claude-sonnet-4-6": {InputPer1K: 0.003, OutputPer1K: 0.015},
	"claude-haiku-4-5":  {InputPer1K: 0.0008, OutputPer1K: 0.004},

	// Google Gemini models
	"gemini-2.5-pro":   {InputPer1K: 0.00125, OutputPer1K: 0.01},
	"gemini-2.5-flash": {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"gemini-2.0-flash": {InputPer1K: 0.0001, OutputPer1K: 0.0004},

	// OpenAI image models
	"gpt-image-1": {PerImage: 0.04},
	"dall-e-3":    {PerImage: 0.04},

	// OpenAI audio models
	"tts-1":             {PerAudioSec: 0.000015},
	"tts-1-hd":          {PerAudioSec: 0.00003},
	"whisper-1":         {PerAudioSec: 0.0001},
	"gpt-4o-transcribe": {PerAudioSec: 0.0001},

	// ElevenLabs audio models (per-character pricing, ~150 chars/sec estimate)
	"eleven_multilingual_v2": {PerAudioSec: 0.0018},
	"eleven_flash_v2_5":     {PerAudioSec: 0.0009},
	"eleven_v3":             {PerAudioSec: 0.0018},

	// Cartesia audio models
	"sonic-2": {PerAudioSec: 0.001},
	"sonic-3": {PerAudioSec: 0.001},

	// WaveSpeed video models (per-second pricing)
	"seedance-v1":     {PerVideoSec: 0.07},
	"seedance-v1.5":   {PerVideoSec: 0.07},
	"wan-2.5":         {PerVideoSec: 0.05},
	"sora-2":          {PerVideoSec: 0.15},
	"veo3":            {PerVideoSec: 0.15},

	// WaveSpeed image models (per-image pricing)
	"seedream-v3.1":              {PerImage: 0.02},
	"seedream-v4":                {PerImage: 0.03},
	"gemini-2.5-flash-image":    {PerImage: 0.04},
	"nano-banana-pro":           {PerImage: 0.05},
	"flux-kontext-pro":          {PerImage: 0.03},

	// fal.ai Kling3 video
	"kling-video": {PerVideoSec: 0.10},
}

// GetModelCost returns the cost info for a model and whether it was found.
// Handles exact matches first, then substring matches for model paths
// (e.g. "/v3/bytedance/seedance-v1.5-pro/text-to-video" matches "seedance-v1.5").
func GetModelCost(model string) (ModelCost, bool) {
	// Exact match first
	if cost, ok := modelCosts[model]; ok {
		return cost, true
	}
	// Substring match for model paths
	lm := strings.ToLower(model)
	for name, cost := range modelCosts {
		if strings.Contains(lm, strings.ToLower(name)) {
			return cost, true
		}
	}
	return ModelCost{}, false
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

// CalculateVideoCost returns the cost for video generation.
func CalculateVideoCost(model string, durationSec float64) float64 {
	cost, ok := GetModelCost(model)
	if !ok {
		return 0
	}
	return cost.PerVideoSec * durationSec
}
