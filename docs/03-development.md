# 开发文档

## 开发目标

`bootstrapctl` 的开发原则是：

- 幂等优先
- 报告优先
- 受限网络优先
- 普通 sudo 用户优先
- 可测试优先

## 代码结构

```text
cmd/bootstrapctl/
internal/app/
internal/config/
internal/remote/
internal/tasks/
internal/runner/
internal/scan/
internal/report/
internal/scaffold/
internal/ui/
examples/
docs/
```

## 关键模块说明

### `cmd/bootstrapctl`

CLI 主入口，只负责启动程序。

### `internal/app`

负责：

- 命令分发
- 参数解析
- 加载配置
- 创建执行器
- 调用扫描器或执行引擎

### `internal/config`

负责：

- 读取 `inventory / profile`
- 应用默认值
- 运行时解析
- 校验配置

这里是整个工具的配置边界，新增功能时应尽量先在这里定义清楚语义。

### `internal/remote`

负责远程执行。

当前支持三种路径：

- 直连
- 跳板机标准转发
- shell-hop 二跳兜底

如果远端执行链路出问题，优先从这里排查。

### `internal/tasks`

初始化任务集合。

每个任务必须实现：

- `Check`
- `Apply`

任务应满足：

- 可重复执行
- 已收敛时不产生误报
- 输出尽量稳定

### `internal/runner`

负责 `plan / apply / verify` 统一执行流程。

### `internal/scan`

负责基线观测，不做变更。

### `internal/report`

负责把结果持久化到 JSON / Markdown。

### `internal/ui`

负责中文终端输出。

## 如何新增一个任务

建议按下面步骤：

1. 在 `internal/tasks/` 新建任务文件
2. 定义结构体和字段
3. 实现 `Key / Title / Node / Check / Apply`
4. 在 `Build(...)` 中决定装配顺序
5. 为任务增加单元测试
6. 如果会产生状态文本，确保解析逻辑稳定

### 任务实现建议

- `Check` 要尽量只做观测，不做变更
- `Apply` 要尽量只改必要部分
- 尽量使用受控区块，而不是无界追加
- 遇到多行脚本时，统一走远端 Bash
- 输出中尽量给出机器可读的状态标识

## 状态解析约定

远端经常会遇到类似：

- `sudo: unable to resolve host ...`

这类警告不应破坏任务结果判断。

当前统一通过 `parseStatusLine(...)` 处理，位置：

- `internal/tasks/task.go`

新增任务如果依赖脚本最后一行状态标识，应优先复用这套模式。

## 如何新增一个扫描项

建议步骤：

1. 在 `internal/scan/scan.go` 中新增观测逻辑
2. 明确分类，例如：
   - `system`
   - `network`
   - `storage`
   - `runtime`
   - `kernel`
3. 输出统一的状态：
   - `ok`
   - `warn`
   - `error`
4. 为扫描项补测试

扫描器要尽量做到：

- 失败可定位
- 报告可读
- JSON 易消费

## 配置演进原则

新增配置时优先考虑：

1. 是不是应该放到 `inventory`
2. 还是应该放到 `profile`
3. 是否需要默认值
4. 是否需要显式开关
5. 是否会影响旧配置兼容性

经验上：

- “连接方式”放 `inventory`
- “目标状态”放 `profile`

## 开发测试命令

### 单元测试

```bash
go test ./...
```

### 查看版本

```bash
go run ./cmd/bootstrapctl version
```

### 本地模板冒烟

```bash
go run ./cmd/bootstrapctl init -d ./demo-init -c demo-env
```

### 真机扫描

```bash
go run ./cmd/bootstrapctl scan -i ./.configs/inventory.yaml -t 20s
```

### 真机规划

```bash
go run ./cmd/bootstrapctl plan -i ./.configs/inventory.yaml -p ./.configs/profile.yaml -t 20s
```

## 构建与发布

### 本地构建

当前仓库自带两个构建脚本：

- [../build.sh](../build.sh)
- [../build.ps1](../build.ps1)

默认会构建：

- Linux `amd64`
- Linux `arm64`

并输出到 `dist/`。

### GitHub Actions

项目还附带两条 GitHub Actions：

- [../.github/workflows/ci.yml](../.github/workflows/ci.yml)
- [../.github/workflows/release.yml](../.github/workflows/release.yml)

推荐用法：

- 日常提交：看 `ci.yml`
- 打版本标签：看 `release.yml`

如果后续把 `bootstrapctl` 独立成单独仓库，这两条 workflow 可以直接使用。

## 开发约定

- 新增能力优先写进 `examples/`
- 新增能力优先补单元测试
- 有真实现场问题时，优先把场景固化成回归测试
- 文档和示例要跟代码一起更新

## 下一阶段建议

从企业化角度，后续最值得继续补的几块是：

- 备份 / 恢复执行器
- `.run` 与离线 bundle
- 运行时层收口
- 更完整的基线检查
