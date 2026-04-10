# bootstrapctl

`bootstrapctl` 是一个面向离线、半离线和受限网络环境的企业级主机初始化工具。

它的目标不是继续把所有逻辑堆进一个越来越难维护的 Shell 脚本，而是把主机初始化、基线扫描、跳板机访问、普通 sudo 用户执行、受控运维账号切换这些能力拆成可测试、可演进、可审计的 Go CLI。

当前版本已经提供四类核心能力：

- 项目模板初始化：`init`
- 兼容旧脚本的 inventory 导出：`export-ops-env`
- 基线扫描：`scan`
- 初始化规划与执行：`plan / apply / verify`
- 受控运维账号引导：`managed_admin`

## 适用场景

- Kubernetes / 容器主机初始化
- 单机或多机批量收敛
- 只能通过跳板机访问的内网节点
- 从 `root` 初始接入迁移到普通 sudo 运维账号
- 需要保留 JSON / Markdown 执行报告的企业环境

`cluster_name` 在当前配置里只是“环境名 / 批次名 / 逻辑分组名”，不是必须真的有 Kubernetes 集群。单机也可以正常使用。

## 当前能力

### 初始化任务

- SSH 连通性检查
- 控制端公钥分发到目标节点
- 当前执行节点本机 `~/.ssh/config` 自动维护
- 主节点/跳板机到内网节点的 SSH 免密链路补齐
- 受控运维账号创建
- sudo / NOPASSWD sudo 收敛
- 可选关闭 root 直登
- hostname 收敛
- `/etc/hosts` 受控区块维护
- 关闭 SWAP
- 关闭 SELinux
- 防火墙收口
  - 停用 `firewalld`
  - 停用 `ufw`
  - 保留 `iptables` 作为最终规则入口
- Kubernetes 内核网络条件收敛
- `graphroot` / `cri root` / `containers/storage.conf`
- `ulimit` 具体数值落盘

### 基线扫描

- 操作系统、内核、架构
- CPU、内存
- 根分区与 `/data` 总量、可用量、使用率
- `lsblk` 块设备摘要
- `hostname -I` 全量地址
- 自动推断或显式指定的主 IP
- SWAP / SELinux / firewall / iptables
- 时间同步状态
- 容器运行时 / kubelet
- Kubernetes 内核网络条件

### 输出能力

- 中文终端摘要
- JSON 报告
- Markdown 报告

默认报告目录：

- `.bootstrapctl-reports/`

## 快速开始

### 0. 构建二进制包

推荐先从仓库根目录构建可执行二进制包：

```bash
./build.sh
```

这会在 `dist/linux-amd64/bootstrapctl`（或 `dist/linux-arm64/bootstrapctl`）生成可执行文件。你也可以直接构建单个本地可执行文件：

```bash
go build -trimpath -ldflags "-s -w -X github.com/yuanyp8/bootstrapctl/internal/app.version=dev" -o ./bootstrapctl ./cmd/bootstrapctl
```

后续示例统一使用当前目录中的可执行文件：

```bash
./bootstrapctl ...
```

如果你使用 `build.sh` 生成了 `dist/linux-amd64/bootstrapctl`，可直接复制到当前目录：

```bash
cp dist/linux-amd64/bootstrapctl ./bootstrapctl
chmod +x ./bootstrapctl
```

### 1. 初始化一套模板

```bash
./bootstrapctl init -d ./demo-init -c demo-env
```

会生成：

- `./demo-init/inventory.yaml`
- `./demo-init/profile.yaml`

这两份文件不是“极简占位模板”，而是带完整中文注释的正式起步模板。
设计原则是：

- 用户第一次上手时，直接改 `init` 生成的文件即可
- `init` 生成内容与 [examples/inventory.full.yaml](./examples/inventory.full.yaml)、
  [examples/profile.full.yaml](./examples/profile.full.yaml) 保持同一套结构
- 注释尽量解释“为什么这样配”，而不只是解释字段名

### 2. 先做基线扫描

```bash
./bootstrapctl scan -i ./demo-init/inventory.yaml -t 20s
```

### 2.1 如果旧脚本还依赖 ops-env，可在补齐 inventory 后导出兼容文件

`bootstrapctl init` 现在只生成：

- `inventory.yaml`
- `profile.yaml`

