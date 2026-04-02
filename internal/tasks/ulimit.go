package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

type UlimitTask struct {
	NodeSpec config.NodeConnection
	NoFile   int
	NProc    int
}

func (t *UlimitTask) Key() string   { return "ulimit" }
func (t *UlimitTask) Title() string { return "写入 ulimit 配置" }
func (t *UlimitTask) Node() string  { return t.NodeSpec.Name }

func (t *UlimitTask) desiredContent() string {
	return fmt.Sprintf(`* soft nofile %d
* hard nofile %d
* soft nproc %d
* hard nproc %d
root soft nofile %d
root hard nofile %d
root soft nproc %d
root hard nproc %d`, t.NoFile, t.NoFile, t.NProc, t.NProc, t.NoFile, t.NoFile, t.NProc, t.NProc)
}

func (t *UlimitTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	expectedB64 := encodeBase64(t.desiredContent())
	script := fmt.Sprintf(`
set -e
target="/etc/security/limits.d/99-bootstrapctl.conf"
expected="$(printf '%%s' '%s' | base64 -d)"
if [ -f "$target" ] && [ "$(cat "$target")" = "$expected" ]; then
  echo OK
else
  echo CHANGE
fi
`, expectedB64)
	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return CheckResult{}, err
	}
	output := parseStatusLine(result.Output, "OK", "CHANGE")
	if output == "OK" {
		return CheckResult{Needed: false, Summary: "ulimit 配置已满足要求"}, nil
	}
	if output == "CHANGE" {
		return CheckResult{Needed: true, Summary: "ulimit 配置需要更新"}, nil
	}
	return CheckResult{}, fmt.Errorf("无法解析 ulimit 检查结果: %s", output)
}

func (t *UlimitTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	expectedB64 := encodeBase64(t.desiredContent())
	script := fmt.Sprintf(`
set -e
target="/etc/security/limits.d/99-bootstrapctl.conf"
mkdir -p /etc/security/limits.d
printf '%%s' '%s' | base64 -d > "$target"
chmod 644 "$target"
echo CHANGED
`, expectedB64)
	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("写入 ulimit 配置失败: %s", strings.TrimSpace(result.Output))
	}
	return ApplyResult{
		Changed: true,
		Summary: "ulimit 配置已写入 /etc/security/limits.d/99-bootstrapctl.conf",
	}, nil
}
