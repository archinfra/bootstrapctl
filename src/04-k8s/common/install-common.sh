#!/usr/bin/env bash

set -Eeuo pipefail

: "${K8S_SEALOS_CONTEXT_DIR:?K8S_SEALOS_CONTEXT_DIR is required}"

APP_NAME="${APP_NAME:-k8s-sealos}"
CONTEXT_DIR="${K8S_SEALOS_CONTEXT_DIR}"
SYSTEM_BIN_DIR="/usr/local/bin"
STATE_DIR="/etc/${APP_NAME}"
ENV_FILE="${STATE_DIR}/cluster.env"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

ACTION="install"
MASTERS=""
NODES=""
PASSWD=""
SSH_USER=""
SSH_PK=""
SSH_PK_PASSWD=""
PORT="22"
DATA_ROOT="/data"
CRI_DATA="/data/containerd"
CNI_HELM_OPTS=""

SEALOS_VERSION=""
IMAGE_REGISTRY=""
K8S_IMAGE_NAME=""
K8S_VERSION=""
HELM_IMAGE_NAME=""
HELM_VERSION=""
CNI_IMAGE_NAME=""
CNI_VERSION=""
ARCH="${ARCH:-unknown}"
PLATFORM="${PLATFORM:-unknown}"
PACKAGE_VARIANT="${PACKAGE_VARIANT:-unknown}"
INCLUDE_IMAGES="${INCLUDE_IMAGES:-unknown}"
ARCH_OVERRIDE=""

FORCE="false"
DEBUG="false"
AUTO_YES="false"
SKIP_IMAGE_LOAD="false"
SKIP_BINARY_INSTALL="false"
SKIP_PRECHECK="false"
DRY_RUN="false"

SEALOS_EXTRA_ARGS=()

PAYLOAD_ROOT=""
BIN_DIR=""
IMAGE_DIR=""
SEALOS_RUNTIME_ROOT=""
SEALOS_DATA_ROOT=""
K8S_IMAGE=""
HELM_IMAGE=""
CNI_IMAGE=""

log() {
  echo -e "${CYAN}[INFO]${NC} $*"
}

success() {
  echo -e "${GREEN}[OK]${NC} $*"
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
用法:
  ./installer.run install|reset|precheck|show-defaults [选项] [-- <额外 sealos 参数>]

命令:
  install                     安装 Kubernetes 集群
  reset                       重置 Kubernetes 集群
  precheck                    只做预检查，不实际安装
  show-defaults               显示安装包内置默认值

常用选项:
  --masters <ip,ip>           主节点 IP 列表，逗号分隔
  --nodes <ip,ip>             工作节点 IP 列表，逗号分隔
  --user <username>           SSH 用户名
  --passwd <password>         SSH 密码
  --pk <path>                 SSH 私钥路径
  --pk-passwd <password>      SSH 私钥口令
  --port <port>               SSH 端口，默认 22
  --data-root <path>          Sealos 数据目录，默认 /data
  --cri-data <path>           容器运行时数据目录，默认 /data/containerd
  --cni-helm-opts <args>      额外传给 Cilium 的 ExtraValues 值
  --registry <registry>       覆盖默认镜像仓库前缀
  --k8s-version <tag>         临时覆盖 Kubernetes 镜像 tag
  --helm-version <tag>        临时覆盖 Helm 镜像 tag
  --cni-version <tag>         临时覆盖 Cilium 镜像 tag
  --skip-image-load           跳过本地镜像导入
  --skip-binary-install       跳过安装 Sealos 二进制
  --skip-precheck             跳过预检查
  --dry-run                   只打印最终 sealos 命令，不实际执行
  --force                     透传 --force 给 sealos，并跳过确认
  --debug                     打开调试输出
  -y, --yes                   自动确认
  -h, --help                  显示帮助
EOF
}

refresh_runtime_values() {
  SEALOS_RUNTIME_ROOT="${DATA_ROOT}/.sealos"
  SEALOS_DATA_ROOT="${DATA_ROOT}/sealos"
  K8S_IMAGE="${IMAGE_REGISTRY}/${K8S_IMAGE_NAME}:${K8S_VERSION}"
  HELM_IMAGE="${IMAGE_REGISTRY}/${HELM_IMAGE_NAME}:${HELM_VERSION}"
  CNI_IMAGE="${IMAGE_REGISTRY}/${CNI_IMAGE_NAME}:${CNI_VERSION}"
}

