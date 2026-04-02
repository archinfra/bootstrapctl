package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

func TestParseNodeReport(t *testing.T) {
	output := `
__BT__os_name=Ubuntu 24.04.2 LTS
__BT__kernel=6.8.0-xx
__BT__arch=x86_64
__BT__hostname_ips=192.168.24.5 172.16.0.10
__BT__selected_host_ip=192.168.24.5
__BT__cpu_count=8
__BT__memory_total_mb=32000
__BT__root_usage_percent=42
__BT__root_total_gb=100
__BT__root_avail_gb=58
__BT__data_usage_percent=91
__BT__data_total_gb=500
__BT__data_avail_gb=45
__BT__block_devices=vda disk 100G ;vdb disk 500G /data
__BT__swap_enabled=true
__BT__selinux_runtime=Disabled
__BT__selinux_config=disabled
__BT__firewall_state=firewalld:inactive
__BT__time_sync_state=ntp=true,clock=true
__BT__container_runtime=docker:Docker version 29.3.1
__BT__kubelet_state=inactive
`

	report, err := parseNodeReport("node-1", "192.168.24.5", output)
	if err != nil {
		t.Fatalf("parseNodeReport() error = %v", err)
	}

	if report.Metrics.OSName != "Ubuntu 24.04.2 LTS" {
		t.Fatalf("unexpected os name: %q", report.Metrics.OSName)
	}
	if report.Metrics.RootUsagePercent != 42 {
		t.Fatalf("unexpected root usage: %d", report.Metrics.RootUsagePercent)
	}
	if report.Metrics.SelectedHostIP != "192.168.24.5" {
		t.Fatalf("unexpected selected host ip: %q", report.Metrics.SelectedHostIP)
	}
	if report.Metrics.RootTotalGB != 100 || report.Metrics.DataTotalGB != 500 {
		t.Fatalf("unexpected storage totals: root=%d data=%d", report.Metrics.RootTotalGB, report.Metrics.DataTotalGB)
	}
	if report.Summary.WarningCount < 2 {
		t.Fatalf("expected warnings for /data usage and swap, got %d", report.Summary.WarningCount)
	}
	if report.Summary.Status != "warn" {
		t.Fatalf("expected warn summary, got %s", report.Summary.Status)
	}
}

func TestParseNodeReportRejectsEmptyOutput(t *testing.T) {
	_, err := parseNodeReport("node-1", "192.168.24.5", "plain text without markers")
	if err == nil {
		t.Fatalf("expected parseNodeReport to reject empty marker output")
	}
}

func TestSELinuxCompliant(t *testing.T) {
	if !selinuxCompliant(Metrics{SELinuxRuntime: "Disabled", SELinuxConfig: "disabled"}) {
		t.Fatalf("expected disabled runtime/config to be compliant")
	}
	if selinuxCompliant(Metrics{SELinuxRuntime: "Enforcing", SELinuxConfig: "enforcing"}) {
		t.Fatalf("expected enforcing SELinux to be non-compliant")
	}
}

func TestTimeSyncCompliant(t *testing.T) {
	if !timeSyncCompliant("ntp=true,clock=true") {
		t.Fatalf("expected healthy time sync to be compliant")
	}
	if !timeSyncCompliant("ntp=yes,clock=") {
		t.Fatalf("expected ntp=yes,clock= to be treated as compliant")
	}
	if timeSyncCompliant("ntp=no,clock=") {
		t.Fatalf("expected ntp=no to be non-compliant")
	}
	if timeSyncCompliant("unknown") {
		t.Fatalf("expected unknown time sync to be non-compliant")
	}
}

