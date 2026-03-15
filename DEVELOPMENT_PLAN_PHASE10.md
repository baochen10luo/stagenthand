# Phase 10 Development Plan

## Overview

**Feature A — Multi-Speaker TTS（多角色語音）**
目前整個 pipeline 的所有對話均使用單一 Polly 語音（由 `NewPollyCLIClientWithLanguage` 決定）。Phase 10A 的目標是讓每個已登錄角色都有自己獨立的 Polly 聲線，透過 `character.Registry` 中的語音設定（`VoiceID`）在 `AudioClientBatcher.BatchGenerateAudio` 層按 panel 路由到對應角色的 TTS client。這需要擴展 `CharacterMeta` 加入 `VoiceID` 欄位、新增 `MultiSpeakerAudioBatcher` adapter、以及在 `character register` 命令中加入 `--voice-id` 選項。

**Feature B — Vertical Video / 9:16 Format（垂直影片社群格式）**
目前 pipeline 硬寫死 1024×576（橫版）。Feature B 新增 `--format` flag，值為 `landscape`（1024×576，預設）或 `portrait`（576×1024 for TikTok/Reels/Shorts）。尺寸選擇需要傳遞到：image generation（改寬高）、RemotionProps（`width`/`height`）、以及 Remotion template（subtitle 佈局適配）。Remotion 的 `PanelSlide` 需要根據長寬比調整字幕欄位最大寬度與字型大小。

**Feature C — Series Continuity / Series Memory（系列記憶）**
當以 `--episodes N` 批次生產多集時，每集之間相互獨立，沒有情節連貫性。Feature C 建立 `SeriesMemory` 機制：儲存每集的角色描述、關鍵情節事件、世界觀設定，並在後續集數的 outline/storyboard 生成 prompt 中注入前情摘要。這讓多集短劇擁有正確的故事延續性，避免角色性格突變或設定矛盾。

---

## Architecture Decisions

### Feature A — Multi-Speaker TTS

**設計選擇（ISP + DIP）**：
- 不修改 `audio.Client` interface（Single Responsibility，避免破壞現有的 `MockClient` 和 `PollyCLIClient`）
- 新增 `audio.MultiSpeakerClient` interface，包含 `GenerateSpeechForCharacter(ctx, text, characterName string) ([]byte, error)`
- 新增 `PollyMultiSpeakerClient` 實作：內部維護 `map[string]*PollyCLIClient`（key=角色名），查不到則 fallback 到預設語音
- 新增 `MultiSpeakerAudioBatcher`（在 `internal/pipeline/adapters.go` 新增，不破壞現有的 `AudioClientBatcher`）
- `CharacterMeta` 加入 `VoiceID string` 欄位
- `character.Registry` 加入 `GetMeta(ctx context.Context, name string) (*CharacterMeta, error)` 方法

**OrchestratorDeps 擴展**：
`OrchestratorDeps.Audio` 從 `AudioBatcher` 介面保持不變，但 `MultiSpeakerAudioBatcher` 同樣實作 `AudioBatcher` interface，透過 `panel.Characters[0]` 路由語音。

### Feature B — Vertical Video

**設計選擇（OCP）**：
- 新增 `domain.VideoFormat` 常數 type（`landscape` / `portrait`），並加入 `domain.VideoFormatDimensions(format VideoFormat) (width, height int)` pure function
- `image.Client.GenerateImage` signature 不變（prompt + refs），改為在 factory 層按 format 選擇正確尺寸注入 `NovaCanvasClient.width/height`
- `NanoBananaClient` 新增 `width`/`height` 欄位並在 request body 加入尺寸參數
- `OrchestratorDeps` 新增 `Format domain.VideoFormat` 欄位
- Remotion 的 `PanelSlide.tsx`：portrait 模式下透過 `useVideoConfig().width/height` 動態計算而非 hardcode

### Feature C — Series Memory

**設計選擇（SRP + OCP）**：
- 新增 `internal/series/` package，包含：
  - `Memory` struct（純資料，可序列化至 JSON 檔案）
  - `Repository` interface（`Load`/`Save`）+ `FileRepository` 實作（`~/.shand/projects/<id>/series_memory.json`）
  - `Summarizer` interface + `LLMSummarizer` 實作（呼叫 LLM 從 storyboard JSON 提取記憶）
