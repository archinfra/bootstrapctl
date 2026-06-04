package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
var version = "0.8.0"

type inlineInventoryOptions struct {
	Host          string
	Hosts         string
	ClusterName   string
	SSHUser       string
	SSHPort       int
	SSHPassword   string
	SSHPrivateKey string
	UseSudo       bool
}

type quickstartOptions struct {
	Dir       string
	Inventory string
	Force     bool
	Inline    inlineInventoryOptions
}

type lifecycleOptions struct {
	InventoryPath string
	ProfilePath   string
	ReportDir     string
	DryRun        bool
	Timeout       time.Duration
	SyncOpsEnv    bool
	NoSyncOpsEnv  bool
	Inline        inlineInventoryOptions
}

type scanOptions struct {
	InventoryPath string
	ProfilePath   string
	ReportDir     string
	Timeout       time.Duration
	SyncOpsEnv    bool
	NoSyncOpsEnv  bool
	Inline        inlineInventoryOptions
}

type initOptions struct {
	Dir         string
	ClusterName string
	Inventory   string
	Profile     string
	Force       bool
	Advanced    bool
}

type exportOpsEnvOptions struct {
	InventoryPath string
	OutputPath    string
}

type doctorOptions struct {
	InventoryPath string
	ProfilePath   string
	SyncOpsEnv    bool
	NoSyncOpsEnv  bool
}

// Run 是 bootstrapctl CLI 的统一入口。
func Run(args []string) int {
	console := ui.NewConsole()
	if len(args) == 0 {
		// 极简入口：直接执行 ./bootstrapctl 等同于 ./bootstrapctl init。
		return runInit(console, nil)
	}

	switch args[0] {
	case "init":
		return runInit(console, args[1:])
	case "quickstart", "new":
		// 兼容旧入口；新手文档不再推荐 quickstart/new。
		return runInit(console, args[1:])
	case "export-ops-env":
		return runExportOpsEnv(console, args[1:])
	case "doctor", "check":
		return runDoctor(console, args[1:])
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
		Advanced:    options.Advanced,
	})
	if err != nil {
		if isAlreadyExistsError(err) {
			inventoryPath := filepath.Join(options.Dir, options.Inventory)
			console.Banner("bootstrapctl")
			console.Success("当前目录已经有配置文件，不再覆盖")
			console.Item("配置文件", inventoryPath)
			console.Section("下一步")
			console.Command("./bootstrapctl apply")
			console.Info("如需重新生成模板，请执行 ./bootstrapctl init -f")
			return 0
		}
		console.Error("%v", err)
		return 1
	}

	// 兼容旧 Shell/LVM 脚本：内部静默生成派生文件，不在新手输出里暴露。
	if inventory, err := config.LoadInventory(result.InventoryPath); err == nil {
		_, _ = writeOpsEnvCompatFile(defaultOpsEnvPath(result.InventoryPath), inventory)
	}

	console.Banner("bootstrapctl")
	console.Success("已生成配置文件")
	console.Item("配置文件", result.InventoryPath)
	if result.ProfilePath != "" {
		console.Item("高级配置", result.ProfilePath)
	}

	console.Section("只需要先改这里")
	console.Item("账号", "transport.ssh_user")
	console.Item("密码", "transport.ssh_password")
	console.Item("主机", "nodes[].hostname / nodes[].ip")

	console.Section("执行")
	console.Command("./bootstrapctl apply")
	console.Info("想先预览可执行 ./bootstrapctl plan；想临时不写配置可用 ./bootstrapctl apply -H node-01=192.168.1.10 -u root -p '<密码>'")
	return 0
}

func isAlreadyExistsError(err error) bool {
	text := err.Error()
	return strings.Contains(text, "已存在") || strings.Contains(text, "already exists")
}

func runQuickstart(console *ui.Console, args []string) int {
	// 旧 quickstart/new 入口退化为 init，避免再引入第三种使用心智。
	return runInit(console, args)
}

