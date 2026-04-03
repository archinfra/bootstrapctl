#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON_DIR="${SCRIPT_DIR}/common"

export K8S_SEALOS_CONTEXT_DIR="${SCRIPT_DIR}"
export K8S_SEALOS_COMMON_DIR="${COMMON_DIR}"

# shellcheck disable=SC1091
source "${COMMON_DIR}/build-common.sh"

k8s_sealos_build_main "$@"
