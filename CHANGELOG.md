# Changelog

## Unreleased

### Added
- **Anthropic streaming support** — auto-streams for `max_tokens > 16384`, required for Opus with large outputs
- **Anthropic tool use support** — `tools` generator param forces structured JSON output via tool calls, extracting tool input as evaluation content
- **JSON extraction post-processing** — `json_extract` generator param strips non-JSON content from model output before evaluation (`"[]"` for arrays, `"{}"` for objects)
- New examples: `tool_use_eval.yaml`, `streaming_eval.yaml`, `json_extract_eval.yaml`

## 0.1.0 — 2025-03-26

### Added
- Initial open-source release
- Multimodal evaluation: text, image, audio, video
- AI judge evaluators with custom rubrics (pointwise + pairwise)
- Automated metrics via Python (BERTScore, CLIP, PESQ, SSIM, etc.)
- Human evaluation with interactive HTML reports
- AI judge calibration against human feedback
- Baseline tracking and run comparison
- 11 generation providers (OpenAI, Anthropic, Google, ElevenLabs, Cartesia, WaveSpeed, fal.ai, Replicate, Together, Ollama, Passthrough)
- Passthrough provider for evaluating pre-generated content
