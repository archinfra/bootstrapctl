package tasks

import (
	"testing"

	"github.com/yuanyp8/bootstrapctl/internal/config"
)

func TestBuildGeneratesExpectedTasks(t *testing.T) {
	inventory := config.Inventory{
		ClusterName: "demo",
		Transport: config.Transport{
			SSHUser:     "root",
			SSHPort:     22,
			SSHPassword: "changeme",
		},
		Nodes: []config.Node{
			{Name: "master01", IP: "192.168.24.5", Roles: []string{"master"}},
			{Name: "worker01", IP: "192.168.24.4", Roles: []string{"worker"}},
		},
	}
	inventory.ApplyDefaults()

	profile := config.Profile{Name: "k8s-host-init"}
	profile.ApplyDefaults()

	taskList := Build(inventory, profile)
	if len(taskList) != 18 {
		t.Fatalf("expected 18 tasks for 2 nodes with 9 tasks each, got %d", len(taskList))
	}
}

func TestBuildIncludesSSHAuthorizedKeyTaskWhenEnabled(t *testing.T) {
	inventory := config.Inventory{
		ClusterName: "demo",
		Transport: config.Transport{
			SSHUser:     "root",
			SSHPort:     22,
			SSHPassword: "changeme",
		},
		Nodes: []config.Node{
			{Name: "master01", IP: "192.168.24.5", Roles: []string{"master"}},
		},
	}
	inventory.ApplyDefaults()

	profile := config.Profile{Name: "k8s-host-init"}
	profile.ApplyDefaults()
	enabled := true
	profile.Features.SSHAuthorizedKey = &enabled
	profile.SSHKey.ResolvedPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBootstrapCtlExampleKey bootstrapctl@example"

	taskList := Build(inventory, profile)
	if len(taskList) != 10 {
		t.Fatalf("expected 10 tasks for 1 node with ssh_authorized_key enabled, got %d", len(taskList))
	}

	found := false
	for _, task := range taskList {
		if task.Key() == "ssh-authorized-key" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ssh-authorized-key task to be present")
	}
}

func TestBuildIncludesBastionHopTaskWhenNodeUsesBastion(t *testing.T) {
	inventory := config.Inventory{
		ClusterName: "demo",
		Transport: config.Transport{
			SSHUser:     "root",
			SSHPort:     22,
			SSHPassword: "changeme",
		},
		Nodes: []config.Node{
			{Name: "worker01", IP: "192.168.24.4", Roles: []string{"worker"}, Bastion: &config.Bastion{Host: "36.137.200.29"}},
		},
	}
	inventory.ApplyDefaults()

	profile := config.Profile{Name: "k8s-host-init"}
	profile.ApplyDefaults()
	enabled := true
	profile.Features.SSHAuthorizedKey = &enabled
	profile.SSHKey.ResolvedPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBootstrapCtlExampleKey bootstrapctl@example"

	taskList := Build(inventory, profile)

	var controllerTasks int
	var hopTasks int
	var bastionConfigTasks int
	for _, task := range taskList {
		switch task.Key() {
		case "ssh-authorized-key":
			controllerTasks++
		case "ssh-bastion-hop-key":
			hopTasks++
		case "ssh-bastion-client-config":
			bastionConfigTasks++
		}
	}

	if controllerTasks != 2 {
		t.Fatalf("expected controller public key to be distributed to bastion and target, got %d tasks", controllerTasks)
	}
	if hopTasks != 1 {
		t.Fatalf("expected one bastion hop task, got %d", hopTasks)
	}
	if bastionConfigTasks != 1 {
		t.Fatalf("expected one bastion ssh client config task, got %d", bastionConfigTasks)
	}
}

func TestBuildIncludesBastionHopTasksEvenWhenControllerSSHKeyDistributionDisabled(t *testing.T) {
	inventory := config.Inventory{
		ClusterName: "demo",
		Transport: config.Transport{
			SSHUser:     "root",
			SSHPort:     22,
			SSHPassword: "changeme",
		},
		Nodes: []config.Node{
			{Name: "master01", IP: "36.137.200.29", Roles: []string{"master"}},
			{Name: "node01", IP: "192.168.24.4", Roles: []string{"worker"}, Bastion: &config.Bastion{Host: "36.137.200.29"}},
		},
	}
	inventory.ApplyDefaults()

	profile := config.Profile{Name: "k8s-host-init"}
	profile.ApplyDefaults()
	disabled := false
	profile.Features.SSHAuthorizedKey = &disabled

	taskList := Build(inventory, profile)

	var controllerTasks int
	var hopTasks int
	var bastionConfigTasks int
	for _, task := range taskList {
		switch task.Key() {
		case "ssh-authorized-key":
			controllerTasks++
		case "ssh-bastion-hop-key":
			hopTasks++
		case "ssh-bastion-client-config":
			bastionConfigTasks++
		}
	}

	if controllerTasks != 0 {
		t.Fatalf("expected controller ssh key task to stay disabled, got %d", controllerTasks)
	}
	if hopTasks != 1 {
		t.Fatalf("expected one bastion hop task even when controller ssh key is disabled, got %d", hopTasks)
	}
	if bastionConfigTasks != 1 {
		t.Fatalf("expected one bastion ssh client config task even when controller ssh key is disabled, got %d", bastionConfigTasks)
	}
}

func TestBuildIncludesManagedAdminTasksWhenEnabled(t *testing.T) {
	inventory := config.Inventory{
		ClusterName: "demo",
		Transport: config.Transport{
			SSHUser:     "root",
			SSHPort:     22,
			SSHPassword: "changeme",
		},
		Nodes: []config.Node{
			{Name: "master01", IP: "192.168.24.5", Roles: []string{"master"}},
		},
	}
	inventory.ApplyDefaults()

	profile := config.Profile{Name: "k8s-host-init"}
	profile.ApplyDefaults()
	enabled := true
	profile.Features.ManagedAdmin = &enabled
	profile.ManagedAdmin.ResolvedPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIManagedAdminKey bootstrapctl@example"

	taskList := Build(inventory, profile)
	var adminTaskFound bool
	var rootPolicyTaskFound bool
	for _, task := range taskList {
		switch task.Key() {
		case "managed-admin-user":
			adminTaskFound = true
		case "root-ssh-login-policy":
			rootPolicyTaskFound = true
		}
	}

	if !adminTaskFound {
		t.Fatalf("expected managed admin task to be present")
	}
	if !rootPolicyTaskFound {
		t.Fatalf("expected root ssh policy task to be present")
	}
}
