# xAI Native Pipeline Plan

## Canonical Plan

This is the current source of truth for the xAI-native pipeline as of
2026-06-12. Detailed implementation notes and historical TDD logs below this
section are supporting context; if they conflict with this section, this section
wins.

Build the default xAI path as a dedicated model-native video pipeline, not as
the legacy image/TTS/Remotion pipeline with xAI swapped in.

New render decision: the xAI pipeline uses HyperFrames and FFmpeg/ffprobe
together as the render tooling. HyperFrames is the only default timeline
composer for xAI-native output. FFmpeg/ffprobe is the only default media
conditioning, final packaging, metadata validation, and preview extraction
tooling. They are required together; neither tool is a fallback for the other.

This replaces the unstable browser-generation approach for the default xAI
path. Browser automation, Remotion rendering, and FFmpeg-only concat remain
explicit legacy or experimental routes only; they must not be reached from the
default `xai_oauth` / `xai-oauth` pipeline.

```text
story input
  -> xAI OAuth LLM writes xai_manifest.json
  -> xAI OAuth video writes shots/shot_NNN.mp4
  -> FFmpeg normalizes raw shots into normalized/shot_NNN.mp4
  -> HyperFrames renders normalized shots into timeline_hyperframes.mp4
  -> FFmpeg finalizes timeline_hyperframes.mp4 into output_xai.mp4
  -> ffprobe validates output_xai.mp4 and writes render_metadata.json
  -> FFmpeg extracts preview_frame.jpg
```

Current verified status, 2026-06-12:

- A real xAI OAuth 1-shot production smoke completed end to end at
  `outputs/xai-native-live-now-1shot/output_xai.mp4`.
- The run reused the already-generated raw xAI shot and completed the render
  stack through FFmpeg normalization, HyperFrames timeline render, FFmpeg final
  packaging, ffprobe validation, and preview extraction.
- `shand xai validate outputs/xai-native-live-now-1shot` reports `valid`:
  portrait 720x1280, 24fps, H.264, yuv420p, 8 seconds, silent MP4.
- The FFmpeg muxer failure caused by staged media paths ending in `.tmp` has
  been fixed by preserving media extensions on staged outputs, such as
  `.tmp.mp4` and `.tmp.jpg`.

## Planning Update: xAI + HyperFrames/FFmpeg

The xAI-native pipeline plan is now a two-layer system: xAI OAuth is the model
layer, and HyperFrames plus FFmpeg/ffprobe is the render layer. This is not a
browser-generation plan, not a Remotion compatibility plan, and not an
FFmpeg-only concat plan.

Planning record, 2026-06-12:

- xAI OAuth models own all creative/model output: planning, prompts, and raw
  per-shot video generation.
- HyperFrames and FFmpeg/ffprobe are paired render infrastructure: HyperFrames
  owns timeline composition; FFmpeg/ffprobe owns media normalization, final
  packaging, validation, and preview extraction.
- The default `xai_oauth` / `xai-oauth` route must not enter browser
  automation, Remotion rendering, local model inference, image/TTS/BGM stages,
  or FFmpeg-only concat.
- Render succeeds only when the HyperFrames timeline and the FFmpeg/ffprobe
  final bundle both validate against the same `xai_manifest.json`.

Layer contract:

| Layer | Owner | Contract |
|---|---|---|
| Model layer | xAI OAuth LLM + xAI OAuth video | Produce `xai_manifest.json` and raw `shots/shot_NNN.mp4` files only |
| Timeline layer | HyperFrames | Compose normalized xAI shots into `timeline_hyperframes.mp4` using manifest timing, subtitles, overlays, and transitions |
| Media layer | FFmpeg / ffprobe | Normalize shots, finalize the HyperFrames timeline, validate media metadata, and extract preview frames |

Render tool split:

| Step | Tool | Required input | Canonical output |
|---|---|---|---|
| Normalize raw xAI shots | FFmpeg | `shots/shot_NNN.mp4` | `normalized/shot_NNN.mp4` |
| Compose timeline | HyperFrames | normalized shots + `xai_manifest.json` timing/specs | `timeline_hyperframes.mp4` |
| Finalize MP4 | FFmpeg | `timeline_hyperframes.mp4` | `output_xai.mp4` |
| Validate final media | ffprobe | `output_xai.mp4` | `render_metadata.json` |
| Extract production preview | FFmpeg | `output_xai.mp4` | `preview_frame.jpg` |

The renderer is considered successful only when every canonical output above
exists and belongs to the same manifest. A partial render is debug evidence, not
production output.

Decision:

- xAI OAuth LLM owns planning and manifest generation.
- xAI OAuth video owns per-shot MP4 generation.
- FFmpeg owns media normalization before timeline composition.
- HyperFrames owns timeline composition.
- FFmpeg/ffprobe owns final packaging, metadata validation, and preview
  extraction.

Execution order:

1. Persist the xAI source of truth in `xai_manifest.json`.
2. Generate or reuse raw xAI shot videos only at `shots/shot_NNN.mp4`.
3. Normalize every raw shot through FFmpeg into `normalized/shot_NNN.mp4`.
4. Build the HyperFrames project from normalized shots, manifest timing,
   subtitles, overlays, and transitions.
5. Render `timeline_hyperframes.mp4` through HyperFrames.
6. Finalize that timeline through FFmpeg into `output_xai.mp4`.
7. Validate the exact final MP4 through `ffprobe`, write `render_metadata.json`,
   and extract `preview_frame.jpg`.

Implementation plan:

1. Keep this path isolated in `internal/xaipipeline`; do not route it through
   the legacy image/TTS/BGM/Remotion orchestrator.
2. Keep small injected interfaces for planner, video generator, shot validator,
   FFmpeg normalizer, HyperFrames renderer, FFmpeg finalizer, metadata validator,
   and preview extractor.
3. Add tests that prove the default `xai_oauth` and `xai-oauth` routes never
   call local models, image generation, TTS, BGM, Remotion, Grok browser
   automation, or FFmpeg-only concat.
4. Add renderer tests that prove raw shots cannot reach HyperFrames before
   FFmpeg normalization, and that `output_xai.mp4` cannot be produced without a
   successful HyperFrames `timeline_hyperframes.mp4`.
5. Keep dry-run no-network but production-shaped: it must write the same
   manifest, raw shot, normalized shot, HyperFrames, final MP4, metadata, and
   preview artifact layout.
6. Keep live production validation opt-in through Hermes `xai-oauth`; live
   tests use xAI models only and must not run local models.

Renderer acceptance plan:

1. Prove FFmpeg normalization always runs before HyperFrames can read a shot.
2. Prove HyperFrames always writes the timeline artifact before FFmpeg
   finalization can start.
3. Prove FFmpeg finalization always consumes `timeline_hyperframes.mp4`, never
   raw `shots/` or `normalized/` inputs directly.
4. Prove `ffprobe` validation and preview extraction are part of the same final
   render bundle as `output_xai.mp4`.
5. Prove browser automation, Remotion, and FFmpeg-only concat are unreachable in
   the default xAI-native route.

## HyperFrames + FFmpeg Render Plan

Decision record, 2026-06-12: the xAI-native pipeline renders through a paired
HyperFrames and FFmpeg/ffprobe stack. HyperFrames is the timeline compositor.
FFmpeg/ffprobe is the media processor. The two are required together for the
default `xai_oauth` / `xai-oauth` path.

This means the render stage is a coordinator, not a model stage:

- xAI OAuth creates the manifest and one raw MP4 per shot.
- FFmpeg normalizes every raw xAI MP4 into the exact render target before any
  timeline work starts.
- HyperFrames builds and renders the timeline from normalized MP4 files only.
- FFmpeg finalizes the HyperFrames timeline into the production MP4.
- ffprobe validates the final MP4 and records metadata before the preview frame
  is accepted.

Implementation slices:

1. Define a small render coordinator interface in `internal/xaipipeline` that
   depends on `ShotNormalizer`, `TimelineRenderer`, `Finalizer`,
   `MetadataValidator`, and `PreviewExtractor`.
2. Keep the HyperFrames implementation responsible only for project generation
   and `timeline_hyperframes.mp4`; it must not normalize media, finalize MP4s,
   call xAI, or inspect legacy Remotion props.
3. Keep the FFmpeg implementation responsible only for deterministic media
   work: normalize raw shots, finalize the timeline, strip audio, enforce H.264
   `yuv420p`, write faststart output, and extract preview JPEGs.
4. Keep ffprobe validation as a required gate before metadata is persisted.
5. Keep dry-run fixtures production-shaped: raw shot, normalized shot,
   HyperFrames timeline, final MP4, metadata, and preview must all exist even
   when no xAI or real renderer process is called.

TDD order for this render plan:

1. Route test: `xai_oauth` selects the xAI-native coordinator and never reaches
   browser, Remotion, local-model, image, TTS, BGM, or FFmpeg-only concat paths.
2. Normalization test: HyperFrames receives only `normalized/shot_NNN.mp4`
   paths, never raw `shots/shot_NNN.mp4`.
3. Timeline test: FFmpeg finalization cannot start unless
   `timeline_hyperframes.mp4` exists and validates.
4. Final bundle test: `output_xai.mp4`, `render_metadata.json`, and
   `preview_frame.jpg` are accepted or rejected as one manifest-bound bundle.
5. Live-gate test: production validation is opt-in through Hermes
   `xai-oauth`, uses xAI models only, and still renders through
   HyperFrames plus FFmpeg/ffprobe.

## Boundaries

- Model boundary: xAI OAuth only. xAI plans the story, writes the shot
  manifest, and generates one raw MP4 per shot.
- Render boundary: HyperFrames plus FFmpeg/ffprobe only. HyperFrames composes
  the timeline; FFmpeg/ffprobe normalizes, finalizes, validates, and extracts
  preview media.
- Source-of-truth boundary: `xai_manifest.json` owns shot order, duration,
  prompts, prompt hashes, video model identity, subtitles, transitions, FPS,
  dimensions, and xAI request metadata.

HyperFrames and FFmpeg are render tools, not model providers. They must never
rewrite story content, invent prompts, call local models, or substitute missing
xAI output.

## Render Contract

The render stack is locked to HyperFrames plus FFmpeg:

- Raw xAI shots are committed only as output-local `shots/shot_NNN.mp4`.
- FFmpeg normalization is mandatory before HyperFrames and writes
  `normalized/shot_NNN.mp4`.
- HyperFrames is mandatory for timeline composition and writes
  `hyperframes/index.html`, `hyperframes/package.json`, and
  `timeline_hyperframes.mp4`.
- FFmpeg finalization must consume `timeline_hyperframes.mp4`, not raw shots or
  normalized shots directly.
- `ffprobe` validation must pass before `render_metadata.json` is accepted.
- HyperFrames cannot be bypassed with FFmpeg-only concat, and FFmpeg
  finalization/validation cannot be skipped after HyperFrames succeeds.
- `output_xai.mp4`, `render_metadata.json`, and `preview_frame.jpg` are one
  consistent final render bundle.
- Generated HyperFrames HTML is render evidence, not an authoring source of
  truth.

First stable output target:

```text
portrait, 720x1280, 24fps, H.264, yuv420p, silent final MP4
```

Audio is out of scope for the first stable xAI-native path. Any future audio
support must be an explicit stage with manifest fields, tests, mux policy, and
validation rules; it must not leak in through raw xAI shot audio.

## Forbidden Default Fallbacks

The default xAI-native path must fail clearly instead of silently falling back
to:

- local model inference
- legacy image generation, TTS, or BGM
- Remotion rendering
- Grok browser automation
- FFmpeg-only concat when HyperFrames fails
- `remotion_props.json` as a compatibility source of truth

Callers that want legacy Remotion, Nova Reel, browser automation, or other
renderers must select those backends explicitly.

## Implementation Roadmap

1. Keep orchestration in `internal/xaipipeline` behind small interfaces:
   planner, shot generator, shot validator, normalizer, HyperFrames renderer,
   finalizer, metadata validator, preview extractor, inspect, and validate.
2. Route `video.provider: xai_oauth` and `video.provider: xai-oauth` to this
   xAI-native pipeline by default.
3. Generate or resume canonical raw shot files only at `shots/shot_NNN.mp4`.
4. Normalize every raw shot to `normalized/shot_NNN.mp4` before HyperFrames can
   see it.
5. Render the xAI timeline only through HyperFrames.
6. Finalize only from `timeline_hyperframes.mp4` to `output_xai.mp4` through
   FFmpeg.
7. Stage and commit `output_xai.mp4`, `render_metadata.json`, and
   `preview_frame.jpg` as one consistent final render bundle.
8. Keep dry-run production-shaped: no network calls, but the same raw shot,
   normalized shot, HyperFrames, final MP4, metadata, and preview artifact
   layout.
9. Keep renderer dependencies injectable so tests can prove the HyperFrames
   timeline step and FFmpeg finalization step both run in order.

## TDD Checkpoints

- Route tests prove default xAI runs do not initialize or call legacy local
  model, image, TTS, BGM, Remotion, browser, or FFmpeg-only concat paths.
- Planner tests prove xAI OAuth owns the manifest and honors requested shot
  count, portrait format, 720x1280, 24fps, and safe project ids.
- Shot-generation tests prove raw shot reuse requires canonical path, MP4 magic,
  matching `prompt_hash`, xAI request/status metadata, and validator-approved
  media evidence.
