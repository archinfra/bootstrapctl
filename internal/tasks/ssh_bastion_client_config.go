package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// SSHBastionClientConfigTask 负责在跳板机上写入 SSH 客户端配置。
// 这样 bastion 端可以直接执行 `ssh 192.168.x.x`，而不需要额外手写 -i 参数。
// 该任务会把目标节点的 IP、host_ip 和节点名收敛成受控 Host 条目。
type SSHBastionClientConfigTask struct {
	TargetNodeSpec        config.NodeConnection
	BastionNodeSpec       config.NodeConnection
	BastionKeyPath        string
	BastionSSHConfigPath  string
}

func (t *SSHBastionClientConfigTask) Key() string   { return "ssh-bastion-client-config" }
func (t *SSHBastionClientConfigTask) Title() string { return "收敛跳板机 SSH 客户端配置" }
func (t *SSHBastionClientConfigTask) Node() string  { return t.TargetNodeSpec.Name }

func (t *SSHBastionClientConfigTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	block := renderBastionSSHClientConfigBlock(t.TargetNodeSpec, t.BastionKeyPath)
	script := renderBastionSSHConfigCheckScript(t.BastionNodeSpec.SSHUser, t.BastionSSHConfigPath, t.TargetNodeSpec.Name, block)
	result, err := runScript(ctx, exec, t.BastionNodeSpec, script)
	if err != nil {
		return CheckResult{}, err
	}

	output := parseStatusLine(result.Output, "OK:", "CHANGE:", "ERROR:user-not-found:", "ERROR:user-home-not-found:")
	switch {
	case strings.HasPrefix(output, "OK:"):
		return CheckResult{
			Needed:  false,
			Summary: fmt.Sprintf("跳板机 SSH 配置已就绪 (%s)", strings.TrimSpace(strings.TrimPrefix(output, "OK:"))),
		}, nil
	case strings.HasPrefix(output, "CHANGE:"):
		return CheckResult{
			Needed:  true,
			Summary: fmt.Sprintf("跳板机 SSH 配置待收敛 (%s)", strings.TrimSpace(strings.TrimPrefix(output, "CHANGE:"))),
		}, nil
	case strings.HasPrefix(output, "ERROR:user-not-found:"):
		return CheckResult{}, fmt.Errorf("跳板机不存在用户 %s", t.BastionNodeSpec.SSHUser)
	case strings.HasPrefix(output, "ERROR:user-home-not-found:"):
		return CheckResult{}, fmt.Errorf("无法解析跳板机用户 %s 的 home 目录", t.BastionNodeSpec.SSHUser)
	default:
		return CheckResult{}, fmt.Errorf("无法解析跳板机 SSH 配置检查结果: %s", output)
	}
}

func (t *SSHBastionClientConfigTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	block := renderBastionSSHClientConfigBlock(t.TargetNodeSpec, t.BastionKeyPath)
	script := renderBastionSSHConfigApplyScript(t.BastionNodeSpec.SSHUser, t.BastionSSHConfigPath, t.TargetNodeSpec.Name, block)
	result, err := runScript(ctx, exec, t.BastionNodeSpec, script)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("写入跳板机 SSH 配置失败: %s", strings.TrimSpace(result.Output))
	}

	output := parseStatusLine(result.Output, "CHANGED:", "ERROR:user-not-found:", "ERROR:user-home-not-found:")
	switch {
	case strings.HasPrefix(output, "CHANGED:"):
		return ApplyResult{
			Changed: true,
			Summary: fmt.Sprintf("跳板机 SSH 配置已收敛 (%s)", strings.TrimSpace(strings.TrimPrefix(output, "CHANGED:"))),
		}, nil
	case strings.HasPrefix(output, "ERROR:user-not-found:"):
		return ApplyResult{}, fmt.Errorf("跳板机不存在用户 %s", t.BastionNodeSpec.SSHUser)
	case strings.HasPrefix(output, "ERROR:user-home-not-found:"):
		return ApplyResult{}, fmt.Errorf("无法解析跳板机用户 %s 的 home 目录", t.BastionNodeSpec.SSHUser)
	default:
		return ApplyResult{}, fmt.Errorf("无法解析跳板机 SSH 配置应用结果: %s", output)
	}
}

