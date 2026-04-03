#!/usr/bin/env bash

set -Eeuo pipefail

: "${K8S_SEALOS_CONTEXT_DIR:?K8S_SEALOS_CONTEXT_DIR is required}"
: "${K8S_SEALOS_COMMON_DIR:?K8S_SEALOS_COMMON_DIR is required}"

ROOT_DIR="${K8S_SEALOS_CONTEXT_DIR}"
COMMON_DIR="${K8S_SEALOS_COMMON_DIR}"
COMPONENT_VERSIONS_FILE="${COMMON_DIR}/component-versions.env"
INSTALL_SCRIPT="${ROOT_DIR}/install.sh"
INSTALL_COMMON_FILE="${COMMON_DIR}/install-common.sh"
CACHE_DIR="${ROOT_DIR}/.cache"
DOWNLOAD_CACHE_DIR="${CACHE_DIR}/downloads"
BINARY_CACHE_ROOT="${CACHE_DIR}/bin"
IMAGE_CACHE_ROOT="${CACHE_DIR}/images"
BUILD_ROOT="${ROOT_DIR}/.build"
DIST_DIR="${ROOT_DIR}/dist"

ARCH_SELECTOR="all"
BUNDLE_SELECTOR="all"
DOWNLOAD_BINARIES="true"
PREPARE_IMAGES="true"
FORCE="false"
CLEAN_ONLY="false"
SEALOS_ARCHIVE_FILE="${SEALOS_ARCHIVE_FILE:-}"
SEALOS_ARCHIVE_DIR="${SEALOS_ARCHIVE_DIR:-}"
SEALOS_ARCHIVE_URL="${SEALOS_ARCHIVE_URL:-}"
SEALOS_DOWNLOAD_BASE_URL="${SEALOS_DOWNLOAD_BASE_URL:-}"

RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log() {
  echo -e "${CYAN}[INFO]${NC} $*"
}

warn() {
  echo -e "${YELLOW}[WARN]${NC} $*" >&2
}

die() {
  echo -e "${RED}[ERROR]${NC} $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage:
  ./build.sh [options]

Options:
  --arch <amd64|arm64|all>    Target architecture, default: all
  --bundle <full|lite|all>    Package variant, default: all
  --skip-binary-download      Reuse cached Sealos binaries
  --skip-image-prepare        Reuse cached image tar files for full bundles
  --sealos-archive-file <p>   Use one pre-downloaded Sealos archive file
  --sealos-archive-dir <dir>  Use pre-downloaded Sealos archives from a directory
  --sealos-archive-url <url>  Download one Sealos archive from a custom URL
  --sealos-download-base <u>  Download Sealos archives from a custom base URL
  --force                     Refresh binaries and image tar files
  --clean                     Remove .build and dist, then exit
  -h, --help                  Show help

Package variants:
  full  Includes Sealos binaries and offline image tar files
  lite  Includes Sealos binaries only, no image tar files

Sealos source overrides:
  The build first reuses .cache/downloads when available. If a refresh is needed,
  you can override the default GitHub release source with one of:
    --sealos-archive-file  /data/pkg/sealos_5.1.1_linux_amd64.tar.gz
    --sealos-archive-dir   /data/pkg/sealos/
    --sealos-archive-url   https://mirror.example.com/sealos_5.1.1_linux_amd64.tar.gz
    --sealos-download-base https://mirror.example.com/sealos
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --arch)
        ARCH_SELECTOR="$2"
        shift 2
        ;;
      --bundle)
        BUNDLE_SELECTOR="$2"
        shift 2
        ;;
      --skip-binary-download)
        DOWNLOAD_BINARIES="false"
        shift
        ;;
      --skip-image-prepare)
        PREPARE_IMAGES="false"
        shift
        ;;
      --sealos-archive-file)
        SEALOS_ARCHIVE_FILE="$2"
        shift 2
        ;;
      --sealos-archive-dir)
        SEALOS_ARCHIVE_DIR="$2"
        shift 2
        ;;
      --sealos-archive-url)
        SEALOS_ARCHIVE_URL="$2"
        shift 2
        ;;
      --sealos-download-base)
        SEALOS_DOWNLOAD_BASE_URL="$2"
        shift 2
        ;;
      --force)
        FORCE="true"
        shift
        ;;
      --clean)
        CLEAN_ONLY="true"
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "Unknown argument: $1"
        ;;
    esac
  done
}

