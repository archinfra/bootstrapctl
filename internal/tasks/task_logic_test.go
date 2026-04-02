package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

func testNode() config.NodeConnection {
	return config.NodeConnection{
		Name:        "node-1",
		IP:          "192.168.24.5",
		SSHUser:     "root",
		SSHPort:     22,
		SSHPassword: "changeme",
	}
}

func TestHostnameTaskCheckAndApply(t *testing.T) {
	task := &HostnameTask{
		NodeSpec:        testNode(),
		DesiredHostname: "k8s-master01",
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		if strings.Contains(script, "hostnamectl --static status") {
			return remote.Result{Output: "CHANGE:old-host\n", ExitCode: 0}, nil
		}
		if strings.Contains(script, "hostnamectl set-hostname") {
			return remote.Result{Output: "CHANGED:k8s-master01\n", ExitCode: 0}, nil
		}
		return remote.Result{Output: "unexpected", ExitCode: 1}, nil
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected hostname change to be needed")
	}

	apply, err := task.Apply(context.Background(), exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected hostname apply to report changed")
	}
}

func TestHostsFileTaskCheck(t *testing.T) {
	task := &HostsFileTask{
		NodeSpec:     testNode(),
		BlockContent: "# BEGIN BOOTSTRAPCTL HOSTS\n192.168.24.5 k8s-master01\n# END BOOTSTRAPCTL HOSTS",
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		if !strings.Contains(script, "# BEGIN BOOTSTRAPCTL HOSTS") {
			t.Fatalf("expected hosts block markers in script")
		}
		return remote.Result{Output: "CHANGE\n", ExitCode: 0}, nil
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected hosts file drift to require update")
	}
}

func TestHostsFileTaskResolvesHostIPFromNodeConfigOrRemote(t *testing.T) {
	task := &HostsFileTask{
		NodeSpec: testNode(),
		ClusterNodes: []config.NodeConnection{
			{Name: "master-01", IP: "36.137.200.29", HostIP: "192.168.24.5", SSHUser: "root", SSHPort: 22, SSHPassword: "changeme"},
			{Name: "node-01", IP: "192.168.24.4", SSHUser: "root", SSHPort: 22, SSHPassword: "changeme"},
		},
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		if strings.Contains(script, "hostname -I") {
			if node.Name == "node-01" {
				return remote.Result{Output: "192.168.24.4\n", ExitCode: 0}, nil
			}
			t.Fatalf("should not resolve host_ip remotely for node with explicit host_ip")
		}
		return remote.Result{Output: "CHANGE\n", ExitCode: 0}, nil
	})

	block, err := task.desiredBlock(context.Background(), exec)
	if err != nil {
		t.Fatalf("desiredBlock() error = %v", err)
	}
	if !strings.Contains(block, "192.168.24.5 master-01") {
		t.Fatalf("expected explicit host_ip entry, got %s", block)
	}
	if !strings.Contains(block, "192.168.24.4 node-01") {
		t.Fatalf("expected remote-resolved host_ip entry, got %s", block)
	}
}

func TestSSHAuthorizedKeyTaskCheckAndApply(t *testing.T) {
	task := &SSHAuthorizedKeyTask{
		NodeSpec:       testNode(),
		AuthorizedUser: "root",
		PublicKey:      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBootstrapCtlExampleKey bootstrapctl@example",
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		if !strings.Contains(script, "authorized_keys") {
			t.Fatalf("expected authorized_keys path in script")
		}
		if strings.Contains(script, "grep -Fqx") && strings.Contains(script, "CHANGE:") {
			return remote.Result{Output: "CHANGE:/root/.ssh/authorized_keys\n", ExitCode: 0}, nil
		}
		if strings.Contains(script, "install -d -m 700") {
			return remote.Result{Output: "CHANGED:/root/.ssh/authorized_keys\n", ExitCode: 0}, nil
		}
		return remote.Result{Output: "unexpected", ExitCode: 1}, nil
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected ssh key distribution to be needed")
	}

	apply, err := task.Apply(context.Background(), exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected ssh key apply to report changed")
	}
}