不会默认直接生成 `ops-environment.sh`。原因是用户在 `init` 阶段通常还没有把真实节点、认证信息和跳板机链路补齐；如果这时直接导出，会更像一份带占位值的假配置。

更合理的顺序是：

1. 先 `init`
2. 填写 `inventory.yaml`
3. 再执行 `export-ops-env`

当你需要兼容旧 Shell 工具时，例如 `02-lvm/lvm.sh`，在 inventory 补齐后手动导出一次即可：

```bash
./bootstrapctl export-ops-env -i ./demo-init/inventory.yaml -o ./ops-environment.sh
```

这条命令适合两类场景：

1. 第一次把已补齐的 inventory 导出给旧脚本使用
2. inventory 后续又改过，需要手动刷新兼容文件

典型例子包括：

- `02-lvm/lvm.sh`
- 旧的批量巡检脚本
- 仍然读取 `NODE_NAMES / NODE_IPS` 数组的交付脚本

说明：
- `scan` 当前只依赖 `inventory`
- 为了统一命令习惯，也兼容接收 `-p ./profile.yaml`
- 但 `profile` 在扫描阶段不会参与判断逻辑

### 3. 看规划，不落变更

```bash
./bootstrapctl plan -i ./demo-init/inventory.yaml -p ./demo-init/profile.yaml -t 20s
```

### 4. 正式执行初始化

```bash
./bootstrapctl apply -i ./demo-init/inventory.yaml -p ./demo-init/profile.yaml -t 20s
```

### 5. 做最终校验

```bash
./bootstrapctl verify -i ./demo-init/inventory.yaml -p ./demo-init/profile.yaml -t 20s
```

## 配置模型

`bootstrapctl` 使用两份 YAML：

- `inventory`
  - 描述“目标是谁、如何连接”
- `profile`
  - 描述“最终要收敛成什么状态”

推荐先看这几份示例：

- [examples/inventory.full.yaml](./examples/inventory.full.yaml)
- [examples/profile.full.yaml](./examples/profile.full.yaml)
- [examples/profile-k8s-host-init.yaml](./examples/profile-k8s-host-init.yaml)
- [examples/profile.managed-admin.yaml](./examples/profile.managed-admin.yaml)

如果你只是第一次使用，推荐顺序是：

1. 先执行 `init`
2. 直接修改生成出来的 `inventory.yaml` 和 `profile.yaml`
3. 只有在需要对照完整字段说明时，再回看 `examples/*.full.yaml`

### inventory 的核心字段

- `cluster_name`
- `transport.ssh_user`
- `transport.ssh_password`
- `transport.ssh_private_key`
- `transport.use_sudo`
- `transport.bastion`
- `nodes[].name`
- `nodes[].ip`
- `nodes[].host_ip`
- `nodes[].roles`

`ip` 用于 SSH 连接入口，`host_ip` 用于 `/etc/hosts`、节点互联和主机主 IP 识别。`host_ip` 留空时，工具会自动读取远端 `hostname -I` 的第一个非回环地址。

### profile 的核心字段

- `features`
- `ssh_key`
- `managed_admin`
- `firewall`
- `kernel_network`
- `storage`
- `ulimit`

## 推荐的企业落地路径

### 路径一：直接 root 接入

适合实验环境或临时收敛。

### 路径二：先 root 引导，再切换普通 sudo 运维账号

这是更推荐的企业路径：

1. 用 root 账号完成首轮接入
2. 启用 `managed_admin`
3. 创建普通运维账号，分发控制端公钥
4. 赋予 `sudo -n` 能力
5. 可选关闭 root SSH 直登
6. 后续改用普通 sudo 用户执行 `plan / apply / verify`

## 已验证状态

截至 `2026-04-02`，以下真实路径已经完成验证：

- 公网节点直连
- 通过跳板机访问内网节点
- 跳板机拒绝 `direct-tcpip` 时自动切换 shell-hop 二跳
- 受控运维账号创建
- 普通 sudo 用户执行全量 `k8s-host-init` profile

完整验证说明见：

- [docs/04-testing-and-validation.md](./docs/04-testing-and-validation.md)

## 文档导航

- [文档总览](./docs/README.md)
- [架构文档](./docs/01-architecture.md)
- [使用文档](./docs/02-usage.md)
- [开发文档](./docs/03-development.md)
- [测试与验证文档](./docs/04-testing-and-validation.md)
- [排障文档](./docs/05-troubleshooting.md)

## 当前边界

