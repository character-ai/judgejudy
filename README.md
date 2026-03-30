# JudgeJudy

Multimodal AI evaluation framework. Judge text, image, audio, and video generation using AI judges, automated metrics, and human evaluation — with any model, any provider.

## Why JudgeJudy?

Evaluating AI-generated content is hard. Different modalities need different metrics. AI judges are fast but can be miscalibrated. Human evaluation is accurate but slow. JudgeJudy combines all three into a single pipeline:

1. **Generate** outputs from any provider (OpenAI, Anthropic, Gemini, ElevenLabs, Cartesia, WaveSpeed, fal.ai, Replicate, Together, Ollama) — or evaluate pre-generated content
2. **Evaluate** with AI judges (custom rubrics), automated metrics (BERTScore, CLIP, PESQ, etc.), and human scoring
3. **Calibrate** AI judges against human feedback and get rubric improvement suggestions
4. **Compare** runs over time with baseline tracking and regression detection

## Install

```bash
# Build from source
git clone https://github.com/character-ai/judgejudy.git
cd judgejudy
make build

# Or install to $GOPATH/bin
make install

# Install Python dependencies (for automated metrics)
pip install -r python/requirements.txt
```

## Quick Start

### 1. Set API Keys

```bash
cp .env.example .env
# Edit .env with your API keys — only set the providers you use
```

### 2. Run an Evaluation

```bash
# Text generation quality
judgejudy run examples/text_eval.yaml --report report.html

# Image generation
judgejudy run examples/image_eval.yaml --report report.html

# Audio TTS quality
judgejudy run examples/audio_eval.yaml --report report.html

# Video generation with realism + audio checks
judgejudy run examples/video_eval.yaml --report report.html

# Export structured results as JSON (for programmatic consumers)
judgejudy run examples/text_eval.yaml --json-output results.json
```

### Go Library

JudgeJudy can be used as a Go library for programmatic integration:

```go
import (
    "github.com/character-ai/judgejudy/pkg/judgejudy"
    "github.com/character-ai/judgejudy/pkg/config"
)

// Load config and run evaluation
cfg, _ := config.LoadConfig("eval.yaml")
result, _ := judgejudy.Run(ctx, cfg, judgejudy.Options{
    Store: myCustomStore,  // optional — defaults to SQLite
})

// Access results
fmt.Println(result.Run.Aggregate.TotalPassRate)
for _, tr := range result.Run.Results {
    fmt.Println(tr.TestCaseID, tr.Scores)
}

// Generate HTML report
judgejudy.GenerateReport(result, "report.html")
```

The `pkg/` packages provide the full public API:

| Package | Description |
|---------|-------------|
| `pkg/judgejudy` | Top-level entry point — `Run()` and `GenerateReport()` |
| `pkg/config` | Config loading and validation |
| `pkg/models` | All domain types (Run, TestCase, Score, etc.) |
| `pkg/store` | Store interface for custom persistence backends |
| `pkg/pipeline` | Pipeline construction and execution |
| `pkg/evaluator` | Evaluator interface and built-in evaluators |
| `pkg/provider` | Provider interface and built-in providers |

**Custom Store**: Implement `store.Store` to use your own backend (Spanner, Postgres, DynamoDB, etc.):

```go
type Store interface {
    SaveRun(ctx context.Context, run *models.Run) error
    GetRun(ctx context.Context, id string) (*models.Run, error)
    ListRuns(ctx context.Context, opts ListOpts) ([]models.Run, error)
    GetBaseline(ctx context.Context, datasetID string) (*models.Run, error)
    SetBaseline(ctx context.Context, runID string) error
    SaveComparison(ctx context.Context, comp *models.Comparison) error
    SaveHumanEvaluations(ctx context.Context, evals []models.HumanEvaluation) error
    GetHumanEvaluations(ctx context.Context, runID string) ([]models.HumanEvaluation, error)
    Close() error
}
```

### 3. Score with Human Evaluation

Open the HTML report in a browser. Each test case has interactive 1-5 scoring buttons. Your scores persist across page reloads. Click **Export Human Eval** to download a JSON file.

### 4. Import and Calibrate

```bash
# Import human scores
judgejudy human-eval import <run-id> human_eval_<run-id>.json

# Calibrate AI judges against your scores + get rubric suggestions
judgejudy calibrate <run-id>
```

The calibrate command computes correlation, bias, and agreement between AI and human scores, then uses Claude to suggest specific rubric improvements based on where they diverged.

## Evaluating Pre-Generated Content

Use the `passthrough` provider to evaluate content generated outside JudgeJudy — for example, A/B testing two different generation systems or comparing model versions.

