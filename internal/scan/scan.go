package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// Report 表示一批节点的基线扫描结果。
type Report struct {
	RunID       string       `json:"run_id"`
	ClusterName string       `json:"cluster_name"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  time.Time    `json:"finished_at"`
	Nodes       []NodeReport `json:"nodes"`
}

// Totals 是整批扫描的汇总统计。
type Totals struct {
	TotalNodes    int `json:"total_nodes"`
	OKNodes       int `json:"ok_nodes"`
	WarnNodes     int `json:"warn_nodes"`
	ErrorNodes    int `json:"error_nodes"`
	TotalWarnings int `json:"total_warnings"`
	TotalErrors   int `json:"total_errors"`
}

// NodeReport 表示单台节点的扫描结果。
type NodeReport struct {
	NodeName     string        `json:"node_name"`
	NodeIP       string        `json:"node_ip"`
	StartedAt    time.Time     `json:"started_at"`
	FinishedAt   time.Time     `json:"finished_at"`
	Error        string        `json:"error,omitempty"`
	Summary      Summary       `json:"summary"`
	Observations []Observation `json:"observations"`
	Metrics      Metrics       `json:"metrics"`
}

// Summary 是节点级汇总结果。
type Summary struct {
	Status       string `json:"status"`
	WarningCount int    `json:"warning_count"`
	ErrorCount   int    `json:"error_count"`
}

// Observation 表示一条面向人类阅读的检查结论。
type Observation struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Category string `json:"category"`
}

// Metrics 保存结构化扫描指标。
type Metrics struct {
	OSName           string `json:"os_name"`
	Kernel           string `json:"kernel"`
	Arch             string `json:"arch"`
	HostnameIPs      string `json:"hostname_ips"`
	SelectedHostIP   string `json:"selected_host_ip"`
	CPUCount         int    `json:"cpu_count"`
	MemoryTotalMB    int    `json:"memory_total_mb"`
	RootUsagePercent int    `json:"root_usage_percent"`
	RootTotalGB      int    `json:"root_total_gb"`
	RootAvailGB      int    `json:"root_avail_gb"`
	DataUsagePercent int    `json:"data_usage_percent"`
	DataTotalGB      int    `json:"data_total_gb"`
	DataAvailGB      int    `json:"data_avail_gb"`
	BlockDevices     string `json:"block_devices"`
	SwapEnabled      bool   `json:"swap_enabled"`
	SELinuxRuntime   string `json:"selinux_runtime"`
	SELinuxConfig    string `json:"selinux_config"`
	FirewallState    string `json:"firewall_state"`
	IPTablesCommand  string `json:"iptables_command"`
	IPTablesBackend  string `json:"iptables_backend"`
	TimeSyncState    string `json:"time_sync_state"`
	ContainerRuntime string `json:"container_runtime"`
	KubeletState     string `json:"kubelet_state"`
	OverlayModule    string `json:"overlay_module"`
	BrNetfilter      string `json:"br_netfilter_module"`
	IPForward        string `json:"ip_forward"`
	BridgeNFIptables string `json:"bridge_nf_call_iptables"`
}

// Runner 负责对 inventory 中的节点执行扫描。
type Runner struct {
	Executor remote.Executor
}

func NewRunner(executor remote.Executor) *Runner {
	return &Runner{Executor: executor}
}

// Run 会遍历全部节点；单节点失败不会中断整批扫描。
func (r *Runner) Run(ctx context.Context, inventory config.Inventory) (*Report, error) {
	report := &Report{
		RunID:       newRunID(),
		ClusterName: inventory.ClusterName,
		StartedAt:   time.Now(),
	}

	for _, node := range inventory.ResolveNodes() {
		started := time.Now()

		raw, err := r.Executor.Run(ctx, node, baselineScript())
		if err != nil {
			report.Nodes = append(report.Nodes, NodeReport{
				NodeName:   node.Name,
				NodeIP:     node.IP,
				StartedAt:  started,
				FinishedAt: time.Now(),
				Error:      err.Error(),
				Summary: Summary{
					Status:     "error",
					ErrorCount: 1,
				},
				Observations: []Observation{
					makeObs("connectivity", "远程连接", "error", err.Error(), "transport"),
				},
			})
			continue
		}

		nodeReport, err := parseNodeReport(node.Name, node.IP, raw.Output)
		if err != nil {
			report.Nodes = append(report.Nodes, NodeReport{
				NodeName:   node.Name,
				NodeIP:     node.IP,
				StartedAt:  started,
				FinishedAt: time.Now(),
				Error:      err.Error(),
				Summary: Summary{
					Status:     "error",
					ErrorCount: 1,
				},
				Observations: []Observation{
					makeObs("parse", "解析扫描结果", "error", err.Error(), "scan"),
				},
			})
			continue
		}

		nodeReport.StartedAt = started
		nodeReport.FinishedAt = time.Now()
		report.Nodes = append(report.Nodes, nodeReport)
	}

	report.FinishedAt = time.Now()
	return report, nil
}

func (r *Report) SaveJSON(reportDir string) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("创建扫描报告目录失败: %w", err)
	}

	path := filepath.Join(reportDir, r.RunID+"-scan.json")
	content, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化扫描报告失败: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("写入扫描报告失败: %w", err)
	}
	return path, nil
}

// SaveMarkdown 生成更适合人工阅读的 Markdown 报告。
func (r *Report) SaveMarkdown(reportDir string) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create scan report directory failed: %w", err)
	}

	var builder strings.Builder
	totals := r.Totals()

	builder.WriteString("# bootstrapctl baseline scan report\n\n")
	builder.WriteString(fmt.Sprintf("- run id: `%s`\n", r.RunID))
	builder.WriteString(fmt.Sprintf("- cluster: `%s`\n", r.ClusterName))
	builder.WriteString(fmt.Sprintf("- started at: `%s`\n", r.StartedAt.Format(time.RFC3339)))
	builder.WriteString(fmt.Sprintf("- finished at: `%s`\n", r.FinishedAt.Format(time.RFC3339)))
	builder.WriteString(fmt.Sprintf("- overall status: `%s`\n\n", r.OverallStatus()))

	builder.WriteString("## summary\n\n")
	builder.WriteString(fmt.Sprintf("- total nodes: `%d`\n", totals.TotalNodes))
	builder.WriteString(fmt.Sprintf("- ok nodes: `%d`\n", totals.OKNodes))
	builder.WriteString(fmt.Sprintf("- warn nodes: `%d`\n", totals.WarnNodes))
	builder.WriteString(fmt.Sprintf("- error nodes: `%d`\n", totals.ErrorNodes))
	builder.WriteString(fmt.Sprintf("- warning items: `%d`\n", totals.TotalWarnings))
	builder.WriteString(fmt.Sprintf("- error items: `%d`\n\n", totals.TotalErrors))

	for _, node := range r.Nodes {
		builder.WriteString(fmt.Sprintf("## %s (%s)\n\n", node.NodeName, node.NodeIP))
		builder.WriteString(fmt.Sprintf("- status: `%s`\n", node.Summary.Status))
		builder.WriteString(fmt.Sprintf("- started at: `%s`\n", node.StartedAt.Format(time.RFC3339)))
		builder.WriteString(fmt.Sprintf("- finished at: `%s`\n", node.FinishedAt.Format(time.RFC3339)))
		if strings.TrimSpace(node.Error) != "" {
			builder.WriteString(fmt.Sprintf("- error: `%s`\n", node.Error))
		}

		builder.WriteString("\n### metrics\n\n")
		builder.WriteString(fmt.Sprintf("- os: `%s`\n", node.Metrics.OSName))
		builder.WriteString(fmt.Sprintf("- kernel: `%s`\n", node.Metrics.Kernel))
		builder.WriteString(fmt.Sprintf("- arch: `%s`\n", node.Metrics.Arch))
		builder.WriteString(fmt.Sprintf("- hostname -I: `%s`\n", node.Metrics.HostnameIPs))
		builder.WriteString(fmt.Sprintf("- selected host ip: `%s`\n", node.Metrics.SelectedHostIP))
		builder.WriteString(fmt.Sprintf("- cpu count: `%d`\n", node.Metrics.CPUCount))
		builder.WriteString(fmt.Sprintf("- memory total mb: `%d`\n", node.Metrics.MemoryTotalMB))
		builder.WriteString(fmt.Sprintf("- root usage: `%d%%`\n", node.Metrics.RootUsagePercent))
		builder.WriteString(fmt.Sprintf("- root total gb: `%d`\n", node.Metrics.RootTotalGB))
		builder.WriteString(fmt.Sprintf("- root avail gb: `%d`\n", node.Metrics.RootAvailGB))
		builder.WriteString(fmt.Sprintf("- data usage: `%d%%`\n", node.Metrics.DataUsagePercent))
		builder.WriteString(fmt.Sprintf("- data total gb: `%d`\n", node.Metrics.DataTotalGB))
		builder.WriteString(fmt.Sprintf("- data avail gb: `%d`\n", node.Metrics.DataAvailGB))
		builder.WriteString(fmt.Sprintf("- block devices: `%s`\n", node.Metrics.BlockDevices))
		builder.WriteString(fmt.Sprintf("- firewall: `%s`\n", node.Metrics.FirewallState))
		builder.WriteString(fmt.Sprintf("- iptables: `%s`\n", node.Metrics.IPTablesBackend))
		builder.WriteString(fmt.Sprintf("- runtime: `%s`\n", node.Metrics.ContainerRuntime))
		builder.WriteString(fmt.Sprintf("- kubelet: `%s`\n\n", node.Metrics.KubeletState))

		builder.WriteString("### observations\n\n")
		for _, obs := range node.Observations {
			builder.WriteString(fmt.Sprintf("- [%s][%s] %s: %s\n", strings.ToUpper(obs.Status), obs.Category, obs.Title, obs.Detail))
			if obs.Status != "ok" {
				if suggestion := suggestionForObservation(obs); suggestion != "" {
					builder.WriteString(fmt.Sprintf("  - suggestion: %s\n", suggestion))
				}
			}
		}
		builder.WriteString("\n")
	}

	path := filepath.Join(reportDir, r.RunID+"-scan.md")
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return "", fmt.Errorf("write markdown scan report failed: %w", err)
	}
	return path, nil
}

func (r *Report) Totals() Totals {
	totals := Totals{TotalNodes: len(r.Nodes)}
	for _, node := range r.Nodes {
		totals.TotalWarnings += node.Summary.WarningCount
		totals.TotalErrors += node.Summary.ErrorCount
		switch node.Summary.Status {
		case "ok":
			totals.OKNodes++
		case "warn":
			totals.WarnNodes++
		default:
			totals.ErrorNodes++
		}
	}
	return totals
}

func (r *Report) OverallStatus() string {
	totals := r.Totals()
	if totals.ErrorNodes > 0 || totals.TotalErrors > 0 {
		return "error"
	}
	if totals.WarnNodes > 0 || totals.TotalWarnings > 0 {
		return "warn"
	}
	return "ok"
}

func baselineScript() string {
	return `
set -e

kv() {
  printf '__BT__%s=%s\n' "$1" "$2"
}

os_name="$(. /etc/os-release 2>/dev/null; echo "${PRETTY_NAME:-unknown}")"
kernel="$(uname -r 2>/dev/null || echo unknown)"
arch="$(uname -m 2>/dev/null || echo unknown)"
hostname_ips="$(hostname -I 2>/dev/null | xargs echo || true)"
selected_host_ip=""
for ip in $hostname_ips; do
  case "$ip" in
    127.*|::1)
      continue
      ;;
    *)
      selected_host_ip="$ip"
      break
      ;;
  esac
