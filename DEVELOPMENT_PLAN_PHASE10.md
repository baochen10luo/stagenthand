# Phase 10 Development Plan (v2)

> v2 修訂說明：根據架構審查，補入三項修正：
> 1. `DialogueLine` 結構化為 **prerequisite**（所有 feature 共同基礎）
> 2. `VideoFormat` 相關 function 移至 `internal/render/`，不污染 `domain/`
> 3. Series 執行改為「narrative 串行 + production 並行」+ summary HITL + sliding window

---

## Overview

### Phase 10.0 — Prerequisite: Structured Dialogue（所有 feature 的共同基礎）

`Panel.Dialogue string` 重構為 `Panel.DialogueLines []DialogueLine`。每個 `DialogueLine` 帶有 `Speaker`、`Text`、`Emotion`。這是 Multi-Speaker TTS 路由、Series Memory 角色追蹤、AI Critic 對話評估的共同前提。沒有這個基礎，Feature A 只是在 `Characters[0]` 上的 hack。

### Feature A — Multi-Speaker TTS（多角色語音）

每個已登錄角色有自己獨立的 Polly 語音設定（`VoiceID` + SSML 情緒 preset）。`MultiSpeakerAudioBatcher` 按 `DialogueLine.Speaker` 路由每一句對白到對應角色的 TTS client。`MultiSpeakerClient` interface 設計為 provider-agnostic，Polly 是第一個實作；未來接 Azure Neural TTS 或 ElevenLabs 不需改 interface。

### Feature B — Vertical Video / 9:16 Format（垂直影片社群格式）

新增 `--format landscape|portrait` flag。`VideoFormat` type 與尺寸換算函數放在 `internal/render/format.go`（**不在** `internal/domain/`）。Portrait 模式下，PanelSlide 使用 `useVideoConfig()` 動態計算 layout，並對 ken_burns 動效加入 `objectFit: cover` 防黑邊。Image provider 尺寸傳遞有 fallback 策略（provider 不支援時由 Remotion 裁切適配）。

### Feature C — Series Continuity（系列記憶）

多集生產時，**只有 narrative 階段串行**（story→outline→storyboard，因為下一集需要上一集的摘要）；image/audio/render 仍然並行。每集 storyboard 完成後立即生成摘要，並進入 `StageSeriesSummary` HITL checkpoint 供人工校正。注入下一集的 context 採用 **sliding window**（預設最近 3 集完整摘要 + 1 段全系列壓縮大綱），控制 token 消耗。

---

## Phase 10.0 — Prerequisite: Structured Dialogue

### Domain Change

```go
// internal/domain/types.go

// DialogueLine represents a single spoken line by one character.
type DialogueLine struct {
    Speaker string `json:"speaker"`           // character name, "" = narrator
    Text    string `json:"text"`
    Emotion string `json:"emotion,omitempty"` // happy | sad | angry | whisper | neutral
}

// Panel — replace Dialogue string with DialogueLines
type Panel struct {
    // ... existing fields ...
    Dialogue      string         `json:"dialogue,omitempty"`        // DEPRECATED: kept for backward compat
    DialogueLines []DialogueLine `json:"dialogue_lines,omitempty"`  // NEW: structured
    // ...
}
```

`Dialogue` 欄位保留但標記 deprecated，舊的 TTS batcher 繼續讀它（backward compat）。新的 MultiSpeakerAudioBatcher 優先讀 `DialogueLines`，若為空則 fallback 到 `Dialogue`。

### LLM Prompt Change

`PromptStoryboardToPanels` 的 output schema 加入 `dialogue_lines` 欄位，要求 LLM 拆分每句對白並標注說話者與情緒。同時，每個 panel 原則上只有一個主說話者（導演規則：對話場景拆為多個 panel）。

### Files Changed
- `internal/domain/types.go` — 加入 `DialogueLine` struct，`Panel` 加 `DialogueLines`
- `internal/pipeline/stages.go` — `PromptStoryboardToPanels` schema 加入 `dialogue_lines`
- `internal/pipeline/stages_test.go` — 新增 dialogue_lines parse 測試

---

## Feature A — Multi-Speaker TTS

### Architecture Decisions（ISP + DIP）

- **不修改** `audio.Client` interface（`GenerateSpeech(ctx, text string)`）
- 新增 `audio.MultiSpeakerClient` interface：
  ```go
  type MultiSpeakerClient interface {
      GenerateSpeechForLine(ctx context.Context, line domain.DialogueLine) ([]byte, error)
  }
  ```
