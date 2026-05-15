# StagentHand (`shand`)

![StagentHand Banner](assets/banner.png)

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

[繁體中文](./README.zh-TW.md)

> **CLI-first AI short drama pipeline — fully automated, agent-driven production.**

---

## Pipeline Flow

```
Story Prompt
  ↓ story-to-outline       (LLM)
Outline JSON
  ↓ outline-to-storyboard  (LLM)
Storyboard JSON
  ↓ storyboard-to-panels   (LLM)
Panel[] JSON
  ↓ panels-to-images       (Nano Banana 2 / Nova Canvas, concurrent)
Panel[] + image_url
  ↓ TTS                    (Amazon Polly Neural + SSML)
Panel[] + audio_url
  ↓ BGM                    (Jamendo API)
  ↓ storyboard-to-remotion-props
RemotionProps JSON
  ↓ remotion-render        (npx remotion)
output.mp4
  ↓ critic                 (Amazon Nova Pro, multimodal)
APPROVE / REJECT
  ↓ postprod loop          (optional: auto fix → rerender until APPROVE)
Converged mp4
```

---

## Features

### Core Pipeline

End-to-end pipeline from a raw story prompt to a rendered MP4. Every stage reads from stdin and writes to stdout as JSON, composable with standard Unix tools.

### LLM Support

All providers are hot-swappable via config — no code changes required. Priority: flag > env > `~/.shand/config.yaml` > defaults.

| Provider | Config value | Notes |
|---|---|---|
| Local / self-hosted (Qwen3, vLLM, LM Studio) | `llm.provider: openai` | Default; set `base_url` to your endpoint |
| OpenAI | `llm.provider: openai` | Set `api_key` |
| Google Gemini | `llm.provider: gemini` | Set `api_key` |
| Anthropic Claude | `llm.provider: anthropic` | Set `api_key` or `ANTHROPIC_API_KEY` env |
| AWS Bedrock (Nova, Claude) | `llm.provider: bedrock` | Uses shared `aws:` credentials |

### Image Generation

Providers are swappable via `image.provider`. Returned format (PNG / JPEG / WebP) is detected automatically from magic bytes — no hardcoded format assumption.

| Provider | Config value | Notes |
|---|---|---|
| aiark (Qwen2.5-VL) | `image.provider: aiark` | Self-hosted; set `image.base_url` |
| Nano Banana 2 | `image.provider: nanobanana` | Gemini-based; supports character refs |
| AWS Nova Canvas | `image.provider: bedrock` + `image.model: amazon.nova-canvas-*` | Uses shared `aws:` credentials |
| AWS Titan | `image.provider: bedrock` + `image.model: amazon.titan-image-generator-*` | Uses shared `aws:` credentials |
| Stability AI | `image.provider: stability` | Via Bedrock; uses shared `aws:` credentials |

### Text-to-Speech

TTS provider is hot-swappable via `audio.voice_provider`.

| Provider | Config value | Notes |
|---|---|---|
| Amazon Polly Neural | `voice_provider: polly` | Default; multi-language, SSML |
| aiark TTS (Qwen3-TTS) | `voice_provider: aiark` | Self-hosted; set `aiark_tts_base_url` |

Multi-speaker mode (`--multi-speaker`) routes each `DialogueLine` to a per-character voice based on the character registry.

### Background Music

BGM provider is hot-swappable via `audio.music_provider`.

| Provider | Config value |
|---|---|
| Jamendo | `music_provider: jamendo` (default) |
| aiark Music (ACE-Step) | `music_provider: aiark` |

### AI Critic

Post-render evaluation using a multimodal LLM. Provider is independently configurable via `critic.provider` — decoupled from the generation LLM. Scores across 4 dimensions; hard-stop thresholds: `visual_score ≥ 8`, `audio_sync_score ≥ 8`, total `≥ 32/40`.

| Provider | Config value |
|---|---|
| AWS Bedrock Nova Pro | `critic.provider: bedrock` (default) |

