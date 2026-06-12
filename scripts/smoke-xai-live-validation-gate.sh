#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE_SCRIPT="$ROOT_DIR/scripts/validate-xai-native-live.sh"

if [[ ! -x "$GATE_SCRIPT" ]]; then
  echo "missing executable live validation gate: $GATE_SCRIPT" >&2
  exit 1
fi

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/shand-xai-live-gate-smoke-XXXXXX")"
cleanup() {
  rm -rf "$TMP_ROOT"
}
trap cleanup EXIT

STUB_LOG="$TMP_ROOT/shand.log"
STUB_BIN="$TMP_ROOT/shand"
cat > "$STUB_BIN" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >> "${SHAND_STUB_LOG:?}"

cmd="${1:-}"
shift || true

case "$cmd" in
  auth)
    if [[ "${SHAND_STUB_AUTH_FAIL:-0}" == "1" ]]; then
      echo '{"error":"missing auth"}' >&2
      exit 1
    fi
    echo '{"provider":"xai-oauth","found":true,"access_token_present":true}'
    ;;
  pipeline)
    out_dir=""
    episodes="1"
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --output-dir)
          out_dir="$2"
          shift 2
          ;;
        --episodes)
          episodes="$2"
          shift 2
          ;;
        *)
          shift
          ;;
      esac
    done
    if [[ -z "$out_dir" ]]; then
      echo "missing --output-dir" >&2
      exit 1
    fi
    write_output() {
      local target_dir="$1"
      local episode="$2"
      local prompt_hash="729ef258e456fb9ddf6bbf143bb17fe97f31ef13e0a9f86dfd4c83c5038b0ac1"
      mkdir -p "$target_dir/shots" "$target_dir/normalized" "$target_dir/hyperframes"
      printf '{"project_id":"stub-%s","story_hash":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","video_model":"grok-imagine-video","format":"portrait","fps":24,"width":720,"height":1280,"shots":[{"index":1,"prompt":"shot","prompt_hash":"%s","duration_sec":8,"video_path":"shots/shot_001.mp4","xai_request_id":"req_%s","xai_status":"done","transition_out":"cut"}]}\n' "$episode" "$prompt_hash" "$episode" > "$target_dir/xai_manifest.json"
      printf '{"planned":true,"video_model":"grok-imagine-video","generated_shots":[1],"shot_decisions":[{"index":1,"decision":"generated","video_path":"shots/shot_001.mp4","prompt_hash":"%s","xai_request_id":"req_%s","xai_status":"done"}]}\n' "$prompt_hash" "$episode" > "$target_dir/xai_run_metadata.json"
      printf 'video' > "$target_dir/shots/shot_001.mp4"
      printf 'normalized' > "$target_dir/normalized/shot_001.mp4"
      printf '<html></html>' > "$target_dir/hyperframes/index.html"
      printf 'final' > "$target_dir/output_xai.mp4"
      printf 'preview' > "$target_dir/preview_frame.jpg"
    }
    if [[ "$episodes" -gt 1 ]]; then
      printf '{"pipeline":"xai_native_batch","output_dir":"%s","story_hash":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","video_model":"grok-imagine-video","total_episodes":%s}\n' "$out_dir" "$episodes"
      for ((episode = 1; episode <= episodes; episode++)); do
        episode_dir="$(printf '%s/episode_%03d' "$out_dir" "$episode")"
        write_output "$episode_dir" "$episode"
      done
    else
      printf '{"pipeline":"xai_native","output_dir":"%s","story_hash":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","video_model":"grok-imagine-video"}\n' "$out_dir"
      write_output "$out_dir" "1"
    fi
    ;;
  xai)
    subcmd="${1:-}"
    shift || true
    case "$subcmd" in
      inspect)
        echo '{"status":"complete","story_hash":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","video_model":"grok-imagine-video","shots":1}'
        ;;
      validate)
        echo '{"status":"valid","story_hash":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","video_model":"grok-imagine-video","inspect":{"shots":1},"issues":null}'
        ;;
      *)
        echo "unexpected xai subcommand: $subcmd" >&2
        exit 1
        ;;
    esac
    ;;
  *)
    echo "unexpected command: $cmd" >&2
    exit 1
    ;;
