---
name: xai-static-voice-video
description: Create voiced static story videos in StagentHand with Grok xAI OAuth image generation/editing, character reference sheets for consistency, xAI OAuth TTS, HyperFrames timelines, FFmpeg final muxing, preview extraction, and ffprobe validation. Use when the user wants a rough-cut/static-image video with voice, no SVG-drawn art, no Remotion, no local TTS/model, no i2v, and xAI OAuth as the only AI provider.
---

# xAI Static Voice Video

Use this skill for static visual rough cuts that need Grok-generated stills and
xAI OAuth voice.

## Contract

- Generate visual assets with Grok xAI image endpoints. Do not hand-draw SVG
  story cards for production rough cuts.
- Generate character sheets before scene stills. Treat character sheets as the
  source of truth for character appearance.
- Generate scene stills from prompts plus character/style reference images.
- Use a newly rendered HyperFrames silent timeline as the video source.
- Generate narration/dialogue audio with `shand xai voice probe`.
- Do not use Remotion, local TTS, Polly, Qwen, or browser automation.
- Use FFmpeg for muxing/final packaging and `ffprobe` for validation.
- Keep outputs under `outputs/<slug>/`.

## Workflow

1. Write a shot plan with a fixed visual style and concise dialogue timing.
2. Generate one character sheet per recurring character with front, side, and
   three-quarter views on a plain background.
3. Generate scene stills with Grok image editing. Pass the relevant character
   sheet(s), and at most one style reference, because xAI image editing supports
   up to 3 source images.
4. Build a HyperFrames still-image timeline from those generated stills with
   `scripts/build_still_timeline.js`.
5. Put narration/dialogue text in a UTF-8 text file. Do not explain the joke;
   leave timing around the punchline.
6. Run `scripts/render_static_voice_video.sh` to create xAI voice and mux audio.
7. Verify the final MP4 has:
   - H.264 video
   - yuv420p
   - intended dimensions, usually 720x1280
   - stable 24 fps
   - AAC audio
   - duration long enough for the xAI voice track
5. Inspect `preview_frame.jpg`.

## Script

Generate a HyperFrames timeline from Grok stills:

```bash
.agents/skills/xai-static-voice-video/scripts/build_still_timeline.js \
  outputs/my-video \
  outputs/my-video/stills/scene_001.png:4 \
  outputs/my-video/stills/scene_002.png:4 \
  outputs/my-video/stills/scene_003.png:4.5

cd outputs/my-video/hyperframes
npx --yes hyperframes@0.6.55 render --output ../timeline_hyperframes.mp4 --fps 24
```

Render the final voice mux from the repository root:

```bash
SHAND_BIN=./shand .agents/skills/xai-static-voice-video/scripts/render_static_voice_video.sh \
  outputs/my-static-video/timeline_hyperframes.mp4 \
  outputs/my-static-video/narration.txt \
  outputs/my-static-video \
  eve \
  zh
```

If `SHAND_BIN` is unset, the script builds a temporary `shand` binary in the
output directory.

Outputs:

- `narration_xai_<voice>.mp3`
- `output_xai_voice.mp4`
- `preview_frame.jpg`
- `ffprobe.txt`

Use `shand xai image generate` for the first character/style references and
`shand xai image edit` for scene stills that need consistent recurring
characters.

Example character and scene commands:

```bash
shand xai image generate \
  --prompt "cute moose character reference sheet, front side and three-quarter views, no text" \
  --output outputs/my-video/characters/moose_sheet.png

shand xai image edit \
  --prompt "the same moose walks alone in a forest, no text" \
  --reference outputs/my-video/characters/moose_sheet.png \
  --output outputs/my-video/stills/scene_001.png
```

## Notes

- If the generated audio is longer than the timeline, the script pads the final
  visual by freezing the last frame so the punchline is not cut off.
- Store character sheets under `outputs/<slug>/characters/` and scene stills
  under `outputs/<slug>/stills/`.
- Keep reference prompts explicit: name the same character, body shape, colors,
  accessories, antler/neck proportions, and cartoon style every time.
- Use `--voices` or `--random` manually with `shand xai voice probe` only when
  testing voice selection; production rough cuts should record the selected
  voice in the output path or metadata.