load_component_versions() {
  [[ -f "${COMPONENT_VERSIONS_FILE}" ]] || die "Missing versions file: ${COMPONENT_VERSIONS_FILE}"
  [[ -f "${INSTALL_SCRIPT}" ]] || die "Missing install header: ${INSTALL_SCRIPT}"
  [[ -f "${INSTALL_COMMON_FILE}" ]] || die "Missing shared install logic: ${INSTALL_COMMON_FILE}"

  # shellcheck disable=SC1090
  source "${COMPONENT_VERSIONS_FILE}"

  : "${SEALOS_VERSION:?SEALOS_VERSION is required}"
  : "${IMAGE_REGISTRY:?IMAGE_REGISTRY is required}"
  : "${K8S_IMAGE_NAME:?K8S_IMAGE_NAME is required}"
  : "${K8S_VERSION:?K8S_VERSION is required}"
  : "${HELM_IMAGE_NAME:?HELM_IMAGE_NAME is required}"
  : "${HELM_VERSION:?HELM_VERSION is required}"
  : "${CNI_IMAGE_NAME:?CNI_IMAGE_NAME is required}"
  : "${CNI_VERSION:?CNI_VERSION is required}"
  : "${K8S_IMAGE_TAR:?K8S_IMAGE_TAR is required}"
  : "${HELM_IMAGE_TAR:?HELM_IMAGE_TAR is required}"
  : "${CNI_IMAGE_TAR:?CNI_IMAGE_TAR is required}"
}

resolve_arches() {
  case "${ARCH_SELECTOR}" in
    amd64|arm64)
      echo "${ARCH_SELECTOR}"
      ;;
    all)
      echo "amd64 arm64"
      ;;
    *)
      die "Unsupported arch selector: ${ARCH_SELECTOR}"
      ;;
  esac
}

resolve_bundles() {
  case "${BUNDLE_SELECTOR}" in
    full|lite)
      echo "${BUNDLE_SELECTOR}"
      ;;
    all)
      echo "full lite"
      ;;
    *)
      die "Unsupported bundle selector: ${BUNDLE_SELECTOR}"
      ;;
  esac
}

set_arch_context() {
  ARCH="$1"

  case "${ARCH}" in
    amd64)
      PLATFORM="linux/amd64"
      ;;
    arm64)
      PLATFORM="linux/arm64"
      ;;
    *)
      die "Unsupported arch: ${ARCH}"
      ;;
  esac

  BINARY_CACHE_DIR="${BINARY_CACHE_ROOT}/${ARCH}"
  IMAGE_CACHE_DIR="${IMAGE_CACHE_ROOT}/${ARCH}"
}

set_bundle_context() {
  PACKAGE_VARIANT="$1"

  case "${PACKAGE_VARIANT}" in
    full)
      INCLUDE_IMAGES="true"
      ;;
    lite)
      INCLUDE_IMAGES="false"
      ;;
    *)
      die "Unsupported package variant: ${PACKAGE_VARIANT}"
      ;;
  esac

  INSTALLER_NAME="k8s-sealos-linux-${ARCH}-${PACKAGE_VARIANT}.run"
  BUILD_DIR="${BUILD_ROOT}/${ARCH}/${PACKAGE_VARIANT}"
  PAYLOAD_DIR="${BUILD_DIR}/payload"
  PAYLOAD_BIN_DIR="${PAYLOAD_DIR}/bin"
  PAYLOAD_IMAGE_DIR="${PAYLOAD_DIR}/images"
  PAYLOAD_LIB_DIR="${PAYLOAD_DIR}/lib"
  PAYLOAD_TAR="${BUILD_DIR}/payload.tar.gz"
  PAYLOAD_VERSIONS_FILE="${BUILD_DIR}/payload-versions.env"
  IMAGE_JSON="${BUILD_DIR}/image.json"
  IMAGE_MAPPING="${BUILD_DIR}/image-mapping.txt"
  MANIFEST_FILE="${BUILD_DIR}/release-manifest.env"
  INSTALLER_PATH="${DIST_DIR}/${INSTALLER_NAME}"
  CHECKSUM_PATH="${INSTALLER_PATH}.sha256"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Missing command: $1"
}

copy_local_sealos_archive() {
  local src_path="$1"
  local archive_path="$2"

  [[ -f "${src_path}" ]] || die "Sealos archive not found: ${src_path}"

  if [[ "${src_path}" != "${archive_path}" ]]; then
    cp -f "${src_path}" "${archive_path}"
  fi
}

