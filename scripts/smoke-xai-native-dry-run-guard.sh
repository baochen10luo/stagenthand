#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SMOKE_SCRIPT="$ROOT_DIR/scripts/smoke-xai-native-dry-run.sh"

if [[ ! -x "$SMOKE_SCRIPT" ]]; then
  echo "missing executable dry-run smoke script: $SMOKE_SCRIPT" >&2
  exit 1
fi

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/shand-xai-dry-run-guard-XXXXXX")"
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
echo '{"pipeline":"stub"}'
SH
chmod +x "$STUB_BIN"

run_smoke() {
  SHAND_STUB_LOG="$STUB_LOG" SHAND_BIN="$STUB_BIN" "$SMOKE_SCRIPT" "$@"
}

symlink_out_target="$TMP_ROOT/symlink-out-target"
symlink_out_dir="$TMP_ROOT/symlink-out-dir"
mkdir -p "$symlink_out_target"
ln -s "$symlink_out_target" "$symlink_out_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$symlink_out_dir" run_smoke \
  > "$TMP_ROOT/symlink-out-dir.out" \
  2> "$TMP_ROOT/symlink-out-dir.err"
symlink_out_code=$?
set -e
if [[ "$symlink_out_code" -eq 0 ]]; then
  echo "symlinked dry-run OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" is a symlink; xAI-native dry-run smoke output must be an output-local directory' "$TMP_ROOT/symlink-out-dir.err"
if [[ -s "$STUB_LOG" ]]; then
  echo "symlinked dry-run OUT_DIR unexpectedly ran shand" >&2
  exit 1
fi
if [[ -e "$symlink_out_target/pipeline_summary.json" ]]; then
  echo "symlinked dry-run OUT_DIR unexpectedly wrote pipeline summary through symlink" >&2
  exit 1
fi

file_out_dir="$TMP_ROOT/file-out-dir"
printf 'not a directory' > "$file_out_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$file_out_dir" run_smoke \
  > "$TMP_ROOT/file-out-dir.out" \
  2> "$TMP_ROOT/file-out-dir.err"
file_out_code=$?
set -e
if [[ "$file_out_code" -eq 0 ]]; then
  echo "file dry-run OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" is not a directory' "$TMP_ROOT/file-out-dir.err"
if [[ -s "$STUB_LOG" ]]; then
  echo "file dry-run OUT_DIR unexpectedly ran shand" >&2
  exit 1
fi

file_parent="$TMP_ROOT/file-parent"
printf 'not a directory' > "$file_parent"
file_parent_out="$file_parent/generated"
: > "$STUB_LOG"
set +e
OUT_DIR="$file_parent_out" run_smoke \
  > "$TMP_ROOT/file-parent.out" \
  2> "$TMP_ROOT/file-parent.err"
file_parent_code=$?
set -e
if [[ "$file_parent_code" -eq 0 ]]; then
  echo "dry-run OUT_DIR under file parent unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR parent ".*" is not a directory; xAI-native dry-run smoke output must be an output-local directory' "$TMP_ROOT/file-parent.err"
if [[ -s "$STUB_LOG" ]]; then
  echo "dry-run OUT_DIR under file parent unexpectedly ran shand" >&2
  exit 1
fi

existing_symlink_parent_target="$TMP_ROOT/existing-symlink-parent-target"
existing_symlink_parent_dir="$TMP_ROOT/existing-symlink-parent"
existing_symlink_parent_out="$existing_symlink_parent_dir/generated"
mkdir -p "$existing_symlink_parent_target/generated"
ln -s "$existing_symlink_parent_target" "$existing_symlink_parent_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$existing_symlink_parent_out" run_smoke \
  > "$TMP_ROOT/existing-symlink-parent.out" \
  2> "$TMP_ROOT/existing-symlink-parent.err"
existing_symlink_parent_code=$?
set -e
if [[ "$existing_symlink_parent_code" -eq 0 ]]; then
  echo "existing leaf under symlink parent dry-run OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" has symlink parent ".*"; xAI-native dry-run smoke output must be an output-local directory' "$TMP_ROOT/existing-symlink-parent.err"
if [[ -s "$STUB_LOG" ]]; then
  echo "existing leaf under symlink parent dry-run OUT_DIR unexpectedly ran shand" >&2
  exit 1