- `pipeline.RunBatch` 接受可選的 `SeriesMemoryRepository`，在每集完成後 append 記憶，在下一集 prompt 前注入
- Orchestrator 本身不知道 series memory（SRP）——注入發生在 `batch.go` 的 episode loop 中，透過修改傳入下一集的 inputData（在 story prompt 前 prepend 前情摘要文字）
- 有 SeriesRepo 時 RunBatch 自動轉為串行（因為下一集依賴上一集的摘要）；無 SeriesRepo 保持並發

---

## Domain Changes

### `internal/domain/types.go` additions

```go
// VideoFormat defines the output video aspect ratio.
type VideoFormat string

const (
    VideoFormatLandscape VideoFormat = "landscape" // 1024×576 (default)
    VideoFormatPortrait  VideoFormat = "portrait"  // 576×1024
)

// VideoFormatDimensions returns the (width, height) for the given format.
func VideoFormatDimensions(format VideoFormat) (width, height int) {
    if format == VideoFormatPortrait {
        return 576, 1024
    }
    return 1024, 576 // default landscape
}
```

### `internal/character/registry.go` — CharacterMeta extension

```go
type CharacterMeta struct {
    Name      string    `json:"name"`
    ImagePath string    `json:"image_path"`
    VoiceID   string    `json:"voice_id,omitempty"` // Polly VoiceID, e.g. "Zhiyu"
    CreatedAt time.Time `json:"created_at"`
}
```

### `internal/series/types.go` — new file

```go
type EpisodeMemory struct {
    Episode    int                 `json:"episode"`
    KeyEvents  []string            `json:"key_events"`
    Characters []CharacterSnapshot `json:"characters"`
    WorldFacts []string            `json:"world_facts"`
}

type CharacterSnapshot struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Motivation  string `json:"motivation"`
    State       string `json:"state"`
}

type SeriesMemory struct {
    SeriesTitle string          `json:"series_title"`
    Episodes    []EpisodeMemory `json:"episodes"`
    UpdatedAt   time.Time       `json:"updated_at"`
}
```

---

## New Packages / Files

### Feature A

| 路徑 | 用途 |
|---|---|
| `internal/audio/multispeaker.go` | `MultiSpeakerClient` interface + `PollyMultiSpeakerClient` 實作 |
| `internal/audio/multispeaker_test.go` | table-driven 測試 |
| `internal/audio/mock_multispeaker.go` | `MockMultiSpeakerClient` for pipeline tests |
| `internal/pipeline/adapters_multispeaker.go` | `MultiSpeakerAudioBatcher` 實作 `AudioBatcher` interface |
| `internal/pipeline/adapters_multispeaker_test.go` | table-driven 測試 |

### Feature B

（無新 package，修改現有檔案）

### Feature C

| 路徑 | 用途 |
|---|---|
| `internal/series/types.go` | `EpisodeMemory`, `CharacterSnapshot`, `SeriesMemory` 資料結構 |
| `internal/series/repository.go` | `Repository` interface（`Load`, `Save`, `Append`） |
| `internal/series/file_repository.go` | `FileRepository` 實作（JSON 持久化） |
| `internal/series/file_repository_test.go` | 測試 |
| `internal/series/summarizer.go` | `Summarizer` interface + `LLMSummarizer` 實作 |
| `internal/series/summarizer_test.go` | mock LLM 測試 |
| `internal/series/mock.go` | `MockRepository` + `MockSummarizer` |

---

## Modified Files

### Feature A

| 檔案 | 修改內容 |
|---|---|
| `internal/character/registry.go` | `Registry` interface 新增 `GetMeta`；`CharacterMeta` 加 `VoiceID` |
| `internal/character/file_registry.go` | 實作 `GetMeta`；`Register` 支援 VoiceID 寫入 meta.json |
| `internal/character/mock.go` | 實作 `GetMeta` mock |
| `cmd/character.go` | `register` 子命令新增 `--voice-id` flag |
| `cmd/pipeline.go` | 新增 `--multi-speaker` flag；構建並注入 `MultiSpeakerAudioBatcher` |

