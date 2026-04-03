# OneKube K8s Offline Installer

这套脚本用于基于 `sealos` 构建 Kubernetes 离线安装包，并同时支持：

- `amd64`
- `arm64`
- `full` 全离线包
- `lite` 半离线包

当前仓库已经按“单一脚本源 + 构建时按架构出包”的方式整理过，不再维护两套重复的 `amd64/`、`arm64/` 脚本目录，后续维护成本会低很多。

## 设计目标

这次调整主要遵循两条原则：

- 不大改原有部署方案，仍然使用 `sealos run kubernetes-docker helm cilium`
- 把公共变量、公共构建逻辑、公共安装逻辑收敛，减少后续版本升级时的重复修改

所以现在保留的核心行为没有变：

- Kubernetes 组件仍然来自 `kubernetes-docker`
- CNI 仍然使用 `cilium`
- Helm 仍然作为独立组件随包分发
- 安装脚本最终还是通过 `sealos` 完成集群安装或重置

## 当前版本

统一版本文件在：

```bash
common/component-versions.env
```

当前默认版本为：

- `sealos v5.1.1`
- `kubernetes-docker v1.31.11`
- `helm v3.19.2`
- `cilium 1.18.1`

如果后面只需要升级组件版本，优先改这一个文件即可。

## 仓库结构

```text
.
├── .github/workflows/build-k8s-offline.yml
├── build.sh
├── install.sh
├── versions.env
└── common
    ├── build-common.sh
    ├── component-versions.env
    └── install-common.sh
```

各文件职责如下：

- `build.sh`
  - 统一构建入口
  - 给 GitHub Actions 和手工构建共用
- `install.sh`
  - `.run` 安装包头部脚本
  - 支持源码目录直接执行 `show-defaults`
- `versions.env`
  - 提供源码模式下的默认版本展示
- `common/component-versions.env`
  - 统一维护组件版本和镜像 tar 名称
- `common/build-common.sh`
  - 真正的构建逻辑
  - 负责下载 Sealos 二进制、按架构拉镜像、打 `.run` 包
- `common/install-common.sh`
  - 真正的安装逻辑
  - 负责预检查、装二进制、导入镜像、执行 `sealos run/reset`

## 为什么不用两套目录了

之前 `amd64` 和 `arm64` 目录的大部分内容其实是重复的，真正跟架构强相关的只有两类东西：

- `sealos` 及相关工具二进制
- Docker 拉取镜像时的 `--platform`

而镜像名字本身并不需要拆成两套，例如：

- `registry.cn-shanghai.aliyuncs.com/labring/kubernetes-docker:<tag>`
- `registry.cn-shanghai.aliyuncs.com/labring/helm:<tag>`
- `registry.cn-shanghai.aliyuncs.com/labring/cilium:<tag>`

架构差异通过构建阶段处理即可：

```bash
docker pull --platform linux/amd64 ...
docker pull --platform linux/arm64 ...
```

所以现在改成一套源码，按 `--arch` 选择目标架构，这样版本升级、参数调整、安装逻辑修复都只改一处。

## 两种包型说明

### `full`

`full` 表示全离线包，包含：

- `sealos`
- `sealctl`
- `image-cri-shim`
- `lvscare`
- `kubernetes.tar`
- `helm.tar`
- `cilium.tar`

适合目标机器无法访问镜像仓库的场景。

### `lite`

`lite` 表示半离线包，包含：

- `sealos`
- `sealctl`
- `image-cri-shim`
- `lvscare`
- 镜像元信息
- 不包含任何镜像 tar

适合目标机器还能联网拉镜像，或者现场已有私有仓库可用的场景。

## 输出文件

标准构建会产生 4 个安装包：

```text
dist/k8s-sealos-linux-amd64-full.run
dist/k8s-sealos-linux-amd64-lite.run
dist/k8s-sealos-linux-arm64-full.run
dist/k8s-sealos-linux-arm64-lite.run
```

同时每个包都会生成对应校验文件：

```text
dist/*.run.sha256
```

## Sealos 二进制处理方式

现在仓库里不再需要提交这些大文件：

- `amd64/bin/*`
- `arm64/bin/*`

构建时会自动从 Sealos 官方 release 下载对应架构压缩包，然后解压出：

- `sealos`
- `sealctl`
- `image-cri-shim`
- `lvscare`

下载来源：

