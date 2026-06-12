# AGENTS.md — shand (StagentHand)

AI short drama pipeline: Go CLI that defaults to xAI OAuth video generation with HyperFrames timeline rendering and FFmpeg/ffprobe finalization; legacy image/TTS/Remotion paths remain explicit backends.

## Skills

Workflow knowledge lives in `.agents/skills/` (symlinked to `.claude/skills/`). Load `aiark-pipeline` before any pipeline work; load `remotion` before Remotion/video work.

## Commands

```bash
go build -o shand .                     # build binary
go test ./...                           # all tests
go test -cover ./...                    # coverage (CI threshold ≥ 70%)
go test -run TestFunctionName ./internal/store/  # single test
go vet ./...                            # required before tests in CI
golangci-lint run                       # linter config in .golangci.yml (errcheck, govet, staticcheck, unused, ineffassign, gofmt)

# Pipeline dry-run (no API calls):
echo "一個程序員愛上了咖啡師的故事" | ./shand pipeline --skip-hitl --dry-run

# Validate single stage:
echo '{"title":"test"}' | ./shand story-to-outline --dry-run

# CI smoke test:
cat test_storyboard.json | ./shand storyboard-to-remotion-props | ./shand remotion-render --output /tmp/test.mp4 --dry-run

# xAI-native production validation:
./shand xai inspect --strict outputs/story-a
./shand xai validate outputs/story-a
OUT_DIR=outputs/story-a scripts/validate-xai-native-live.sh
scripts/smoke-xai-live-validation-gate.sh

# Lint: golangci-lint v1.64.5 (CI pins this version)
```

## Git push (multiple SSH keys)

```bash
GIT_SSH_COMMAND="ssh -i ~/.ssh/github-baochen10luo -o IdentitiesOnly=yes" git push
git config core.sshCommand "ssh -i ~/.ssh/github-baochen10luo -o IdentitiesOnly=yes"
```

## Architecture

### Data flow

```
stdin → [xAI OAuth shot plan] → [xAI OAuth video shots]
  → [HyperFrames timeline] → [FFmpeg finalization] → mp4
```

The default pipeline is xAI OAuth first: LLM uses `llm.provider: xai-oauth`,
video uses `video.provider: xai_oauth` (`xai-oauth` is accepted as an alias), and image/TTS/BGM/Remotion static
rendering are skipped unless a non-xAI video backend is selected.
For the xAI-native path, HyperFrames owns timeline rendering and FFmpeg/ffprobe
owns normalization, silent finalization, validation metadata, and preview
extraction. Do not add Remotion, browser automation, or FFmpeg-only concat as an
implicit fallback for this path.
Use `shand xai validate <output-dir>` for production validation; it requires a
complete xAI-native artifact set, FFmpeg/ffprobe-compatible final MP4, silent
output, and per-shot `xai_request_id` / `xai_status=done` metadata.
The rewrite plan for a dedicated xAI-native pipeline is documented in
`docs/xai-native-pipeline-plan.md`.

`Orchestrator` (`internal/pipeline/orchestrator.go`) auto-detects input: raw text, Outline JSON, Storyboard JSON, or RemotionProps — routes to correct starting stage.

### Pipeline modes

| Flag | Behavior |
|---|---|
| `--verbatim` | Single-pass: segment text exactly, skip outline/storyboard |
| `--narration` | Single-pass: rewrite as narrator voice, all `speaker: ""` |
| `--faithful` | LLM only splits original text, no invention |
| `--multi-speaker` | Per-character voice routing via character registry |
| `--format portrait` | Vertical 9:16 (TikTok/Reels); default |
| `--panels N` | In xAI-native mode, maps one-to-one to exactly N xAI video shots |
| `--episodes N` | Batch production; `--batch-concurrency` (default 2) |
| `--max-retries N` | AI Critic auto-retry on REJECT |
| `--video-backend` | `xai_oauth` (default; `xai-oauth` alias) or `remotion` / `nova_reel` / `grok_browser` (deprecated) / `hyperframes` |

