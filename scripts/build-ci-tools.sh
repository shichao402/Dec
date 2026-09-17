#!/bin/bash
# 构建仅供提供方 CI 使用的工具。它们不属于 Console 运行时套件，
# 必须留在 dist/ci/，避免被 release.yml 的 dist/dec-* 扫描进 RUP。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${OUTPUT_DIR:-${ROOT}/dist/ci}"
TARGET_OS="${GOOS:-linux}"
TARGET_ARCH="${GOARCH:-amd64}"
EXT=""
if [ "${TARGET_OS}" = "windows" ]; then
  EXT=".exe"
fi

mkdir -p "${OUTPUT_DIR}"
CGO_ENABLED=0 GOOS="${TARGET_OS}" GOARCH="${TARGET_ARCH}" go build \
  -trimpath \
  -o "${OUTPUT_DIR}/dec-registry-${TARGET_OS}-${TARGET_ARCH}${EXT}" \
  "${ROOT}/cmd/dec-registry"

echo "${OUTPUT_DIR}/dec-registry-${TARGET_OS}-${TARGET_ARCH}${EXT}"
