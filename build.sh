#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
APP_NAME="bootstrapctl"
VERSION="${VERSION:-dev}"

PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
)

echo "[INFO] project: ${APP_NAME}"
echo "[INFO] version: ${VERSION}"
echo "[INFO] dist: ${DIST_DIR}"

rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"

CHECKSUM_FILE="${DIST_DIR}/checksums.txt"
: > "${CHECKSUM_FILE}"

for platform in "${PLATFORMS[@]}"; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"
  TARGET_DIR="${DIST_DIR}/${GOOS}-${GOARCH}"
  BINARY_PATH="${TARGET_DIR}/${APP_NAME}"
  ARCHIVE_NAME="${APP_NAME}_${VERSION}_${GOOS}_${GOARCH}.tar.gz"
  ARCHIVE_PATH="${DIST_DIR}/${ARCHIVE_NAME}"

  mkdir -p "${TARGET_DIR}"

  echo "[INFO] building ${GOOS}/${GOARCH}"
  CGO_ENABLED=0 \
  GOOS="${GOOS}" \
  GOARCH="${GOARCH}" \
  go build \
    -trimpath \
    -ldflags "-s -w -X github.com/yuanyp8/bootstrapctl/internal/app.version=${VERSION}" \
    -o "${BINARY_PATH}" \
    ./cmd/bootstrapctl

  tar -C "${TARGET_DIR}" -czf "${ARCHIVE_PATH}" "${APP_NAME}"
  sha256sum "${ARCHIVE_PATH}" >> "${CHECKSUM_FILE}"
done

echo "[INFO] build finished"
echo "[INFO] artifacts:"
ls -1 "${DIST_DIR}"