func TestSSHBastionHopKeyTaskCheckAndApply(t *testing.T) {
	task := &SSHBastionHopKeyTask{
		TargetNodeSpec: testNode(),
		BastionNodeSpec: config.NodeConnection{
			Name:        "bastion@node-1",
			IP:          "36.137.200.29",
			SSHUser:     "root",
			SSHPort:     22,
			SSHPassword: "changeme",
		},
		AuthorizedUser: "root",
		BastionKeyPath: "~/.ssh/bootstrapctl_ed25519",
	}

	bastionPublicKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBastionExampleKey bootstrapctl-bastion@example"
	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		switch {
		case node.Name == "bastion@node-1" && strings.Contains(script, "requested_key_path"):
			if strings.Contains(script, "ssh-keygen -q -t ed25519") {
				return remote.Result{Output: "CHANGE:/root/.ssh/bootstrapctl_ed25519:" + encodeBase64(bastionPublicKey) + "\n", ExitCode: 0}, nil
			}
			return remote.Result{Output: "OK:/root/.ssh/bootstrapctl_ed25519:" + encodeBase64(bastionPublicKey) + "\n", ExitCode: 0}, nil
		case node.Name == "node-1" && strings.Contains(script, "grep -Fqx"):
			return remote.Result{Output: "CHANGE:/root/.ssh/authorized_keys\n", ExitCode: 0}, nil
		case node.Name == "node-1" && strings.Contains(script, "install -d -m 700"):
			return remote.Result{Output: "CHANGED:/root/.ssh/authorized_keys\n", ExitCode: 0}, nil
		default:
			return remote.Result{Output: "unexpected", ExitCode: 1}, nil
		}
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected bastion hop key task to require change")
	}

	apply, err := task.Apply(context.Background(), exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected bastion hop key apply to report changed")
	}
}

func TestSSHBastionClientConfigTaskCheckAndApply(t *testing.T) {
	task := &SSHBastionClientConfigTask{
		TargetNodeSpec: config.NodeConnection{
			Name:        "amd-node-01",
			IP:          "192.168.24.4",
			HostIP:      "192.168.24.4",
			SSHUser:     "root",
			SSHPort:     22,
			SSHPassword: "changeme",
		},
		BastionNodeSpec: config.NodeConnection{
			Name:        "bastion@amd-node-01",
			IP:          "36.137.200.29",
			SSHUser:     "root",
			SSHPort:     22,
			SSHPassword: "changeme",
		},
		BastionKeyPath:       "~/.ssh/bootstrapctl_ed25519",
		BastionSSHConfigPath: "~/.ssh/config",
	}
	block := renderBastionSSHClientConfigBlock(task.TargetNodeSpec, task.BastionKeyPath)
	if !strings.Contains(block, "IdentityFile ~/.ssh/bootstrapctl_ed25519") {
		t.Fatalf("expected identity file in bastion ssh config block")
	}
	if !strings.Contains(block, "Host amd-node-01 192.168.24.4") {
		t.Fatalf("expected host aliases in bastion ssh config block")
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		switch {
		case node.Name == "bastion@amd-node-01" && strings.Contains(script, "cmp -s"):
			return remote.Result{Output: "CHANGE:/root/.ssh/config\n", ExitCode: 0}, nil
		case node.Name == "bastion@amd-node-01" && strings.Contains(script, "cp \"$candidate_file\" \"$config_path\""):
			return remote.Result{Output: "CHANGED:/root/.ssh/config\n", ExitCode: 0}, nil
		default:
			return remote.Result{Output: "unexpected", ExitCode: 1}, nil
		}
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected bastion ssh client config to require change")
	}

	apply, err := task.Apply(context.Background(), exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected bastion ssh client config apply to report changed")
	}
}

