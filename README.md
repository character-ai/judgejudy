# judgejudy

Multimodal AI evaluation framework — judge text, image, audio, and video generation with any model.

## Features

- **Multi-provider**: OpenAI, Anthropic, Google Gemini, Replicate, Together AI, Ollama
- **Multi-modality**: Evaluate text, image, audio, and video outputs
- **AI-as-Judge**: Use any LLM as an evaluator with custom rubrics and scoring dimensions
- **Automated Metrics**: BERTScore, ROUGE, BLEU, FID, LPIPS, CLIP Score, PESQ, STOI, and more via Python bridge
- **Composite Scoring**: Weighted multi-evaluator consensus (e.g., 3 different judges)
- **Pairwise Comparison**: Compare outputs head-to-head with bias mitigations
- **HTML Reports**: Self-contained, professional reports with charts and expandable details
- **Caching**: Redis-backed generation cache to avoid redundant API calls
- **Storage**: SQLite for run history, baselines, and comparisons

## Quick Start

### Install

```bash
# Build from source
make build

# Or install to $GOPATH/bin
make install
```

### Set API Keys

```bash
# Copy the example env file and fill in your keys
cp .env.example .env

# Or export directly — only set keys for providers you use:
export JUDGEJUDY_OPENAI_API_KEY="sk-..."
export JUDGEJUDY_ANTHROPIC_API_KEY="sk-ant-..."
export JUDGEJUDY_GOOGLE_API_KEY="..."
export JUDGEJUDY_REPLICATE_API_KEY="r8_..."
export JUDGEJUDY_TOGETHER_API_KEY="..."
# Ollama: no key needed, uses JUDGEJUDY_OLLAMA_HOST (default http://localhost:11434)
```

### Install Python Dependencies (for automated metrics)

```bash
pip install -r python/requirements.txt
```

### Run an Evaluation

```bash
# Run with an example config
judgejudy run examples/text_eval.yaml --report ./report.html

# Mark a run as baseline
judgejudy run examples/text_eval.yaml --baseline

# Compare against a previous run
judgejudy run examples/text_eval.yaml --compare <run-id>
```

## Architecture

```
┌──────────┐     ┌───────────────┐     ┌─────────────┐
│  Config   │────▶│   Pipeline    │────▶│   Report    │
│  (YAML)   │     │ (Orchestrator)│     │   (HTML)    │
└──────────┘     └───────┬───────┘     └─────────────┘
                         │
              ┌──────────┼──────────┐
              ▼          ▼          ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Provider │ │Evaluator │ │  Store   │
        │(Generate)│ │ (Score)  │ │(SQLite+  │
        └──────────┘ └──────────┘ │ Redis)   │
              │          │        └──────────┘
    ┌─────────┼────┐   ┌─┴────┐
    ▼    ▼    ▼    ▼   ▼      ▼
  OpenAI Anthr Gem  ...  AI    Python
              opi  ini   Judge  Metrics
              c
```

## Config Reference

Create a YAML config file to define your evaluation:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | yes | — | Evaluation name |
| `description` | string | no | — | Description |
| `dataset.path` | string | one of path/inline | — | Path to dataset YAML/JSON file |
| `dataset.inline` | object | one of path/inline | — | Inline dataset definition |
| `dataset.sample` | int | no | 0 (all) | Random sample N test cases |
| `generator.provider` | string | yes | — | Provider name |
| `generator.model` | string | yes | — | Model name |
| `generator.params` | map | no | — | Model parameters (temperature, etc.) |
| `evaluators[]` | array | yes | — | List of evaluator configs |
| `pipeline.concurrency` | int | no | 5 | Max parallel test cases |
| `pipeline.timeout_seconds` | int | no | 120 | Per-test-case timeout |
| `pipeline.retry_attempts` | int | no | 2 | Retry count on failure |
| `pipeline.cache_enabled` | bool | no | true | Enable Redis caching |
| `pipeline.fail_fast` | bool | no | false | Stop on first error |
| `report.output_path` | string | no | ./report.html | HTML report output path |
| `report.title` | string | no | — | Report title |

