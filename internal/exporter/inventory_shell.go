package exporter

import (
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
)

const (
	defaultMountDir  = "/data"
	defaultGraphBase = "/data/graphroot"
)

// RenderInventoryShell 把 inventory 渲染成旧版 ops-environment.sh 兼容格式。
// 这里故意只保留旧脚本真正依赖的核心变量，方便 LVM 和历史脚本直接 source。
func RenderInventoryShell(inventory config.Inventory) string {
	nodes := inventory.ResolveNodes()

	nodeNames := make([]string, 0, len(nodes))
	nodeIPs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
		nodeIPs = append(nodeIPs, node.IP)
	}

	builder := &strings.Builder{}
	fmt.Fprintln(builder, "#!/bin/bash")
	fmt.Fprintln(builder)
	fmt.Fprintln(builder, "# 由 bootstrapctl 自动生成。")
	fmt.Fprintln(builder, "# 如需更新，请重新执行 scan / plan / apply / verify 或 export-ops-env。")
	fmt.Fprintln(builder, "# 建议放置路径：/etc/profile.d/ops-environment.sh")
	fmt.Fprintln(builder)
	fmt.Fprintln(builder, "# SSH 密钥认证/免密准备开关（兼容旧脚本，默认 yes）")
	fmt.Fprintf(builder, "export SSH_AUTH=%s\n", shellValue(defaultSSHAuth(inventory)))
	fmt.Fprintln(builder)
	fmt.Fprintln(builder, "# 集群各机器 IP 数组")
	fmt.Fprintf(builder, "export NODE_IPS=(%s)\n", joinShellValues(nodeIPs))
	fmt.Fprintln(builder)
	fmt.Fprintln(builder, "# 集群各 IP 对应的主机名数组")
	fmt.Fprintf(builder, "export NODE_NAMES=(%s)\n", joinShellValues(nodeNames))
	fmt.Fprintln(builder)
	fmt.Fprintf(builder, "export MOUNT_DIR=%s\n", shellValue(defaultMountDir))
	fmt.Fprintf(builder, "export GRAPH_BASE=%s\n", shellValue(defaultGraphBase))
	return builder.String()
}

func joinShellValues(values []string) string {
	if len(values) == 0 {
		return ""
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, shellValue(value))
	}
	return strings.Join(result, " ")
}

func shellValue(value string) string {
	if value == "" {
		return "''"
	}
	if strings.ContainsAny(value, " \t'\"") {
		return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
	}
	return value
}

func defaultSSHAuth(inventory config.Inventory) string {
	value := strings.TrimSpace(inventory.Transport.SSHAuth)
	if value == "" {
		return "yes"
	}
	return value
}
