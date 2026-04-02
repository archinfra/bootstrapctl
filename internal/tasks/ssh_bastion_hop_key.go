package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// SSHBastionHopKeyTask 负责补齐“跳板机 -> 目标节点”的免密链路。
// 它会先在跳板机上确保存在一把专用 SSH key，然后把该公钥写入目标节点的 authorized_keys。
// 这样即使后续 bastion 场景需要在跳板机上继续 ssh 到内网节点，也能走无密码方式。
type SSHBastionHopKeyTask struct {
	TargetNodeSpec  config.NodeConnection
	BastionNodeSpec config.NodeConnection
	AuthorizedUser  string
	BastionKeyPath  string
}

func (t *SSHBastionHopKeyTask) Key() string   { return "ssh-bastion-hop-key" }
func (t *SSHBastionHopKeyTask) Title() string { return "配置跳板机到目标节点免密" }
func (t *SSHBastionHopKeyTask) Node() string  { return t.TargetNodeSpec.Name }

func (t *SSHBastionHopKeyTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	publicKey, keyChanged, keyPath, err := t.ensureBastionPublicKey(ctx, exec, false)
	if err != nil {
		return CheckResult{}, err
	}
	if keyChanged && strings.TrimSpace(publicKey) == "" {
		return CheckResult{
			Needed:  true,
			Summary: fmt.Sprintf("跳板机尚未生成专用 SSH 密钥 (bastion_key=%s)", keyPath),
		}, nil
	}

	keyCheck, err := checkAuthorizedKey(ctx, exec, t.TargetNodeSpec, t.AuthorizedUser, publicKey)
	if err != nil {
		return CheckResult{}, err
	}

	if keyChanged || keyCheck.Needed {
		return CheckResult{
			Needed:  true,
			Summary: fmt.Sprintf("跳板机密钥链路待收敛 (bastion_key=%s, target_user=%s)", keyPath, t.AuthorizedUser),
		}, nil
	}

	return CheckResult{
		Needed:  false,
		Summary: fmt.Sprintf("跳板机到目标节点免密已就绪 (bastion_key=%s)", keyPath),
	}, nil
}

func (t *SSHBastionHopKeyTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	publicKey, _, keyPath, err := t.ensureBastionPublicKey(ctx, exec, true)
	if err != nil {
		return ApplyResult{}, err
	}

	targetApply, err := applyAuthorizedKey(ctx, exec, t.TargetNodeSpec, t.AuthorizedUser, publicKey)
	if err != nil {
		return ApplyResult{}, err
	}

	return ApplyResult{
		Changed: true,
		Summary: fmt.Sprintf("%s；跳板机密钥=%s", targetApply.Summary, keyPath),
	}, nil
}

func (t *SSHBastionHopKeyTask) ensureBastionPublicKey(ctx context.Context, exec remote.Executor, apply bool) (publicKey string, changed bool, resolvedPath string, err error) {
	script := renderBastionKeyCheckScript(t.BastionNodeSpec.SSHUser, t.BastionKeyPath)
	if apply {
		script = renderBastionKeyApplyScript(t.BastionNodeSpec.SSHUser, t.BastionKeyPath, t.TargetNodeSpec.Name)
	}

	result, err := runScript(ctx, exec, t.BastionNodeSpec, script)
	if err != nil {
		return "", false, "", err
	}
	if apply && result.ExitCode != 0 {
		return "", false, "", fmt.Errorf("在跳板机准备 SSH 密钥失败: %s", strings.TrimSpace(result.Output))
	}

	output := parseStatusLine(result.Output, "OK:", "CHANGE:", "ERROR:user-not-found:", "ERROR:user-home-not-found:")
	switch {
	case strings.HasPrefix(output, "OK:"):
		path, key, parseErr := parseBastionKeyOutput(strings.TrimPrefix(output, "OK:"))
		return key, false, path, parseErr
	case strings.HasPrefix(output, "CHANGE:"):
		path, key, parseErr := parseBastionKeyOutput(strings.TrimPrefix(output, "CHANGE:"))
		return key, true, path, parseErr
	case strings.HasPrefix(output, "ERROR:user-not-found:"):
		return "", false, "", fmt.Errorf("跳板机不存在用户 %s", t.BastionNodeSpec.SSHUser)
	case strings.HasPrefix(output, "ERROR:user-home-not-found:"):
		return "", false, "", fmt.Errorf("无法解析跳板机用户 %s 的 home 目录", t.BastionNodeSpec.SSHUser)
	default:
		return "", false, "", fmt.Errorf("无法解析跳板机密钥检查结果: %s", output)
	}
}

