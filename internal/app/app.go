package app

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/exporter"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
	"github.com/yuanyp8/bootstrapctl/internal/report"
	"github.com/yuanyp8/bootstrapctl/internal/runner"
	"github.com/yuanyp8/bootstrapctl/internal/scaffold"
	"github.com/yuanyp8/bootstrapctl/internal/scan"
	"github.com/yuanyp8/bootstrapctl/internal/tasks"
	"github.com/yuanyp8/bootstrapctl/internal/ui"
)

// version 会在本地开发时使用；正式构建时可通过 -ldflags 注入。
var version = "0.6.0"

type lifecycleOptions struct {
	InventoryPath string
	ProfilePath   string
	ReportDir     string
	DryRun        bool
	Timeout       time.Duration
}

type scanOptions struct {
	InventoryPath string
	ProfilePath   string
	ReportDir     string
	Timeout       time.Duration
}

type initOptions struct {
	Dir         string
	ClusterName string
	Inventory   string
	Profile     string
	Force       bool
}

type exportOpsEnvOptions struct {
	InventoryPath string
	OutputPath    string
}

// Run 是 bootstrapctl CLI 的统一入口。
func Run(args []string) int {
	console := ui.NewConsole()
	if len(args) == 0 {
		printUsage(console)
		return 0
	}

	switch args[0] {
	case "init":
		return runInit(console, args[1:])
	case "export-ops-env":
		return runExportOpsEnv(console, args[1:])
	case "plan":
		return runLifecycle(console, tasks.ModePlan, args[1:])
	case "apply":
		return runLifecycle(console, tasks.ModeApply, args[1:])
	case "verify":
		return runLifecycle(console, tasks.ModeVerify, args[1:])
	case "scan":
		return runScan(console, args[1:])
	case "version":
		console.Plain("bootstrapctl %s", version)
		return 0
	case "help", "-h", "--help":
		printUsage(console)
		return 0
	default:
		console.Error("未知命令: %s", args[0])
		printUsage(console)
		return 1
	}
}

func runInit(console *ui.Console, args []string) int {
	options, ok := parseInitFlags(console, args)
	if !ok {
		return 1
	}

	result, err := scaffold.WriteTemplates(scaffold.InitOptions{
		Dir:         options.Dir,
		ClusterName: options.ClusterName,
		Inventory:   options.Inventory,
		Profile:     options.Profile,
		Force:       options.Force,
	})
	if err != nil {
		console.Error("%v", err)
		return 1
	}

	opsEnvPath := defaultOpsEnvPath(result.InventoryPath)

	console.Banner("bootstrapctl 项目模板已生成")
	console.Section("模板文件")
	console.Item("inventory", result.InventoryPath)
	console.Item("profile", result.ProfilePath)

	console.Section("模板特点")
	console.Item("连接模型", "inventory 负责描述节点、SSH 与跳板机，profile 负责描述目标收敛状态")
	console.Item("防火墙策略", "默认停用 firewalld/ufw，并以 iptables 作为最终规则入口")
	console.Item("SSH 免密", "可选开启控制端公钥分发，以及跳板机到内网节点的免密链路")
	console.Item("LVM 兼容", "ops-environment.sh 会在 scan/plan/apply/verify 时按最新 inventory 自动同步")

	console.Section("建议的下一步")
	console.Command("bootstrapctl scan -i " + quoteIfNeeded(result.InventoryPath))
	console.Command("bootstrapctl plan -i " + quoteIfNeeded(result.InventoryPath) + " -p " + quoteIfNeeded(result.ProfilePath))
	console.Command("bootstrapctl apply -i " + quoteIfNeeded(result.InventoryPath) + " -p " + quoteIfNeeded(result.ProfilePath))
	console.Command("bootstrapctl verify -i " + quoteIfNeeded(result.InventoryPath) + " -p " + quoteIfNeeded(result.ProfilePath))

	console.Section("ops-env 兼容文件")
	console.Item("默认行为", "init 只生成模板；scan/plan/apply/verify 会自动在 inventory 同目录同步 ops-environment.sh")
	console.Item("自动输出", opsEnvPath)
	console.Item("手工导出", "如需单独刷新，也可以执行下面这条命令")
	console.Command("bootstrapctl export-ops-env -i " + quoteIfNeeded(result.InventoryPath) + " -o " + quoteIfNeeded(opsEnvPath))

	console.Success("现在先补齐 inventory 的节点与认证信息，再按 scan -> plan -> apply -> verify 的顺序执行即可。")
	return 0
}

