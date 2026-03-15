# StagentHand 開發計劃書

> 版本：v0.2
> 日期：2026-03-02
> 負責人：005 瑪勒列（開發主管）
> 狀態：前期規劃完成，Phase 1 待啟動

---

## 專案定位

**StagentHand（`shand`）** = Stage + Agent + Hand

CLI-first AI 短劇製作 pipeline。在幕後讓製作動起來，不搶鏡。

### 核心哲學
- **Unix philosophy**：每個 skill 做好一件事，stdin/stdout 傳遞 JSON，可任意組合
- **Agent-friendly**：任何能執行 shell 的 agent 直接呼叫，零設定
- **Human-in-the-loop**：四個關鍵節點暫停等待審核，人或 agent 皆可批准
- **開源優先**：provider interface 開放，社群自行接入自己的 API endpoint

---

## 技術棧

| 層 | 技術 | 選型理由 |
|---|---|---|
| 語言 | Go 1.22+ | 單一 binary、跨平台、低資源消耗 |
| CLI | cobra | Go 生態標準，子命令管理成熟 |
| 資料庫 | SQLite (gorm) | 零依賴部署，本機 pipeline state |
| 設定 | viper | flag/env/yaml 優先順序管理 |
| 視頻合成 | Remotion (exec) | 保羅已驗證，中文字幕支援佳 |
| 通知 | Discord webhook | HITL 節點觸發，低延遲 |
| 開發方法 | TDD + SOLID | 測試先行，覆蓋率 ≥ 80% |

---

## 開發工作流：Claude + Codex 雙模型架構

這是整個專案的施工核心，參考保羅分享的雙模型開發工作流文章。

```
瑪勒列（Claude / claude-sonnet-4-6）   → 施工隊長
  寫測試 → 確認 fail → 寫實作 → 確認 pass → 推進流程

gpt-5.2（openai-codex）               → 獨立審核員
  read-only，從不同模型視角審查
  輸出 ✅ Ready 或 ⛔ Blocked
```

### 為什麼兩個模型

Claude 和 Codex 用不同訓練路徑，天然從不同角度看同一段 code。最容易漏掉的不是明顯寫錯的東西，而是「看起來合理、測試剛好沒蓋到」的那種。Codex 的價值就在這裡。

### 工具鏈

```
opencode（施工環境）
  └── claude-sonnet-4-6（主力施工，cliproxyapi）
  └── gpt-5.2（MCP 接入，read-only reviewer，openai-codex）
```

opencode 在 `~/projects/stagenthand/` 目錄下運行，讀取 `.opencode.json` 與 `AGENTS.md`。

### 每個函數的標準流程

```
1. 寫 *_test.go              → 確認 FAIL（紅燈）
2. 寫最小實作                → 確認 PASS（綠燈）
3. 觸發 Codex review         → 等待 ✅ / ⛔
4. ⛔ Blocked → 修 → 回 step 3
5. ✅ Ready → refactor → commit
```

### Codex review 的規則

- 不把結論先餵給它，不用引導式問法
- 讓它自己 `git diff`、`grep`、`cat` 去研究
- 只問：「這個實作有什麼問題？」
- 每次 review 保存 threadId，被擋後改完用 `--continue` 回同條 thread 再驗

### 什麼時候觸發 review

- 每個新函數完成後
- 每個 Phase 完成後（全面 review）
- 任何涉及 interface 定義的改動

---

## Image Provider 策略

### 主力（開發期 + 正式期）：nano-banana-2

**為什麼正式期也用它：**
- Gemini API 免費額度充足，成本幾乎為零
- 支援多圖輸入（最多 14 張）→ 角色一致性透過「角色基準圖」傳入
- 雙語 prompt 支援佳（中英皆可）
- 已在城堡體系驗證，整合風險低

**整合方式：**
```bash
uv run scripts/generate_image.py \
  --prompt "咖啡廳場景，主角坐在窗邊" \
  --filename panel_001.png \
  --resolution 1K \
  -i character_ref_hero.png character_ref_cafe.png
```

