package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

type SwapTask struct {
	NodeSpec config.NodeConnection
}

func (t *SwapTask) Key() string   { return "disable-swap" }
func (t *SwapTask) Title() string { return "关闭 SWAP" }
func (t *SwapTask) Node() string  { return t.NodeSpec.Name }

func (t *SwapTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	script := `
active="$(swapon --noheadings --show=NAME 2>/dev/null | wc -l | tr -d ' ')"
fstab="$(grep -Ec '^[^#].*[[:space:]]swap[[:space:]]' /etc/fstab || true)"
if [ "${active:-0}" -eq 0 ] && [ "${fstab:-0}" -eq 0 ]; then
  echo OK
else
  echo "CHANGE:active=${active:-0},fstab=${fstab:-0}"
fi
`
	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return CheckResult{}, err
	}
	output := parseStatusLine(result.Output, "OK", "CHANGE:")
	if output == "OK" {
		return CheckResult{Needed: false, Summary: "SWAP 已关闭且 /etc/fstab 无活动 swap 条目"}, nil
	}
	if strings.HasPrefix(output, "CHANGE:") {
		return CheckResult{Needed: true, Summary: "检测到活动 SWAP 或 fstab 中仍有 swap 配置"}, nil
	}
	return CheckResult{}, fmt.Errorf("无法解析 swap 检查结果: %s", output)
}

func (t *SwapTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	script := `
set -e
swapoff -a || true
cp /etc/fstab /etc/fstab.bootstrapctl.bak.$(date +%Y%m%d%H%M%S) || true
tmp="$(mktemp)"
awk '
/^[[:space:]]*#/ { print; next }
$0 ~ /[[:space:]]swap[[:space:]]/ { print "# bootstrapctl disabled: " $0; next }
{ print }
' /etc/fstab > "$tmp"
cat "$tmp" > /etc/fstab
rm -f "$tmp"
echo CHANGED
`
	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("关闭 SWAP 失败: %s", strings.TrimSpace(result.Output))
	}
	return ApplyResult{
		Changed: true,
		Summary: "SWAP 已关闭并注释 /etc/fstab 中的 swap 项",
	}, nil
}