esac
SH
chmod +x "$STUB_BIN"

run_gate() {
  SHAND_STUB_LOG="$STUB_LOG" SHAND_BIN="$STUB_BIN" "$GATE_SCRIPT" "$@"
}

assert_identity_json() {
  local file="$1"
  grep -Eq '"story_hash":"[0-9a-f]{64}"' "$file"
  grep -q '"video_model":"grok-imagine-video"' "$file"
}

symlink_out_target="$TMP_ROOT/symlink-out-target"
symlink_out_dir="$TMP_ROOT/symlink-out-dir"
mkdir -p "$symlink_out_target"
ln -s "$symlink_out_target" "$symlink_out_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$symlink_out_dir" run_gate \
  > "$TMP_ROOT/symlink-out-dir.out" \
  2> "$TMP_ROOT/symlink-out-dir.err"
symlink_out_code=$?
set -e
if [[ "$symlink_out_code" -eq 0 ]]; then
  echo "symlinked OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" is a symlink; xAI-native live validation output must be an output-local directory' "$TMP_ROOT/symlink-out-dir.err"
if grep -q '^auth ' "$STUB_LOG" || grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "symlinked OUT_DIR unexpectedly ran auth, pipeline, or validation" >&2
  exit 1
fi

file_out_dir="$TMP_ROOT/file-out-dir"
printf 'not a directory' > "$file_out_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$file_out_dir" run_gate \
  > "$TMP_ROOT/file-out-dir.out" \
  2> "$TMP_ROOT/file-out-dir.err"
file_out_code=$?
set -e
if [[ "$file_out_code" -eq 0 ]]; then
  echo "file OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" is not a directory' "$TMP_ROOT/file-out-dir.err"
if grep -q '^auth ' "$STUB_LOG" || grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "file OUT_DIR unexpectedly ran auth, pipeline, or validation" >&2
  exit 1
fi

file_parent="$TMP_ROOT/file-parent"
printf 'not a directory' > "$file_parent"
file_parent_out="$file_parent/generated"
: > "$STUB_LOG"
set +e
OUT_DIR="$file_parent_out" run_gate \
  > "$TMP_ROOT/file-parent.out" \
  2> "$TMP_ROOT/file-parent.err"
file_parent_code=$?
set -e
if [[ "$file_parent_code" -eq 0 ]]; then
  echo "OUT_DIR under file parent unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR parent ".*" is not a directory; xAI-native live validation output must be an output-local directory' "$TMP_ROOT/file-parent.err"
if grep -q '^auth ' "$STUB_LOG" || grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "OUT_DIR under file parent unexpectedly ran auth, pipeline, or validation" >&2
  exit 1
fi

symlink_parent_target="$TMP_ROOT/symlink-parent-target"
symlink_parent_dir="$TMP_ROOT/symlink-parent"
symlink_parent_out="$symlink_parent_dir/generated"
mkdir -p "$symlink_parent_target"
ln -s "$symlink_parent_target" "$symlink_parent_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$symlink_parent_out" run_gate \
  > "$TMP_ROOT/symlink-parent.out" \
  2> "$TMP_ROOT/symlink-parent.err"
symlink_parent_code=$?
set -e
if [[ "$symlink_parent_code" -eq 0 ]]; then
  echo "symlink parent OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" has symlink parent ".*"; xAI-native live validation output must be an output-local directory' "$TMP_ROOT/symlink-parent.err"
if grep -q '^auth ' "$STUB_LOG" || grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "symlink parent OUT_DIR unexpectedly ran auth, pipeline, or validation" >&2
  exit 1
fi
if [[ -e "$symlink_parent_target/generated" ]]; then
  echo "symlink parent OUT_DIR unexpectedly created external output directory" >&2
  exit 1
fi