done
[ -n "$selected_host_ip" ] || selected_host_ip="missing"
cpu_count="$(nproc 2>/dev/null || echo 0)"
memory_total_mb="$(awk '/MemTotal/ {printf "%d", $2/1024}' /proc/meminfo 2>/dev/null || echo 0)"
root_usage_percent="$(df -P / | awk 'NR==2 {gsub("%","",$5); print $5}' 2>/dev/null || echo 0)"
root_total_gb="$(df -BG / | awk 'NR==2 {gsub("G","",$2); print $2}' 2>/dev/null || echo 0)"
root_avail_gb="$(df -BG / | awk 'NR==2 {gsub("G","",$4); print $4}' 2>/dev/null || echo 0)"
data_usage_percent="$(df -P /data 2>/dev/null | awk 'NR==2 {gsub("%","",$5); print $5}' || true)"
[ -n "$data_usage_percent" ] || data_usage_percent="0"
data_total_gb="$(df -BG /data 2>/dev/null | awk 'NR==2 {gsub("G","",$2); print $2}' || true)"
[ -n "$data_total_gb" ] || data_total_gb="0"
data_avail_gb="$(df -BG /data 2>/dev/null | awk 'NR==2 {gsub("G","",$4); print $4}' || true)"
[ -n "$data_avail_gb" ] || data_avail_gb="0"
block_devices="$(lsblk -o NAME,TYPE,SIZE,MOUNTPOINT -nr 2>/dev/null | sed 's/[[:space:]]\+/ /g' | paste -sd ';' - || true)"
[ -n "$block_devices" ] || block_devices="unknown"

