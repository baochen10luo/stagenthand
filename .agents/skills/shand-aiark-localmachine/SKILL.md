---
name: shand-aiark-localmachine
description: Run shand video pipeline on aiark server — pre-flight, narration/verbatim/standard modes, clean-cache workflow, SCP to MacBook
metadata:
  tags: pipeline, video, aiark, shand
---

Run a full local pipeline on the aiark server using ERNIE image, Qwen TTS (paul-chen voice), and ACE-Step BGM.

## Pre-flight checklist

Before running, verify these services are up on the aiark server:

```bash
ssh aiark-agent "docker ps --format '{{.Names}}\t{{.Status}}' | grep -E 'ollama|image-api|tts-api|acestep|music-api'"
```

Expected: all should be `Up ... (healthy)`. If any are missing:
- **Ollama** (LLM): `ssh aiark-agent "cd /opt/aiark-agent && docker compose --profile ollama up -d ollama"`
- **image-api** (ERNIE): `ssh aiark-agent "cd /opt/aiark-agent && docker compose --profile image up -d image-api"`
- **ACE-Step + music-api**: `ssh aiark-agent "cd /opt/aiark-agent && docker compose --profile acestep up -d acestep"`

## Config file

`~/.shand/config-local.yaml`:

```yaml
llm:
  provider: openai-compat
  base_url: https://aiark.com.tw/v1
  model: aiark/qwen36-35b-iq3
  api_key: datasys2026
  no_json_mode: true
  strip_think_tags: true

image:
  provider: aiark
  base_url: https://aiark.com.tw/image
  model: aiark/ernie-image-turbo
  api_key: datasys2026
  width: 576
  height: 1024

audio:
  voice_provider: aiark
  aiark_tts_base_url: https://aiark.com.tw/tts
  aiark_tts_api_key: datasys2026
  aiark_tts_voice_id: paul-chen-zh-tw-v1
  music_provider: aiark
  aiark_music_base_url: https://aiark.com.tw/music
  aiark_music_api_key: datasys2026

remotion:
  template_path: ./remotion-template
```

## Execution modes

### Standard mode
LLM expands story through full outline → storyboard → panels pipeline:
```bash
echo "故事主題" | ./shand pipeline --skip-hitl --config ~/.shand/config-local.yaml
```

### Narration mode (`--narration`)
LLM rewrites story as single narrator voice. All dialogue becomes `speaker: ""`. No new content added — only segments and adapts the original text:
```bash
echo "故事內容" | ./shand pipeline --skip-hitl --narration --config ~/.shand/config-local.yaml
```

### Verbatim mode (`--verbatim`)
LLM only segments original text into panels, copies dialogue character-for-character. No invention, no rewriting:
```bash
echo "故事內容" | ./shand pipeline --skip-hitl --verbatim --config ~/.shand/config-local.yaml
```

### With retries (`--max-retries`)
Adds AI critic loop — re-renders up to N times if quality check fails. Each attempt gets its own versioned output file:
```bash
echo "故事內容" | ./shand pipeline --skip-hitl --narration --max-retries 3 --config ~/.shand/config-local.yaml
```

## Clean cache before re-run

Smart resume reuses existing images/audio by filename. If you changed the script or narration, **always clear the project folder first** to avoid mismatched assets:

```bash
# Find project folder from previous run output, then:
rm -rf ~/.shand/projects/<project_id>/
# Then re-run normally
```

## Output

Filename format: `{project_id}_{YYYYMMDD_HHMMSS}_v{n}.mp4`

Example: `elk-lost-in-forest_20260512_143022_v1.mp4`

Located at: `~/.shand/projects/<project_id>/`

SCP to MacBook:
```bash
scp ~/.shand/projects/<project_id>/<filename>.mp4 paulchen@100.120.51.5:/Users/paulchen/Downloads/
```

## Subtitle line breaking (portrait 9:16)

`BreakLongDialogueLines` in `internal/remotion/subtitle.go` splits dialogue at punctuation before Remotion rendering. Two-tier punctuation:

- **breakPunct** (。！？，；：.!?,;) — triggers line break AND is removed
- **stripPunct** (「」『』（）【】〈〉…—～) — removed silently, no break

`、` is deliberately **not** in breakPunct to avoid cutting enumeration phrases (e.g. "三、四分大的祖產田地"). Spaces within a phrase are preserved (e.g. "deadline 真的" stays intact).

Rerunning the pipeline reuses existing images/audio via Smart Resume, but `remotion_props.json` is regenerated with new subtitle segments.

## Known retry behaviour

- **LLM 5xx / GPU busy (409)**: Qwen waits up to 24 × 10s. Normal when image model just ran and GPU is releasing VRAM.
- **Image VRAM**: ERNIE waits for GPU. Normal when Qwen is still loaded.
- **Image 502**: ERNIE recycled after each request (`IMAGE_RECYCLE_AFTER_REQUEST=true`). Retries automatically.