func parseBastionKeyOutput(value string) (resolvedPath string, publicKey string, err error) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("跳板机密钥输出格式错误: %s", value)
	}
	resolvedPath = strings.TrimSpace(parts[0])
	decoded, decodeErr := decodeBase64(parts[1])
	if decodeErr != nil {
		return "", "", fmt.Errorf("解析跳板机公钥失败: %w", decodeErr)
	}
	return resolvedPath, strings.TrimSpace(decoded), nil
}

func renderBastionKeyCheckScript(user string, keyPath string) string {
	return fmt.Sprintf(`
set -e
user="%s"
requested_key_path="%s"

if ! id "$user" >/dev/null 2>&1; then
  echo "ERROR:user-not-found:$user"
  exit 0
fi

home_dir="$(getent passwd "$user" | cut -d: -f6)"
if [ -z "$home_dir" ]; then
  echo "ERROR:user-home-not-found:$user"
  exit 0
fi

case "$requested_key_path" in
  ""|"~")
    suffix=".ssh/bootstrapctl_ed25519"
    key_path="$home_dir/$suffix"
    ;;
  ~/*)
    suffix="${requested_key_path#~/}"
    key_path="$home_dir/$suffix"
    ;;
  *)
    key_path="$requested_key_path"
    ;;
esac

pub_path="$key_path.pub"
if [ -s "$key_path" ] && [ -s "$pub_path" ]; then
  printf 'OK:%%s:' "$key_path"
  base64 < "$pub_path" | tr -d '\n'
  printf '\n'
else
  printf 'CHANGE:%%s:' "$key_path"
  if [ -s "$pub_path" ]; then
    base64 < "$pub_path" | tr -d '\n'
  else
    printf '%%s' '%s' | base64 | tr -d '\n'
  fi
  printf '\n'
fi
`, user, keyPath, "")
}

func renderBastionKeyApplyScript(user string, keyPath string, targetNodeName string) string {
	return fmt.Sprintf(`
set -e
user="%s"
requested_key_path="%s"
comment="bootstrapctl-bastion-hop@%s"

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
case "$requested_key_path" in
  ""|"~")
    suffix=".ssh/bootstrapctl_ed25519"
    key_path="$home_dir/$suffix"
    ;;
  ~/*)
    suffix="${requested_key_path#~/}"
    key_path="$home_dir/$suffix"
    ;;
  *)
    key_path="$requested_key_path"
    ;;
esac

ssh_dir="$(dirname "$key_path")"
pub_path="$key_path.pub"

install -d -m 700 -o "$user" -g "$group_name" "$ssh_dir"

if [ ! -s "$key_path" ] || [ ! -s "$pub_path" ]; then
  rm -f "$key_path" "$pub_path"
  if [ "$user" = "$(id -un)" ]; then
    ssh-keygen -q -t ed25519 -N '' -f "$key_path" -C "$comment"
  else
    su -s /bin/sh -c "ssh-keygen -q -t ed25519 -N '' -f '$key_path' -C '$comment'" "$user"
  fi
fi

chown "$user:$group_name" "$key_path" "$pub_path"
chmod 700 "$ssh_dir"
chmod 600 "$key_path"
chmod 644 "$pub_path"

printf 'CHANGE:%%s:' "$key_path"
base64 < "$pub_path" | tr -d '\n'
printf '\n'
`, user, keyPath, targetNodeName)
}
