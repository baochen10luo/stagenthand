# OpenCLI Grok Video Adapter Patch
# 修正 I2V 模式下影片下載失敗 (blob data not captured) 問題
# 新增三層下載策略：anchor href → Node.js 直接下載 CDN URL → browser fetch → blob interceptor
#
# 安裝方式：
#   cp scripts/patches/opencli_grok_video.js $(npm root -g)/@jackwener/opencli/clis/grok/video.js