func runExportOpsEnv(console *ui.Console, args []string) int {
	options, ok := parseExportOpsEnvFlags(console, args)
	if !ok {
		return 1
	}

	inventory, err := config.LoadInventory(options.InventoryPath)
	if err != nil {
		console.Error("%v", err)
		return 1
	}

	content := exporter.RenderInventoryShell(inventory)
	if strings.TrimSpace(options.OutputPath) == "" {
		console.Plain("%s", content)
		return 0
	}

	changed, err := writeOpsEnvCompatFile(options.OutputPath, inventory)
	if err != nil {
		console.Error("写出 ops-env 兼容文件失败: %v", err)
		return 1
	}

	console.Banner("bootstrapctl EXPORT-OPS-ENV")
	console.Section("输出结果")
	console.Item("inventory", options.InventoryPath)
	console.Item("cluster", inventory.ClusterName)
	console.Item("output", options.OutputPath)
	console.Item("node-count", len(inventory.Nodes))
	if changed {
		console.Success("已导出为兼容 02-lvm 的 ops-environment.sh 格式")
	} else {
		console.Success("ops-environment.sh 已是最新内容，无需重复写入")
	}
	return 0
}

func runLifecycle(console *ui.Console, mode tasks.Mode, args []string) int {
	options, ok := parseLifecycleFlags(console, string(mode), args)
	if !ok {
		return 1
	}

	inventory, err := config.LoadInventory(options.InventoryPath)
	if err != nil {
		console.Error("%v", err)
		return 1
	}
	profile, err := config.LoadProfile(options.ProfilePath)
	if err != nil {
		console.Error("%v", err)
		return 1
	}

	console.Banner("bootstrapctl " + strings.ToUpper(string(mode)))
	console.Section("执行参数")
	console.Item("inventory", options.InventoryPath)
	console.Item("profile", options.ProfilePath)
	console.Item("环境", inventory.ClusterName)
	console.Item("节点数", len(inventory.Nodes))
	console.Item("超时", options.Timeout)
	if mode == tasks.ModeApply {
		console.Item("dry-run", options.DryRun)
	}

	taskList := tasks.Build(inventory, profile)
	console.Item("任务数", len(taskList))
	syncOpsEnvCompat(console, options.InventoryPath, inventory)

	rep := report.New(string(mode), inventory.ClusterName, options.DryRun)
	engine := &runner.Engine{
		Executor: remote.NewSSHExecutor(options.Timeout),
		Console:  console,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(taskList)+1)*options.Timeout)
	defer cancel()

	err = engine.Run(ctx, mode, taskList, options.DryRun, rep)
	reportPath, saveErr := rep.SaveJSON(options.ReportDir)
	if saveErr != nil {
		console.Warn("保存执行报告失败: %v", saveErr)
	} else {
		console.Section("执行报告")
		console.Item("JSON 报告", reportPath)
	}

	if err != nil {
		console.Error("%v", err)
		return 1
	}

	console.Success("执行完成")
	return 0
}

