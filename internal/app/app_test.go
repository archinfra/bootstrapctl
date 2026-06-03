package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuanyp8/bootstrapctl/internal/ui"
)

func TestParseLifecycleFlagsSupportsShortAliases(t *testing.T) {
	options, ok := parseLifecycleFlags(ui.NewConsole(), "plan", []string{"-i", "inventory.yaml", "-p", "profile.yaml", "-t", "20s"})
	if !ok {
		t.Fatalf("expected short aliases to parse successfully")
	}
	if options.InventoryPath != "inventory.yaml" {
		t.Fatalf("unexpected inventory path: %q", options.InventoryPath)
	}
	if options.ProfilePath != "profile.yaml" {
		t.Fatalf("unexpected profile path: %q", options.ProfilePath)
	}
	if options.Timeout.String() != "20s" {
		t.Fatalf("unexpected timeout: %s", options.Timeout)
	}
}

func TestParseLifecycleFlagsAllowsBuiltinProfile(t *testing.T) {
	options, ok := parseLifecycleFlags(ui.NewConsole(), "plan", []string{"-i", "inventory.yaml"})
	if !ok {
		t.Fatalf("expected lifecycle flags without profile to parse successfully")
	}
	if options.InventoryPath != "inventory.yaml" {
		t.Fatalf("unexpected inventory path: %q", options.InventoryPath)
	}
	if options.ProfilePath != "" {
		t.Fatalf("expected profile path to stay empty for builtin profile, got %q", options.ProfilePath)
	}
	if !options.SyncOpsEnv {
		t.Fatalf("expected ops-environment.sh sync to be enabled by default in file mode")
	}
}

func TestParseLifecycleFlagsSupportsInlineSingleHost(t *testing.T) {
	options, ok := parseLifecycleFlags(ui.NewConsole(), "plan", []string{"--host", "192.168.1.10", "-p", "secret", "--user", "root", "--sudo", "-t", "20s"})
	if !ok {
		t.Fatalf("expected inline single host to parse successfully")
	}
	if options.InventoryPath != "" {
		t.Fatalf("expected inventory path to be empty in inline mode, got %q", options.InventoryPath)
	}
	if options.Inline.Host != "192.168.1.10" || options.Inline.SSHPassword != "secret" || !options.Inline.UseSudo {
		t.Fatalf("unexpected inline options: %#v", options.Inline)
	}
	if options.Timeout.String() != "20s" {
		t.Fatalf("unexpected timeout: %s", options.Timeout)
	}
}

func TestParseLifecycleFlagsRejectsMissingInventoryAndHost(t *testing.T) {
	t.Chdir(t.TempDir())
	_, ok := parseLifecycleFlags(ui.NewConsole(), "plan", []string{})
	if ok {
		t.Fatalf("expected lifecycle flags without inventory/default file or host to fail")
	}
}

func TestParseLifecycleFlagsDefaultsToCurrentInventory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "inventory.yaml"), []byte("nodes: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	options, ok := parseLifecycleFlags(ui.NewConsole(), "plan", []string{})
	if !ok {
		t.Fatalf("expected lifecycle flags to use current directory inventory.yaml")
	}
	want := filepath.Join(dir, "inventory.yaml")
	if options.InventoryPath != want {
		t.Fatalf("unexpected default inventory path: got %q want %q", options.InventoryPath, want)
	}
}

func TestParseLifecycleFlagsSupportsShortHostAndUser(t *testing.T) {
	options, ok := parseLifecycleFlags(ui.NewConsole(), "apply", []string{"-H", "node-01=192.168.1.10", "-u", "root", "-p", "secret"})
	if !ok {
		t.Fatalf("expected -H/-u/-p inline mode to parse successfully")
	}
	if options.InventoryPath != "" {
		t.Fatalf("expected inline mode, got inventory path %q", options.InventoryPath)
	}
	if options.Inline.Hosts != "node-01=192.168.1.10" || options.Inline.SSHUser != "root" || options.Inline.SSHPassword != "secret" {
		t.Fatalf("unexpected inline options: %#v", options.Inline)
	}
}

func TestParseLifecycleFlagsCanDisableOpsEnvSync(t *testing.T) {
	options, ok := parseLifecycleFlags(ui.NewConsole(), "plan", []string{"-i", "inventory.yaml", "--no-sync-ops-env"})
	if !ok {
		t.Fatalf("expected lifecycle flags with no-sync-ops-env to parse successfully")
	}
	if options.SyncOpsEnv {
		t.Fatalf("expected ops-environment.sh sync to be disabled")
	}
}

func TestParseLifecycleFlagsRejectsInventoryAndInlineHostTogether(t *testing.T) {
	_, ok := parseLifecycleFlags(ui.NewConsole(), "plan", []string{"-i", "inventory.yaml", "--host", "192.168.1.10"})
	if ok {
		t.Fatalf("expected file mode and inline mode conflict to fail")
	}
}

func TestBuildInlineInventorySingleHost(t *testing.T) {
	inventory, err := buildInlineInventory(inlineInventoryOptions{
		Host:        "192.168.1.10",
		ClusterName: "demo",
		SSHUser:     "root",
		SSHPort:     22,
		SSHPassword: "secret",
	})
	if err != nil {
		t.Fatalf("buildInlineInventory() error = %v", err)
	}
	if len(inventory.Nodes) != 1 {
		t.Fatalf("expected one node, got %d", len(inventory.Nodes))
	}
	node := inventory.ResolveNodes()[0]
	if node.Name != "node-01" || node.IP != "192.168.1.10" {
		t.Fatalf("unexpected node: %#v", node)
	}
	if len(node.Roles) != 0 {
		t.Fatalf("default inline host should not expose roles, got %#v", node.Roles)
	}
	if node.SSHPassword != "secret" {
		t.Fatalf("expected password to be inherited")
	}
}