func TestSSHControllerClientConfigTaskCheckAndApply(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config")
	task := &SSHControllerClientConfigTask{
		TargetNodeSpec: config.NodeConnection{
			Name:        "node-01",
			IP:          "192.168.24.4",
			HostIP:      "192.168.24.4",
			SSHUser:     "root",
			SSHPort:     22,
			SSHPassword: "changeme",
		},
		ControllerKeyPath:       "~/.ssh/bootstrapctl_ed25519",
		ControllerSSHConfigPath: configPath,
	}

	check, err := task.Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected controller ssh client config to require change")
	}

	apply, err := task.Apply(context.Background(), nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected controller ssh client config apply to report changed")
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(content), "Host node-01 192.168.24.4") {
		t.Fatalf("expected host aliases in controller ssh config")
	}
	if !strings.Contains(string(content), "IdentityFile ~/.ssh/bootstrapctl_ed25519") {
		t.Fatalf("expected identity file in controller ssh config")
	}

	check, err = task.Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("second Check() error = %v", err)
	}
	if check.Needed {
		t.Fatalf("expected controller ssh client config to be idempotent after apply")
	}
}

func TestManagedAdminUserTaskCheckAndApply(t *testing.T) {
	task := &ManagedAdminUserTask{
		NodeSpec:         testNode(),
		Username:         "opsadmin",
		PasswordHash:     "$6$bootstrapctl$hashvalue",
		Shell:            "/bin/bash",
		PrimaryGroup:     "opsadmin",
		ExtraGroups:      []string{"sudo", "wheel"},
		CreateHome:       true,
		GrantSudo:        true,
		SudoNoPasswd:     true,
		InstallPublicKey: true,
		ControllerPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIManagedAdminKey bootstrapctl@example",
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		switch {
		case strings.Contains(script, "CHANGE:user-missing"):
			if strings.Contains(script, "CHANGE:") {
				return remote.Result{Output: "CHANGE:user-missing\n", ExitCode: 0}, nil
			}
			return remote.Result{Output: "unexpected-check\n", ExitCode: 1}, nil
		case strings.Contains(script, "groupadd \"$primary_group\""):
			return remote.Result{Output: "CHANGED\n", ExitCode: 0}, nil
		case strings.Contains(script, "authorized_keys"):
			if strings.Contains(script, "grep -Fqx") {
				return remote.Result{Output: "CHANGE:/home/opsadmin/.ssh/authorized_keys\n", ExitCode: 0}, nil
			}
			return remote.Result{Output: "CHANGED:/home/opsadmin/.ssh/authorized_keys\n", ExitCode: 0}, nil
		default:
			return remote.Result{Output: "unexpected", ExitCode: 1}, nil
		}
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected managed admin task to require change")
	}

	apply, err := task.Apply(context.Background(), exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected managed admin apply to report changed")
	}
}

func TestRootSSHLoginPolicyTaskCheckAndApply(t *testing.T) {
	task := &RootSSHLoginPolicyTask{
		NodeSpec:        testNode(),
		SSHDConfigPath:  "/etc/ssh/sshd_config",
		PermitRootLogin: "no",
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		switch {
		case strings.Contains(script, "sshd -T -f"):
			return remote.Result{Output: "CHANGE:yes\n", ExitCode: 0}, nil
		case strings.Contains(script, "BEGIN BOOTSTRAPCTL ROOT SSH POLICY"):
			return remote.Result{Output: "CHANGED\n", ExitCode: 0}, nil
		default:
			return remote.Result{Output: "unexpected", ExitCode: 1}, nil
		}
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected root ssh policy task to require change")
	}

	apply, err := task.Apply(context.Background(), exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected root ssh policy apply to report changed")
	}
}

func TestParseStatusLineIgnoresSudoHostWarnings(t *testing.T) {
	output := "sudo: unable to resolve host demo: Name or service not known\nOK:/root/.ssh/authorized_keys\n"
	got := parseStatusLine(output, "OK:", "CHANGE:", "ERROR:")
	if got != "OK:/root/.ssh/authorized_keys" {
		t.Fatalf("unexpected parsed status line: %q", got)
	}
}

func TestSwapTaskCheckParsesChange(t *testing.T) {
	task := &SwapTask{NodeSpec: testNode()}
	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		return remote.Result{Output: "CHANGE:active=1,fstab=1\n", ExitCode: 0}, nil
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected swap change to be needed")
	}
}

func TestSELinuxTaskCheckMissingIsOkay(t *testing.T) {
	task := &SELinuxTask{NodeSpec: testNode()}
	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		return remote.Result{Output: "OK\n", ExitCode: 0}, nil
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if check.Needed {
		t.Fatalf("expected missing SELinux environment to be treated as compliant")
	}
}