- Renderer tests prove normalization is mandatory before HyperFrames.
- Renderer tests prove HyperFrames receives manifest FPS, dimensions, shot
  order, normalized paths, timing, subtitles, overlays, and transitions.
- Renderer tests prove `timeline_hyperframes.mp4` is mandatory before FFmpeg
  finalization.
- Validation tests prove final output is decodable, silent, portrait,
  720x1280, 24fps, H.264, yuv420p, and backed by xAI request metadata.
- Smoke tests prove dry-run emits the production artifact shape without xAI
  network calls or local model calls.
- Live validation remains opt-in and requires Hermes `xai-oauth` credentials.

## Non-Goals

- Do not remove the legacy Remotion pipeline yet.
- Do not make aiark image, aiark TTS, Jamendo, Polly, or local model services part of the xAI default path.
- Do not depend on xAI API-key config for this path; use Hermes xAI OAuth credentials from `~/.hermes/auth.json`.
- Do not use Remotion static rendering as fallback inside the xAI pipeline. Fail clearly if xAI video generation fails.

## Proposed CLI Behavior

`shand pipeline` remains the main command, but routes internally:

| Backend | Pipeline |
|---|---|
| `xai_oauth` / `xai-oauth` | New xAI-native pipeline, default; `xai-oauth` is accepted as a config/flag alias |
| `remotion` | Legacy image/TTS/BGM/Remotion pipeline |
| `hyperframes` | Legacy panels/assets rendered by HyperFrames |
| `nova_reel` | Legacy I2V provider path |
| `grok_browser` | Deprecated browser automation path |

Recommended explicit command:

```bash
echo "故事主題" | ./shand pipeline --skip-hitl --panels 3
```

Resume and override controls:

```bash
# Reuse matching xai_manifest.json and valid cached shots when possible.
echo "故事主題" | ./shand pipeline --skip-hitl --panels 3 --output-dir outputs/story-a

# Ignore a matching manifest and call xAI planning again; unchanged shot prompts may still reuse cached videos.
echo "故事主題" | ./shand pipeline --skip-hitl --panels 3 --output-dir outputs/story-a --force-replan

# Keep a matching story manifest but regenerate xAI shot videos; a changed
# video.model updates video_model and prompt_hash without re-running planning.
echo "故事主題" | ./shand pipeline --skip-hitl --panels 3 --output-dir outputs/story-a --force-regenerate
```

Effective defaults:

```yaml
llm:
  provider: xai-oauth

video:
  provider: xai_oauth # xai-oauth alias accepted
  model: grok-imagine-video

image:
  provider: mock

audio:
  voice_provider: mock
  music_provider: mock
```

## Current Implementation Status

Initial TDD slice is implemented:

- `internal/xaipipeline` owns the xAI-native manifest, planner, orchestrator, shot generator, cache validator, and render orchestration.
- `shand pipeline` routes the default single-episode `xai_oauth` path to the new xAI-native pipeline.
- The renderer normalizes raw xAI shots into `normalized/`, writes an xAI-specific HyperFrames video timeline project (`hyperframes/index.html` plus `hyperframes/package.json`), and finalizes it through FFmpeg.
- The renderer rejects unsupported xAI-native render specs before normalization: explicit manifest values must stay on the first stable portrait target, 720x1280 at 24fps; omitted values still default to that target.
- The renderer rejects non-canonical explicit manifest format values before normalization; values such as `" portrait "` must be normalized before reaching the HyperFrames/FFmpeg render boundary.
- The renderer rejects non-positive manifest `duration_sec` values before normalization; HyperFrames timeline duration is no longer allowed to silently fall back from stale zero-duration source-of-truth data.
- The renderer rejects explicit per-shot `aspect_ratio` / `resolution` values outside the first stable xAI video target, including whitespace-padded values such as `" 9:16 "` or `" 720p "`, before FFmpeg normalization or HyperFrames execution.
- Production validation rejects non-canonical manifest format values with the same source-of-truth rule; stale manifests containing values such as `" portrait "` no longer pass as merely equivalent to `portrait`.
- Production validation rejects non-canonical manifest `aspect_ratio` / `resolution` values with the same source-of-truth rule; stale manifests containing values such as `" 9:16 "` or `" 720p "` now fail with canonical contract issues.
- The renderer rejects non-canonical manifest prompts before normalization; values such as `" shot "` must be normalized before any media work starts, even though prompts are not consumed by HyperFrames.
- The renderer rejects non-canonical manifest subtitles before normalization; values such as `" 第一個字幕 "` must be normalized before HyperFrames subtitle clips are generated.
- The renderer rejects unsafe manifest `project_id` values before normalization; path traversal or non-single-component ids cannot reach HyperFrames project generation.
- The renderer rejects duplicate, non-contiguous, or out-of-order manifest shot indexes before normalization, preventing ambiguous `shot_NNN.mp4` writes or reordered HyperFrames timelines.
- The renderer rejects missing or non-canonical manifest `video_path` values before creating `normalized/` or `hyperframes/`; raw xAI shots must already be declared as `shots/shot_NNN.mp4`.
- The renderer treats the incoming manifest as immutable source-of-truth data: FFmpeg normalization may rewrite only the renderer-local manifest copy used for HyperFrames, never the caller's manifest or `Result.Manifest`.
- Production validation rejects non-canonical manifest `video_path` values with the same source-of-truth rule; stale manifests containing values such as `" shots/shot_001.mp4 "` now fail with canonical contract issues.
- Production validation rejects non-canonical manifest `prompt_hash` values before comparing them with the deterministic shot hash; manifest prompt hashes must be trimmed 64-character lowercase SHA-256 hex strings.
- Production validation rejects non-canonical run metadata `shot_decisions[].video_path` values with the same rule; stale `xai_run_metadata.json` decisions containing values such as `" shots/shot_001.mp4 "` now fail before being treated as ordinary manifest mismatches.
- Production validation rejects non-canonical run metadata `shot_decisions[].prompt_hash` values before matching the manifest; prompt hashes must be trimmed 64-character lowercase SHA-256 hex strings.
- The renderer also rejects non-canonical per-shot transition metadata before normalization; non-empty `transition_out` values must already be `cut` or `fade` when HyperFrames rendering starts.
- xAI-native FFmpeg/ffprobe tooling is resolved through one renderer helper: system `ffmpeg`/`ffprobe` is preferred, with `bunx remotion ffmpeg` / `bunx remotion ffprobe` as the fallback command path.
- The renderer writes manifest identity into `render_metadata.json`: the source `project_id` plus a canonical SHA-256 `manifest_hash` of the xAI source-of-truth manifest used for rendering.
- Production validation recomputes that manifest identity and rejects persisted `render_metadata.json` files whose `project_id` or `manifest_hash` belong to a different manifest.
- Production validation reports every persisted `render_metadata.json` contract problem it can prove in one pass, so stale final-bundle evidence can surface identity, path, render-spec, and file-size mismatches together.
- The no-network xAI-native dry-run smoke checks the same render metadata identity in both single-output and batch episode outputs: metadata `project_id` must match the manifest and `manifest_hash` must be canonical lowercase SHA-256 evidence.
- Resume is prompt/model-aware: a cached shot is reused only when the previous
  manifest already persists the same `video_model` plus a `prompt_hash` that
  matches the current shot plan and `ffprobe` can decode the MP4.
- Whole-manifest resume skips planning only when every raw shot has reusable
  evidence: canonical `video_path`, persisted matching `video_model` and
  `prompt_hash`, reusable xAI request/status metadata, and validator-approved
  `shots/shot_NNN.mp4`.
- Production validation requires persisted manifest prompts to remain in canonical trimmed form and rejects empty shot prompts, matching the xAI-native orchestration boundary before shot generation.
- The xAI-native planner is pinned to `xai-oauth`; legacy `llm.provider`, `llm.model`, and `llm.base_url` do not override this path, and xAI planner `model` / `base_url` values are trimmed with blank values falling back to xAI defaults.
- The xAI-native LLM planner trims story input and canonicalizes requested format before calling the transformer; format is trimmed, lowercased, blank values default to `portrait`, and empty stories, negative target-shot counts, or unsupported formats are rejected before any xAI LLM request.
- The xAI-native LLM planner normalizes nil contexts, rejects canceled contexts
  before calling the transformer, and rechecks cancellation after the
  transformer returns before parsing a manifest, so canceled planning cannot
  accept stale xAI LLM bytes even if the downstream transformer ignores context.
- Planner-returned shot subtitles are trimmed at the xAI-native orchestration boundary before xAI video generation, manifest persistence, and HyperFrames rendering.
- Production validation requires persisted manifest subtitles to remain in that same canonical trimmed form; stale manifests with surrounding subtitle whitespace no longer pass as valid output evidence.
- The Hermes xAI OAuth token source normalizes nil contexts, rejects already-canceled contexts before token file reads, and rechecks cancellation after refresh responses return and again after response bodies are read, before parsing or saving refreshed tokens; cached-token and refresh-token paths share the same context contract.
- The xAI OAuth LLM client normalizes nil contexts, normalizes configured API roots, trims request `model` and OAuth bearer token values, rejects empty bearer tokens before provider requests, preflights canceled contexts before OAuth token retrieval or `/v1/responses` requests, and rejects empty response content after think-tag/markdown-fence cleanup.
- The xAI-native video model defaults to `grok-imagine-video`; trimmed `video.model` is honored only when `video.provider` is `xai_oauth` / `xai-oauth`, and blank model values fall back to the default.
- The selected xAI video model is persisted as manifest/run metadata identity:
  `video_model` is part of the shot generation cache hash, so changing
  `video.model` regenerates cached raw shots instead of reusing stale MP4s from
  a different model.
- Production validation recomputes `prompt_hash` with manifest `video_model`
  and rejects run metadata whose persisted `video_model` disagrees with the
  manifest.
- Production validation requires both `xai_manifest.json` and
  `xai_run_metadata.json` to persist `video_model`; missing model identity is
  invalid production evidence.
- The xAI OAuth video client normalizes nil contexts, normalizes configured API roots to `/v1`, trims request `model`, `prompt`, `image.url`, and OAuth bearer token values, rejects empty prompts before auth/provider requests, rejects empty bearer tokens before provider requests, trims returned `request_id` before polling `/videos/{request_id}`, trims returned `video.url` before download, and canonicalizes completed video status to `done`.
- Per-shot manifest fields `duration_sec`, `aspect_ratio`, and `resolution` are forwarded to the xAI video generation request.
- Production validation requires persisted manifest `duration_sec` values to be positive; stale manifests that rely on runtime defaulting for zero or negative durations are invalid source-of-truth evidence.
- The xAI-native video shot generator trims shot prompts, rejects empty prompts, and normalizes duration/aspect-ratio/resolution generation options before calling the xAI video client.
- The xAI-native video shot generator normalizes nil contexts, rejects canceled
  contexts before calling the xAI video client, and rechecks cancellation after
  the client returns before accepting video bytes or provider metadata, so
  canceled shot generation cannot turn stale provider output into manifest
  evidence.
- The xAI-native video shot generator trims returned provider request IDs and canonicalizes returned provider statuses before handing metadata to orchestration.
- Per-shot xAI provider metadata fields `xai_request_id` and `xai_status` are trimmed, canonicalized, and persisted only when the status is reusable (`done` for live xAI output, `dry_run` for deterministic no-network fixtures).
- Production validation requires persisted `xai_request_id` and `xai_status` values in both manifest and run metadata to remain canonical; stale whitespace-padded request IDs or status values such as `" Done "` are invalid output evidence.
- `--panels N` maps one-to-one to exactly N xAI video shots in the xAI-native path; planner mismatch is rejected before generation.
- The core xAI-native orchestrator rejects negative target-shot counts before planning, generation, or rendering, so the `--panels N` invariant is enforced below the CLI wrapper as well.
- When `--output-dir` is omitted, outputs go under `~/.shand/projects/<project_id>` after the xAI manifest has been planned, but the default output base `~/.shand/projects` is preflighted before planning.
- The CLI summary reports artifact paths for `xai_manifest.json`, `xai_run_metadata.json`, `render_metadata.json`, `preview_frame.jpg`, and `output_xai.mp4`.
- xAI-native projects use `xai_manifest.json` as the source of truth, do not write `remotion_props.json` compatibility artifacts, and strict inspect plus production validation reject single-output or batch-root directories that contain a legacy `remotion_props.json`.
- Production validation rejects symlinked xAI-native source-of-truth and render artifacts, including manifests, run/render metadata, raw shots, normalized shots, HyperFrames project files, timeline, final output, and preview frame; valid outputs must be output-local regular files.
- xAI-native single-output production preflights context cancellation and explicit output roots before resume lookup, planning, shot generation, or rendering; when using default output, it preflights the `ShandHome/projects` base before planning. Already-canceled contexts, contexts canceled by the time reusable-shot validation, per-shot cached-shot validation, or planning returns, symlinked output roots, non-directory output roots, non-directory path components, or symlinked ancestors inside the requested output path fail before any artifact is read or written through that path, even when the output leaf already exists.
- If the context is canceled by the time an xAI shot generation or generated-shot
  validation call returns, the single-output pipeline stops before staging or
  committing the returned bytes, and before manifest/run metadata or render
  artifacts are written.