swap_enabled="false"
if [ "$(swapon --noheadings --show=NAME 2>/dev/null | wc -l | tr -d ' ')" != "0" ]; then
  swap_enabled="true"
fi

selinux_runtime="disabled"
selinux_config="missing"
if command -v getenforce >/dev/null 2>&1; then
  selinux_runtime="$(getenforce 2>/dev/null || echo disabled)"
fi
if [ -f /etc/selinux/config ]; then
  selinux_config="$(awk -F= '/^SELINUX=/{print $2; found=1} END{if(!found) print "missing"}' /etc/selinux/config)"
fi

firewall_state="unknown"
if command -v systemctl >/dev/null 2>&1; then
  if systemctl list-unit-files 2>/dev/null | grep -q '^firewalld.service'; then
    firewall_state="firewalld:$(systemctl is-active firewalld 2>/dev/null || echo unknown)"
  elif systemctl list-unit-files 2>/dev/null | grep -q '^ufw.service'; then
    firewall_state="ufw:$(systemctl is-active ufw 2>/dev/null || echo unknown)"
  else
    firewall_state="not-installed"
  fi
fi

iptables_command="missing"
iptables_backend="unknown"
if command -v iptables >/dev/null 2>&1; then
  iptables_command="present"
  iptables_backend="$(iptables -V 2>/dev/null || echo unknown)"
