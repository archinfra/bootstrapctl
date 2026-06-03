package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InitOptions 描述 bootstrapctl init 命令的模板输出参数。
type InitOptions struct {
	Dir         string
	ClusterName string
	Inventory   string
	Profile     string
	Force       bool
	Advanced    bool
}

// InitResult 返回实际生成的模板路径，便于 CLI 输出下一步提示。
type InitResult struct {
	InventoryPath string
	ProfilePath   string
}

// WriteTemplates 根据用户指定目录生成模板。
// 默认只生成最小 inventory，profile 改为内置默认值；
// 当用户显式使用 Advanced 时，再生成完整 inventory/profile。
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
		if options.Advanced {
			if _, err := os.Stat(profilePath); err == nil {
				return InitResult{}, fmt.Errorf("profile 模板已存在: %s", profilePath)
			}
		}
	}

	inventoryContent := renderSimpleInventory(options.ClusterName)
	if options.Advanced {
		inventoryContent = renderInventoryFull(options.ClusterName)
	}
	if err := os.WriteFile(inventoryPath, []byte(inventoryContent), 0o644); err != nil {
		return InitResult{}, fmt.Errorf("写入 inventory 模板失败: %w", err)
	}

	result := InitResult{InventoryPath: inventoryPath}
	if options.Advanced {
		if err := os.WriteFile(profilePath, []byte(renderProfile()), 0o644); err != nil {
			return InitResult{}, fmt.Errorf("写入 profile 模板失败: %w", err)
		}
		result.ProfilePath = profilePath
	}

	return result, nil
}

func renderSimpleInventory(clusterName string) string {
	template := `# bootstrapctl 配置文件
#
# 使用方式：
#   1. 先改最上面的账号、密码、hostname、ip
#   2. 然后执行：./bootstrapctl apply
#
# 常规客户只需要改【常改区】；下面的【默认区】只是说明，通常不用动。

# =========================
# 常改区：优先只改这里
# =========================

# 环境名 / 批次名，随便起一个好识别的名字即可。
cluster_name: __CLUSTER_NAME__

# SSH 登录信息。默认用 root；只需要把密码改成真实密码。
transport:
  ssh_user: root
  ssh_password: changeme

# 目标主机。hostname 是你希望设置/识别的主机名，ip 是 SSH 连接地址。
nodes:
  - hostname: node-01
    ip: 192.168.1.10

  # 多台机器就继续往下加：
  # - hostname: node-02
  #   ip: 192.168.1.11

# =========================
# 默认区：一般不用改，需要时再取消注释
# =========================

# SSH 默认值：
# transport:
#   ssh_port: 22              # 默认 22，不用写
#   ssh_private_key: ""       # 使用私钥登录时再填
#   ssh_auth: yes             # 兼容旧脚本的免密开关，默认 yes
#   use_sudo: false           # root 登录默认 false；普通 sudo 用户才改 true
#
# 节点高级字段：
# nodes:
#   - hostname: node-01
#     ip: 192.168.1.10
#     host_ip: ""             # 内网主 IP；留空会自动探测
#     ssh_user: ""            # 节点级覆盖；留空继承 transport.ssh_user
#     ssh_password: ""        # 节点级覆盖；留空继承 transport.ssh_password
#     ssh_port: 22             # 节点级覆盖；默认 22
#     ssh_private_key: ""      # 节点级私钥
#     use_sudo: false          # 节点级 sudo 开关
#
# 跳板机场景才需要：
# transport:
#   bastion:
#     host: 1.2.3.4
#     ssh_user: root
#     ssh_port: 22
#     ssh_password: changeme
#     ssh_private_key: ""
`
	return strings.ReplaceAll(template, "__CLUSTER_NAME__", clusterName)
}

