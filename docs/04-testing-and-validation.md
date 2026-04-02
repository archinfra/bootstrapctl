# 测试与验证文档

## 目标

这份文档记录两件事：

- 当前有哪些自动化测试
- 当前有哪些真实机器验证已经完成

它的目的不是写成流水账，而是明确告诉使用者：

- 哪些能力已经可信
- 哪些能力还处在持续补齐阶段

## 自动化测试

当前已经覆盖的测试方向包括：

- 配置默认值与显式开关
- bastion 继承规则
- SSH 公钥解析
- 模板生成
- 任务装配数量
- 任务状态解析
- 远程执行器 shell-hop 兜底
- 扫描结果汇总
- Markdown / JSON 报告生成

建议本地执行：

```bash
go test ./...
```

## 真机验证矩阵

截至 `2026-04-02`，已完成如下验证。

### AMD 公网节点

- `36.137.200.29`

### AMD 内网节点

- `192.168.24.4`
- 经 `36.137.200.29` 跳板访问

### ARM 公网节点

- `36.137.200.45`

### ARM 内网节点

- `192.168.10.3`
- 经 `36.137.200.45` 跳板访问

其中 `192.168.10.3` 这条链路曾因 bastion 拒绝 `direct-tcpip` 报错，后续已通过 shell-hop 兜底打通。

## 已完成验证的关键能力

### 1. 基线扫描

已验证：

- 公网直连扫描
- 跳板机场景扫描
- 生成 JSON 报告
- 生成 Markdown 报告

### 2. 受控运维账号引导

已验证：

- 创建普通账号
- 授予 sudo
- 授予 `sudo -n` 能力
- 分发控制端公钥
- 可选关闭 root SSH 直登

### 3. 普通 sudo 用户全量执行

这是当前最重要的验证结果之一。

已在真实 4 节点上完成以下闭环：

1. 用 root 接入并创建受控运维账号
2. 切换 inventory 到普通 sudo 用户
3. 执行全量 `k8s-host-init` profile
4. 完成 `scan -> plan -> apply -> verify`

实际覆盖的任务包括：

- `ssh-connectivity`
- `ssh-authorized-key`
- `ssh-bastion-hop-key`
- `hostname`
- `hosts-file`
- `disable-swap`
- `disable-selinux`
- `firewall`
- `kernel-network`
- `storage-layout`
- `ulimit`

结论：

普通 sudo 用户路径已经完成真机闭环验证，不再只是实验性能力。

## 代表性命令

### root 引导受控运维账号

```bash
go run ./cmd/bootstrapctl apply -i ./.configs/inventory.yaml -p ./.configs/profile.managed-admin.yaml -t 20s
```

### 普通 sudo 用户扫描

```bash
go run ./cmd/bootstrapctl scan -i ./.configs/inventory.sudo.yaml -t 20s
```

### 普通 sudo 用户规划

```bash
go run ./cmd/bootstrapctl plan -i ./.configs/inventory.sudo.yaml -p ./.configs/profile.yaml -t 20s
```

### 普通 sudo 用户执行

```bash
go run ./cmd/bootstrapctl apply -i ./.configs/inventory.sudo.yaml -p ./.configs/profile.yaml -t 20s
```

### 普通 sudo 用户校验

```bash
go run ./cmd/bootstrapctl verify -i ./.configs/inventory.sudo.yaml -p ./.configs/profile.yaml -t 20s
```

## 报告产物

所有真机验证都会在以下目录留下证据：

- `.bootstrapctl-reports/`

报告格式：

- `*.json`
- `*.md`

推荐保留这些报告，用于：

- 交付记录
- 排障留痕
- 回归对比

## 当前仍建议继续补齐的验证

虽然当前已经具备较好的可信度，但从企业化角度，仍建议后续继续补：

- 更多 Linux 发行版矩阵
- 重复 apply 幂等回归
- 失败注入与回滚验证
- 备份 / 恢复执行器验证
- 离线交付包验证
