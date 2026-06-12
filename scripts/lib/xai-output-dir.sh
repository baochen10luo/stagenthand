#!/usr/bin/env bash

validate_xai_output_dir_path() {
  local output_dir="$1"
  local description="${2:-xAI-native output}"
  local name="${3:-OUT_DIR}"
  local checked_dir="$output_dir"
  local parent
  local direct_parent

  if [[ -z "$checked_dir" ]]; then
    echo "$name must not be empty" >&2
    return 1
  fi

  while [[ "$checked_dir" != "/" && "$checked_dir" == */ ]]; do
    checked_dir="${checked_dir%/}"
  done

  if [[ -L "$checked_dir" ]]; then
    echo "$name \"$output_dir\" is a symlink; $description must be an output-local directory" >&2
    return 1
  fi

  if [[ -e "$checked_dir" && ! -d "$checked_dir" ]]; then
    echo "$name \"$output_dir\" is not a directory" >&2
    return 1
  fi

  parent="$(dirname "$checked_dir")"
  direct_parent="$parent"
  while [[ "$parent" != "." && "$parent" != "$checked_dir" ]]; do
    if [[ -L "$parent" ]]; then
      if [[ "$(dirname "$parent")" == "/" ]]; then
        checked_dir="$parent"
        parent="$(dirname "$checked_dir")"
        continue
      elif [[ "$parent" == "$direct_parent" ]]; then
        echo "$name \"$output_dir\" has symlink parent \"$parent\"; $description must be an output-local directory" >&2
      else
        echo "$name \"$output_dir\" has symlink ancestor \"$parent\"; $description must be an output-local directory" >&2
      fi
      return 1
    fi
    if [[ -e "$parent" && ! -d "$parent" ]]; then
      if [[ "$parent" == "$direct_parent" ]]; then
        echo "$name parent \"$parent\" is not a directory; $description must be an output-local directory" >&2
      else
        echo "$name ancestor \"$parent\" is not a directory; $description must be an output-local directory" >&2
      fi
      return 1
    fi
    checked_dir="$parent"
    parent="$(dirname "$checked_dir")"
  done
}