normalize_semver() {
  local version="${1#v}"
  local core="${version%%[-+]*}"
  local major minor patch

  IFS='.' read -r major minor patch <<<"${core}"
  major="${major:-0}"
  minor="${minor:-0}"
  patch="${patch:-0}"

  printf '%03d%03d%03d\n' "${major}" "${minor}" "${patch}"
}

version_ge() {
  local left right

  left="$(normalize_semver "$1")"
  right="$(normalize_semver "$2")"
  (( 10#"${left}" >= 10#"${right}" ))
}

sealos_extra_contains_env() {
  local key="$1"
  local i arg next

  for ((i = 0; i < ${#SEALOS_EXTRA_ARGS[@]}; i++)); do
    arg="${SEALOS_EXTRA_ARGS[i]}"
    next="${SEALOS_EXTRA_ARGS[i+1]:-}"

    case "${arg}" in
      -e|--env)
        [[ "${next}" == "${key}"=* ]] && return 0
        ;;
      -e${key}=*|--env=${key}=*)
        return 0
        ;;
    esac
  done

  return 1
}

resolve_cni_helm_opts() {
  if [[ -n "${CNI_HELM_OPTS}" ]]; then
    return 0
  fi

  if sealos_extra_contains_env "ExtraValues"; then
    return 0
  fi

  if [[ "${CNI_IMAGE_NAME}" == "cilium" ]] && version_ge "${CNI_VERSION}" "1.18.0"; then
    CNI_HELM_OPTS="kubeProxyReplacement=false"
  fi

  return 0
}

load_runtime_metadata() {
  local versions_file="${CONTEXT_DIR}/versions.env"
  local manifest_file="${CONTEXT_DIR}/release-manifest.env"
  local binary

  [[ -f "${versions_file}" ]] || die "Missing versions file: ${versions_file}"
  [[ -d "${CONTEXT_DIR}/bin" ]] || die "Missing binary directory: ${CONTEXT_DIR}/bin"
  [[ -d "${CONTEXT_DIR}/images" ]] || die "Missing image directory: ${CONTEXT_DIR}/images"

  # shellcheck disable=SC1090
  source "${versions_file}"

  if [[ -f "${manifest_file}" ]]; then
    # shellcheck disable=SC1090
    source "${manifest_file}"
  fi

  PAYLOAD_ROOT="${CONTEXT_DIR}"
  BIN_DIR="${CONTEXT_DIR}/bin"
  IMAGE_DIR="${CONTEXT_DIR}/images"

  for binary in sealos sealctl image-cri-shim lvscare; do
    if [[ -f "${BIN_DIR}/${binary}" ]]; then
      chmod +x "${BIN_DIR}/${binary}" || true
    fi
  done

  refresh_runtime_values
  resolve_cni_helm_opts
}

load_show_defaults_metadata() {
  local versions_file="${CONTEXT_DIR}/versions.env"
  local source_versions_file="${CONTEXT_DIR}/common/component-versions.env"

  if [[ -f "${versions_file}" ]]; then
    # shellcheck disable=SC1090
    source "${versions_file}"
  elif [[ -f "${source_versions_file}" ]]; then
    # shellcheck disable=SC1090
    source "${source_versions_file}"
    ARCH="${ARCH_OVERRIDE:-amd64}"
    PLATFORM="linux/${ARCH}"
    PACKAGE_VARIANT="source"
    INCLUDE_IMAGES="n/a"
  else
    die "Unable to find version metadata"
  fi

  refresh_runtime_values
  resolve_cni_helm_opts
}

parse_args() {
  [[ $# -eq 0 ]] && {
    usage
    exit 0
  }

  while [[ $# -gt 0 ]]; do
    case "$1" in
      install|reset|precheck|show-defaults)
        ACTION="$1"
        shift
        ;;
      --masters)
        MASTERS="$2"
        shift 2
        ;;
      --nodes)
        NODES="$2"
        shift 2
        ;;
      --user)
        SSH_USER="$2"
        shift 2
        ;;
      --passwd)
        PASSWD="$2"
        shift 2
        ;;
      --pk)
        SSH_PK="$2"
        shift 2
        ;;
      --pk-passwd)
        SSH_PK_PASSWD="$2"
        shift 2
        ;;
      --port)
        PORT="$2"
        shift 2
        ;;
      --data-root)
        DATA_ROOT="$2"
        shift 2
        ;;
      --cri-data|--criData)
        CRI_DATA="$2"
        shift 2
        ;;
      --cni-helm-opts)
        CNI_HELM_OPTS="$2"
        shift 2
        ;;
      --registry)
        IMAGE_REGISTRY="$2"
        shift 2
        ;;
      --k8s-version)
        K8S_VERSION="$2"
        shift 2
        ;;
      --helm-version)
        HELM_VERSION="$2"
        shift 2
        ;;
      --cni-version)
        CNI_VERSION="$2"
        shift 2
        ;;
      --arch)
        ARCH_OVERRIDE="$2"
        shift 2
        ;;
      --skip-image-load)
        SKIP_IMAGE_LOAD="true"
        shift
        ;;
      --skip-binary-install)
        SKIP_BINARY_INSTALL="true"
        shift
        ;;
      --skip-precheck)
        SKIP_PRECHECK="true"
        shift
        ;;
      --dry-run)
        DRY_RUN="true"
        shift
        ;;
      --force)
        FORCE="true"
        AUTO_YES="true"
        shift
        ;;
      --debug)
        DEBUG="true"
        shift
        ;;
      -y|--yes)
        AUTO_YES="true"
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      --)
        shift
        while [[ $# -gt 0 ]]; do
          SEALOS_EXTRA_ARGS+=("$1")
          shift
        done
        ;;
      *)
        die "Unknown argument: $1"
        ;;
    esac
  done

  refresh_runtime_values
}