existing_symlink_parent_target="$TMP_ROOT/existing-symlink-parent-target"
existing_symlink_parent_dir="$TMP_ROOT/existing-symlink-parent"
existing_symlink_parent_out="$existing_symlink_parent_dir/generated"
mkdir -p "$existing_symlink_parent_target/generated"
ln -s "$existing_symlink_parent_target" "$existing_symlink_parent_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$existing_symlink_parent_out" run_gate \
  > "$TMP_ROOT/existing-symlink-parent.out" \
  2> "$TMP_ROOT/existing-symlink-parent.err"
existing_symlink_parent_code=$?
set -e
if [[ "$existing_symlink_parent_code" -eq 0 ]]; then
  echo "existing leaf under symlink parent OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" has symlink parent ".*"; xAI-native live validation output must be an output-local directory' "$TMP_ROOT/existing-symlink-parent.err"
if grep -q '^auth ' "$STUB_LOG" || grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "existing leaf under symlink parent OUT_DIR unexpectedly ran auth, pipeline, or validation" >&2
  exit 1
fi
if [[ -e "$existing_symlink_parent_target/generated/pipeline_summary.json" ]]; then
  echo "existing leaf under symlink parent OUT_DIR unexpectedly wrote pipeline summary through symlink" >&2
  exit 1
fi

symlink_ancestor_target="$TMP_ROOT/symlink-ancestor-target"
symlink_ancestor_dir="$TMP_ROOT/symlink-ancestor"
symlink_ancestor_out="$symlink_ancestor_dir/missing/generated"
mkdir -p "$symlink_ancestor_target"
ln -s "$symlink_ancestor_target" "$symlink_ancestor_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$symlink_ancestor_out" run_gate \
  > "$TMP_ROOT/symlink-ancestor.out" \
  2> "$TMP_ROOT/symlink-ancestor.err"
symlink_ancestor_code=$?
set -e
if [[ "$symlink_ancestor_code" -eq 0 ]]; then
  echo "symlink ancestor OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" has symlink ancestor ".*"; xAI-native live validation output must be an output-local directory' "$TMP_ROOT/symlink-ancestor.err"
if grep -q '^auth ' "$STUB_LOG" || grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "symlink ancestor OUT_DIR unexpectedly ran auth, pipeline, or validation" >&2
  exit 1
fi
if [[ -e "$symlink_ancestor_target/missing" ]]; then
  echo "symlink ancestor OUT_DIR unexpectedly created external output directory" >&2
  exit 1
fi

existing_symlink_ancestor_target="$TMP_ROOT/existing-symlink-ancestor-target"
existing_symlink_ancestor_dir="$TMP_ROOT/existing-symlink-ancestor"
existing_symlink_ancestor_out="$existing_symlink_ancestor_dir/existing-parent/generated"
mkdir -p "$existing_symlink_ancestor_target/existing-parent/generated"
ln -s "$existing_symlink_ancestor_target" "$existing_symlink_ancestor_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$existing_symlink_ancestor_out" run_gate \
  > "$TMP_ROOT/existing-symlink-ancestor.out" \
  2> "$TMP_ROOT/existing-symlink-ancestor.err"
existing_symlink_ancestor_code=$?
set -e
if [[ "$existing_symlink_ancestor_code" -eq 0 ]]; then
  echo "existing leaf under symlink ancestor OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" has symlink ancestor ".*"; xAI-native live validation output must be an output-local directory' "$TMP_ROOT/existing-symlink-ancestor.err"
if grep -q '^auth ' "$STUB_LOG" || grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "existing leaf under symlink ancestor OUT_DIR unexpectedly ran auth, pipeline, or validation" >&2
  exit 1
fi
if [[ -e "$existing_symlink_ancestor_target/existing-parent/generated/pipeline_summary.json" ]]; then
  echo "existing leaf under symlink ancestor OUT_DIR unexpectedly wrote pipeline summary through symlink" >&2
  exit 1
fi

