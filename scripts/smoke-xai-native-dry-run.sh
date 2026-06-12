#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT_DIR/scripts/lib/xai-output-dir.sh"

SHAND_BIN="${SHAND_BIN:-$ROOT_DIR/shand}"
STORY="${STORY:-機器人找到了一朵會發光的花}"
OUT_DIR_CREATED=0
if [[ -z "${OUT_DIR:-}" ]]; then
  OUT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/shand-xai-native-smoke-XXXXXX")"
  OUT_DIR_CREATED=1
fi
INVALID_DIR=""

cleanup() {
  local status=$?
  if [[ "$status" -eq 0 ]]; then
    if [[ "${KEEP_SMOKE_OUTPUTS:-0}" != "1" ]]; then
      if [[ "$OUT_DIR_CREATED" == "1" ]]; then
        rm -rf "$OUT_DIR"
        echo "xAI-native smoke artifacts cleaned"
      else
        echo "xAI-native smoke artifacts kept in $OUT_DIR"
      fi
      if [[ -n "$INVALID_DIR" ]]; then
        rm -rf "$INVALID_DIR"
      fi
    else
      echo "xAI-native smoke artifacts kept in $OUT_DIR"
      if [[ -n "$INVALID_DIR" ]]; then
        echo "xAI-native invalid fixture kept in $INVALID_DIR"
      fi
    fi
  else
    echo "xAI-native smoke failed; artifacts kept in $OUT_DIR" >&2
    if [[ -n "$INVALID_DIR" ]]; then
      echo "xAI-native invalid fixture kept in $INVALID_DIR" >&2
    fi
  fi
  exit "$status"
}
trap cleanup EXIT

assert_mp4_magic() {
  local file="$1"
  local magic
  magic="$(dd if="$file" bs=1 skip=4 count=4 2>/dev/null)"
  if [[ "$magic" != "ftyp" ]]; then
    echo "smoke artifact is not MP4-shaped: $file" >&2
    exit 1
  fi
}

assert_jpeg_magic() {
  local file="$1"
  local magic
  magic="$(od -An -tx1 -N2 "$file" | tr -d ' \n')"
  if [[ "$magic" != "ffd8" ]]; then
    echo "smoke artifact is not JPEG-shaped: $file" >&2
    exit 1
  fi
}

assert_hyperframes_no_raw_shot_references() {
  local index_file="$1"
  local output_dir="$2"
  if grep -Fq '../shots/' "$index_file"; then
    echo "HyperFrames project must not reference relative raw xAI shots: $index_file" >&2
    exit 1
  fi
  local absolute_raw_dir
  absolute_raw_dir="$(cd "$output_dir" && pwd -P)/shots/"
  if grep -Fq "$absolute_raw_dir" "$index_file"; then
    echo "HyperFrames project must not reference absolute raw xAI shots: $index_file" >&2
    exit 1
  fi
  local absolute_raw_file_url
  absolute_raw_file_url="file://${absolute_raw_dir// /%20}"
  if grep -Fq "$absolute_raw_file_url" "$index_file"; then
    echo "HyperFrames project must not reference file URL raw xAI shots: $index_file" >&2
    exit 1
  fi
}

