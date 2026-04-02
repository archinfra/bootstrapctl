package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

type HostsFileTask struct {
	NodeSpec     config.NodeConnection
	ClusterNodes []config.NodeConnection
	BlockContent string
}

func (t *HostsFileTask) Key() string   { return "hosts-file" }
func (t *HostsFileTask) Title() string { return "维护 /etc/hosts 受控区块" }
func (t *HostsFileTask) Node() string  { return t.NodeSpec.Name }

func (t *HostsFileTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	blockContent, err := t.desiredBlock(ctx, exec)
	if err != nil {
		return CheckResult{}, err
	}

	expectedB64 := encodeBase64(blockContent)
	script := fmt.Sprintf(`
set -e
expected="$(printf '%%s' '%s' | base64 -d)"
start='# BEGIN BOOTSTRAPCTL HOSTS'
end='# END BOOTSTRAPCTL HOSTS'
current=""
if grep -qF "$start" /etc/hosts && grep -qF "$end" /etc/hosts; then
  current="$(awk -v start="$start" -v end="$end" '
    $0==start { printing=1 }
    printing { print }
    $0==end { exit }
  ' /etc/hosts)"
fi
if [ "$current" = "$expected" ]; then
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
		return CheckResult{Needed: false, Summary: "/etc/hosts 受控区块已正确写入"}, nil
	}
	if output == "CHANGE" {
		return CheckResult{Needed: true, Summary: "/etc/hosts 受控区块需要更新"}, nil
	}
	return CheckResult{}, fmt.Errorf("无法解析 hosts 检查结果: %s", output)
}

func (t *HostsFileTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	blockContent, err := t.desiredBlock(ctx, exec)
	if err != nil {
		return ApplyResult{}, err
	}

	expectedB64 := encodeBase64(blockContent)
	script := fmt.Sprintf(`
set -e
expected="$(printf '%%s' '%s' | base64 -d)"
start='# BEGIN BOOTSTRAPCTL HOSTS'
end='# END BOOTSTRAPCTL HOSTS'
tmp="$(mktemp)"
if grep -qF "$start" /etc/hosts && grep -qF "$end" /etc/hosts; then
  awk -v start="$start" -v end="$end" '
    $0==start { skip=1; next }
    $0==end { skip=0; next }
    !skip { print }
  ' /etc/hosts > "$tmp"
else
  cp /etc/hosts "$tmp"
fi
printf '\n%%s\n' "$expected" >> "$tmp"
cat "$tmp" > /etc/hosts
rm -f "$tmp"
echo CHANGED
`, expectedB64)
	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("更新 /etc/hosts 失败: %s", strings.TrimSpace(result.Output))
	}
	return ApplyResult{
		Changed: true,
		Summary: "/etc/hosts 受控区块已更新",
	}, nil
}

func (t *HostsFileTask) desiredBlock(ctx context.Context, exec remote.Executor) (string, error) {
	if strings.TrimSpace(t.BlockContent) != "" {
		return t.BlockContent, nil
	}

	nodes := t.ClusterNodes
	if len(nodes) == 0 {
		nodes = []config.NodeConnection{t.NodeSpec}
	}

	lines := []string{"# BEGIN BOOTSTRAPCTL HOSTS"}
	for _, node := range nodes {
		hostIP, err := t.resolveNodeHostIP(ctx, exec, node)
		if err != nil {
			return "", fmt.Errorf("解析节点 %s 的 hosts IP 失败: %w", node.Name, err)
		}
		lines = append(lines, fmt.Sprintf("%s %s", hostIP, node.Name))
	}
	lines = append(lines, "# END BOOTSTRAPCTL HOSTS")
	return strings.Join(lines, "\n"), nil
}

func (t *HostsFileTask) resolveNodeHostIP(ctx context.Context, exec remote.Executor, node config.NodeConnection) (string, error) {
	if strings.TrimSpace(node.HostIP) != "" {
		return strings.TrimSpace(node.HostIP), nil
	}

	script := `
set -e
ips="$(hostname -I 2>/dev/null || true)"
for ip in $ips; do
  case "$ip" in
    127.*|::1)
      continue
      ;;
    *)
      printf '%s\n' "$ip"
      exit 0
      ;;
  esac
done
exit 1
`
	result, err := runScript(ctx, exec, node, script)
	if err != nil {
		return "", err
	}
	hostIP := strings.TrimSpace(result.Output)
	if result.ExitCode != 0 || hostIP == "" {
		return "", fmt.Errorf("hostname -I 未返回可用地址")
	}
	return hostIP, nil
}