func renderInventoryFull(clusterName string) string {
	template := `# bootstrapctl inventory 完整模板
#
# inventory 负责回答两个问题：
# 1. 目标机器是谁
# 2. 控制端应该如何连接到它们
#
# 这份模板默认既能覆盖单机，也能覆盖多机和跳板机场景。
# 如果你只是做单机初始化，保留一个节点即可；其余节点可以直接删除。
cluster_name: __CLUSTER_NAME__

transport:
  # 默认 SSH 用户。
  # 如果节点级字段留空，会继承这里的值。
  ssh_user: root

  # 默认 SSH 端口。
  ssh_port: 22

  # 默认 SSH 密码。
  # 如果你使用私钥登录，可以把它留空。
  ssh_password: changeme

  # 默认 SSH 私钥路径。
  # 留空表示优先使用密码。
  ssh_private_key: ""

  # 兼容旧脚本的 SSH 密钥认证/免密准备开关。
  # 这里默认写 yes，而不是 true，贴近 ops-environment.sh 的使用习惯。
  ssh_auth: yes

  # 是否在远端命令执行时统一使用 sudo。
  # 典型场景是：
  # - ssh_user 不是 root
  # - 该用户具备 sudo -n 权限
  use_sudo: false

  # 全局跳板机配置。
  # 如果目标节点只能通过堡垒机访问，可以在这里统一定义。
  # 节点级 bastion 留空时，会继承这里的配置。
  bastion:
    host: ""
    ssh_user: root
    ssh_port: 22
    ssh_password: ""
    ssh_private_key: ""

nodes:
  - hostname: node-01
    # 也兼容旧字段 name: node-01。
    # ip 用于 SSH 连接入口。
    # 对公网节点，这里通常填公网 IP。
    ip: 192.168.24.5

    # host_ip 用于：
    # - /etc/hosts 受控区块
    # - 节点互联
    # - 主机主 IP 标识
    #
    # 如果留空，bootstrapctl 会在远端执行 hostname -I，
    # 并选择第一个非回环地址。
    host_ip: ""

    # roles 是高级兼容字段；普通客户不用填。
    # 需要对接旧脚本或做逻辑标签时再启用。
    roles: [single]

    # 节点级 SSH 参数。
    # 留空时会继承 transport.*。
    ssh_user: root
    ssh_port: 22
    ssh_password: ""
    ssh_private_key: ""
    ssh_auth: yes
    use_sudo: false

    # 节点级跳板机。
    # 如果你只想让某台节点单独走跳板机，可以在这里覆盖。
    #
    # 只填 host 也可以，其余认证参数会按以下顺序自动继承：
    # 1. transport.bastion.*
    # 2. transport.*
    bastion:
      host: ""
      ssh_user: root
      ssh_port: 22
      ssh_password: ""
      ssh_private_key: ""

  - hostname: node-02
    ip: 192.168.24.6
    host_ip: ""
    # roles 是高级兼容字段；普通客户不用填。
    roles: [worker]
    ssh_user: root
    ssh_port: 22
    ssh_password: ""
    ssh_private_key: ""
    ssh_auth: yes
    use_sudo: false
    bastion:
      host: ""
      ssh_user: root
      ssh_port: 22
      ssh_password: ""
      ssh_private_key: ""
`
	return strings.ReplaceAll(template, "__CLUSTER_NAME__", clusterName)
}

