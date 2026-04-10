package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadInventoryDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.yaml")
	content := `
cluster_name: demo
transport:
  ssh_password: changeme
nodes:
  - name: master01
    ip: 192.168.24.5
    roles: [master]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write inventory: %v", err)
	}

	inventory, err := LoadInventory(path)
	if err != nil {
		t.Fatalf("LoadInventory() error = %v", err)
	}

	node := inventory.ResolveNodes()[0]
	if node.SSHUser != "root" {
		t.Fatalf("expected default ssh user root, got %q", node.SSHUser)
	}
	if node.SSHPort != 22 {
		t.Fatalf("expected default ssh port 22, got %d", node.SSHPort)
	}
	if node.SSHPassword != "changeme" {
		t.Fatalf("expected inherited password")
	}
	if node.HostIP != "" {
		t.Fatalf("expected host_ip to default empty, got %q", node.HostIP)
	}
}

func TestLoadInventoryBastionDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.yaml")
	content := `
cluster_name: demo
transport:
  ssh_password: changeme
  bastion:
    host: 36.137.200.29
    ssh_password: changeme
nodes:
  - name: worker01
    ip: 192.168.24.4
    roles: [worker]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write inventory: %v", err)
	}

	inventory, err := LoadInventory(path)
	if err != nil {
		t.Fatalf("LoadInventory() error = %v", err)
	}

	node := inventory.ResolveNodes()[0]
	if node.Bastion == nil {
		t.Fatalf("expected bastion defaults to be inherited")
	}
	if node.Bastion.SSHPort != 22 {
		t.Fatalf("expected default bastion ssh port 22, got %d", node.Bastion.SSHPort)
	}
}

func TestLoadInventoryNodeBastionInheritsTransportAuth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.yaml")
	content := `
cluster_name: demo
transport:
  ssh_user: root
  ssh_password: changeme
nodes:
  - name: worker01
    ip: 192.168.24.4
    roles: [worker]
    bastion:
      host: 36.137.200.29
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write inventory: %v", err)
	}

	inventory, err := LoadInventory(path)
	if err != nil {
		t.Fatalf("LoadInventory() error = %v", err)
	}

	node := inventory.ResolveNodes()[0]
	if node.Bastion == nil {
		t.Fatalf("expected node bastion to be populated")
	}
	if node.Bastion.SSHUser != "root" {
		t.Fatalf("expected bastion ssh user to inherit root, got %q", node.Bastion.SSHUser)
	}
	if node.Bastion.SSHPort != 22 {
		t.Fatalf("expected bastion ssh port 22, got %d", node.Bastion.SSHPort)
	}
	if node.Bastion.SSHPassword != "changeme" {
		t.Fatalf("expected bastion password to inherit transport password, got %q", node.Bastion.SSHPassword)
	}
}

func TestLoadProfileDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.yaml")
	content := `
name: test
storage:
  graph_root: /data/graphroot
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	profile, err := LoadProfile(path)
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}

	if !profile.Features.HostnameEnabled() || !profile.Features.HostsFileEnabled() || !profile.Features.UlimitEnabled() {
		t.Fatalf("expected feature defaults to be enabled")
	}
	if !profile.Features.SSHAuthorizedKeyEnabled() {
		t.Fatalf("expected ssh_authorized_key to default to enabled")
	}
	if profile.Features.ManagedAdminEnabled() {
		t.Fatalf("expected managed_admin to default to disabled")
	}
	if !profile.Features.KernelNetworkEnabled() || !profile.Features.FirewallEnabled() {
		t.Fatalf("expected kernel_network and firewall defaults to be enabled")
	}
	if profile.Storage.CRIRoot != "/data/containerd" {
		t.Fatalf("expected default cri root, got %q", profile.Storage.CRIRoot)
	}
	if profile.Storage.StorageConfPath != "/etc/containers/storage.conf" {
		t.Fatalf("expected default storage conf path, got %q", profile.Storage.StorageConfPath)
	}
	if profile.Storage.RunRoot != "/run/containers/storage" {
		t.Fatalf("expected default run root, got %q", profile.Storage.RunRoot)
	}
	if profile.Storage.GraphDriver != "overlay" {
		t.Fatalf("expected default graph driver overlay, got %q", profile.Storage.GraphDriver)
	}
	if profile.Ulimit.NoFile == 0 || profile.Ulimit.NProc == 0 {
		t.Fatalf("expected default ulimit values")
	}
	if len(profile.KernelNetwork.Modules) == 0 || len(profile.KernelNetwork.Sysctls) == 0 {
		t.Fatalf("expected default kernel network config to be populated")
	}
	if !profile.Firewall.ManageFirewalldEnabled() || !profile.Firewall.ManageUFWEnabled() {
		t.Fatalf("expected default firewall management switches to be enabled")
	}
	if !profile.SSHKey.EnableBastionHopEnabled() {
		t.Fatalf("expected bastion hop key flow to default to enabled")
	}
	if profile.SSHKey.BastionKeyPath != "~/.ssh/bootstrapctl_ed25519" {
		t.Fatalf("expected default bastion key path, got %q", profile.SSHKey.BastionKeyPath)
	}
	if !profile.SSHKey.ManageBastionSSHConfigEnabled() {
		t.Fatalf("expected bastion ssh config management to default to enabled")
	}
	if profile.SSHKey.BastionSSHConfigPath != "~/.ssh/config" {
		t.Fatalf("expected default bastion ssh config path, got %q", profile.SSHKey.BastionSSHConfigPath)
	}
	if profile.ManagedAdmin.Username != "opsadmin" {
		t.Fatalf("expected default managed admin username, got %q", profile.ManagedAdmin.Username)
	}
	if !profile.ManagedAdmin.CreateHomeEnabled() || !profile.ManagedAdmin.GrantSudoEnabled() || !profile.ManagedAdmin.SudoNoPasswdEnabled() {
		t.Fatalf("expected managed admin defaults to be enabled")
	}
	if !profile.ManagedAdmin.InstallControllerPublicKeyEnabled() || !profile.ManagedAdmin.DisableRootSSHEnabled() {
		t.Fatalf("expected managed admin key install and root ssh disable to default to enabled")
	}
}