fi

time_sync_state="unknown"
if command -v timedatectl >/dev/null 2>&1; then
  sync="$(timedatectl show -p NTPSynchronized --value 2>/dev/null || echo unknown)"
  state="$(timedatectl show -p SystemClockSynchronized --value 2>/dev/null || echo unknown)"
  time_sync_state="ntp=$sync,clock=$state"
fi

container_runtime="missing"
if command -v docker >/dev/null 2>&1; then
  container_runtime="docker:$(docker --version 2>/dev/null | head -n 1)"
elif command -v containerd >/dev/null 2>&1; then
  container_runtime="containerd:present"
fi

overlay_module="missing"
br_netfilter_module="missing"
if command -v lsmod >/dev/null 2>&1; then
  if lsmod | awk '$1=="overlay"{found=1} END{exit !found}'; then
    overlay_module="loaded"
  fi
  if lsmod | awk '$1=="br_netfilter"{found=1} END{exit !found}'; then
    br_netfilter_module="loaded"
  fi
fi

ip_forward="$(sysctl -n net.ipv4.ip_forward 2>/dev/null || echo missing)"
bridge_nf_call_iptables="$(sysctl -n net.bridge.bridge-nf-call-iptables 2>/dev/null || echo missing)"

kubelet_state="missing"
if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q '^kubelet.service'; then
  kubelet_state="$(systemctl is-active kubelet 2>/dev/null || echo inactive)"
