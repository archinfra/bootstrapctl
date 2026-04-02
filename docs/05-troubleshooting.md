# 排障文档

## 跳板机报 `administratively prohibited (open failed)`

### 现象

扫描或执行时出现：

```text
ssh: rejected: administratively prohibited (open failed)
```

### 原因

跳板机拒绝标准 SSH `direct-tcpip` 通道转发。

### 当前处理

`bootstrapctl` 会自动从标准转发切换到 shell-hop 二跳模式：

1. 先登录跳板机
2. 再从跳板机执行第二跳 SSH

### 建议

- 确认跳板机已安装 `ssh` 客户端
- 确认跳板机到目标节点能连通
- 如果仍失败，重点排查 bastion 到目标节点的认证信息

## 开启了 `ssh_authorized_key` 但提示找不到公钥

### 现象

```text
未找到可用的 SSH 公钥，请通过 ssh_key.public_key 或 ssh_key.public_key_path 提供
```

### 原因

你开启了控制端公钥分发，但本机没有找到可分发的公钥。

### 处理方法

三选一：

1. 关闭功能

```yaml
features:
  ssh_authorized_key: false
```

2. 指定公钥路径

```yaml
ssh_key:
  public_key_path: ~/.ssh/id_ed25519.pub
```

3. 直接写公钥内容

```yaml
ssh_key:
  public_key: ssh-ed25519 AAAA...
```

## `sudo: unable to resolve host`

### 现象

远端执行时伴随：

```text
sudo: unable to resolve host ...
```

### 原因

主机名与 `/etc/hosts` 还未收敛。

### 当前影响

`bootstrapctl` 当前已经在状态解析层做了容错，这类告警不会再误伤任务结果判断。

### 建议

继续执行：

- `hostname`
- `hosts_file`

通常完成一次完整 `apply` 后会恢复正常。

## root SSH 明明写了 `PermitRootLogin no` 还是没生效

### 原因

OpenSSH 对很多配置项使用“首条生效”规则。

如果前面已有：

```text
PermitRootLogin yes
```

后面再追加一条：

```text
PermitRootLogin no
```

并不会覆盖前面的值。

### 当前处理

`bootstrapctl` 会：

- 清理旧的托管块
- 在合适位置插入受控配置
- 避免被前面的同名配置抢先生效
- 在变更后执行 `sshd -t` 校验

## `bad interpreter: /bin/bash^M`

### 现象

```text
/bin/bash^M: bad interpreter
```

### 原因

脚本使用了 Windows CRLF 换行。

### 处理

```bash
sed -i 's/\r$//' ./init.sh
chmod +x ./init.sh
```

或批量修复：

```bash
find . -type f -name '*.sh' -exec sed -i 's/\r$//' {} +
```

## 普通 sudo 用户执行时报 sudo 需要密码

### 现象

任务在非 root 用户下执行失败，提示 sudo 需要密码或命令无法执行。

### 原因

当前执行器在普通用户模式下使用：

```text
sudo -n bash -se
```

如果目标用户没有 `NOPASSWD`，执行会失败。

### 处理

确认：

- `inventory` 中开启了 `use_sudo: true`
- 目标用户具备 `sudo` 权限
- 如需无交互执行，具备 `NOPASSWD`

推荐先使用 `managed_admin` 完成标准化引导。

## bastion 认证字段看起来没写全

### 说明

当前继承规则如下：

- 节点没写 `bastion`：继承 `transport.bastion`
- 节点只写了 `bastion.host`：其余认证字段优先继承 `transport.bastion.*`
- 如果 `transport.bastion.*` 也没写：继续继承 `transport.*`

所以很多场景下，你不需要在每个节点重复写一遍 bastion 密码或私钥。

## 扫描里时间同步或容器运行时是告警

### 说明

这通常不是连接错误，而是基线尚未完全收敛。

常见情况：

- `ntp=no`
- `container runtime=missing`
- `overlay=missing`
- `ip_forward=0`

建议顺序：

1. 先 `scan`
2. 再 `plan`
3. 再 `apply`
4. 最后 `verify`