**角色一致性設計：**
- `Panel` struct 含 `CharacterRefs []string`（角色基準圖路徑）
- pipeline 第一個 panel 生成後，把角色圖存到 `~/.shand/projects/<id>/refs/`
- 後續 panel 自動帶入該 project 的角色基準圖

### 備援 / 開源社群：OpenAI-compatible endpoint

```yaml
image:
  provider: openai-compatible
  base_url: http://localhost:8188/api  # ComfyUI API
  # 或 https://api.together.xyz/v1   # Together AI $0.01-0.04/張
  # 或任何跑 Z-Image-Turbo 的自架服務
```

`ImageClient` interface 不綁 provider，`base_url` 可配置。開源社群接入自己的 Z-Image / SDXL / 任意 API。

### Video：Grok（xAI）

- API 開放，OpenAI-compatible（`base_url: https://api.x.ai/v1`）
- `VideoClient` 實作與 OpenAI 幾乎相同，只換 `base_url` 和模型名
- 列為 Phase 3 後期實作，預設 `video.enabled: false`

---

## 架構設計（SOLID）

```
cmd/                    薄層：IO + 依賴注入，不含業務邏輯
internal/
  domain/               純資料結構，零外部依賴
  llm/                  LLMClient interface + OpenAI/Gemini 實作 + Mock
  image/                ImageClient interface + NanoBanana/Compatible 實作 + Mock
  video/                VideoClient interface + Grok 實作 + Mock
  store/                Repository pattern：JobRepo + CheckpointRepo + Mock
  notify/               Notifier interface + Discord 實作 + Mock
  remotion/             RemotionExecutor interface + exec 實作 + Mock
  pipeline/             Orchestrator，依賴所有 interface
config/                 viper 載入，~/.shand/config.yaml
remotion-template/      React + Remotion，ShortDrama composition
```

### SOLID 執行原則

| 原則 | 實作方式 |
|---|---|
| Single Responsibility | 每個 package 只負責一個領域，store/ 不含業務邏輯 |
| Open/Closed | 新 provider 只需實作 interface，不改既有代碼 |
| Liskov Substitution | Mock 必須完全可替換真實實作，行為語意相同 |
| Interface Segregation | LLMClient / ImageClient / VideoClient 各自獨立 |
| Dependency Inversion | cmd/ 依賴 interface，具體實作透過 constructor injection |

### 核心資料流

```
純文字故事
  ↓ story-to-outline（LLM）
Outline JSON
  ↓ outline-to-storyboard（LLM）
Storyboard JSON
  ↓ storyboard-to-panels（LLM）
Panel[] JSON（含 prompt、character_refs）
  ↓ panels-to-images（ImageClient goroutine 並發）
Panel[] JSON（含 image_url）
  ↓ storyboard-to-remotion-props
RemotionProps JSON
  ↓ remotion-render（exec npx remotion）
mp4
```

### HITL 四節點

```
story → [outline ⏸] → [storyboard ⏸] → [images ⏸] → [final ⏸] → mp4
```

**三種審核管道（皆指向同一 SQLite Checkpoint record）：**
- CLI：`shand checkpoint approve <id>`
- Discord bot：回覆觸發 webhook → `POST /checkpoints/:id/approve`
- Agent：直接呼叫 `POST /checkpoints/:id/approve`（Gin server on `:28080`）

---

## Agent DX (Agent-First CLI 設計修正版)

參考分析後，我們拒絕過度設計（如內建 MCP Server 或動態 Schema Introspection），回歸實用主義與 Unix 哲學：

