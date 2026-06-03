# bootstrapctl

`bootstrapctl` 是一个面向离线、半离线和受限网络环境的主机初始化工具。

这版把使用入口收敛成两种模式：

1. **文件模式**：执行 `./bootstrapctl` 生成 `inventory.yaml`，人工修改后执行 `./bootstrapctl apply`。
2. **命令行模式**：不写配置，直接 `./bootstrapctl apply -H ... -u ... -p ...` 一步执行。

其它 `check / scan / plan / verify` 都是可选辅助命令，不是强制流程。

## 最简单用法

### 方式一：生成配置文件再执行

```bash
./bootstrapctl
# 修改 inventory.yaml 里的账号、密码、hostname、ip
./bootstrapctl apply
```

`./bootstrapctl` 等同于：

```bash
./bootstrapctl init
```

生成的 `inventory.yaml` 会把最常改的字段放在最前面：

```yaml
cluster_name: demo-env

transport:
  ssh_user: root
  ssh_password: changeme

nodes:
  - hostname: node-01
    ip: 192.168.1.10
```

你通常只需要改：

- `transport.ssh_user`
- `transport.ssh_password`
- `nodes[].hostname`
- `nodes[].ip`

端口、sudo、ssh_auth、节点级账号、host_ip、跳板机等默认参数都放在模板下半部分的注释里，需要时再打开。

### 方式二：命令行一步执行

单台机器：

```bash
./bootstrapctl apply -H node-01=192.168.1.10 -u root -p 'root密码'
```

多台机器：

```bash
./bootstrapctl apply -H 'master01=10.0.0.1,node01=10.0.0.2' -u root -p 'root密码'
```

也可以先预览：

```bash
./bootstrapctl plan -H node-01=192.168.1.10 -u root -p 'root密码'
```

## 命令说明

主线命令：

```bash
./bootstrapctl          # 生成当前目录 inventory.yaml
./bootstrapctl init     # 同上
./bootstrapctl apply    # 执行初始化，默认读取当前目录 inventory.yaml
```

可选辅助：

```bash
./bootstrapctl check    # 只检查本地配置，不连接远端
./bootstrapctl scan     # 扫描目标主机状态
./bootstrapctl plan     # 预览将要执行的动作，不落变更
./bootstrapctl verify   # 执行后校验
./bootstrapctl version  # 查看版本
```

常用参数：

```text
-H                主机，格式 ip、hostname=ip，或 hostname=ip,hostname2=ip
-u / --user       SSH 用户，默认 root
-p / --password   SSH 密码
--key             SSH 私钥路径；密码登录不用填
--ssh-port        SSH 端口，默认 22；默认端口不用填
--sudo            普通 sudo 用户场景才需要；root 默认不用填
-t                单个 SSH 任务超时时间，例如 -t 20s
-f                init 时覆盖已有 inventory.yaml
--inventory / -i  高级：指定其它 inventory 文件
--profile / -P    高级：指定 profile 文件；不填使用内置默认策略
```

## Windows 交叉编译 Linux 版本

在 Windows PowerShell 项目根目录执行：

```powershell
New-Item -ItemType Directory -Force -Path .\dist | Out-Null

$env:GOOS="linux"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"

go build -trimpath -ldflags="-s -w" -o .\dist\bootstrapctl-linux-amd64 ./cmd/bootstrapctl
```

上传到 Linux 后：

```bash
chmod +x bootstrapctl-linux-amd64
./bootstrapctl-linux-amd64
./bootstrapctl-linux-amd64 apply
```

## 当前默认初始化能力

默认策略面向 Kubernetes / 容器主机初始化，主要包括：

- SSH 连通性检查
- 控制端 SSH 公钥分发
- hostname 收敛
- `/etc/hosts` 受控区块维护
- 关闭 SWAP
- 关闭 SELinux
- 防火墙收口
- Kubernetes 内核网络参数收敛
- 容器数据目录相关配置
- ulimit 配置

需要跳板机、普通 sudo 用户、受控运维账号、细粒度任务开关时，再使用高级参数或 `--profile`。