- The HyperFrames/FFmpeg renderer treats cancellation as a staged-artifact
  boundary: an already-canceled context fails before normalization, external
  tools, or render artifact directories are touched; after FFmpeg normalization
  and validation, HyperFrames rendering and timeline validation, FFmpeg
  finalization and validation, and preview extraction and validation, it
  rechecks context before promoting staged artifacts to canonical output paths.
- The FFmpeg normalizer and preview extractor also preflight cancellation before
  creating output directories or resolving/running `ffmpeg`, so lower-level
  media helpers do not rely on the renderer or orchestrator to prevent canceled
  side effects.
- The shared FFmpeg/ffprobe command resolver preflights cancellation before
  system binary or `bunx remotion` lookup, so canceled media stages report
  cancellation instead of missing-tool errors.
- The ffprobe output validator preflights cancellation before filesystem stat
  checks or `ffprobe` execution, so canceled validation reports cancellation
  instead of misleading missing-file or probe errors.
- `ValidateOutputDir` preflights cancellation before inspect, artifact checks,
  or ffprobe validation, and rechecks after ffprobe dependency calls return so
  canceled validation exits without scanning output directories or accepting
  stale probe metadata.
- `ValidateBatchOutputDir` applies the same cancellation preflight before batch
  root scanning or per-episode validation.
- Batch inspect and validation reject symlinked `episode_###` directories; batch episodes must be output-local directories, not external targets hidden behind symlinks.
- CLI xAI inspect/validate summaries and the optional live validation gate treat symlinked batch episode directories as invalid batch roots instead of falling back to single-output handling.
- CLI xAI inspect and validate give batch-root detection priority over
  single-output handling; roots with `episode_###` signals return batch summaries
  without first reading root-level single-output manifests or running root-level
  ffprobe checks.
- xAI-native batch production preflights story input, context cancellation, and output root before starting episode workers; empty stories, already-canceled contexts, symlinked batch roots, non-directory batch roots, non-directory path components, symlinked ancestors inside the requested output path, root-level single-output artifacts, malformed/gapped `episode_*` entries, symlinked episode directories, and existing episodes beyond the requested `--episodes N` fail before partial generation starts, even when the batch root already exists.
- The xAI-native CLI single and batch wrappers normalize nil contexts before
  handing work to the runner, and reject already-canceled contexts before
  resolving the runner factory.
- `Orchestrator.Run`, `RunBatch`, `ValidateOutputDir`, and
  `ValidateBatchOutputDir` normalize nil contexts to `context.Background()`
  before preflight checks, so command callers that do not install a Cobra context
  cannot panic before xAI-native single, batch, or validation routing starts.
- The optional live validation gate preflights `OUT_DIR` before creating it; symlinked `OUT_DIR` values, non-directory `OUT_DIR` values, non-directory path components, or symlinked ancestors inside the requested output path fail before auth, generation, inspection, or validation commands can run, even when the output leaf already exists.
- The xAI-native dry-run smoke uses the same shell-level `OUT_DIR` preflight before creating directories or writing command summaries, so smoke tests cannot write through symlinked output roots before Go preflight runs.
- `--force-replan` bypasses same-input manifest reuse and calls xAI planning again.
- `--force-regenerate` can reuse a matching story manifest but forces xAI shot
  video regeneration; if `video.model` changed, the reused manifest is updated
  to the requested `video_model` and all prompt hashes are recomputed without
  calling xAI planning again.
- `--episodes N` now routes to xAI-native batch production for `xai_oauth`; each episode writes a separate `episode_###` directory under the batch output root.
- xAI-native CLI routing, static-render skipping, and video-mode validation helpers normalize backend names by trimming and lowercasing before handling the `xai-oauth` alias, so uppercase xAI OAuth aliases cannot fall through to legacy routes. Routing helpers also trim `--image-dir` before deciding whether the xAI-native path is eligible.
- xAI-native CLI backend resolution treats whitespace-only flag or config backend values as unset, preserving the intended priority of effective flag > effective config > `xai_oauth` default.
- xAI-native CLI validation rejects unknown `--video-backend` values before routing; typoed xAI backends cannot silently fall through to legacy Remotion or browser paths.
- xAI-native CLI runners trim and lowercase `--format` before building single-episode and batch `RunOptions`; blank format values fall back to the first stable `portrait` target.
- xAI-native CLI validation rejects non-portrait `--format` values for `xai_oauth` before the planner or renderer is created; legacy non-xAI routes may still choose their existing formats explicitly.
- xAI-native and legacy CLI batch mode both reject non-positive `--batch-concurrency` values before any batch runner starts; single-episode runs ignore the batch-only flag.
- xAI-native CLI validation rejects `--force-replan` and `--force-regenerate` on explicit legacy backends; those flags are xAI-native-only controls and are no longer silent no-ops outside `xai_oauth`.
- Legacy Remotion, image/TTS/BGM, `--skip-llm`, series-memory, multi-speaker TTS, faithful/verbatim/narration story modes, AI Critic rerender retries, and image-dir/i2v paths remain available only under explicit non-xAI legacy routes. `xai_oauth` rejects `--skip-llm`, `--series-memory`, `--multi-speaker`, `--faithful`, `--verbatim`, `--narration`, `--max-retries`, `--image-dir`, and `--i2v` so those flags cannot silently bypass the xAI-native HyperFrames/FFmpeg render contract.
- The old legacy `xai_oauth` concat stage has been removed. xAI OAuth video output now routes through the xAI-native manifest, HyperFrames timeline, and FFmpeg finalization path only.
- `internal/xaipipeline` no longer exposes an FFmpeg-only concat renderer. HyperFrames is the only xAI-native timeline renderer.

## Render Stack Decision

The xAI-native pipeline uses HyperFrames and FFmpeg together as the render layer.

| Layer | Responsibility | Must Not Do |
|---|---|---|
| xAI OAuth LLM | Turn story input into a shot manifest | Render video, call local models, depend on legacy prompt stages |
| xAI OAuth video | Generate one MP4 per planned shot | Compose the final timeline, repair codecs, provide fallback media |
| HyperFrames | Compose normalized shot videos into a timeline with subtitles, overlays, and transitions | Generate model content, call xAI, normalize media, mux final audio |
| FFmpeg / ffprobe | Normalize shots, strip audio in the stable path, finalize/transcode the timeline, validate output metadata | Make creative planning decisions, replace HyperFrames timeline layout |

This means render is deterministic after xAI shot generation:

```text
xai_manifest.json
  -> shots/shot_NNN.mp4              # xAI output
  -> xai_run_metadata.json           # planning/resume/generation decisions
  -> normalized/shot_NNN.mp4         # FFmpeg normalization
  -> hyperframes/index.html          # HyperFrames timeline project
  -> hyperframes/package.json        # xAI HyperFrames render project manifest
  -> timeline_hyperframes.mp4        # HyperFrames rendered timeline
  -> output_xai.mp4                  # FFmpeg finalized artifact
  -> render metadata / preview frame # ffprobe + FFmpeg validation artifacts
```

Remotion is not a fallback inside this path. Browser-based Grok generation is deprecated for this path. A caller that wants Remotion, Nova Reel, or browser automation must select that backend explicitly.

Final render artifacts are treated as one bundle. The renderer must stage
`output_xai.mp4`, `render_metadata.json`, and `preview_frame.jpg` before
committing any canonical final artifact. A metadata or preview commit failure
must roll back the final MP4 instead of leaving mismatched output, metadata, and
preview files.

## Render Tooling Plan

The render layer is split deliberately:

- HyperFrames is the timeline renderer. It owns visual timing, shot placement, subtitles, overlays, and transitions.
- FFmpeg is the media conditioning and packaging tool. It owns codec normalization, audio stripping, final transcode, preview extraction, and `ffprobe` validation.
- xAI is the only model surface in the default path. It plans the manifest and generates raw shot videos, but it does not compose the final timeline.

Pairing constraints:

- HyperFrames must never read raw `shots/shot_NNN.mp4`; it only reads FFmpeg-normalized files from `normalized/shot_NNN.mp4`.
- FFmpeg-only concat is not an allowed xAI-native renderer. FFmpeg can normalize and finalize media, but HyperFrames owns timeline composition.
- HyperFrames output is not production-final until FFmpeg finalization, `ffprobe` validation, and preview extraction all complete.
- The default xAI pipeline fails closed when either tool is unavailable or returns invalid media; it does not switch to Remotion, browser automation, local models, or placeholder videos.
- Tests should assert the tool order directly: normalize shots, render HyperFrames timeline, finalize timeline, validate final MP4, extract preview.

Stable render contract:

1. Raw xAI videos are written to `shots/shot_NNN.mp4`.
2. FFmpeg normalizes every shot before composition into `normalized/shot_NNN.mp4`.
3. HyperFrames receives the normalized shot list, manifest dimensions, manifest FPS, and subtitle/timing data.
4. HyperFrames renders `timeline_hyperframes.mp4` from the generated `hyperframes/index.html` project.
5. FFmpeg finalizes `timeline_hyperframes.mp4` into `output_xai.mp4`.
6. `ffprobe` validates the final file and writes `render_metadata.json`.
7. FFmpeg extracts `preview_frame.jpg` for quick production inspection.

Failure policy:

- If xAI video generation fails, stop the run. Do not synthesize placeholder videos.
- If FFmpeg normalization fails, stop before HyperFrames. Do not pass raw xAI videos directly into the final timeline.
- If HyperFrames rendering fails, stop the run. Do not fall back to Remotion or FFmpeg-only concat in the default xAI-native path.
- If final FFmpeg validation fails, return a clear error and keep artifacts for debugging.

First stable output policy:

- Portrait default: 720x1280, 24fps, H.264, yuv420p.
- Silent final output: no audio stream in `output_xai.mp4`.
- Shot count is explicit: `--panels N` means exactly N generated xAI video shots.
- The source of truth is `xai_manifest.json`, not `remotion_props.json` or generated HyperFrames HTML.

## TDD Implementation Roadmap

Build the xAI-native render path as a separate production pipeline, with tests
locking each boundary before live xAI usage is widened.

1. Command routing contract
   - Default `shand pipeline` routes to `xai_oauth`.
   - `xai-oauth` remains an accepted alias.
   - `--skip-llm`, `--image-dir`, and `--i2v` are rejected in the xAI-native path.
   - `--episodes` must be greater than zero, and `--panels` must be zero or greater.
   - Tests prove the default path does not initialize local models, image
     generation, TTS, BGM, Remotion render, browser automation, or FFmpeg-only
     concat.

2. Manifest and planning contract
   - xAI OAuth LLM produces `xai_manifest.json`.
   - The manifest owns project id, story hash, xAI video model, shot order,
     prompt hashes, duration, format, dimensions, FPS, subtitles, and xAI
     request metadata.
   - Legacy `llm.provider`, `llm.model`, and `llm.base_url` do not override this
     xAI-native planner.
   - Tests reject unsafe project ids, mismatched shot indexes, non-portrait
     formats, and render specs outside the first stable target.

3. xAI shot generation contract
   - xAI OAuth video generates only raw per-shot videos into `shots/shot_NNN.mp4`.
   - `video.model` is honored only for `video.provider: xai_oauth`.
   - Each generated shot must be validated before manifest/run metadata is
     committed.
   - Cached shots are reused only when prompt hash, manifest contract, MP4 magic,
     and ffprobe validation match.

4. FFmpeg normalization contract
   - Every raw xAI shot is normalized before HyperFrames receives it.
   - Normalized outputs live at `normalized/shot_NNN.mp4`.
   - Normalization enforces 720x1280 portrait, 24fps, H.264, yuv420p, and no
     audio for the first stable path.
   - The renderer fails before timeline creation if normalization or validation
     fails.

5. HyperFrames timeline contract
   - HyperFrames is the only timeline renderer in the default xAI-native path.
   - It receives normalized shot paths, manifest dimensions, FPS, shot order,
     timing, subtitles, overlays, and transitions.
   - It writes `hyperframes/index.html`, `hyperframes/package.json`, and
     `timeline_hyperframes.mp4`.
   - A HyperFrames failure is terminal; Remotion or FFmpeg-only concat must not
     replace it implicitly.

6. FFmpeg finalization and validation contract
   - FFmpeg finalizes `timeline_hyperframes.mp4` into `output_xai.mp4`.
   - ffprobe validates the exact final artifact before `render_metadata.json` is
     committed.
   - FFmpeg extracts and validates `preview_frame.jpg`.
   - `output_xai.mp4`, `render_metadata.json`, and `preview_frame.jpg` are
     treated as an atomic final render bundle.

7. Dry-run, inspect, validate, and live gates
   - Dry-run writes production-shaped placeholders without network calls.
   - `xai inspect --strict` verifies artifact completeness, including
     HyperFrames project evidence.
   - `xai validate` adds ffprobe checks, xAI request/status metadata checks, and
     staged-artifact rejection.
   - Live validation remains opt-in and requires Hermes `xai-oauth`
     credentials.

## Architecture

Add a new package instead of continuing to stretch the existing orchestrator:

```text
internal/xaipipeline/
  orchestrator.go      # story -> shot plan -> xAI videos -> render
  planner.go           # xAI LLM prompt and JSON parsing
  types.go             # manifest, shot, dependency interfaces
  video.go             # shot generator adapter and per-shot video options
  cache.go             # shot resume/cache checks with ffprobe
  normalizer.go        # FFmpeg shot normalization
  hyperframes_renderer.go # HyperFrames + FFmpeg render orchestration
  inspect.go           # xAI-native artifact inspection
  validate.go          # production validation across inspect + ffprobe + xAI request metadata
  dryrun.go            # no-network dry-run generator/finalizer
  mock.go              # test doubles
```

Keep existing provider packages:

```text
internal/auth/xai      # Hermes OAuth token source
internal/llm           # xAI OAuth Responses client
internal/video         # xAI OAuth video client
internal/hyperframes   # HTML/timeline renderer
internal/video/ffmpeg  # legacy concat helpers; not the xAI-native renderer
```

The legacy `internal/pipeline` stays intact for Remotion and asset-heavy workflows.

## Data Model

Introduce an xAI-specific manifest that is closer to video production than Remotion props:

```json
{
  "project_id": "last_glowing_flower",
  "story_hash": "sha256-of-normalized-story-input",
  "video_model": "grok-imagine-video",
  "format": "portrait",
  "fps": 24,
  "width": 720,
  "height": 1280,
  "shots": [
    {
      "index": 1,
      "prompt": "Wide shot of a robot crossing a gray wasteland...",
      "prompt_hash": "sha256-of-generation-spec",
      "xai_request_id": "req_abc123",
      "xai_status": "done",
      "duration_sec": 8,
      "aspect_ratio": "9:16",
      "resolution": "720p",
      "video_path": "shots/shot_001.mp4",
      "subtitle": "在灰色荒原中，它找到了最後一朵花。",
      "transition_out": "cut"
    }
  ]
}
```

This manifest should be the source of truth for:

- xAI video generation
- resume behavior
- same-input manifest reuse before calling xAI planning
- run metadata for planning/cache/debug decisions, including per-shot decision entries
- HyperFrames timeline layout
- HyperFrames timeline shot order
- final validation metadata

## Render Strategy

### 1. xAI Video Generation

For each shot:

```text
POST /videos/generations
GET  /videos/{request_id}
download video.url -> shots/shot_NNN.mp4
```

Defaults:

```text
model: grok-imagine-video
duration: 8
aspect_ratio: 9:16
resolution: 720p
```

Resume rule:

- If an explicit `--output-dir` already contains an `xai_manifest.json` with a matching `story_hash`, compatible format/shot count, and reusable evidence for every cached shot, reuse that manifest before calling the planner.
- If `shots/shot_NNN.mp4` exists, is non-empty, `ffprobe` can decode it, and the prior manifest persists a `prompt_hash` matching the current shot plan, skip generation.
- Cached shots with `xai_status` such as `pending`, `failed`, `error`, `expired`, or `cancelled` must regenerate instead of being reused.
- Reused provider metadata is normalized before writing the next `xai_manifest.json` and `xai_run_metadata.json`; surrounding whitespace and status casing in legacy manifests are not carried forward.
- Legacy manifests missing `prompt_hash` for a shot must regenerate that shot instead of deriving trust from old prompt fields.
- If the file exists but fails validation or the prompt/spec changed, regenerate only that shot.
- `--force-replan` disables the same-input manifest reuse check but does not by itself force video regeneration.
- `--force-regenerate` disables shot cache reuse and rewrites xAI shot videos.
  It may still reuse the same story/shot manifest, including across
  `video.model` changes, because model selection belongs to xAI video
  generation rather than xAI LLM planning.

### 2. FFmpeg Normalization

Normalize every xAI shot before timeline composition:

```text
shots/shot_001.mp4
  -> normalized/shot_001.mp4
```

Normalization target:

- H.264 video
- no audio in the first stable path; later iterations must add an explicit mux/audio plan
- 720x1280 portrait
- 24 fps
- consistent timebase
- yuv420p

This avoids concat failures caused by slight codec/timebase differences.

Audio policy:

- xAI-generated shot audio is stripped during normalization.
- FFmpeg finalization writes silent video (`-an`) in the default xAI-native path.
- `ffprobe` final validation fails if `output_xai.mp4` contains an audio stream.
- Any future audio support must be explicit, e.g. muxing selected xAI shot audio or adding a separate audio plan.

### 3. HyperFrames Timeline

HyperFrames should become the timeline renderer for xAI output. It should support video segments, not only still images.

Current first slice keeps this isolated in `internal/xaipipeline` instead of changing the existing image-based `internal/hyperframes` template:

- Render `<video>` elements for xAI shot files.
- Keep subtitle rendering and basic timing.
- Register `window.__hf.seek` for HyperFrames.
- Keep all shot timing in seconds from `xai_manifest.json`.
- Treat the generated `hyperframes/index.html` as an implementation detail of the render stage, not as the project source of truth.
- Always render through HyperFrames in the xAI-native path, even when there are no subtitles. Do not add an FFmpeg-only concat shortcut as the default path.

Initial mode:

```text
xAI shot videos + subtitles + simple transitions -> silent/timeline video
```

The first stable path is silent by design. HyperFrames renders visual overlays and subtitles only; any future muxed audio must be added as a separate tested stage.

### 4. FFmpeg Final Assembly

FFmpeg remains responsible for:

- final transcode from the HyperFrames timeline output
- stripping audio in the default stable path
- generating validation metadata with `ffprobe`
- extracting a preview frame for smoke-test inspection

Final output:

```text
output_xai.mp4
```

## Pipeline Stages

### Stage A: Plan

Input:

- raw story
- optional `--panels N`
- optional format

Output:

- `xai_manifest.json`
- shot prompts
- subtitles/dialogue text
- visual continuity notes

Test:

- xAI LLM mocked
- invalid JSON recovery
- `--panels N` honored
- no image/audio/remotion provider calls

### Stage B: Generate Shots

Input:

- `xai_manifest.json`

Output:

- `shots/shot_001.mp4`
- updated manifest with paths and request IDs if available
- `xai_run_metadata.json` with planned/reused/generated decisions and per-shot decision entries

Test:

- httptest xAI submit/poll/download
- cached shot skip
- failed validation regenerates only that shot
- force-replan bypasses manifest reuse
- force-regenerate rewrites cached shots
- run metadata shot decisions include index, decision, video path, prompt hash, and xAI request/status when available

### Stage C: Normalize

Input:

- raw xAI shot MP4s

Output:

- normalized MP4s

Test:

- invalid input reports a clear error
- ffprobe metadata matches target shape
- normalized output has no audio in the first stable path

### Stage D: Render Timeline

Input:

- normalized shots
- subtitle/timing data

Output:

- HyperFrames timeline output

Test:

- generated `index.html` contains video segments
- no blank frame at start
- dimensions match manifest
- HyperFrames receives an absolute project directory and output path
- HyperFrames receives the manifest FPS instead of relying on its default FPS

### Stage E: Finalize

Input:

- timeline output or normalized shot list

Output:

- `output_xai.mp4`
- `render_metadata.json`
- optional `preview_frame.jpg`

Test:

- HyperFrames timeline shot order stable
- ffprobe duration roughly equals sum of shots
- output decodable
- output has no audio stream in the first stable xAI-native path
- preview frame exists and is readable

## TDD Plan

Done:

- Add `internal/xaipipeline` with mocked tests first.
- Move xAI shot generation behind `internal/xaipipeline` interfaces.
- Route default single-episode `xai_oauth` runs to the new orchestrator.
- Add prompt-aware shot cache validation.
- Add FFmpeg normalization before HyperFrames render.
- Add xAI-specific HyperFrames video timeline generation.
- Add final output validation metadata after FFmpeg finalize.
- Add preview-frame extraction for smoke tests.
- Pass manifest FPS through to HyperFrames so the timeline render matches FFmpeg/ffprobe validation.
- Add explicit CLI/help deprecation wording for `grok_browser`.
- Decide first stable audio policy: strip xAI shot audio and require silent final output.
- Expose xAI-native artifact paths in the orchestrator result and CLI summary.
- Decide first stable shot-count policy: `--panels N` means exactly N xAI video shots.
- Decide first stable timeline policy: always render xAI-native output through HyperFrames before FFmpeg finalization.
- Decide first stable manifest policy: xAI-native writes `xai_manifest.json` only, not `remotion_props.json`.
- Keep legacy `internal/pipeline` for `remotion`, `nova_reel`, `hyperframes`, and old project workflows.
- Run a 3-shot xAI OAuth production smoke through shot generation; this exposed HyperFrames' 30fps default, which is now fixed by passing `--fps 24`.
- Re-render the generated 3-shot assets locally without new xAI calls; the HyperFrames + production FFmpeg finalizer path produced 720x1280, 24fps, 24s silent output and a preview frame.
- Run a post-fix 1-shot xAI OAuth CLI production smoke; it wrote the named artifacts and validated `output_xai.mp4` as 720x1280, 24fps, 8s, silent H.264.
- Run a post-fix 3-shot xAI OAuth CLI production smoke; it wrote all named artifacts and validated `output_xai.mp4` as 720x1280, 24fps, 18s, silent H.264.
- Add a same-output-dir resume unit smoke for the orchestrator; unchanged valid cached shots are not regenerated, while render still runs against the manifest.
- Add manifest-level `story_hash` reuse for explicit output directories, so same-story resume can skip xAI planning before render.
- Run a deterministic CLI-level resume smoke; the second run preserved `shots/shot_001.mp4` mtime/size while re-rendering through HyperFrames at 24fps.
- Add user-facing `--force-replan` and `--force-regenerate` controls with tests for planner and shot-cache bypass semantics.
- Add `xai_run_metadata.json` and expose it in the CLI summary; it records whether the run planned, reused a manifest, reused shots, generated shots, and force flag state.
- Run a production-cache metadata smoke; the repeated run preserved `shots/shot_001.mp4` mtime/size and wrote `planned=false`, `manifest_reused=true`, `reused_shots=[1]`.
- Add per-shot `xai_request_id` and `xai_status` metadata through an optional result interface on the video adapter; dry-run now emits deterministic shot metadata for smoke checks.
- Add run-level `shot_decisions` entries that combine cache decisions with video path, prompt hash, and xAI request/status metadata.
- Tighten the orchestrator generation boundary so a generated shot must include non-empty xAI provider metadata before the MP4 is written and before `xai_manifest.json` / `xai_run_metadata.json` are persisted; missing request/status metadata now fails the run early instead of deferring discovery to production validation.
- Tighten cached-shot resume so an existing MP4 is reused only when the previous manifest also has non-empty xAI provider metadata for that shot; legacy caches missing request/status metadata are regenerated instead of carrying invalid metadata forward.
- Tighten the xAI video adapter boundary so production shot generation requires a metadata-capable video client before any video request is made; metadata-less `GenerateVideo` fallback is no longer used in the xAI-native path.
- Run a 1-shot live xAI production smoke for request metadata; real `xai_request_id` and `xai_status=done` were written to both `xai_manifest.json` and `xai_run_metadata.json`, with final output validated as 720x1280, 24fps, 8s, silent H.264.
- Add `shand xai inspect <output-dir>` and `internal/xaipipeline.InspectOutputDir` to summarize xAI-native artifacts, run metadata, render metadata, final output, preview frame, per-shot request IDs, and missing optional artifacts.
- Run a production inspect smoke against `outputs/xai-native-live-request-metadata-20260611-151213`; it reported one portrait shot, `xai_status=done`, no missing artifacts, existing final output/preview frame, and render metadata of 720x1280, 24fps, 8s, silent H.264.
- Add machine-readable `status` and `issues` fields to `shand xai inspect <output-dir>`: `complete` for fully materialized outputs, `partial` for valid manifests with missing optional artifacts, and `invalid` for missing or malformed required inspection data.
- Run inspect status smokes: the live xAI output reports `status=complete`, while an empty directory reports `status=invalid` with `missing required xai_manifest.json` as JSON on stdout.
- Add `shand xai inspect --strict <output-dir>` for automation: it preserves JSON stdout, exits 0 only when `status=complete`, and exits non-zero when the inspected output is `partial` or `invalid`.
- Run strict inspect smokes: the live xAI output exits 0 with `status=complete`; an empty directory writes `status=invalid` JSON to stdout and exits 1 with structured stderr.
- Add README and README.zh-TW production loop notes for xAI-native generation, inspect, strict inspect, resume, force replan, force regenerate, and expected artifacts.
- Verify README-documented `xai inspect --strict`, `pipeline --output-dir`, `--panels`, `--force-replan`, and `--force-regenerate` flags against current CLI help.
- Add `scripts/smoke-xai-native-dry-run.sh` to exercise the README production loop without xAI network calls: dry-run generation, partial inspect, local complete fixture, strict inspect success, and strict inspect invalid failure.
- Add the no-network xAI-native smoke to `.github/workflows/ci.yml` after `go build`, reusing `/tmp/shand`.
- Fix the CI build step to create `GOTMPDIR=/tmp/gobuild` before invoking `go build`.
- Verify the CI-equivalent local path with `mkdir -p /tmp/gobuild && GOTMPDIR=/tmp/gobuild go build -o /tmp/shand . && SHAND_BIN=/tmp/shand scripts/smoke-xai-native-dry-run.sh`.
- Add a cleanup policy to `scripts/smoke-xai-native-dry-run.sh`: auto-created temp output is removed on success, failures preserve artifacts for debugging, and `KEEP_SMOKE_OUTPUTS=1` or an explicit `OUT_DIR` preserves the main output.
- Add `scripts/lib/xai-output-dir.sh` as the shared shell-level `OUT_DIR` preflight for xAI smoke/gate scripts; the dry-run smoke has its own guard smoke proving symlinked roots or nearest existing ancestors fail before `shand` can run or shell redirections can create external artifacts.
- Add a failed-run artifact upload step to `.github/workflows/ci.yml`; xAI-native smoke now writes to `$RUNNER_TEMP/xai-native-smoke`, and `actions/upload-artifact@v4` uploads that directory on job failure.
- Verify the CI-equivalent fixed-output path locally with `OUT_DIR=$RUNNER_TEMP/xai-native-smoke SHAND_BIN=/tmp/shand scripts/smoke-xai-native-dry-run.sh`.
- Add `shand xai validate <output-dir>` and `internal/xaipipeline.ValidateOutputDir` for production validation: inspect must be complete, final output must pass FFmpeg/ffprobe validation, and manifest/run metadata must include `xai_request_id` plus `xai_status=done` for every shot.
- Run `shand xai validate` against `outputs/xai-native-live-request-metadata-20260611-151213`; it returned `status=valid`, one shot, 720x1280, 24fps, 8s, H.264, and no audio stream.
- Run an invalid validation smoke against an incomplete dry-run output; it preserved machine-readable JSON on stdout and exited non-zero with a structured stderr error.
- Run a fresh live 3-shot xAI OAuth production smoke against `outputs/xai-native-live-3shot-validate-20260611-155259`; `validation.json` reports `status=valid`, 3 shots, 720x1280, 24fps, 24s, H.264, no audio stream, and every manifest/run-metadata shot has `xai_request_id` with `xai_status=done`.
- Add `scripts/validate-xai-native-live.sh` as the optional live validation gate: it validates existing xAI-native output directories, generates fresh live output only when `RUN_XAI_LIVE_VALIDATION=1`, writes `inspect.json` and `validation.json`, and can skip cleanly when Hermes xAI OAuth credentials are absent.
- Add `scripts/smoke-xai-live-validation-gate.sh` and wire it into CI as a no-network smoke for the live gate behavior, including no-spend guards for missing auth and missing `RUN_XAI_LIVE_VALIDATION=1`.
- Tighten the optional live validation gate so existing xAI-native output validation can run offline without Hermes auth; auth is required only for fresh live generation, while available auth status is still recorded as `auth_status.json`.
- Add manual GitHub Actions workflow `.github/workflows/xai-native-live-validation.yml`; it restores `~/.hermes/auth.json` from repository secret `HERMES_AUTH_JSON_B64`, runs the optional live gate, and uploads the output directory as an artifact.
- Add video-backend normalization for the `xai-oauth` alias so CLI flags and `video.provider` config cannot accidentally route away from the xAI-native pipeline; `video.model` is honored for both `xai_oauth` and `xai-oauth`.
- Add a production-validation regression for dry-run metadata: `xai inspect --strict` may report a dry-run fixture as complete after local fixture files are added, but `xai validate` must still reject `xai_status=dry_run`; the no-network smoke now asserts this.
- Add xAI-native batch production for `--episodes N`: it uses `internal/xaipipeline.RunBatch`, bounded concurrency, and per-episode `episode_###` output directories instead of legacy `internal/pipeline.RunBatch`.
- Add batch-root inspection and validation: `shand xai inspect <batch-root>` and `shand xai validate <batch-root>` auto-detect `episode_###` directories and summarize every episode.
- Tighten batch-root inspection and validation so episode directories must be named exactly `episode_###` with three digits and must be contiguous from `episode_001`; malformed names or missing episode numbers now invalidate the batch root instead of being silently ignored.
- Add command-level regression coverage so `shand xai inspect` and `shand xai validate` return invalid batch JSON for malformed or gapped `episode_###` roots, not a misleading single-output summary.
- Tighten the optional live validation gate so existing batch output detection only accepts `episode_###/xai_manifest.json`; malformed directories such as `episode_1` are rejected before any generation or validation command runs.
- Add live-gate preflight coverage for mixed valid/malformed batch roots and gapped batch roots; any first-level `episode_*` directory must match `episode_###`, be greater than `episode_000`, and be contiguous before `xai inspect` or `xai validate` runs.
- Tighten batch-root inspection and validation so a directory containing `episode_###` outputs cannot also contain root-level single-output artifacts such as `xai_manifest.json`, `xai_run_metadata.json`, `render_metadata.json`, `output_xai.mp4`, `preview_frame.jpg`, `timeline_hyperframes.mp4`, `shots/`, `normalized/`, or `hyperframes/`; ambiguous single-output/batch-output roots now report invalid batch status instead of silently ignoring episodes.
- Extend the optional live validation gate to accept existing xAI-native batch roots containing `episode_###/xai_manifest.json`, and to generate fresh batch outputs with `EPISODES` plus `BATCH_CONCURRENCY` when live generation is explicitly enabled.
- Add CLI-level xAI-native batch failure policy: `internal/xaipipeline.RunBatch` still returns a partial result with per-episode errors, but `shand pipeline --episodes N` now returns an error when any episode fails so production automation cannot treat partial batch output as success.
- Add command-level regression coverage for the default xAI route: `runPipeline` succeeds through the xAI-native runner even when legacy local LLM, image, TTS, and BGM providers are intentionally invalid, proving the default route does not initialize those legacy factories.
- Add matching command-level regression coverage for the default xAI batch route: `runPipeline --episodes N` succeeds through `xai_native_batch` with intentionally invalid legacy local LLM, image, TTS, and BGM providers, proving batch routing also avoids legacy factories.
- Tighten the xAI-native dry-run render contract so HyperFrames dry-run writes `timeline_hyperframes.mp4`, the dry-run finalizer writes `output_xai.mp4`, the dry-run preview extractor writes `preview_frame.jpg`, and the no-network smoke verifies the complete production artifact shape without hand-written fixtures.
- Tighten production validation so persisted `render_metadata.json` must also satisfy the manifest-derived render spec; live ffprobe validation still runs, but stale or mismatched persisted render metadata now invalidates `shand xai validate`.
- Tighten production validation so final output metadata must report `codec_name=h264`; both persisted `render_metadata.json` and live ffprobe validation now enforce the first stable H.264 output contract.
- Tighten production validation so final and normalized output metadata must report `pixel_format=yuv420p`; persisted `render_metadata.json` and live ffprobe validation now enforce the first stable yuv420p output contract, while raw xAI shots and the HyperFrames intermediate timeline remain unconstrained on pixel format.
- Tighten production validation so persisted `render_metadata.json.path` must resolve to the actual `output_xai.mp4`; stale render metadata for another file now invalidates `shand xai validate`.
- Tighten production validation so persisted `render_metadata.json.size_bytes` must match the actual `output_xai.mp4` file size; stale render metadata copied from another artifact now invalidates `shand xai validate`.
- Tighten production validation so `preview_frame.jpg` must be a readable JPEG with manifest-matching dimensions; text, stale placeholder, or wrong-size preview artifacts no longer pass `shand xai validate`.
- Tighten production validation so every manifest shot must have a non-empty raw xAI shot artifact and a non-empty `normalized/shot_NNN.mp4` artifact before `shand xai validate` can pass.
- Tighten production validation so raw xAI shot files, normalized shot files, and `timeline_hyperframes.mp4` must have MP4 `ftyp` magic bytes; non-empty text placeholders no longer pass production validation.
- Tighten production validation so every raw xAI shot must be ffprobe-decodable and roughly match the manifest shot duration; raw xAI shots are not required to satisfy the final silent H.264 render spec before normalization.
- Tighten production validation so every normalized shot also passes ffprobe against the manifest-derived normalization spec: portrait 720x1280, 24fps, H.264, expected shot duration, and no audio stream.
- Tighten production validation so `timeline_hyperframes.mp4` also passes ffprobe against the manifest timeline spec: portrait dimensions, 24fps, expected total duration, and no audio stream; H.264 remains the final FFmpeg output contract.
- Tighten production validation so `hyperframes/index.html` must reference every `normalized/shot_NNN.mp4` used by the manifest; stale or placeholder HyperFrames projects no longer pass production validation.
- Tighten production validation so `hyperframes/index.html` must reference normalized shot MP4s in manifest shot order; reversed or stale HyperFrames timelines no longer pass production validation.
- Tighten production validation so `hyperframes/index.html` composition width, height, and total duration must match the manifest-derived render spec; stale HyperFrames projects with wrong dimensions or timing no longer pass production validation.
- Tighten production validation so every HyperFrames video clip's `data-start` and `data-duration` must match the manifest-derived per-shot timeline; stale clip timing no longer passes production validation.
- Tighten production validation so `hyperframes/index.html` must include the xAI composition id plus HyperFrames runtime hooks (`window.__timelines["xai-video"]` and `window.__hf`); static HTML video lists no longer pass production validation.
- Tighten production validation so manifest subtitles must appear as timed HyperFrames subtitle clips; outputs with missing or stale subtitle overlays no longer pass production validation.
- Tighten production validation so HyperFrames project evidence is required: `hyperframes/index.html`, `hyperframes/package.json`, and `timeline_hyperframes.mp4` must exist and be non-empty before `shand xai validate` can pass.
- Tighten production validation so `hyperframes/package.json` must be the expected xAI render project manifest (`name=xai-video-timeline`, `version=1.0.0`, `private=true`); stale or malformed package manifests no longer pass production validation.
- Tighten production validation so xAI-native single-output roots and batch roots reject legacy `remotion_props.json`; stale Remotion compatibility artifacts can no longer coexist with `xai_manifest.json` or `episode_###` batch outputs in a valid production output.
- Tighten `xai inspect --strict` completeness so it now reports partial output when raw shot files, normalized shot files, `hyperframes/index.html`, `hyperframes/package.json`, or `timeline_hyperframes.mp4` are missing. `xai validate` remains stricter because it also runs ffprobe and checks production xAI request metadata.
- Tighten production validation so every `xai_run_metadata.json` shot decision must match the manifest shot's `video_path`, `prompt_hash`, `xai_request_id`, and `xai_status`; stale run metadata now invalidates `shand xai validate`.
- Tighten production validation so `generated_shots` and `reused_shots` in `xai_run_metadata.json` must match the per-shot `shot_decisions`; duplicate, unknown, or contradictory run metadata now invalidates `shand xai validate`.
- Tighten production validation so run metadata `shot_decisions[].decision` values must be canonical `generated` or `reused`; stale values such as `" generated "` now fail with a canonical decision issue instead of only surfacing as set mismatches.
- Tighten production validation so run metadata `shot_decisions[].video_path` values must also be canonical before matching the manifest; stale values such as `" shots/shot_001.mp4 "` now produce canonical contract issues instead of generic manifest mismatch errors.
- Tighten production validation so run metadata `shot_decisions[].prompt_hash` values must be canonical 64-character lowercase SHA-256 hex strings before matching the manifest; stale values such as whitespace-padded hashes now produce canonical contract issues instead of generic mismatch errors.
- Tighten production validation so `xai_run_metadata.json` must identify exactly one run origin: either `planned=true` for a newly planned run or `manifest_reused=true` for a resume run. Impossible or missing run-origin metadata now invalidates `shand xai validate`.
- Tighten production validation so `force_replan=true` cannot be paired with `manifest_reused=true`; force replan runs must be represented as planned runs, not resumed manifest reuse.
- Tighten production validation so `force_regenerate=true` cannot leave `reused_shots` or `decision=reused` entries; forced regeneration runs must represent every shot as generated metadata.
- Tighten manifest normalization so xAI-native shot indexes must match array order from 1 through N before generation starts; this prevents per-shot MP4 overwrites and ambiguous timeline/cache ordering.
- Tighten manifest normalization so xAI-native `project_id` must be a safe single path component before default output directory resolution; planner-returned path traversal such as `../escaped` is rejected before shot generation or rendering.
- Tighten the first stable xAI-native format contract: the orchestrator accepts `portrait` only, and planner-returned manifest format must match the requested format before shot generation starts.
- Tighten the first stable xAI-native render spec contract: the manifest must normalize to exactly 720x1280 at 24fps before shot generation starts; planner-returned alternate dimensions or FPS are rejected early.
- Tighten the first stable xAI-native shot generation contract: every shot must normalize to xAI video `aspect_ratio=9:16` and `resolution=720p`; planner-returned alternate shot generation specs are rejected before any xAI video call.
- Tighten production validation so every manifest `project_id` must satisfy the same safe single-component contract; stale or hand-written manifests with path traversal cannot pass `shand xai validate`.
- Tighten production validation so `shand xai validate` also enforces the first stable manifest contract: portrait, 720x1280, 24fps, and per-shot xAI video `aspect_ratio=9:16` / `resolution=720p` when those shot fields are present.
- Tighten production validation so `shand xai validate` also enforces manifest shot index/order: shot indexes must be unique, contiguous, and match array order from 1 through N.
- Tighten production validation so manifest prompts must be non-empty canonical trimmed text; stale manifests containing values such as `" shot "` now fail even when `prompt_hash` still matches the trimmed generation spec.
- Tighten production validation so manifest `prompt_hash` values must be canonical 64-character lowercase SHA-256 hex strings before being compared with the deterministic shot hash; stale whitespace-padded prompt hashes now produce canonical contract issues instead of generic hash mismatch errors.
- Tighten production validation so manifest `duration_sec` values must be positive; outputs with `duration_sec=0` no longer pass by falling back to the default 8-second interpretation during validation.
- Tighten production validation so non-empty manifest subtitles must already be canonical trimmed text; stale manifests containing values such as `" 第一個字幕 "` now fail with a manifest contract issue instead of relying only on HyperFrames subtitle evidence mismatches.
- Tighten production validation so non-empty manifest `transition_out` values must already be canonical (`cut` or `fade`); stale manifests containing values such as `" Fade "` now fail with a manifest contract issue instead of relying only on HyperFrames evidence mismatches.
- Tighten production validation so each manifest shot must reference its canonical self-contained raw artifact path `shots/shot_NNN.mp4`; absolute paths or alternate relative paths no longer pass even if the file exists.
- Tighten the HyperFrames/FFmpeg renderer so shot normalization always reads raw xAI videos from the canonical output-local path `shots/shot_NNN.mp4`; manifest `video_path` remains required metadata, must match that canonical path, and is not followed as an alternate input path.
- Tighten the HyperFrames/FFmpeg renderer so a shot normalizer is mandatory; a renderer without normalization fails before creating a HyperFrames project, and dry-run uses a deterministic normalizer placeholder so no-network smoke tests still prove the normalized artifact shape.
- Tighten the HyperFrames/FFmpeg renderer so an output validator is mandatory; a renderer without ffprobe/render metadata validation fails before rendering, and dry-run uses deterministic render metadata so no-network smoke tests prove `render_metadata.json` is produced by the renderer path.
- Tighten `xai inspect --strict` completeness so raw shot artifact presence is determined from the canonical output-local path `shots/shot_NNN.mp4`, not manifest `video_path`; external manifest paths can no longer make an output look complete.
- Tighten production validation so raw shot artifact checks and raw-shot ffprobe validation always use the canonical output-local path `shots/shot_NNN.mp4` instead of following manifest `video_path`; malformed manifests can no longer cause validation to read outside the output directory.
- Tighten production validation so every manifest shot and matching run metadata decision must include a `prompt_hash` that matches the deterministic hash of the shot prompt, duration, aspect ratio, and resolution, preserving resume/cache traceability.
- Tighten production validation so manifest and run metadata `xai_request_id` / `xai_status` values must be canonical strings; outputs with values such as `" req_123 "` or `" Done "` no longer pass even when manifest/run metadata agree with each other.
- Tighten production validation so every production manifest must include a 64-character lowercase hex `story_hash`; outputs without a persisted SHA-256-shaped story input fingerprint are no longer accepted as traceable xAI-native production artifacts.
- Tighten the optional live validation gate so `PANELS`, `EPISODES`, and `BATCH_CONCURRENCY` must be positive integers before auth checks or pipeline generation can run; invalid manual workflow inputs now fail without spend.
- Tighten the xAI-native dry-run planner so it honors `target_shots`; the no-network smoke now runs a 3-shot single-output pipeline and verifies `shots/shot_001.mp4` through `shot_003.mp4` plus matching normalized artifacts.
- Tighten xAI-native dry-run resume so cached placeholder shots use a dry-run shot validator instead of production ffprobe; the no-network smoke now reruns the same 3-shot output directory and verifies `manifest_reused=true` with reused shot metadata.
- Tighten command-level batch detection so `shand xai inspect` and `shand xai validate` return invalid batch JSON even when the root contains only malformed `episode_*` directories such as `episode_1`, instead of falling back to a misleading single-output missing-manifest summary.
- Tighten `ValidateOutputDir` so it rechecks metadata returned by the injected `OutputValidator` against the manifest-derived render spec; a custom validator cannot return mismatched ffprobe metadata with a nil error and still pass production validation.
- Extend the same returned-metadata recheck to raw xAI shots, normalized shots, and the HyperFrames timeline; injected ffprobe validators can no longer claim success while returning stage metadata that violates the manifest-derived duration, normalization, timeline, or silent-output contracts.
- Tighten returned ffprobe metadata evidence so every live/injected validation result must point back to the exact artifact being validated and report the artifact's actual file size; validators can no longer return metadata for a different MP4 or stale size while claiming success.
- Move the same media contract into the renderer path: normalized shots are validated immediately after FFmpeg normalization and before HyperFrames sees them, the HyperFrames timeline is validated before FFmpeg finalization, and final render metadata is checked against the exact output artifact before `render_metadata.json` is written.
- Validate `preview_frame.jpg` inside the renderer before returning success; the preview must be a readable manifest-sized JPEG, and dry-run now writes a valid JPEG placeholder so no-network smoke tests exercise the same preview contract.
- Tighten dry-run artifact fidelity so raw xAI shots, normalized shots, the HyperFrames timeline, and final output placeholders all include MP4 `ftyp` magic bytes; the no-network smoke now rejects `.mp4` artifacts that are merely arbitrary text files.
- Tighten dry-run preview fidelity so unit tests decode the generated preview JPEG and the no-network smoke rejects preview artifacts without JPEG magic bytes.
- Tighten dry-run resume validation so cached shot reuse requires MP4 `ftyp` magic bytes; stale text placeholders are regenerated instead of being reused as shot video artifacts.
- Tighten production resume validation so cached xAI shot reuse requires MP4 `ftyp` magic before ffprobe; decodable non-MP4 containers with `.mp4` names are regenerated instead of being reused as raw xAI shots.
- Consolidate MP4 `ftyp` artifact evidence behind one xAI pipeline helper so production resume validation, dry-run resume validation, and production artifact validation cannot drift apart.
- Tighten generated-shot persistence so every newly generated xAI raw shot requires a configured shot validator and must pass validation before `xai_manifest.json`, `xai_run_metadata.json`, or the HyperFrames/FFmpeg renderer can run.
- Make generated raw-shot commits atomic: new xAI video bytes are written to a temporary file, validated there, and only then renamed into `shots/shot_NNN.mp4`, so failed force-regenerate runs preserve the previous valid shot artifact and metadata.
- Stage all newly generated xAI shots before committing any canonical raw shot path; multi-shot force-regenerate failures now preserve the previous consistent raw-shot set instead of leaving old metadata paired with a partially regenerated `shots/` directory.
- Add rollback for raw-shot commit failures: existing `shots/shot_NNN.mp4` files are backed up during staged commit and restored if a later shot commit fails, preventing commit-stage filesystem errors from leaving partially regenerated shot sets.
- Tighten production validation so staged raw-shot artifacts left behind by interrupted commits, such as `.tmp.mp4` or `.bak` files in `shots/`, invalidate `shand xai validate` instead of being silently ignored.
- Make `xai_manifest.json` and `xai_run_metadata.json` a staged metadata commit pair: both are written to temporary files first, committed with backup/rollback, and a run metadata write failure preserves the previous manifest instead of leaving mismatched source-of-truth metadata.
- Tighten production validation so staged metadata artifacts left behind by interrupted commits, such as `.xai_manifest.json_*.tmp/.bak` or `.xai_run_metadata.json_*.tmp/.bak`, invalidate `shand xai validate`.
- Extend metadata write failure rollback to raw shots: generated shot backups are retained until the manifest/run-metadata source-of-truth commit succeeds, so metadata write failures restore both the previous manifest and previous `shots/shot_NNN.mp4` files.
- Make renderer `render_metadata.json` writes staged and output-local: the HyperFrames/FFmpeg renderer now commits render metadata via the same temporary-file/backup path and replaces symlinks instead of following them outside the output directory.
- Tighten production validation so staged `render_metadata.json` artifacts left behind by interrupted renderer commits, such as `.render_metadata.json_*.tmp/.bak`, invalidate `shand xai validate`.
- Make renderer `normalized/shot_NNN.mp4` writes staged: FFmpeg shot normalization now writes each normalized shot to a temporary file, validates the normalized-shot media contract there, and only then commits it to the canonical normalized path, preserving the previous valid normalized shot when a new normalization fails validation.
- Tighten production validation so staged normalized-shot artifacts left behind by interrupted normalization commits, such as `normalized/.shot_NNN.mp4_*.tmp/.bak`, invalidate `shand xai validate`.
- Make renderer HyperFrames project writes staged: `hyperframes/index.html` and `hyperframes/package.json` now commit as a pair, so package commit failures roll back the generated index instead of leaving a mismatched render project.
- Tighten production validation so staged HyperFrames project artifacts left behind by interrupted project commits, such as `hyperframes/.index.html_*.tmp/.bak` or `hyperframes/.package.json_*.tmp/.bak`, invalidate `shand xai validate`.
- Make renderer `timeline_hyperframes.mp4` writes staged: HyperFrames now renders to a temporary timeline, validates the timeline media contract there, and only then commits it to the canonical timeline path, preserving the previous valid timeline when a new render fails validation.
- Tighten production validation so staged `timeline_hyperframes.mp4` artifacts left behind by interrupted timeline commits, such as `.timeline_hyperframes.mp4_*.tmp/.bak`, invalidate `shand xai validate`.
- Make renderer `output_xai.mp4` writes staged: FFmpeg finalization now writes to a temporary file, validates the final media contract there, and only then commits it to the canonical final output path, preserving the previous valid output when a new finalization fails validation.
- Tighten production validation so staged `output_xai.mp4` artifacts left behind by interrupted final-output commits, such as `.output_xai.mp4_*.tmp/.bak`, invalidate `shand xai validate`.
- Make renderer `preview_frame.jpg` writes staged: preview extraction now writes to a temporary file, validates the JPEG there, and only then commits it to the canonical preview path, preserving the previous valid preview when a new extraction is corrupt.
- Tighten xAI shell smoke/gate output handling so symlinked `OUT_DIR` values, non-directory `OUT_DIR` values, non-directory path components, and symlinked ancestors inside the requested output path are rejected by shared preflight before `mkdir -p`, auth checks, pipeline generation, inspect, validate, or shell summary redirections can write through an external path; existing output leaves are checked the same way as missing leaves.
- Tighten default xAI output handling so `ShandHome/projects` is preflighted before xAI planning when `--output-dir` is omitted; symlinked `ShandHome` values or symlinked nearest existing ancestors fail before model calls or artifact writes.
- Tighten production validation so staged `preview_frame.jpg` artifacts left behind by interrupted preview commits, such as `.preview_frame.jpg_*.tmp/.bak`, invalidate `shand xai validate`.
- Make the final render bundle atomic: `output_xai.mp4`,
  `render_metadata.json`, and `preview_frame.jpg` are staged before any
  canonical final artifact is committed, so metadata or preview commit failures
  roll back the final MP4 instead of leaving mismatched final artifacts.