skip_dir="$TMP_ROOT/skip"
mkdir -p "$skip_dir"
SHAND_STUB_LOG="$STUB_LOG" SHAND_STUB_AUTH_FAIL=1 SHAND_BIN="$STUB_BIN" ALLOW_SKIP=1 OUT_DIR="$skip_dir" "$GATE_SCRIPT" \
  > "$TMP_ROOT/skip.out" \
  2> "$TMP_ROOT/skip.err"
grep -q "skipped" "$TMP_ROOT/skip.out"
if [[ -e "$skip_dir/pipeline_summary.json" ]]; then
  echo "skip mode unexpectedly generated pipeline artifacts" >&2
  exit 1
fi

auth_required_dir="$TMP_ROOT/auth-required"
mkdir -p "$auth_required_dir"
set +e
SHAND_STUB_LOG="$STUB_LOG" SHAND_STUB_AUTH_FAIL=1 SHAND_BIN="$STUB_BIN" OUT_DIR="$auth_required_dir" "$GATE_SCRIPT" \
  > "$TMP_ROOT/auth-required.out" \
  2> "$TMP_ROOT/auth-required.err"
auth_required_code=$?
set -e
if [[ "$auth_required_code" -eq 0 ]]; then
  echo "missing auth without ALLOW_SKIP unexpectedly succeeded" >&2
  exit 1
fi
grep -q "requires Hermes xAI OAuth credentials" "$TMP_ROOT/auth-required.err"

no_spend_dir="$TMP_ROOT/no-spend"
mkdir -p "$no_spend_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$no_spend_dir" run_gate \
  > "$TMP_ROOT/no-spend.out" \
  2> "$TMP_ROOT/no-spend.err"
no_spend_code=$?
set -e
if [[ "$no_spend_code" -eq 0 ]]; then
  echo "missing output without RUN_XAI_LIVE_VALIDATION unexpectedly succeeded" >&2
  exit 1
fi
grep -q "set RUN_XAI_LIVE_VALIDATION=1" "$TMP_ROOT/no-spend.err"
if grep -q '^pipeline ' "$STUB_LOG"; then
  echo "no-spend guard unexpectedly ran pipeline" >&2
  exit 1
fi

invalid_panels_dir="$TMP_ROOT/invalid-panels"
mkdir -p "$invalid_panels_dir"
: > "$STUB_LOG"
set +e
PANELS=0 OUT_DIR="$invalid_panels_dir" run_gate \
  > "$TMP_ROOT/invalid-panels.out" \
  2> "$TMP_ROOT/invalid-panels.err"
invalid_panels_code=$?
set -e
if [[ "$invalid_panels_code" -eq 0 ]]; then
  echo "invalid PANELS unexpectedly succeeded" >&2
  exit 1
fi
grep -q "PANELS must be a positive integer: 0" "$TMP_ROOT/invalid-panels.err"
if grep -q '^auth ' "$STUB_LOG" || grep -q '^pipeline ' "$STUB_LOG"; then
  echo "invalid PANELS unexpectedly ran auth or pipeline" >&2
  exit 1
fi

invalid_episodes_dir="$TMP_ROOT/invalid-episodes"
mkdir -p "$invalid_episodes_dir"
: > "$STUB_LOG"
set +e
EPISODES=0 OUT_DIR="$invalid_episodes_dir" run_gate \
  > "$TMP_ROOT/invalid-episodes.out" \
  2> "$TMP_ROOT/invalid-episodes.err"
invalid_episodes_code=$?
set -e
if [[ "$invalid_episodes_code" -eq 0 ]]; then
  echo "invalid EPISODES unexpectedly succeeded" >&2
  exit 1
fi
grep -q "EPISODES must be a positive integer: 0" "$TMP_ROOT/invalid-episodes.err"
if grep -q '^auth ' "$STUB_LOG" || grep -q '^pipeline ' "$STUB_LOG"; then
  echo "invalid EPISODES unexpectedly ran auth or pipeline" >&2
  exit 1
fi

invalid_batch_concurrency_dir="$TMP_ROOT/invalid-batch-concurrency"
mkdir -p "$invalid_batch_concurrency_dir"
: > "$STUB_LOG"
set +e
BATCH_CONCURRENCY=0 OUT_DIR="$invalid_batch_concurrency_dir" run_gate \
  > "$TMP_ROOT/invalid-batch-concurrency.out" \
  2> "$TMP_ROOT/invalid-batch-concurrency.err"
