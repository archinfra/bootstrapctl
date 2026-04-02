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
				Name:    "sichuan-master1",
				IP:      "10.120.103.6",
				Roles:   []string{"master"},
				SSHUser: "root",
				SSHPort: 22,
				UseSudo: boolPtr(false),
			},
			{
				Name:    "sichuan-node1",
				IP:      "10.120.103.7",
				Roles:   []string{"worker"},
				SSHUser: "root",
				SSHPort: 22,
				UseSudo: boolPtr(false),
			},
		},
	}
	inventory.ApplyDefaults()

	content := RenderInventoryShell(inventory)

	assertContains(t, content, "#!/bin/bash")
	assertContains(t, content, "export NODE_IPS=(10.120.103.6 10.120.103.7)")
	assertContains(t, content, "export NODE_NAMES=(sichuan-master1 sichuan-node1)")
	assertContains(t, content, "export MOUNT_DIR=/data")
	assertContains(t, content, "export GRAPH_BASE=/data/graphroot")
}

func TestShellValueQuotesWhitespace(t *testing.T) {
	got := shellValue("node 01")
	if got != "'node 01'" {
		t.Fatalf("unexpected shell value: %s", got)
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
