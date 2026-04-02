package exporter

import (
	"strings"
	"testing"

	"github.com/yuanyp8/bootstrapctl/internal/config"
)

func TestRenderInventoryShell(t *testing.T) {
	inventory := config.Inventory{
		ClusterName: "demo-env",
		Nodes: []config.Node{
			{
				Name:        "master-01",
				IP:          "36.137.200.29",
				HostIP:      "192.168.24.5",
				Roles:       []string{"master", "etcd"},
				SSHUser:     "root",
				SSHPort:     22,
				UseSudo:     boolPtr(false),
				SSHPassword: "secret",
			},
			{
				Name:          "node-01",
				IP:            "192.168.24.4",
				Roles:         []string{"worker"},
				SSHUser:       "ops",
				SSHPort:       2222,
				SSHPrivateKey: "~/.ssh/id_ed25519",
				UseSudo:       boolPtr(true),
				Bastion: &config.Bastion{
					Host:    "36.137.200.29",
					SSHUser: "root",
					SSHPort: 22,
				},
			},
		},
	}
	inventory.ApplyDefaults()

	content := RenderInventoryShell(inventory)

	assertContains(t, content, "CLUSTER_NAME='demo-env'")
	assertContains(t, content, "NODE_COUNT=2")
	assertContains(t, content, "NODE_NAMES=('master-01' 'node-01')")
	assertContains(t, content, "NODE_HOST_IPS=('192.168.24.5' '192.168.24.4')")
	assertContains(t, content, "NODE_SSH_USERS=('root' 'ops')")
	assertContains(t, content, "NODE_SSH_PORTS=('22' '2222')")
	assertContains(t, content, "NODE_USE_SUDO=('false' 'true')")
	assertContains(t, content, "NODE_BASTION_HOSTS=('' '36.137.200.29')")
	assertContains(t, content, "NODE_ROLES=('etcd,master' 'worker')")
	assertContains(t, content, "NODE_SSH_PASSWORDS=('secret' '')")
}

func TestShellQuoteEscapesSingleQuote(t *testing.T) {
	content := shellQuote(`ops'env`)
	if content != `'ops'\''env'` {
		t.Fatalf("unexpected quote result: %s", content)
	}
}

func assertContains(t *testing.T, content, want string) {
	t.Helper()
	if !strings.Contains(content, want) {
		t.Fatalf("expected content to contain %q, got:\n%s", want, content)
	}
}

func boolPtr(value bool) *bool {
	return &value
}