| Dimension | Description |
|---|---|
| Visual Coherence (A) | Character consistency, subtitle cleanliness |
| Audio-Visual Sync (B) | BGM ducking, voice naturalness, subtitle timing |
| Directive Adherence (C) | BGM mood match, visual directive compliance |
| Narrative Tone (D) | Pacing, dramatic breathing room, story closure |

### Directives System

Two global directives injected into the pipeline via JSON:

- `style_prompt`: Prepended to every panel's image generation prompt for visual consistency.
- `bgm_tags`: Passed to Jamendo for music mood selection.

Additional per-panel `PanelDirective` fields control camera motion (`ken_burns_in`, `pan_left`, etc.), transition type, subtitle position, and font size.

### Multi-Language TTS

Amazon Polly Neural with multi-language support. Use `--language` to select the voice locale. Defaults to `zh-TW`.

| Language code | Locale |
|---|---|
| `zh-TW` | Traditional Chinese (Taiwan) — default |
| `cmn-CN` | Simplified Chinese (Mainland) |
| `en-US` | English (United States) |
| `en-GB` | English (United Kingdom) |
| `ja-JP` | Japanese |
| `ko-KR` | Korean |

### AI Critic Auto-Retry

When `--max-retries N` is set, a REJECT verdict automatically triggers up to N retry cycles. Each cycle adjusts pipeline parameters based on which dimension scored below threshold:

| Condition | Action |
|---|---|
| `visual_score < 8` | Append 8K detail hint to `StylePrompt` |
| `audio_sync_score < 8` | Decrease `DuckingDepth` by 0.1 |
| `tone_score < 6` | Multiply `DurationSec` by 1.2 |

### Character Registry

Persistent reference image store under `~/.shand/characters/<name>/ref.png`. Register a character once; the pipeline automatically injects their reference image into every panel that names them, ensuring visual consistency across scenes and episodes.

```bash
# Generate a reference sheet via the image provider, then register
./shand character generate 阿志 --description "男，28歲，短黑髮，黑框眼鏡，白色廚師服"

# Or register from an existing file
./shand character register 小芸 --image ./xiaoyun_ref.png

# List registered characters
./shand character list
```

Once registered, any panel whose `characters` array includes the name will automatically receive the reference image path in `character_refs`, passed through to the image generation prompt.

### Batch Production

Produce multiple episodes from a single story prompt with `--episodes N`. Episodes run concurrently up to the limit set by `--batch-concurrency` (default: 2). Each episode gets its own project directory and job ID.

### Agentic Post-Production

Phase 9.5 adds a fully autonomous post-production loop. The `postprod` subcommands evaluate a rendered MP4, generate an edit plan, apply patches to `RemotionProps`, and re-render — all without human intervention.

Post-production operations are organized in three layers:

**Layer A — API calls required:**
- `regenerate_image`: Regenerate a specific panel's image via image provider
- `regenerate_audio`: Re-synthesize dialogue audio via TTS
- `replace_bgm`: Fetch a new BGM track from Jamendo

**Layer B — Zero cost, props-only patches:**
- `patch_dialogue`: Edit subtitle/dialogue text
- `patch_duration`: Adjust a panel's display duration
- `patch_panel_directive`: Modify per-panel directives (camera motion, transition, etc.)
- `patch_global_directive`: Modify global directives (StylePrompt, BGMTags)

**Layer C — Render layer:**
- `rerender`: Re-render the Remotion composition from updated props

### Smart Resume

Asset-aware caching. If a pipeline run is interrupted, re-running skips panels whose `image_url` or `audio_url` files already exist on disk. No duplicate API calls, no duplicate costs.

### Human-in-the-Loop

Four HITL checkpoints: `outline`, `storyboard`, `images`, `final`. When a checkpoint is created, the checkpoint ID and approval commands are printed to stderr so you know exactly what to run next.

```
story → [outline ⏸] → [storyboard ⏸] → [images ⏸] → [final ⏸] → mp4
```

```
⏸  HITL checkpoint [stage=outline  id=xxxx-xxxx]
   Approve : shand checkpoint approve xxxx-xxxx
   Reject  : shand checkpoint reject  xxxx-xxxx
```

