# StagentHand 開發計劃書

> 版本：v0.1
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

## Image Provider 策略

### 主力：nano-banana-2（Gemini API）

- **成本**：Google 免費額度內，現有 API key 直接用
- **特性**：支援多圖輸入（最多 14 張），透過「角色基準圖」達成角色一致性
- **整合**：`uv run scripts/generate_image.py`，輸入 `--prompt --filename -i [ref_images]`
- **限制**：無底層 seed 控制，靠 Gemini 理解力撐著

### 備援：Together AI / Fal.ai

- 定價 $0.01–0.04/張，一集 15 panel 約 $0.15–0.60
- 介面設計留 `base_url` 可配置，任何 OpenAI-compatible endpoint 皆可接入
- Z-Image-Turbo 自架版（ComfyUI API 模式）也可透過此管道接入

### Video：Grok（xAI）

- API 已開放，OpenAI-compatible（`base_url: https://api.x.ai/v1`）
- 接入成本低，VideoClient interface 實作與 OpenAI 幾乎相同
- 列為 Phase 3 後期實作

### 開源設計原則

```
ImageClient interface 不綁死任何 provider
config.yaml 的 base_url 留空讓使用者填自己的 endpoint
→ 自架 Z-Image / ComfyUI / Stable Diffusion API 皆可接入
→ 社群補 provider，shand 本身保持乾淨
```

---

## 架構設計（SOLID）

```
cmd/                    薄層：IO + 依賴注入，不含業務邏輯
internal/
  domain/               純資料結構，零外部依賴
  llm/                  LLMClient interface + OpenAI/Gemini 實作
  image/                ImageClient interface + NanoBanana/Together 實作
  video/                VideoClient interface + Grok 實作
  store/                Repository pattern，JobRepo + CheckpointRepo
  notify/               Notifier interface + Discord 實作
  remotion/             RemotionExecutor interface + exec 實作
  pipeline/             Orchestrator，依賴所有 interface
config/                 viper 載入，~/.shand/config.yaml
remotion-template/      React + Remotion，ShortDrama composition
```

### 核心資料流

```
純文字故事
  ↓ story-to-outline（LLM）
Outline JSON
  ↓ outline-to-storyboard（LLM）
Storyboard JSON
  ↓ storyboard-to-panels（LLM）
Panel[] JSON（含 prompt、character_refs）
  ↓ panels-to-images（ImageClient，goroutine 並發）
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

每個 ⏸ 節點：
1. Discord 通知（附摘要）
2. 寫 Checkpoint record 到 SQLite（status: pending）
3. pipeline 暫停輪詢
4. 任何管道 approve → status: approved → 繼續

**審核管道（三選一，皆指向同一 DB record）：**
- CLI：`shand checkpoint approve <id>`
- Discord bot：回覆觸發 webhook
- Agent：呼叫內部 Gin HTTP endpoint `POST /checkpoints/:id/approve`

---

## 開發階段

### Phase 1 — 骨架（第 1 週）
**目標**：可以跑的 CLI，所有指令有佔位，store 與 config 完整

- [ ] cobra root + 所有 subcommand 佔位（`--dry-run` 支援）
- [ ] viper config 載入（`~/.shand/config.yaml`）
- [ ] domain/types.go（Project / Outline / Storyboard / Panel / Job / Checkpoint / RemotionProps）
- [ ] SQLite store：JobRepository + CheckpointRepository（gorm）
- [ ] `shand status <job-id>`（含 `--wait` 輪詢）
- [ ] `shand checkpoint list/show/approve/reject/wait`
- [ ] Gin HTTP server（checkpoint API endpoint，供 agent 呼叫）
- [ ] 測試覆蓋率 ≥ 80%

**驗收標準**：
```bash
shand status fake-job-id --dry-run  # 輸出假 JSON，exit 0
shand checkpoint list               # 輸出空陣列 JSON
go test -cover ./...                # ≥ 80%
```

---

### Phase 2 — 文字 Skills（第 2 週）
**目標**：完整文字 pipeline 可端到端跑通（dry-run 模式）

- [ ] LLMClient interface + OpenAI-compatible 實作（支援 Gemini via base_url）
- [ ] `shand story-to-outline`（`--episodes`、`--style`、`--lang`）
- [ ] `shand outline-to-storyboard`（`--scenes-per-ep`）
- [ ] `shand storyboard-to-panels`（`--panels-per-scene`，含 character_refs 欄位）
- [ ] Prompt 模板（繁中短劇風格）
- [ ] `--dry-run` 全面覆蓋：所有 skill 不呼叫 API，輸出固定 fixture

**驗收標準**：
```bash
echo "一個程序員愛上了咖啡師的故事" \
  | shand story-to-outline --dry-run \
  | shand outline-to-storyboard --dry-run \
  | shand storyboard-to-panels --dry-run
# 全程輸出合法 JSON，exit 0
```

---

### Phase 3 — 圖像生成（第 3 週）
**目標**：圖像 pipeline 可實際生成圖片

- [ ] ImageClient interface
- [ ] nano-banana-2 實作（exec `uv run scripts/generate_image.py`，支援 `-i` 多圖角色參考）
- [ ] OpenAI-compatible image 實作（Together AI / Fal.ai / 任意 endpoint）
- [ ] `shand panel-to-image`（非同步 job 模式 + `--sync`）
- [ ] `shand panels-to-images`（goroutine 並發，`--concurrency` 可設定）
- [ ] Notifier interface + Discord webhook 實作
- [ ] HITL checkpoint 通知觸發
- [ ] API retry（3 次，指數退避 1s/2s/4s）

**驗收標準**：
```bash
echo '[{"id":"p1","prompt":"一個程序員坐在咖啡廳","character_refs":[]}]' \
  | shand panels-to-images --provider nano-banana --sync
