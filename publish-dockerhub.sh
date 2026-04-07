#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR_DEFAULT="$(cd "${ROOT_DIR}/../Cli-Proxy-API-Management-Center" && pwd)"

FRONTEND_DIR="${FRONTEND_DIR:-${FRONTEND_DIR_DEFAULT}}"
IMAGE_NAME="${IMAGE_NAME:-sunchaowang/cli-proxy-api}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"
BUILDER_NAME="${BUILDER_NAME:-codex-multiarch-builder}"

cd "${ROOT_DIR}"

if [[ ! -d "${FRONTEND_DIR}" ]]; then
  echo "Frontend directory not found: ${FRONTEND_DIR}" >&2
  exit 1
fi

if [[ ! -f "${FRONTEND_DIR}/package.json" ]]; then
  echo "Frontend package.json not found in: ${FRONTEND_DIR}" >&2
  exit 1
fi

VERSION="$(git describe --tags --always)"
COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

echo "==> Building management frontend"
(
  cd "${FRONTEND_DIR}"
  npm run build
)

if [[ ! -f "${FRONTEND_DIR}/dist/management.html" ]]; then
  echo "Frontend build output missing: ${FRONTEND_DIR}/dist/management.html" >&2
  exit 1
fi

echo "==> Syncing management frontend into backend image context"
mkdir -p "${ROOT_DIR}/static"
cp "${FRONTEND_DIR}/dist/management.html" "${ROOT_DIR}/static/management.html"

echo "==> Ensuring buildx builder '${BUILDER_NAME}'"
if ! docker buildx inspect "${BUILDER_NAME}" >/dev/null 2>&1; then
  docker buildx create --name "${BUILDER_NAME}" --driver docker-container --use >/dev/null
fi
docker buildx use "${BUILDER_NAME}" >/dev/null
docker buildx inspect --bootstrap >/dev/null

echo "==> Publishing multi-arch image"
echo "    image: ${IMAGE_NAME}"
echo "    tags : latest, ${VERSION}, ${COMMIT}"
echo "    plats: ${PLATFORMS}"

docker buildx build \
  --builder "${BUILDER_NAME}" \
  --platform "${PLATFORMS}" \
  --build-arg VERSION="${VERSION}" \
  --build-arg COMMIT="${COMMIT}" \
  --build-arg BUILD_DATE="${BUILD_DATE}" \
  -t "${IMAGE_NAME}:latest" \
  -t "${IMAGE_NAME}:${VERSION}" \
  -t "${IMAGE_NAME}:${COMMIT}" \
  --push \
  .

echo "==> Published successfully"
echo "    ${IMAGE_NAME}:latest"
echo "    ${IMAGE_NAME}:${VERSION}"
echo "    ${IMAGE_NAME}:${COMMIT}"

