# bootstrapctl 文档总览

这套文档面向三类读者：

- 使用者：想快速接管一批主机并完成初始化
- 运维工程师：想把 `bootstrapctl` 纳入企业环境
- 开发者：想继续扩展任务、扫描器和交付形态

建议阅读顺序如下：

1. [架构文档](./01-architecture.md)
2. [使用文档](./02-usage.md)
3. [开发文档](./03-development.md)
4. [测试与验证文档](./04-testing-and-validation.md)
5. [排障文档](./05-troubleshooting.md)

## 文档清单

### 1. 架构文档

- 文件：[01-architecture.md](./01-architecture.md)
- 内容：
  - 设计目标
  - `inventory / profile` 模型
  - CLI、执行引擎、任务系统、扫描系统关系
  - 跳板机与 shell-hop 设计

### 2. 使用文档

- 文件：[02-usage.md](./02-usage.md)
- 内容：
  - `init / scan / plan / apply / verify`
  - 单机场景
  - 跳板机场景
  - 受控运维账号场景
  - 普通 sudo 用户场景

### 3. 开发文档

- 文件：[03-development.md](./03-development.md)
- 内容：
  - 代码结构
  - 新增任务的方法
  - 新增扫描项的方法
  - SSH 执行器与状态解析约定
  - 开发测试命令

### 4. 测试与验证文档

- 文件：[04-testing-and-validation.md](./04-testing-and-validation.md)
- 内容：
  - 单元测试范围
  - 真机验证矩阵
  - 普通 sudo 用户全量验证
  - 报告产物说明

### 5. 排障文档

- 文件：[05-troubleshooting.md](./05-troubleshooting.md)
- 内容：
  - 跳板机拒绝转发
  - 公钥找不到
  - `sudo` 主机名告警
  - root SSH 策略
  - CRLF 脚本问题

## 相关文件

- 项目首页：[../README.md](../README.md)
- 全量 inventory 模板：[../examples/inventory.full.yaml](../examples/inventory.full.yaml)
- 全量 profile 模板：[../examples/profile.full.yaml](../examples/profile.full.yaml)
- managed-admin 示例：[../examples/profile.managed-admin.yaml](../examples/profile.managed-admin.yaml)