# 輸出含 image_url 的 Panel[] JSON
```

---

### Phase 4 — Remotion 整合（第 4 週）
**目標**：從 panel 到 mp4 全流程打通

- [ ] remotion-template 建立（React + Remotion）
  - `ShortDrama.tsx`：接受 RemotionProps，每 Panel 顯示背景圖 + 底部字幕
  - 淡入淡出轉場，FPS 24，解析度 1024×576
  - 中文字幕支援（字型路徑 config 可設定）
  - videoUrl 存在用 `<Video>`，否則用 `<Img>`
- [ ] `shand storyboard-to-remotion-props`
- [ ] `shand remotion-render`（exec npx remotion render）
- [ ] `shand remotion-preview`（exec npx remotion studio，blocking HITL）

**驗收標準**：
```bash
# 用 Phase 3 輸出的 panels，跑完輸出 mp4
cat panels_with_images.json \
  | shand storyboard-to-remotion-props \
  | shand remotion-render --output ./test.mp4
ls -lh test.mp4  # 存在且 > 0
```

---

### Phase 5 — Pipeline Orchestrator（第 5 週）
**目標**：`shand pipeline` 一行指令端到端，HITL 四節點完整

- [ ] `shand pipeline`（串接所有 skill）
- [ ] HITL 四節點整合（outline / storyboard / images / final）
- [ ] `--skip-hitl` 全自動模式
- [ ] `--resume-from outline|storyboard|images|final`（從任意節點繼續）
- [ ] 中間產物儲存：`~/.shand/projects/<project-id>/`
- [ ] End-to-end 測試（含 mock 所有外部 API）

**驗收標準**：
```bash
echo "一個程序員愛上了咖啡師的故事" \
  | shand pipeline --skip-hitl --dry-run --output ./final.mp4
# exit 0，輸出 project-id 和每個 stage 的 JSON 產物路徑
```

---

## 開發工作流（雙模型）

```
瑪勒列（Claude / sonnet-4-6）   → 施工隊長
  寫測試 → 確認 fail → 寫實作 → 確認 pass → review 請求

gpt-5.2（openai-codex）         → 獨立審核員
  read-only，不被施工方向影響
  輸出 ✅ Ready 或 ⛔ Blocked
  Blocked → 修完同 thread 再驗
```

**每個函數流程**：
1. 寫 `_test.go`，確認 FAIL
2. 寫實作，確認 PASS
3. 請 gpt-5.2 review
4. ✅ → commit；⛔ → 修 → 回 step 3

---

## Config 範例（`~/.shand/config.yaml`）

```yaml
llm:
  provider: openai          # openai | gemini
  model: gemini-3-flash     # 或 gpt-4o
  api_key: ${GOOGLE_API_KEY}
  base_url: ""              # 留空用預設；填入任意 OpenAI-compatible URL

image:
  provider: nano-banana     # nano-banana | openai-compatible
  api_key: ${GOOGLE_API_KEY}
  width: 1024
  height: 576
  concurrency: 3            # panels-to-images 並發數

video:
  enabled: false
  provider: grok            # grok | kling
  api_key: ${XAI_API_KEY}
  base_url: https://api.x.ai/v1

remotion:
  template_path: ./remotion-template
  composition: ShortDrama
  font_path: ""             # 中文字型路徑，留空用系統預設

notify:
  discord_webhook: ${DISCORD_WEBHOOK_URL}

store:
  db_path: ~/.shand/shand.db

server:
  port: 18080               # Gin HTTP server（checkpoint API）
```

---

## 測試策略

| 層 | 策略 |
|---|---|
| domain/types | 序列化/反序列化 round-trip |
| store/ | in-memory mock，不依賴真實 SQLite |
| llm/ image/ video/ | MockClient，不呼叫任何 API |
| pipeline/ | 全 mock，測 orchestration 邏輯 |
| cmd/ | 黑箱 integration test，`--dry-run` 模式 |

**規則**：
- Table-driven tests 優先
- 禁止測試呼叫真實 API
- 每個 PR 必須 `go test -cover ./... ≥ 80%`

---

## 里程碑

| 時間 | 交付物 |
|---|---|
| 第 1 週末 | Phase 1 完成，CLI 骨架可跑，store 完整 |
| 第 2 週末 | Phase 2 完成，文字 pipeline dry-run 端到端通過 |
| 第 3 週末 | Phase 3 完成，實際圖像生成可用 |
| 第 4 週末 | Phase 4 完成，mp4 輸出可用 |
| 第 5 週末 | Phase 5 完成，`shand pipeline` 端到端，準備開源 |

---

## 開源準備（Phase 5 後）

- [ ] LICENSE（MIT）
- [ ] CONTRIBUTING.md
- [ ] 補齊 provider 文件（如何接入自己的 Z-Image endpoint）
- [ ] GitHub Actions CI（`go test -cover ./...`）
- [ ] 搬 repo 到 `castle-studio-work/stagenthand`

---

*StagentHand — Part of Castle Studio C3A ecosystem.*
*Binary: `shand` | Module: `github.com/castle-studio-work/stagenthand`*