func runScan(console *ui.Console, args []string) int {
	options, ok := parseScanFlags(console, args)
	if !ok {
		return 1
	}

	inventory, err := config.LoadInventory(options.InventoryPath)
	if err != nil {
		console.Error("%v", err)
		return 1
	}

	console.Banner("bootstrapctl SCAN")
	console.Section("执行参数")
	console.Item("inventory", options.InventoryPath)
	if strings.TrimSpace(options.ProfilePath) != "" {
		console.Item("profile", options.ProfilePath+"（当前 scan 阶段仅兼容接收，不参与扫描判断）")
	}
	console.Item("环境", inventory.ClusterName)
	console.Item("节点数", len(inventory.Nodes))
	console.Item("超时", options.Timeout)
	syncOpsEnvCompat(console, options.InventoryPath, inventory)

	scanRunner := scan.NewRunner(remote.NewSSHExecutor(options.Timeout))
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(inventory.Nodes)+1)*options.Timeout)
	defer cancel()

	rep, err := scanRunner.Run(ctx, inventory)
	if err != nil {
		console.Error("%v", err)
		return 1
	}

	console.Section("节点摘要")
	for _, node := range rep.Nodes {
		console.Info("节点 %s (%s) -> %s", node.NodeName, node.NodeIP, node.Summary.Status)
		for _, obs := range node.Observations {
			switch obs.Status {
			case "warn":
				console.Warn("  [%s] %s: %s", obs.Category, obs.Title, obs.Detail)
			case "error":
				console.Error("  [%s] %s: %s", obs.Category, obs.Title, obs.Detail)
			default:
				console.Success("  [%s] %s: %s", obs.Category, obs.Title, obs.Detail)
			}
		}
	}

	jsonPath, err := rep.SaveJSON(options.ReportDir)
	if err != nil {
		console.Warn("保存扫描报告失败: %v", err)
	} else {
		console.Item("JSON 报告", jsonPath)
	}

	markdownPath, err := rep.SaveMarkdown(options.ReportDir)
	if err != nil {
		console.Warn("保存 Markdown 扫描报告失败: %v", err)
	} else {
		console.Item("Markdown 报告", markdownPath)
	}

	totals := rep.Totals()
	console.Section("扫描汇总")
	console.Item("总节点数", totals.TotalNodes)
	console.Item("正常节点", totals.OKNodes)
	console.Item("告警节点", totals.WarnNodes)
	console.Item("异常节点", totals.ErrorNodes)
	console.Item("告警项", totals.TotalWarnings)
	console.Item("错误项", totals.TotalErrors)

	if totals.ErrorNodes > 0 || totals.TotalErrors > 0 {
		console.Error("基线扫描完成，但存在异常节点，请优先处理错误项")
		return 1
	}
	if totals.WarnNodes > 0 || totals.TotalWarnings > 0 {
		console.Warn("基线扫描完成，存在待处理告警")
		return 0
	}

	console.Success("基线扫描完成")
	return 0
}

func syncOpsEnvCompat(console *ui.Console, inventoryPath string, inventory config.Inventory) {
	for idx, outputPath := range defaultOpsEnvPaths(inventoryPath) {
		changed, err := writeOpsEnvCompatFile(outputPath, inventory)
		if err != nil {
			if idx < len(defaultOpsEnvPaths(inventoryPath))-1 {
				console.Warn("写入 %s 失败，改为回退到下一位置: %v", outputPath, err)
				continue
			}
			console.Warn("同步 ops-environment.sh 失败: %v", err)
			return
		}

		console.Item("ops-env", outputPath)
		if changed {
			console.Info("已根据当前 inventory 同步 ops-environment.sh")
		} else {
			console.Info("ops-environment.sh 已是最新状态")
		}
		return
	}
}

