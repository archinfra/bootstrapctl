package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// checkAuthorizedKey 用统一逻辑检查目标用户的 authorized_keys 中是否已存在指定公钥。
// 这样控制端公钥分发和跳板机公钥分发可以复用同一套判断逻辑，避免两处脚本漂移。
func checkAuthorizedKey(ctx context.Context, exec remote.Executor, node config.NodeConnection, authorizedUser string, publicKey string) (CheckResult, error) {
	script := renderAuthorizedKeyCheckScript(authorizedUser, publicKey)
	result, err := runScript(ctx, exec, node, script)
	if err != nil {
		return CheckResult{}, err
	}

	output := parseStatusLine(result.Output, "OK:", "CHANGE:", "ERROR:user-not-found:", "ERROR:user-home-not-found:")
	switch {
	case strings.HasPrefix(output, "OK:"):
		return CheckResult{
			Needed:  false,
			Summary: fmt.Sprintf("用户 %s 已包含目标公钥", authorizedUser),
		}, nil
	case strings.HasPrefix(output, "CHANGE:"):
		return CheckResult{
			Needed:  true,
			Summary: fmt.Sprintf("用户 %s 需要补充 SSH 公钥", authorizedUser),
		}, nil
	case strings.HasPrefix(output, "ERROR:user-not-found:"):
		return CheckResult{}, fmt.Errorf("目标节点不存在用户 %s", authorizedUser)
	case strings.HasPrefix(output, "ERROR:user-home-not-found:"):
		return CheckResult{}, fmt.Errorf("无法解析用户 %s 的 home 目录", authorizedUser)
	default:
		return CheckResult{}, fmt.Errorf("无法解析 SSH 公钥检查结果: %s", output)
	}
}

// applyAuthorizedKey 负责把指定公钥写入目标用户的 authorized_keys。
func applyAuthorizedKey(ctx context.Context, exec remote.Executor, node config.NodeConnection, authorizedUser string, publicKey string) (ApplyResult, error) {
	script := renderAuthorizedKeyApplyScript(authorizedUser, publicKey)
	result, err := runScript(ctx, exec, node, script)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("分发 SSH 公钥失败: %s", strings.TrimSpace(result.Output))
	}

	return ApplyResult{
		Changed: true,
		Summary: fmt.Sprintf("已向用户 %s 分发 SSH 公钥", authorizedUser),
	}, nil
}

func renderAuthorizedKeyCheckScript(authorizedUser string, publicKey string) string {
	return fmt.Sprintf(`
set -e
user="%s"
public_key="$(printf '%%s' '%s' | base64 -d)"

if ! id "$user" >/dev/null 2>&1; then
  echo "ERROR:user-not-found:$user"
  exit 0
fi

home_dir="$(getent passwd "$user" | cut -d: -f6)"
if [ -z "$home_dir" ]; then
  echo "ERROR:user-home-not-found:$user"
  exit 0
fi

authorized_keys="$home_dir/.ssh/authorized_keys"
if [ -f "$authorized_keys" ] && grep -Fqx "$public_key" "$authorized_keys"; then
  echo "OK:$authorized_keys"
else
  echo "CHANGE:$authorized_keys"
fi
`, authorizedUser, encodeBase64(publicKey))
}

func renderAuthorizedKeyApplyScript(authorizedUser string, publicKey string) string {
	return fmt.Sprintf(`
set -e
user="%s"
public_key="$(printf '%%s' '%s' | base64 -d)"

if ! id "$user" >/dev/null 2>&1; then
  echo "ERROR:user-not-found:$user"
  exit 1
fi

home_dir="$(getent passwd "$user" | cut -d: -f6)"
if [ -z "$home_dir" ]; then
  echo "ERROR:user-home-not-found:$user"
  exit 1
fi

group_name="$(id -gn "$user")"
ssh_dir="$home_dir/.ssh"
authorized_keys="$ssh_dir/authorized_keys"

install -d -m 700 -o "$user" -g "$group_name" "$ssh_dir"
touch "$authorized_keys"
chown "$user:$group_name" "$authorized_keys"
chmod 600 "$authorized_keys"

if ! grep -Fqx "$public_key" "$authorized_keys"; then
  printf '%%s\n' "$public_key" >> "$authorized_keys"
fi

echo "CHANGED:$authorized_keys"
`, authorizedUser, encodeBase64(publicKey))
}