resolve_sealos_archive_url() {
  local archive_name="$1"

  if [[ -n "${SEALOS_ARCHIVE_URL}" ]]; then
    printf '%s\n' "${SEALOS_ARCHIVE_URL}"
    return 0
  fi

  if [[ -n "${SEALOS_DOWNLOAD_BASE_URL}" ]]; then
    printf '%s/%s\n' "${SEALOS_DOWNLOAD_BASE_URL%/}" "${archive_name}"
    return 0
  fi

  printf 'https://github.com/labring/sealos/releases/download/%s/%s\n' "${SEALOS_VERSION}" "${archive_name}"
}

needs_docker() {
  local bundle

  for bundle in $(resolve_bundles); do
    if [[ "${bundle}" == "full" && "${PREPARE_IMAGES}" == "true" ]]; then
      return 0
    fi
  done

  return 1
}

ensure_requirements() {
  require_cmd curl
  require_cmd tar
  require_cmd sha256sum

  if needs_docker; then
    require_cmd docker
  fi
}

clean_artifacts() {
  rm -rf "${BUILD_ROOT}" "${DIST_DIR}"
}

prepare_build_directories() {
  mkdir -p \
    "${DOWNLOAD_CACHE_DIR}" \
    "${BINARY_CACHE_DIR}" \
    "${IMAGE_CACHE_DIR}" \
    "${BUILD_DIR}" \
    "${PAYLOAD_BIN_DIR}" \
    "${PAYLOAD_IMAGE_DIR}" \
    "${PAYLOAD_LIB_DIR}" \
    "${DIST_DIR}"
}

refresh_image_refs() {
  K8S_IMAGE="${IMAGE_REGISTRY}/${K8S_IMAGE_NAME}:${K8S_VERSION}"
  HELM_IMAGE="${IMAGE_REGISTRY}/${HELM_IMAGE_NAME}:${HELM_VERSION}"
  CNI_IMAGE="${IMAGE_REGISTRY}/${CNI_IMAGE_NAME}:${CNI_VERSION}"
  K8S_CACHE_TAR="${K8S_IMAGE_NAME}_${K8S_VERSION}_${ARCH}.tar"
  HELM_CACHE_TAR="${HELM_IMAGE_NAME}_${HELM_VERSION}_${ARCH}.tar"
  CNI_CACHE_TAR="${CNI_IMAGE_NAME}_${CNI_VERSION}_${ARCH}.tar"
}

download_sealos_binaries() {
  local archive_name
  local archive_path
  local archive_url
  local source_path

  archive_name="sealos_${SEALOS_VERSION#v}_linux_${ARCH}.tar.gz"
  archive_path="${DOWNLOAD_CACHE_DIR}/${archive_name}"

  if [[ "${FORCE}" == "false" ]] \
    && [[ -x "${BINARY_CACHE_DIR}/sealos" ]] \
    && [[ -x "${BINARY_CACHE_DIR}/sealctl" ]] \
    && [[ -x "${BINARY_CACHE_DIR}/image-cri-shim" ]] \
    && [[ -x "${BINARY_CACHE_DIR}/lvscare" ]]; then
    log "Using cached binaries for ${ARCH}"
    return
  fi

  if [[ -n "${SEALOS_ARCHIVE_FILE}" ]]; then
    log "Using local Sealos archive file for ${ARCH}: ${SEALOS_ARCHIVE_FILE}"
    copy_local_sealos_archive "${SEALOS_ARCHIVE_FILE}" "${archive_path}"
  elif [[ -n "${SEALOS_ARCHIVE_DIR}" ]]; then
    source_path="${SEALOS_ARCHIVE_DIR%/}/${archive_name}"
    log "Using local Sealos archive from directory for ${ARCH}: ${source_path}"
    copy_local_sealos_archive "${source_path}" "${archive_path}"
  else
    archive_url="$(resolve_sealos_archive_url "${archive_name}")"

    if [[ -n "${SEALOS_ARCHIVE_URL}" || -n "${SEALOS_DOWNLOAD_BASE_URL}" ]]; then
      log "Downloading Sealos archive for ${ARCH} from custom source"
    else
      log "Downloading Sealos release for ${ARCH}"
    fi

    curl -fL --retry 3 --retry-delay 2 \
      -o "${archive_path}" \
      "${archive_url}"
  fi

  rm -rf "${BINARY_CACHE_DIR}"
  mkdir -p "${BINARY_CACHE_DIR}"
  tar -xzf "${archive_path}" -C "${BINARY_CACHE_DIR}"
  chmod +x \
    "${BINARY_CACHE_DIR}/sealos" \
    "${BINARY_CACHE_DIR}/sealctl" \
    "${BINARY_CACHE_DIR}/image-cri-shim" \
    "${BINARY_CACHE_DIR}/lvscare"
}

