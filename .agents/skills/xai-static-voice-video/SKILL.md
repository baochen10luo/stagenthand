---
name: xai-static-voice-video
description: Create voiced static story videos in StagentHand with xAI OAuth TTS, HyperFrames silent timelines, FFmpeg final muxing, preview extraction, and ffprobe validation. Use when the user wants a rough-cut/static-image video with voice, no Remotion, no local TTS/model, no i2v, and xAI OAuth as the only AI provider.
---

# xAI Static Voice Video

Use this skill for static visual rough cuts that need xAI OAuth voice.

## Contract

- Use an existing or newly rendered HyperFrames silent timeline as the video source.
- Generate narration/dialogue audio with `shand xai voice probe`.
- Do not use Remotion, local TTS, Polly, Qwen, or browser automation.
- Use FFmpeg for muxing/final packaging and `ffprobe` for validation.
- Keep outputs under `outputs/<slug>/`.

## Workflow

1. Build or locate a silent HyperFrames timeline, usually `timeline_hyperframes.mp4`.
2. Put the narration/dialogue text in a UTF-8 text file.
3. Run `scripts/render_static_voice_video.sh`.
4. Verify the final MP4 has:
   - H.264 video
   - yuv420p
   - intended dimensions, usually 720x1280
   - stable 24 fps
   - AAC audio
   - duration long enough for the xAI voice track
5. Inspect `preview_frame.jpg`.

## Script

Run from the repository root:

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

## Notes

- If the generated audio is longer than the timeline, the script pads the final
  visual by freezing the last frame so the punchline is not cut off.
- Use `--voices` or `--random` manually with `shand xai voice probe` only when
  testing voice selection; production rough cuts should record the selected
  voice in the output path or metadata.