require_root() {
  [[ "${EUID}" -eq 0 ]] || die "Please run as root"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Missing command: $1"
}

prepare_runtime_layout() {
  load_runtime_metadata
  log "Using runtime directory: ${PAYLOAD_ROOT}"
}

validate_args() {
  local default_pk=""

  case "${ACTION}" in
    install|reset|precheck)
      [[ -n "${MASTERS}" ]] || die "At least one master node is required via --masters"
      ;;
  esac

  if [[ -n "${SSH_PK}" && ! -f "${SSH_PK}" ]]; then
    die "SSH private key not found: ${SSH_PK}"
  fi

  if [[ -z "${PASSWD}" && -z "${SSH_PK}" ]]; then
    default_pk="${HOME:-/root}/.ssh/id_rsa"
    if [[ ! -f "${default_pk}" ]]; then
      die "No SSH authentication material found. Pass --passwd, or pass --pk/--user, or ensure the default private key exists at ${default_pk}"
    fi
  fi
}

show_defaults() {
  cat <<EOF
arch:             ${ARCH}
platform:         ${PLATFORM}
packageVariant:   ${PACKAGE_VARIANT}
includeImages:    ${INCLUDE_IMAGES}
sealosVersion:    ${SEALOS_VERSION}
kubernetesImage:  ${K8S_IMAGE}
helmImage:        ${HELM_IMAGE}
ciliumImage:      ${CNI_IMAGE}

defaults:
  data-root:      ${DATA_ROOT}
  cri-data:       ${CRI_DATA}
  ssh-port:       ${PORT}
  cni-helm-opts:  ${CNI_HELM_OPTS:-<none>}
EOF
}

run_prechecks() {
  local probe_dir

  require_cmd tar
  require_cmd awk
  require_cmd tail
  require_cmd grep
  require_cmd install
  require_cmd df

  [[ -f "${BIN_DIR}/sealos" ]] || die "Missing sealos binary: ${BIN_DIR}/sealos"
  [[ -f "${BIN_DIR}/sealctl" ]] || warn "Missing sealctl binary: ${BIN_DIR}/sealctl"
  [[ -f "${IMAGE_DIR}/image.json" ]] || warn "Missing image.json"

  probe_dir="${DATA_ROOT}"
  while [[ ! -d "${probe_dir}" && "${probe_dir}" != "/" ]]; do
    probe_dir="$(dirname "${probe_dir}")"
  done
  [[ -d "${probe_dir}" ]] || probe_dir="/"

  log "Precheck passed"
  df -h "${probe_dir}" | tail -n 1
}

