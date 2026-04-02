package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// FirewallTask 负责把宿主机防火墙收敛到适合 Kubernetes 初始化的状态。
// 当前策略是：
// - 关闭 firewalld / ufw
// - 保留 iptables 作为最终的规则控制入口
type FirewallTask struct {
	NodeSpec        config.NodeConnection
	Mode            string
	ManageFirewalld bool
	ManageUFW       bool
	RequireIPTables bool
}

func (t *FirewallTask) Key() string   { return "firewall" }
func (t *FirewallTask) Title() string { return "收敛宿主机防火墙" }
func (t *FirewallTask) Node() string  { return t.NodeSpec.Name }

func (t *FirewallTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	script := fmt.Sprintf(`
set -e
need=0
details=""

manage_firewalld="%t"
manage_ufw="%t"
require_iptables="%t"

if [ "$manage_firewalld" = "true" ] && command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q '^firewalld.service'; then
  firewalld_active="$(systemctl is-active firewalld 2>/dev/null || echo unknown)"
  firewalld_enabled="$(systemctl is-enabled firewalld 2>/dev/null || echo unknown)"
  if [ "$firewalld_active" = "active" ] || [ "$firewalld_enabled" = "enabled" ]; then
    need=1
    details="${details}firewalld(active=${firewalld_active},enabled=${firewalld_enabled}) "
  fi
fi

if [ "$manage_ufw" = "true" ]; then
  ufw_active="missing"
  ufw_enabled="missing"
  if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q '^ufw.service'; then
    ufw_active="$(systemctl is-active ufw 2>/dev/null || echo unknown)"
    ufw_enabled="$(systemctl is-enabled ufw 2>/dev/null || echo unknown)"
  fi
  ufw_status="inactive"
  if command -v ufw >/dev/null 2>&1; then
    ufw_status="$(ufw status 2>/dev/null | head -n 1 || echo inactive)"
  fi
  if [ "$ufw_active" = "active" ] || [ "$ufw_enabled" = "enabled" ] || printf '%%s' "$ufw_status" | grep -qi '^status: active'; then
    need=1
    details="${details}ufw(active=${ufw_active},enabled=${ufw_enabled},status=${ufw_status}) "
  fi
fi

if [ "$require_iptables" = "true" ]; then
  if ! command -v iptables >/dev/null 2>&1; then
    need=1
    details="${details}iptables(missing) "
  else
    backend="$(iptables -V 2>/dev/null || echo unknown)"
    details="${details}iptables-ready(${backend}) "
  fi
fi

if [ "$need" -eq 0 ]; then
  echo "OK:${details}"
else
  echo "CHANGE:${details}"
fi
`, t.ManageFirewalld, t.ManageUFW, t.RequireIPTables)

	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return CheckResult{}, err
	}

	output := parseStatusLine(result.Output, "OK:", "CHANGE:")
	if strings.HasPrefix(output, "OK:") {
		detail := strings.TrimSpace(strings.TrimPrefix(output, "OK:"))
		summary := "宿主机防火墙已处于收敛状态"
		if detail != "" {
			summary = fmt.Sprintf("%s，最终控制面: %s", summary, detail)
		}
		return CheckResult{Needed: false, Summary: summary}, nil
	}
	if strings.HasPrefix(output, "CHANGE:") {
		return CheckResult{
			Needed:  true,
			Summary: fmt.Sprintf("检测到需要处理的防火墙状态: %s", strings.TrimSpace(strings.TrimPrefix(output, "CHANGE:"))),
		}, nil
	}
	return CheckResult{}, fmt.Errorf("无法解析防火墙检查结果: %s", output)
}

func (t *FirewallTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	script := fmt.Sprintf(`
set -e
manage_firewalld="%t"
manage_ufw="%t"
require_iptables="%t"

if [ "$manage_firewalld" = "true" ] && command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q '^firewalld.service'; then
  systemctl stop firewalld || true
  systemctl disable firewalld || true
fi

if [ "$manage_ufw" = "true" ]; then
  if command -v ufw >/dev/null 2>&1; then
    ufw --force disable || true
  fi
  if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q '^ufw.service'; then
    systemctl stop ufw || true
    systemctl disable ufw || true
  fi
fi

if [ "$require_iptables" = "true" ] && ! command -v iptables >/dev/null 2>&1; then
  echo "ERROR:iptables-missing"
  exit 1
fi

echo "CHANGED"
`, t.ManageFirewalld, t.ManageUFW, t.RequireIPTables)

	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("处理宿主机防火墙失败: %s", strings.TrimSpace(result.Output))
	}

	summary := "宿主机防火墙已收敛，规则入口保留在 iptables"
	if !t.RequireIPTables {
		summary = "宿主机防火墙已收敛"
	}
	return ApplyResult{
		Changed: true,
		Summary: summary,
	}, nil
}
