# 使用文档

## 适用前提

开始前建议确认：

- 控制端能访问目标节点或跳板机
- 目标节点已开启 SSH
- 你已准备好密码或私钥
- 如果走普通 sudo 用户模式，该用户具备 `sudo -n` 能力

## 命令总览

当前命令如下：

- `init`
- `scan`
- `plan`
- `apply`
- `verify`
- `version`

常用短参数：

- `-i`：`--inventory`
- `-p`：`--profile`
- `-t`：`--timeout`
- `-r`：`--report-dir`

`init` 的常用短参数：

- `-d`：输出目录
- `-c`：环境名
- `-f`：强制覆盖

## 标准使用流程

### 1. 生成模板

```bash
go run ./cmd/bootstrapctl init -d ./demo-init -c demo-env
```

### 2. 填写 inventory

单机最小示例：

```yaml
cluster_name: demo-env

transport:
  ssh_user: root
  ssh_port: 22
  ssh_password: changeme
  use_sudo: false

nodes:
  - name: host-01
    ip: 192.168.1.10
    roles: [single]
```

### 3. 填写 profile

推荐从这份开始：

- [../examples/profile-k8s-host-init.yaml](../examples/profile-k8s-host-init.yaml)

### 4. 先扫描

```bash
go run ./cmd/bootstrapctl scan -i ./demo-init/inventory.yaml -t 20s
```

### 5. 再规划

```bash
go run ./cmd/bootstrapctl plan -i ./demo-init/inventory.yaml -p ./demo-init/profile.yaml -t 20s
```

### 6. 正式执行

```bash
go run ./cmd/bootstrapctl apply -i ./demo-init/inventory.yaml -p ./demo-init/profile.yaml -t 20s
```

### 7. 最终校验

```bash
go run ./cmd/bootstrapctl verify -i ./demo-init/inventory.yaml -p ./demo-init/profile.yaml -t 20s
```

## 典型场景

## 场景一：单机直连初始化

适合公网节点、实验环境或小规模主机。

关键点：

- `transport` 里填写默认 SSH 账号和密码
- `nodes` 里只写一台机器即可
- `cluster_name` 只是环境标签，单机照样能用

## 场景二：通过跳板机访问内网节点

当目标节点没有公网 IP 时，在 `transport.bastion` 或节点级 `bastion` 中配置跳板机。

示例：

```yaml
cluster_name: private-env

transport:
  ssh_user: root
  ssh_port: 22
  ssh_password: changeme

nodes:
  - name: master-01
    ip: 36.137.200.29
    host_ip: 192.168.24.5
    roles: [master]

  - name: worker-01
    ip: 192.168.24.4
    roles: [worker]
    bastion:
      host: 36.137.200.29
```

说明：

- `ip` 是连接入口
- `host_ip` 是主机真正用于互联的 IP
- 节点级 `bastion` 只写了 `host` 时，其余认证信息会自动继承

## 场景三：分发控制端公钥

如果希望当前执行机后续能免密登录目标节点，可以打开：

```yaml
features:
  ssh_authorized_key: true

ssh_key:
  authorized_user: root
  public_key_path: ~/.ssh/id_ed25519.pub
```

也可以直接内联公钥：

```yaml
ssh_key:
  authorized_user: root
  public_key: ssh-ed25519 AAAA... bootstrapctl@example
```

当前版本还支持自动生成控制端专用 key。

如果你开启了 `ssh_authorized_key`，并且没有显式提供 `public_key` 或现成的 `public_key_path`，默认会优先维护：

- `~/.ssh/bootstrapctl_ed25519`

也可以显式指定：

```yaml
ssh_key:
  authorized_user: root
  auto_generate: true
  generated_key_path: ~/.ssh/bootstrapctl_ed25519
```

如果节点声明了 `bastion`，并且 `enable_bastion_hop: true`，工具会继续补齐：

- 跳板机 -> 目标节点 的免密链路

## 场景四：先 root 接入，再切普通 sudo 用户

这是更推荐的企业路径。

### 第一步：创建受控运维账号

使用：

- [../examples/profile.managed-admin.yaml](../examples/profile.managed-admin.yaml)

典型配置：

```yaml
features:
  managed_admin: true
  ssh_authorized_key: true

managed_admin:
  username: bootstrapctlops
  grant_sudo: true
  sudo_nopasswd: true
  install_controller_public_key: true
  disable_root_ssh: false
```

先执行：

```bash
go run ./cmd/bootstrapctl apply -i ./inventory.root.yaml -p ./profile.managed-admin.yaml -t 20s
```

### 第二步：切换 inventory 到普通用户

示例：

```yaml
transport:
  ssh_user: bootstrapctlops
  ssh_private_key: ~/.ssh/id_rsa
  use_sudo: true
```

然后再用普通账号执行完整 profile：

```bash
go run ./cmd/bootstrapctl plan -i ./inventory.sudo.yaml -p ./profile.yaml -t 20s
go run ./cmd/bootstrapctl apply -i ./inventory.sudo.yaml -p ./profile.yaml -t 20s
go run ./cmd/bootstrapctl verify -i ./inventory.sudo.yaml -p ./profile.yaml -t 20s
```

截至 `2026-04-02`，这条路径已经在 4 台真实机器上验证通过。

## profile 关键策略说明

### 防火墙

当前推荐策略是：

- 关闭 `firewalld`
- 关闭 `ufw`
- 统一把规则入口收口到 `iptables`

配置示例：

```yaml
firewall:
  mode: iptables
  manage_firewalld: true
  manage_ufw: true
  require_iptables: true
```

### 内核网络

适合 Kubernetes / CNI 主机场景：

```yaml
kernel_network:
  modules:
    - overlay
    - br_netfilter
  sysctls:
    net.ipv4.ip_forward: "1"
    net.bridge.bridge-nf-call-iptables: "1"
    net.bridge.bridge-nf-call-ip6tables: "1"
```

### 存储布局

当前支持把 graphroot 和容器运行时目录统一落到数据盘：

```yaml
storage:
  graph_root: /data/graphroot
  cri_root: /data/containerd
  storage_conf_path: /etc/containers/storage.conf
  run_root: /run/containers/storage
  graph_driver: overlay
```

### ulimit

`ulimit` 不是简单的开关，而是明确的数值目标：

```yaml
ulimit:
  nofile: 1048576
  nproc: 1048576
```

## 推荐执行顺序

每次建议都按这个顺序：

1. `scan`
2. `plan`
3. `apply`
4. `verify`

不要直接跳过 `scan` 和 `plan`，这样更容易提前发现：

- 跳板机认证问题
- 公钥缺失问题
- `sudo` 权限问题
- 内核或存储状态与预期差异
