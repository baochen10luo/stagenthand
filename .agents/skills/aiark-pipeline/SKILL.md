---
name: aiark-pipeline
description: StagentHand pipeline workflow rules, especially the xAI OAuth-native path using HyperFrames and FFmpeg.
metadata:
  tags: shand, stagenthand, xai, oauth, hyperframes, ffmpeg, pipeline
---

# StagentHand Pipeline Rules

Use this skill before pipeline work in this repository.

## Default xAI OAuth Path

The default xAI OAuth route is the xAI-native pipeline:

```text
story input
  -> xAI OAuth LLM shot plan
  -> xAI OAuth video shots
  -> HyperFrames timeline
  -> FFmpeg finalization
  -> ffprobe validation
  -> preview_frame.jpg
```

For this path:

- HyperFrames is the primary timeline/render composition tool.
- FFmpeg/ffprobe owns media conditioning, final packaging, metadata validation,
  and preview extraction.
- Do not use Remotion as an implicit fallback.
- Do not use browser automation or Grok browser for the default xAI OAuth path.
- Do not use FFmpeg-only concat as the default renderer.
- Do not call local models in xAI OAuth tests unless explicitly requested.
- `remotion_props.json` is not the source of truth for xAI-native output;
  `xai_manifest.json` is.

## Static Visual Tests

When testing story timing with still images before I2V/video generation:

- Prefer a HyperFrames still-image timeline plus FFmpeg finalization.
- Do not route through Remotion unless the user explicitly asks for legacy
  Remotion output.
- Do not call xAI video generation or I2V for static-image rough cuts.
- Keep artifacts under an output directory and validate the MP4 with ffprobe.

## Validation

For xAI-native outputs, validate with:

```bash
shand xai inspect --strict <output-dir>
shand xai validate <output-dir>
```

For ad hoc static HyperFrames rough cuts, at minimum verify:

- MP4 is readable by ffprobe.
- dimensions match the intended format, usually 720x1280 portrait.
- fps is stable, usually 24.
- duration matches the sum of still-image holds.
- no audio stream unless the test explicitly adds audio.

## Legacy Routes

Remotion, Nova Reel, Grok browser, image generation, TTS, BGM, and old
`internal/pipeline` flows remain legacy or explicit backend paths. Keep them
separate from the xAI OAuth-native path.
