---
name: create-eval
description: Create a new JudgeJudy evaluation config file interactively
user-invocable: true
disable-model-invocation: true
allowed-tools: Read, Write, Glob, Bash, AskUserQuestion
argument-hint: [output-filename.yaml]
---

# Create a JudgeJudy Evaluation Config

You are helping the user create a new evaluation YAML config for JudgeJudy.

## Step 1: Gather Basic Info

Ask the user for the following (one question at a time, use AskUserQuestion):

1. **What are you evaluating?** (e.g., "text generation quality", "TTS naturalness", "image generation from prompts", "video generation realism")
2. **What modality?** — text, image, audio, or video
3. **Which provider and model for generation?** Show them the available options:
   - Text: openai (gpt-4o, gpt-4.1), anthropic (claude-sonnet-4-6), google (gemini-2.5-pro/flash), together, ollama
   - Image: openai (dall-e-3), wavespeed (seedream-v3.1, seedream-v4)
   - Audio: openai (tts-1, tts-1-hd), elevenlabs (eleven_multilingual_v2, eleven_v3), cartesia (sonic-2, sonic-3)
   - Video: wavespeed (seedance-v1.5-pro/text-to-video, wan-2.5-t2v), falai (kling-video/v3)
4. **What should the judge evaluate?** Ask them to describe the quality criteria. Examples: "accuracy and clarity", "visual quality and prompt adherence", "naturalness and intelligibility", "realism and temporal consistency"
5. **Which provider/model for the AI judge?** Recommend:
   - Text/Image judging: anthropic (claude-sonnet-4-6) or openai (gpt-4o)
   - Audio judging: google (gemini-2.0-flash) — supports native audio
   - Video judging: anthropic (claude-sonnet-4-6) — via frame extraction
6. **Do you have test cases already, or should I generate some?** If they want generated ones, ask how many (default 10) and what topics/themes.
7. **Do you want automated metrics too?** Suggest relevant ones based on modality:
   - Text: bertscore (needs reference), rouge, bleu
   - Image: clip_score (no reference needed)
   - Audio: utmos (no reference needed)
   - Video: temporal_consistency, clip_temporal (no reference needed)
8. **Output filename?** Default to `$ARGUMENTS` if provided, otherwise `examples/<modality>_eval_custom.yaml`

## Step 2: Generate the Config

Based on the answers, generate a complete YAML config file. Follow the exact format used in existing examples.

Read an existing example for reference based on the modality:
- Text: read `examples/text_eval.yaml`
- Image: read `examples/image_eval.yaml`
- Audio: read `examples/audio_eval.yaml`
- Video: read `examples/video_eval.yaml`

## Step 3: Write the rubric

Write a detailed, specific rubric based on what the user said they want to evaluate. Break it into clear dimensions. Each dimension should have a description of what 1 (worst) and 5 (best) mean.

Example rubric format:
```
Evaluate the response on these criteria:
- Dimension Name: Description. Score 1 if [bad]. Score 5 if [good].
```

## Step 4: Generate test cases

If the user wants generated test cases, create diverse, realistic prompts that cover different aspects of the evaluation. Include:
- A mix of easy and hard cases
- Different topics/styles
- Edge cases where relevant
- `expected_output` for text evaluations (needed for bertscore/rouge)

## Step 5: Write and validate

1. Write the YAML file using the Write tool
2. Run `./judgejudy run <filename> --verbose` with `--sample 1` to validate it works with a single test case
3. If it fails, fix the issue and retry
4. Tell the user the file is ready and show them the command to run the full evaluation

## Important Rules

- Always use the latest model names (claude-sonnet-4-6, gpt-4o, gemini-2.5-flash, etc.)
- For video generators using wavespeed, include both `model` and `model_path` params
- For audio, include appropriate voice params
- Set reasonable pipeline defaults (concurrency: 3, timeout: 60-300 depending on modality)
- Don't set thresholds unless the user specifically asks for pass/fail criteria
- Keep test case IDs short and descriptive (e.g., "tc-simplify-1", "img-landscape-1")