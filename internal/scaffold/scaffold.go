package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
)

// InitOptions 描述 bootstrapctl init 命令的模板输出参数。
type InitOptions struct {
	Dir         string
	ClusterName string
	Inventory   string
	Profile     string
	Force       bool
}

// InitResult 返回实际生成的模板路径，便于 CLI 输出下一步提示。
type InitResult struct {
	InventoryPath string
	ProfilePath   string
}

// WriteTemplates 根据用户指定目录生成 inventory/profile 模板。
func WriteTemplates(options InitOptions) (InitResult, error) {
	inventoryPath := filepath.Join(options.Dir, options.Inventory)
	profilePath := filepath.Join(options.Dir, options.Profile)

	if err := os.MkdirAll(options.Dir, 0o755); err != nil {
		return InitResult{}, fmt.Errorf("创建模板目录失败: %w", err)
	}

	if !options.Force {
		if _, err := os.Stat(inventoryPath); err == nil {
			return InitResult{}, fmt.Errorf("inventory 模板已存在: %s", inventoryPath)
		}
		if _, err := os.Stat(profilePath); err == nil {
			return InitResult{}, fmt.Errorf("profile 模板已存在: %s", profilePath)
		}
	}

	if err := os.WriteFile(inventoryPath, []byte(renderInventory(options.ClusterName)), 0o644); err != nil {
		return InitResult{}, fmt.Errorf("写入 inventory 模板失败: %w", err)
	}
	if err := os.WriteFile(profilePath, []byte(renderProfile()), 0o644); err != nil {
		return InitResult{}, fmt.Errorf("写入 profile 模板失败: %w", err)
	}

	return InitResult{
		InventoryPath: inventoryPath,
		ProfilePath:   profilePath,
	}, nil
}

func renderInventory(clusterName string) string {
	return fmt.Sprintf(`# bootstrapctl inventory template
#
# inventory 回答两个问题：
# 1. 目标机器是谁
# 2. 控制端应该如何连接到它们
cluster_name: %s

transport:
  ssh_user: root
  ssh_port: 22
  ssh_password: changeme
  ssh_private_key: ""
  use_sudo: false

  # 如目标节点不能被控制端直连，可在这里配置全局跳板机。
  # 节点级 bastion 留空时，会继承这里的配置。
  bastion:
    host: ""
    ssh_user: root
    ssh_port: 22
    ssh_password: ""
    ssh_private_key: ""

nodes:
  - name: node-01
    ip: 192.168.24.5
    # host_ip 用于 /etc/hosts、节点互联和主 IP 标识。
    # 留空时，bootstrapctl 会在远端执行 hostname -I，并取第一个非回环地址。
    host_ip: ""
    roles: [single]

    # 节点级字段留空时，会继承 transport.* 的默认值。
    ssh_user: root
    ssh_port: 22
    ssh_password: ""
    ssh_private_key: ""
    use_sudo: false

    # 如需按节点单独指定跳板机，可在此覆盖。
    # 只填写 host 也可以，其余认证字段会自动继承：
    # 1. transport.bastion.*
    # 2. transport.*
    bastion:
      host: ""
      ssh_user: root
      ssh_port: 22
      ssh_password: ""
      ssh_private_key: ""
`, clusterName)
}

func renderProfile() string {
	return `# bootstrapctl profile template
#
# profile 负责描述“这批主机最终要收敛成什么状态”。
# 这份模板默认面向 Kubernetes / 容器主机初始化场景。
name: k8s-host-init

features:
  # 开启后会做两类免密动作：
  # 1. 控制端 -> 节点
  # 2. 如节点配置了 bastion，则额外补齐 bastion -> 目标节点
  ssh_authorized_key: false

  # 开启后会新建一个受控运维账号：
  # - 配置 sudo 权限
  # - 允许 sudo su - 到 root 不再输入密码
  # - 可选关闭直接 root SSH 登录
  managed_admin: false

ssh_key:
  authorized_user: root
  # 留空时会自动尝试控制端默认公钥：
  # - ~/.ssh/id_ed25519.pub
  # - ~/.ssh/id_rsa.pub
  public_key_path: ""
  public_key: ""
  enable_bastion_hop: true
  # bastion_key_path 是在跳板机上生成/复用的私钥路径。
  bastion_key_path: ~/.ssh/bootstrapctl_ed25519

managed_admin:
  username: opsadmin
  password: ""
  password_hash: ""
  shell: /bin/bash
  primary_group: ""
  extra_groups:
    - sudo
    - wheel
  create_home: true
  grant_sudo: true
  sudo_nopasswd: true
  install_controller_public_key: true
  controller_public_key_path: ""
  controller_public_key: ""
  disable_root_ssh: true
  sshd_config_path: /etc/ssh/sshd_config

firewall:
  # 当前推荐策略：
  # - firewalld / ufw 全部停用并禁用
  # - 最终规则入口统一收口到 iptables
  mode: iptables
  manage_firewalld: true
  manage_ufw: true
  require_iptables: true

kernel_network:
  modules:
    - overlay
    - br_netfilter
  sysctls:
    net.ipv4.ip_forward: "1"
    net.bridge.bridge-nf-call-iptables: "1"
    net.bridge.bridge-nf-call-ip6tables: "1"

storage:
  graph_root: /data/graphroot
  cri_root: /data/containerd
  storage_conf_path: /etc/containers/storage.conf
  run_root: /run/containers/storage
  graph_driver: overlay

ulimit:
  nofile: 1048576
  nproc: 1048576
`
}