func TestUlimitTaskApplyWritesManagedFile(t *testing.T) {
	task := &UlimitTask{
		NodeSpec: testNode(),
		NoFile:   1048576,
		NProc:    1048576,
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		if !strings.Contains(script, "/etc/security/limits.d/99-bootstrapctl.conf") {
			t.Fatalf("expected managed limits file path in script")
		}
		return remote.Result{Output: "CHANGED\n", ExitCode: 0}, nil
	})

	apply, err := task.Apply(context.Background(), exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected ulimit apply to report changed")
	}
}

func TestStorageLayoutTaskCheckAndApply(t *testing.T) {
	task := &StorageLayoutTask{
		NodeSpec:        testNode(),
		GraphRoot:       "/data/graphroot",
		CRIRoot:         "/data/containerd",
		StorageConfPath: "/etc/containers/storage.conf",
		RunRoot:         "/run/containers/storage",
		GraphDriver:     "overlay",
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		if strings.Contains(script, "current_config") {
			if !strings.Contains(script, "/data/graphroot/containers/storage") {
				t.Fatalf("expected containers graphroot path in check script")
			}
			return remote.Result{Output: "CHANGE:graph-root storage-conf\n", ExitCode: 0}, nil
		}
		if strings.Contains(script, "mkdir -p") {
			if !strings.Contains(script, "/etc/containers/storage.conf") {
				t.Fatalf("expected storage.conf path in apply script")
			}
			if !strings.Contains(script, "/data/graphroot/containers/storage") {
				t.Fatalf("expected effective graphroot path in apply script")
			}
			return remote.Result{Output: "CHANGED\n", ExitCode: 0}, nil
		}
		return remote.Result{Output: "unexpected", ExitCode: 1}, nil
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected storage convergence to be needed")
	}

	apply, err := task.Apply(context.Background(), exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected storage apply to report changed")
	}
}

func TestFirewallTaskCheckAndApply(t *testing.T) {
	task := &FirewallTask{
		NodeSpec:        testNode(),
		ManageFirewalld: true,
		ManageUFW:       true,
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		if strings.Contains(script, "systemctl is-active firewalld") {
			return remote.Result{Output: "CHANGE:firewalld(active=active,enabled=enabled) ", ExitCode: 0}, nil
		}
		if strings.Contains(script, "ufw --force disable") {
			return remote.Result{Output: "CHANGED\n", ExitCode: 0}, nil
		}
		return remote.Result{Output: "unexpected", ExitCode: 1}, nil
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected firewall convergence to be needed")
	}

	apply, err := task.Apply(context.Background(), exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected firewall apply to report changed")
	}
}

func TestKernelNetworkTaskCheckAndApply(t *testing.T) {
	task := &KernelNetworkTask{
		NodeSpec: testNode(),
		Modules:  []string{"overlay", "br_netfilter"},
		Sysctls: map[string]string{
			"net.ipv4.ip_forward":                 "1",
			"net.bridge.bridge-nf-call-iptables":  "1",
			"net.bridge.bridge-nf-call-ip6tables": "1",
		},
	}

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		if strings.Contains(script, "/etc/modules-load.d/bootstrapctl-k8s.conf") {
			return remote.Result{Output: "CHANGE:modules-file sysctl-file module:overlay ", ExitCode: 0}, nil
		}
		if strings.Contains(script, "modprobe overlay") {
			if !strings.Contains(script, "/etc/sysctl.d/99-bootstrapctl-k8s.conf") {
				t.Fatalf("expected managed sysctl file path in apply script")
			}
			return remote.Result{Output: "CHANGED\n", ExitCode: 0}, nil
		}
		return remote.Result{Output: "unexpected", ExitCode: 1}, nil
	})

	check, err := task.Check(context.Background(), exec)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !check.Needed {
		t.Fatalf("expected kernel network convergence to be needed")
	}

	apply, err := task.Apply(context.Background(), exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !apply.Changed {
		t.Fatalf("expected kernel network apply to report changed")
	}
}
