package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// SELinuxTask 负责在支持 SELinux 的系统上将其关闭到适合 Kubernetes 的状态。
// 对 Ubuntu 这类没有 SELinux 的系统，它会自然退化成 no-op。
type SELinuxTask struct {
	NodeSpec config.NodeConnection
}

func (t *SELinuxTask) Key() string   { return "disable-selinux" }
func (t *SELinuxTask) Title() string { return "关闭 SELinux" }
func (t *SELinuxTask) Node() string  { return t.NodeSpec.Name }

func (t *SELinuxTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	script := `
runtime="Disabled"
config_state="missing"

if command -v getenforce >/dev/null 2>&1; then
  runtime="$(getenforce 2>/dev/null || echo Disabled)"
fi

if [ -f /etc/selinux/config ]; then
  config_state="$(awk -F= '/^SELINUX=/{print $2; found=1} END{if(!found) print "missing"}' /etc/selinux/config)"
fi

if [ "$runtime" = "Disabled" ] && { [ "$config_state" = "disabled" ] || [ "$config_state" = "missing" ]; }; then
  echo OK
else
  echo "CHANGE:runtime=$runtime,config=$config_state"
fi
`
	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return CheckResult{}, err
	}
	output := parseStatusLine(result.Output, "OK", "CHANGE:")
	if output == "OK" {
		return CheckResult{Needed: false, Summary: "SELinux 已关闭或当前系统未启用 SELinux"}, nil
	}
	if strings.HasPrefix(output, "CHANGE:") {
		return CheckResult{Needed: true, Summary: "SELinux 仍处于启用态，需要关闭"}, nil
	}
	return CheckResult{}, fmt.Errorf("无法解析 SELinux 检查结果: %s", output)
}

func (t *SELinuxTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	script := `
set -e
if command -v setenforce >/dev/null 2>&1; then
  setenforce 0 || true
fi

if [ -f /etc/selinux/config ]; then
  cp /etc/selinux/config /etc/selinux/config.bootstrapctl.bak.$(date +%Y%m%d%H%M%S) || true
  if grep -q '^SELINUX=' /etc/selinux/config; then
    sed -i 's/^SELINUX=.*/SELINUX=disabled/' /etc/selinux/config
  else
    printf '\nSELINUX=disabled\n' >> /etc/selinux/config
  fi
fi

echo CHANGED
`
	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("关闭 SELinux 失败: %s", strings.TrimSpace(result.Output))
	}
	return ApplyResult{
		Changed: true,
		Summary: "SELinux 已切换到关闭状态",
	}, nil
}