func renderProfile() string {
	return `# bootstrapctl profile 完整模板
#
# profile 描述“这批主机最终要收敛成什么状态”。
# 这份模板默认面向 Kubernetes / 容器主机初始化场景，
# 但也可以裁剪成普通业务主机的初始化模板。
name: k8s-host-init

features:
  # 是否检查 SSH 连通性。
  # 一般保持开启，便于在 plan/apply 前先发现连通性问题。
  ssh_connectivity: true

  # 是否把“当前执行机”的 SSH 公钥分发到目标节点。
  # 这里仅负责：
  # 1. 控制端 -> 目标节点
  #
  # 注意：
  # - bastion -> 内网节点 这条链路，已经独立由 enable_bastion_hop 控制
  # - 所以即使这里关闭，像 master1 -> node1 这种主节点到私网节点的免密仍可单独生效
  ssh_authorized_key: yes

  # 是否启用“受控运维账号”能力。
  # 开启后可以：
  # - 创建一个新的普通运维账号
  # - 配置 sudo
  # - 配置 sudo -n / sudo su -
  # - 可选关闭 root SSH 直登
  managed_admin: false

  # 是否收敛 hostname。
  hostname: true

  # 是否维护 /etc/hosts 受控区块。
  hosts_file: true

  # 是否关闭 swap。
  disable_swap: true

  # 是否关闭 SELinux。
  disable_selinux: true

  # 是否收敛防火墙。
  firewall: true

  # 是否收敛 Kubernetes 相关内核网络参数。
  kernel_network: true

  # 是否收敛容器 graphroot / cri root / storage.conf。
  storage: true

ssh_key:
  # 公钥要写入哪个远端用户。
  # 一般保持 root；如果你使用普通运维账号，也可以改成该账号。
  authorized_user: root

  # 控制端现成公钥路径。
  # 留空时：
  # 1. 如果 auto_generate=true，会优先使用 generated_key_path 对应的专用密钥
  # 2. 如果 auto_generate=false，会尝试 ~/.ssh/id_ed25519.pub 和 ~/.ssh/id_rsa.pub
  public_key_path: ""

  # 如需直接内联公钥，可填这里，优先级高于 public_key_path。
  public_key: ""

  # 是否在控制端自动生成一把专用 SSH key。
  # 推荐开启，这样不会污染你日常登录用的默认密钥。
  auto_generate: true

  # 控制端专用密钥对路径。
  # 当 auto_generate=true 时，会优先维护这把 key。
  generated_key_path: ~/.ssh/bootstrapctl_ed25519

  # 是否在“当前执行节点”本机维护 SSH 客户端配置。
  # 开启后，执行节点上可直接使用：
  # - ssh node-01
  # - ssh 192.168.x.x
  # 而不需要每次手工追加 -i ~/.ssh/bootstrapctl_ed25519
  manage_controller_ssh_config: true

  # 当前执行节点本机的 SSH 客户端配置文件路径。
  controller_ssh_config_path: ~/.ssh/config

  # 是否补齐 bastion -> 内网目标节点 的免密链路。
  # 这更接近“主节点/跳板机 到 私网节点”的互信配置，
  # 与“控制端是否免密登录所有节点”不是一回事。
  enable_bastion_hop: true

  # bastion 上专用 key 的路径。
  # 如果跳板机上不存在这把 key，工具会自动生成。
  bastion_key_path: ~/.ssh/bootstrapctl_ed25519

  # 是否在跳板机上顺手维护 SSH 客户端配置。
  # 开启后，跳板机上可直接执行 ssh 192.168.x.x，
  # 不需要再手动追加 -i ~/.ssh/bootstrapctl_ed25519。
  manage_bastion_ssh_config: true

  # 跳板机上的 SSH 客户端配置文件路径。
  bastion_ssh_config_path: ~/.ssh/config

managed_admin:
  # 新运维账号的用户名。
  username: opsadmin

  # 明文密码。
  # 与 password_hash 二选一；如果都为空，则主要依赖 SSH 公钥登录。
  password: ""

  # 已加密的 shadow hash。
  password_hash: ""

  # 登录 shell。
  shell: /bin/bash

  # 主组。
  # 留空时默认使用与用户名同名的组。
  primary_group: ""

  # 附加组。
  extra_groups:
    - sudo
    - wheel

  # 是否创建 home 目录。
  create_home: true

  # 是否授予 sudo 能力。
  grant_sudo: true

  # 是否写入 NOPASSWD sudo 规则。
  # 开启后，该账号可无密码执行 sudo -n 和 sudo su -。
  sudo_nopasswd: true

  # 是否给新运维账号安装控制端公钥。
  install_controller_public_key: true

  # 指定运维账号要安装的控制端公钥路径。
  # 留空时会优先复用 ssh_key 解析出的控制端公钥。
  controller_public_key_path: ""

  # 也可以直接内联公钥。
  controller_public_key: ""

  # 是否关闭 root SSH 直登。
  disable_root_ssh: true

  # sshd 主配置文件路径。
  sshd_config_path: /etc/ssh/sshd_config

firewall:
  # 推荐策略：
  # - firewalld / ufw 全部停用并禁用
  # - 最终规则入口统一收口到 iptables
  mode: iptables
  manage_firewalld: true
  manage_ufw: true
  require_iptables: true

kernel_network:
  # Kubernetes / 容器场景常用内核模块。
  modules:
    - overlay
    - br_netfilter

  # Kubernetes / CNI 常用 sysctl。
  sysctls:
    net.ipv4.ip_forward: "1"
    net.bridge.bridge-nf-call-iptables: "1"
    net.bridge.bridge-nf-call-ip6tables: "1"

storage:
  # graphroot 目录。
  graph_root: /data/graphroot

  # 容器运行时数据目录。
  cri_root: /data/containerd

  # containers/storage.conf 路径。
  storage_conf_path: /etc/containers/storage.conf

  # runroot 路径。
  run_root: /run/containers/storage

  # 存储驱动。
  graph_driver: overlay

ulimit:
  # 文件句柄数上限。
  nofile: 1048576

  # 进程数上限。
  nproc: 1048576
`
}
