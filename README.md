# StagentHand (`shand`)

![StagentHand Banner](assets/banner.png)

> CLI-first AI 短劇製作 Pipeline — 讓 AI 員工在幕後讓製作動起來

---

## 是什麼

`shand` 是一套 Unix-philosophy 的 CLI 工具，將 AI 短劇製作拆解為可獨立組合的 skill：

```
故事描述 → 大綱 → 分鏡腳本 → 畫格 → 圖像生成 → Remotion 合成 → mp4
```

每個環節：
- **stdin/stdout 傳遞 JSON** — 任何 agent 或腳本都能直接串接
- **Human-in-the-loop** — 四個關鍵節點暫停等待審核，通過後繼續
- **模型可配置** — LLM / image gen / video gen 都可透過 config 切換

---

## 快速開始

```bash
# 全自動（跳過審核）
echo "一個程序員愛上了咖啡師的故事" | shand pipeline --skip-hitl --output ./ep1.mp4

# 互動模式（有 HITL 審核節點）
echo "..." | shand pipeline --episodes 3 --style "都市愛情"

# 手動一步步執行
echo "..." \
  | shand story-to-outline \
  | shand outline-to-storyboard \
  | shand storyboard-to-panels \
  | shand panels-to-images --sync \
  | shand storyboard-to-remotion-props \
  | shand remotion-render --output ./final.mp4
```

---

## 技術棧

| 層 | 技術 |
|---|---|
| 語言 | Go 1.22+ |
| CLI | cobra |
| 資料庫 | SQLite (gorm) |
| 設定 | viper (`~/.shand/config.yaml`) |
| 視頻合成 | Remotion (exec npx remotion) |
| 通知 | Discord webhook |

---

## Skills

| 指令 | 輸入 | 輸出 |
|---|---|---|
| `shand story-to-outline` | 純文字故事 | Outline JSON |
| `shand outline-to-storyboard` | Outline JSON | Storyboard JSON |
| `shand storyboard-to-panels` | Storyboard JSON | Panel[] JSON |
| `shand panel-to-image` | Panel JSON | Job / Panel+imageURL |
| `shand panels-to-images` | Panel[] JSON | Panel[] + imageURL |
| `shand storyboard-to-remotion-props` | Storyboard JSON | RemotionProps JSON |
| `shand remotion-render` | RemotionProps JSON | mp4 |
| `shand remotion-preview` | — | Remotion Studio (blocking) |
| `shand pipeline` | 純文字故事 | mp4 (全流程) |
| `shand checkpoint` | — | HITL 審核管理 |
| `shand status <job-id>` | — | Job 狀態 |

---

## HITL 節點

```
[story] → outline ⏸ → storyboard ⏸ → images ⏸ → final ⏸ → mp4
```

每個 ⏸ 節點：Discord 通知 → `shand checkpoint approve <id>` → 繼續

---

## Config (`~/.shand/config.yaml`)

```yaml
llm:
  provider: openai       # openai | gemini | anthropic
  model: gpt-4o
  api_key: ${OPENAI_API_KEY}
  base_url: ""           # 留空使用預設，可填 OpenAI-compatible URL

image:
  provider: flux         # flux | imagen3 | sdxl
  api_key: ${FLUX_API_KEY}
  width: 1024
  height: 576

video:
  enabled: false
  provider: kling

remotion:
  template_path: ./remotion-template
  composition: ShortDrama

notify:
  discord_webhook: ${DISCORD_WEBHOOK_URL}

store:
  db_path: ~/.shand/shand.db
```

---

## 開發狀態

- [ ] Phase 1 — CLI 骨架 + config + SQLite + status/checkpoint
- [ ] Phase 2 — LLM interface + 文字 skills
- [ ] Phase 3 — image gen + 並發 + Discord notify
- [ ] Phase 4 — Remotion template + render/preview
- [ ] Phase 5 — pipeline orchestrator + HITL + e2e test

---

## 專案命名

**StagentHand** = Stage + Agent + Hand

舞台工作人員。在幕後讓製作動起來，不搶鏡。

Binary：`shand`

---

*Part of Castle Studio C3A ecosystem.*