| Channel | How |
|---|---|
| CLI | `shand checkpoint approve <id>` |
| Discord | Webhook → bot reply |
| HTTP API | `POST :28080/checkpoints/:id/approve` |

### Agent Friendly

Built with AI agents as first-class consumers. Strict input sanitization blocks path traversal (`../../../.ssh`), double-encoding (`%2e%2e`), and control character injection. Non-zero exit codes and structured stderr errors let agents retry predictably.

---

## Quick Start

### Prerequisites

```bash
# Go 1.23+, Node.js 20+, FFmpeg, AWS CLI
brew install awscli ffmpeg node
go build -o shand .
```

### End-to-end run

```bash
echo "機器人找到了一朵會發光的花" | ./shand pipeline --skip-hitl
```

### Resume from existing panels

```bash
cat ~/.shand/projects/my-id/remotion_props.json | ./shand pipeline --skip-hitl
```

### Render only

```bash
cat remotion_props.json | ./shand remotion-render --output ./final.mp4
```

### Run AI Critic

```bash
./shand critic --video ./final.mp4 --props ./remotion_props.json
```

---

## Configuration

Default config path: `~/.shand/config.yaml`. Env vars use `SHAND_` prefix (e.g. `SHAND_LLM_API_KEY`). Flags take highest priority.

```yaml
# Shared AWS credentials used by ALL AWS-backed providers
# (Bedrock LLM, Polly TTS, Nova Canvas/Titan image, Nova Reel video, Stability).
# Old llm.aws_* keys still work for backward compatibility.
aws:
  access_key_id: ""
  secret_access_key: ""
  region: us-east-1

llm:
  provider: openai           # openai | gemini | anthropic | bedrock
  model: ""                  # empty = provider default (gpt-4o / gemini-2.5-pro / etc.)
  api_key: ""
  base_url: ""               # any OpenAI-compatible endpoint; empty = official API
  no_json_mode: false        # set true for servers that don't support response_format:json
  strip_think_tags: false    # set true for reasoning models (Qwen3, QwQ) that emit <think>

image:
  provider: nanobanana        # nanobanana | aiark | bedrock | stability
  api_key: ""
  base_url: ""               # for aiark self-hosted
  model: ""                  # for bedrock: amazon.nova-canvas-* or amazon.titan-image-*
  width: 1024
  height: 576
  region: ""                 # image-specific region override (defaults to aws.region)

audio:
  voice_provider: polly       # polly | aiark
  music_provider: jamendo     # jamendo | aiark
  jamendo_client_id: ""
  # aiark TTS (self-hosted Qwen3-TTS):
  # aiark_tts_base_url: ""
  # aiark_tts_api_key: ""
  # aiark_tts_voice: ""

critic:
  provider: bedrock           # bedrock (default); future: anthropic, gemini
  model: ""                   # empty = amazon.nova-pro-v1:0

video:
  provider: remotion          # remotion | nova_reel | grok_browser | hyperframes
  s3_bucket: ""               # required for nova_reel
  region: ""                  # video-specific region override

remotion:
  template_path: ./remotion-template
  composition: ShortDrama

notify:
  discord_webhook: ${DISCORD_WEBHOOK_URL}

store:
  db_path: ~/.shand/shand.db

server:
  port: 28080
```

---

## Commands Reference

All commands read JSON from stdin and write JSON to stdout unless noted. Use `--dry-run` for any command to validate without calling external APIs.