- 新增 `PollyMultiSpeakerClient`：內部維護 `map[characterName]*PollyCLIClient`，查不到 fallback 到 default voice。`EmotionPresets` map 把 `DialogueLine.Emotion` 轉成 SSML `prosody`/`domain` 標籤
- 新增 `MultiSpeakerAudioBatcher`（在 `internal/pipeline/adapters_multispeaker.go`，不改現有 `AudioClientBatcher`）
- `CharacterMeta` 加入 `VoiceID string` + `EmotionPresets map[string]string`
- `character.Registry` 加入 `GetMeta(ctx, name string) (*CharacterMeta, error)`
- `OrchestratorDeps.Audio AudioBatcher` interface 不變——`MultiSpeakerAudioBatcher` 同樣實作 `AudioBatcher`

### New Files

| 路徑 | 用途 |
|---|---|
| `internal/audio/multispeaker.go` | `MultiSpeakerClient` interface + `PollyMultiSpeakerClient` 實作 |
| `internal/audio/multispeaker_test.go` | table-driven 測試 |
| `internal/audio/mock_multispeaker.go` | `MockMultiSpeakerClient` |
| `internal/pipeline/adapters_multispeaker.go` | `MultiSpeakerAudioBatcher` 實作 `AudioBatcher` |
| `internal/pipeline/adapters_multispeaker_test.go` | table-driven 測試 |

### Modified Files

| 檔案 | 修改內容 |
|---|---|
| `internal/character/registry.go` | `CharacterMeta` 加 `VoiceID`, `EmotionPresets`；`Registry` interface 加 `GetMeta` |
| `internal/character/file_registry.go` | 實作 `GetMeta`；`Register` 寫入新 meta 欄位 |
| `internal/character/mock.go` | 實作 `GetMeta` mock |
| `cmd/character.go` | `register` 子命令加 `--voice-id` flag |
| `cmd/pipeline.go` | 加 `--multi-speaker` flag；按此 flag 選擇注入 `MultiSpeakerAudioBatcher` 或舊的 `AudioClientBatcher` |

### CLI

```
shand pipeline --multi-speaker
  Enable per-character voice routing. Each DialogueLine.Speaker is looked up
  in the character registry for its VoiceID and EmotionPresets.
  Falls back to --language default if speaker not found. (default: false)

shand character register <name>
  --voice-id string   Polly VoiceID (e.g. Zhiyu, Joanna, Takumi)
```

---

## Feature B — Vertical Video / 9:16 Format

### Architecture Decisions（OCP）

