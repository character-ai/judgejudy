# Contributing to JudgeJudy

Thanks for your interest in contributing! This guide will help you get started.

## Getting Started

### Prerequisites

- Go 1.22+
- Python 3.9+ (for automated metrics)
- ffmpeg (for video frame extraction)
- Redis (optional, for caching)

### Setup

```bash
git clone https://github.com/character-ai/judgejudy.git
cd judgejudy
make build
make deps   # Go modules + Python requirements
```

Copy `.env.example` to `.env` and fill in API keys for the providers you need:

```bash
cp .env.example .env
```

### Running Tests

```bash
make test    # Go tests with race detection
make lint    # golangci-lint
```

## Project Structure

```
cmd/judgejudy/          CLI entrypoint
internal/
  cli/                  Cobra command definitions
  config/               YAML config loading and validation
  pipeline/             Orchestrates generation + evaluation
  provider/             Generation backends (OpenAI, Anthropic, etc.)
  evaluator/            Scoring logic (AI judge, metrics, composite)
  calibrate/            Human-vs-AI calibration statistics
  report/               HTML report generation
  store/                SQLite persistence + Redis cache
  dataset/              Dataset loading (local files, GCS)
  models/               Shared types
  util/                 Utilities (video frame extraction)
python/
  metrics/              Python metric scripts (BERTScore, CLIP, etc.)
examples/               Example evaluation configs
```

## How to Contribute

### Reporting Bugs

Open a [GitHub issue](https://github.com/character-ai/judgejudy/issues) with:
- What you expected to happen
- What actually happened
- Steps to reproduce
- Config YAML (redact any API keys)

### Suggesting Features

Open an issue describing the use case and proposed solution. Discussion before implementation helps avoid wasted effort.

### Submitting Changes

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Run `make test` and `make lint`
4. Ensure `go build ./...` and `go vet ./...` pass
5. Open a pull request with a clear description of what and why

## Adding a New Provider

1. Create `internal/provider/<name>.go`
2. Implement the `Provider` interface:

```go
type Provider interface {
    Name() string
    Generate(ctx context.Context, req *models.GenerateRequest) (*models.GenerateResponse, error)
    SupportsModality(m models.Modality) bool
}
```

3. Register in `init()`:

```go
func init() {
    Register("myprovider", func(apiKey string) (Provider, error) {
        return &MyProvider{apiKey: apiKey}, nil
    })
}
```

4. API keys are resolved automatically from `JUDGEJUDY_<PROVIDER>_API_KEY` environment variables.
5. Use the shared helpers in `helpers.go` (`doJSON`, `downloadAndEncode`, `pollForCompletion`, `httpError`, `getParam*`).
6. Use `io.LimitReader` on all HTTP response body reads -- see existing providers for the pattern.
7. Add cost entries in `costs.go` if pricing data is available.

## Adding a New Metric

1. Add a function in the appropriate `python/metrics/<modality>_metrics.py`
2. Register it in the `METRICS` dict at the bottom of the file
3. The function receives a JSON input file and writes a JSON output file:

```python
def my_metric(input_path, output_path):
    with open(input_path) as f:
        data = json.load(f)
    # data has: input, generated_output, expected_output, content_type, file_path, metric, params
    score = compute_score(data)
    with open(output_path, 'w') as f:
        json.dump({"score": score, "raw": {"detail": "..."}}, f)
```

4. Scores should be normalized to 0-1 where higher is better.

## Adding a New Evaluator Type

The evaluator interface is in `internal/evaluator/evaluator.go`:

```go
type Evaluator interface {
    Name() string
    Type() models.EvalType
    Evaluate(ctx context.Context, input models.TestCase, output models.GenerateResponse) (*models.Score, error)
}
```

Wire it up in `NewEvaluator()` in the same file.

## Code Guidelines

- **Error handling**: Always check and propagate errors. Use `fmt.Errorf("context: %w", err)` for wrapping.
- **HTTP responses**: Always use `io.LimitReader` when reading response bodies.
- **Concurrency**: The pipeline runs evaluators concurrently. Evaluators must be safe for concurrent use.
- **Path safety**: Sanitize user-supplied strings (test case IDs, file paths) with `filepath.Base()` before using in file operations.
- **Keep it simple**: Avoid abstractions until they're needed in more than one place.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