func TestBuildInlineInventoryMultipleHosts(t *testing.T) {
	inventory, err := buildInlineInventory(inlineInventoryOptions{
		Hosts:       "master=10.0.0.1:master,worker=10.0.0.2:worker",
		ClusterName: "demo",
		SSHUser:     "root",
		SSHPort:     22,
	})
	if err != nil {
		t.Fatalf("buildInlineInventory() error = %v", err)
	}
	if len(inventory.Nodes) != 2 {
		t.Fatalf("expected two nodes, got %d", len(inventory.Nodes))
	}
	if inventory.Nodes[0].Name != "master" || inventory.Nodes[0].IP != "10.0.0.1" || inventory.Nodes[0].Roles[0] != "master" {
		t.Fatalf("unexpected first node: %#v", inventory.Nodes[0])
	}
	if inventory.Nodes[1].Name != "worker" || inventory.Nodes[1].IP != "10.0.0.2" || inventory.Nodes[1].Roles[0] != "worker" {
		t.Fatalf("unexpected second node: %#v", inventory.Nodes[1])
	}
}

func TestParseScanFlagsSupportsAbbreviatedInventory(t *testing.T) {
	options, ok := parseScanFlags(ui.NewConsole(), []string{"--inv", "inventory.yaml"})
	if !ok {
		t.Fatalf("expected abbreviated inventory alias to parse successfully")
	}
	if options.InventoryPath != "inventory.yaml" {
		t.Fatalf("unexpected inventory path: %q", options.InventoryPath)
	}
}

func TestParseScanFlagsAcceptsOptionalProfileAlias(t *testing.T) {
	options, ok := parseScanFlags(ui.NewConsole(), []string{"-i", "inventory.yaml", "-p", "profile.yaml", "-t", "20s"})
	if !ok {
		t.Fatalf("expected scan flags with optional profile to parse successfully")
	}
	if options.InventoryPath != "inventory.yaml" {
		t.Fatalf("unexpected inventory path: %q", options.InventoryPath)
	}
	if options.ProfilePath != "profile.yaml" {
		t.Fatalf("unexpected profile path: %q", options.ProfilePath)
	}
	if options.Timeout.String() != "20s" {
		t.Fatalf("unexpected timeout: %s", options.Timeout)
	}
}

func TestParseQuickstartFlagsAllowsNoHostForInitAlias(t *testing.T) {
	options, ok := parseQuickstartFlags(ui.NewConsole(), []string{"-d", "demo"})
	if !ok {
		t.Fatalf("expected quickstart without --host/--hosts to parse for init-compatible behavior")
	}
	if options.Dir != "demo" {
		t.Fatalf("unexpected quickstart dir: %#v", options)
	}
}

func TestParseQuickstartFlagsSupportsSingleHost(t *testing.T) {
	options, ok := parseQuickstartFlags(ui.NewConsole(), []string{"--host", "192.168.1.10", "-p", "secret", "-d", "demo"})
	if !ok {
		t.Fatalf("expected quickstart flags to parse successfully")
	}
	if options.Dir != "demo" || options.Inline.Host != "192.168.1.10" || options.Inline.SSHPassword != "secret" {
		t.Fatalf("unexpected quickstart options: %#v", options)
	}
}

func TestParseDoctorFlagsDefaultsToSyncOpsEnv(t *testing.T) {
	options, ok := parseDoctorFlags(ui.NewConsole(), []string{"-i", "inventory.yaml"})
	if !ok {
		t.Fatalf("expected doctor flags to parse successfully")
	}
	if !options.SyncOpsEnv {
		t.Fatalf("expected doctor to sync ops env by default")
	}
}

func TestParseDoctorFlagsDefaultsToCurrentInventory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "inventory.yaml"), []byte("nodes: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	options, ok := parseDoctorFlags(ui.NewConsole(), []string{})
	if !ok {
		t.Fatalf("expected doctor/check to use current directory inventory.yaml")
	}
	want := filepath.Join(dir, "inventory.yaml")
	if options.InventoryPath != want {
		t.Fatalf("unexpected default inventory path: got %q want %q", options.InventoryPath, want)
	}
}

func TestRenderQuickstartInventoryKeepsMinimalShape(t *testing.T) {
	inventory, err := buildInlineInventory(inlineInventoryOptions{
		Host:        "192.168.1.10",
		ClusterName: "demo",
		SSHUser:     "root",
		SSHPort:     22,
		SSHPassword: "secret",
	})
	if err != nil {
		t.Fatalf("buildInlineInventory() error = %v", err)
	}
	content := renderQuickstartInventory(inventory)
	if !strings.Contains(content, "cluster_name: demo") {
		t.Fatalf("expected cluster name, got %s", content)
	}
	if !strings.Contains(content, "hostname: node-01") {
		t.Fatalf("expected hostname field, got %s", content)
	}
	if strings.Contains(content, "ssh_auth:") || strings.Contains(content, "ssh_port:") || strings.Contains(content, "use_sudo:") || strings.Contains(content, "roles:") {
		t.Fatalf("quickstart inventory should hide default/advanced fields, got %s", content)
	}
	if strings.Contains(content, "bastion:") || strings.Contains(content, "profile:") {
		t.Fatalf("quickstart inventory should stay minimal, got %s", content)
	}
}
