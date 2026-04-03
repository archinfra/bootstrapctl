#!/usr/bin/env bash

set -Eeuo pipefail

APP_NAME="k8s-sealos"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON_DIR="${SCRIPT_DIR}/common"
WORKDIR="/tmp/${APP_NAME}-installer.$$"
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() {
  echo -e "${CYAN}[INFO]${NC} $*"
}

note() {
  echo -e "${YELLOW}[NOTE]${NC} $*"
}

cleanup() {
  rm -rf "${WORKDIR}"
}

trap cleanup EXIT

wants_fast_help() {
  local arg

  if [[ $# -eq 0 ]]; then
    return 0
  fi

  for arg in "$@"; do
    case "${arg}" in
      --)
        break
        ;;
      -h|--help)
        return 0
        ;;
    esac
  done

  return 1
}

show_fast_help() {
  cat <<'EOF'
OneKube Kubernetes 离线安装包

用法:
  ./k8s-sealos-linux-<arch>-<bundle>.run install|reset|precheck|show-defaults [选项] [-- <额外 sealos 参数>]

命令说明:
  install                     安装 Kubernetes 集群
  reset                       重置 Kubernetes 集群
  precheck                    只执行预检查，不真正安装
  show-defaults               查看安装包内置默认值

SSH 参数:
  --user <username>           SSH 用户名
  --passwd <password>         SSH 密码
  --pk <path>                 SSH 私钥路径
  --pk-passwd <password>      SSH 私钥口令
  --port <port>               SSH 端口，默认 22

安装参数:
  --masters <ip,ip>           主节点 IP 列表，逗号分隔
  --nodes <ip,ip>             工作节点 IP 列表，逗号分隔
  --data-root <path>          Sealos 数据目录，默认 /data
  --cri-data <path>           容器运行时数据目录，默认 /data/containerd
  --cni-helm-opts <args>      额外传给 Cilium Helm 的参数
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

推荐用法:
  1. 密码登录安装
     ./k8s-sealos-linux-amd64-full.run install \
       --masters 10.0.0.11 \
       --nodes 10.0.0.21 \
       --passwd 'your-password' \
       --yes

  2. 私钥免密安装
     ./k8s-sealos-linux-amd64-full.run install \
       --masters 10.0.0.11 \
       --nodes 10.0.0.21 \
       --user root \
       --pk /root/.ssh/id_rsa \
       --yes

  3. 先做预检查
     ./k8s-sealos-linux-amd64-full.run precheck \
       --masters 10.0.0.11 \
       --nodes 10.0.0.21 \
       --user root \
       --pk /root/.ssh/id_rsa \
       --yes

重要说明:
  - 帮助路径是秒开的，不会先解压整个 .run 安装包。
  - 真正执行 install/reset/precheck 时，安装包才会开始解压 payload。
  - full 全离线包体积更大，解压会比 lite 半离线包慢一些。
  - 当前默认使用 Cilium 1.17.1。
  - 安装命令默认不会再自动附加 ExtraValues / HELM_OPTS。
EOF
}

bootstrap_payload() {
  local payload_line

  info "Preparing installer payload from self-extracting package"
  note "Full offline bundles can take a while to unpack on slower disks"

  mkdir -p "${WORKDIR}"
  payload_line="$(awk '/^__PAYLOAD_BELOW__$/ { print NR + 1; exit }' "$0")"
  [[ -n "${payload_line}" ]] || {
    echo "[ERROR] package payload marker not found" >&2
    exit 1
  }

  tail -n +"${payload_line}" "$0" | tar -xz -C "${WORKDIR}" || {
    echo "[ERROR] failed to extract payload" >&2
    exit 1
  }

  info "Installer payload is ready: ${WORKDIR}"
}

if wants_fast_help "$@"; then
  show_fast_help
  exit 0
fi

if [[ -f "${COMMON_DIR}/install-common.sh" && -f "${SCRIPT_DIR}/versions.env" ]]; then
  export K8S_SEALOS_CONTEXT_DIR="${SCRIPT_DIR}"
  # shellcheck disable=SC1091
  source "${COMMON_DIR}/install-common.sh"
else
  bootstrap_payload
  export K8S_SEALOS_CONTEXT_DIR="${WORKDIR}"
  # shellcheck disable=SC1091
  source "${WORKDIR}/lib/install-common.sh"
fi

k8s_sealos_install_main "$@"
exit 0
__PAYLOAD_BELOW__
