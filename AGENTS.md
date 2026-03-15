# StagentHand — PROJECT KNOWLEDGE BASE

**Binary**: `shand`
**Module**: `github.com/baochen10luo/stagenthand`
**Language**: Go 1.22+

---

## OVERVIEW

CLI-first AI 短劇製作 pipeline。Unix philosophy：每個 skill 做好一件事，stdin/stdout 傳遞 JSON。
完整規格見 `SPEC_FULL.md`。

---

## ARCHITECTURE — SOLID 原則執行標準

### Single Responsibility
- 每個 `internal/` package 只負責一個領域
- `store/` 只管持久化，不含業務邏輯
- `llm/` 只管 LLM 呼叫，不管 prompt 組裝（prompt 在 cmd/ 層）
- `pipeline/` 只管 orchestration，不管個別 skill 實作

### Open/Closed
- 所有外部服務用 interface 定義：`LLMClient`、`ImageClient`、`VideoClient`
- 新增 provider（e.g. Claude）只需實作 interface，不改既有代碼
- `store/` 用 Repository pattern，DB 實作可替換

### Liskov Substitution
- `MockLLMClient` 必須完全可替換 `OpenAIClient`，行為語意相同
- Mock 實作放 `*_test.go` 或 `internal/*/mock.go`

### Interface Segregation
- `LLMClient` 不包含 image gen 方法
- `ImageClient` 不包含 LLM 方法
- `CheckpointRepository` 與 `JobRepository` 分開定義

### Dependency Inversion
- cmd/ 層依賴 interface，不依賴具體實作
- 具體實作透過 constructor injection 注入
- 禁止在 cmd/ 層直接 `new(OpenAIClient)`

---

## STRUCTURE

```
cmd/                    # cobra subcommands（薄層，只做 IO + 注入）
  root.go               # root command + global flags
  story_to_outline.go
  outline_to_storyboard.go
  storyboard_to_panels.go
  panel_to_image.go
  panels_to_images.go
  storyboard_to_remotion.go
  remotion_render.go
  remotion_preview.go
  pipeline.go
  checkpoint.go
  status.go

internal/
  domain/               # 純資料結構，零依賴
    types.go            # Project, Outline, Episode, Storyboard, Scene, Panel, Job, Checkpoint, RemotionProps
  llm/
    client.go           # LLMClient interface
    openai.go           # OpenAIClient（實作）
    gemini.go           # GeminiClient（實作）
    mock.go             # MockLLMClient（測試用）
  image/
    client.go           # ImageClient interface
    flux.go
    mock.go
  video/
    client.go           # VideoClient interface
    kling.go
    mock.go
  store/
    db.go               # gorm + SQLite setup, DB interface
    job.go              # JobRepository interface + gorm 實作
    checkpoint.go       # CheckpointRepository interface + gorm 實作
    mock.go             # in-memory mock（測試用）
  notify/
    notifier.go         # Notifier interface
    discord.go          # DiscordNotifier
    mock.go
  remotion/
    executor.go         # RemotionExecutor interface
    exec.go             # exec npx remotion 實作
    mock.go
  pipeline/
    orchestrator.go     # Pipeline struct，依賴所有 interface
    stages.go           # 四個 HITL stage 邏輯

config/
  loader.go             # viper config 載入
  default.yaml          # 預設值

remotion-template/      # React + Remotion 專案
```

---

## TDD 規則（強制）

**測試先行。每個函數，測試在實作前寫。**

1. 寫 `*_test.go` → 確認 test fail → 寫實作 → 確認 test pass → refactor
2. 每個 interface 的 Mock 必須在對應的 `mock.go` 裡，不散落在 `*_test.go`
3. 測試覆蓋率目標：**≥ 80%**（`go test -cover ./...`）
4. Table-driven tests 優先（`[]struct{ name, input, want }`）
5. 禁止在測試裡呼叫真實 API — 全部用 Mock

---

## CONVENTIONS

### 命名
- interface：`LLMClient`、`JobRepository`（動詞+名詞或名詞+Repository）
- 具體實作：`OpenAIClient`、`GormJobRepository`
- Mock：`MockLLMClient`、`MockJobRepository`
- constructor：`NewOpenAIClient(cfg Config) LLMClient`（回傳 interface，不回傳具體型別）

### 錯誤處理
- 業務錯誤用 sentinel error：`var ErrCheckpointNotFound = errors.New("checkpoint not found")`
- 禁止 `panic`（除非是 init 階段的致命錯誤）
- 所有 error 向上傳遞，在 cmd/ 層統一處理

### IO 規則（CLI）
- stdout：純 JSON 輸出（`encoding/json`）
- stderr：所有 log（`log/slog`，支援 `--verbose` 開 debug）
- exit code：0=成功 1=失敗 2=等待HITL

### API 呼叫
- 所有外部 API：retry 3 次，指數退避（1s → 2s → 4s）
- timeout 必須設定（預設 30s）
- 速率限制錯誤（429）需識別並等待

### 設定優先順序
flag > 環境變數 > `~/.shand/config.yaml` > 預設值

---

## ANTI-PATTERNS（禁止）

- 禁止在 `internal/` 裡直接讀 CLI flags（分層污染）
- 禁止在 cmd/ 層寫業務邏輯（薄層原則）
- 禁止 global state（除了 config 初始化）
- 禁止超過 3 層縮進（Linus 準則）
- 禁止在 `domain/types.go` 加任何方法或依賴（純資料結構）
- 禁止 `interface{}` 或 `any`（除非確實必要，必須加註解說明）

---

## COMMANDS

```bash
# 開發
go test ./...                    # 跑所有測試
go test -cover ./...             # 含覆蓋率
go test -run TestXxx ./internal/store/  # 跑特定測試
go build -o shand ./cmd/         # 編譯

# dry-run 驗證
echo '{"title":"test"}' | ./shand story-to-outline --dry-run

# Phase 驗證（每個 phase 完成後跑）
go test -cover ./... && echo "✅ Phase OK"
```

---

## DEVELOPMENT WORKFLOW（雙模型）

1. **我（Claude / sonnet-4-6）** 主導施工：寫測試 → 寫實作 → 驗證
2. **gpt-5.2（openai-codex）** 獨立 review：read-only，不被施工方向影響
3. Review 通過（✅ Ready）才推進下一個函數或 phase
4. Review 被擋（⛔ Blocked）→ 修完同一條 thread 再驗

---

## PHASE STATUS

- [x] Phase 1：cobra骨架 + config(viper) + domain/types + SQLite(gorm) + status/checkpoint
- [x] Phase 2：LLM interface + story-to-outline/outline-to-storyboard/storyboard-to-panels + dry-run
- [x] Phase 3：image interface + panel-to-image/panels-to-images + Discord notify
- [x] Phase 4：remotion-template + storyboard-to-remotion-props + render/preview
- [x] Phase 5：pipeline orchestrator + HITL四節點 + e2e test
- [x] Phase 6：AWS Bedrock LLM/Image + Amazon Polly TTS + 音頻同步
- [x] Phase 7：AI Critic（多模態視頻評估）+ Jamendo BGM + 字幕淨化 + 動態時長
- [x] Phase 8：Directives 配置系統（StylePrompt / BGMTags）+ 智能恢復機制

**Current：Phase 9（規劃中）**
