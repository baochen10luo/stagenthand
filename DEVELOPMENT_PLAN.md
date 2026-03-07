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
- Agent：直接呼叫 `POST /checkpoints/:id/approve`（Gin server on `:18080`）

---

## Agent DX (Agent-First CLI 設計)

參考 [Justin Poehnelt 的 CLI for AI Agents 實踐](https://justin.poehnelt.com/posts/rewrite-your-cli-for-ai-agents/)，`shand` 必須將 LLM/Agent 視為第一級使用者：

1. **Raw JSON 優先**：所有指令從 stdin 讀取 JSON 並輸出 JSON（已達成）。
2. **Safety Rails (dry-run)**：所有變更操作皆支援 `--dry-run` 供 Agent 預先驗證（已達成）。
3. **Input Hardening (輸入防護)**：在 `internal/domain` 或 `internal/pipeline` 實作嚴格的 sanitize 邏輯，防止 Agent 幻覺產生目錄穿越攻擊（如 `project_id: ../../../.ssh`）或不可見的控制字元。
4. **Schema Introspection (動態結構內省)**：新增 `shand schema <stage>`，讓 Agent 不需要消耗 Token 查閱靜態 `AGENTS.md`，而是直接在終端機取得最新的 JSON Schema。
5. **Multi-Surface (MCP 支援)**：除 Gin HTTP Server 之外，新增 `shand mcp` 子命令，將 pipeline 步驟暴露為 JSON-RPC tools，讓 Claude/Cursor 零設定接入。
6. **Agent Skills (技能包發布)**：隨開源程式碼發布包含指示與規則的 `SKILL.md` 包，取代傳統 `--help`，防禦 Agent 幻覺。

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
- [ ] `shand schema <stage>`：動態輸出 domain structs 的 JSON Schema
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
- [ ] `shand mcp` 子命令：暴露所有轉換步驟為 MCP tools
- [ ] 提供標準 `SKILL.md` 供 Agent 動態載入
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
  port: 18080               # Gin HTTP，供 agent / Discord bot 呼叫 checkpoint API
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

---

*StagentHand — Part of Castle Studio C3A ecosystem.*
*Binary: `shand` | Module: `github.com/castle-studio-work/stagenthand`*
*開發架構：Claude（施工）+ Codex（審核）+ opencode（執行環境）*