- [Sealos Releases](https://github.com/labring/sealos/releases)

本地缓存目录：

```bash
.cache/downloads/
.cache/bin/<arch>/
```

这样做的好处是：

- Git 仓库不会再被大二进制污染
- `amd64` / `arm64` 不再需要维护两份 `bin`
- GitHub Actions 可以直接按架构拉取官方二进制构建

## 镜像处理方式

镜像名保持统一，不再因为架构拆目录。

构建时根据目标架构决定拉取平台：

```bash
docker pull --platform linux/amd64 <image>
docker pull --platform linux/arm64 <image>
```

镜像 tar 缓存目录：

```bash
.cache/images/<arch>/
```

其中：

- `full` 包会把 tar 一起打进 `.run`
- `lite` 包不会打 tar，只保留元信息

## 本地构建

先给脚本执行权限：

```bash
chmod +x build.sh install.sh
```

### 只构建一个包

构建 `amd64` 全离线包：

```bash
./build.sh --arch amd64 --bundle full --force
```

构建 `arm64` 半离线包：

```bash
./build.sh --arch arm64 --bundle lite --force
```

### 一次构建全部 4 个包

```bash
./build.sh --arch all --bundle all --force
```

### 只重打包，不重新下载二进制和镜像

```bash
./build.sh --arch amd64 --bundle full --skip-binary-download --skip-image-prepare
```

### 使用本地 Sealos 安装包构建

如果构建机访问 GitHub 很慢，可以直接复用本地已经准备好的 Sealos 压缩包：

```bash
./build.sh \
  --arch amd64 \
  --bundle full \
  --sealos-archive-file /data/pkg/sealos_5.1.1_linux_amd64.tar.gz \
  --force
```

如果一个目录里已经放好了不同架构的压缩包：

```bash
./build.sh \
  --arch all \
  --bundle lite \
  --sealos-archive-dir /data/pkg/sealos \
  --force
```

目录里的文件名需要保持为：

```text
sealos_5.1.1_linux_amd64.tar.gz
sealos_5.1.1_linux_arm64.tar.gz
```

### 使用自定义下载地址构建

如果你已经把 Sealos 压缩包放到了内网 HTTP/HTTPS 地址，可以直接指定：

```bash
./build.sh \
  --arch amd64 \
  --bundle full \
  --sealos-archive-url http://your-mirror/sealos_5.1.1_linux_amd64.tar.gz \
  --force
```

或者给一个统一前缀地址：

```bash
./build.sh \
  --arch all \
  --bundle lite \
  --sealos-download-base http://your-mirror/sealos \
  --force
```

这时脚本会自动拼接：

```text
http://your-mirror/sealos/sealos_5.1.1_linux_amd64.tar.gz
http://your-mirror/sealos/sealos_5.1.1_linux_arm64.tar.gz
```

参数说明：

- `--arch <amd64|arm64|all>`
  - 选择目标架构
- `--bundle <full|lite|all>`
  - 选择包型
- `--skip-binary-download`
  - 跳过 Sealos 二进制下载，直接复用缓存
- `--skip-image-prepare`
  - 跳过镜像拉取和保存，直接复用缓存
- `--sealos-archive-file <path>`
  - 指定单个本地 Sealos 压缩包
- `--sealos-archive-dir <dir>`
  - 指定本地 Sealos 压缩包目录
- `--sealos-archive-url <url>`
  - 指定单个 Sealos 压缩包下载地址
- `--sealos-download-base <url>`
  - 指定 Sealos 压缩包统一下载前缀
- `--force`
  - 强制重新准备缓存
- `--clean`
  - 清理 `.build` 和 `dist`

## 默认值查看

源码目录下可直接查看默认配置：

```bash
./install.sh show-defaults
```

查看帮助时：

```bash
./install.sh -h
./k8s-sealos-linux-amd64-full.run -h
```

现在都会走快速帮助路径，不会先解压整个 `.run` 包。

查看指定架构的默认值也可以：

```bash
./install.sh show-defaults --arch arm64
```

对已构建好的安装包同样适用：

```bash
./k8s-sealos-linux-amd64-full.run show-defaults
```

## 安装包使用方式

### 安装前预检查

```bash
./k8s-sealos-linux-amd64-full.run precheck \
  --masters 10.0.0.11,10.0.0.12,10.0.0.13 \
  --nodes 10.0.0.21,10.0.0.22 \
  --passwd 'your-password' \
  --yes
```

### 安装集群

```bash
./k8s-sealos-linux-amd64-full.run install \
  --masters 10.0.0.11,10.0.0.12,10.0.0.13 \
  --nodes 10.0.0.21,10.0.0.22 \
  --passwd 'your-password' \
  --yes
```

### 使用私钥安装

```bash
./k8s-sealos-linux-amd64-full.run install \
  --masters 10.0.0.11,10.0.0.12,10.0.0.13 \
  --nodes 10.0.0.21,10.0.0.22 \
  --user root \
  --pk /root/.ssh/id_rsa \
  --yes
```

### 重置集群

```bash
./k8s-sealos-linux-amd64-full.run reset \
  --masters 10.0.0.11,10.0.0.12,10.0.0.13 \
  --nodes 10.0.0.21,10.0.0.22 \
  --passwd 'your-password' \
  --yes
```

## 常用安装参数

- `--masters`
  - 必填，主节点 IP 列表，逗号分隔
- `--nodes`
  - 可选，工作节点 IP 列表，逗号分隔
- `--passwd`
  - SSH 密码
- `--user`
  - SSH 用户名
- `--pk`
  - SSH 私钥路径
- `--pk-passwd`
  - SSH 私钥口令
- `--port`
  - SSH 端口，默认 `22`
- `--data-root`
  - Sealos 数据目录，默认 `/data`
- `--cri-data`
  - 容器运行时数据目录，默认 `/data/containerd`
- `--cni-helm-opts`
  - 额外传给 Cilium 的 `ExtraValues` 值
- `--registry`
  - 覆盖默认镜像仓库前缀
- `--k8s-version`
  - 临时覆盖 Kubernetes 镜像 tag
- `--helm-version`
  - 临时覆盖 Helm 镜像 tag
- `--cni-version`
  - 临时覆盖 Cilium 镜像 tag
- `--skip-image-load`
  - 跳过本地导入镜像 tar
- `--skip-binary-install`
  - 跳过安装 Sealos 二进制到 `/usr/local/bin`
- `--skip-precheck`
  - 跳过本地预检查
- `--dry-run`
  - 只打印最终的 `sealos` 命令，不实际执行
- `--debug`
  - 打开 shell trace，并向 `sealos` 传递 `--debug`
- `--`
  - 将后续参数原样透传给 `sealos`

## SSH 认证说明

这套安装脚本是在“当前执行节点”上调用 `sealos` 去 SSH 连接所有目标节点。

这意味着：

- 不是你电脑能 SSH 上去就够了
- 必须是你当前执行 `.run` 的这台机器，能够认证到所有 `masters/nodes`

常见可用方式有两种：

- 密码认证
  - 传 `--passwd`
- 私钥认证
  - 传 `--user` 和 `--pk`
  - 或者确保当前机器默认私钥存在于 `~/.ssh/id_rsa`

如果你看到类似下面的报错：

```text
ssh: handshake failed: ssh: unable to authenticate, attempted methods [none]
```

通常不是网络不通，而是当前执行节点没有把正确的 SSH 认证方式传给 `sealos`。

## Cilium 1.18 默认参数说明

当前默认版本恢复为 `Cilium 1.18.1`。

结合现场验证，当前这套 `sealos run kubernetes-docker helm cilium` 方案里，对 `labring/cilium:1.18.1` 生效的参数形式是：

```bash
-e ExtraValues="kubeProxyReplacement=false"
```

而不是：

```bash
-e ExtraValues="--set kubeProxyReplacement=false"
```

也不再默认传：

```bash
-e HELM_OPTS=...
```

因此现在原版安装脚本的默认行为是：

- 默认直接使用 `1.18.1`
- 默认自动附加 `ExtraValues=kubeProxyReplacement=false`
- 默认不再附加 `HELM_OPTS`

如果你要手工覆盖，也建议按这个格式传：

```bash
-- -e ExtraValues="kubeProxyReplacement=false"
```

## 运行后的环境文件

安装脚本会生成：

```bash
/etc/k8s-sealos/cluster.env
```

里面会记录：

- 当前包的架构
- 当前包型
- 组件版本
- data root
- cri data
- masters / nodes
- sealos 运行相关环境变量

后续排查或二次执行时可以直接：

```bash
source /etc/k8s-sealos/cluster.env
```

## GitHub Actions

工作流文件：

```bash
.github/workflows/build-k8s-offline.yml
```

默认会构建 4 个组合：

- `amd64/full`
- `amd64/lite`
- `arm64/full`
- `arm64/lite`

行为说明：

- `push` 到 `main/master`
  - 自动构建 4 种安装包并上传 artifact
- `pull_request`
  - 自动做同样的构建校验
- `workflow_dispatch`
  - 支持手工触发构建
- `tag v*`
  - 在构建完成后自动创建 GitHub Release，并上传所有 `.run` 和 `.sha256`

GitHub Actions 里已经按现在的设计处理了两件关键事：

- 不依赖仓库内置 `bin`
- 构建时按架构下载 Sealos release 二进制

也就是说，GitHub 仓库只保留源码，真正出包时再动态拉取对应架构内容。

## 维护建议

后面如果你继续维护这套脚本，通常只需要关注这几个入口：

1. 升级组件版本
   - 修改 `common/component-versions.env`
2. 调整构建逻辑
   - 修改 `common/build-common.sh`
3. 调整安装逻辑
   - 修改 `common/install-common.sh`
4. 调整 CI/CD
   - 修改 `.github/workflows/build-k8s-offline.yml`

## 注意事项

- `full` 包构建依赖 Docker，因为需要拉取并导出镜像 tar
- `lite` 包不包含镜像 tar，目标机器需要能访问镜像来源，或者你手工改成自己的可达仓库
- 不建议提交 `.cache`、`.build`、`dist` 到 Git 仓库
- 如需进一步升级版本，建议优先验证上游镜像 tag 是否真实存在，再更新统一版本文件