The passthrough provider reads from `metadata.generated_output` in each test case instead of calling an API. Pair it with pairwise judges for blind A/B comparison:

```yaml
dataset:
  inline:
    id: "my-ab-test"
    modality: text
    test_cases:
      - id: "tc-1"
        input: "The prompt or context for this test case"
        expected_output: |
          Baseline output (system A / current version)
        metadata:
          generated_output: |
            Candidate output (system B / new version)

generator:
  provider: passthrough
  model: passthrough

evaluators:
  - name: "pairwise-quality"
    type: ai_judge
    provider: anthropic
    model: claude-sonnet-4-6
    mode: pairwise
    rubric: |
      Compare both outputs for quality. Which is better?
    dimensions: [overall_quality]
    scale: [1, 10]
    params:
      num_rounds: 3          # multiple rounds for consistency
      randomize_order: true   # blind: randomize A/B position
```

How it works:
- `metadata.generated_output` → fed through the passthrough provider as the candidate output
- `expected_output` → used as the reference in pairwise comparison
- `input` → shown to judges as context (the original prompt/scenario)
- Pairwise scores: **1.0** = candidate wins, **0.5** = tie, **0.0** = baseline wins

No API keys are needed for the passthrough provider. See `examples/pregenerated_eval.yaml` for a complete example.

## Features

### Generation Providers

| Provider | Text | Image | Audio | Video |
|----------|------|-------|-------|-------|
| OpenAI | GPT-4o, GPT-4.1 | DALL-E 3, gpt-image-1 | TTS-1, TTS-1-HD | - |
| Anthropic | Claude Opus/Sonnet/Haiku (streaming, tool_use) | - | - | - |
| Google Gemini | Gemini 2.5 Pro/Flash | - | - | - |
| ElevenLabs | - | - | Eleven v3, Multilingual v2 | - |
| Cartesia | - | - | Sonic 2, Sonic 3 | - |
| WaveSpeed | - | Seedream | - | Seedance, WAN, VEO3, Sora-2 |
| fal.ai | - | - | - | Kling3 |
| Replicate | - | Various | Various | Various |
| Together AI | Llama, Mistral | Various | - | - |
| Ollama | Local models | - | - | - |
| Passthrough | Pre-generated content | Pre-generated content | Pre-generated content | Pre-generated content |

### Evaluator Types

**AI Judge** — Use any LLM to score outputs against a custom rubric with named dimensions (accuracy, clarity, realism, etc.). Supports pointwise scoring and pairwise comparison with bias mitigation.

**Automated Metrics** — Reference-based and reference-free metrics via Python:

| Metric | Modality | Reference needed? | What it measures |
|--------|----------|-------------------|-----------------|
| `bertscore` | text | yes | Semantic similarity (contextual embeddings) |
| `rouge` | text | yes | N-gram overlap recall |
| `bleu` | text | yes | N-gram precision |
| `clip_score` | image | no (uses prompt) | Image-text alignment |
| `fid` | image | yes (directory) | Distribution-level quality |
| `lpips` | image | yes | Perceptual similarity |
| `ssim` | image | yes | Structural similarity |
| `pesq` | audio | yes | Speech quality (ITU standard) |
| `stoi` | audio | yes | Speech intelligibility |
| `utmos` | audio | no | Neural MOS prediction |
| `temporal_consistency` | video | no | Frame-to-frame SSIM |
| `clip_temporal` | video | no | Semantic consistency across frames |

**Composite** — Weighted combination of multiple evaluators for consensus scoring.

**Human Evaluation** — Interactive scoring in HTML reports with export/import and calibration.

### HTML Reports

Reports are self-contained HTML files with:
- Score distributions and summary cards per evaluator
- What each evaluator tests (rubric, dimensions, metric description)
- Per-test-case details with AI reasoning ("why this score")
- Playable audio/video and inline images for media outputs
- Sortable results table with prompt visibility
- Interactive human scoring (1-5) with localStorage persistence

### Calibration

After collecting human scores, `judgejudy calibrate` computes per-evaluator:
- **Pearson/Spearman correlation** — ranking alignment
- **Mean bias** — systematic over/under-scoring
- **Agreement rate** — fraction within threshold

Then generates **rubric improvement suggestions** using Claude, analyzing the biggest AI-vs-human divergences with both sides' reasoning to produce specific, actionable rewording.

## Config Reference

Evaluations are defined in YAML:

```yaml
name: "My Evaluation"
description: "What this eval tests"

dataset:
  inline:
    id: "dataset-v1"
    modality: text    # text, image, audio, video
    test_cases:
      - id: "tc-1"
        input: "Your prompt here"
        expected_output: "Reference output (optional)"

generator:
  provider: openai    # any supported provider
  model: gpt-4o
  params:
    temperature: 0.7
    # Tool use — force structured JSON output via tool calls (Anthropic)
    tools: '[{"name":"submit_result","description":"Submit structured output","input_schema":{"type":"object","properties":{"data":{"type":"string"}},"required":["data"]}}]'
    # JSON extraction — extract JSON from raw output before evaluation
    json_extract: "[]"  # "[]" for arrays, "{}" for objects

evaluators:
  - name: "quality-judge"
    type: ai_judge
    provider: anthropic
    model: claude-sonnet-4-6
    rubric: |
      Evaluate on accuracy, clarity, and relevance.
    dimensions: [accuracy, clarity, relevance]
    scale: [1, 5]
    threshold: 0.7    # optional pass/fail

  - name: "bertscore"
    type: metric
    metric: bertscore

pipeline:
  concurrency: 3
  timeout_seconds: 60

report:
  output_path: "./report.html"
```

### Structured Output with Tool Use

When models ignore JSON format instructions and produce prose, use `tools` to force structured output via tool calls:

```yaml
generator:
  provider: anthropic
  model: claude-opus-4-5
  params:
    max_tokens: 32768
    tools: '[{"name":"submit_data","description":"Submit the result","input_schema":{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}},"required":["items"]}}]'
```

The provider sends the tool definition to the API and extracts the tool call input as the evaluation content. This guarantees valid JSON matching the schema. See `examples/tool_use_eval.yaml`.

### JSON Extraction

When models output JSON wrapped in markdown code fences or mixed with prose, `json_extract` strips everything except the JSON:

```yaml
generator:
  params:
    json_extract: "[]"  # Extract array: finds first [ and last ]
    # json_extract: "{}"  # Extract object: finds first { and last }
```

Applied after generation and before evaluation. See `examples/json_extract_eval.yaml`.

### Streaming for Large Outputs

The Anthropic provider automatically uses streaming for requests with `max_tokens > 16384`. This is required for Claude Opus with large outputs — the API returns an error without it. No configuration needed; it activates automatically. See `examples/streaming_eval.yaml`.

### Dataset Sources

The `dataset.path` field supports local files and GCS:

```yaml
# Local file (YAML or JSON)
dataset:
  path: "./my_dataset.yaml"

# GCS file — parsed as a standard dataset
dataset:
  path: "gs://my-bucket/evals/text_dataset.yaml"

# GCS directory — each file becomes a test case
# Filename (minus extension) = test case ID
# File content = test case input
dataset:
  path: "gs://my-bucket/evals/prompts/"
```

When pointing to a GCS directory, each file is processed as follows:
- **YAML/JSON files** with `input`/`id` fields are parsed as structured test cases
- **All other files** use the raw content as the input and the filename as the test case ID

GCS auth uses standard Google Application Default Credentials (`gcloud auth application-default login` or `GOOGLE_APPLICATION_CREDENTIALS`).

See `examples/` for complete configs covering text, image, audio, video, multi-judge consensus, and pre-generated content A/B evaluation.

## CLI Reference

```
judgejudy run <config.yaml>         Run an evaluation pipeline
  -r, --report string               Output HTML report path
  -b, --baseline                    Mark this run as baseline
      --compare string              Compare against a run ID
  -s, --sample int                  Sample N test cases
  -c, --concurrency int             Override concurrency

judgejudy compare <id1> <id2>       Compare two runs side by side
  -r, --report string               Output HTML report path

judgejudy list [runs|baselines]     List evaluation runs
      --dataset string              Filter by dataset ID
      --limit int                   Max results (default 20)

judgejudy report <run-id>           Generate report for a completed run
  -o, --output string               Output path

judgejudy human-eval import <run-id> <file.json>
                                    Import human scores from exported JSON

judgejudy calibrate <run-id>        Calibrate AI judges against human scores
  -o, --output string               Write calibration JSON to file
      --threshold float             Agreement threshold (default 0.1)

Global flags:
      --db string                   SQLite path (default ~/.judgejudy/judgejudy.db)
      --redis string                Redis address (empty to disable)
  -v, --verbose                     Debug logging
```

## Architecture

```
Config (YAML) ──> Pipeline ──> Report (HTML)
                     │
          ┌──────────┼──────────┐
          v          v          v
      Providers   Evaluators   Store
      (Generate)  (Score)      (SQLite + Redis)
          │          │
   ┌──────┼────┐   ┌─┴──────────┐
   v      v    v   v     v       v
 OpenAI  ...  WS  AI   Python  Human
              fal Judge Metrics  Eval
```

## Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, guidelines, and how to add new providers or metrics.

## License

[MIT](LICENSE)
