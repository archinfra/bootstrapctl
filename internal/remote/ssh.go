package remote

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"golang.org/x/crypto/ssh"
)

type SSHExecutor struct {
	timeout time.Duration
}

type sshSessionClient interface {
	NewSession() (*ssh.Session, error)
	Close() error
}

func NewSSHExecutor(timeout time.Duration) *SSHExecutor {
	return &SSHExecutor{timeout: timeout}
}

// Run 通过 SSH 在目标节点执行一段 Bash 脚本。
// 默认会优先尝试标准 SSH 连接方式；如果配置了 bastion，则先尝试
// direct-tcpip 端口转发，再在必要时自动回退到“先登录跳板机，再在跳板机内执行二跳 ssh”。
func (e *SSHExecutor) Run(ctx context.Context, node config.NodeConnection, script string) (Result, error) {
	if node.Bastion != nil && strings.TrimSpace(node.Bastion.Host) != "" {
		return e.runViaBastion(ctx, node, script)
	}
	return e.runDirect(ctx, node, script)
}

func (e *SSHExecutor) runDirect(ctx context.Context, node config.NodeConnection, script string) (Result, error) {
	client, err := e.connectDirect(ctx, node)
	if err != nil {
		return Result{}, err
	}
	defer client.Close()
	return e.runOnClient(client, node, script)
}

func (e *SSHExecutor) runViaBastion(ctx context.Context, node config.NodeConnection, script string) (Result, error) {
	bastionClient, err := e.connectBastion(ctx, *node.Bastion)
	if err != nil {
		return Result{}, err
	}
	defer bastionClient.Close()

	targetAddr := net.JoinHostPort(node.IP, strconv.Itoa(node.SSHPort))
	channelConn, err := bastionClient.Dial("tcp", targetAddr)
	if err != nil {
		if shouldFallbackToShellHop(err) {
			return e.runViaBastionShell(bastionClient, node, script)
		}
		return Result{}, fmt.Errorf("通过跳板机连接目标节点 %s 失败: %w", targetAddr, err)
	}

	nodeConfig, err := e.buildNodeConfig(node)
	if err != nil {
		channelConn.Close()
		return Result{}, err
	}

	targetClientConn, chans, reqs, err := ssh.NewClientConn(channelConn, targetAddr, nodeConfig)
	if err != nil {
		channelConn.Close()
		return Result{}, fmt.Errorf("建立目标节点 SSH 会话失败: %w", err)
	}

	client := ssh.NewClient(targetClientConn, chans, reqs)
	defer client.Close()
	return e.runOnClient(client, node, script)
}

func (e *SSHExecutor) runViaBastionShell(bastionClient *ssh.Client, node config.NodeConnection, script string) (Result, error) {
	wrapperScript, err := e.buildBastionShellScript(node, script)
	if err != nil {
		return Result{}, err
	}

	session, err := bastionClient.NewSession()
	if err != nil {
		return Result{}, fmt.Errorf("创建跳板机 SSH session 失败: %w", err)
	}
	defer session.Close()

	session.Stdin = strings.NewReader(wrapperScript)
	output, err := session.CombinedOutput("bash -se")
	if err == nil {
		return Result{Output: string(output), ExitCode: 0}, nil
	}
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return Result{Output: string(output), ExitCode: exitErr.ExitStatus()}, nil
	}
	return Result{}, fmt.Errorf("通过跳板机执行二跳 SSH 脚本失败: %w", err)
}

func (e *SSHExecutor) runOnClient(client sshSessionClient, node config.NodeConnection, script string) (Result, error) {
	session, err := client.NewSession()
	if err != nil {
		return Result{}, fmt.Errorf("创建 SSH session 失败: %w", err)
	}
	defer session.Close()

	session.Stdin = strings.NewReader(script)
	output, err := session.CombinedOutput(buildRemoteCommand(node))
	if err == nil {
		return Result{Output: string(output), ExitCode: 0}, nil
	}

	if exitErr, ok := err.(*ssh.ExitError); ok {
		return Result{Output: string(output), ExitCode: exitErr.ExitStatus()}, nil
	}
	return Result{}, fmt.Errorf("远程命令执行失败: %w", err)
}

func buildRemoteCommand(node config.NodeConnection) string {
	if node.UseSudo && node.SSHUser != "" && node.SSHUser != "root" {
		// 使用 -n 避免因为 sudo 口令提示导致执行链路卡死。
		return "sudo -n bash -se"
	}
	return "bash -se"
}

func shouldFallbackToShellHop(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "administratively prohibited") ||
		strings.Contains(message, "open failed")
}

