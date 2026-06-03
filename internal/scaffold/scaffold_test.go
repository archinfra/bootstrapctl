package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTemplatesDefaultSimpleMode(t *testing.T) {
	dir := t.TempDir()

	result, err := WriteTemplates(InitOptions{
		Dir:         dir,
		ClusterName: "demo-cluster",
		Inventory:   "inventory.yaml",
		Profile:     "profile.yaml",
	})
	if err != nil {
		t.Fatalf("WriteTemplates() error = %v", err)
	}

	inventoryContent, err := os.ReadFile(result.InventoryPath)
	if err != nil {
		t.Fatalf("ReadFile(inventory) error = %v", err)
	}
	inventoryText := string(inventoryContent)

	if !strings.Contains(inventoryText, "cluster_name: demo-cluster") {
		t.Fatalf("expected cluster name in inventory template, got %s", inventoryText)
	}
	if !strings.Contains(inventoryText, "bootstrapctl 配置文件") {
		t.Fatalf("expected simplified inventory template, got %s", inventoryText)
	}
	if !strings.Contains(inventoryText, "常改区：优先只改这里") || !strings.Contains(inventoryText, "默认区：一般不用改") {
		t.Fatalf("expected inventory template to separate common edits and defaults, got %s", inventoryText)
	}
	if !strings.Contains(inventoryText, "hostname: node-01") {
		t.Fatalf("expected hostname in simple inventory, got %s", inventoryText)
	}
	if strings.Contains(inventoryText, "roles:") {
		t.Fatalf("simple inventory should not push roles into the user path, got %s", inventoryText)
	}
	if !strings.Contains(inventoryText, "#   ssh_port: 22") || !strings.Contains(inventoryText, "#   use_sudo: false") {
		t.Fatalf("expected defaults to be documented below as comments, got %s", inventoryText)
	}
	if result.ProfilePath != "" {
		t.Fatalf("default simple mode should not generate profile, got %#v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "profile.yaml")); !os.IsNotExist(err) {
		t.Fatalf("profile.yaml should not exist in simple mode, err=%v", err)
	}
}

func TestWriteTemplatesAdvancedMode(t *testing.T) {
	dir := t.TempDir()

	result, err := WriteTemplates(InitOptions{
		Dir:         dir,
		ClusterName: "demo-cluster",
		Inventory:   "inventory.yaml",
		Profile:     "profile.yaml",
		Advanced:    true,
	})
	if err != nil {
		t.Fatalf("WriteTemplates(advanced) error = %v", err)
	}

	inventoryContent, err := os.ReadFile(result.InventoryPath)
	if err != nil {
		t.Fatalf("ReadFile(inventory) error = %v", err)
	}
	profileContent, err := os.ReadFile(result.ProfilePath)
	if err != nil {
		t.Fatalf("ReadFile(profile) error = %v", err)
	}

	inventoryText := string(inventoryContent)
	profileText := string(profileContent)

	if !strings.Contains(inventoryText, "bootstrapctl inventory 完整模板") {
		t.Fatalf("expected full inventory template, got %s", inventoryText)
	}
	if !strings.Contains(inventoryText, "ssh_auth: yes") {
		t.Fatalf("expected ssh_auth to default to yes in full inventory, got %s", inventoryText)
	}
	if !strings.Contains(profileText, "mode: iptables") {
		t.Fatalf("expected iptables mode in profile template, got %s", profileText)
	}
	if !strings.Contains(profileText, "nofile: 1048576") {
		t.Fatalf("expected explicit ulimit values in profile template, got %s", profileText)
	}
	if !strings.Contains(profileText, "graph_root: /data/graphroot") {
		t.Fatalf("expected explicit storage values in profile template, got %s", profileText)
	}
	if !strings.Contains(profileText, "ssh_authorized_key: yes") {
		t.Fatalf("expected ssh_authorized_key to use yes in profile template, got %s", profileText)
	}
	if !strings.Contains(profileText, "enable_bastion_hop: true") {
		t.Fatalf("expected bastion hop ssh key flow in profile template, got %s", profileText)
	}
	if !strings.Contains(profileText, "public_key_path: \"\"") {
		t.Fatalf("expected auto-discovery style public_key_path in profile template, got %s", profileText)
	}
	if strings.Contains(profileText, "ulimit: true") {
		t.Fatalf("profile template should not expose ulimit enable switch by default, got %s", profileText)
	}
	if result.InventoryPath == "" || result.ProfilePath == "" {
		t.Fatalf("expected result paths to be populated, got %#v", result)
	}
}

func TestWriteTemplatesRejectsExistingInventoryWithoutForce(t *testing.T) {
	dir := t.TempDir()
	inventoryPath := filepath.Join(dir, "inventory.yaml")
	if err := os.WriteFile(inventoryPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := WriteTemplates(InitOptions{
		Dir:         dir,
		ClusterName: "demo-cluster",
		Inventory:   "inventory.yaml",
		Profile:     "profile.yaml",
	})
	if err == nil {
		t.Fatalf("expected existing inventory to fail without force")
	}
}

func TestWriteTemplatesRejectsExistingProfileInAdvancedModeWithoutForce(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(profilePath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := WriteTemplates(InitOptions{
		Dir:         dir,
		ClusterName: "demo-cluster",
		Inventory:   "inventory.yaml",
		Profile:     "profile.yaml",
		Advanced:    true,
	})
	if err == nil {
		t.Fatalf("expected existing profile to fail in advanced mode without force")
	}
}

func TestWriteTemplatesAllowsOverwriteWhenForceEnabled(t *testing.T) {
	dir := t.TempDir()
	inventoryPath := filepath.Join(dir, "inventory.yaml")
	profilePath := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(inventoryPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(profilePath, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := WriteTemplates(InitOptions{
		Dir:         dir,
		ClusterName: "demo-cluster",
		Inventory:   "inventory.yaml",
		Profile:     "profile.yaml",
		Force:       true,
		Advanced:    true,
	})
	if err != nil {
		t.Fatalf("WriteTemplates(force) error = %v", err)
	}
}