### Evaluator Types

**AI Judge** (`type: ai_judge`):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | string | yes | Judge model provider |
| `model` | string | yes | Judge model name |
| `mode` | string | no | `pointwise` (default) or `pairwise` |
| `rubric` | string | yes | Evaluation rubric/instructions |
| `dimensions` | []string | no | Scoring dimensions |
| `scale` | [int, int] | no | Score range (default [1, 5]) |
| `threshold` | float | no | Pass/fail threshold (0.0-1.0) |

**Metric** (`type: metric`):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `metric` | string | yes | Metric name (see table below) |
| `params` | map | no | Metric-specific parameters |

**Composite** (`type: composite`):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `children` | []evaluator | yes | Child evaluators |
| `threshold` | float | no | Pass/fail threshold for weighted average |

## Supported Providers

| Provider | Text | Image Gen | Audio Gen | Video Gen | Vision Judge | Audio Judge |
|----------|------|-----------|-----------|-----------|-------------|-------------|
| OpenAI | yes | yes (DALL-E) | yes (TTS) | — | yes (GPT-4o) | — |
| Anthropic | yes | — | — | — | yes (Claude) | — |
| Google Gemini | yes | — | — | — | yes | yes (native) |
| Replicate | — | yes | yes | yes | — | — |
| Together AI | yes | yes | — | — | — | — |
| Ollama | yes | — | — | — | yes (llava) | — |

## Supported Metrics

| Metric | Modality | Library | Description |
|--------|----------|---------|-------------|
| `bertscore` | text | bert-score | Semantic similarity (F1) |
| `rouge` | text | rouge-score | ROUGE-L F1 |
| `bleu` | text | nltk | BLEU score |
| `fid` | image | clean-fid | Frechet Inception Distance |
| `lpips` | image | lpips | Learned Perceptual Image Patch Similarity |
| `clip_score` | image | transformers | CLIP cosine similarity |
| `ssim` | image | torchmetrics | Structural Similarity Index |
| `pesq` | audio | pesq | Perceptual Evaluation of Speech Quality |
| `stoi` | audio | pystoi | Short-Time Objective Intelligibility |
| `temporal_consistency` | video | opencv | Frame-to-frame SSIM |
| `clip_temporal` | video | transformers | Temporal CLIP embedding consistency |

## CLI Reference

### `judgejudy run <config.yaml>`

Run an evaluation pipeline.

```
Flags:
  -r, --report string     Output path for HTML report
  -b, --baseline          Mark this run as baseline
      --compare string    Run ID to compare against
  -s, --sample int        Override dataset sample size
  -c, --concurrency int   Override concurrency
```

### `judgejudy compare <run-id-1> <run-id-2>`

Compare two evaluation runs side by side.

```
Flags:
  -r, --report string     Output path for HTML report
```

### `judgejudy report <run-id>`

Generate an HTML report for a completed run.

```
Flags:
  -o, --output string     Output path (default: report_<run-id>.html)
      --compare string    Include comparison data against another run
```

### `judgejudy list [runs|baselines]`

List evaluation runs.

```
Flags:
      --dataset string    Filter by dataset ID
      --limit int         Maximum results (default 20)
```

### Global Flags

```
      --db string         SQLite database path (default ~/.judgejudy/judgejudy.db)
      --redis string      Redis address (e.g. localhost:6379, empty to disable)
  -v, --verbose           Enable debug logging
```

## Examples

See the `examples/` directory for complete evaluation configs:

- `text_eval.yaml` — Text generation with Claude as judge + BERTScore
- `image_eval.yaml` — DALL-E 3 image gen with GPT-4o vision judge + CLIP score
- `audio_eval.yaml` — TTS evaluation with Gemini audio judge + PESQ
- `video_eval.yaml` — Video generation with frame analysis + temporal metrics
- `multi_judge.yaml` — Multi-judge consensus with composite scoring