- Preserve manifest transition metadata in the HyperFrames render project:
  `transition_out` now appears on each generated video clip and production
  validation rejects outputs where a manifest transition is missing from the
  HyperFrames timeline evidence.
- Normalize the first stable transition contract before xAI video generation:
  empty `transition_out` becomes `cut`, `fade` is the only alternate supported
  value, unsupported transition strings fail before shot generation, and
  production validation rejects persisted manifests outside that support set.
- Make HyperFrames timeline behavior deterministic for xAI video clips: the
  generated project now hides inactive clips, applies per-shot opacity on seek,
  supports the first stable `fade` transition in the template runtime, and
  production validation rejects HyperFrames projects missing that visibility
  runtime evidence.
- Tighten the no-network xAI-native dry-run smoke so single-output and batch
  artifacts must include manifest `transition_out`, HyperFrames
  `data-transition-out`, and the visibility/fade runtime hooks; stale
  HyperFrames project templates no longer pass smoke validation.
- Tighten HyperFrames project validation so `hyperframes/index.html` must not
  reference any raw `shots/` files, including extra raw shots outside the
  manifest. HyperFrames consumes only `normalized/shot_NNN.mp4`, and the
  no-network smoke now checks the same invariant for single-output and batch
  artifacts.
- Extend the raw-shot HyperFrames guard to output-local absolute paths as well
  as `../shots/` relative paths; hand-edited projects cannot bypass the
  normalized-only render contract by pointing directly at
  `<outputDir>/shots/shot_NNN.mp4`.
- Extend the same guard to `file://` URL-encoded raw shot references, including
  output directories with spaces; HyperFrames timeline evidence must still use
  normalized project-relative shot paths only.
- Tighten HyperFrames project validation so `hyperframes/index.html` may
  reference only manifest-owned `normalized/shot_NNN.mp4` files; extra
  normalized clips outside the manifest now invalidate production validation,
  and the no-network smoke checks the same allowlist for single-output and
  batch artifacts.
- Tighten the same HyperFrames normalized-shot contract to exact-once
  references: each manifest-owned `normalized/shot_NNN.mp4` may appear only
  once in `hyperframes/index.html`, so stale or hand-edited timelines cannot
  duplicate a valid shot clip and still pass production validation.
- Tighten HyperFrames video-source validation so every `<video src=...>` in
  `hyperframes/index.html` must point to a manifest-owned normalized shot; extra
  external video tags or hand-edited source URLs now invalidate production
  validation, and the no-network smoke checks the same video-source count.
- Make that HyperFrames video-source validation insensitive to HTML tag and
  attribute case, so uppercase `<VIDEO SRC=...>` cannot bypass the
  manifest-owned normalized-shot allowlist.
