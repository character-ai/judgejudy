package evaluator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/character-ai/judgejudy/internal/models"
)

// extractVideoFrames decodes a base64 video, extracts N evenly-spaced frames
// using ffmpeg, and returns them as base64 PNG strings.
func extractVideoFrames(b64Video string, numFrames int) []string {
	videoData, err := base64.StdEncoding.DecodeString(b64Video)
	if err != nil {
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "jj_frames_*")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(videoPath, videoData, 0644); err != nil {
		return nil
	}

	// Use ffmpeg to extract N evenly spaced frames, scaled to 512px wide
	outPattern := filepath.Join(tmpDir, "frame_%03d.png")
	// Get video duration first
	probeCmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)
	durationOut, _ := probeCmd.Output()
	duration := strings.TrimSpace(string(durationOut))

	// Calculate fps to get roughly numFrames total frames
	fpsVal := "1"
	if duration != "" {
		var dur float64
		fmt.Sscanf(duration, "%f", &dur)
		if dur > 0 {
			fps := float64(numFrames) / dur
			fpsVal = fmt.Sprintf("%.4f", fps)
		}
	}

	cmd := exec.Command("ffmpeg",
		"-i", videoPath,
		"-vf", fmt.Sprintf("fps=%s,scale=512:-1", fpsVal),
		"-frames:v", fmt.Sprintf("%d", numFrames),
		"-y", outPattern,
	)
	cmd.Run()

	// Read extracted frames
	var frames []string
	for i := 1; i <= numFrames; i++ {
		framePath := filepath.Join(tmpDir, fmt.Sprintf("frame_%03d.png", i))
		data, err := os.ReadFile(framePath)
		if err != nil {
			continue
		}
		frames = append(frames, base64.StdEncoding.EncodeToString(data))
	}
	return frames
}

// extractVideoAudio decodes a base64 video and extracts the audio track as WAV using ffmpeg.
func extractVideoAudio(b64Video string) string {
	videoData, err := base64.StdEncoding.DecodeString(b64Video)
	if err != nil {
		return ""
	}

	tmpDir, err := os.MkdirTemp("", "jj_audio_*")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "input.mp4")
	audioPath := filepath.Join(tmpDir, "audio.wav")
	if err := os.WriteFile(videoPath, videoData, 0644); err != nil {
		return ""
	}

	cmd := exec.Command("ffmpeg",
		"-i", videoPath,
		"-vn", "-acodec", "pcm_s16le",
		"-ar", "16000", "-ac", "1",
		"-y", audioPath,
	)
	if err := cmd.Run(); err != nil {
		return "" // no audio track or ffmpeg error
	}

	data, err := os.ReadFile(audioPath)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

// JudgeEvaluator uses an AI model as a judge to score outputs.
type JudgeEvaluator struct {
	name       string
	mode       models.JudgeMode
	rubric     string
	dimensions []string
	scale      [2]int
	threshold  *float64
	params     map[string]any
	provider   ProviderFunc
	rng        *rand.Rand
}

// judgeResponse is the expected JSON structure returned by the judge model
// for pointwise evaluation.
type judgeResponse struct {
	Scores    map[string]float64 `json:"scores"`
	Overall   float64            `json:"overall"`
	Reasoning string             `json:"reasoning"`
}

// pairwiseResponse is the expected JSON structure for pairwise evaluation.
type pairwiseResponse struct {
	Winner    string `json:"winner"` // "a", "b", or "tie"
	Reasoning string `json:"reasoning"`
}