1. **Raw JSON 與 Unix Pipe 的堅持**：所有指令從 stdin 讀取 JSON 並輸出 JSON。複雜的過濾留給 `jq`，我們不重新發明輪子。
2. **Safety Rails (dry-run)**：所有變更操作必備 `--dry-run` 供 Agent 預先驗證。
3. **Input Hardening (輸入防護)**：【這是最重要的防線】在 `internal/domain` 實作嚴格的 sanitize 邏輯，防止 Agent 幻覺產生目錄穿越（`../../../.ssh`）、雙重編碼（`%2e%2e`）或注入控制字元。Agent 是**不可信來源**。
4. **Predictable Error Recovery (可預測的錯誤恢復)**：當發生錯誤時，保證返回 non-zero exit code，並且在 stderr 輸出結構化的錯誤訊息，讓 Agent 知道如何 retry。

---

## 開發階段

### Phase 1 — 骨架（第 1 週）
**目標**：可以跑的 CLI，store 與 config 完整，Codex review 流程建立

- [ ] cobra root + 所有 subcommand 佔位（`--dry-run` 支援）
- [ ] viper config 載入（`~/.shand/config.yaml`）
- [ ] `domain/types.go`（Project / Outline / Storyboard / Panel / Job / Checkpoint / RemotionProps）
- [ ] SQLite store：JobRepository + CheckpointRepository（gorm）+ in-memory Mock
- [ ] `shand status <job-id>`（含 `--wait` 輪詢）
- [ ] `shand checkpoint list/show/approve/reject/wait`
- [ ] Gin HTTP server（`POST /checkpoints/:id/approve`，供 agent / Discord bot 呼叫）
- [ ] 實作 Input Hardening（輸入防護）：驗證 ID、路徑與過濾控制字元，防止幻覺攻擊
- [ ] `.opencode.json` + Codex review flow 建立
- [ ] 測試覆蓋率 ≥ 80%

**驗收標準**：
```bash
shand status fake-job-id --dry-run   # 輸出假 JSON，exit 0
shand checkpoint list                # 輸出 [] JSON
go test -cover ./...                 # ≥ 80%
# Codex review Phase 1 → ✅ Ready
```

---

### Phase 2 — 文字 Skills（第 2 週）
**目標**：完整文字 pipeline dry-run 端到端通過

- [ ] LLMClient interface + OpenAI-compatible 實作（Gemini via base_url）+ Mock
- [ ] `shand story-to-outline`（`--episodes`、`--style`、`--lang`）
- [ ] `shand outline-to-storyboard`（`--scenes-per-ep`）
- [ ] `shand storyboard-to-panels`（`--panels-per-scene`，含 `character_refs` 欄位）
- [ ] Prompt 模板（繁中短劇風格，含角色描述 instruction）
- [ ] `--dry-run` 全面覆蓋

**驗收標準**：
```bash
echo "一個程序員愛上了咖啡師的故事" \
  | shand story-to-outline --dry-run \
  | shand outline-to-storyboard --dry-run \
  | shand storyboard-to-panels --dry-run
# 合法 JSON，exit 0，Codex ✅
```

---

### Phase 3 — 圖像生成（第 3 週）
**目標**：nano-banana-2 實際生成圖片，並發管控

- [ ] ImageClient interface + Mock
- [ ] nano-banana-2 實作（exec `uv run`，支援 `-i` 多圖角色參考）
- [ ] OpenAI-compatible image 實作（備援 / 開源社群用）
- [ ] `shand panel-to-image`（async job + `--sync`）
- [ ] `shand panels-to-images`（goroutine 並發，`--concurrency` 可設）
- [ ] 角色基準圖存取邏輯（`~/.shand/projects/<id>/refs/`）
- [ ] Notifier interface + Discord webhook
- [ ] API retry（3 次，指數退避 1s/2s/4s）
- [ ] Grok VideoClient（`video.enabled: false` 預設）

**驗收標準**：
```bash
# 實際呼叫 nano-banana-2 生成一張圖
echo '[{"id":"p1","prompt":"咖啡廳場景","character_refs":[]}]' \
  | shand panels-to-images --provider nano-banana --sync
# Panel[] JSON 含有效 image_url，Codex ✅
```

