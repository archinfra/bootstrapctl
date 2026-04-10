# 使用文档

## 适用前提

开始前建议确认：

- 控制端能够访问目标节点或跳板机
- 目标节点已开启 SSH
- 你已经准备好密码或私钥
- 如果走普通 `sudo` 用户模式，该用户具备 `sudo -n` 能力

## 命令总览

当前命令如下：

- `init`
- `export-ops-env`
- `scan`
- `plan`
- `apply`
- `verify`
- `version`

常用短参数：

- `-i` / `--inventory`
- `-p` / `--profile`
- `-t` / `--timeout`
- `-r` / `--report-dir`

`init` 的常用短参数：

- `-d`：输出目录
- `-c`：环境名
- `-f`：强制覆盖

## 标准使用流程

### 0. 构建可执行文件

如果你已经下载了仓库并希望使用发布形态，推荐先构建本地二进制：

```bash
./build.sh
```

完成后可以直接使用构建出的可执行文件：

```bash
cp dist/linux-amd64/bootstrapctl ./bootstrapctl
chmod +x ./bootstrapctl
./bootstrapctl init -d ./demo-init -c demo-env
```

如果你只是想在开发环境中快速验证，也可以直接使用 `go run`，但正式使用场景推荐优先使用二进制包。

### 1. 生成模板

```bash
./bootstrapctl init -d ./demo-init -c demo-env
```

`init` 生成的是完整体模板，不是简化版占位文件：

- `inventory.yaml` 会包含连接方式、`host_ip`、跳板机覆盖等详细注释
- `profile.yaml` 会包含 SSH 公钥、managed admin、防火墙、内核网络、存储、ulimit 等完整字段说明
- 你通常只需要直接改这两份生成文件，不需要再手抄示例

### 2. 填写 inventory

单机最小示例如下：

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

推荐优先使用 `init` 生成出来的 `profile.yaml`。

如果需要查看完整字段总览或做交叉对照，再看：

- [../examples/profile.full.yaml](../examples/profile.full.yaml)
- [../examples/profile-k8s-host-init.yaml](../examples/profile-k8s-host-init.yaml)

### 4. 先扫描

```bash
./bootstrapctl scan -i ./demo-init/inventory.yaml -t 20s
```

### 5. 再规划

```bash
./bootstrapctl plan -i ./demo-init/inventory.yaml -p ./demo-init/profile.yaml -t 20s
```

### 6. 正式执行

```bash
./bootstrapctl apply -i ./demo-init/inventory.yaml -p ./demo-init/profile.yaml -t 20s
```

### 7. 最终校验

```bash
./bootstrapctl verify -i ./demo-init/inventory.yaml -p ./demo-init/profile.yaml -t 20s
```

## ops-environment.sh 自动同步规则

从当前版本开始，推荐把 `ops-environment.sh` 当成 `inventory.yaml` 的兼容派生产物来理解：

- `init` 只生成模板，不直接生成 `ops-environment.sh`
- `scan`、`plan`、`apply`、`verify` 会自动在 `inventory.yaml` 同目录下同步一份 `ops-environment.sh`
- 如果 `inventory` 没有变化，再次执行这些命令时会识别为“已是最新状态”，不会重复改写
- `export-ops-env` 仍然可用，但主要用于手工刷新或单独桥接旧脚本

这样用户只需要维护真实的 `inventory.yaml`，旧的 LVM 或巡检脚本继续消费 `ops-environment.sh` 即可。

## 典型场景

### 场景一：单机直连初始化

适合公网节点、实验环境或小规模主机。

关键点：

- `transport` 里填写默认 SSH 账号和密码
- `nodes` 里只写一台机器即可
- `cluster_name` 只是环境标签，单机照样能用

### 场景二：通过跳板机访问内网节点

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
- 节点级 `bastion` 只写 `host` 时，其余认证信息会自动继承

### 场景三：分发控制端公钥

如果希望当前执行机后续能免密登录目标节点，可以打开：

```yaml
features:
  ssh_authorized_key: true
```

并配置：

```yaml
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

如果未显式提供控制端公钥，当前版本也支持自动生成控制端专用 key，例如：

```yaml
ssh_key:
  authorized_user: root
  auto_generate: true
  generated_key_path: ~/.ssh/bootstrapctl_ed25519
```

如果节点声明了 `bastion`，并且开启了 bastion hop，工具还会继续补齐：

- 跳板机 -> 目标节点 的免密链路

### 场景四：先用 root 接入，再切换普通 sudo 用户

这是更推荐的企业路径。

第一步先创建受控运维账号，可参考：

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
./bootstrapctl apply -i ./inventory.root.yaml -p ./profile.managed-admin.yaml -t 20s
```

第二步把 inventory 切换为普通用户，例如：

```yaml
transport:
  ssh_user: bootstrapctlops
  ssh_private_key: ~/.ssh/id_rsa
  use_sudo: true
```

再用普通账号执行完整 profile：

```bash
./bootstrapctl plan -i ./inventory.sudo.yaml -p ./profile.yaml -t 20s
./bootstrapctl apply -i ./inventory.sudo.yaml -p ./profile.yaml -t 20s
./bootstrapctl verify -i ./inventory.sudo.yaml -p ./profile.yaml -t 20s
```

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
