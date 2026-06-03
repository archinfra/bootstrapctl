# 使用文档

## 两种使用模式

### 1. 文件模式

直接执行：

```bash
./bootstrapctl
```

这会在当前目录生成 `inventory.yaml`。你只需要改最前面的几项：

```yaml
cluster_name: demo-env

transport:
  ssh_user: root
  ssh_password: changeme

nodes:
  - hostname: node-01
    ip: 192.168.1.10
```

然后执行：

```bash
./bootstrapctl apply
```

`apply` 默认读取当前目录的 `inventory.yaml`，所以常规情况下不用写 `-i`，也不用写 `-c`。

### 2. 命令行模式

不想生成配置文件时，直接一行执行：

```bash
./bootstrapctl apply -H node-01=192.168.1.10 -u root -p 'root密码'
```

多台机器：

```bash
./bootstrapctl apply -H 'master01=10.0.0.1,node01=10.0.0.2' -u root -p 'root密码'
```

`-H` 支持三种写法：

```bash
-H 192.168.1.10
-H node-01=192.168.1.10
-H 'master01=10.0.0.1,node01=10.0.0.2'
```

## 主线命令

```bash
./bootstrapctl          # 生成 inventory.yaml
./bootstrapctl init     # 同上
./bootstrapctl apply    # 正式执行初始化
```

## 可选辅助命令

```bash
./bootstrapctl check    # 本地配置检查，不连接远端
./bootstrapctl scan     # 扫描远端主机当前状态
./bootstrapctl plan     # 预览动作，不落变更
./bootstrapctl verify   # 执行后校验
```

推荐实际交付时只告诉客户主线：

```bash
./bootstrapctl
./bootstrapctl apply
```

需要更稳妥时，可以在 `apply` 前多执行一次：

```bash
./bootstrapctl plan
```

## 常用参数

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

## 配置文件原则

`inventory.yaml` 采取“上面常改，下面默认”的结构：

- 常改区：账号、密码、hostname、ip
- 默认区：端口、sudo、ssh_auth、host_ip、节点级账号、跳板机等说明

普通客户只改常改区即可。