assert_hyperframes_normalized_references() {
  local index_file="$1"
  shift
  local expected_refs=""
  local expected_count=0
  local ref
  for ref in "$@"; do
    expected_refs+="../normalized/$ref "
    expected_count=$((expected_count + 1))
  done
  local actual_refs
  actual_refs="$(grep -o '\.\./normalized/shot_[0-9][0-9][0-9]\.mp4' "$index_file" | tr '\n' ' ')"
  if [[ "$actual_refs" != "$expected_refs" ]]; then
    echo "HyperFrames normalized references mismatch in $index_file" >&2
    echo "expected: $expected_refs" >&2
    echo "actual:   $actual_refs" >&2
    exit 1
  fi
  local actual_video_count
  actual_video_count="$(grep -Eio '<video[^>]*src="[^"]*"' "$index_file" | wc -l | tr -d ' ')"
  if [[ "$actual_video_count" != "$expected_count" ]]; then
    echo "HyperFrames video source count mismatch in $index_file" >&2
    echo "expected: $expected_count" >&2
    echo "actual:   $actual_video_count" >&2
    exit 1
  fi
}

assert_render_metadata_identity() {
  local output_dir="$1"
  local manifest_file="$output_dir/xai_manifest.json"
  local metadata_file="$output_dir/render_metadata.json"
  local project_id
  project_id="$(sed -n 's/.*"project_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$manifest_file" | head -n 1)"
  if [[ -z "$project_id" ]]; then
    echo "manifest project_id missing in $manifest_file" >&2
    exit 1
  fi
  if ! grep -Fq "\"project_id\": \"$project_id\"" "$metadata_file"; then
    echo "render metadata project_id does not match manifest in $metadata_file" >&2
    exit 1
  fi
  if ! grep -Eq '"manifest_hash"[[:space:]]*:[[:space:]]*"[0-9a-f]{64}"' "$metadata_file"; then
    echo "render metadata manifest_hash is missing or non-canonical in $metadata_file" >&2
    exit 1
  fi
}

if [[ ! -x "$SHAND_BIN" ]]; then
  echo "missing executable: $SHAND_BIN" >&2
  echo "build it first: go build -o shand ." >&2
  exit 1
fi

validate_xai_output_dir_path "$OUT_DIR" "xAI-native dry-run smoke output" "OUT_DIR"
mkdir -p "$OUT_DIR"
if [[ -n "$(find "$OUT_DIR" -mindepth 1 -print -quit)" ]]; then
  echo "OUT_DIR must be empty for this smoke: $OUT_DIR" >&2
  exit 1
fi

echo "Running xAI-native dry-run smoke in $OUT_DIR"

printf '%s' "$STORY" \
  | "$SHAND_BIN" --dry-run pipeline --skip-hitl --panels 3 --output-dir "$OUT_DIR" \
  > "$OUT_DIR/pipeline_summary.json"

required_files=(
  "$OUT_DIR/pipeline_summary.json"
  "$OUT_DIR/xai_manifest.json"
  "$OUT_DIR/xai_run_metadata.json"
  "$OUT_DIR/render_metadata.json"
  "$OUT_DIR/shots/shot_001.mp4"
  "$OUT_DIR/shots/shot_002.mp4"
  "$OUT_DIR/shots/shot_003.mp4"
  "$OUT_DIR/normalized/shot_001.mp4"
  "$OUT_DIR/normalized/shot_002.mp4"
  "$OUT_DIR/normalized/shot_003.mp4"
  "$OUT_DIR/timeline_hyperframes.mp4"
  "$OUT_DIR/output_xai.mp4"
  "$OUT_DIR/preview_frame.jpg"
  "$OUT_DIR/hyperframes/index.html"
  "$OUT_DIR/hyperframes/package.json"
)

for file in "${required_files[@]}"; do
  if [[ ! -s "$file" ]]; then
    echo "missing or empty smoke artifact: $file" >&2
    exit 1
  fi
done
for file in \
  "$OUT_DIR/shots/shot_001.mp4" \
  "$OUT_DIR/shots/shot_002.mp4" \
  "$OUT_DIR/shots/shot_003.mp4" \
  "$OUT_DIR/normalized/shot_001.mp4" \
  "$OUT_DIR/normalized/shot_002.mp4" \
  "$OUT_DIR/normalized/shot_003.mp4" \
  "$OUT_DIR/timeline_hyperframes.mp4" \
  "$OUT_DIR/output_xai.mp4"; do
  assert_mp4_magic "$file"
done
assert_jpeg_magic "$OUT_DIR/preview_frame.jpg"
assert_render_metadata_identity "$OUT_DIR"
grep -Eq '"story_hash":"[0-9a-f]{64}"' "$OUT_DIR/pipeline_summary.json"
grep -q '"video_model":"grok-imagine-video"' "$OUT_DIR/pipeline_summary.json"
grep -q '"video_model": "grok-imagine-video"' "$OUT_DIR/xai_manifest.json"
grep -q '"video_model": "grok-imagine-video"' "$OUT_DIR/xai_run_metadata.json"
grep -q '"transition_out": "cut"' "$OUT_DIR/xai_manifest.json"
grep -q 'data-transition-out="cut"' "$OUT_DIR/hyperframes/index.html"
grep -q 'applyShotVisibility' "$OUT_DIR/hyperframes/index.html"
grep -q 'fadeSeconds' "$OUT_DIR/hyperframes/index.html"
grep -q 'style.opacity' "$OUT_DIR/hyperframes/index.html"
assert_hyperframes_no_raw_shot_references "$OUT_DIR/hyperframes/index.html" "$OUT_DIR"
assert_hyperframes_normalized_references \
  "$OUT_DIR/hyperframes/index.html" \
  "shot_001.mp4" \
  "shot_002.mp4" \
  "shot_003.mp4"
printf '%s' "$STORY" \
  | "$SHAND_BIN" --dry-run pipeline --skip-hitl --panels 3 --output-dir "$OUT_DIR" \
  > "$OUT_DIR/pipeline_resume_summary.json"
grep -q '"pipeline":"xai_native"' "$OUT_DIR/pipeline_resume_summary.json"
grep -Eq '"story_hash":"[0-9a-f]{64}"' "$OUT_DIR/pipeline_resume_summary.json"
grep -q '"video_model":"grok-imagine-video"' "$OUT_DIR/pipeline_resume_summary.json"
grep -q '"manifest_reused": true' "$OUT_DIR/xai_run_metadata.json"
grep -q '"reused_shots": \[' "$OUT_DIR/xai_run_metadata.json"
for shot_index in 1 2 3; do
  grep -q "    $shot_index" "$OUT_DIR/xai_run_metadata.json"
done
if [[ -e "$OUT_DIR/remotion_props.json" ]]; then
  echo "xAI-native smoke unexpectedly wrote legacy remotion_props.json" >&2
  exit 1
fi

BATCH_DIR="$OUT_DIR/batch"
printf '%s' "$STORY" \
  | "$SHAND_BIN" --dry-run pipeline --skip-hitl --panels 1 --episodes 2 --batch-concurrency 1 --output-dir "$BATCH_DIR" \
  > "$OUT_DIR/batch_summary.json"

grep -q '"pipeline":"xai_native_batch"' "$OUT_DIR/batch_summary.json"
grep -q '"total_episodes":2' "$OUT_DIR/batch_summary.json"
grep -Eq '"story_hash":"[0-9a-f]{64}"' "$OUT_DIR/batch_summary.json"
grep -q '"video_model":"grok-imagine-video"' "$OUT_DIR/batch_summary.json"
batch_required_files=(
  "$BATCH_DIR/episode_001/xai_manifest.json"
  "$BATCH_DIR/episode_001/xai_run_metadata.json"
  "$BATCH_DIR/episode_001/render_metadata.json"
  "$BATCH_DIR/episode_001/shots/shot_001.mp4"
  "$BATCH_DIR/episode_001/normalized/shot_001.mp4"
  "$BATCH_DIR/episode_001/timeline_hyperframes.mp4"
  "$BATCH_DIR/episode_001/output_xai.mp4"
  "$BATCH_DIR/episode_001/preview_frame.jpg"
  "$BATCH_DIR/episode_001/hyperframes/index.html"
  "$BATCH_DIR/episode_001/hyperframes/package.json"
  "$BATCH_DIR/episode_002/xai_manifest.json"
  "$BATCH_DIR/episode_002/xai_run_metadata.json"
  "$BATCH_DIR/episode_002/render_metadata.json"
  "$BATCH_DIR/episode_002/shots/shot_001.mp4"
  "$BATCH_DIR/episode_002/normalized/shot_001.mp4"
  "$BATCH_DIR/episode_002/timeline_hyperframes.mp4"
  "$BATCH_DIR/episode_002/output_xai.mp4"
  "$BATCH_DIR/episode_002/preview_frame.jpg"
  "$BATCH_DIR/episode_002/hyperframes/index.html"
  "$BATCH_DIR/episode_002/hyperframes/package.json"
)
for file in "${batch_required_files[@]}"; do
  if [[ ! -s "$file" ]]; then
    echo "missing or empty batch smoke artifact: $file" >&2
    exit 1
  fi
done
for file in \
  "$BATCH_DIR/episode_001/shots/shot_001.mp4" \
  "$BATCH_DIR/episode_001/normalized/shot_001.mp4" \
  "$BATCH_DIR/episode_001/timeline_hyperframes.mp4" \
  "$BATCH_DIR/episode_001/output_xai.mp4" \
  "$BATCH_DIR/episode_002/shots/shot_001.mp4" \
  "$BATCH_DIR/episode_002/normalized/shot_001.mp4" \
  "$BATCH_DIR/episode_002/timeline_hyperframes.mp4" \
  "$BATCH_DIR/episode_002/output_xai.mp4"; do
  assert_mp4_magic "$file"
done
assert_jpeg_magic "$BATCH_DIR/episode_001/preview_frame.jpg"
assert_jpeg_magic "$BATCH_DIR/episode_002/preview_frame.jpg"
for episode_dir in "$BATCH_DIR/episode_001" "$BATCH_DIR/episode_002"; do
  assert_render_metadata_identity "$episode_dir"
  grep -q '"video_model": "grok-imagine-video"' "$episode_dir/xai_manifest.json"
  grep -q '"video_model": "grok-imagine-video"' "$episode_dir/xai_run_metadata.json"
  grep -q '"transition_out": "cut"' "$episode_dir/xai_manifest.json"
  grep -q 'data-transition-out="cut"' "$episode_dir/hyperframes/index.html"
  grep -q 'applyShotVisibility' "$episode_dir/hyperframes/index.html"
  grep -q 'fadeSeconds' "$episode_dir/hyperframes/index.html"
  grep -q 'style.opacity' "$episode_dir/hyperframes/index.html"
  assert_hyperframes_no_raw_shot_references "$episode_dir/hyperframes/index.html" "$episode_dir"
  assert_hyperframes_normalized_references "$episode_dir/hyperframes/index.html" "shot_001.mp4"
done
for file in \
  "$BATCH_DIR/episode_001/remotion_props.json" \
  "$BATCH_DIR/episode_002/remotion_props.json"; do
  if [[ -e "$file" ]]; then
    echo "xAI-native batch smoke unexpectedly wrote legacy remotion_props.json: $file" >&2
    exit 1
  fi
done
"$SHAND_BIN" xai inspect "$BATCH_DIR" > "$OUT_DIR/batch_inspect.json"
grep -q '"status":"complete"' "$OUT_DIR/batch_inspect.json"
grep -q '"total_episodes":2' "$OUT_DIR/batch_inspect.json"
grep -Eq '"story_hash":"[0-9a-f]{64}"' "$OUT_DIR/batch_inspect.json"
grep -q '"video_model":"grok-imagine-video"' "$OUT_DIR/batch_inspect.json"
set +e
"$SHAND_BIN" xai validate "$BATCH_DIR" \
  > "$OUT_DIR/batch_validate_dry_run.json" \
  2> "$OUT_DIR/batch_validate_dry_run.stderr.json"
batch_validate_dry_run_code=$?
set -e
if [[ "$batch_validate_dry_run_code" -eq 0 ]]; then
  echo "production validate unexpectedly accepted dry-run batch output" >&2
  exit 1
fi
grep -q '"status":"invalid"' "$OUT_DIR/batch_validate_dry_run.json"
grep -q '"total_episodes":2' "$OUT_DIR/batch_validate_dry_run.json"
grep -Eq '"story_hash":"[0-9a-f]{64}"' "$OUT_DIR/batch_validate_dry_run.json"
grep -q '"video_model":"grok-imagine-video"' "$OUT_DIR/batch_validate_dry_run.json"

"$SHAND_BIN" xai inspect "$OUT_DIR" > "$OUT_DIR/inspect_complete.json"
grep -q '"status":"complete"' "$OUT_DIR/inspect_complete.json"
grep -q '"shots":3' "$OUT_DIR/inspect_complete.json"
grep -Eq '"story_hash":"[0-9a-f]{64}"' "$OUT_DIR/inspect_complete.json"
grep -q '"video_model":"grok-imagine-video"' "$OUT_DIR/inspect_complete.json"

"$SHAND_BIN" xai inspect --strict "$OUT_DIR" > "$OUT_DIR/inspect_strict.json"
grep -q '"status":"complete"' "$OUT_DIR/inspect_strict.json"
grep -q '"shots":3' "$OUT_DIR/inspect_strict.json"
grep -Eq '"story_hash":"[0-9a-f]{64}"' "$OUT_DIR/inspect_strict.json"
grep -q '"video_model":"grok-imagine-video"' "$OUT_DIR/inspect_strict.json"

set +e
"$SHAND_BIN" xai validate "$OUT_DIR" \
  > "$OUT_DIR/validate_dry_run.json" \
  2> "$OUT_DIR/validate_dry_run.stderr.json"
validate_dry_run_code=$?
set -e
if [[ "$validate_dry_run_code" -eq 0 ]]; then
  echo "production validate unexpectedly accepted dry-run provider metadata" >&2
  exit 1
fi
grep -q '"status":"invalid"' "$OUT_DIR/validate_dry_run.json"
grep -Eq '"story_hash":"[0-9a-f]{64}"' "$OUT_DIR/validate_dry_run.json"
grep -q '"video_model":"grok-imagine-video"' "$OUT_DIR/validate_dry_run.json"
grep -q 'dry_run' "$OUT_DIR/validate_dry_run.json"

INVALID_DIR="$(mktemp -d "${TMPDIR:-/tmp}/shand-xai-native-invalid-XXXXXX")"
set +e
"$SHAND_BIN" xai inspect --strict "$INVALID_DIR" \
  > "$OUT_DIR/inspect_invalid.json" \
  2> "$OUT_DIR/inspect_invalid.stderr.json"
invalid_code=$?
set -e
if [[ "$invalid_code" -eq 0 ]]; then
  echo "strict inspect unexpectedly accepted invalid output dir" >&2
  exit 1
fi
grep -q '"status":"invalid"' "$OUT_DIR/inspect_invalid.json"

echo "xAI-native dry-run smoke passed"
