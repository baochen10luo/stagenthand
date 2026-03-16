# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build -o shand .

# Test all packages
go test ./...

# Test with coverage (target ≥ 80%)
go test -cover ./...

# Run a single test
go test -run TestFunctionName ./internal/store/

# Run pipeline dry-run (no API calls)
echo "一個程序員愛上了咖啡師的故事" | ./shand pipeline --skip-hitl --dry-run

# Validate a single stage
echo '{"title":"test"}' | ./shand story-to-outline --dry-run
```

## Architecture

### Data Flow

```
Raw story (stdin)
  ↓ story-to-outline     (LLM)  → Outline JSON
  ↓ outline-to-storyboard (LLM) → Storyboard JSON (with Directives, Scenes)
  ↓ storyboard-to-panels  (LLM) → []Panel JSON (prompt, dialogue, duration)
  ↓ panels-to-images  (ImageClient) → []Panel with image_url
  ↓ TTS (AudioBatcher) + BGM (MusicBatcher)
  ↓ storyboard-to-remotion-props → RemotionProps JSON
  ↓ remotion-render (exec npx) → mp4
```

The `Orchestrator` (`internal/pipeline/orchestrator.go`) auto-detects input format — raw story text, Outline JSON, Storyboard JSON, or a flat RemotionProps — and routes to the correct starting stage.

### Layer Rules

- **`cmd/`** — thin cobra wrappers only: read stdin, inject deps, call internal packages, write stdout JSON. No business logic.
- **`internal/domain/`** — pure data structs, zero external deps. Never add methods with side effects here.
- **`internal/pipeline/`** — orchestration only. Depends on interfaces (`Transformer`, `ImageBatcher`, `AudioBatcher`, `MusicBatcher`, `CheckpointGate`), never on concrete providers.
- **`internal/llm/`, `image/`, `audio/`, `video/`** — provider implementations. Each has a `Client` interface + concrete impl(s) + `mock.go`.
- **`internal/store/`** — SQLite via GORM. `JobRepository` + `CheckpointRepository` interfaces with `GormXxx` impls and in-memory mocks.
- **`internal/server/`** — Gin HTTP server on `:28080` for checkpoint approve/reject (agent/Discord bot entry point).

### Key Interfaces

| Interface | Defined in | Purpose |
|---|---|---|
| `llm.Client` | `internal/llm/client.go` | `GenerateTransformation(ctx, systemPrompt, input) ([]byte, error)` |
| `llm.VideoCriticClient` | `internal/llm/client.go` | Multi-modal video review |
| `image.Client` | `internal/image/client.go` | `GenerateImage(ctx, prompt, refs) ([]byte, error)` |
| `audio.Client` | `internal/audio/client.go` | TTS (Polly) |
| `audio.MusicClient` | `internal/audio/client.go` | BGM (Jamendo) |
| `pipeline.Transformer` | `internal/pipeline/stages.go` | Wraps `llm.Client` for orchestrator |
| `pipeline.ImageBatcher` | `internal/pipeline/orchestrator.go` | Batch image gen adapter |
| `store.CheckpointRepository` | `internal/store/checkpoint.go` | HITL checkpoint persistence |

### HITL Checkpoints

Four pause points: `outline → storyboard → images → final`. At each, `CheckpointGateAdapter.CreateAndWait()` writes a DB record and polls every 5s. Approval via:
- CLI: `shand checkpoint approve <id>`
- HTTP: `POST /checkpoints/:id/approve` (Gin server)
- Bypass: `--skip-hitl` flag

### Smart Resume (Phase 8)

`ImageClientBatcher` and `AudioClientBatcher` skip generation if the target file already exists and is non-empty (`projects/<id>/images/scene_X_panel_Y.png`). This means reruns only call paid APIs for missing assets.

### Adapters Pattern

`internal/pipeline/adapters.go` provides three adapter structs (`ImageClientBatcher`, `AudioClientBatcher`, `MusicClientBatcher`) that wrap provider clients to implement the batcher interfaces. This keeps `Orchestrator` decoupled from provider specifics.

### Provider Factories

- `llm.NewClient(provider, dryRun, cfg)` — returns `mock` in dry-run; routes to `openai-compat`, `gemini`, `bedrock`, or `nova`
- `image.NewClient(provider, dryRun, cfg)` — routes to `nanobanana`, `nova`, or `mock`

### Configuration

Priority: CLI flag > `SHAND_*` env vars > `~/.shand/config.yaml` > defaults.

Key defaults: `llm.provider=openai`, `image.provider=nanobanana`, `store.db_path=~/.shand/shand.db`, `server.port=28080`.

### Directives System (Phase 8)

`Storyboard.Directives` holds global render config (`StylePrompt`, `BGMTags`, `ColorFilter`, audio ducking params). `Panel.Directive` holds per-panel overrides (`MotionEffect`, `TransitionIn/Out`, `SubtitleEffect`). The orchestrator prepends `StylePrompt` to every panel's `Description` before image generation.

### IO Contract

- **stdout**: pure JSON only
- **stderr**: all logs (`slog`, enabled by `--verbose`)
- **exit codes**: `0` = success, `1` = failure, `2` = waiting HITL

### TDD Rules

Tests are written before implementation. Every interface has a `mock.go` in its own package (not in `*_test.go` files). Use table-driven tests. No real API calls in tests.

## Current Phase

**Phase 10.1 complete.** All phases through 10.1 are shipped:
- Phase 9: multi-language TTS, character registry, AI Critic auto-retry, batch multi-episode
- Phase 10a: multi-speaker TTS with per-character voice routing
- Phase 10b: vertical video 9:16 format
- Phase 10c: series continuity with sliding window memory
- Phase 10.0: structured `DialogueLine`
- Phase 10.1: direct subtitle patching + LLM translation

See `DEVELOPMENT_PLAN.md` for full spec and `AGENTS.md` for anti-patterns and naming conventions.
