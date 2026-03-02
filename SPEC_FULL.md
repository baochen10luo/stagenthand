# StagentHand 完整規格文件
建檔：2026-03-01 | 更新：2026-03-02 | 負責人：005 瑪勒列

## 背景與目標
參考 waoowaoo（https://github.com/waoowaooAI/waoowaoo）與 huobao-drama（https://github.com/chatfire-AI/huobao-drama）
從頭用 Go 實作 CLI-first AI 短劇製作 pipeline。不 fork，以其為規格參考重新設計。

## 核心哲學
- Unix philosophy：每個 skill stdin/stdout JSON
- Agent-friendly：任何能執行 shell 的 agent 都可呼叫
- Human-in-the-loop：關鍵節點暫停等待審核
- 模型可配置：每個環節 AI provider 可透過 config 切換

## 技術棧
- Go 1.22+ / cobra / Gin / SQLite(gorm) / viper
- Remotion（exec npx remotion）/ Discord webhook

## Image / Video Provider（確定，2026-03-02 保羅指示）
- 主力圖片：nano-banana-2（Gemini API，OpenClaw skill 封裝）
  - 角色一致性：Panel struct 保留 CharacterRefs []string，傳入角色基準圖
  - 多圖輸入最多 14 張
- 影片：Grok image-to-video（xAI API，$0.05/sec，Phase 3 後期接入）
- 備援：不設置。只實作以上兩個。

## CLI 全域規則
- binary 名稱：shand
- stdin：接受 JSON（無 --input 時）
- stdout：輸出 JSON（純資料）
- stderr：所有 log、進度、錯誤
- exit code：0=成功 1=失敗 2=等待HITL
- --dry-run：所有指令支援

## Skills
shand story-to-outline
shand outline-to-storyboard
shand storyboard-to-panels
shand panel-to-image
shand panels-to-images
shand storyboard-to-remotion-props
shand remotion-render
shand remotion-preview
shand pipeline
shand status <job-id>
shand checkpoint list/show/approve/reject/wait

## HITL 四節點
Stage 1: outline      - story-to-outline 完成後暫停
Stage 2: storyboard   - outline-to-storyboard 完成後暫停
Stage 3: images       - panels-to-images 全部完成後暫停
Stage 4: final        - remotion preview 後暫停 → approve → render mp4

## 目錄結構
stagenthand/
  cmd/           cobra subcommands（薄層）
  internal/
    domain/      types.go（純資料結構，零依賴）
    llm/         client.go / openai.go / gemini.go / mock.go
    image/       client.go / nanobanana.go / mock.go
    video/       client.go / grok.go / mock.go
    store/       db.go / job.go / checkpoint.go / mock.go
    notify/      notifier.go / discord.go / mock.go
    remotion/    executor.go / exec.go / mock.go
    pipeline/    orchestrator.go / stages.go
  config/default.yaml
  remotion-template/src/

## Config (~/.shand/config.yaml)
llm.provider / llm.model / llm.api_key / llm.base_url
image.provider / image.api_key / image.width / image.height
image.character_refs_dir
video.enabled / video.provider / video.api_key
remotion.template_path / remotion.composition
notify.discord_webhook
store.db_path

優先順序：flag > 環境變數 > config.yaml > 預設值

## Remotion Template
ShortDrama.tsx：每 Panel 顯示背景圖+字幕，淡入淡出轉場
FPS：24，解析度：1024x576

## 實作順序
Phase 1（第1週）：cobra骨架 + config(viper) + domain/types + SQLite(gorm) + status/checkpoint + Gin HTTP :18080
Phase 2（第2週）：LLM interface + story-to-outline/outline-to-storyboard/storyboard-to-panels + dry-run
Phase 3（第3週）：image interface (nanobanana) + panel-to-image/panels-to-images + Discord notify + Grok video
Phase 4（第4週）：remotion-template + storyboard-to-remotion-props + render/preview
Phase 5（第5週）：pipeline orchestrator + HITL四節點 + e2e test + 開源準備

## 注意事項
- 所有 API 呼叫：retry 3次，指數退避
- panels-to-images 並發數可設定
- pipeline 支援 --resume-from outline|storyboard|images|final
- 中間產物：~/.shand/projects/<project-id>/
- log：slog，--verbose 開 debug level
- nano-banana-2 整合：透過 OpenClaw skill 呼叫（exec），角色一致性由 CharacterRefs 傳入基準圖
- Grok video：xAI API，OpenAI-compatible endpoint，$0.05/sec
