package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// SSHControllerClientConfigTask 负责在当前执行节点本机维护 ~/.ssh/config。
// 这样即使控制端使用的是 bootstrapctl 专用 key，
// 用户也可以直接执行 `ssh node-01` / `ssh 192.168.x.x`，
// 而不用每次手工追加 `-i ~/.ssh/bootstrapctl_ed25519`。
type SSHControllerClientConfigTask struct {
	TargetNodeSpec          config.NodeConnection
	ControllerKeyPath       string
	ControllerSSHConfigPath string
}

func (t *SSHControllerClientConfigTask) Key() string { return "ssh-controller-client-config" }
func (t *SSHControllerClientConfigTask) Title() string {
	return "维护当前执行节点 SSH 客户端配置"
}
func (t *SSHControllerClientConfigTask) Node() string { return t.TargetNodeSpec.Name }

func (t *SSHControllerClientConfigTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	block := renderControllerSSHClientConfigBlock(t.TargetNodeSpec, t.ControllerKeyPath)
	configPath := expandControllerSSHPath(t.ControllerSSHConfigPath)
	current, err := renderManagedSSHConfig(configPath, t.TargetNodeSpec.Name, block)
	if err != nil {
		return CheckResult{}, err
	}
	if current.changed {
		return CheckResult{
			Needed:  true,
			Summary: fmt.Sprintf("当前执行节点 SSH 配置待收敛(%s)", configPath),
		}, nil
	}
	return CheckResult{
		Needed:  false,
		Summary: fmt.Sprintf("当前执行节点 SSH 配置已就绪(%s)", configPath),
	}, nil
}

func (t *SSHControllerClientConfigTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	block := renderControllerSSHClientConfigBlock(t.TargetNodeSpec, t.ControllerKeyPath)
	configPath := expandControllerSSHPath(t.ControllerSSHConfigPath)
	current, err := renderManagedSSHConfig(configPath, t.TargetNodeSpec.Name, block)
	if err != nil {
		return ApplyResult{}, err
	}
	if !current.changed {
		return ApplyResult{
			Changed: false,
			Summary: fmt.Sprintf("当前执行节点 SSH 配置已就绪(%s)", configPath),
		}, nil
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return ApplyResult{}, fmt.Errorf("创建控制端 SSH 配置目录失败: %w", err)
	}
	if err := os.WriteFile(configPath, []byte(current.candidate), 0o600); err != nil {
		return ApplyResult{}, fmt.Errorf("写入控制端 SSH 配置失败: %w", err)
	}

	return ApplyResult{
		Changed: true,
		Summary: fmt.Sprintf("当前执行节点 SSH 配置已收敛(%s)", configPath),
	}, nil
}

type managedSSHConfigState struct {
	changed   bool
	candidate string
}

func renderManagedSSHConfig(configPath string, nodeName string, block string) (managedSSHConfigState, error) {
	startMarker := fmt.Sprintf("# BEGIN BOOTSTRAPCTL SSH HOST %s", nodeName)
	endMarker := fmt.Sprintf("# END BOOTSTRAPCTL SSH HOST %s", nodeName)

	existingBytes, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return managedSSHConfigState{}, fmt.Errorf("读取控制端 SSH 配置失败: %w", err)
	}
	existing := string(existingBytes)
	stripped := stripManagedSSHBlock(existing, startMarker, endMarker)

	candidate := strings.TrimRight(stripped, "\n")
	if candidate != "" {
		candidate += "\n\n"
	}
	candidate += block + "\n"

	return managedSSHConfigState{
		changed:   normalizeSSHConfig(existing) != normalizeSSHConfig(candidate),
		candidate: candidate,
	}, nil
}

func stripManagedSSHBlock(content string, startMarker string, endMarker string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		switch line {
		case startMarker:
			skip = true
			continue
		case endMarker:
			skip = false
			continue
		}
		if !skip {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func normalizeSSHConfig(content string) string {
	return strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
}

func expandControllerSSHPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "~" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return path
		}
		return filepath.Join(home, ".ssh", "config")
	}
	if strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))
	}
	return trimmed
}

func renderControllerSSHClientConfigBlock(node config.NodeConnection, keyPath string) string {
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