---

### Phase 4 — Remotion 整合（第 4 週）
**目標**：從 panel 到 mp4 全流程打通

- [ ] `remotion-template/`（React + Remotion）
  - `ShortDrama.tsx`：RemotionProps → 每 Panel 背景圖 + 底部字幕
  - 淡入淡出轉場，FPS 24，1024×576
  - 中文字幕支援（字型路徑 config 可設）
  - videoUrl 有用 `<Video>`，否則用 `<Img>`
- [ ] `shand storyboard-to-remotion-props`
- [ ] `shand remotion-render`（exec npx remotion render）
- [ ] `shand remotion-preview`（exec npx remotion studio，blocking）

**驗收標準**：
```bash
cat panels_with_images.json \
  | shand storyboard-to-remotion-props \
  | shand remotion-render --output ./test.mp4
ls -lh test.mp4   # 存在且 > 0，Codex ✅
```

---

### Phase 5 — Pipeline Orchestrator（第 5 週）
**目標**：`shand pipeline` 一行端到端，開源準備完成

- [ ] `shand pipeline`（串接所有 skill）
- [ ] HITL 四節點完整整合
- [ ] `--skip-hitl` 全自動模式
- [ ] `--resume-from outline|storyboard|images|final`
- [ ] 中間產物：`~/.shand/projects/<project-id>/`
- [ ] End-to-end 測試（全 mock 外部 API）
- [ ] Codex 全專案 review → ✅
- [ ] LICENSE（MIT）、CONTRIBUTING.md、CI（GitHub Actions）
- [ ] 開源發布準備

**驗收標準**：
```bash
echo "一個程序員愛上了咖啡師的故事" \
  | shand pipeline --skip-hitl --dry-run --output ./final.mp4
# exit 0，輸出 project-id + 各 stage 產物路徑
# Codex 全專案 ✅
```

---

### Phase 6 — AWS Bedrock + Amazon Polly TTS（已完成）

- [x] AWS Bedrock LLM client（amazon.nova-pro-v1:0）
- [x] AWS Bedrock Nova Canvas 圖像生成
- [x] Amazon Polly Neural TTS（Zhiyu 中文）+ SSML 格式化（Whisper 效果、角色標籤剝除）
- [x] AudioClientBatcher 整合進 pipeline orchestrator
- [x] 音頻與圖像同步邏輯

---

### Phase 7 — AI Critic + Jamendo BGM + 字幕淨化（已完成）

- [x] AI Critic 多模態視頻評估（AWS Bedrock Nova Pro）
  - 4 維度評分：視覺一致性 / 音視頻同步 / 指令遵守 / 敘事節奏
  - 硬性失敗條件：visual < 8 OR audio_sync < 8 → REJECT
  - 總分 ≥ 32 → APPROVE
- [x] Jamendo API 背景音樂搜索下載（MusicClientBatcher）
- [x] 字幕污染檢測與過濾（VO / Narrator 標籤）
- [x] 動態時長計算（依對白長度 + 戲劇停頓）

---

### Phase 8 — Directives 配置系統 + 智能恢復（已完成）

- [x] Directives 全域渲染配置（StylePrompt 視覺風格注入、BGMTags 音樂標籤）
- [x] PanelDirective 細粒度控制（MotionEffect、TransitionIn/Out、SubtitleEffect）
- [x] 智能恢復機制（Smart Resume）：圖像/音頻/BGM 檔案已存在則跳過生成
- [x] storyboard-to-remotion-props 支援 wrapped panels 格式 + --project-id flag

---

### Phase 9 — 多語言 + 批次 + 角色持久化 + Critic 自動 retry

**目標**：擴展 pipeline 的語言能力、生產規模化、角色一致性與品質保障

#### 功能一：多語言 TTS（P0）

