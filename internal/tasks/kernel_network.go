package tasks

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// KernelNetworkTask 负责收敛 Kubernetes 节点依赖的内核模块和 sysctl 参数。
// 它会同时保证：
// - 运行时状态已生效
// - 持久化配置文件已写入
type KernelNetworkTask struct {
	NodeSpec config.NodeConnection
	Modules  []string
	Sysctls  map[string]string
}

func (t *KernelNetworkTask) Key() string   { return "kernel-network" }
func (t *KernelNetworkTask) Title() string { return "收敛 Kubernetes 内核网络参数" }
func (t *KernelNetworkTask) Node() string  { return t.NodeSpec.Name }

func (t *KernelNetworkTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	modulesFileB64 := encodeBase64(renderModulesFile(t.Modules))
	sysctlFileB64 := encodeBase64(renderSysctlFile(t.Sysctls))

	var checks []string
	for _, module := range t.Modules {
		checks = append(checks, fmt.Sprintf(`
if ! lsmod | awk '$1=="%s"{found=1} END{exit !found}'; then
  need=1
  details="${details}module:%s "
fi
`, module, module))
	}

	keys := make([]string, 0, len(t.Sysctls))
	for key := range t.Sysctls {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := t.Sysctls[key]
		checks = append(checks, fmt.Sprintf(`
if [ "$(sysctl -n %s 2>/dev/null || echo missing)" != "%s" ]; then
  need=1
  details="${details}sysctl:%s "
fi
`, key, value, key))
	}

	script := fmt.Sprintf(`
set -e
need=0
details=""
desired_modules="$(printf '%%s' '%s' | base64 -d)"
desired_sysctl="$(printf '%%s' '%s' | base64 -d)"

current_modules=""
if [ -f /etc/modules-load.d/bootstrapctl-k8s.conf ]; then
  current_modules="$(cat /etc/modules-load.d/bootstrapctl-k8s.conf)"
fi
if [ "$current_modules" != "$desired_modules" ]; then
  need=1
  details="${details}modules-file "
fi

current_sysctl=""
if [ -f /etc/sysctl.d/99-bootstrapctl-k8s.conf ]; then
  current_sysctl="$(cat /etc/sysctl.d/99-bootstrapctl-k8s.conf)"
fi
if [ "$current_sysctl" != "$desired_sysctl" ]; then
  need=1
  details="${details}sysctl-file "
fi

%s

if [ "$need" -eq 0 ]; then
  echo OK
else
  echo "CHANGE:${details}"
fi
`, modulesFileB64, sysctlFileB64, strings.Join(checks, "\n"))

	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return CheckResult{}, err
	}

	output := parseStatusLine(result.Output, "OK", "CHANGE:")
	if output == "OK" {
		return CheckResult{Needed: false, Summary: "Kubernetes 内核网络参数已满足要求"}, nil
	}
	if strings.HasPrefix(output, "CHANGE:") {
		return CheckResult{
			Needed:  true,
			Summary: fmt.Sprintf("检测到内核网络条件漂移: %s", strings.TrimSpace(strings.TrimPrefix(output, "CHANGE:"))),
		}, nil
	}
	return CheckResult{}, fmt.Errorf("无法解析内核网络检查结果: %s", output)
}

func (t *KernelNetworkTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	modulesFileB64 := encodeBase64(renderModulesFile(t.Modules))
	sysctlFileB64 := encodeBase64(renderSysctlFile(t.Sysctls))

	var modprobeCommands []string
	for _, module := range t.Modules {
		modprobeCommands = append(modprobeCommands, fmt.Sprintf("modprobe %s || true", module))
	}

	var sysctlCommands []string
	keys := make([]string, 0, len(t.Sysctls))
	for key := range t.Sysctls {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sysctlCommands = append(sysctlCommands, fmt.Sprintf("sysctl -w %s=%s >/dev/null", key, t.Sysctls[key]))
	}

	script := fmt.Sprintf(`
set -e
modules_file="$(printf '%%s' '%s' | base64 -d)"
sysctl_file="$(printf '%%s' '%s' | base64 -d)"

mkdir -p /etc/modules-load.d /etc/sysctl.d
printf '%%s\n' "$modules_file" > /etc/modules-load.d/bootstrapctl-k8s.conf
printf '%%s\n' "$sysctl_file" > /etc/sysctl.d/99-bootstrapctl-k8s.conf

%s
%s

echo CHANGED
`, modulesFileB64, sysctlFileB64, strings.Join(modprobeCommands, "\n"), strings.Join(sysctlCommands, "\n"))

	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("收敛 Kubernetes 内核网络参数失败: %s", strings.TrimSpace(result.Output))
	}
	return ApplyResult{
		Changed: true,
		Summary: "Kubernetes 内核网络参数已收敛并持久化",
	}, nil
}