- Move HyperFrames video-source validation onto an HTML parser instead of
  ad-hoc string scanning; valid manifest-owned normalized sources with normal
  HTML attribute spacing still pass, while external video sources remain
  rejected.
- Tighten HyperFrames runtime-hook validation so visibility/fade timeline hooks
  must appear inside actual `<script>` text; HTML comments containing the same
  hook strings no longer satisfy production validation.
- Tighten HyperFrames composition evidence the same way: `data-composition-id`
  must be present on an actual HTML element with value `xai-video`; comments
  containing that attribute text no longer satisfy production validation.
- Tighten HyperFrames render-spec evidence so composition `data-width`,
  `data-height`, and `data-duration` are read from that parsed xAI composition
  element; comments containing the expected values no longer satisfy production
  validation.
- Tighten HyperFrames subtitle evidence so manifest subtitles must be present on
  actual parsed subtitle elements with the expected `id`, class tokens, timing
  attributes, and text; comments containing the same strings no longer satisfy
  production validation.
- Move HyperFrames video clip timing and transition validation onto parsed
  `<video>` attributes; valid HTML attribute spacing still passes, while
  `data-start`, `data-duration`, and `data-transition-out` must remain on the
  actual manifest-owned video clip.
- Tighten HyperFrames video clip runtime metadata validation so manifest-owned
  video clips must be addressable timeline clips with `id="video-N"`,
  `class="shot-video clip"`, and manifest-order `data-track-index`; bare video
  tags can no longer satisfy the timeline evidence contract.
- Tighten HyperFrames subtitle runtime metadata validation so manifest subtitle
  clips must carry the manifest-order subtitle `data-track-index`, preventing
  subtitle overlays from passing validation when they are not wired to the
  expected HyperFrames track.
- Tighten HyperFrames runtime wiring validation so the script must reference
  each manifest-owned `video-N` clip, and subtitle clips when present, through
  `document.getElementById`; stale runtime scripts can no longer pass by only
  containing generic `window.__hf` / `applyShotVisibility` hooks.
- Tighten runtime evidence validation so JavaScript comments inside `<script>`
  blocks no longer satisfy HyperFrames hook or clip wiring checks; validation
  now evaluates hook/wiring strings after stripping JS line and block comments.
- Tighten HyperFrames seek runtime validation so the generated script must bind
  the xAI timeline object and expose `timeline.seek` through `window.__hf`;
  placeholder runtime objects without a seek binding no longer pass production
  validation.
- Tighten HyperFrames video seek validation so the runtime must update
  `shot.video.currentTime`; scripts that expose a seek API without actually
  driving the normalized xAI MP4 frame position no longer pass production
  validation.
- Tighten HyperFrames seek timing validation so runtime seek evidence must use
  shot-local time (`local = t - start`) and assign a derived `target` to
  `shot.video.currentTime`; scripts that seek every clip with global timeline
  time no longer pass production validation.
- Tighten runtime evidence validation so HyperFrames hook and clip-wiring
  fragments inside JavaScript string/template literals no longer satisfy
  production validation; evidence must start in executable script code.
- Add dedicated validation coverage for JavaScript template-literal-only runtime
  evidence so `shand xai validate` continues to reject fake HyperFrames hooks
  and clip wiring even when the literal can contain unescaped quote characters.
- Tighten runtime evidence validation so JavaScript regex literals no longer
  satisfy HyperFrames hook or clip-wiring checks; the scanner now distinguishes
  regex literals from ordinary division expressions before accepting executable
  code fragments.
- Tighten xAI shot generation orchestration so the pipeline rejects
  metadata-less `ShotGenerator` implementations before any generation call; raw
  xAI shots must come from a `GenerateShotResult` path that returns provider
  request/status metadata.
- Narrow the orchestrator dependency to `ShotResultGenerator`, turning the same
  provider-metadata requirement into a compile-time contract instead of a
  runtime type assertion.
- Narrow `VideoShotGenerator` and the xAI OAuth video adapter to metadata-capable
  video client interfaces, so xAI-native shot generation cannot be wired to a
  data-only video client.
- Align xAI test mocks with the same contract: `GenerateShotResult` and
  `GenerateVideoResult` no longer synthesize metadata results from data-only
  mock functions, preventing tests from hiding missing provider metadata paths.
- Tighten xAI-native resume so cached raw shots require a persisted matching
  `prompt_hash` in the previous manifest; legacy manifest entries without
  `prompt_hash` regenerate instead of being trusted through a fallback hash.
- Tighten whole-manifest resume so planning is skipped only after every cached
  raw shot proves canonical path, matching `prompt_hash`, xAI provider metadata,
  and validator-approved MP4 evidence; incomplete legacy manifests fall back to
  planning instead of carrying stale plan metadata forward.
- Tighten xAI provider status handling so generated and cached shots only pass
  orchestration with reusable statuses (`done` for live output, `dry_run` for
  deterministic fixtures); incomplete statuses such as `pending` regenerate or
  fail before raw shot/manifest artifacts are committed.
- Normalize xAI provider metadata at the orchestration boundary, so reused
  cached metadata is trimmed and status values are lowercased before they are
  persisted into the next manifest and run metadata.
- Normalize xAI OAuth video adapter request metadata before it leaves the
  provider layer: returned `request_id` is trimmed before polling and before
  manifest/run metadata receives it, and returned `video.url` is trimmed before
  download/result metadata uses it.
- Normalize xAI OAuth video request payload inputs before submission; model and
  prompt text are trimmed, and when an optional `image.url` is present,
  surrounding whitespace is removed before the request is sent to xAI.
- Reject empty xAI OAuth video prompts before Hermes token lookup or provider
  HTTP requests, keeping invalid generation input inside the provider adapter
  boundary.
- Normalize xAI OAuth video bearer tokens before HTTP submission; surrounding
  whitespace is removed and empty bearer tokens fail before any provider
  request.
- Normalize xAI OAuth LLM adapter request metadata before submission; configured
  base URLs and model names are trimmed, bearer tokens are trimmed, and empty
  bearer tokens fail before any `/v1/responses` request.
- Normalize xAI-native planner config at the CLI boundary before the LLM client
  is built: `xai_oauth.model` and `xai_oauth.base_url` are trimmed, blank values
  fall back to xAI defaults, and legacy `llm.model` / `llm.base_url` remain
  cleared for this path.
- Normalize xAI backend aliases inside CLI helper boundaries, including static
  render skipping, xAI-native single/batch routing, and video-mode validation;
  passing `xai-oauth` directly to these helpers behaves the same as
  `xai_oauth`.
- Normalize blank legacy asset inputs at the xAI-native routing boundary:
  whitespace-only `--image-dir` values behave as unset, matching
  `validatePipelineVideoMode`, so they cannot silently push a default xAI run
  into a legacy path.
- Normalize xAI-native CLI format input before constructing single/batch
  `RunOptions`: surrounding whitespace is removed, casing is lowered, and blank
  values use the first stable `portrait` default.
- Reject non-portrait xAI-native CLI format input during video-mode validation,
  before planner, shot generator, or renderer dependencies can run; explicit
  legacy backends keep their existing format behavior.
- Reject non-positive batch concurrency during CLI video-mode validation when
  `--episodes > 1`, before xAI-native or legacy batch runners can default or
  consume the value.
- Reject negative `--max-retries` during CLI video-mode validation for all
  backends, before either xAI-native routing or legacy critic rerender logic can
  consume an invalid retry count.
- Reject xAI-native-only force flags on explicit legacy video backends during
  CLI video-mode validation, so `--force-replan` / `--force-regenerate` cannot
  be silently ignored outside the xAI-native route.
- Reject legacy `--series-memory` on `xai_oauth` during CLI video-mode
  validation, so users do not assume the xAI-native batch path uses the legacy
  series repository/summarizer; explicit legacy backends still accept the flag.
- Reject legacy `--multi-speaker`, `--faithful`, `--verbatim`, and
  `--narration` on `xai_oauth` during CLI video-mode validation, because the
  xAI-native path does not run legacy TTS or single-pass story transformation
  modes; explicit legacy backends still accept those flags.
- Reject legacy `--max-retries` on `xai_oauth` during CLI video-mode
  validation, because the current xAI-native path does not run the legacy AI
  Critic Remotion rerender loop; explicit legacy backends still accept it.
- Reject empty xAI OAuth LLM response content after response cleanup; think-tag
  removal and markdown-fence stripping must still leave non-empty content before
  the planner receives it.
- Enforce non-negative target-shot counts in the core xAI-native orchestrator
  before planner, shot generator, or renderer dependencies can run.
- Normalize xAI-native core `RunOptions.Format` in the orchestrator before
  planner calls and resume lookup: surrounding whitespace is removed, casing is
  lowered, and blank values use the first stable `portrait` default.
- Reject empty stories in `internal/xaipipeline.RunBatch` before batch output
  root creation or episode worker startup, so invalid batch input cannot leave
  an empty production directory behind.
- Bind renderer metadata to the xAI manifest: `render_metadata.json` now records
  `project_id` and a canonical manifest SHA-256, and production validation
  rejects missing or mismatched identity before accepting a final bundle.
- Extend the no-network xAI-native dry-run smoke so single-output and batch
  episode fixtures must expose that manifest identity in `render_metadata.json`.
- Improve production-validation diagnostics for persisted render metadata: stale
  metadata now reports all provable identity, path, spec, and size issues in one
  validation response instead of stopping at the first render metadata failure.
- Keep renderer normalization side effects local: HyperFrames still receives
  `normalized/shot_NNN.mp4`, but the source manifest passed into the renderer
  and returned by the orchestrator keeps canonical raw `shots/shot_NNN.mp4`
  paths.
- Enforce the same input boundary in the xAI-native LLM planner: story and
  format are normalized before transformer calls, blank formats default to
  `portrait`, format casing is canonicalized, and empty stories, negative
  target-shot counts, or unsupported formats fail before any xAI LLM request.
- Enforce the xAI-native video shot generator prompt boundary before provider
  calls: shot prompts are trimmed and empty prompts fail before the xAI video
  client is invoked.
- Normalize xAI-native video shot generator options before provider calls:
  missing or non-positive durations default to the stable duration, and
  aspect-ratio/resolution values are trimmed with stable defaults.
- Normalize xAI-native video shot generator result metadata before orchestration:
  returned request IDs are trimmed and returned statuses are lowercased after
  trimming, while persistence validity remains enforced by the orchestrator.
- Normalize xAI-native video model selection at the CLI boundary: `video.model`
  is trimmed, accepted only for the `xai_oauth` / `xai-oauth` provider, and
  blank values fall back to `grok-imagine-video`.
- Persist xAI video model identity into `xai_manifest.json` and
  `xai_run_metadata.json`; prompt hashes now include the selected model, and
  cached shots are regenerated when `video.model` changes.
- Preserve the xAI planning/video-generation boundary for forced regeneration:
  `--force-regenerate` may update `video_model` and regenerate raw shots from a
  matching story manifest without calling the xAI LLM planner again.
- Normalize xAI-native video backend selection at the CLI boundary: backend
  values are trimmed and lowercased before alias handling, so `XAI-OAUTH` and
  `XAI_OAUTH` select the xAI-native HyperFrames/FFmpeg route instead of falling
  through to legacy routing.
- Treat whitespace-only video backend flag/config values as unset at the CLI
  resolution boundary, so blank config noise cannot override the `xai_oauth`
  default or a real configured backend.
- Reject unknown video backend names at the CLI validation boundary before any
  pipeline route is selected; only `xai_oauth`, `remotion`, `nova_reel`,
  `grok_browser`, and `hyperframes` are accepted.
- Enforce the first stable render spec at the renderer boundary before any
  normalizer, HyperFrames executor, or FFmpeg finalizer dependency can run:
  explicit manifest format/dimensions/FPS must resolve to portrait,
  720x1280, 24fps.
- Tighten that renderer boundary so explicit manifest `format` values must
  already be canonical before FFmpeg normalization or HyperFrames execution;
  stale source-of-truth values such as `" portrait "` now fail instead of being
  silently trimmed by the renderer.
- Tighten production validation so explicit manifest `format` values must also
  be canonical; stale persisted manifests such as `" portrait "` now produce a
  manifest contract issue rather than a generic format mismatch.
- Tighten the renderer boundary so every manifest shot must carry a positive
  `duration_sec` before FFmpeg normalization or HyperFrames execution; stale
  zero-duration manifests now fail instead of defaulting to 8 seconds at render
  time.
- Tighten the renderer boundary so explicit per-shot xAI video generation specs
  must already be canonical before media work starts: `aspect_ratio` must be
  `9:16` and `resolution` must be `720p` when present.
- Tighten production validation so explicit manifest `aspect_ratio` and
  `resolution` values must also be canonical; stale persisted manifests such as
  `" 9:16 "` or `" 720p "` now produce canonical contract issues instead of
  generic first-stable-target mismatches.
- Tighten the renderer boundary so manifest prompts must already be canonical
  when present before FFmpeg normalization or HyperFrames execution; stale
  values such as `" shot "` now fail instead of being ignored by the render
  layer.
- Tighten the renderer boundary so manifest subtitles must already be canonical
  before FFmpeg normalization or HyperFrames execution; stale values such as
  `" 第一個字幕 "` now fail instead of producing non-canonical subtitle clips.