func renderBastionSSHClientConfigBlock(node config.NodeConnection, keyPath string) string {
	aliases := make([]string, 0, 3)
	seen := map[string]struct{}{}
	addAlias := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		aliases = append(aliases, trimmed)
	}

	addAlias(node.Name)
	addAlias(node.IP)
	addAlias(node.HostIP)

	identityFile := strings.TrimSpace(keyPath)
	if identityFile == "" {
		identityFile = "~/.ssh/bootstrapctl_ed25519"
	}

	return fmt.Sprintf(`# BEGIN BOOTSTRAPCTL SSH HOST %s
Host %s
  HostName %s
  User %s
  Port %d
  IdentityFile %s
  IdentitiesOnly yes
  PreferredAuthentications publickey
  PasswordAuthentication no
  StrictHostKeyChecking accept-new
# END BOOTSTRAPCTL SSH HOST %s`,
		node.Name,
		strings.Join(aliases, " "),
		node.IP,
		node.SSHUser,
		node.SSHPort,
		identityFile,
		node.Name,
	)
}

func renderBastionSSHConfigCheckScript(user string, configPath string, nodeName string, block string) string {
	return fmt.Sprintf(`
set -e
user="%s"
requested_config_path="%s"
start_marker="# BEGIN BOOTSTRAPCTL SSH HOST %s"
end_marker="# END BOOTSTRAPCTL SSH HOST %s"
managed_block_b64="%s"

if ! id "$user" >/dev/null 2>&1; then
  echo "ERROR:user-not-found:$user"
  exit 0
fi

home_dir="$(getent passwd "$user" | cut -d: -f6)"
if [ -z "$home_dir" ]; then
  echo "ERROR:user-home-not-found:$user"
  exit 0
fi

case "$requested_config_path" in
  ""|"~")
    config_path="$home_dir/.ssh/config"
    ;;
  ~/*)
    suffix="${requested_config_path#~/}"
    config_path="$home_dir/$suffix"
    ;;
  *)
    config_path="$requested_config_path"
    ;;
esac

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

existing_file="$tmp_dir/existing"
stripped_file="$tmp_dir/stripped"
candidate_file="$tmp_dir/candidate"
block_file="$tmp_dir/block"

if [ -f "$config_path" ]; then
  cp "$config_path" "$existing_file"
else
  : > "$existing_file"
fi

printf '%%s' "$managed_block_b64" | base64 -d > "$block_file"

awk -v start="$start_marker" -v end="$end_marker" '
  $0 == start { skip=1; next }
  $0 == end { skip=0; next }
  skip == 0 { print }
' "$existing_file" > "$stripped_file"

cp "$stripped_file" "$candidate_file"
if [ -s "$candidate_file" ]; then
  printf '\n' >> "$candidate_file"
fi
cat "$block_file" >> "$candidate_file"
printf '\n' >> "$candidate_file"

if cmp -s "$existing_file" "$candidate_file"; then
  echo "OK:$config_path"
else
  echo "CHANGE:$config_path"
fi
`, user, configPath, nodeName, nodeName, encodeBase64(block))
}

func renderBastionSSHConfigApplyScript(user string, configPath string, nodeName string, block string) string {
	return fmt.Sprintf(`
set -e
user="%s"
requested_config_path="%s"
start_marker="# BEGIN BOOTSTRAPCTL SSH HOST %s"
end_marker="# END BOOTSTRAPCTL SSH HOST %s"
managed_block_b64="%s"

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
case "$requested_config_path" in
  ""|"~")
    config_path="$home_dir/.ssh/config"
    ;;
  ~/*)
    suffix="${requested_config_path#~/}"
    config_path="$home_dir/$suffix"
    ;;
  *)
    config_path="$requested_config_path"
    ;;
esac

ssh_dir="$(dirname "$config_path")"
install -d -m 700 -o "$user" -g "$group_name" "$ssh_dir"

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

existing_file="$tmp_dir/existing"
stripped_file="$tmp_dir/stripped"
candidate_file="$tmp_dir/candidate"
block_file="$tmp_dir/block"

if [ -f "$config_path" ]; then
  cp "$config_path" "$existing_file"
else
  : > "$existing_file"
fi

printf '%%s' "$managed_block_b64" | base64 -d > "$block_file"

awk -v start="$start_marker" -v end="$end_marker" '
  $0 == start { skip=1; next }
  $0 == end { skip=0; next }
  skip == 0 { print }
' "$existing_file" > "$stripped_file"

cp "$stripped_file" "$candidate_file"
if [ -s "$candidate_file" ]; then
  printf '\n' >> "$candidate_file"
fi
cat "$block_file" >> "$candidate_file"
printf '\n' >> "$candidate_file"

cp "$candidate_file" "$config_path"
chown "$user:$group_name" "$config_path"
chmod 600 "$config_path"

echo "CHANGED:$config_path"
`, user, configPath, nodeName, nodeName, encodeBase64(block))
}