ensure_cached_image() {
  local image_ref="$1"
  local cache_tar_name="$2"
  local tar_path="${IMAGE_CACHE_DIR}/${cache_tar_name}"

  if [[ "${PREPARE_IMAGES}" == "false" ]]; then
    [[ -f "${tar_path}" ]] || die "Missing cached image tar for ${ARCH}: ${tar_path}"
    return
  fi

  if [[ "${FORCE}" == "false" && -f "${tar_path}" ]]; then
    log "Using cached image tar ${cache_tar_name} for ${ARCH}"
    return
  fi

  log "Pulling ${image_ref} for ${PLATFORM}"
  docker pull --platform "${PLATFORM}" "${image_ref}"

  log "Saving ${cache_tar_name}"
  docker save -o "${tar_path}" "${image_ref}"
}

prepare_images() {
  ensure_cached_image "${CNI_IMAGE}" "${CNI_CACHE_TAR}"
  ensure_cached_image "${HELM_IMAGE}" "${HELM_CACHE_TAR}"
  ensure_cached_image "${K8S_IMAGE}" "${K8S_CACHE_TAR}"
}

write_image_manifest() {
  cat > "${IMAGE_JSON}" <<EOF
[
  {
    "pull": "${CNI_IMAGE}",
    "tag": "${CNI_IMAGE}",
    "tar": "${CNI_IMAGE_TAR}"
  },
  {
    "pull": "${HELM_IMAGE}",
    "tag": "${HELM_IMAGE}",
    "tar": "${HELM_IMAGE_TAR}"
  },
  {
    "pull": "${K8S_IMAGE}",
    "tag": "${K8S_IMAGE}",
    "tar": "${K8S_IMAGE_TAR}"
  }
]
EOF

  cat > "${IMAGE_MAPPING}" <<EOF
# Generated by build.sh
${CNI_IMAGE} -> ${CNI_IMAGE_TAR}
${HELM_IMAGE} -> ${HELM_IMAGE_TAR}
${K8S_IMAGE} -> ${K8S_IMAGE_TAR}
EOF
}

write_release_manifest() {
  cat > "${MANIFEST_FILE}" <<EOF
ARCH="${ARCH}"
PLATFORM="${PLATFORM}"
PACKAGE_VARIANT="${PACKAGE_VARIANT}"
INCLUDE_IMAGES="${INCLUDE_IMAGES}"
INSTALLER_NAME="${INSTALLER_NAME}"
SEALOS_VERSION="${SEALOS_VERSION}"
IMAGE_REGISTRY="${IMAGE_REGISTRY}"
K8S_IMAGE_NAME="${K8S_IMAGE_NAME}"
K8S_VERSION="${K8S_VERSION}"
K8S_IMAGE_TAR="${K8S_IMAGE_TAR}"
HELM_IMAGE_NAME="${HELM_IMAGE_NAME}"
HELM_VERSION="${HELM_VERSION}"
HELM_IMAGE_TAR="${HELM_IMAGE_TAR}"
CNI_IMAGE_NAME="${CNI_IMAGE_NAME}"
CNI_VERSION="${CNI_VERSION}"
CNI_IMAGE_TAR="${CNI_IMAGE_TAR}"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
EOF
}

write_payload_versions() {
  cat > "${PAYLOAD_VERSIONS_FILE}" <<EOF
SEALOS_VERSION="${SEALOS_VERSION}"
IMAGE_REGISTRY="${IMAGE_REGISTRY}"
K8S_IMAGE_NAME="${K8S_IMAGE_NAME}"
K8S_VERSION="${K8S_VERSION}"
K8S_IMAGE_TAR="${K8S_IMAGE_TAR}"
HELM_IMAGE_NAME="${HELM_IMAGE_NAME}"
HELM_VERSION="${HELM_VERSION}"
HELM_IMAGE_TAR="${HELM_IMAGE_TAR}"
CNI_IMAGE_NAME="${CNI_IMAGE_NAME}"
CNI_VERSION="${CNI_VERSION}"
CNI_IMAGE_TAR="${CNI_IMAGE_TAR}"
ARCH="${ARCH}"
PLATFORM="${PLATFORM}"
PACKAGE_VARIANT="${PACKAGE_VARIANT}"
INCLUDE_IMAGES="${INCLUDE_IMAGES}"
INSTALLER_NAME="${INSTALLER_NAME}"
EOF
}

