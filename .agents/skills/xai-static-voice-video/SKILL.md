---
name: xai-static-voice-video
description: Create voiced non-I2V static story videos in StagentHand with Grok xAI OAuth image generation/editing, character reference sheets for consistency, xAI OAuth TTS, HyperFrames still-image timelines, FFmpeg final muxing, preview extraction, and ffprobe validation. Use when the user wants a static-image video with voice, no SVG-drawn art, no Remotion, no local TTS/model, no I2V unless explicitly requested, and xAI OAuth as the only AI provider.
---

# xAI Static Voice Video

Use this skill first for the non-I2V static pipeline: Grok-generated stills,
HyperFrames still-image timeline, xAI OAuth voice, and FFmpeg final muxing.
I2V is an explicit upgrade path only; do not use it unless the user asks for
I2V, animation, moving clips, or a video-generation upgrade.

## Default Non-I2V Pipeline

This is the default route for voiced story tests:

```text
story
  -> xAI OAuth shot plan
  -> Grok character sheets
  -> Grok referenced stills
  -> subtitles.ass
  -> xAI OAuth vision audit
  -> HyperFrames still-image timeline
  -> xAI OAuth voice
  -> FFmpeg mux/finalize
  -> ffprobe + preview extraction
```

Hard rules:

- Do not run `shand xai video i2v`.
- Do not generate MP4 clips per scene.
- Do not use Remotion, browser automation, local image models, or local TTS.
- Use `timeline_hyperframes.mp4` from still images as the video source.
- Final filename should identify this as non-I2V by omitting `i2v`, for example
  `<story-slug>-grok-ref-xai-voice-v1.mp4`.

## Contract

- Generate visual assets with Grok xAI image endpoints. Do not hand-draw SVG
  story cards for production rough cuts.
- Generate character sheets before scene stills. Treat character sheets as the
  source of truth for character appearance.
- Generate scene stills from prompts plus character/style reference images.
- For the default static pipeline, use a newly rendered HyperFrames still-image
  timeline as the video source.
- For I2V upgrades, use the audited still as the first frame/source image for
  `shand xai video i2v`; do not ask I2V to redesign the scene.
- Generate narration/dialogue audio with `shand xai voice probe`.
- Do not use Remotion, local TTS, Polly, Qwen, or browser automation.
- Add visible subtitles with an ASS subtitle file when the rough cut has voice.
- Run an xAI OAuth vision audit on generated stills before final packaging.
- Audit animal anatomy explicitly. Regenerate any still or I2V clip with extra
  arms, six hands, duplicated forelegs, fused limbs, malformed hooves, or other
  impossible limb counts.
- Use FFmpeg for muxing/final packaging and `ffprobe` for validation.
- Preserve original video audio when present, but lower it under the xAI voice
  track.
- Keep outputs under `outputs/<slug>/`.
- Use meaningful final filenames, for example
  `<story-slug>-grok-ref-xai-voice-v1.mp4`.

## Subtitle Rules

- Avoid Chinese punctuation in rendered subtitles: `，。？！、；：`.
- Prefer short subtitle lines over long complete sentences.
- Keep each subtitle visible for at least about 1.5 seconds.
- Target Chinese reading speed around 6 characters per second.
- Do not translate or rewrite proper nouns unnecessarily, for example OpenCL,
  Linux, and Fable.
- Dubbing/subtitle timing may lag behind the source video slightly, like
  synchronized interpretation.
- If the dubbing is longer than the picture track, extend the video by freezing
  the final frame.
- If the source video has audio, keep it as low-volume bed audio and overlay
  the dubbed voice.
- In HITL review, the `字幕：` field is the main final human-edit entry point.

## Static Workflow

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
6. Write `subtitles.ass` with short visible subtitle lines. Keep subtitles to
   the played timing and do not add joke explanations.
7. Audit stills with `shand xai image audit-story`. The audit must use an xAI
   OAuth vision-capable model and check story fit, character consistency,
   spatial relationships, unwanted text/speech bubbles, and whether phone-call
   scenes are cross-cut instead of showing separated callers physically
   together. It must also check animal anatomy and reject extra limbs/hands.