func (e *SSHExecutor) buildBastionShellScript(node config.NodeConnection, script string) (string, error) {
	remoteCommand := buildRemoteCommand(node)
	targetScriptB64 := base64.StdEncoding.EncodeToString([]byte(script))

	authMode := ""
	passwordB64 := ""
	privateKeyB64 := ""

	if strings.TrimSpace(node.SSHPrivateKey) != "" {
		privateKeyContent, err := os.ReadFile(node.SSHPrivateKey)
		if err != nil {
			return "", fmt.Errorf("读取目标节点 SSH 私钥失败: %w", err)
		}
		authMode = "key"
		privateKeyB64 = base64.StdEncoding.EncodeToString(privateKeyContent)
	} else if strings.TrimSpace(node.SSHPassword) != "" {
		authMode = "password"
		passwordB64 = base64.StdEncoding.EncodeToString([]byte(node.SSHPassword))
	} else {
		return "", fmt.Errorf("跳板机二跳模式至少需要目标节点密码或私钥")
	}

	return fmt.Sprintf(`
set -e
target_user="%s"
target_host="%s"
target_port="%d"
auth_mode="%s"
remote_command="%s"
target_script_b64="%s"
password_b64="%s"
private_key_b64="%s"

if ! command -v ssh >/dev/null 2>&1; then
  echo "ERROR:ssh-client-missing"
  exit 127
fi

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

script_file="$tmp_dir/target-script.sh"
printf '%%s' "$target_script_b64" | base64 -d > "$script_file"
chmod 600 "$script_file"

if [ "$auth_mode" = "key" ]; then
  key_file="$tmp_dir/target-key"
  printf '%%s' "$private_key_b64" | base64 -d > "$key_file"
  chmod 600 "$key_file"
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o PreferredAuthentications=publickey -o PasswordAuthentication=no -i "$key_file" -p "$target_port" "$target_user@$target_host" "$remote_command" < "$script_file"
elif [ "$auth_mode" = "password" ]; then
  pass_file="$tmp_dir/password"
  askpass_file="$tmp_dir/askpass.sh"
  printf '%%s' "$password_b64" | base64 -d > "$pass_file"
  cat > "$askpass_file" <<'EOF_ASKPASS'
#!/bin/sh
cat "$BOOTSTRAPCTL_PASS_FILE"
EOF_ASKPASS
  chmod 700 "$askpass_file"
  export BOOTSTRAPCTL_PASS_FILE="$pass_file"
  export SSH_ASKPASS="$askpass_file"
  export SSH_ASKPASS_REQUIRE=force
  export DISPLAY=bootstrapctl:0
  setsid ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o PreferredAuthentications=password,keyboard-interactive -o PubkeyAuthentication=no -o NumberOfPasswordPrompts=1 -p "$target_port" "$target_user@$target_host" "$remote_command" < "$script_file"
else
  echo "ERROR:unsupported-auth-mode"
  exit 1
fi
`, node.SSHUser, node.IP, node.SSHPort, authMode, remoteCommand, targetScriptB64, passwordB64, privateKeyB64), nil
}

func (e *SSHExecutor) connectDirect(ctx context.Context, node config.NodeConnection) (sshSessionClient, error) {
	sshConfig, err := e.buildNodeConfig(node)
	if err != nil {
		return nil, err
	}

	addr := net.JoinHostPort(node.IP, strconv.Itoa(node.SSHPort))
	dialer := net.Dialer{Timeout: e.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", addr, err)
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("建立 SSH 会话失败: %w", err)
	}
	return ssh.NewClient(clientConn, chans, reqs), nil
}

func (e *SSHExecutor) connectBastion(ctx context.Context, bastion config.Bastion) (*ssh.Client, error) {
	bastionConfig, err := e.buildBastionConfig(bastion)
	if err != nil {
		return nil, err
	}

	bastionAddr := net.JoinHostPort(bastion.Host, strconv.Itoa(bastion.SSHPort))
	dialer := net.Dialer{Timeout: e.timeout}
	bastionConn, err := dialer.DialContext(ctx, "tcp", bastionAddr)
	if err != nil {
		return nil, fmt.Errorf("连接跳板机 %s 失败: %w", bastionAddr, err)
	}

	bastionClientConn, bastionChans, bastionReqs, err := ssh.NewClientConn(bastionConn, bastionAddr, bastionConfig)
	if err != nil {
		bastionConn.Close()
		return nil, fmt.Errorf("建立跳板机 SSH 会话失败: %w", err)
	}
	return ssh.NewClient(bastionClientConn, bastionChans, bastionReqs), nil
}

func (e *SSHExecutor) buildNodeConfig(node config.NodeConnection) (*ssh.ClientConfig, error) {
	auth, err := buildAuthMethods(node.SSHPassword, node.SSHPrivateKey)
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{
		User:            node.SSHUser,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         e.timeout,
	}, nil
}

func (e *SSHExecutor) buildBastionConfig(bastion config.Bastion) (*ssh.ClientConfig, error) {
	auth, err := buildAuthMethods(bastion.SSHPassword, bastion.SSHPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("跳板机认证配置错误: %w", err)
	}
	user := bastion.SSHUser
	if user == "" {
		user = "root"
	}
	return &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         e.timeout,
	}, nil
}

func buildAuthMethods(password, privateKey string) ([]ssh.AuthMethod, error) {
	auth := make([]ssh.AuthMethod, 0, 2)
	if password != "" {
		auth = append(auth, ssh.Password(password))
	}
	if privateKey != "" {
		signer, err := readSigner(privateKey)
		if err != nil {
			return nil, err
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("未配置 SSH 密码或私钥")
	}
	return auth, nil
}

// readSigner 从 PEM 私钥文件构造 SSH signer。
func readSigner(path string) (ssh.Signer, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 SSH 私钥失败: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(content)
	if err != nil {
		return nil, fmt.Errorf("解析 SSH 私钥失败: %w", err)
	}
	return signer, nil
}