### Feature B

| 檔案 | 修改內容 |
|---|---|
| `internal/domain/types.go` | 新增 `VideoFormat` type + `VideoFormatDimensions` |
| `internal/image/nanobanana.go` | 加 `width`/`height` 欄位，request body 加尺寸 |
| `internal/image/factory.go` | 按 format 注入尺寸到 client constructor |
| `internal/remotion/props.go` | `PanelsToProps` 接受 `format` 參數，調用 `VideoFormatDimensions` |
| `cmd/pipeline.go` | 新增 `--format` flag；傳遞到 image client 和 remotion props |
| `cmd/remotion_render.go` | 新增 `--format` flag |
| `remotion-template/src/components/PanelSlide.tsx` | 引入 `useVideoConfig`；portrait 模式字幕 maxWidth/fontSize 動態調整 |

### Feature C

| 檔案 | 修改內容 |
|---|---|
| `internal/pipeline/batch.go` | `BatchConfig` 加 `SeriesRepo`/`Summarizer`；有 repo 時串行並注入前情摘要 |
| `internal/pipeline/batch_test.go` | 新增 series memory 相關測試 |
| `cmd/pipeline.go` | 新增 `--series-memory` flag；構建 `FileRepository`/`LLMSummarizer` |

---

## Implementation Order — TDD Sequence

### Feature A（建議先做）

1. `[RED]` `TestGetMeta` table-driven test in `file_registry_test.go`
2. `[GREEN]` `CharacterMeta.VoiceID` + `Registry.GetMeta` + `FileRegistry.GetMeta`
3. `[VERIFY]` `go test ./internal/character/`
4. `[RED]` `TestPollyMultiSpeakerClient_GenerateSpeechForCharacter`
5. `[GREEN]` `internal/audio/multispeaker.go`
6. `[VERIFY]` `go test ./internal/audio/`
7. `[RED]` `TestMultiSpeakerAudioBatcher_BatchGenerateAudio`
8. `[GREEN]` `internal/pipeline/adapters_multispeaker.go`
9. `[VERIFY]` `go test ./internal/pipeline/`
10. `[INTEGRATE]` `cmd/character.go` (`--voice-id`) + `cmd/pipeline.go` (`--multi-speaker`)
11. `[E2E]` `echo "test" | ./shand pipeline --skip-hitl --dry-run --multi-speaker`

### Feature B

1. `[RED]` `TestVideoFormatDimensions` table-driven in `internal/domain/`
2. `[GREEN]` `domain.VideoFormat` + `VideoFormatDimensions`
3. `[VERIFY]` `go test ./internal/domain/`
4. `[RED]` `TestNanoBananaClient_PortraitDimensions`
5. `[GREEN]` `internal/image/nanobanana.go` width/height
6. `[VERIFY]` `go test ./internal/image/`
7. `[INTEGRATE]` factory → remotion.PanelsToProps → `cmd/pipeline.go --format`
8. `[REMOTION]` `PanelSlide.tsx` portrait 響應式字幕
9. `[E2E]` `echo "test" | ./shand pipeline --skip-hitl --dry-run --format portrait`（驗證 props width=576, height=1024）

### Feature C（最後做，相依性最高）

1. `[RED]` `TestSeriesMemory_JSONRoundTrip`
2. `[GREEN]` `internal/series/types.go`
3. `[RED]` `TestFileRepository_LoadSaveAppend`
4. `[GREEN]` `internal/series/file_repository.go`
5. `[VERIFY]` `go test ./internal/series/`
6. `[RED]` `TestLLMSummarizer_Summarize` (mock LLM)
7. `[GREEN]` `internal/series/summarizer.go`
8. `[RED]` `TestRunBatch_WithSeriesMemory`
9. `[GREEN]` `internal/pipeline/batch.go` series 串接邏輯
10. `[INTEGRATE]` `cmd/pipeline.go --series-memory`
11. `[E2E]` `echo "機器人" | ./shand pipeline --skip-hitl --dry-run --episodes 3 --series-memory`

---

## CLI Surface

### Feature A