- [ ] 解除 Polly voiceID 硬寫死（Zhiyu），改由 config/flag 控制
- [ ] 語言對應表：zh-TW → Zhiyu/cmn-CN，en-US → Joanna/en-US，ja-JP → Takumi/ja-JP
- [ ] `Directives` 加 `Language string` 欄位
- [ ] `cmd/pipeline.go` 加 `--language` flag
- [ ] `PromptStoryboardToPanels` 注入語言指令

#### 功能二：角色 Embedding 持久化（P1）

- [ ] 新建 `internal/character/` package：`Registry` interface + `FileRegistry` 實作
- [ ] 儲存路徑：`~/.shand/characters/<name>/<uuid>.png`
- [ ] Panel 加 `Characters []string` 欄位，LLM 生成時填充
- [ ] pipeline 自動 lookup/register 角色基準圖
- [ ] 新增 `shand character list/show` subcommand

#### 功能三：AI Critic 自動 retry（P0）

- [ ] `OrchestratorDeps` 加入 `Critic VideoCriticEvaluator`、`MaxRetries int`
- [ ] retry 策略：visual < 8 → StylePrompt 前加 "highly detailed, 8K"；audio < 8 → 調 DuckingDepth；tone < 6 → DurationSec × 1.2
- [ ] `--max-retries` flag（預設 2）
- [ ] stdout JSON 含 `critic_attempts` 和 `critic_approved` 欄位

#### 功能四：批次多集生產（P1）

- [ ] 新增 `internal/pipeline/batch.go`：`RunBatch()` + bounded concurrency（errgroup）
- [ ] `--episodes N` flag、`--batch-concurrency` flag（預設 2）
- [ ] 輸出 JSON 含 episodes[0..N-1] 各自狀態

**驗收標準**：
```bash
echo "機器人的故事" | ./shand pipeline --skip-hitl --language en-US --dry-run
echo "機器人的故事" | ./shand pipeline --skip-hitl --episodes 3 --dry-run
./shand character list
go test -cover ./... # ≥ 80%
```

---

### Phase 9.5 — Agentic 後製（Post-Production）

**目標**：基於已產出的影片，讓 AI Agent 進行外科手術式後製修復，而非全體重生

#### 核心差異

- Phase 9 的 Critic retry = 整條 pipeline 重跑（全體重生）
- Phase 9.5 = 只動問題素材（可能只重生 1 張圖）

#### 4 層後製操作

**Layer A（需呼叫外部 API）：**
- [ ] `regenerate_image`：只重生指定 Panel 圖像
- [ ] `regenerate_audio`：只重生指定 Panel 語音
- [ ] `replace_bgm`：用新 tag 換 BGM

**Layer B（零 API 成本，只改 JSON）：**
- [ ] `patch_dialogue`：修字幕文字
- [ ] `patch_duration`：調整 Panel 時長
- [ ] `patch_panel_directive`：改單一 Panel 轉場/鏡頭效果
- [ ] `patch_global_directive`：改全域 BGM 音量/色彩濾鏡

**Layer C（Render 層）：**
- [ ] `rerender`：只重跑 Remotion，不動素材

#### 新 CLI 設計

```bash
shand postprod evaluate --video v1.mp4 --props remotion_props.json
shand postprod apply    --plan edit_plan.json [--dry-run]
shand postprod rerender --props remotion_props.json
shand postprod loop     --video v1.mp4 --props remotion_props.json --max-iterations 3
```

#### 新增套件

- [ ] `internal/domain/types.go`：新增 `EditOperation`、`EditPlan`、`EditResult`、`PanelRef` 結構
- [ ] `internal/postprod/planner.go`：`LLMEditPlanner`（LLM 把 Evaluation 轉 EditPlan）
- [ ] `internal/postprod/applier.go`：`DefaultEditApplier`（執行每個 EditOperation）
- [ ] `internal/postprod/loop.go`：`PostProdLoop`（自主迴路）
- [ ] `cmd/postprod.go`：cobra subcommand 群組

