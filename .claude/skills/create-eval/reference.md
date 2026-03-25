# JudgeJudy Config Reference

## Full Config Structure

```yaml
name: "Evaluation Name"
description: "What this evaluates"

dataset:
  # Option 1: Inline test cases
  inline:
    id: "dataset-id"
    name: "Dataset Name"
    version: "1.0.0"
    modality: text  # text, image, audio, video
    test_cases:
      - id: "tc-1"
        input: "The prompt"
        expected_output: "Reference output (optional, needed for bertscore/rouge)"
        tags: ["tag1", "tag2"]

  # Option 2: External file
  path: "./path/to/dataset.yaml"
  # Or GCS: path: "gs://bucket/path/to/dataset.yaml"

  # Optional: random sample
  sample: 5

generator:
  provider: openai
  model: gpt-4o
  params:
    temperature: 0.7
    max_tokens: 500

evaluators:
  - name: "judge-name"
    type: ai_judge
    provider: anthropic
    model: claude-sonnet-4-6
    mode: pointwise  # or pairwise
    rubric: |
      Multi-line rubric text...
    dimensions:
      - dimension_1
      - dimension_2
    scale: [1, 5]
    # threshold: 0.7  # optional pass/fail

  - name: "metric-name"
    type: metric
    metric: bertscore  # see metrics list
    params:
      model_type: "roberta-base"

pipeline:
  concurrency: 3
  timeout_seconds: 60
  cache_enabled: true

report:
  output_path: "./report.html"
  title: "Report Title"
```

## Provider-Specific Params

### OpenAI Text
```yaml
params:
  temperature: 0.7
  max_tokens: 500
```

### OpenAI TTS
```yaml
params:
  voice: "nova"  # alloy, echo, fable, onyx, nova, shimmer
  response_format: "wav"
```

### OpenAI Image (DALL-E 3)
```yaml
params:
  size: "1024x1024"
  quality: "hd"
```

### ElevenLabs
```yaml
params:
  model: "eleven_multilingual_v2"
  voice_id: "21m00Tcm4TlvDq8ikWAM"
  stability: 0.5
  similarity_boost: 0.75
```

### Cartesia
```yaml
params:
  model: "sonic-2"
  voice_id: "a0e99841-438c-4a64-b679-ae501e7d6091"
  language: "en"
  sample_rate: 44100
```

### WaveSpeed Video (T2V)
```yaml
params:
  model_path: "/v3/bytedance/seedance-v1.5-pro/text-to-video"
  duration: 5
```

### WaveSpeed Image
```yaml
params:
  model_path: "/v3/bytedance/seedream-v3.1"
```

### fal.ai Kling3
```yaml
params:
  model: "fal-ai/kling-video/v3/pro/image-to-video"
  duration: "5"
  aspect_ratio: "16:9"
  start_image_url: "https://..."  # required for i2v
```

## Available Metrics

| Metric | Modality | Needs Reference | Notes |
|--------|----------|-----------------|-------|
| bertscore | text | yes (expected_output) | Use model_type: "roberta-base" |
| rouge | text | yes | ROUGE-L F1 |
| bleu | text | yes | |
| clip_score | image | no (uses prompt) | |
| fid | image | yes (reference dir) | |
| lpips | image | yes | |
| ssim | image | yes | |
| pesq | audio | yes | |
| stoi | audio | yes | |
| utmos | audio | no | Neural MOS prediction |
| temporal_consistency | video | no | Frame-to-frame SSIM |
| clip_temporal | video | no | Semantic frame consistency |