confirm_plan() {
  [[ "${AUTO_YES}" == "true" ]] && return 0

  cat <<EOF
==================================================
action:            ${ACTION}
masters:           ${MASTERS}
nodes:             ${NODES:-<none>}
sshUser:           ${SSH_USER:-<sealos-default>}
sshPort:           ${PORT}
sshKey:            ${SSH_PK:-<default ~/.ssh/id_rsa>}
dataRoot:          ${DATA_ROOT}
criData:           ${CRI_DATA}
cniHelmOpts:       ${CNI_HELM_OPTS:-<none>}
packageVariant:    ${PACKAGE_VARIANT}
includeImages:     ${INCLUDE_IMAGES}
sealosVersion:     ${SEALOS_VERSION}
kubernetesImage:   ${K8S_IMAGE}
helmImage:         ${HELM_IMAGE}
ciliumImage:       ${CNI_IMAGE}
skipBinaryInstall: ${SKIP_BINARY_INSTALL}
skipImageLoad:     ${SKIP_IMAGE_LOAD}
dryRun:            ${DRY_RUN}
==================================================
EOF

  read -r -p "Continue? [y/N]: " answer
  [[ "${answer}" =~ ^[Yy]$ ]] || die "Canceled"
}

backup_if_exists() {
  local target="$1"

  if [[ -f "${target}" ]]; then
    mv -f "${target}" "${target}.bak.$(date +%Y%m%d%H%M%S)"
  fi
}

install_binaries() {
  local binary

  mkdir -p "${SYSTEM_BIN_DIR}"

  for binary in sealos sealctl image-cri-shim lvscare; do
    [[ -f "${BIN_DIR}/${binary}" ]] || continue
    backup_if_exists "${SYSTEM_BIN_DIR}/${binary}"
    install -m 0755 "${BIN_DIR}/${binary}" "${SYSTEM_BIN_DIR}/${binary}"
    success "Installed ${SYSTEM_BIN_DIR}/${binary}"
  done
}

ensure_sealos_available() {
  command -v sealos >/dev/null 2>&1 || die "sealos command not found"
  log "Sealos version: $(sealos version 2>/dev/null | head -n 1 || echo unknown)"
}