// NewJudgeEvaluator creates a new AI-as-Judge evaluator.
func NewJudgeEvaluator(cfg models.EvaluatorConfig, provider ProviderFunc) (*JudgeEvaluator, error) {
	if provider == nil {
		return nil, fmt.Errorf("judge evaluator %q: provider function is required", cfg.Name)
	}

	mode := cfg.Mode
	if mode == "" {
		mode = models.JudgeModePointwise
	}

	scale := cfg.Scale
	if scale[1] <= scale[0] {
		scale = [2]int{1, 5}
	}

	return &JudgeEvaluator{
		name:       cfg.Name,
		mode:       mode,
		rubric:     cfg.Rubric,
		dimensions: cfg.Dimensions,
		scale:      scale,
		threshold:  cfg.Threshold,
		params:     cfg.Params,
		provider:   provider,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

func (j *JudgeEvaluator) Name() string           { return j.name }
func (j *JudgeEvaluator) Type() models.EvalType   { return models.EvalTypeAIJudge }

// Evaluate runs the judge on a single test case.
func (j *JudgeEvaluator) Evaluate(ctx context.Context, input models.TestCase, output models.GenerateResponse) (*models.Score, error) {
	numRounds := j.getIntParam("num_rounds", 1)
	if numRounds < 1 {
		numRounds = 1
	}

	switch j.mode {
	case models.JudgeModePointwise:
		return j.evaluatePointwise(ctx, input, output, numRounds)
	case models.JudgeModePairwise:
		return j.evaluatePairwise(ctx, input, output, numRounds)
	default:
		return nil, fmt.Errorf("judge %q: unknown mode %q", j.name, j.mode)
	}
}

func (j *JudgeEvaluator) evaluatePointwise(ctx context.Context, input models.TestCase, output models.GenerateResponse, numRounds int) (*models.Score, error) {
	var totalScore float64
	var lastReasoning string
	// Accumulate per-dimension scores across rounds for averaging.
	dimTotals := make(map[string]float64)

	for round := 0; round < numRounds; round++ {
		prompt := j.buildPointwisePrompt(input, output)

		req := models.GenerateRequest{
			Prompt:   prompt,
			Modality: models.ModalityText,
			Params:   map[string]any{"temperature": 0.0},
		}

		// For audio/image content, pass the generated data as reference inputs
		// so the judge provider can process the actual media.
		// Keep ModalityText since the judge always produces text output.
		// Video is too large to send as base64 — extract sample frames instead.
		if strings.HasPrefix(output.ContentType, "audio") && output.Content != "" {
			req.Modality = models.ModalityAudio
			req.ReferenceInputs = []string{output.Content}
			req.Params["audio_mime_type"] = output.ContentType
		} else if strings.HasPrefix(output.ContentType, "image") && output.Content != "" {
			req.ReferenceInputs = []string{output.Content}
		} else if strings.HasPrefix(output.ContentType, "video") && output.Content != "" {
			// Check if this judge wants to evaluate audio (e.g. "audio-stt-judge")
			wantsAudio := strings.Contains(strings.ToLower(j.name), "audio") ||
				strings.Contains(strings.ToLower(j.name), "stt")
			if wantsAudio {
				audioB64 := extractVideoAudio(output.Content)
				if audioB64 != "" {
					req.Modality = models.ModalityAudio
					req.ReferenceInputs = []string{audioB64}
					req.Params["audio_mime_type"] = "audio/wav"
				}
			} else {
				numFrames := 4
				if nf, ok := j.params["num_sample_frames"]; ok {
					if n, ok := nf.(int); ok {
						numFrames = n
					} else if n, ok := nf.(float64); ok {
						numFrames = int(n)
					}
				}
				frames := extractVideoFrames(output.Content, numFrames)
				if len(frames) > 0 {
					req.ReferenceInputs = frames
				}
			}
		}

		resp, err := j.provider(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("judge %q: provider call failed (round %d): %w", j.name, round+1, err)
		}

		parsed, err := j.parsePointwiseResponse(resp.Content)
		if err != nil {
			return nil, fmt.Errorf("judge %q: parse response (round %d): %w", j.name, round+1, err)
		}

		totalScore += parsed.Overall
		for dim, val := range parsed.Scores {
			dimTotals[dim] += val
		}
		lastReasoning = parsed.Reasoning
	}

	avgScore := totalScore / float64(numRounds)
	normalized := j.normalizeScore(avgScore)

	// Average the per-dimension scores across rounds.
	avgDimScores := make(map[string]float64, len(dimTotals))
	for dim, total := range dimTotals {
		avgDimScores[dim] = total / float64(numRounds)
	}

	score := &models.Score{
		Value:     normalized,
		RawValue:  avgDimScores,
		Reasoning: lastReasoning,
	}

	if j.threshold != nil {
		passed := normalized >= *j.threshold
		score.Passed = &passed
	}

	return score, nil
}

func (j *JudgeEvaluator) evaluatePairwise(ctx context.Context, input models.TestCase, output models.GenerateResponse, numRounds int) (*models.Score, error) {
	// In pairwise mode, the expected output serves as output_b.
	if input.ExpectedOutput == "" {
		return nil, fmt.Errorf("judge %q: pairwise mode requires expected_output as the reference", j.name)
	}

	randomizeOrder := j.getBoolParam("randomize_order", true)

	var totalScore float64
	var lastReasoning string

	for round := 0; round < numRounds; round++ {
		outputA := output.Content
		outputB := input.ExpectedOutput
		swapped := false

		if randomizeOrder && j.rng.Intn(2) == 1 {
			outputA, outputB = outputB, outputA
			swapped = true
		}

		prompt := j.buildPairwisePrompt(input, outputA, outputB)

		resp, err := j.provider(ctx, models.GenerateRequest{
			Prompt:   prompt,
			Modality: models.ModalityText,
			Params:   map[string]any{"temperature": 0.0},
		})
		if err != nil {
			return nil, fmt.Errorf("judge %q: provider call failed (round %d): %w", j.name, round+1, err)
		}

		parsed, err := j.parsePairwiseResponse(resp.Content)
		if err != nil {
			return nil, fmt.Errorf("judge %q: parse pairwise response (round %d): %w", j.name, round+1, err)
		}

		roundScore := j.pairwiseWinnerScore(parsed.Winner, swapped)
		totalScore += roundScore
		lastReasoning = parsed.Reasoning
	}

	avgScore := totalScore / float64(numRounds)

	score := &models.Score{
		Value:     avgScore,
		Reasoning: lastReasoning,
	}

	if j.threshold != nil {
		passed := avgScore >= *j.threshold
		score.Passed = &passed
	}

	return score, nil
}

// pairwiseWinnerScore converts the winner string to a score for output_a (the candidate).
// If swapped is true, the labels are reversed.
func (j *JudgeEvaluator) pairwiseWinnerScore(winner string, swapped bool) float64 {
	switch strings.ToLower(strings.TrimSpace(winner)) {
	case "a":
		if swapped {
			return 0.0 // "a" was actually the reference
		}
		return 1.0
	case "b":
		if swapped {
			return 1.0 // "b" was actually the candidate
		}
		return 0.0
	default: // tie
		return 0.5
	}
}

func (j *JudgeEvaluator) buildPointwisePrompt(input models.TestCase, output models.GenerateResponse) string {
	var b strings.Builder

	b.WriteString("You are an expert evaluator. Score the following AI-generated output.\n\n")

	// Rubric
	if j.rubric != "" {
		b.WriteString("## Rubric\n")
		b.WriteString(j.rubric)
		b.WriteString("\n\n")
	}

	// Dimensions
	if len(j.dimensions) > 0 {
		b.WriteString("## Dimensions to evaluate\n")
		for _, dim := range j.dimensions {
			b.WriteString("- ")
			b.WriteString(dim)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Scale
	b.WriteString(fmt.Sprintf("## Scoring scale\nRate each dimension and provide an overall score from %d to %d.\n\n", j.scale[0], j.scale[1]))

	// Max tokens penalty
	if penalty, ok := j.params["max_tokens_penalty"]; ok {
		b.WriteString(fmt.Sprintf("## Additional constraint\nPenalize outputs that exceed %v tokens. Reduce the score if the output is unnecessarily verbose.\n\n", penalty))
	}

	// Input
	b.WriteString("## User input\n")
	b.WriteString(input.Input)
	b.WriteString("\n\n")

	// Expected output (if any)
	if input.ExpectedOutput != "" {
		b.WriteString("## Expected output (reference)\n")
		b.WriteString(input.ExpectedOutput)
		b.WriteString("\n\n")
	}

	// Generated output — handle by content type
	b.WriteString("## Generated output\n")
	switch {
	case strings.HasPrefix(output.ContentType, "image"):
		b.WriteString("[Image content — the image should be sent as base64 vision content via the provider]\n")
		if output.FilePath != "" {
			b.WriteString(fmt.Sprintf("Image file: %s\n", output.FilePath))
		}
	case strings.HasPrefix(output.ContentType, "audio"):
		b.WriteString("[Audio content is attached as media input for the model to evaluate directly]\n")
	case strings.HasPrefix(output.ContentType, "video"):
		b.WriteString("[Video content — sample frames are attached as images for evaluation]\n")
	default:
		b.WriteString(output.Content)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Response format
	b.WriteString("## Response format\n")
	b.WriteString("Respond with ONLY a JSON object (no markdown fences) in this format:\n")
	b.WriteString(`{"scores": {"dimension_name": <score>, ...}, "overall": <score>, "reasoning": "<brief explanation>"}`)
	b.WriteString("\n")

	return b.String()
}

func (j *JudgeEvaluator) buildPairwisePrompt(input models.TestCase, outputA, outputB string) string {
	var b strings.Builder

	b.WriteString("You are an expert evaluator. Compare the following two AI-generated outputs and decide which is better.\n\n")

	if j.rubric != "" {
		b.WriteString("## Rubric\n")
		b.WriteString(j.rubric)
		b.WriteString("\n\n")
	}

	if len(j.dimensions) > 0 {
		b.WriteString("## Dimensions to evaluate\n")
		for _, dim := range j.dimensions {
			b.WriteString("- ")
			b.WriteString(dim)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if penalty, ok := j.params["max_tokens_penalty"]; ok {
		b.WriteString(fmt.Sprintf("## Additional constraint\nPenalize outputs that exceed %v tokens. Reduce the score if the output is unnecessarily verbose.\n\n", penalty))
	}

	b.WriteString("## User input\n")
	b.WriteString(input.Input)
	b.WriteString("\n\n")

	b.WriteString("## Output A\n")
	b.WriteString(outputA)
	b.WriteString("\n\n")

	b.WriteString("## Output B\n")
	b.WriteString(outputB)
	b.WriteString("\n\n")

	b.WriteString("## Response format\n")
	b.WriteString("Respond with ONLY a JSON object (no markdown fences) in this format:\n")
	b.WriteString(`{"winner": "a" or "b" or "tie", "reasoning": "<brief explanation>"}`)
	b.WriteString("\n")

	return b.String()
}

func (j *JudgeEvaluator) parsePointwiseResponse(content string) (judgeResponse, error) {
	content = cleanJSONResponse(content)

	var resp judgeResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return resp, fmt.Errorf("invalid judge JSON: %w\nraw: %s", err, content)
	}
	return resp, nil
}

func (j *JudgeEvaluator) parsePairwiseResponse(content string) (pairwiseResponse, error) {
	content = cleanJSONResponse(content)

	var resp pairwiseResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return resp, fmt.Errorf("invalid pairwise JSON: %w\nraw: %s", err, content)
	}
	return resp, nil
}

// normalizeScore maps a raw score from [scale[0], scale[1]] to [0.0, 1.0].
func (j *JudgeEvaluator) normalizeScore(raw float64) float64 {
	lo := float64(j.scale[0])
	hi := float64(j.scale[1])
	if hi == lo {
		if raw >= lo {
			return 1.0
		}
		return 0.0
	}
	normalized := (raw - lo) / (hi - lo)
	if normalized < 0.0 {
		return 0.0
	}
	if normalized > 1.0 {
		return 1.0
	}
	return normalized
}

func (j *JudgeEvaluator) getIntParam(key string, defaultVal int) int {
	if j.params == nil {
		return defaultVal
	}
	v, ok := j.params[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	default:
		return defaultVal
	}
}

func (j *JudgeEvaluator) getBoolParam(key string, defaultVal bool) bool {
	if j.params == nil {
		return defaultVal
	}
	v, ok := j.params[key]
	if !ok {
		return defaultVal
	}
	b, ok := v.(bool)
	if !ok {
		return defaultVal
	}
	return b
}

// cleanJSONResponse strips markdown code fences and leading/trailing whitespace.
// It finds the FIRST line starting with ``` and the LAST line starting with ```,
// then extracts the content between them.
func cleanJSONResponse(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")

	firstFence := -1
	lastFence := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if firstFence == -1 {
				firstFence = i
			}
			lastFence = i
		}
	}

	// Only strip if we found a pair of fences (not the same line).
	if firstFence != -1 && lastFence != -1 && firstFence < lastFence {
		lines = lines[firstFence+1 : lastFence]
		s = strings.TrimSpace(strings.Join(lines, "\n"))
	}

	return s
}