当前还在持续补齐的能力包括：

- 备份 / 恢复执行器
- 离线 bundle 与 `.run` 交付形态
- 更完整的容器运行时收口
- 更多 OS profile
- 更完整的企业基线扫描项

## 目录结构

```text
bootstrapctl/
├─ cmd/bootstrapctl/          # CLI 入口
├─ internal/app/             # 命令分发与参数解析
├─ internal/config/          # inventory / profile 配置模型
├─ internal/remote/          # SSH 执行器与跳板机逻辑
├─ internal/tasks/           # 初始化任务集合
├─ internal/runner/          # plan/apply/verify 执行引擎
├─ internal/scan/            # 基线扫描器
├─ internal/report/          # JSON / Markdown 报告
├─ internal/scaffold/        # init 模板生成
├─ internal/ui/              # 中文终端输出
├─ examples/                 # 示例配置
└─ docs/                     # 正式文档
```

## 开发与测试

```bash
go test ./...
# 构建单个本地可执行文件，然后查看版本
go build -o ./bootstrapctl ./cmd/bootstrapctl
./bootstrapctl version
```

## 构建与发布

### 本地构建

Linux `amd64` / `arm64` 双架构构建脚本：

- [build.sh](./build.sh)
- [build.ps1](./build.ps1)

示例：

```bash
./build.sh
```

或在 PowerShell 中：

```powershell
.\build.ps1
```

默认会在 `dist/` 下生成：

- `bootstrapctl_<version>_linux_amd64.tar.gz`
- `bootstrapctl_<version>_linux_arm64.tar.gz`
- `checksums.txt`

### GitHub Actions

项目已经附带 GitHub Actions 工作流：

- [CI 工作流](./.github/workflows/ci.yml)
- [Release 工作流](./.github/workflows/release.yml)

行为如下：

- `push / pull_request`
  - 执行 `go test ./...`
  - 构建 Linux `amd64` / `arm64` 二进制
  - 上传 Actions artifacts
- 推送 `v*` 标签
  - 执行测试
  - 构建正式 release 资产
  - 自动创建 GitHub Release 并附带压缩包与校验文件

说明：

当前这些 workflow 是按“`bootstrapctl` 作为独立 GitHub 仓库”组织的。如果你把当前目录单独推到 GitHub 仓库根目录，工作流就可以直接生效。

### 控制端专用 SSH key

当你开启：

```yaml
features:
  ssh_authorized_key: true
```

并且未显式提供 `public_key` 或 `public_key_path` 时，当前版本默认会优先维护一把控制端专用 key：

- `~/.ssh/bootstrapctl_ed25519`

行为是：

- 如果这把 key 已存在，直接复用
- 如果不存在，自动生成 `ed25519` 密钥对
- 再把对应公钥分发到目标节点

这样可以避免把日常使用的 `id_rsa` / `id_ed25519` 和批量初始化用途混在一起。

### 发布到独立 GitHub 仓库

当前 `bootstrapctl` 仍位于 `release` 大仓库中，因此不能直接把这个子目录当成独立 Git 仓库 `git push`。

项目提供了一个发布脚本：

- [publish-bootstrapctl.ps1](./publish-bootstrapctl.ps1)

它会自动完成：

- 检查 `bootstrapctl` 子目录是否有未提交改动
- 执行 `git subtree split`
- 推送到独立 GitHub 仓库

先看 dry-run：

```powershell
.\publish-bootstrapctl.ps1 -DryRun
```

正式推送：

```powershell
.\publish-bootstrapctl.ps1 -SetUpstream
```

更多开发说明见：

- [docs/03-development.md](./docs/03-development.md)

## ops-environment.sh 最新规则

从当前版本开始，`bootstrapctl` 对 `ops-environment.sh` 的处理规则如下：

- `init` 只生成 `inventory.yaml` 和 `profile.yaml` 模板，不会直接生成 `ops-environment.sh`
- `scan`、`plan`、`apply`、`verify` 会自动在 `inventory.yaml` 同一目录下同步一份 `ops-environment.sh`
- 如果内容没有变化，会直接复用已有文件，不会重复改写，因此可以多次幂等执行
- `export-ops-env` 现在保留为手工刷新或单独桥接旧 Shell 脚本的补充命令

这样做的好处是，文件的生成时点更靠近真实执行前，不会把 `init` 阶段的占位模板误当成真实可用的节点清单。