fi

kv os_name "$os_name"
kv kernel "$kernel"
kv arch "$arch"
kv hostname_ips "$hostname_ips"
kv selected_host_ip "$selected_host_ip"
kv cpu_count "$cpu_count"
kv memory_total_mb "$memory_total_mb"
kv root_usage_percent "$root_usage_percent"
kv root_total_gb "$root_total_gb"
kv root_avail_gb "$root_avail_gb"
kv data_usage_percent "$data_usage_percent"
kv data_total_gb "$data_total_gb"
kv data_avail_gb "$data_avail_gb"
kv block_devices "$block_devices"
kv swap_enabled "$swap_enabled"
kv selinux_runtime "$selinux_runtime"
kv selinux_config "$selinux_config"
kv firewall_state "$firewall_state"
kv iptables_command "$iptables_command"
kv iptables_backend "$iptables_backend"
kv time_sync_state "$time_sync_state"
kv container_runtime "$container_runtime"
kv kubelet_state "$kubelet_state"
kv overlay_module "$overlay_module"
kv br_netfilter_module "$br_netfilter_module"
kv ip_forward "$ip_forward"
kv bridge_nf_call_iptables "$bridge_nf_call_iptables"
`
}

func parseNodeReport(name, ip, output string) (NodeReport, error) {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "__BT__") {
			continue
		}
		line = strings.TrimPrefix(line, "__BT__")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		values[parts[0]] = parts[1]
	}
	if len(values) == 0 {
		return NodeReport{}, fmt.Errorf("未解析到任何基线扫描字段")
	}

	nodeReport := NodeReport{
		NodeName: name,
		NodeIP:   ip,
		Metrics: Metrics{
			OSName:           values["os_name"],
			Kernel:           values["kernel"],
			Arch:             values["arch"],
			HostnameIPs:      values["hostname_ips"],
			SelectedHostIP:   values["selected_host_ip"],
			CPUCount:         mustInt(values["cpu_count"]),
			MemoryTotalMB:    mustInt(values["memory_total_mb"]),
			RootUsagePercent: mustInt(values["root_usage_percent"]),
			RootTotalGB:      mustInt(values["root_total_gb"]),
			RootAvailGB:      mustInt(values["root_avail_gb"]),
			DataUsagePercent: mustInt(values["data_usage_percent"]),
			DataTotalGB:      mustInt(values["data_total_gb"]),
			DataAvailGB:      mustInt(values["data_avail_gb"]),
			BlockDevices:     values["block_devices"],
			SwapEnabled:      values["swap_enabled"] == "true",
			SELinuxRuntime:   values["selinux_runtime"],
			SELinuxConfig:    values["selinux_config"],
			FirewallState:    values["firewall_state"],
			IPTablesCommand:  values["iptables_command"],
			IPTablesBackend:  values["iptables_backend"],
			TimeSyncState:    values["time_sync_state"],
			ContainerRuntime: values["container_runtime"],
			KubeletState:     values["kubelet_state"],
			OverlayModule:    values["overlay_module"],
			BrNetfilter:      values["br_netfilter_module"],
			IPForward:        values["ip_forward"],
			BridgeNFIptables: values["bridge_nf_call_iptables"],
		},
	}

	nodeReport.Observations = append(nodeReport.Observations,
		makeObs("os", "操作系统", "ok", nodeReport.Metrics.OSName, "system"),
		makeObs("kernel", "内核版本", "ok", nodeReport.Metrics.Kernel, "system"),
		makeObs("arch", "系统架构", "ok", nodeReport.Metrics.Arch, "system"),
		makeObs("host-ip", "主机可见 IP", "ok", fmt.Sprintf("selected=%s, hostname -I=%s", nodeReport.Metrics.SelectedHostIP, nodeReport.Metrics.HostnameIPs), "network"),
		makeObs("cpu", "CPU 核数", "ok", fmt.Sprintf("%d", nodeReport.Metrics.CPUCount), "capacity"),
		makeObs("memory", "内存总量(MB)", "ok", fmt.Sprintf("%d", nodeReport.Metrics.MemoryTotalMB), "capacity"),
		makeObs("storage-capacity", "磁盘容量概览", "ok", fmt.Sprintf("root(total=%dGB,avail=%dGB), data(total=%dGB,avail=%dGB)", nodeReport.Metrics.RootTotalGB, nodeReport.Metrics.RootAvailGB, nodeReport.Metrics.DataTotalGB, nodeReport.Metrics.DataAvailGB), "storage"),
		makeObs("block-devices", "块设备摘要", "ok", nodeReport.Metrics.BlockDevices, "storage"),
	)

	addStatusObs := func(key, title, detail, category string, warn bool) {
		status := "ok"
		if warn {
			status = "warn"
			nodeReport.Summary.WarningCount++
		}
		nodeReport.Observations = append(nodeReport.Observations, makeObs(key, title, status, detail, category))
	}

	addStatusObs("root-disk", "根分区使用率", fmt.Sprintf("%d%%", nodeReport.Metrics.RootUsagePercent), "storage", nodeReport.Metrics.RootUsagePercent >= 85)
	addStatusObs("data-disk", "/data 使用率", fmt.Sprintf("%d%%", nodeReport.Metrics.DataUsagePercent), "storage", nodeReport.Metrics.DataUsagePercent >= 85)
	addStatusObs("swap", "SWAP 状态", boolText(nodeReport.Metrics.SwapEnabled, "已开启", "已关闭"), "system", nodeReport.Metrics.SwapEnabled)
	addStatusObs("selinux", "SELinux 运行态", fmt.Sprintf("runtime=%s, config=%s", nodeReport.Metrics.SELinuxRuntime, nodeReport.Metrics.SELinuxConfig), "system", !selinuxCompliant(nodeReport.Metrics))
	addStatusObs("firewall", "防火墙与规则入口", fmt.Sprintf("firewall=%s, iptables=%s", nodeReport.Metrics.FirewallState, nodeReport.Metrics.IPTablesBackend), "network", !firewallCompliant(nodeReport.Metrics))
	addStatusObs("time-sync", "时间同步状态", nodeReport.Metrics.TimeSyncState, "time", !timeSyncCompliant(nodeReport.Metrics.TimeSyncState))
	addStatusObs("runtime", "容器运行时", nodeReport.Metrics.ContainerRuntime, "runtime", strings.Contains(nodeReport.Metrics.ContainerRuntime, "missing"))
	addStatusObs(
		"kernel-network",
		"Kubernetes 内核网络条件",
		fmt.Sprintf(
			"overlay=%s, br_netfilter=%s, ip_forward=%s, bridge-nf-call-iptables=%s",
			nodeReport.Metrics.OverlayModule,
			nodeReport.Metrics.BrNetfilter,
			nodeReport.Metrics.IPForward,
			nodeReport.Metrics.BridgeNFIptables,
		),
		"kernel",
		!kernelNetworkCompliant(nodeReport.Metrics),
	)
	addStatusObs("kubelet", "kubelet 状态", nodeReport.Metrics.KubeletState, "kubernetes", nodeReport.Metrics.KubeletState == "failed")

	nodeReport.Summary.Status = "ok"
	if nodeReport.Summary.WarningCount > 0 {
		nodeReport.Summary.Status = "warn"
	}
	return nodeReport, nil
}

func makeObs(key, title, status, detail, category string) Observation {
	return Observation{
		Key:      key,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Category: category,
	}
}

func mustInt(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}

func boolText(value bool, trueText, falseText string) string {
	if value {
		return trueText
	}
	return falseText
}

func selinuxCompliant(m Metrics) bool {
	runtime := strings.ToLower(strings.TrimSpace(m.SELinuxRuntime))
	config := strings.ToLower(strings.TrimSpace(m.SELinuxConfig))
	return runtime == "disabled" && (config == "disabled" || config == "missing" || config == "")
}

func firewallCompliant(m Metrics) bool {
	firewallState := strings.ToLower(strings.TrimSpace(m.FirewallState))
	if strings.Contains(firewallState, "firewalld:active") || strings.Contains(firewallState, "ufw:active") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m.IPTablesCommand), "present")
}

func timeSyncCompliant(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || normalized == "unknown" {
		return false
	}
	if strings.Contains(normalized, "ntp=false") || strings.Contains(normalized, "ntp=no") {
		return false
	}
	if strings.Contains(normalized, "clock=false") || strings.Contains(normalized, "clock=no") {
		return false
	}
	if (strings.Contains(normalized, "ntp=true") || strings.Contains(normalized, "ntp=yes")) &&
		(strings.HasSuffix(normalized, "clock=") || strings.Contains(normalized, "clock=unknown")) {
		return true
	}
	if strings.Contains(normalized, "clock=") && !strings.Contains(normalized, "clock=true") && !strings.Contains(normalized, "clock=yes") {
		return false
	}
	return true
}

func kernelNetworkCompliant(m Metrics) bool {
	return strings.EqualFold(strings.TrimSpace(m.OverlayModule), "loaded") &&
		strings.EqualFold(strings.TrimSpace(m.BrNetfilter), "loaded") &&
		strings.TrimSpace(m.IPForward) == "1" &&
		strings.TrimSpace(m.BridgeNFIptables) == "1"
}

func newRunID() string {
	return strings.ReplaceAll(time.Now().Format("20060102-150405.000"), ".", "-")
}

func suggestionForObservation(obs Observation) string {
	switch obs.Key {
	case "connectivity":
		return "检查目标节点 SSH 端口、认证信息以及跳板机转发策略；如使用内网节点，请确认跳板机允许继续访问目标地址。"
	case "parse":
		return "检查远端 shell 环境和命令输出是否异常，必要时保留原始输出再做排查。"
	case "root-disk", "data-disk":
		return "建议尽快清理磁盘或扩容，避免后续安装 Kubernetes、容器运行时或镜像缓存时空间不足。"
	case "swap":
		return "建议在 Kubernetes 初始化前关闭 SWAP，并校验 /etc/fstab 中没有自动挂载配置。"
	case "selinux":
		return "请确认当前 SELinux 策略与平台要求一致；如要求关闭，建议纳入初始化流程统一收口。"
	case "firewall":
		return "推荐停用 firewalld 和 ufw，把最终规则入口统一收口到 iptables，并确认 iptables 命令可用。"
	case "time-sync":
		return "建议启用 chronyd 或 systemd-timesyncd，保证证书、日志和集群控制面的时间一致。"
	case "runtime":
		return "如该节点计划承载容器或 Kubernetes 工作负载，请安装并验证 Docker 或 containerd。"
	case "kernel-network":
		return "建议提前加载 overlay、br_netfilter 模块，并设置 net.ipv4.ip_forward=1、net.bridge.bridge-nf-call-iptables=1。"
	case "kubelet":
		return "如果该节点应加入 Kubernetes 集群，请进一步检查 kubelet 服务状态和日志。"
	default:
		return ""
	}
}