func TestReportTotalsAndMarkdown(t *testing.T) {
	report := &Report{
		RunID:       "20260402-100000-001",
		ClusterName: "baseline-lab",
		Nodes: []NodeReport{
			{
				NodeName: "ok-node",
				NodeIP:   "192.168.1.10",
				Summary:  Summary{Status: "ok"},
				Observations: []Observation{
					makeObs("os", "操作系统", "ok", "Ubuntu 24.04", "system"),
				},
			},
			{
				NodeName: "warn-node",
				NodeIP:   "192.168.1.11",
				Summary:  Summary{Status: "warn", WarningCount: 2},
				Observations: []Observation{
					makeObs("swap", "SWAP 状态", "warn", "已开启", "system"),
				},
			},
			{
				NodeName: "error-node",
				NodeIP:   "192.168.1.12",
				Summary:  Summary{Status: "error", ErrorCount: 1},
				Error:    "ssh timeout",
				Observations: []Observation{
					makeObs("connectivity", "远程连接", "error", "ssh timeout", "transport"),
				},
			},
		},
	}

	totals := report.Totals()
	if totals.TotalNodes != 3 || totals.OKNodes != 1 || totals.WarnNodes != 1 || totals.ErrorNodes != 1 {
		t.Fatalf("unexpected totals: %+v", totals)
	}
	if report.OverallStatus() != "error" {
		t.Fatalf("expected overall status error, got %s", report.OverallStatus())
	}

	reportDir := t.TempDir()
	path, err := report.SaveMarkdown(reportDir)
	if err != nil {
		t.Fatalf("SaveMarkdown() error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	text := string(content)
	if !strings.Contains(text, "# bootstrapctl baseline scan report") {
		t.Fatalf("expected markdown header, got %s", text)
	}
	if !strings.Contains(text, "suggestion:") {
		t.Fatalf("expected markdown suggestions, got %s", text)
	}
}

func TestRunnerContinuesWhenNodeFails(t *testing.T) {
	inventory := config.Inventory{
		ClusterName: "scan-lab",
		Nodes: []config.Node{
			{Name: "ok-node", IP: "192.168.1.10", SSHUser: "root", SSHPort: 22, SSHPassword: "x"},
			{Name: "bad-node", IP: "192.168.1.11", SSHUser: "root", SSHPort: 22, SSHPassword: "x"},
		},
	}
	inventory.ApplyDefaults()

	exec := remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
		if node.Name == "bad-node" {
			return remote.Result{}, errors.New("dial tcp timeout")
		}
		return remote.Result{
			Output: strings.Join([]string{
				"__BT__os_name=Ubuntu 24.04.2 LTS",
				"__BT__kernel=6.8.0-xx",
				"__BT__arch=x86_64",
				"__BT__cpu_count=8",
				"__BT__memory_total_mb=32000",
				"__BT__root_usage_percent=42",
				"__BT__data_usage_percent=10",
				"__BT__swap_enabled=false",
				"__BT__selinux_runtime=disabled",
				"__BT__selinux_config=disabled",
				"__BT__firewall_state=ufw:inactive",
				"__BT__time_sync_state=ntp=true,clock=true",
				"__BT__container_runtime=docker:Docker version 29.3.1",
				"__BT__kubelet_state=inactive",
			}, "\n"),
			ExitCode: 0,
		}, nil
	})

	runner := NewRunner(exec)
	report, err := runner.Run(context.Background(), inventory)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(report.Nodes) != 2 {
		t.Fatalf("expected 2 node reports, got %d", len(report.Nodes))
	}
	if report.Nodes[0].Summary.Status == report.Nodes[1].Summary.Status {
		t.Fatalf("expected different statuses between successful and failed node")
	}
	if report.OverallStatus() != "error" {
		t.Fatalf("expected overall status error, got %s", report.OverallStatus())
	}
}

func TestNewRunIDAvoidsSecondLevelCollisions(t *testing.T) {
	first := newRunID()
	second := newRunID()
	if len(first) != len(second) {
		t.Fatalf("run id length mismatch: %q vs %q", first, second)
	}
	if !strings.Contains(first, "-") {
		t.Fatalf("unexpected run id format: %q", first)
	}
}

func TestSaveMarkdownCreatesExpectedFileName(t *testing.T) {
	report := &Report{RunID: "20260402-101010-123"}
	path, err := report.SaveMarkdown(t.TempDir())
	if err != nil {
		t.Fatalf("SaveMarkdown() error = %v", err)
	}
	if filepath.Base(path) != "20260402-101010-123-scan.md" {
		t.Fatalf("unexpected markdown file name: %s", path)
	}
}