```
shand pipeline
  --multi-speaker    Enable per-character voice routing via character registry.
                     Characters[0] in each panel is looked up for its VoiceID.
                     Falls back to --language default if not found. (default: false)

shand character register <name>
  --voice-id string  Polly VoiceID for this character (e.g. Zhiyu, Joanna, Takumi).
                     Stored in character meta. Used by --multi-speaker mode.
```

### Feature B

```
shand pipeline
  --format string    Output video format: landscape (1024×576, default) or
                     portrait (576×1024 for TikTok/Reels/Shorts).
                     Controls image generation dimensions and Remotion canvas.
                     (default: "landscape")

shand remotion-render
  --format string    Override width/height in props for rendering.
                     (default: "landscape")
```

### Feature C

```
shand pipeline
  --series-memory    Enable series continuity across episodes.
                     Each episode's LLM prompts include a summary of previous
                     episodes' characters, key events, and world-building facts.
                     Requires --episodes > 1. Persisted to <output-dir>/series_memory.json.
                     (default: false)
```

---

## Test Plan

### Feature A

| Test | 描述 | 期望 |
|---|---|---|
| `TestGetMeta_Found` | 已登錄角色回傳正確 meta（含 VoiceID） | `CharacterMeta{VoiceID: "Zhiyu"}`, nil |
| `TestGetMeta_NotFound` | 未登錄角色 | nil, nil |
| `TestPollyMultiSpeaker_KnownCharacter` | 已知角色用正確 voiceID | cmd args 含 `--voice-id Zhiyu` |
| `TestPollyMultiSpeaker_FallbackLanguage` | 未知角色 fallback | 使用語言預設語音 |
| `TestMultiSpeakerBatcher_RouteByCharacter` | 按 `Characters[0]` 路由 | 正確 audio client 被呼叫 |
| `TestMultiSpeakerBatcher_EmptyCharacters` | panel 無角色用預設聲線 | default client 被呼叫 |
| `TestMultiSpeakerBatcher_SmartResume` | 已有 mp3 跳過生成 | audio.Client NOT called |
| `TestCharacterRegister_WithVoiceID` | CLI register 儲存 VoiceID | meta.json 含正確 VoiceID |

### Feature B

| Test | 描述 | 期望 |
|---|---|---|
| `TestVideoFormatDimensions_Landscape` | landscape 尺寸正確 | 1024, 576 |
| `TestVideoFormatDimensions_Portrait` | portrait 尺寸正確 | 576, 1024 |
| `TestVideoFormatDimensions_Default` | 未知值 fallback | 1024, 576 |
| `TestNanoBananaClient_PortraitRequest` | request body 含正確尺寸 | size field 匹配 |
| `TestPanelsToProps_Format` | props 有正確 width/height | props.Width=576, props.Height=1024 |

### Feature C

| Test | 描述 | 期望 |
|---|---|---|
| `TestSeriesMemory_JSONRoundTrip` | 序列化/反序列化 | 完全相同結構 |
| `TestFileRepository_Load_Empty` | 無檔案回傳空 memory | `&SeriesMemory{}`, nil |
| `TestFileRepository_Append` | Append 後正確累積 | `Episodes` 長度正確 |
| `TestLLMSummarizer_PromptContainsStoryboard` | prompt 含 storyboard JSON | mock client 收到正確 prompt |
| `TestRunBatch_SeriesContext_Injected` | 第 2 集 input 含前情摘要 | inputData 含 `[SERIES_CONTEXT]` |
| `TestRunBatch_NoSeriesRepo_Concurrent` | 無 repo 保持並發 | 2 goroutines 並發執行 |
| `TestRunBatch_SeriesRepo_Sequential` | 有 repo 轉串行 | ep2 在 ep1 完成後才開始 |

---

## Anti-Patterns to Avoid