| Command | Description |
|---|---|
| `shand pipeline` | Full pipeline: story → mp4 |
| `shand story-to-outline` | Story prompt → Outline JSON (LLM) |
| `shand outline-to-storyboard` | Outline JSON → Storyboard JSON (LLM) |
| `shand storyboard-to-panels` | Storyboard JSON → Panel[] JSON (LLM) |
| `shand panel-to-image` | Generate image for a single panel |
| `shand panels-to-images` | Batch image generation (concurrent) |
| `shand storyboard-to-remotion-props` | Panel[] → RemotionProps JSON |
| `shand remotion-render` | Render MP4 via Remotion |
| `shand remotion-preview` | Open Remotion Studio (blocking) |
| `shand critic` | AI Critic multimodal video evaluation |
| `shand checkpoint list` | List all HITL checkpoints |
| `shand checkpoint approve <id>` | Approve a checkpoint |
| `shand checkpoint reject <id>` | Reject a checkpoint |
| `shand checkpoint wait <id>` | Poll until checkpoint resolves |
| `shand status <job-id>` | Query job status |
| `shand character list` | List all registered character reference images |
| `shand character show <name>` | Show character reference details |
| `shand character generate <name>` | Generate + register a reference sheet via image provider |
| `shand character register <name>` | Register an existing image file as character reference |
| `shand postprod evaluate` | Evaluate rendered MP4 with AI Critic |
| `shand postprod apply` | Apply an EditPlan to RemotionProps |
| `shand postprod rerender` | Re-render MP4 from updated props |
| `shand postprod loop` | Autonomous evaluate→fix→rerender loop |

### Key flags

| Flag | Applies to | Effect |
|---|---|---|
| `--dry-run` | All commands | Skip external API calls, return mock JSON |
| `--skip-hitl` | `pipeline` | Disable all 4 HITL pause points |
| `--output <path>` | `remotion-render` | Output MP4 path |
| `--video <path>` | `critic` | Path to rendered MP4 |
| `--props <path>` | `critic` | Path to `remotion_props.json` |
| `--config <path>` | All commands | Override default config file path |
| `--language` | `pipeline` | TTS language code (default: zh-TW) |
| `--max-retries` | `pipeline` | AI Critic auto-retry count (default: 0) |
| `--episodes N` | `pipeline` | Produce N episodes in batch |
| `--batch-concurrency` | `pipeline` | Max concurrent episodes (default: 2) |
| `--max-iterations` | `postprod loop` | Max postprod loop iterations (default: 3) |
| `--faithful` | `pipeline` | LLM only splits original text, no invention |
| `--verbatim` | `pipeline` | Single-pass LLM: segment text verbatim, skip outline/storyboard |
| `--narration` | `pipeline` | Single-pass LLM: rewrite as narrator voice, all speaker: "" |
| `--multi-speaker` | `pipeline` | Per-character voice routing via character registry |
| `--format portrait` | `pipeline` | Vertical 9:16 video (TikTok / Reels / Shorts) |
| `--video-backend` | `pipeline` | Video renderer: remotion \| nova_reel \| grok_browser \| hyperframes |

---

## Architecture

SOLID-based layered architecture. The `cmd/` layer is thin: IO only, no business logic. All external services are accessed through interfaces, injected at construction time.

```
cmd/                   Thin layer: IO + dependency injection
internal/
  domain/              Pure data structs, zero external dependencies
  llm/                 Client interface + factory (openai-compat / bedrock / anthropic / mock)
                       VideoCriticClient interface + NewVideoCriticClient factory
  image/               Client interface + factory (aiark / nanobanana / bedrock / stability / mock)
  audio/               Client interface + factory (polly / aiark-tts / mock)
                       MusicClient interface + factory (jamendo / aiark-music / mock)
                       MultiSpeakerClient interface + factory
  video/               Critic (multimodal evaluation via llm.VideoCriticClient)
  store/               Repository pattern: JobRepo + CheckpointRepo (SQLite/gorm)
  notify/              Notifier interface + Discord webhook
  remotion/            RemotionExecutor interface + exec npx remotion
  character/           Character Registry: persists reference images for cross-panel consistency
  postprod/            Agentic post-production: planner, applier, autonomous loop
  pipeline/            Orchestrator — depends on interfaces only, never on concrete providers
config/                viper loader: flag > env > yaml > defaults
                       aws:   shared credentials for all AWS-backed providers
                       critic: independent model config for video evaluation
remotion-template/     React + Remotion (ShortDrama composition)
```

### Provider swapping

Switching any provider requires only a config change — no code modification:

```yaml
# Switch LLM from local Qwen3 to Anthropic:
llm:
  provider: anthropic
  api_key: sk-ant-...

# Switch image from Nano Banana to aiark:
image:
  provider: aiark
  base_url: http://aiark.internal:8080

# Switch TTS from Polly to aiark:
audio:
  voice_provider: aiark
  aiark_tts_base_url: http://aiark.internal:7860

# Use a different model for video critic than for generation:
critic:
  provider: bedrock
  model: amazon.nova-pro-v1:0
```

### SOLID at a glance

| Principle | Implementation |
|---|---|
| Single Responsibility | Each package owns exactly one domain |
| Open/Closed | New provider = implement interface + add factory case, touch nothing else |
| Liskov Substitution | Every Mock is a drop-in replacement, same behavioral contract |
| Interface Segregation | `LLMClient`, `VideoCriticClient`, `ImageClient`, `AudioBatcher`, `MusicBatcher` are separate |
| Dependency Inversion | `cmd/` depends on interfaces; concrete types injected via constructors |

---

## For AI Agents

`shand` is designed to be controlled by AI agents without human intervention.

```bash
# Full automated run — agent controls everything
echo "太空飛行員愛上了外星植物學家" | ./shand pipeline --skip-hitl

# Agent approves a HITL checkpoint via HTTP
curl -X POST http://localhost:28080/checkpoints/<id>/approve

# Agent reads structured exit codes
./shand pipeline --skip-hitl
echo $?   # 0=success, 1=failed, 2=waiting_hitl

# Agent pipes stages independently
echo "story text" \
  | ./shand story-to-outline \
  | ./shand outline-to-storyboard \
  | ./shand storyboard-to-panels \
  | ./shand panels-to-images \
  | ./shand storyboard-to-remotion-props \
  | ./shand remotion-render --output ./out.mp4
```

**Input hardening:**

All user-supplied strings (IDs, file paths, prompts) pass through `internal/domain` sanitization before use. The pipeline rejects path traversal sequences, double-encoded characters, and control characters. Agents are treated as untrusted sources.

---

## Development Status

| Phase | Status | Deliverables |
|---|---|---|
| Phase 1 | Done | CLI skeleton, viper config, domain types, SQLite/gorm, status/checkpoint |
| Phase 2 | Done | LLM interface, story-to-outline / outline-to-storyboard / storyboard-to-panels |
| Phase 3 | Done | Image interface, panel-to-image / panels-to-images, Discord notify |
| Phase 4 | Done | Remotion template, storyboard-to-remotion-props, render/preview |
| Phase 5 | Done | Pipeline orchestrator, 4-node HITL, end-to-end tests |
| Phase 6 | Done | AWS Bedrock LLM/Image, Amazon Polly Neural TTS + SSML, audio sync |
| Phase 7 | Done | AI Critic (multimodal), Jamendo BGM, subtitle sanitization, dynamic duration |
| Phase 8 | Done | Directives system (StylePrompt / BGMTags), Smart Resume |
| Phase 9 | Done | Multi-language TTS, AI Critic auto-retry, Character Registry, batch production |
| Phase 9.5 | Done | Agentic post-production (postprod evaluate/apply/rerender/loop) |
| Phase 10a | Done | Multi-speaker TTS with per-character voice routing |
| Phase 10b | Done | Vertical video 9:16 format support |
| Phase 10c | Done | Series continuity with sliding window memory |
| Phase 10.0 | Done | Structured `DialogueLine` (prerequisite for multi-speaker) |
| Phase 10.1 | Done | Direct subtitle patching + LLM translation (`--language`) |
| Refactor | Done | Provider decoupling: shared `aws:` config, `NewVideoCriticClient` factory, image multi-format, llm factory reverse-import removed |

---

## License / Credits

MIT License. See [LICENSE](LICENSE).

Built by **Castle Studio**. Developed using a dual-model workflow: Claude (implementation) + Codex (review).

---

*StagentHand — Part of the Castle Studio C3A ecosystem.*
*Binary: `shand` | Module: `github.com/baochen10luo/stagenthand`*