fi
if [[ -e "$existing_symlink_parent_target/generated/pipeline_summary.json" ]]; then
  echo "existing leaf under symlink parent dry-run OUT_DIR unexpectedly wrote pipeline summary through symlink" >&2
  exit 1
fi

symlink_parent_target="$TMP_ROOT/symlink-parent-target"
symlink_parent_dir="$TMP_ROOT/symlink-parent"
symlink_parent_out="$symlink_parent_dir/generated"
mkdir -p "$symlink_parent_target"
ln -s "$symlink_parent_target" "$symlink_parent_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$symlink_parent_out" run_smoke \
  > "$TMP_ROOT/symlink-parent.out" \
  2> "$TMP_ROOT/symlink-parent.err"
symlink_parent_code=$?
set -e
if [[ "$symlink_parent_code" -eq 0 ]]; then
  echo "symlink parent dry-run OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" has symlink parent ".*"; xAI-native dry-run smoke output must be an output-local directory' "$TMP_ROOT/symlink-parent.err"
if [[ -s "$STUB_LOG" ]]; then
  echo "symlink parent dry-run OUT_DIR unexpectedly ran shand" >&2
  exit 1
fi
if [[ -e "$symlink_parent_target/generated" ]]; then
  echo "symlink parent dry-run OUT_DIR unexpectedly created external output directory" >&2
  exit 1
fi

symlink_ancestor_target="$TMP_ROOT/symlink-ancestor-target"
symlink_ancestor_dir="$TMP_ROOT/symlink-ancestor"
symlink_ancestor_out="$symlink_ancestor_dir/missing/generated"
mkdir -p "$symlink_ancestor_target"
ln -s "$symlink_ancestor_target" "$symlink_ancestor_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$symlink_ancestor_out" run_smoke \
  > "$TMP_ROOT/symlink-ancestor.out" \
  2> "$TMP_ROOT/symlink-ancestor.err"
symlink_ancestor_code=$?
set -e
if [[ "$symlink_ancestor_code" -eq 0 ]]; then
  echo "symlink ancestor dry-run OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" has symlink ancestor ".*"; xAI-native dry-run smoke output must be an output-local directory' "$TMP_ROOT/symlink-ancestor.err"
if [[ -s "$STUB_LOG" ]]; then
  echo "symlink ancestor dry-run OUT_DIR unexpectedly ran shand" >&2
  exit 1
fi
if [[ -e "$symlink_ancestor_target/missing" ]]; then
  echo "symlink ancestor dry-run OUT_DIR unexpectedly created external output directory" >&2
  exit 1
fi

existing_symlink_ancestor_target="$TMP_ROOT/existing-symlink-ancestor-target"
existing_symlink_ancestor_dir="$TMP_ROOT/existing-symlink-ancestor"
existing_symlink_ancestor_out="$existing_symlink_ancestor_dir/existing-parent/generated"
mkdir -p "$existing_symlink_ancestor_target/existing-parent/generated"
ln -s "$existing_symlink_ancestor_target" "$existing_symlink_ancestor_dir"
: > "$STUB_LOG"
set +e
OUT_DIR="$existing_symlink_ancestor_out" run_smoke \
  > "$TMP_ROOT/existing-symlink-ancestor.out" \
  2> "$TMP_ROOT/existing-symlink-ancestor.err"
existing_symlink_ancestor_code=$?
set -e
if [[ "$existing_symlink_ancestor_code" -eq 0 ]]; then
  echo "existing leaf under symlink ancestor dry-run OUT_DIR unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'OUT_DIR ".*" has symlink ancestor ".*"; xAI-native dry-run smoke output must be an output-local directory' "$TMP_ROOT/existing-symlink-ancestor.err"
if [[ -s "$STUB_LOG" ]]; then
  echo "existing leaf under symlink ancestor dry-run OUT_DIR unexpectedly ran shand" >&2
  exit 1
fi
if [[ -e "$existing_symlink_ancestor_target/existing-parent/generated/pipeline_summary.json" ]]; then
  echo "existing leaf under symlink ancestor dry-run OUT_DIR unexpectedly wrote pipeline summary through symlink" >&2
  exit 1
fi

echo "xAI-native dry-run smoke guard passed"