func writeOpsEnvCompatFile(outputPath string, inventory config.Inventory) (bool, error) {
	content := exporter.RenderInventoryShell(inventory)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return false, err
	}

	existing, err := os.ReadFile(outputPath)
	if err == nil && string(existing) == content {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	if err := os.WriteFile(outputPath, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func defaultOpsEnvPath(inventoryPath string) string {
	return defaultOpsEnvPaths(inventoryPath)[0]
}

func defaultOpsEnvPaths(inventoryPath string) []string {
	paths := make([]string, 0, 2)
	if runtime.GOOS != "windows" {
		if info, err := os.Stat("/etc/profile.d"); err == nil && info.IsDir() {
			paths = append(paths, "/etc/profile.d/ops-environment.sh")
		}
	}
	paths = append(paths, filepath.Join(filepath.Dir(inventoryPath), "ops-environment.sh"))
	return paths
}

func parseLifecycleFlags(console *ui.Console, command string, args []string) (lifecycleOptions, bool) {
	var options lifecycleOptions
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cwd, _ := os.Getwd()
	defaultReportDir := filepath.Join(cwd, ".bootstrapctl-reports")

	fs.StringVar(&options.InventoryPath, "inventory", "", "inventory YAML 文件路径")
	fs.StringVar(&options.InventoryPath, "inv", "", "inventory YAML 文件路径（缩写）")
	fs.StringVar(&options.InventoryPath, "i", "", "inventory YAML 文件路径（短参数）")
	fs.StringVar(&options.ProfilePath, "profile", "", "profile YAML 文件路径")
	fs.StringVar(&options.ProfilePath, "prof", "", "profile YAML 文件路径（缩写）")
	fs.StringVar(&options.ProfilePath, "p", "", "profile YAML 文件路径（短参数）")
	fs.StringVar(&options.ReportDir, "report-dir", defaultReportDir, "执行报告输出目录")
	fs.StringVar(&options.ReportDir, "r", defaultReportDir, "执行报告输出目录（短参数）")
	fs.BoolVar(&options.DryRun, "dry-run", false, "仅显示会发生的变更，不真正执行")
	fs.DurationVar(&options.Timeout, "timeout", 15*time.Second, "单个 SSH 任务超时时间")
	fs.DurationVar(&options.Timeout, "t", 15*time.Second, "单个 SSH 任务超时时间（短参数）")

	if err := fs.Parse(args); err != nil {
		return options, false
	}
	if options.InventoryPath == "" {
		console.Error("必须通过 --inventory / --inv / -i 指定 inventory 文件")
		return options, false
	}
	if options.ProfilePath == "" {
		console.Error("必须通过 --profile / --prof / -p 指定 profile 文件")
		return options, false
	}
	return options, true
}

func parseScanFlags(console *ui.Console, args []string) (scanOptions, bool) {
	var options scanOptions
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cwd, _ := os.Getwd()
	defaultReportDir := filepath.Join(cwd, ".bootstrapctl-reports")

	fs.StringVar(&options.InventoryPath, "inventory", "", "inventory YAML 文件路径")
	fs.StringVar(&options.InventoryPath, "inv", "", "inventory YAML 文件路径（缩写）")
	fs.StringVar(&options.InventoryPath, "i", "", "inventory YAML 文件路径（短参数）")
	fs.StringVar(&options.ProfilePath, "profile", "", "profile YAML 文件路径（当前 scan 阶段为兼容参数，不参与扫描逻辑）")
	fs.StringVar(&options.ProfilePath, "prof", "", "profile YAML 文件路径（缩写，当前 scan 阶段仅兼容接收）")
	fs.StringVar(&options.ProfilePath, "p", "", "profile YAML 文件路径（短参数，当前 scan 阶段仅兼容接收）")
	fs.StringVar(&options.ReportDir, "report-dir", defaultReportDir, "扫描报告输出目录")
	fs.StringVar(&options.ReportDir, "r", defaultReportDir, "扫描报告输出目录（短参数）")
	fs.DurationVar(&options.Timeout, "timeout", 15*time.Second, "单个 SSH 任务超时时间")
	fs.DurationVar(&options.Timeout, "t", 15*time.Second, "单个 SSH 任务超时时间（短参数）")

	if err := fs.Parse(args); err != nil {
		return options, false
	}
	if options.InventoryPath == "" {
		console.Error("必须通过 --inventory / --inv / -i 指定 inventory 文件")
		return options, false
	}
	return options, true
}

func parseInitFlags(console *ui.Console, args []string) (initOptions, bool) {
	var options initOptions
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cwd, _ := os.Getwd()

	options.Dir = cwd
	options.ClusterName = defaultClusterName(cwd)
	options.Inventory = "inventory.yaml"
	options.Profile = "profile.yaml"

	fs.StringVar(&options.Dir, "dir", options.Dir, "模板输出目录")
	fs.StringVar(&options.Dir, "d", options.Dir, "模板输出目录（短参数）")
	fs.StringVar(&options.ClusterName, "cluster-name", options.ClusterName, "默认环境名称")
	fs.StringVar(&options.ClusterName, "c", options.ClusterName, "默认环境名称（短参数）")
	fs.StringVar(&options.Inventory, "inventory", options.Inventory, "inventory 模板文件名")
	fs.StringVar(&options.Inventory, "i", options.Inventory, "inventory 模板文件名（短参数）")
	fs.StringVar(&options.Profile, "profile", options.Profile, "profile 模板文件名")
	fs.StringVar(&options.Profile, "p", options.Profile, "profile 模板文件名（短参数）")
	fs.BoolVar(&options.Force, "force", false, "如目标文件已存在则覆盖")
	fs.BoolVar(&options.Force, "f", false, "如目标文件已存在则覆盖（短参数）")

	if err := fs.Parse(args); err != nil {
		return options, false
	}
	if strings.TrimSpace(options.ClusterName) == "" {
		console.Error("cluster-name 不能为空")
		return options, false
	}
	if strings.TrimSpace(options.Inventory) == "" || strings.TrimSpace(options.Profile) == "" {
		console.Error("inventory/profile 模板文件名不能为空")
		return options, false
	}
	return options, true
}

func parseExportOpsEnvFlags(console *ui.Console, args []string) (exportOpsEnvOptions, bool) {
	var options exportOpsEnvOptions
	fs := flag.NewFlagSet("export-ops-env", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	fs.StringVar(&options.InventoryPath, "inventory", "", "inventory YAML 文件路径")
	fs.StringVar(&options.InventoryPath, "inv", "", "inventory YAML 文件路径（缩写）")
	fs.StringVar(&options.InventoryPath, "i", "", "inventory YAML 文件路径（短参数）")
	fs.StringVar(&options.OutputPath, "output", "", "输出的 ops-environment.sh 路径，留空表示输出到 stdout")
	fs.StringVar(&options.OutputPath, "o", "", "输出的 ops-environment.sh 路径（短参数）")

	if err := fs.Parse(args); err != nil {
		return options, false
	}
	if options.InventoryPath == "" {
		console.Error("必须通过 --inventory / --inv / -i 指定 inventory 文件")
		return options, false
	}
	return options, true
}

func printUsage(console *ui.Console) {
	console.Plain(`bootstrapctl - 面向离线、半离线和受限网络环境的企业级主机初始化工具

用法:
  bootstrapctl init    [-d .] [-c demo]
  bootstrapctl export-ops-env -i ./inventory.yaml [-o ./ops-environment.sh]
  bootstrapctl scan    -i ./inventory.yaml [-p ./profile.yaml]
  bootstrapctl plan    -i ./inventory.yaml -p ./profile.yaml
  bootstrapctl apply   -i ./inventory.yaml -p ./profile.yaml
  bootstrapctl verify  -i ./inventory.yaml -p ./profile.yaml
  bootstrapctl version

常用短参数:
  -i / --inv        inventory 文件
  -p / --prof       profile 文件
  -t                timeout
  -r                report-dir
  -d                init 输出目录
  -c                init 默认环境名称
  -f                init 覆盖已有模板
  -o                export-ops-env 输出文件

当前支持的初始化能力:
  - SSH 连通性检查
  - 控制端 SSH 公钥分发
  - 当前执行节点本机 ~/.ssh/config 自动维护
  - 跳板机到内网节点免密
  - 主机名设置
  - /etc/hosts 受控区块
  - 关闭 SWAP
  - 关闭 SELinux
  - 收口 firewalld / ufw，并以 iptables 作为最终规则入口
  - 收口 Kubernetes 内核网络参数
  - 容器 graphroot / cri root / containers/storage.conf
  - ulimit 具体数值写入

当前支持的基线扫描:
  - 操作系统 / 内核 / 架构
  - CPU / 内存 / 根分区与 /data 使用率
  - hostname -I 与选中的主 IP
  - 根分区与 /data 总量 / 可用量
  - lsblk 块设备摘要
  - SWAP / SELinux / firewalld / ufw / iptables
  - 时间同步
  - 容器运行时 / kubelet
  - overlay / br_netfilter / ip_forward / bridge-nf-call-iptables
`)
}

func defaultClusterName(cwd string) string {
	name := filepath.Base(cwd)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "bootstrap-cluster"
	}
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

func quoteIfNeeded(value string) string {
	if strings.ContainsAny(value, " \t") {
		return `"` + value + `"`
	}
	return value
}