invalid_batch_concurrency_code=$?
set -e
if [[ "$invalid_batch_concurrency_code" -eq 0 ]]; then
  echo "invalid BATCH_CONCURRENCY unexpectedly succeeded" >&2
  exit 1
fi
grep -q "BATCH_CONCURRENCY must be a positive integer: 0" "$TMP_ROOT/invalid-batch-concurrency.err"
if grep -q '^auth ' "$STUB_LOG" || grep -q '^pipeline ' "$STUB_LOG"; then
  echo "invalid BATCH_CONCURRENCY unexpectedly ran auth or pipeline" >&2
  exit 1
fi

existing_dir="$TMP_ROOT/existing"
mkdir -p "$existing_dir"
printf '{}' > "$existing_dir/xai_manifest.json"
: > "$STUB_LOG"
OUT_DIR="$existing_dir" run_gate > "$TMP_ROOT/existing.out"
grep -q "xAI-native live validation passed" "$TMP_ROOT/existing.out"
grep -q '^xai inspect --strict' "$STUB_LOG"
grep -q '^xai validate' "$STUB_LOG"
if grep -q '^pipeline ' "$STUB_LOG"; then
  echo "existing output validation unexpectedly ran pipeline" >&2
  exit 1
fi
test -s "$existing_dir/inspect.json"
test -s "$existing_dir/validation.json"
assert_identity_json "$existing_dir/inspect.json"
assert_identity_json "$existing_dir/validation.json"

existing_no_auth_dir="$TMP_ROOT/existing-no-auth"
mkdir -p "$existing_no_auth_dir"
printf '{}' > "$existing_no_auth_dir/xai_manifest.json"
: > "$STUB_LOG"
SHAND_STUB_AUTH_FAIL=1 OUT_DIR="$existing_no_auth_dir" run_gate > "$TMP_ROOT/existing-no-auth.out"
grep -q "xAI-native live validation passed" "$TMP_ROOT/existing-no-auth.out"
grep -q '^xai inspect --strict' "$STUB_LOG"
grep -q '^xai validate' "$STUB_LOG"
if grep -q '^pipeline ' "$STUB_LOG"; then
  echo "existing output without auth unexpectedly ran pipeline" >&2
  exit 1
fi
test -s "$existing_no_auth_dir/inspect.json"
test -s "$existing_no_auth_dir/validation.json"
assert_identity_json "$existing_no_auth_dir/inspect.json"
assert_identity_json "$existing_no_auth_dir/validation.json"
if [[ -e "$existing_no_auth_dir/auth_status.json" ]]; then
  echo "existing output without auth unexpectedly wrote auth_status.json" >&2
  exit 1
fi

malformed_batch_dir="$TMP_ROOT/malformed-batch"
mkdir -p "$malformed_batch_dir/episode_1"
printf '{}' > "$malformed_batch_dir/episode_1/xai_manifest.json"
: > "$STUB_LOG"
set +e
OUT_DIR="$malformed_batch_dir" run_gate \
  > "$TMP_ROOT/malformed-batch.out" \
  2> "$TMP_ROOT/malformed-batch.err"
malformed_batch_code=$?
set -e
if [[ "$malformed_batch_code" -eq 0 ]]; then
  echo "malformed existing batch output unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'malformed episode directory "episode_1": want episode_###' "$TMP_ROOT/malformed-batch.err"
if grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "malformed existing batch unexpectedly ran pipeline or validation" >&2
  exit 1
fi

mixed_malformed_batch_dir="$TMP_ROOT/mixed-malformed-batch"
mkdir -p "$mixed_malformed_batch_dir/episode_001" "$mixed_malformed_batch_dir/episode_1"
printf '{}' > "$mixed_malformed_batch_dir/episode_001/xai_manifest.json"
printf '{}' > "$mixed_malformed_batch_dir/episode_1/xai_manifest.json"
: > "$STUB_LOG"
set +e
OUT_DIR="$mixed_malformed_batch_dir" run_gate \
  > "$TMP_ROOT/mixed-malformed-batch.out" \
  2> "$TMP_ROOT/mixed-malformed-batch.err"