- Tighten the renderer boundary so manifest `project_id` must pass the same safe
  single-component contract used by orchestration and production validation
  before HyperFrames project files are written.
- Tighten the renderer boundary so manifest shot indexes must be unique,
  contiguous from 1, and match array order before FFmpeg normalization or
  HyperFrames execution; direct renderer use now shares the same shot-order
  contract as orchestration and production validation.
- Tighten the renderer boundary so manifest `video_path` values are checked
  before any render artifact directory is created; malformed paths now fail
  without leaving `normalized/` or `hyperframes/` behind.
- Tighten production validation so manifest `video_path` values must also be
  canonical before matching `shots/shot_NNN.mp4`; stale persisted values such as
  `" shots/shot_001.mp4 "` now produce canonical contract issues.
- Tighten production validation so manifest `prompt_hash` values must also be
  canonical trimmed 64-character lowercase SHA-256 hex strings before matching
  deterministic shot hashes; stale whitespace-padded hashes now produce
  canonical contract issues instead of generic hash mismatch errors.
- Tighten production validation so run metadata `shot_decisions[].video_path`
  values must also be canonical before matching manifest shot paths; stale
  persisted values such as `" shots/shot_001.mp4 "` now produce canonical
  contract issues instead of generic run metadata mismatch errors.
- Tighten production validation so run metadata `shot_decisions[].prompt_hash`
  values must also be canonical trimmed 64-character lowercase SHA-256 hex
  strings before matching manifest prompt hashes; stale whitespace-padded
  hashes now produce canonical contract issues instead of generic run metadata
  mismatch errors.
- Expose xAI video model identity in both single and batch CLI summaries:
  `pipeline_summary.json` and `batch_summary.json` now include `video_model`,
  so operators do not need to inspect per-episode manifests to confirm which
  xAI video model produced the run.
- Expose xAI story identity in both single and batch pipeline CLI summaries:
  `pipeline_summary.json` and `batch_summary.json` now include root
  `story_hash`, so stdout carries the same story fingerprint as the manifest,
  inspect, and validation surfaces.
- Tighten the live-validation generation gate so fresh single and batch runs
  must emit pipeline stdout summaries with `story_hash` and `video_model`
  before the script proceeds to inspect/validate artifacts.
- Tighten the no-network dry-run smoke so explicit-output resume summaries must
  also emit `story_hash` and `video_model`, proving resume stdout preserves the
  same manifest identity as fresh generation.
- Tighten inspect/validate gate coverage so dry-run single-output inspect,
  strict inspect, and optional live validation `inspect.json` /
  `validation.json` must expose root `story_hash` and `video_model` before the
  gates can pass.
- Add command-level routing regression coverage so `shand pipeline` rejects
  xAI-native legacy-mode flags such as `--skip-llm`, `--image-dir`, `--i2v`,
  and `--max-retries` before any xAI-native runner or legacy provider path can
  initialize.
- Add a no-real-FFmpeg regression for the finalization command contract:
  `FFmpegFinalizer` must consume `timeline_hyperframes.mp4` with `libx264`,
  `yuv420p`, `+faststart`, and `-an` before writing `output_xai.mp4`.
- Add a no-real-FFmpeg regression for raw-shot normalization too:
  `FFmpegShotNormalizer` must map only the first video stream, scale/crop/fps
  to the xAI render spec, encode H.264/yuv420p, strip audio, and faststart the
  committed `normalized/shot_NNN.mp4`.
- Add the matching no-real-FFmpeg regression for preview extraction:
  `FFmpegPreviewExtractor` must read `output_xai.mp4`, seek to 0.25s, extract
  exactly one high-quality frame, and write the staged `preview_frame.jpg`.
- Add a no-real-ffprobe regression for render metadata extraction:
  `FFprobeOutputValidator` must query stream codec/type/size/fps/pixel-format
  plus format duration, parse the exact validated artifact, and report the
  artifact's real file size.
- Expose xAI story and video model identity in single-output production
  validation summaries too: `xai validate <output-dir>` now reports root
  `story_hash` and `video_model`, matching the single inspect, batch inspect,
  and batch validation surfaces.
- Add command-level regression coverage for that identity surface: `shand xai
  inspect` and `shand xai validate` stdout now stay tested for root
  `story_hash` and `video_model` on both single-output and batch roots.
- Tighten batch production validation so a batch root with individually valid
  episodes but mixed `video_model` values is invalid; one batch run must prove a
  single xAI video model identity across all valid `episode_###` outputs.
- Tighten batch inspection with the same model-identity surface: complete batch
  inspect summaries now expose root `video_model`, and mixed complete episode
  model identities make `xai inspect <batch-root>` invalid before production
  validation.
- Expose xAI video model identity in batch production-validation summaries as
  well: `xai validate <batch-root>` now reports root `video_model` from episode
  validation evidence, even when the batch is invalid for other production
  reasons such as dry-run provider metadata.
- Expose and enforce xAI story identity at the batch root as well: batch
  inspect and production-validation summaries now report root `story_hash`, and
  mixed complete/validated episode story hashes invalidate the batch root.
- Add xAI OAuth video polling regression coverage for terminal provider
  statuses: `failed`, `error`, `expired`, and `cancelled` are canonicalized and
  treated as fatal, even if the provider response includes a `video.url`; no
  raw shot bytes or reusable provider metadata are returned unless status is
  `done`.
- Tighten the live xAI `VideoShotGenerator` adapter boundary too: generated
  shot results must include a canonical request id and `xai_status=done` before
  bytes can leave the adapter, while deterministic dry-run metadata remains in
  the separate dry-run generator path.
- Tighten xAI OAuth video download handling so a completed provider request with
  an empty downloaded body is rejected as invalid provider output; `done` status
  alone is not enough to produce reusable raw shot bytes.
- Tighten the HyperFrames/FFmpeg renderer manifest boundary so empty shot
  prompts are rejected before FFmpeg normalization or HyperFrames project
  generation; renderer fixtures now carry non-empty prompts like production
  `xai_manifest.json`.
- Tighten the CLI xAI OAuth video adapter boundary so empty provider video data
  is rejected before it enters the xAI-native shot generator contract, even when
  provider request metadata is present.
- Tighten the same CLI adapter dependency boundary so a missing xAI OAuth video
  client returns a clear error from both byte-only and metadata-capable adapter
  methods instead of panicking.
- Tighten the CLI xAI OAuth video adapter byte-only path too: empty provider
  video bytes are rejected before direct `GenerateVideo` callers can treat them
  as a generated shot.
- Add regression coverage for the xAI-native `LLMPlanner` dependency boundary:
  a missing transformer returns a clear error before any planning request can
  run.
- Tighten the xAI-native `LLMPlanner` response boundary so empty or whitespace
  transformer output is rejected before JSON parsing, with a planner-specific
  error instead of a generic manifest parse failure.
- Tighten the same planner response boundary for JSON `null`: the LLM response
  must decode to a manifest object before it can leave the planner adapter,
  while downstream manifest field rules stay in the orchestrator boundary.
- Tighten the live xAI `VideoShotGenerator` provider-output boundary so a
  `done` request with empty video bytes returns a zero result and a clear
  shot-specific error before it can enter shot staging or direct byte callers.
- Tighten the CLI xAI-native runner dependency boundary so a runner factory that
  returns `(nil, nil)` fails with `xai-native runner is nil` before single or
  batch routing can panic.
- Tighten the single-run xAI-native command result boundary so a runner that
  returns `(nil, nil)` fails with `xai-native result is nil` before JSON summary
  rendering can dereference a nil result.
- Tighten the xAI-native batch runner result boundary so an episode runner that
  returns `(nil, nil)` is tallied as that episode's failure instead of being
  counted as a successful batch result with a nil payload.
- Tighten the CLI xAI-native batch result boundary so a nil batch result now
  fails with `xai-native batch result is nil` instead of being treated as a
  successful no-failure batch.
- Tighten xAI OAuth video download handling so returned video URLs must respond
  with a 2xx status before bytes are accepted; redirects or other non-success
  responses can no longer be mistaken for reusable MP4 data.
- Tighten xAI OAuth LLM response handling so `/v1/responses` must return a 2xx
  status before response JSON can be treated as planner content; redirects or
  other non-success statuses now fail before manifest planning.
- Tighten the default xAI OAuth LLM HTTP client so `/v1/responses` redirects are
  not followed transparently; 3xx responses remain visible to the 2xx status
  guard instead of being converted into a later successful planner response.
- Tighten xAI OAuth LLM response-shape handling so nested Responses API content
  must be `type=output_text` before its text is accepted; refusal or other
  non-output content is no longer forwarded to the manifest planner.
- Tighten xAI OAuth LLM response extraction so whitespace-only top-level
  `output_text` does not mask a valid nested `type=output_text` payload.
- Tighten xAI OAuth LLM URL normalization so the shared `xai_oauth.base_url` may
  be an API root, `/v1`, `/v1/responses`, `/v1/videos`, or the full
  `/v1/videos/generations` endpoint without duplicating path segments.
- Tighten xAI OAuth video URL normalization so the shared `xai_oauth.base_url`
  may be an API root, `/v1`, `/v1/responses`, `/v1/videos`, or the full
  `/v1/videos/generations` endpoint while submit and poll requests still route
  through the canonical `/v1` API root.
- Tighten xAI OAuth video submission so `/videos/generations` must return a 2xx
  status before `request_id` can be accepted; redirects or other non-success
  submit responses no longer enter polling.
- Tighten the default xAI OAuth video HTTP client so submit redirects are not
  followed transparently; 3xx responses remain visible to the 2xx status guard
  instead of being converted into a later successful JSON response.
- Tighten xAI OAuth video polling so `/videos/{request_id}` must return a 2xx
  status before its JSON can be treated as generation state; redirects or other
  non-success poll responses no longer lead to video downloads.
- Tighten xAI OAuth LLM cancellation so an already-canceled context fails before
  OAuth token retrieval, `/v1/responses` request construction, or provider I/O;
  after a response object is returned, cancellation is checked again before the
  response body is read or parsed, so canceled planning cannot turn stale
  provider bytes into a successful manifest.
- Tighten xAI OAuth video cancellation so an already-canceled context fails
  before OAuth token retrieval, request construction, submit calls, polling, or
  downloads; it also rechecks cancellation after submit responses return before
  reading `request_id` bodies or polling, after poll responses return before
  reading provider status bodies, and after polling returns `done` before
  downloading provider video bytes, and after download responses return before
  reading provider video bytes, so canceled generation cannot turn stale provider
  responses into accepted video output.
- Tighten the Hermes xAI OAuth token source so nil contexts are normalized before
  cached-token reads or refresh requests, while already-canceled contexts still
  fail before token file reads and contexts canceled after refresh responses
  return cannot read provider bodies or save refreshed tokens.
- Tighten the same token source again so contexts canceled while refresh bodies
  are being read fail before parsing the refreshed token payload or writing it
  back to Hermes auth storage.
- Restore the Remotion template type gate by aligning `PanelDirective.bg_style`
  with the implemented `blur` and `gradient` render branches, and mirror the
  extended visual directive fields in the Go domain contract.
- Run the local completion gates: `go test ./...`, `go vet ./...`,
  `remotion-template` TypeScript `npx tsc --noEmit`, `/tmp/shand` build,
  no-network xAI-native dry-run smoke, and no-spend live validation gate smoke.
- Tighten the xAI-native CLI wrappers so nil contexts are normalized before
  single or batch runners are called, and already-canceled contexts fail before
  runner factory resolution; rebuild `/tmp/shand`, rerun the no-network dry-run
  smoke, and rerun the no-spend live validation gate smoke.
- Tighten xAI-native shot-cache validation so cached and newly staged raw shots
  are validated against the manifest-derived shot spec before reuse or commit.
  A decodable MP4 with the wrong duration is now rejected at the shot cache
  boundary instead of being reused and failing later during normalization,
  timeline rendering, or final production validation.

Next:

- Run the manual `xAI Native Live Validation` workflow once `HERMES_AUTH_JSON_B64` is configured and record the workflow artifact/run result.

## Acceptance Criteria

- `./shand pipeline --skip-hitl --panels 1` uses only xAI OAuth model calls plus HyperFrames/FFmpeg deterministic rendering.
- No image provider, TTS provider, BGM provider, Remotion render, browser automation, or local model is called in the default path.
- HyperFrames is always used for xAI-native timeline rendering; FFmpeg-only concat is not the default render path.
- FFmpeg/ffprobe is always used for shot normalization, finalization, validation metadata, and preview extraction.
- `./shand xai validate <output-dir>` exits 0 only when the inspected output is complete, the final MP4 matches the manifest render spec, there is no audio stream, and every shot has persisted xAI request/status metadata.
- `./shand xai validate <batch-root>` exits 0 only when every `episode_###` output satisfies the same production validation.
- A 3-shot smoke produces:
  - `xai_manifest.json`
  - `xai_run_metadata.json`
  - `shots/shot_001.mp4` through `shot_003.mp4`
  - normalized shot files
  - final `output_xai.mp4`
  - ffprobe metadata showing 720x1280, 24fps, expected duration, and no audio stream
  - one preview frame
- `go test ./...` passes.
- Re-running the same output directory resumes from valid existing shots.

## Open Questions

- None for the first stable xAI-native path.