### Layer rules

- `cmd/` — thin cobra wrappers: stdin IO, dep injection, call internals, write stdout. No biz logic.
- `internal/domain/` — pure data structs, zero external deps. No side-effect methods.
- `internal/pipeline/` — orchestration only. Depends on interfaces, never concrete providers.
- `internal/{llm,image,audio,video,store,server,character,postprod,series,pronunciation,hyperframes,notify}/` — each has a `Client`/`Repository` interface + concrete impl(s) + `mock.go` (not in `_test.go`).
- `config/` — viper loader. Priority: flag > `SHAND_*` env > `~/.shand/config.yaml` > defaults. Auto-loads `.env` then `~/.shand/.env`.
- `remotion-template/` — React + Remotion (composition `ShortDrama`). TS typecheck in CI via `npx tsc --noEmit`.

### IO Contract

- **stdout**: pure JSON only
- **stderr**: logs (slog, `--verbose`) + structured JSON errors
- **exit codes**: `0` success, `1` failure, `2` waiting HITL

### HITL Checkpoints

Four pause points: `outline → storyboard → images → final` (+ `series_summary`). Approval via:
- CLI: `shand checkpoint approve <id>`
- HTTP: `POST :28080/checkpoints/:id/approve`
- Bypass: `--skip-hitl`

Polls DB every 5s. Exit code `2` while waiting.

### Smart Resume

Asset-aware caching: skips image/audio generation if `projects/<id>/images/scene_X_panel_Y.png` exists and is non-empty. Reruns only call paid APIs for missing assets. Format detected by magic bytes (PNG/JPEG/WebP) in `internal/pipeline/adapters.go`.

### Config highlights

- **Env prefix**: `SHAND_` (e.g. `SHAND_LLM_PROVIDER`, `SHAND_IMAGE_API_KEY`)
- **Shared AWS creds**: `aws.access_key_id` etc. — used by ALL AWS-backed providers (Bedrock, Polly, Nova, Titan, Stability)
- **Old `llm.aws_*` keys** backfilled into `aws.*` for backward compat

## CI workflow (`.github/workflows/ci.yml`)

Order: `go vet ./...` → `go test -coverprofile=coverage.out ./...` (≥70%) → `go build` → smoke test (`storyboard-to-remotion-props | remotion-render --dry-run`)

Also: `golangci-lint` in parallel, `npx tsc --noEmit` in `remotion-template/`. Go version 1.23, Node 20.

## Testing conventions

- Every interface has a hand-crafted `mock.go` in the same package (not in `_test.go`). Mocks expose function fields (e.g. `GenerateFunc`) and a `CallCount`.
- Table-driven tests with `testify`. No real API calls in tests.
- Package-level tests use `package foo_test` (external test package).
- `internal/store/mock.go` has in-memory `MockJobRepository` / `MockCheckpointRepository`.

## Key unique packages

| Package | Purpose |
|---|---|
| `internal/character` | Persistent reference image store under `~/.shand/characters/<name>/ref.png` |
| `internal/pronunciation` | Dictionary + homophone map + STT audit for subtitle accuracy |
| `internal/series` | Sliding-window series continuity memory across episodes |
| `internal/hyperframes` | I2V (image-to-video) via browser-based template |
| `internal/postprod` | Agentic post-production: evaluate → plan → apply → rerender loop |
| `internal/render` | `VideoFormat` (landscape 16:9 / portrait 9:16) dimension helpers |
| `internal/clipboardbridge` | Clipboard bridge for aiark server integration |
| `internal/notify` | `Notifier` interface + Discord webhook (exponential backoff) |

## Naming & style

- `Client` interfaces in provider packages; consumer defines its own interface per DIP (e.g. `pipeline.Transformer`)
- `ClientWithExt` in `audio/` for providers that output non-MP3 formats
- Go 1.25. Use `slog` not `log`.