load_images() {
  local image_tar
  local loaded=0

  if ! compgen -G "${IMAGE_DIR}/*.tar" >/dev/null 2>&1; then
    warn "No image tar files found, skipping image import"
    return 0
  fi

  for image_tar in "${IMAGE_DIR}"/*.tar; do
    log "Loading $(basename "${image_tar}")"
    if sealos load -i "${image_tar}"; then
      loaded=$((loaded + 1))
    else
      warn "Failed to load $(basename "${image_tar}")"
    fi
  done

  [[ "${loaded}" -gt 0 ]] || warn "No image tar file was imported successfully"
}

write_environment_file() {
  mkdir -p "${STATE_DIR}"

  cat > "${ENV_FILE}" <<EOF
# Generated by ${APP_NAME}
export ARCH="${ARCH}"
export PLATFORM="${PLATFORM}"
export PACKAGE_VARIANT="${PACKAGE_VARIANT}"
export INCLUDE_IMAGES="${INCLUDE_IMAGES}"
export SEALOS_VERSION="${SEALOS_VERSION}"
export IMAGE_REGISTRY="${IMAGE_REGISTRY}"
export K8S_IMAGE_NAME="${K8S_IMAGE_NAME}"
export K8S_VERSION="${K8S_VERSION}"
export HELM_IMAGE_NAME="${HELM_IMAGE_NAME}"
export HELM_VERSION="${HELM_VERSION}"
export CNI_IMAGE_NAME="${CNI_IMAGE_NAME}"
export CNI_VERSION="${CNI_VERSION}"
export SEALOS_RUNTIME_ROOT="${SEALOS_RUNTIME_ROOT}"
export SEALOS_DATA_ROOT="${SEALOS_DATA_ROOT}"
export SEALOS_SCP_CHECKSUM="false"
export SEALOS_REGISTRY_SKIP_TLS="true"
export DATA_ROOT="${DATA_ROOT}"
export CRI_DATA="${CRI_DATA}"
export CNI_HELM_OPTS="${CNI_HELM_OPTS}"
export MASTERS="${MASTERS}"
export NODES="${NODES}"
export SSH_USER="${SSH_USER}"
export SSH_PK="${SSH_PK}"
EOF

  success "Wrote ${ENV_FILE}"
}

run_sealos() {
  local -a env_cmd
  local -a sealos_cmd

  env_cmd=(
    env
    "SEALOS_RUNTIME_ROOT=${SEALOS_RUNTIME_ROOT}"
    "SEALOS_DATA_ROOT=${SEALOS_DATA_ROOT}"
    "SEALOS_SCP_CHECKSUM=false"
    "SEALOS_REGISTRY_SKIP_TLS=true"
  )

  if [[ "${ACTION}" == "install" ]]; then
    sealos_cmd=(sealos run "${K8S_IMAGE}" "${HELM_IMAGE}" "${CNI_IMAGE}")
    sealos_cmd+=(--masters "${MASTERS}")
    [[ -n "${NODES}" ]] && sealos_cmd+=(--nodes "${NODES}")
    [[ -n "${SSH_USER}" ]] && sealos_cmd+=(--user "${SSH_USER}")
    [[ -n "${PASSWD}" ]] && sealos_cmd+=(--passwd "${PASSWD}")
    [[ -n "${SSH_PK}" ]] && sealos_cmd+=(--pk "${SSH_PK}")
    [[ -n "${SSH_PK_PASSWD}" ]] && sealos_cmd+=(--pk-passwd "${SSH_PK_PASSWD}")
    [[ -n "${PORT}" ]] && sealos_cmd+=(--port "${PORT}")
    sealos_cmd+=(-e "criData=${CRI_DATA}")
    if [[ -n "${CNI_HELM_OPTS}" ]]; then
      log "Applying Cilium ExtraValues: ${CNI_HELM_OPTS}"
      sealos_cmd+=(-e "ExtraValues=${CNI_HELM_OPTS}")
    fi
  else
    sealos_cmd=(sealos reset)
    sealos_cmd+=(--masters "${MASTERS}")
    [[ -n "${NODES}" ]] && sealos_cmd+=(--nodes "${NODES}")
    [[ -n "${SSH_USER}" ]] && sealos_cmd+=(--user "${SSH_USER}")
    [[ -n "${PASSWD}" ]] && sealos_cmd+=(--passwd "${PASSWD}")
    [[ -n "${SSH_PK}" ]] && sealos_cmd+=(--pk "${SSH_PK}")
    [[ -n "${SSH_PK_PASSWD}" ]] && sealos_cmd+=(--pk-passwd "${SSH_PK_PASSWD}")
    [[ -n "${PORT}" ]] && sealos_cmd+=(--port "${PORT}")

    if [[ "${AUTO_YES}" == "true" && "${FORCE}" != "true" ]]; then
      sealos_cmd+=(--force)
    fi
  fi

  [[ "${FORCE}" == "true" ]] && sealos_cmd+=(--force)
  [[ "${DEBUG}" == "true" ]] && sealos_cmd+=(--debug)

  if [[ "${#SEALOS_EXTRA_ARGS[@]}" -gt 0 ]]; then
    sealos_cmd+=("${SEALOS_EXTRA_ARGS[@]}")
  fi

  log "Executing command:"
  printf '  %q ' "${env_cmd[@]}" "${sealos_cmd[@]}"
  printf '\n'

  if [[ "${DRY_RUN}" == "true" ]]; then
    warn "Dry-run enabled, sealos was not executed"
    return 0
  fi

  "${env_cmd[@]}" "${sealos_cmd[@]}"
}

show_post_install_info() {
  if [[ "${DRY_RUN}" == "true" ]]; then
    cat <<EOF

Dry-run completed.
Environment file:
  ${ENV_FILE}
EOF
    return
  fi

  cat <<EOF

Cluster operation completed.

Next commands:
  source ${ENV_FILE}
  kubectl get nodes -o wide
  sealos images
EOF
}

k8s_sealos_install_main() {
  parse_args "$@"

  if [[ "${DEBUG}" == "true" ]]; then
    set -x
  fi

  if [[ "${ACTION}" == "show-defaults" ]]; then
    load_show_defaults_metadata
    show_defaults
    exit 0
  fi

  prepare_runtime_layout
  require_root
  validate_args

  if [[ "${SKIP_PRECHECK}" != "true" ]]; then
    run_prechecks
  fi

  if [[ "${ACTION}" == "precheck" ]]; then
    success "precheck complete"
    exit 0
  fi

  confirm_plan

  if [[ "${SKIP_BINARY_INSTALL}" != "true" ]]; then
    install_binaries
  fi

  ensure_sealos_available

  if [[ "${ACTION}" == "install" && "${SKIP_IMAGE_LOAD}" != "true" ]]; then
    load_images
  fi

  write_environment_file
  run_sealos
  show_post_install_info
}