mixed_malformed_batch_code=$?
set -e
if [[ "$mixed_malformed_batch_code" -eq 0 ]]; then
  echo "mixed malformed existing batch output unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'malformed episode directory "episode_1": want episode_###' "$TMP_ROOT/mixed-malformed-batch.err"
if grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "mixed malformed existing batch unexpectedly ran pipeline or validation" >&2
  exit 1
fi

gapped_batch_dir="$TMP_ROOT/gapped-batch"
mkdir -p "$gapped_batch_dir/episode_001" "$gapped_batch_dir/episode_003"
printf '{}' > "$gapped_batch_dir/episode_001/xai_manifest.json"
printf '{}' > "$gapped_batch_dir/episode_003/xai_manifest.json"
: > "$STUB_LOG"
set +e
OUT_DIR="$gapped_batch_dir" run_gate \
  > "$TMP_ROOT/gapped-batch.out" \
  2> "$TMP_ROOT/gapped-batch.err"
gapped_batch_code=$?
set -e
if [[ "$gapped_batch_code" -eq 0 ]]; then
  echo "gapped existing batch output unexpectedly succeeded" >&2
  exit 1
fi
grep -q "missing episode_002 directory" "$TMP_ROOT/gapped-batch.err"
if grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "gapped existing batch unexpectedly ran pipeline or validation" >&2
  exit 1
fi

symlinked_batch_dir="$TMP_ROOT/symlinked-batch"
external_episode_dir="$TMP_ROOT/external-episode"
mkdir -p "$symlinked_batch_dir" "$external_episode_dir"
printf '{}' > "$external_episode_dir/xai_manifest.json"
ln -s "$external_episode_dir" "$symlinked_batch_dir/episode_001"
: > "$STUB_LOG"
set +e
OUT_DIR="$symlinked_batch_dir" run_gate \
  > "$TMP_ROOT/symlinked-batch.out" \
  2> "$TMP_ROOT/symlinked-batch.err"
symlinked_batch_code=$?
set -e
if [[ "$symlinked_batch_code" -eq 0 ]]; then
  echo "symlinked existing batch output unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'episode directory "episode_001" is a symlink; xAI-native batch episodes must be output-local directories' "$TMP_ROOT/symlinked-batch.err"
if grep -q '^pipeline ' "$STUB_LOG" || grep -q '^xai ' "$STUB_LOG"; then
  echo "symlinked existing batch unexpectedly ran pipeline or validation" >&2
  exit 1
fi

existing_batch_dir="$TMP_ROOT/existing-batch"
mkdir -p "$existing_batch_dir/episode_001" "$existing_batch_dir/episode_002"
printf '{}' > "$existing_batch_dir/episode_001/xai_manifest.json"
printf '{}' > "$existing_batch_dir/episode_002/xai_manifest.json"
: > "$STUB_LOG"
OUT_DIR="$existing_batch_dir" run_gate > "$TMP_ROOT/existing-batch.out"
grep -q "xAI-native live validation passed" "$TMP_ROOT/existing-batch.out"
grep -q '^xai inspect --strict' "$STUB_LOG"
grep -q '^xai validate' "$STUB_LOG"
if grep -q '^pipeline ' "$STUB_LOG"; then
  echo "existing batch output validation unexpectedly ran pipeline" >&2
  exit 1
fi
test -s "$existing_batch_dir/inspect.json"
test -s "$existing_batch_dir/validation.json"
assert_identity_json "$existing_batch_dir/inspect.json"
assert_identity_json "$existing_batch_dir/validation.json"

