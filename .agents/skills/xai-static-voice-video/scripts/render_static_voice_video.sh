#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 || $# -gt 5 ]]; then
  echo "usage: $0 <timeline.mp4> <narration.txt> <output-dir> [voice=eve] [language=zh]" >&2
  exit 64
fi

timeline="$1"
narration_file="$2"
output_dir="$3"
voice="${4:-eve}"
language="${5:-zh}"

if [[ ! -f "$timeline" ]]; then
  echo "timeline not found: $timeline" >&2
  exit 66
fi
if [[ ! -f "$narration_file" ]]; then
  echo "narration file not found: $narration_file" >&2
  exit 66
fi

mkdir -p "$output_dir"

shand_bin="${SHAND_BIN:-}"
if [[ -z "$shand_bin" ]]; then
  shand_bin="$output_dir/.shand-xai-voice"
  go build -o "$shand_bin" .
fi

voice_safe="$(printf '%s' "$voice" | tr -c 'A-Za-z0-9_-' '_' | sed 's/^_*//;s/_*$//')"
if [[ -z "$voice_safe" ]]; then
  voice_safe="voice"
fi

audio_path="$output_dir/narration_xai_${voice_safe}.mp3"
final_path="$output_dir/output_xai_voice.mp4"
preview_path="$output_dir/preview_frame.jpg"
probe_path="$output_dir/ffprobe.txt"

"$shand_bin" xai voice probe \
  --text "$(cat "$narration_file")" \
  --voice "$voice" \
  --language "$language" \
  --output "$audio_path" >/tmp/shand-xai-voice-probe.json

video_duration="$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$timeline")"
audio_duration="$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$audio_path")"
pad_duration="$(awk -v a="$audio_duration" -v v="$video_duration" 'BEGIN { p=a-v; if (p < 0) p=0; printf "%.3f", p + 0.250 }')"

ffmpeg -hide_banner -y \
  -i "$timeline" \
  -i "$audio_path" \
  -filter_complex "[0:v]tpad=stop_mode=clone:stop_duration=${pad_duration},setpts=PTS-STARTPTS[v]" \
  -map "[v]" \
  -map 1:a \
  -c:v libx264 \
  -pix_fmt yuv420p \
  -c:a aac \
  -b:a 128k \
  -movflags +faststart \
  "$final_path"

ffmpeg -hide_banner -y \
  -ss 00:00:10 \
  -i "$final_path" \
  -frames:v 1 \
  -update 1 \
  "$preview_path"

ffprobe -v error \
  -show_entries format=duration,format_name \
  -show_entries stream=index,codec_type,codec_name,width,height,r_frame_rate,pix_fmt,sample_rate,channels \
  -of default=noprint_wrappers=1 \
  "$final_path" >"$probe_path"

printf '{"output_video":"%s","audio":"%s","preview":"%s","ffprobe":"%s"}\n' \
  "$final_path" "$audio_path" "$preview_path" "$probe_path"
