package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// RootSSHLoginPolicyTask 负责收敛远端主机的 root SSH 登录策略。
// 当前主要用于在受控运维账号就绪后关闭直接 root 登录。
type RootSSHLoginPolicyTask struct {
	NodeSpec        config.NodeConnection
	SSHDConfigPath  string
	PermitRootLogin string
}

func (t *RootSSHLoginPolicyTask) Key() string   { return "root-ssh-login-policy" }
func (t *RootSSHLoginPolicyTask) Title() string { return "收敛 root SSH 登录策略" }
func (t *RootSSHLoginPolicyTask) Node() string  { return t.NodeSpec.Name }

func (t *RootSSHLoginPolicyTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	result, err := runScript(ctx, exec, t.NodeSpec, t.renderCheckScript())
	if err != nil {
		return CheckResult{}, err
	}

	output := parseStatusLine(result.Output, "OK:", "CHANGE:")
	switch {
	case strings.HasPrefix(output, "OK:"):
		return CheckResult{
			Needed:  false,
			Summary: fmt.Sprintf("root SSH 登录策略已收敛为 PermitRootLogin=%s", t.PermitRootLogin),
		}, nil
	case strings.HasPrefix(output, "CHANGE:"):
		current := strings.TrimSpace(strings.TrimPrefix(output, "CHANGE:"))
		if current == "" {
			current = "unknown"
		}
		return CheckResult{
			Needed:  true,
			Summary: fmt.Sprintf("当前 root SSH 登录策略=%s，目标=%s", current, t.PermitRootLogin),
		}, nil
	default:
		return CheckResult{}, fmt.Errorf("无法解析 root SSH 登录策略检查结果: %s", output)
	}
}

func (t *RootSSHLoginPolicyTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	result, err := runScript(ctx, exec, t.NodeSpec, t.renderApplyScript())
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("收敛 root SSH 登录策略失败: %s", strings.TrimSpace(result.Output))
	}
	return ApplyResult{
		Changed: true,
		Summary: fmt.Sprintf("已设置 PermitRootLogin=%s", t.PermitRootLogin),
	}, nil
}

func (t *RootSSHLoginPolicyTask) renderCheckScript() string {
	return fmt.Sprintf(`
set -e
main_conf="%s"
target="%s"
current=""

if command -v sshd >/dev/null 2>&1; then
  current="$(sshd -T -f "$main_conf" 2>/dev/null | awk '/^permitrootlogin / {print $2; exit}')"
fi

if [ -z "$current" ]; then
  current="$(awk '
    /^[[:space:]]*# BEGIN BOOTSTRAPCTL ROOT SSH POLICY[[:space:]]*$/ {managed=1; next}
    /^[[:space:]]*# END BOOTSTRAPCTL ROOT SSH POLICY[[:space:]]*$/ {managed=0; next}
    managed && /^[[:space:]]*PermitRootLogin[[:space:]]+/ {print $2; found=1; exit}
    END {if (!found) print ""}
  ' "$main_conf")"
fi

if [ "$current" = "$target" ]; then
  echo "OK:$current"
else
  echo "CHANGE:$current"
fi
`, t.SSHDConfigPath, t.PermitRootLogin)
}

func (t *RootSSHLoginPolicyTask) renderApplyScript() string {
	return fmt.Sprintf(`
set -e
main_conf="%s"
target="%s"
begin_marker="# BEGIN BOOTSTRAPCTL ROOT SSH POLICY"
end_marker="# END BOOTSTRAPCTL ROOT SSH POLICY"

tmp_file="$(mktemp)"
awk -v begin_marker="$begin_marker" -v end_marker="$end_marker" -v target="$target" '
  function print_managed() {
    if (!inserted) {
      print begin_marker
      print "PermitRootLogin " target
      print end_marker
      inserted=1
    }
  }
  $0 == begin_marker {skip=1; next}
  $0 == end_marker {skip=0; next}
  skip {next}
  /^[[:space:]]*Include[[:space:]]+/ {
    print
    print_managed()
    next
  }
  /^[[:space:]]*Match[[:space:]]+/ {
    print_managed()
    print
    in_match=1
    next
  }
  !in_match && /^[[:space:]]*PermitRootLogin[[:space:]]+/ {next}
  {print}
  END {print_managed()}
' "$main_conf" > "$tmp_file"

cat "$tmp_file" > "$main_conf"
rm -f "$tmp_file"

if command -v sshd >/dev/null 2>&1; then
  sshd -t -f "$main_conf"
fi

if systemctl list-unit-files 2>/dev/null | grep -q '^sshd\\.service'; then
  systemctl restart sshd
elif systemctl list-unit-files 2>/dev/null | grep -q '^ssh\\.service'; then
  systemctl restart ssh
fi

echo "CHANGED"
`, t.SSHDConfigPath, t.PermitRootLogin)
}