existing_batch_no_auth_dir="$TMP_ROOT/existing-batch-no-auth"
mkdir -p "$existing_batch_no_auth_dir/episode_001" "$existing_batch_no_auth_dir/episode_002"
printf '{}' > "$existing_batch_no_auth_dir/episode_001/xai_manifest.json"
printf '{}' > "$existing_batch_no_auth_dir/episode_002/xai_manifest.json"
: > "$STUB_LOG"
SHAND_STUB_AUTH_FAIL=1 OUT_DIR="$existing_batch_no_auth_dir" run_gate > "$TMP_ROOT/existing-batch-no-auth.out"
grep -q "xAI-native live validation passed" "$TMP_ROOT/existing-batch-no-auth.out"
grep -q '^xai inspect --strict' "$STUB_LOG"
grep -q '^xai validate' "$STUB_LOG"
if grep -q '^pipeline ' "$STUB_LOG"; then
  echo "existing batch output without auth unexpectedly ran pipeline" >&2
  exit 1
fi
test -s "$existing_batch_no_auth_dir/inspect.json"
test -s "$existing_batch_no_auth_dir/validation.json"
assert_identity_json "$existing_batch_no_auth_dir/inspect.json"
assert_identity_json "$existing_batch_no_auth_dir/validation.json"
if [[ -e "$existing_batch_no_auth_dir/auth_status.json" ]]; then
  echo "existing batch output without auth unexpectedly wrote auth_status.json" >&2
  exit 1
fi

generated_dir="$TMP_ROOT/generated"
mkdir -p "$generated_dir"
: > "$STUB_LOG"
RUN_XAI_LIVE_VALIDATION=1 PANELS=1 STORY="stub story" OUT_DIR="$generated_dir" run_gate > "$TMP_ROOT/generated.out"
grep -q "xAI-native live validation passed" "$TMP_ROOT/generated.out"
grep -q '^pipeline --skip-hitl --panels 1 --output-dir' "$STUB_LOG"
test -s "$generated_dir/pipeline_summary.json"
test -s "$generated_dir/inspect.json"
test -s "$generated_dir/validation.json"
assert_identity_json "$generated_dir/inspect.json"
assert_identity_json "$generated_dir/validation.json"
grep -Eq '"story_hash":"[0-9a-f]{64}"' "$generated_dir/pipeline_summary.json"
grep -q '"video_model":"grok-imagine-video"' "$generated_dir/pipeline_summary.json"
grep -q '"video_model":"grok-imagine-video"' "$generated_dir/xai_manifest.json"
grep -q '"video_model":"grok-imagine-video"' "$generated_dir/xai_run_metadata.json"

generated_batch_dir="$TMP_ROOT/generated-batch"
mkdir -p "$generated_batch_dir"
: > "$STUB_LOG"
RUN_XAI_LIVE_VALIDATION=1 PANELS=1 EPISODES=2 BATCH_CONCURRENCY=1 STORY="stub story" OUT_DIR="$generated_batch_dir" run_gate > "$TMP_ROOT/generated-batch.out"
grep -q "xAI-native live validation passed" "$TMP_ROOT/generated-batch.out"
grep -q '^pipeline --skip-hitl --panels 1 --output-dir .* --episodes 2 --batch-concurrency 1' "$STUB_LOG"
test -s "$generated_batch_dir/pipeline_summary.json"
test -s "$generated_batch_dir/inspect.json"
test -s "$generated_batch_dir/validation.json"
assert_identity_json "$generated_batch_dir/inspect.json"
assert_identity_json "$generated_batch_dir/validation.json"
test -s "$generated_batch_dir/episode_001/xai_manifest.json"
test -s "$generated_batch_dir/episode_002/xai_manifest.json"
grep -Eq '"story_hash":"[0-9a-f]{64}"' "$generated_batch_dir/pipeline_summary.json"
grep -q '"video_model":"grok-imagine-video"' "$generated_batch_dir/pipeline_summary.json"
grep -q '"video_model":"grok-imagine-video"' "$generated_batch_dir/episode_001/xai_manifest.json"
grep -q '"video_model":"grok-imagine-video"' "$generated_batch_dir/episode_002/xai_run_metadata.json"

echo "xAI live validation gate smoke passed"
