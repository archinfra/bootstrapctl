package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuanyp8/bootstrapctl/internal/config"
)

func TestShouldFallbackToShellHop(t *testing.T) {
	if !shouldFallbackToShellHop(assertError("ssh: rejected: administratively prohibited (open failed)")) {
		t.Fatalf("expected administratively prohibited error to trigger shell hop fallback")
	}
	if shouldFallbackToShellHop(assertError("connection refused")) {
		t.Fatalf("did not expect generic connection error to trigger shell hop fallback")
	}
}

func TestBuildBastionShellScriptPasswordMode(t *testing.T) {
	exec := NewSSHExecutor(15 * time.Second)
	node := config.NodeConnection{
		Name:        "arm-node-01",
		IP:          "192.168.10.3",
		SSHUser:     "root",
		SSHPort:     22,
		SSHPassword: "secret",
	}

	script, err := exec.buildBastionShellScript(node, "echo hello")
	if err != nil {
		t.Fatalf("buildBastionShellScript() error = %v", err)
	}
	if !strings.Contains(script, `auth_mode="password"`) {
		t.Fatalf("expected password mode in script")
	}
	if !strings.Contains(script, `target_host="192.168.10.3"`) {
		t.Fatalf("expected target host in script")
	}
	if !strings.Contains(script, "SSH_ASKPASS_REQUIRE=force") {
		t.Fatalf("expected SSH_ASKPASS flow in password mode")
	}
}

func TestBuildBastionShellScriptPrivateKeyMode(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_rsa")
	keyContent := []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nexample\n-----END OPENSSH PRIVATE KEY-----\n")
	if err := os.WriteFile(keyPath, keyContent, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	exec := NewSSHExecutor(15 * time.Second)
	node := config.NodeConnection{
		Name:          "node-01",
		IP:            "192.168.24.4",
		SSHUser:       "root",
		SSHPort:       22,
		SSHPrivateKey: keyPath,
	}

	script, err := exec.buildBastionShellScript(node, "echo hello")
	if err != nil {
		t.Fatalf("buildBastionShellScript() error = %v", err)
	}
	if !strings.Contains(script, `auth_mode="key"`) {
		t.Fatalf("expected key mode in script")
	}
	if !strings.Contains(script, `PasswordAuthentication=no`) {
		t.Fatalf("expected key mode to disable password authentication")
	}
	if !strings.Contains(script, `private_key_b64="`) {
		t.Fatalf("expected embedded private key in generated script")
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
