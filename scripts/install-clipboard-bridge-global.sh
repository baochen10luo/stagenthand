#!/usr/bin/env bash

set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local/bin}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${BUILD_DIR:-$ROOT_DIR/.cache/clipboard-bridge-build}"

mkdir -p "$PREFIX" "$BUILD_DIR"

echo "Building ccimg into $BUILD_DIR"
GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}" \
GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.cache/go-mod}" \
go build -o "$BUILD_DIR/ccimg" ./cmd/ccimg

echo "Building ccimgd into $BUILD_DIR"
GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}" \
GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.cache/go-mod}" \
go build -o "$BUILD_DIR/ccimgd" ./cmd/ccimgd

install -m 0755 "$BUILD_DIR/ccimg" "$PREFIX/ccimg"
install -m 0755 "$BUILD_DIR/ccimgd" "$PREFIX/ccimgd"

echo "Installed:"
echo "  $PREFIX/ccimg"
echo "  $PREFIX/ccimgd"