- `VideoFormat` type 和尺寸換算放在 `internal/render/format.go`（**不在 domain/**）
- `internal/domain/types.go` 不改動（domain 只含純業務資料）
- Image provider：接受 width/height 參數，若 provider 不支援自訂尺寸，由 `ImageBatcher` 層在取回圖片後 crop/letterbox 到目標比例
- Remotion `PanelSlide.tsx`：portrait 動效防黑邊：image container 加 `objectFit: 'cover'`，ken_burns scale 從 canvas 長邊計算
- `internal/remotion/props.go` 的 `PanelsToProps` 接受 `render.VideoFormat` 參數

### New Files

| 路徑 | 用途 |
|---|---|
| `internal/render/format.go` | `VideoFormat` type + constants + `Dimensions()` method |
| `internal/render/format_test.go` | table-driven 測試 |

### Modified Files

| 檔案 | 修改內容 |
|---|---|
| `internal/image/nanobanana.go` | 加 `width`/`height` 欄位，request body 嘗試傳入尺寸 |
| `internal/image/nanobanana.go` | 若 API 回傳非目標比例，`GenerateImage` 內部 crop 到正確比例 |
| `internal/remotion/props.go` | `PanelsToProps` 接受 `render.VideoFormat`，設定 props width/height |
| `cmd/pipeline.go` | 加 `--format` flag，傳遞到 image factory 和 `PanelsToProps` |
| `cmd/remotion_render.go` | 加 `--format` flag |
| `remotion-template/src/components/PanelSlide.tsx` | portrait 模式：`objectFit: cover`；字幕 `maxWidth`/`fontSize` 用 `useVideoConfig()` 動態計算 |

### CLI

```
shand pipeline --format <landscape|portrait>
  landscape = 1024×576 (default), portrait = 576×1024 (TikTok/Reels/Shorts)
  Controls image generation dimensions and Remotion canvas. (default: "landscape")

shand remotion-render --format <landscape|portrait>
```

---

## Feature C — Series Continuity

### Architecture Decisions（SRP + pipeline parallelism）

#### 執行模型：只串行 narrative 階段

```
Episode 1:  [narrative: story→outline→storyboard] → [summarize+HITL] ─┐
                                                                        ↓ context injected
Episode 2:  [narrative: story→outline→storyboard] → [summarize+HITL] ─┐
                                                                        ↓
Episode 3:  [narrative: ...]
            ↑ narrative is serial

Meanwhile:
Episode 1:  [production: image+audio+render] ← runs in parallel with Ep2 narrative
Episode 2:  [production: ...]
```

`RunBatch` 拆為兩個 goroutine pool：`narrativePool`（concurrency=1）和 `productionPool`（concurrency=`BatchConfig.Concurrency`）。narrative 完成後把任務送進 productionPool。

#### Sliding Window Context

```go
type SeriesContextWindow struct {
    RecentEpisodes []EpisodeMemory // 最近 N 集（預設 3）
    GlobalSummary  string          // 全系列壓縮大綱（由 LLM 每集更新）
}
```

注入到下一集的 prompt 結構：
```
[SERIES_CONTEXT]
Global: <全系列一段壓縮大綱>
Recent:
  Ep1: <key events bullet list>
  Ep2: <key events bullet list>
[/SERIES_CONTEXT]

<原始 story prompt>
```

#### StageSeriesSummary HITL Checkpoint

`domain.CheckpointStage` 加入 `StageSeriesSummary`。每集 storyboard 完成後：
1. `LLMSummarizer` 產生 `EpisodeMemory`
2. 進入 `StageSeriesSummary` checkpoint，使用者可：
   - `shand checkpoint approve <id>` — 採用摘要，繼續下一集
   - `shand checkpoint reject <id> --notes "角色名應是小芸"` — 摘要被標記，手動編輯 `series_memory.json` 後再 approve
3. approve 後才注入下一集 context

### New Files

| 路徑 | 用途 |
|---|---|
| `internal/series/types.go` | `EpisodeMemory`, `CharacterSnapshot`, `SeriesMemory`, `SeriesContextWindow` |
| `internal/series/repository.go` | `Repository` interface（`Load`, `Save`, `Append`） |
| `internal/series/file_repository.go` | `FileRepository`（JSON → `<output-dir>/series_memory.json`） |
| `internal/series/file_repository_test.go` | 測試 |
| `internal/series/summarizer.go` | `Summarizer` interface + `LLMSummarizer` + `PromptExtractEpisodeMemory` + `PromptCompressGlobalSummary` |
| `internal/series/summarizer_test.go` | mock LLM 測試 |
| `internal/series/context.go` | `BuildContextWindow(memory, windowSize int) SeriesContextWindow` pure function |
| `internal/series/context_test.go` | window size / token 估算測試 |
| `internal/series/mock.go` | `MockRepository`, `MockSummarizer` |

### Modified Files

| 檔案 | 修改內容 |
|---|---|
| `internal/domain/types.go` | `CheckpointStage` 加入 `StageSeriesSummary` |
| `internal/pipeline/batch.go` | 拆 narrative/production goroutine pool；加 `SeriesRepo`/`Summarizer`/`WindowSize` 到 `BatchConfig`；注入 context 到下一集 inputData |
| `internal/pipeline/batch_test.go` | series 串接 + 並行 production 測試 |
| `cmd/pipeline.go` | 加 `--series-memory` + `--series-window` flags |

### CLI

```
shand pipeline --episodes N --series-memory
  Enable series continuity. Narrative stages are serialized; production
  (image/audio/render) remains concurrent. Each episode's storyboard
  triggers a series-summary HITL checkpoint before the next episode starts.
  Memory persisted to <output-dir>/series_memory.json. (default: false)

  --series-window int   Number of recent episodes to inject as full context.
                        A compressed global summary is always included.
                        (default: 3)
```

---

## Implementation Order — TDD Sequence

### Phase 10.0 — Prerequisite（先做）

1. `[RED]` `TestDialogueLine_JSONRoundTrip` + `TestPanel_DialogueLinesBackwardCompat`
2. `[GREEN]` `internal/domain/types.go`：加 `DialogueLine`，`Panel` 加 `DialogueLines`
3. `[RED]` `TestPromptStoryboardToPanels_IncludesDialogueLines`（驗證 LLM schema 有 dialogue_lines）
4. `[GREEN]` `internal/pipeline/stages.go` prompt schema 更新
5. `[VERIFY]` `go test ./internal/domain/ ./internal/pipeline/`

### Feature A

6. `[RED]` `TestGetMeta_Found` / `TestGetMeta_NotFound`
7. `[GREEN]` `CharacterMeta.VoiceID/EmotionPresets` + `FileRegistry.GetMeta`
8. `[RED]` `TestPollyMultiSpeaker_RouteByDialogueLine`（含 emotion → SSML 轉換）
9. `[GREEN]` `internal/audio/multispeaker.go`
10. `[RED]` `TestMultiSpeakerBatcher_PerLineRouting` / `TestMultiSpeakerBatcher_FallbackToDialogue`
11. `[GREEN]` `internal/pipeline/adapters_multispeaker.go`
12. `[INTEGRATE]` `cmd/character.go --voice-id` + `cmd/pipeline.go --multi-speaker`
13. `[VERIFY]` `go test ./internal/character/ ./internal/audio/ ./internal/pipeline/`

### Feature B

14. `[RED]` `TestVideoFormat_Dimensions` / `TestVideoFormat_Default`
15. `[GREEN]` `internal/render/format.go`
16. `[RED]` `TestNanoBananaClient_PortraitRequest`
17. `[GREEN]` `internal/image/nanobanana.go` width/height + fallback crop
18. `[INTEGRATE]` `internal/remotion/props.go` + `cmd/pipeline.go --format`
19. `[REMOTION]` `PanelSlide.tsx` objectFit + 動態字幕 layout
20. `[VERIFY]` `go test ./internal/render/ ./internal/image/ ./internal/pipeline/`

### Feature C

21. `[RED]` Series types JSON round-trip
22. `[GREEN]` `internal/series/types.go`
23. `[RED]` `TestFileRepository_LoadSaveAppend`
24. `[GREEN]` `internal/series/file_repository.go`
25. `[RED]` `TestBuildContextWindow_SlidingWindow` / `TestBuildContextWindow_TokenBudget`
26. `[GREEN]` `internal/series/context.go`
27. `[RED]` `TestLLMSummarizer_Summarize` / `TestLLMSummarizer_CompressGlobal`
28. `[GREEN]` `internal/series/summarizer.go`
29. `[RED]` `TestRunBatch_NarrativeSerial_ProductionParallel`
30. `[GREEN]` `internal/pipeline/batch.go` 兩池架構
31. `[INTEGRATE]` `cmd/pipeline.go --series-memory --series-window`
32. `[VERIFY]` `go test -cover ./... >= 80%`

---

## Test Plan

### Phase 10.0 Prerequisite

| Test | 期望 |
|---|---|
| `TestDialogueLine_JSONRoundTrip` | Speaker/Text/Emotion 序列化正確 |
| `TestPanel_DialogueLinesBackwardCompat` | 舊 Dialogue string 仍可讀取，DialogueLines 為空時 fallback |
| `TestPromptStoryboardToPanels_IncludesDialogueLines` | prompt schema 包含 `dialogue_lines` 欄位定義 |

### Feature A

| Test | 期望 |
|---|---|
| `TestGetMeta_Found` | 回傳正確 `CharacterMeta`（含 VoiceID） |
| `TestGetMeta_NotFound` | 回傳 nil, nil |
| `TestPollyMultiSpeaker_RouteByDialogueLine` | 已知 speaker → 正確 voiceID；未知 → fallback |
| `TestPollyMultiSpeaker_EmotionToSSML` | `angry` → `<prosody rate="fast">`；`whisper` → whisper domain |
| `TestMultiSpeakerBatcher_PerLineRouting` | 三句不同 speaker → 三次不同 client 呼叫 |
| `TestMultiSpeakerBatcher_FallbackToDialogue` | `DialogueLines` 空時讀 `Dialogue` |
| `TestMultiSpeakerBatcher_SmartResume` | 已有 mp3 → 跳過生成 |

### Feature B

| Test | 期望 |
|---|---|
| `TestVideoFormat_Landscape` | `Dimensions()` → 1024, 576 |
| `TestVideoFormat_Portrait` | `Dimensions()` → 576, 1024 |
| `TestVideoFormat_Unknown` | fallback → 1024, 576 |
| `TestNanoBananaClient_PortraitRequest` | request body 含正確尺寸 |
| `TestPanelsToProps_Portrait` | props.Width=576, props.Height=1024 |

### Feature C

| Test | 期望 |
|---|---|
| `TestBuildContextWindow_SlidingWindow` | 6 集只取最近 3 集 EpisodeMemory |
| `TestBuildContextWindow_Always_GlobalSummary` | window=0 仍有 GlobalSummary |
| `TestLLMSummarizer_Summarize` | mock LLM → EpisodeMemory 正確 parse |
| `TestLLMSummarizer_CompressGlobal` | mock LLM → GlobalSummary string |
| `TestRunBatch_NarrativeSerial` | ep2 narrative 在 ep1 summarize+approve 後才啟動 |
| `TestRunBatch_ProductionParallel` | ep1+ep2 production 同時執行 |
| `TestRunBatch_NoSeriesRepo_FullConcurrent` | SeriesRepo=nil → 完全並發（原有行為不變） |

---

## Anti-Patterns to Avoid

1. **禁止把 `VideoFormat` 放進 `domain/types.go`**：domain 只含業務資料，output format 是 presentation concern，放 `internal/render/`
2. **禁止 `MultiSpeakerAudioBatcher` 讀 `panel.Characters[0]`**：必須讀 `DialogueLine.Speaker`，不然 Phase 10.0 白做
3. **禁止修改 `audio.Client` interface**：新開 `MultiSpeakerClient`，原有 `AudioClientBatcher` 完全不動
4. **禁止 Series 全串行**：只有 narrative（story→storyboard）串行，production（image+audio+render）必須並行
5. **禁止無上限注入所有集數的摘要**：sliding window 強制執行，`BuildContextWindow` 有 `windowSize` 參數
6. **禁止 `LLMSummarizer` 在 dry-run 呼叫真實 API**：dry-run 回傳 stub `EpisodeMemory`
7. **禁止 `SeriesMemory` 和 `character.Registry` 對同一角色用不同 key**：必須統一用角色的 canonical name（`CharacterMeta.Name`）

---

## Milestone Checklist

### Phase 10.0 — Prerequisite

- [ ] P1: `domain.DialogueLine` struct + `Panel.DialogueLines` 欄位
- [ ] P2: Backward compat：舊 `Dialogue` string 仍可讀
- [ ] P3: `PromptStoryboardToPanels` schema 加入 `dialogue_lines`
- [ ] P4: 相關測試通過

### Feature A — Multi-Speaker TTS

- [ ] A1: `CharacterMeta.VoiceID` + `EmotionPresets` + `FileRegistry.GetMeta`
- [ ] A2: `audio.MultiSpeakerClient` interface（provider-agnostic）
- [ ] A3: `PollyMultiSpeakerClient`（emotion → SSML 轉換）+ 測試
- [ ] A4: `MockMultiSpeakerClient`
- [ ] A5: `MultiSpeakerAudioBatcher`（讀 `DialogueLines`，fallback 到 `Dialogue`）+ 測試
- [ ] A6: `cmd/character.go --voice-id` + `cmd/pipeline.go --multi-speaker`
- [ ] A7: `go test -cover ./internal/character/ ./internal/audio/ ./internal/pipeline/ >= 80%`

### Feature B — Vertical Video

- [ ] B1: `internal/render/format.go`（`VideoFormat` + `Dimensions()`）+ 測試
- [ ] B2: `NanoBananaClient` width/height + fallback crop 策略
- [ ] B3: `internal/remotion/props.go` 接受 `render.VideoFormat`
- [ ] B4: `cmd/pipeline.go --format` + `cmd/remotion_render.go --format`
- [ ] B5: `PanelSlide.tsx`：`objectFit: cover` + portrait 響應式字幕
- [ ] B6: `go test -cover ./internal/render/ ./internal/image/ >= 80%`

### Feature C — Series Continuity

- [ ] C1: `internal/series/types.go`（含 `SeriesContextWindow`）+ JSON 測試
- [ ] C2: `internal/series/file_repository.go` + 測試
- [ ] C3: `internal/series/context.go`（`BuildContextWindow` sliding window）+ 測試
- [ ] C4: `internal/series/summarizer.go`（`LLMSummarizer` + `PromptExtractEpisodeMemory` + `PromptCompressGlobalSummary`）+ 測試
- [ ] C5: `internal/series/mock.go`
- [ ] C6: `domain.StageSeriesSummary` checkpoint stage
- [ ] C7: `internal/pipeline/batch.go` 雙池架構（narrative serial + production parallel）+ 測試
- [ ] C8: `cmd/pipeline.go --series-memory --series-window`
- [ ] C9: `go test -cover ./internal/series/ ./internal/pipeline/ >= 80%`
- [ ] C10: 全專案 `go test -cover ./... >= 80%`