func TestLoadProfileSSHAuthorizedKeyFromInline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.yaml")
	content := `
name: test
features:
  ssh_authorized_key: true
ssh_key:
  authorized_user: root
  public_key: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBootstrapCtlExampleKey bootstrapctl@example
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	profile, err := LoadProfile(path)
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}

	if !profile.Features.SSHAuthorizedKeyEnabled() {
		t.Fatalf("expected ssh_authorized_key to stay enabled")
	}
	if profile.SSHKey.ResolvedPublicKey == "" {
		t.Fatalf("expected public key to be resolved")
	}
	if profile.SSHKey.ResolvedPublicKey != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBootstrapCtlExampleKey bootstrapctl@example" {
		t.Fatalf("unexpected resolved public key: %q", profile.SSHKey.ResolvedPublicKey)
	}
}

func TestLoadProfileSSHAuthorizedKeyAutoGeneratesDedicatedKey(t *testing.T) {
	dir := t.TempDir()
	generatedPath := filepath.Join(dir, "bootstrapctl_ed25519")
	path := filepath.Join(dir, "profile.yaml")
	content := `
name: test
features:
  ssh_authorized_key: true
ssh_key:
  authorized_user: root
  auto_generate: true
  generated_key_path: ` + generatedPath + `
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	profile, err := LoadProfile(path)
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}

	if profile.SSHKey.ResolvedPublicKey == "" {
		t.Fatalf("expected generated public key to be resolved")
	}
	if profile.SSHKey.PublicKeyPath != generatedPath+".pub" {
		t.Fatalf("expected generated public key path, got %q", profile.SSHKey.PublicKeyPath)
	}
	if _, err := os.Stat(generatedPath); err != nil {
		t.Fatalf("expected generated private key to exist: %v", err)
	}
	if _, err := os.Stat(generatedPath + ".pub"); err != nil {
		t.Fatalf("expected generated public key to exist: %v", err)
	}
}

func TestLoadProfileSSHAuthorizedKeyAutoGeneratesAtExplicitPublicKeyPath(t *testing.T) {
	dir := t.TempDir()
	explicitPubPath := filepath.Join(dir, "id_ed25519.pub")
	path := filepath.Join(dir, "profile.yaml")
	content := `
name: test
features:
  ssh_authorized_key: true
ssh_key:
  authorized_user: root
  auto_generate: true
  public_key_path: ` + explicitPubPath + `
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	profile, err := LoadProfile(path)
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}

	if profile.SSHKey.PublicKeyPath != explicitPubPath {
		t.Fatalf("expected explicit public key path to stay unchanged, got %q", profile.SSHKey.PublicKeyPath)
	}
	if _, err := os.Stat(strings.TrimSuffix(explicitPubPath, ".pub")); err != nil {
		t.Fatalf("expected generated private key to exist: %v", err)
	}
	if _, err := os.Stat(explicitPubPath); err != nil {
		t.Fatalf("expected generated public key to exist: %v", err)
	}
}

func TestLoadProfileExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.yaml")
	content := `
name: test
features:
  hosts_file: false
  firewall: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	profile, err := LoadProfile(path)
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}

	if profile.Features.HostsFileEnabled() {
		t.Fatalf("expected hosts_file to remain disabled")
	}
	if profile.Features.FirewallEnabled() {
		t.Fatalf("expected firewall to remain disabled")
	}
	if !profile.Features.HostnameEnabled() {
		t.Fatalf("expected unspecified features to keep default true")
	}
}

func TestLoadProfileManagedAdminResolvesControllerPublicKeyFromInline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.yaml")
	content := `
name: test
features:
  managed_admin: true
managed_admin:
  username: opsuser
  controller_public_key: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIManagedAdminKey bootstrapctl@example
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	profile, err := LoadProfile(path)
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}

	if !profile.Features.ManagedAdminEnabled() {
		t.Fatalf("expected managed_admin to stay enabled")
	}
	if profile.ManagedAdmin.ResolvedPublicKey == "" {
		t.Fatalf("expected managed admin controller public key to be resolved")
	}
	if profile.ManagedAdmin.ResolvedPublicKey != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIManagedAdminKey bootstrapctl@example" {
		t.Fatalf("unexpected resolved managed admin public key: %q", profile.ManagedAdmin.ResolvedPublicKey)
	}
}
