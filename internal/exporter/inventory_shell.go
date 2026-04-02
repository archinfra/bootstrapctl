package exporter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
)

// RenderInventoryShell 把 inventory 渲染成可被 shell source 的数组文件。
// 这层主要服务于仍处于 Shell 时代的辅助脚本，例如 LVM、巡检或迁移脚本。
func RenderInventoryShell(inventory config.Inventory) string {
	nodes := inventory.ResolveNodes()
	builder := &strings.Builder{}

	fmt.Fprintln(builder, "#!/usr/bin/env bash")
	fmt.Fprintln(builder, "# bootstrapctl inventory-export --format shell")
	fmt.Fprintf(builder, "CLUSTER_NAME=%s\n", shellQuote(inventory.ClusterName))
	fmt.Fprintf(builder, "NODE_COUNT=%d\n", len(nodes))

	nodeNames := make([]string, 0, len(nodes))
	nodeIPs := make([]string, 0, len(nodes))
	nodeHostIPs := make([]string, 0, len(nodes))
	nodeRoles := make([]string, 0, len(nodes))
	nodeUsers := make([]string, 0, len(nodes))
	nodePorts := make([]string, 0, len(nodes))
	nodePasswords := make([]string, 0, len(nodes))
	nodePrivateKeys := make([]string, 0, len(nodes))
	nodeUseSudo := make([]string, 0, len(nodes))
	bastionHosts := make([]string, 0, len(nodes))
	bastionUsers := make([]string, 0, len(nodes))
	bastionPorts := make([]string, 0, len(nodes))
	bastionPasswords := make([]string, 0, len(nodes))
	bastionPrivateKeys := make([]string, 0, len(nodes))

	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
		nodeIPs = append(nodeIPs, node.IP)
		nodeHostIP := strings.TrimSpace(node.HostIP)
		if nodeHostIP == "" {
			nodeHostIP = node.IP
		}
		nodeHostIPs = append(nodeHostIPs, nodeHostIP)
		nodeRoles = append(nodeRoles, strings.Join(sortedCopy(node.Roles), ","))
		nodeUsers = append(nodeUsers, node.SSHUser)
		nodePorts = append(nodePorts, fmt.Sprintf("%d", node.SSHPort))
		nodePasswords = append(nodePasswords, node.SSHPassword)
		nodePrivateKeys = append(nodePrivateKeys, node.SSHPrivateKey)
		nodeUseSudo = append(nodeUseSudo, fmt.Sprintf("%t", node.UseSudo))

		if node.Bastion != nil {
			bastionHosts = append(bastionHosts, node.Bastion.Host)
			bastionUsers = append(bastionUsers, node.Bastion.SSHUser)
			bastionPorts = append(bastionPorts, fmt.Sprintf("%d", node.Bastion.SSHPort))
			bastionPasswords = append(bastionPasswords, node.Bastion.SSHPassword)
			bastionPrivateKeys = append(bastionPrivateKeys, node.Bastion.SSHPrivateKey)
		} else {
			bastionHosts = append(bastionHosts, "")
			bastionUsers = append(bastionUsers, "")
			bastionPorts = append(bastionPorts, "")
			bastionPasswords = append(bastionPasswords, "")
			bastionPrivateKeys = append(bastionPrivateKeys, "")
		}
	}

	writeShellArray(builder, "NODE_NAMES", nodeNames)
	writeShellArray(builder, "NODE_IPS", nodeIPs)
	writeShellArray(builder, "NODE_HOST_IPS", nodeHostIPs)
	writeShellArray(builder, "NODE_ROLES", nodeRoles)
	writeShellArray(builder, "NODE_SSH_USERS", nodeUsers)
	writeShellArray(builder, "NODE_SSH_PORTS", nodePorts)
	writeShellArray(builder, "NODE_SSH_PASSWORDS", nodePasswords)
	writeShellArray(builder, "NODE_SSH_PRIVATE_KEYS", nodePrivateKeys)
	writeShellArray(builder, "NODE_USE_SUDO", nodeUseSudo)
	writeShellArray(builder, "NODE_BASTION_HOSTS", bastionHosts)
	writeShellArray(builder, "NODE_BASTION_USERS", bastionUsers)
	writeShellArray(builder, "NODE_BASTION_PORTS", bastionPorts)
	writeShellArray(builder, "NODE_BASTION_PASSWORDS", bastionPasswords)
	writeShellArray(builder, "NODE_BASTION_PRIVATE_KEYS", bastionPrivateKeys)

	return builder.String()
}

func writeShellArray(builder *strings.Builder, key string, values []string) {
	builder.WriteString(key)
	builder.WriteString("=(")
	for idx, value := range values {
		if idx > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(shellQuote(value))
	}
	builder.WriteString(")\n")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