#### 版本化 Props 管理

```
projects/<project_id>/
  remotion_props.json       ← v1
  remotion_props_v2.json    ← postprod apply 後
  edit_plans/
    edit_plan_v2.json       ← 審計軌跡
  mp4/
    output_v1.mp4
    output_v2.mp4
```

#### 自主迴路決策樹

```
AI Critic → REJECT
    ↓
LLMEditPlanner 分析問題根源
    ↓
劇本/敘事層問題 → Phase 9 全體重生
素材/渲染層問題 → Phase 9.5 外科手術
    ↓
apply → rerender → 再評估 → 收斂或繼續（最多 max-iterations 次）
```

**驗收標準**：
```bash
shand postprod evaluate --video final.mp4 --props remotion_props.json
# 若 REJECT，輸出含 operations[] 的 EditPlan JSON

shand postprod apply --plan edit_plan.json --dry-run
# 輸出計劃動作，不呼叫 API

shand postprod loop --video final.mp4 --props remotion_props.json --max-iterations 3
# 自主收斂，最終輸出 {"converged": true/false, "iterations": N}

go test -cover ./internal/postprod/ # ≥ 80%
```

---

## Config 範例（`~/.shand/config.yaml`）

```yaml
llm:
  provider: openai          # openai-compatible
  model: gemini-3-flash
  api_key: ${GOOGLE_API_KEY}
  base_url: ""              # 留空用預設；可填任意 OpenAI-compatible URL

image:
  provider: nano-banana     # nano-banana | openai-compatible
  api_key: ${GOOGLE_API_KEY}
  width: 1024
  height: 576
  concurrency: 3

video:
  enabled: false            # 預設關閉
  provider: grok
  api_key: ${XAI_API_KEY}
  base_url: https://api.x.ai/v1

remotion:
  template_path: ./remotion-template
  composition: ShortDrama
  font_path: ""             # 中文字型，留空用系統預設

notify:
  discord_webhook: ${DISCORD_WEBHOOK_URL}

store:
  db_path: ~/.shand/shand.db

server:
  port: 28080               # Gin HTTP，供 agent / Discord bot 呼叫 checkpoint API
```

---

## 測試策略

| 層 | 策略 |
|---|---|
| domain/types | 序列化/反序列化 round-trip |
| store/ | in-memory mock，不依賴真實 SQLite |
| llm/ image/ video/ | MockClient，禁止呼叫真實 API |
| pipeline/ | 全 mock，測 orchestration 邏輯 |
| cmd/ | 黑箱 integration，`--dry-run` 模式 |

規則：Table-driven tests 優先。每個 PR：`go test -cover ./... ≥ 80%`。

---

## 里程碑

| 週 | 交付物 | Codex review |
|---|---|---|
| 第 1 週末 | Phase 1：CLI 骨架 + store 完整 | ✅ |
| 第 2 週末 | Phase 2：文字 pipeline dry-run 通過 | ✅ |
| 第 3 週末 | Phase 3：nano-banana-2 實際圖像生成 | ✅ |
| 第 4 週末 | Phase 4：mp4 輸出可用 | ✅ |
| 第 5 週末 | Phase 5：pipeline 端到端 + 開源發布 | ✅ |
| 第 6 週末 | Phase 6：Bedrock + Polly TTS | ✅ |
| 第 7 週末 | Phase 7：AI Critic + BGM | ✅ |
| 第 8 週末 | Phase 8：Directives + Smart Resume | ✅ |
| 第 9 週末 | Phase 9：多語言 + 批次 + 角色 + Critic retry | 待定 |
| 第 10 週末 | Phase 9.5：Agentic 後製 | 待定 |

---

*StagentHand — Part of Castle Studio C3A ecosystem.*
*Binary: `shand` | Module: `github.com/castle-studio-work/stagenthand`*
*開發架構：Claude（施工）+ Codex（審核）+ opencode（執行環境）*