func runDoctor(console *ui.Console, args []string) int {
	options, ok := parseDoctorFlags(console, args)
	if !ok {
		return 1
	}

	inventory, err := config.LoadInventory(options.InventoryPath)
	if err != nil {
		console.Error("%v", err)
		return 1
	}

	profile, err := config.DefaultProfile()
	profileLabel := "builtin:k8s-host-init"
	if strings.TrimSpace(options.ProfilePath) != "" {
		profile, err = config.LoadProfile(options.ProfilePath)
		profileLabel = options.ProfilePath
	}
	if err != nil {
		console.Error("%v", err)
		return 1
	}

	console.Banner("bootstrapctl DOCTOR")
	console.Section("配置检查")
	console.Item("inventory", options.InventoryPath)
	console.Item("profile", profileLabel)
	console.Item("环境", inventory.ClusterName)
	console.Item("节点数", len(inventory.Nodes))
	console.Item("profile.name", profile.Name)

	if options.SyncOpsEnv {
		syncOpsEnvCompat(console, options.InventoryPath, inventory)
	}

	console.Section("节点连接摘要")
	for _, node := range inventory.ResolveNodes() {
		auth := "password"
		if strings.TrimSpace(node.SSHPrivateKey) != "" {
			auth = "key:" + node.SSHPrivateKey
		} else if strings.TrimSpace(node.SSHPassword) == "" {
			auth = "agent/default-key"
		}
		bastion := "direct"
		if node.Bastion != nil && strings.TrimSpace(node.Bastion.Host) != "" {
			bastion = "bastion:" + node.Bastion.Host
		}
		console.Item(node.Name, connectionBriefWithAuth(node, auth, bastion))
	}

	taskList := tasks.Build(inventory, profile)
	console.Section("将会参与的任务")
	console.Item("任务数", len(taskList))
	for _, task := range taskList {
		console.Info("%s/%s -> %s", task.Node(), task.Key(), task.Title())
	}

	console.Section("推荐执行顺序")
	console.Info("如果你就在 inventory 所在目录，直接执行下面命令，不需要 -i/-c。")
	console.Command("bootstrapctl scan")
	console.Command("bootstrapctl plan")
	console.Command("bootstrapctl apply")
	console.Command("bootstrapctl verify")
	console.Success("配置检查通过；doctor 不连接远端，只做本地配置解析、默认值展开和派生产物同步。")
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

	inventory, err := loadLifecycleInventory(options)
	if err != nil {
		console.Error("%v", err)
		return 1
	}
	profile, err := loadLifecycleProfile(options)
	if err != nil {
		console.Error("%v", err)
		return 1
	}

	console.Banner("bootstrapctl " + strings.ToUpper(string(mode)))
	console.Section("执行参数")
	if options.InventoryPath != "" {
		console.Item("inventory", options.InventoryPath)
	} else {
		console.Item("inventory", "inline (-H/--host/--hosts)，未落盘")
	}
	if options.ProfilePath != "" {
		console.Item("profile", options.ProfilePath)
	} else {
		console.Item("profile", "builtin:k8s-host-init")
	}
	console.Item("环境", inventory.ClusterName)
	console.Item("节点数", len(inventory.Nodes))
	console.Item("超时", options.Timeout)
	if mode == tasks.ModeApply {
		console.Item("dry-run", options.DryRun)
	}

	taskList := tasks.Build(inventory, profile)
	console.Item("任务数", len(taskList))
	if options.SyncOpsEnv && options.InventoryPath != "" {
		syncOpsEnvCompat(console, options.InventoryPath, inventory)
	}

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

	inventory, err := loadScanInventory(options)
	if err != nil {
		console.Error("%v", err)
		return 1
	}

	console.Banner("bootstrapctl SCAN")
	console.Section("执行参数")
	if options.InventoryPath != "" {
		console.Item("inventory", options.InventoryPath)
	} else {
		console.Item("inventory", "inline (-H/--host/--hosts)，未落盘")
	}
	if strings.TrimSpace(options.ProfilePath) != "" {
		console.Item("profile", options.ProfilePath+"（当前 scan 阶段仅兼容接收，不参与扫描判断）")
	}
	console.Item("环境", inventory.ClusterName)
	console.Item("节点数", len(inventory.Nodes))
	console.Item("超时", options.Timeout)
	if options.SyncOpsEnv && options.InventoryPath != "" {
		syncOpsEnvCompat(console, options.InventoryPath, inventory)
	}

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

func loadLifecycleInventory(options lifecycleOptions) (config.Inventory, error) {
	if strings.TrimSpace(options.InventoryPath) != "" {
		return config.LoadInventory(options.InventoryPath)
	}
	return buildInlineInventory(options.Inline)
}

func loadScanInventory(options scanOptions) (config.Inventory, error) {
	if strings.TrimSpace(options.InventoryPath) != "" {
		return config.LoadInventory(options.InventoryPath)
	}
	return buildInlineInventory(options.Inline)
}

func loadLifecycleProfile(options lifecycleOptions) (config.Profile, error) {
	if strings.TrimSpace(options.ProfilePath) != "" {
		return config.LoadProfile(options.ProfilePath)
	}
	return config.DefaultProfile()
}

func buildInlineInventory(options inlineInventoryOptions) (config.Inventory, error) {
	tokens := make([]string, 0, 8)
	if strings.TrimSpace(options.Host) != "" {
		tokens = append(tokens, strings.TrimSpace(options.Host))
	}
	if strings.TrimSpace(options.Hosts) != "" {
		for _, token := range strings.Split(options.Hosts, ",") {
			if strings.TrimSpace(token) != "" {
				tokens = append(tokens, strings.TrimSpace(token))
			}
		}
	}
	if len(tokens) == 0 {
		return config.Inventory{}, fmt.Errorf("当前目录没有 inventory.yaml；请使用 -H hostname=ip -u root -p '<密码>'，或通过 --inventory 指定文件")
	}

	inventory := config.Inventory{
		ClusterName: strings.TrimSpace(options.ClusterName),
		Transport: config.Transport{
			SSHUser:       strings.TrimSpace(options.SSHUser),
			SSHPort:       options.SSHPort,
			SSHPassword:   options.SSHPassword,
			SSHPrivateKey: options.SSHPrivateKey,
			UseSudo:       options.UseSudo,
		},
		Nodes: make([]config.Node, 0, len(tokens)),
	}
	if inventory.ClusterName == "" {
		inventory.ClusterName = "bootstrap-inline"
	}

	for idx, token := range tokens {
		node, err := parseInlineHostToken(token, idx+1, len(tokens))
		if err != nil {
			return config.Inventory{}, err
		}
		inventory.Nodes = append(inventory.Nodes, node)
	}

	inventory.ApplyDefaults()
	if err := inventory.Validate(); err != nil {
		return config.Inventory{}, err
	}
	return inventory, nil
}

func parseInlineHostToken(token string, index int, total int) (config.Node, error) {
	value := strings.TrimSpace(token)
	if value == "" {
		return config.Node{}, fmt.Errorf("空的 host 条目")
	}

	name := fmt.Sprintf("node-%02d", index)
	if total == 1 {
		name = "node-01"
	}

	if strings.Contains(value, "=") {
		parts := strings.SplitN(value, "=", 2)
		name = strings.TrimSpace(parts[0])
		value = strings.TrimSpace(parts[1])
		if name == "" {
			return config.Node{}, fmt.Errorf("host 条目 %q 的节点名不能为空", token)
		}
	}

	ip := value
	var roles []string

	// 兼容旧写法 name=ip:role，但新手路径不再展示/要求 role。
	// IPv6 地址里也有冒号，所以只有刚好一个冒号时才按旧 role 后缀解析。
	if strings.Contains(value, ":") && strings.Count(value, ":") == 1 {
		parts := strings.SplitN(value, ":", 2)
		ip = strings.TrimSpace(parts[0])
		roleText := strings.TrimSpace(parts[1])
		if roleText != "" {
			roles = splitRoles(roleText)
		}
	}
	if ip == "" {
		return config.Node{}, fmt.Errorf("host 条目 %q 的 IP 不能为空", token)
	}
	return config.Node{Name: name, Hostname: name, IP: ip, Roles: roles}, nil
}

func splitRoles(input string) []string {
	input = strings.NewReplacer("|", "+", ";", "+", "/", "+").Replace(input)
	parts := strings.Split(input, "+")
	roles := make([]string, 0, len(parts))
	for _, part := range parts {
		role := strings.TrimSpace(part)
		if role != "" {
			roles = append(roles, role)
		}
	}
	if len(roles) == 0 {
		return []string{"node"}
	}
	return roles
}

func syncOpsEnvCompat(console *ui.Console, inventoryPath string, inventory config.Inventory) {
	// 兼容旧 Shell/LVM 脚本的派生产物保持静默同步，避免干扰主线体验。
	_, _ = writeOpsEnvCompatFile(defaultOpsEnvPath(inventoryPath), inventory)
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
	return filepath.Clean("/etc/profile.d/ops-environment.sh")
}

func defaultInventoryCandidates(cwd string) []string {
	return []string{
		filepath.Join(cwd, "inventory.yaml"),
		filepath.Join(cwd, "bootstrapctl.yml"),
		filepath.Join(cwd, "bootstrapctl.yaml"),
	}
}

func findDefaultInventoryPath(cwd string) (string, bool) {
	for _, candidate := range defaultInventoryCandidates(cwd) {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func defaultInventoryHint() string {
	return "当前目录未发现 inventory.yaml / bootstrapctl.yml / bootstrapctl.yaml；请先执行 bootstrapctl init，或直接使用 -H hostname=ip -u root -p '<密码>'"
}

func parseLifecycleFlags(console *ui.Console, command string, args []string) (lifecycleOptions, bool) {
	var options lifecycleOptions
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cwd, _ := os.Getwd()
	defaultReportDir := filepath.Join(cwd, ".bootstrapctl-reports")
	options.Inline = defaultInlineInventoryOptions(cwd)

	fs.StringVar(&options.InventoryPath, "inventory", "", "inventory YAML 文件路径；省略时默认读当前目录，或用 -H/--host/--hosts")
	fs.StringVar(&options.InventoryPath, "inv", "", "inventory YAML 文件路径（缩写）")
	fs.StringVar(&options.InventoryPath, "i", "", "inventory YAML 文件路径（短参数）")
	fs.StringVar(&options.ProfilePath, "profile", "", "profile YAML 文件路径；省略时使用内置默认 profile")
	fs.StringVar(&options.ProfilePath, "prof", "", "profile YAML 文件路径（缩写）")
	fs.StringVar(&options.ProfilePath, "P", "", "profile YAML 文件路径（高级短参数）")
	shortP := ""
	fs.StringVar(&shortP, "p", "", "SSH 密码短参数；文件模式下兼容旧版 -p profile.yaml")
	fs.StringVar(&options.ReportDir, "report-dir", defaultReportDir, "执行报告输出目录")
	fs.StringVar(&options.ReportDir, "r", defaultReportDir, "执行报告输出目录（短参数）")
	fs.BoolVar(&options.DryRun, "dry-run", false, "仅显示会发生的变更，不真正执行")
	options.SyncOpsEnv = true
	fs.DurationVar(&options.Timeout, "timeout", 15*time.Second, "单个 SSH 任务超时时间")
	fs.DurationVar(&options.Timeout, "t", 15*time.Second, "单个 SSH 任务超时时间（短参数）")
	fs.BoolVar(&options.SyncOpsEnv, "sync-ops-env", true, "文件模式下自动同步 /etc/profile.d/ops-environment.sh；可设为 false 关闭")
	fs.BoolVar(&options.NoSyncOpsEnv, "no-sync-ops-env", false, "关闭 ops-environment.sh 自动同步")
	registerInlineInventoryFlags(fs, &options.Inline)

	if err := fs.Parse(args); err != nil {
		return options, false
	}
	if options.NoSyncOpsEnv {
		options.SyncOpsEnv = false
	}
	if strings.TrimSpace(options.InventoryPath) != "" && hasInlineInventoryInput(options.Inline) {
		console.Error("-i/--inventory 与 -H/--host/--hosts 不能同时使用；请选择文件模式或无文件模式")
		return options, false
	}
	if strings.TrimSpace(options.InventoryPath) == "" && !hasInlineInventoryInput(options.Inline) {
		if defaultPath, ok := findDefaultInventoryPath(cwd); ok {
			options.InventoryPath = defaultPath
		} else {
			console.Error("%s", defaultInventoryHint())
			return options, false
		}
	}
	applyLifecycleShortP(&options, shortP)
	return options, true
}

func parseScanFlags(console *ui.Console, args []string) (scanOptions, bool) {
	var options scanOptions
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cwd, _ := os.Getwd()
	defaultReportDir := filepath.Join(cwd, ".bootstrapctl-reports")
	options.Inline = defaultInlineInventoryOptions(cwd)

	fs.StringVar(&options.InventoryPath, "inventory", "", "inventory YAML 文件路径；省略时默认读当前目录，或用 -H/--host/--hosts")
	fs.StringVar(&options.InventoryPath, "inv", "", "inventory YAML 文件路径（缩写）")
	fs.StringVar(&options.InventoryPath, "i", "", "inventory YAML 文件路径（短参数）")
	fs.StringVar(&options.ProfilePath, "profile", "", "profile YAML 文件路径（当前 scan 阶段为兼容参数，不参与扫描逻辑）")
	fs.StringVar(&options.ProfilePath, "prof", "", "profile YAML 文件路径（缩写，当前 scan 阶段仅兼容接收）")
	fs.StringVar(&options.ProfilePath, "P", "", "profile YAML 文件路径（高级短参数，当前 scan 阶段仅兼容接收）")
	shortP := ""
	fs.StringVar(&shortP, "p", "", "SSH 密码短参数；文件模式下兼容旧版 -p profile.yaml")
	fs.StringVar(&options.ReportDir, "report-dir", defaultReportDir, "扫描报告输出目录")
	fs.StringVar(&options.ReportDir, "r", defaultReportDir, "扫描报告输出目录（短参数）")
	options.SyncOpsEnv = true
	fs.DurationVar(&options.Timeout, "timeout", 15*time.Second, "单个 SSH 任务超时时间")
	fs.DurationVar(&options.Timeout, "t", 15*time.Second, "单个 SSH 任务超时时间（短参数）")
	fs.BoolVar(&options.SyncOpsEnv, "sync-ops-env", true, "文件模式下自动同步 /etc/profile.d/ops-environment.sh；可设为 false 关闭")
	fs.BoolVar(&options.NoSyncOpsEnv, "no-sync-ops-env", false, "关闭 ops-environment.sh 自动同步")
	registerInlineInventoryFlags(fs, &options.Inline)

	if err := fs.Parse(args); err != nil {
		return options, false
	}
	if options.NoSyncOpsEnv {
		options.SyncOpsEnv = false
	}
	if strings.TrimSpace(options.InventoryPath) != "" && hasInlineInventoryInput(options.Inline) {
		console.Error("-i/--inventory 与 -H/--host/--hosts 不能同时使用；请选择文件模式或无文件模式")
		return options, false
	}
	if strings.TrimSpace(options.InventoryPath) == "" && !hasInlineInventoryInput(options.Inline) {
		if defaultPath, ok := findDefaultInventoryPath(cwd); ok {
			options.InventoryPath = defaultPath
		} else {
			console.Error("%s", defaultInventoryHint())
			return options, false
		}
	}
	applyScanShortP(&options, shortP)
	return options, true
}

func applyLifecycleShortP(options *lifecycleOptions, shortP string) {
	shortP = strings.TrimSpace(shortP)
	if shortP == "" {
		return
	}
	if hasInlineInventoryInput(options.Inline) || strings.TrimSpace(options.InventoryPath) == "" {
		options.Inline.SSHPassword = shortP
		return
	}
	if strings.TrimSpace(options.ProfilePath) == "" {
		options.ProfilePath = shortP
	}
}

func applyScanShortP(options *scanOptions, shortP string) {
	shortP = strings.TrimSpace(shortP)
	if shortP == "" {
		return
	}
	if hasInlineInventoryInput(options.Inline) || strings.TrimSpace(options.InventoryPath) == "" {
		options.Inline.SSHPassword = shortP
		return
	}
	if strings.TrimSpace(options.ProfilePath) == "" {
		options.ProfilePath = shortP
	}
}

func parseQuickstartFlags(console *ui.Console, args []string) (quickstartOptions, bool) {
	var options quickstartOptions
	fs := flag.NewFlagSet("quickstart", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cwd, _ := os.Getwd()
	options.Dir = filepath.Join(cwd, "bootstrapctl-env")
	options.Inventory = "inventory.yaml"
	options.Inline = defaultInlineInventoryOptions(cwd)

	fs.StringVar(&options.Dir, "dir", options.Dir, "工程目录输出位置")
	fs.StringVar(&options.Dir, "d", options.Dir, "工程目录输出位置（短参数）")
	fs.StringVar(&options.Inventory, "inventory", options.Inventory, "inventory 文件名")
	fs.StringVar(&options.Inventory, "i", options.Inventory, "inventory 文件名（短参数）")
	fs.BoolVar(&options.Force, "force", false, "如 inventory 已存在则覆盖")
	fs.BoolVar(&options.Force, "f", false, "如 inventory 已存在则覆盖（短参数）")
	shortP := ""
	fs.StringVar(&shortP, "p", "", "SSH 密码短参数，等同于 --password")
	registerInlineInventoryFlags(fs, &options.Inline)

	if err := fs.Parse(args); err != nil {
		return options, false
	}
	if strings.TrimSpace(shortP) != "" {
		options.Inline.SSHPassword = shortP
	}
	if strings.TrimSpace(options.Dir) == "" {
		console.Error("quickstart 输出目录不能为空")
		return options, false
	}
	if strings.TrimSpace(options.Inventory) == "" {
		console.Error("inventory 文件名不能为空")
		return options, false
	}
	return options, true
}

func parseDoctorFlags(console *ui.Console, args []string) (doctorOptions, bool) {
	var options doctorOptions
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cwd, _ := os.Getwd()

	fs.StringVar(&options.InventoryPath, "inventory", "", "inventory YAML 文件路径")
	fs.StringVar(&options.InventoryPath, "inv", "", "inventory YAML 文件路径（缩写）")
	fs.StringVar(&options.InventoryPath, "i", "", "inventory YAML 文件路径（短参数）")
	fs.StringVar(&options.ProfilePath, "profile", "", "profile YAML 文件路径；省略时使用内置默认 profile")
	fs.StringVar(&options.ProfilePath, "prof", "", "profile YAML 文件路径（缩写）")
	fs.StringVar(&options.ProfilePath, "P", "", "profile YAML 文件路径（高级短参数）")
	fs.StringVar(&options.ProfilePath, "p", "", "profile YAML 文件路径（兼容旧短参数）")
	options.SyncOpsEnv = true
	fs.BoolVar(&options.SyncOpsEnv, "sync-ops-env", true, "自动同步 /etc/profile.d/ops-environment.sh；可设为 false 关闭")
	fs.BoolVar(&options.NoSyncOpsEnv, "no-sync-ops-env", false, "关闭 ops-environment.sh 自动同步")

	if err := fs.Parse(args); err != nil {
		return options, false
	}
	if options.NoSyncOpsEnv {
		options.SyncOpsEnv = false
	}
	if strings.TrimSpace(options.InventoryPath) == "" {
		if defaultPath, ok := findDefaultInventoryPath(cwd); ok {
			options.InventoryPath = defaultPath
		} else {
			console.Error("%s", defaultInventoryHint())
			return options, false
		}
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
	fs.StringVar(&options.Profile, "profile", options.Profile, "profile 模板文件名（advanced 模式才会生成）")
	fs.StringVar(&options.Profile, "p", options.Profile, "profile 模板文件名（advanced 模式才会生成）")
	fs.BoolVar(&options.Force, "force", false, "如目标文件已存在则覆盖")
	fs.BoolVar(&options.Force, "f", false, "如目标文件已存在则覆盖（短参数）")
	fs.BoolVar(&options.Advanced, "advanced", false, "生成完整 inventory/profile 模板")
	fs.BoolVar(&options.Advanced, "with-profile", false, "生成 profile.yaml（等同于 --advanced）")

	if err := fs.Parse(args); err != nil {
		return options, false
	}
	if strings.TrimSpace(options.ClusterName) == "" {
		console.Error("cluster-name 不能为空")
		return options, false
	}
	if strings.TrimSpace(options.Inventory) == "" {
		console.Error("inventory 模板文件名不能为空")
		return options, false
	}
	if options.Advanced && strings.TrimSpace(options.Profile) == "" {
		console.Error("advanced 模式下 profile 模板文件名不能为空")
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

func renderQuickstartInventory(inventory config.Inventory) string {
	nodes := inventory.ResolveNodes()
	builder := &strings.Builder{}
	builder.WriteString("# 由 bootstrapctl quickstart 生成。\n")
	builder.WriteString("# 常规场景只改 transport.ssh_password 和 nodes 里的 hostname/ip。\n")
	builder.WriteString("# ssh_port、use_sudo、roles、profile 都有默认值；需要时再加。\n")
	fmt.Fprintf(builder, "cluster_name: %s\n\n", yamlScalar(inventory.ClusterName))
	builder.WriteString("transport:\n")
	fmt.Fprintf(builder, "  ssh_user: %s\n", yamlScalar(inventory.Transport.SSHUser))
	fmt.Fprintf(builder, "  ssh_password: %s\n\n", yamlScalar(inventory.Transport.SSHPassword))
	builder.WriteString("nodes:\n")
	for _, node := range nodes {
		fmt.Fprintf(builder, "  - hostname: %s\n", yamlScalar(node.Name))
		fmt.Fprintf(builder, "    ip: %s\n", yamlScalar(node.IP))
		if strings.TrimSpace(node.HostIP) != "" {
			fmt.Fprintf(builder, "    host_ip: %s\n", yamlScalar(node.HostIP))
		}
		if len(node.Roles) > 0 {
			fmt.Fprintf(builder, "    roles: [%s]\n", joinYAMLInlineScalars(node.Roles))
		}
	}
	return builder.String()
}

func connectionBrief(node config.NodeConnection) string {
	parts := []string{fmt.Sprintf("ip=%s", node.IP)}
	if strings.TrimSpace(node.SSHUser) != "" {
		parts = append(parts, "user="+node.SSHUser)
	}
	if node.SSHPort != 0 && node.SSHPort != 22 {
		parts = append(parts, fmt.Sprintf("port=%d", node.SSHPort))
	}
	if node.UseSudo {
		parts = append(parts, "sudo=true")
	}
	if strings.TrimSpace(node.HostIP) != "" {
		parts = append(parts, "host_ip="+node.HostIP)
	}
	return strings.Join(parts, " ")
}

func connectionBriefWithAuth(node config.NodeConnection, auth string, via string) string {
	brief := connectionBrief(node)
	parts := []string{brief}
	if strings.TrimSpace(auth) != "" {
		parts = append(parts, "auth="+auth)
	}
	if strings.TrimSpace(via) != "" && via != "direct" {
		parts = append(parts, "via="+via)
	}
	return strings.Join(parts, " ")
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func joinYAMLInlineScalars(values []string) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, yamlScalar(value))
	}
	return strings.Join(parts, ", ")
}

func yamlScalar(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}
	if isPlainYAMLScalar(value) {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func isPlainYAMLScalar(value string) bool {
	for _, ch := range value {
		if !(ch == '-' || ch == '_' || ch == '.' || ch == '/' || ch == ':' || ch == '+' || ch == '=' || ch == '~' ||
			(ch >= '0' && ch <= '9') ||
			(ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z')) {
			return false
		}
	}
	return !strings.HasPrefix(value, "-") && !strings.Contains(value, "#")
}

func defaultInlineInventoryOptions(cwd string) inlineInventoryOptions {
	return inlineInventoryOptions{
		ClusterName: defaultClusterName(cwd),
		SSHUser:     "root",
		SSHPort:     22,
	}
}

func registerInlineInventoryFlags(fs *flag.FlagSet, options *inlineInventoryOptions) {
	fs.StringVar(&options.Host, "host", options.Host, "单台目标主机，格式 ip 或 hostname=ip")
	fs.StringVar(&options.Hosts, "hosts", options.Hosts, "多台目标主机，格式 hostname=ip,hostname2=ip；也兼容旧写法 hostname=ip:role")
	fs.StringVar(&options.Hosts, "H", options.Hosts, "目标主机，格式 ip、hostname=ip，或 hostname=ip,hostname2=ip")
	fs.StringVar(&options.ClusterName, "cluster-name", options.ClusterName, "环境名称；不填时自动使用当前目录名")
	fs.StringVar(&options.ClusterName, "c", options.ClusterName, "环境名称（短参数）")
	fs.StringVar(&options.SSHUser, "user", options.SSHUser, "SSH 用户，默认 root")
	fs.StringVar(&options.SSHUser, "u", options.SSHUser, "SSH 用户，默认 root")
	fs.StringVar(&options.SSHUser, "ssh-user", options.SSHUser, "SSH 用户，默认 root")
	fs.IntVar(&options.SSHPort, "ssh-port", options.SSHPort, "SSH 端口，默认 22；默认端口不用填")
	fs.StringVar(&options.SSHPassword, "password", options.SSHPassword, "SSH 密码；也可用 -p")
	fs.StringVar(&options.SSHPassword, "ssh-password", options.SSHPassword, "SSH 密码")
	fs.StringVar(&options.SSHPrivateKey, "key", options.SSHPrivateKey, "SSH 私钥路径；密码登录不用填")
	fs.StringVar(&options.SSHPrivateKey, "ssh-key", options.SSHPrivateKey, "SSH 私钥路径；密码登录不用填")
	fs.BoolVar(&options.UseSudo, "sudo", options.UseSudo, "普通 sudo 用户场景才需要；root 默认不用填")
}

func hasInlineInventoryInput(options inlineInventoryOptions) bool {
	return strings.TrimSpace(options.Host) != "" || strings.TrimSpace(options.Hosts) != ""
}

func printUsage(console *ui.Console) {
	console.Plain(`bootstrapctl - 主机初始化工具

只保留两条主线：

  1) 文件模式：先生成配置，再执行

     ./bootstrapctl
     # 修改 inventory.yaml 里的账号、密码、hostname、ip
     ./bootstrapctl apply

  2) 命令行模式：不写配置，一步执行

     ./bootstrapctl apply -H node-01=192.168.1.10 -u root -p '<密码>'
     ./bootstrapctl apply -H 'master01=10.0.0.1,node01=10.0.0.2' -u root -p '<密码>'

常用命令：

  init       生成当前目录 inventory.yaml；直接 ./bootstrapctl 等同于 init
  apply      正式执行初始化，默认读取当前目录 inventory.yaml
  plan       可选：只预览，不落变更
  scan       可选：扫描目标主机当前状态
  check      可选：只检查本地配置，不连接远端
  verify     可选：执行后校验
  version    查看版本

常用参数：

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

inventory.yaml 里优先改这几项：

  transport.ssh_user
  transport.ssh_password
  nodes[].hostname
  nodes[].ip

其它端口、sudo、ssh_auth、host_ip、跳板机、节点级账号等参数已经在模板下半部分说明；一般不用动。
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
