package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

type HostnameTask struct {
	NodeSpec        config.NodeConnection
	DesiredHostname string
}

func (t *HostnameTask) Key() string   { return "hostname" }
func (t *HostnameTask) Title() string { return "设置主机名" }
func (t *HostnameTask) Node() string  { return t.NodeSpec.Name }

func (t *HostnameTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	script := fmt.Sprintf(`
current="$(hostnamectl --static status 2>/dev/null || hostname)"
if [ "$current" = "%s" ]; then
  echo "OK:$current"
else
  echo "CHANGE:$current"
fi
`, t.DesiredHostname)
	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return CheckResult{}, err
	}
	output := parseStatusLine(result.Output, "OK:", "CHANGE:")
	if strings.HasPrefix(output, "OK:") {
		return CheckResult{Needed: false, Summary: fmt.Sprintf("主机名已是 %s", t.DesiredHostname)}, nil
	}
	if strings.HasPrefix(output, "CHANGE:") {
		current := strings.TrimPrefix(output, "CHANGE:")
		return CheckResult{Needed: true, Summary: fmt.Sprintf("当前主机名为 %s，目标为 %s", current, t.DesiredHostname)}, nil
	}
	return CheckResult{}, fmt.Errorf("无法解析主机名检查结果: %s", output)
}

func (t *HostnameTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	script := fmt.Sprintf(`
set -e
if command -v hostnamectl >/dev/null 2>&1; then
  hostnamectl set-hostname "%s"
else
  hostname "%s"
fi
echo "CHANGED:%s"
`, t.DesiredHostname, t.DesiredHostname, t.DesiredHostname)
	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("设置主机名失败: %s", strings.TrimSpace(result.Output))
	}
	return ApplyResult{
		Changed: true,
		Summary: fmt.Sprintf("主机名已更新为 %s", t.DesiredHostname),
	}, nil
}