1. **禁止在 `cmd/pipeline.go` 直接判斷角色語音**：路由邏輯必須在 `internal/pipeline/adapters_multispeaker.go` 或 `internal/audio/multispeaker.go`
2. **禁止修改 `audio.Client` interface**：`GenerateSpeech(ctx, text)` 簽名不變。另開 `MultiSpeakerClient` interface（ISP）
3. **禁止在 `domain/types.go` 加入帶副作用的方法**：`VideoFormatDimensions` 是 pure function，不可有 os/io/network 相依
4. **禁止在 Remotion template 硬寫尺寸判斷**：portrait/landscape 判斷必須從 `useVideoConfig()` 動態取得
5. **禁止讓 `RunBatch` 知道 LLM 細節**：透過 `series.Summarizer` interface 取得摘要，不得直接持有 `llm.Client`
6. **禁止 `interface{}` 在 Series Memory 中**：所有欄位必須有明確型別
7. **禁止在 portrait 模式中靜默截斷字幕**：必須動態調整 font size 或加 overflow 處理
8. **禁止 `--series-memory` 在 dry-run 下呼叫真實 LLM 摘要**：dry-run 模式回傳 mock EpisodeMemory
9. **禁止破壞現有 `AudioClientBatcher`**：Feature A 是新增，`--multi-speaker=false` 時繼續用原有 batcher

---

## Milestone Checklist

### Feature A — Multi-Speaker TTS

- [ ] A1: `CharacterMeta.VoiceID` 欄位，`FileRegistry` 讀寫 VoiceID 至 meta.json
- [ ] A2: `character.Registry` interface 新增 `GetMeta`
- [ ] A3: `FileRegistry.GetMeta` 實作 + 測試通過
- [ ] A4: `character.MockRegistry.GetMeta` mock 實作
- [ ] A5: `audio.MultiSpeakerClient` interface（`multispeaker.go`）
- [ ] A6: `PollyMultiSpeakerClient` 實作 + 測試通過
- [ ] A7: `MockMultiSpeakerClient`（`mock_multispeaker.go`）
- [ ] A8: `MultiSpeakerAudioBatcher` 實作 + 測試通過
- [ ] A9: `cmd/character.go` 新增 `--voice-id` flag
- [ ] A10: `cmd/pipeline.go` 新增 `--multi-speaker` flag + 注入邏輯
- [ ] A11: `go test -cover ./internal/character/ ./internal/audio/ ./internal/pipeline/` ≥ 80%
- [ ] A12: E2E dry-run 驗收通過

### Feature B — Vertical Video

- [ ] B1: `domain.VideoFormat` type + constants + `VideoFormatDimensions`
- [ ] B2: domain 測試通過
- [ ] B3: `NanoBananaClient` width/height + request body 尺寸
- [ ] B4: image 測試通過
- [ ] B5: `NovaCanvasClient` portrait 尺寸確認
- [ ] B6: `image.NewClient` factory 按 format 注入尺寸
- [ ] B7: `remotion.PanelsToProps` 接受 format 參數
- [ ] B8: `cmd/pipeline.go` + `cmd/remotion_render.go` 新增 `--format` flag
- [ ] B9: `PanelSlide.tsx` portrait 響應式字幕
- [ ] B10: `go test -cover ./internal/domain/ ./internal/image/ ./internal/pipeline/` ≥ 80%
- [ ] B11: E2E dry-run 驗收：props 含 width=576, height=1024

### Feature C — Series Continuity

- [ ] C1: `internal/series/types.go` + JSON round-trip 測試
- [ ] C2: `internal/series/repository.go` interface
- [ ] C3: `internal/series/file_repository.go` + 測試
- [ ] C4: `internal/series/mock.go`（`MockRepository`）
- [ ] C5: `internal/series/summarizer.go` interface + `PromptExtractEpisodeMemory` 常數
- [ ] C6: `LLMSummarizer` 實作 + 測試（mock LLM）
- [ ] C7: `internal/series/mock.go`（`MockSummarizer`）
- [ ] C8: `BatchConfig` 加 `SeriesRepo`/`Summarizer` 欄位
- [ ] C9: `RunBatch` series 串接邏輯 + 測試
- [ ] C10: `cmd/pipeline.go` 新增 `--series-memory` flag
- [ ] C11: `go test -cover ./internal/series/ ./internal/pipeline/` ≥ 80%
- [ ] C12: E2E dry-run：`--episodes 3 --series-memory` 驗收
- [ ] C13: 全專案 `go test -cover ./...` ≥ 80%