8. Run `scripts/render_static_voice_video.sh` to create xAI voice and mux
   audio. `build_still_timeline.js` automatically overlays
   `outputs/<slug>/subtitles.ass` into the HyperFrames timeline when that file
   exists.
9. Verify the final MP4 has:
   - H.264 video
   - yuv420p
   - intended dimensions, usually 720x1280
   - stable 24 fps
   - AAC audio
   - duration long enough for the xAI voice track
10. Inspect `preview_frame.jpg`.

## I2V Upgrade Workflow

Use this only when the user explicitly asks for I2V or animation, and only
after the static stills pass story and anatomy audit. If the user asks for a
"video" without saying I2V, keep the static workflow.

1. Treat each approved still as the source image and first frame.
2. Generate one short I2V clip per still with `shand xai video i2v`.
3. Prompts should request small motion only: breathing, blinking, hand/phone
   micro-movement, slight camera drift, and forest light movement. Include
   "preserve the exact first frame composition" and "no extra limbs or hands".
4. Build a HyperFrames video timeline from the I2V clips. Keep the existing
   `subtitles.ass` rules.
5. If I2V clips contain native audio, keep it low under the xAI voice track.
6. Validate with ffprobe and extract preview frames from dialogue/punchline
   moments.

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

Build a HyperFrames timeline from I2V clips:

```bash
.agents/skills/xai-static-voice-video/scripts/build_video_timeline.js \
  outputs/my-video \
  outputs/my-video/i2v/scene_001.mp4:4 \
  outputs/my-video/i2v/scene_002.mp4:4 \
  outputs/my-video/i2v/scene_003.mp4:4

cd outputs/my-video/hyperframes_i2v
npx --yes hyperframes@0.6.55 render --output ../timeline_i2v_hyperframes.mp4 --fps 24
```

Render the final voice mux from the repository root:

```bash
SHAND_BIN=./shand .agents/skills/xai-static-voice-video/scripts/render_static_voice_video.sh \
  outputs/my-static-video/timeline_hyperframes.mp4 \
  outputs/my-static-video/narration.txt \
  outputs/my-static-video \
  eve \
  zh \
  my-static-video-grok-ref-xai-voice-v1.mp4
```

If `SHAND_BIN` is unset, the script builds a temporary `shand` binary in the
output directory.

Outputs:

- `narration_xai_<voice>.mp3`
- `output_xai_voice.mp4` or the requested final filename
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

shand xai image audit-story \
  --story "Moose gets lost and calls giraffe by phone; final punchline is cross-cut, not co-located." \
  --scene outputs/my-video/stills/scene_001.png \
  --scene outputs/my-video/stills/scene_002.png \
  --scene outputs/my-video/stills/scene_003.png \
  --scene outputs/my-video/stills/scene_004.png \
  --scene outputs/my-video/stills/scene_005.png \
  --output outputs/my-video/audit_xai.json

shand xai video i2v \
  --image outputs/my-video/stills/scene_001.png \
  --prompt "Preserve the exact first frame composition. Gentle storybook motion, slow camera drift, subtle breathing and forest light. No text. No extra limbs or hands." \
  --duration 4 \
  --aspect-ratio 9:16 \
  --resolution 720p \
  --output outputs/my-video/i2v/scene_001.mp4
```

## Notes

- If the generated audio is longer than the timeline, the script pads the final
  visual by freezing the last frame so the punchline is not cut off.
- If a phone-call scene shows both callers standing in the same physical place,
  regenerate that still before final packaging. Prefer a reverse angle,
  close-up, or split-screen composition that clearly communicates remote
  contact.
- Store character sheets under `outputs/<slug>/characters/` and scene stills
  under `outputs/<slug>/stills/`.
- Keep reference prompts explicit: name the same character, body shape, colors,
  accessories, antler/neck proportions, and cartoon style every time.
- Use `--voices` or `--random` manually with `shand xai voice probe` only when
  testing voice selection; production rough cuts should record the selected
  voice in the output path or metadata.
