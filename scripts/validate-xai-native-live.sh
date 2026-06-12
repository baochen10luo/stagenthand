#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT_DIR/scripts/lib/xai-output-dir.sh"

SHAND_BIN="${SHAND_BIN:-$ROOT_DIR/shand}"
STORY="${STORY:-一個夜班工程師在台北雨夜追蹤失控的配送機器人，最後發現它只是想把一束花送到醫院。}"
PANELS="${PANELS:-3}"
EPISODES="${EPISODES:-1}"
BATCH_CONCURRENCY="${BATCH_CONCURRENCY:-1}"
RUN_XAI_LIVE_VALIDATION="${RUN_XAI_LIVE_VALIDATION:-0}"
ALLOW_SKIP="${ALLOW_SKIP:-0}"

if [[ ! -x "$SHAND_BIN" ]]; then
  echo "missing executable: $SHAND_BIN" >&2
  echo "build it first: go build -o shand ." >&2
  exit 1
fi

if [[ -z "${OUT_DIR:-}" ]]; then
  OUT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/shand-xai-native-live-XXXXXX")"
fi

validate_xai_output_dir_path "$OUT_DIR" "xAI-native live validation output" "OUT_DIR"
mkdir -p "$OUT_DIR"

auth_tmp="$(mktemp "${TMPDIR:-/tmp}/shand-xai-auth-status-XXXXXX.json")"
cleanup() {
  rm -f "$auth_tmp"
}
trap cleanup EXIT

validate_existing_batch_episode_dirs() {
  local output_dir="$1"
  local episode_dirs=()
  local episode_numbers=()
  local dir
  local name
  local suffix
  local number
  local expected

  while IFS= read -r dir; do
    episode_dirs+=("$dir")
  done < <(find "$output_dir" -mindepth 1 -maxdepth 1 -name 'episode_*' -print | sort)

  if [[ "${#episode_dirs[@]}" -eq 0 ]]; then
    return 0
  fi

  for dir in "${episode_dirs[@]}"; do
    name="$(basename "$dir")"
    if [[ -L "$dir" ]]; then
      echo "episode directory \"$name\" is a symlink; xAI-native batch episodes must be output-local directories" >&2
      return 1
    fi
    if [[ ! -d "$dir" ]]; then
      echo "episode directory \"$name\" is not a directory" >&2
      return 1
    fi
    if ! [[ "$name" =~ ^episode_[0-9][0-9][0-9]$ ]]; then
      echo "malformed episode directory \"$name\": want episode_###" >&2
      return 1
    fi
    suffix="${name#episode_}"
    number=$((10#$suffix))
    if (( number < 1 )); then
      echo "malformed episode directory \"$name\": want episode_###" >&2
      return 1
    fi
    episode_numbers+=("$number")
  done

  expected=1
  for number in $(printf '%s\n' "${episode_numbers[@]}" | sort -n); do
    if (( number != expected )); then
      printf 'missing episode_%03d directory\n' "$expected" >&2
      return 1
    fi
    expected=$((expected + 1))
  done
}

write_auth_status_if_available() {
  if "$SHAND_BIN" auth xai status > "$auth_tmp" 2> /dev/null; then
    cp "$auth_tmp" "$OUT_DIR/auth_status.json"
    return 0
  fi
  return 1
}

require_auth_status() {
  if write_auth_status_if_available; then
    return 0
  fi
  if [[ "$ALLOW_SKIP" == "1" ]]; then
    echo "xAI-native live validation skipped: Hermes xAI OAuth credentials are unavailable"
    exit 0
  fi
  echo "xAI-native live validation requires Hermes xAI OAuth credentials" >&2
  exit 1
}

if ! [[ "$PANELS" =~ ^[0-9]+$ ]] || (( PANELS < 1 )); then
  echo "PANELS must be a positive integer: $PANELS" >&2
  exit 1
fi

if ! [[ "$EPISODES" =~ ^[0-9]+$ ]] || (( EPISODES < 1 )); then
  echo "EPISODES must be a positive integer: $EPISODES" >&2
  exit 1
fi

if ! [[ "$BATCH_CONCURRENCY" =~ ^[0-9]+$ ]] || (( BATCH_CONCURRENCY < 1 )); then
  echo "BATCH_CONCURRENCY must be a positive integer: $BATCH_CONCURRENCY" >&2
  exit 1
fi

validate_existing_batch_episode_dirs "$OUT_DIR"

single_manifest="$OUT_DIR/xai_manifest.json"
batch_manifest="$(
  find "$OUT_DIR" -mindepth 2 -maxdepth 2 -type f -name xai_manifest.json -print \
    | while IFS= read -r manifest; do
      episode_dir="$(basename "$(dirname "$manifest")")"
      if [[ "$episode_dir" =~ ^episode_[0-9][0-9][0-9]$ ]]; then
        printf '%s\n' "$manifest"
        break
      fi
    done
)"

if [[ -f "$single_manifest" || -n "$batch_manifest" ]]; then
  echo "Validating existing xAI-native output in $OUT_DIR"
  write_auth_status_if_available || true
else
  existing_non_auth="$(find "$OUT_DIR" -mindepth 1 ! -name auth_status.json -print -quit)"
  if [[ -n "$existing_non_auth" ]]; then
    echo "OUT_DIR must be empty, contain xai_manifest.json, or contain episode_###/xai_manifest.json for live validation: $OUT_DIR" >&2
    exit 1
  fi

  require_auth_status

  if [[ "$RUN_XAI_LIVE_VALIDATION" != "1" ]]; then
    echo "missing xAI-native output at $OUT_DIR" >&2
    echo "set RUN_XAI_LIVE_VALIDATION=1 to generate a fresh live output" >&2
    exit 1
  fi

  echo "Generating fresh xAI-native live output in $OUT_DIR"
  pipeline_args=(pipeline --skip-hitl --panels "$PANELS" --output-dir "$OUT_DIR")
  if (( EPISODES > 1 )); then
    pipeline_args+=(--episodes "$EPISODES" --batch-concurrency "$BATCH_CONCURRENCY")
  fi

  printf '%s\n' "$STORY" \
    | "$SHAND_BIN" "${pipeline_args[@]}" \
    > "$OUT_DIR/pipeline_summary.json"
  grep -Eq '"story_hash"[[:space:]]*:[[:space:]]*"[0-9a-f]{64}"' "$OUT_DIR/pipeline_summary.json"
  grep -Eq '"video_model"[[:space:]]*:[[:space:]]*"[^"]+"' "$OUT_DIR/pipeline_summary.json"
fi

"$SHAND_BIN" xai inspect --strict "$OUT_DIR" > "$OUT_DIR/inspect.json"
grep -Eq '"story_hash"[[:space:]]*:[[:space:]]*"[0-9a-f]{64}"' "$OUT_DIR/inspect.json"
grep -Eq '"video_model"[[:space:]]*:[[:space:]]*"[^"]+"' "$OUT_DIR/inspect.json"
"$SHAND_BIN" xai validate "$OUT_DIR" > "$OUT_DIR/validation.json"
grep -Eq '"story_hash"[[:space:]]*:[[:space:]]*"[0-9a-f]{64}"' "$OUT_DIR/validation.json"
grep -Eq '"video_model"[[:space:]]*:[[:space:]]*"[^"]+"' "$OUT_DIR/validation.json"

echo "xAI-native live validation passed: $OUT_DIR"