stage_payload() {
  rm -rf "${PAYLOAD_DIR}"
  mkdir -p "${PAYLOAD_BIN_DIR}" "${PAYLOAD_IMAGE_DIR}" "${PAYLOAD_LIB_DIR}"

  cp -f "${BINARY_CACHE_DIR}/sealos" "${PAYLOAD_BIN_DIR}/"
  cp -f "${BINARY_CACHE_DIR}/sealctl" "${PAYLOAD_BIN_DIR}/"
  cp -f "${BINARY_CACHE_DIR}/image-cri-shim" "${PAYLOAD_BIN_DIR}/"
  cp -f "${BINARY_CACHE_DIR}/lvscare" "${PAYLOAD_BIN_DIR}/"
  chmod +x "${PAYLOAD_BIN_DIR}/"*

  cp -f "${IMAGE_JSON}" "${PAYLOAD_IMAGE_DIR}/image.json"
  cp -f "${IMAGE_MAPPING}" "${PAYLOAD_IMAGE_DIR}/image-mapping.txt"
  cp -f "${PAYLOAD_VERSIONS_FILE}" "${PAYLOAD_DIR}/versions.env"
  cp -f "${MANIFEST_FILE}" "${PAYLOAD_DIR}/release-manifest.env"
  cp -f "${INSTALL_COMMON_FILE}" "${PAYLOAD_LIB_DIR}/install-common.sh"

  if [[ "${INCLUDE_IMAGES}" == "true" ]]; then
    cp -f "${IMAGE_CACHE_DIR}/${CNI_CACHE_TAR}" "${PAYLOAD_IMAGE_DIR}/${CNI_IMAGE_TAR}"
    cp -f "${IMAGE_CACHE_DIR}/${HELM_CACHE_TAR}" "${PAYLOAD_IMAGE_DIR}/${HELM_IMAGE_TAR}"
    cp -f "${IMAGE_CACHE_DIR}/${K8S_CACHE_TAR}" "${PAYLOAD_IMAGE_DIR}/${K8S_IMAGE_TAR}"
  fi
}

create_installer() {
  tar -C "${PAYLOAD_DIR}" -czf "${PAYLOAD_TAR}" .
  cat "${INSTALL_SCRIPT}" "${PAYLOAD_TAR}" > "${INSTALLER_PATH}"
  chmod +x "${INSTALLER_PATH}"
  sha256sum "${INSTALLER_PATH}" > "${CHECKSUM_PATH}"
}

print_summary() {
  log "Built ${INSTALLER_NAME}"
  echo "  arch:          ${ARCH}"
  echo "  bundle:        ${PACKAGE_VARIANT}"
  echo "  includeImages: ${INCLUDE_IMAGES}"
  echo "  sealos:        ${SEALOS_VERSION}"
  echo "  kubernetes:    ${K8S_IMAGE}"
  echo "  helm:          ${HELM_IMAGE}"
  echo "  cilium:        ${CNI_IMAGE}"
  echo "  package:       ${INSTALLER_PATH}"
  echo "  checksum:      ${CHECKSUM_PATH}"
}

build_one_package() {
  refresh_image_refs
  prepare_build_directories

  if [[ "${DOWNLOAD_BINARIES}" == "true" ]]; then
    download_sealos_binaries
  else
    [[ -x "${BINARY_CACHE_DIR}/sealos" ]] || die "Missing cached binaries for ${ARCH}. Remove --skip-binary-download or prewarm .cache/bin/${ARCH}"
  fi

  if [[ "${INCLUDE_IMAGES}" == "true" ]]; then
    prepare_images
  fi

  write_image_manifest
  write_release_manifest
  write_payload_versions
  stage_payload
  create_installer
  print_summary
}

k8s_sealos_build_main() {
  local arch
  local bundle

  parse_args "$@"
  load_component_versions
  ensure_requirements

  if [[ "${CLEAN_ONLY}" == "true" ]]; then
    clean_artifacts
    log "Removed .build and dist"
    exit 0
  fi

  clean_artifacts

  for arch in $(resolve_arches); do
    set_arch_context "${arch}"

    for bundle in $(resolve_bundles); do
      set_bundle_context "${bundle}"
      build_one_package
    done
  done
}
