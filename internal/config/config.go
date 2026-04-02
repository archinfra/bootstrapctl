package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

// Inventory 描述“目标机器是谁、如何连接到它们”。
// 它与 Profile 的职责分离：
// - Inventory 负责节点清单与连接方式
// - Profile 负责初始化策略与目标状态
type Inventory struct {
	ClusterName string    `yaml:"cluster_name"`
	Transport   Transport `yaml:"transport"`
	Nodes       []Node    `yaml:"nodes"`
}

// Transport 描述 inventory 级别的默认 SSH 连接参数。
// 节点级配置可以覆盖这些默认值。
type Transport struct {
	SSHUser       string   `yaml:"ssh_user"`
	SSHPort       int      `yaml:"ssh_port"`
	SSHPassword   string   `yaml:"ssh_password"`
	SSHPrivateKey string   `yaml:"ssh_private_key"`
	UseSudo       bool     `yaml:"use_sudo"`
	Bastion       *Bastion `yaml:"bastion"`
}

// Bastion 描述跳板机配置。
type Bastion struct {
	Host          string `yaml:"host"`
	SSHUser       string `yaml:"ssh_user"`
	SSHPort       int    `yaml:"ssh_port"`
	SSHPassword   string `yaml:"ssh_password"`
	SSHPrivateKey string `yaml:"ssh_private_key"`
}

// Node 描述单台目标节点。
type Node struct {
	Name          string   `yaml:"name"`
	IP            string   `yaml:"ip"`
	HostIP        string   `yaml:"host_ip"`
	Roles         []string `yaml:"roles"`
	SSHUser       string   `yaml:"ssh_user"`
	SSHPort       int      `yaml:"ssh_port"`
	SSHPassword   string   `yaml:"ssh_password"`
	SSHPrivateKey string   `yaml:"ssh_private_key"`
	UseSudo       *bool    `yaml:"use_sudo"`
	Bastion       *Bastion `yaml:"bastion"`
}

// NodeConnection 是应用默认值后的运行时连接视图。
type NodeConnection struct {
	Name          string
	IP            string
	HostIP        string
	Roles         []string
	SSHUser       string
	SSHPort       int
	SSHPassword   string
	SSHPrivateKey string
	UseSudo       bool
	Bastion       *Bastion
}

// Profile 描述“要把节点初始化成什么样”。
type Profile struct {
	Name          string             `yaml:"name"`
	Features      FeatureFlags       `yaml:"features"`
	SSHKey        SSHKeyPolicy       `yaml:"ssh_key"`
	ManagedAdmin  ManagedAdminPolicy `yaml:"managed_admin"`
	Storage       Storage            `yaml:"storage"`
	Ulimit        Ulimit             `yaml:"ulimit"`
	KernelNetwork KernelNetwork      `yaml:"kernel_network"`
	Firewall      FirewallPolicy     `yaml:"firewall"`
}

// FeatureFlags 按能力开关任务。
// 使用 *bool 是为了区分：
// - 显式关闭 false
// - 未配置时采用默认值
type FeatureFlags struct {
	SSHConnectivity  *bool `yaml:"ssh_connectivity"`
	SSHAuthorizedKey *bool `yaml:"ssh_authorized_key"`
	ManagedAdmin     *bool `yaml:"managed_admin"`
	Hostname         *bool `yaml:"hostname"`
	HostsFile        *bool `yaml:"hosts_file"`
	DisableSwap      *bool `yaml:"disable_swap"`
	DisableSELinux   *bool `yaml:"disable_selinux"`
	Storage          *bool `yaml:"storage"`
	Ulimit           *bool `yaml:"ulimit"`
	KernelNetwork    *bool `yaml:"kernel_network"`
	Firewall         *bool `yaml:"firewall"`
}

// SSHKeyPolicy 描述控制端公钥如何分发到目标主机。
type SSHKeyPolicy struct {
	AuthorizedUser    string `yaml:"authorized_user"`
	PublicKeyPath     string `yaml:"public_key_path"`
	PublicKey         string `yaml:"public_key"`
	AutoGenerate      *bool  `yaml:"auto_generate"`
	GeneratedKeyPath  string `yaml:"generated_key_path"`
	EnableBastionHop  *bool  `yaml:"enable_bastion_hop"`
	BastionKeyPath    string `yaml:"bastion_key_path"`
	ResolvedPublicKey string `yaml:"-"`
}

// ManagedAdminPolicy 描述“创建一个受控运维账号，并逐步替代直接 root 登录”的策略。
// 这块能力默认关闭，只有显式开启后才会参与收敛。
type ManagedAdminPolicy struct {
	Username                   string   `yaml:"username"`
	Password                   string   `yaml:"password"`
	PasswordHash               string   `yaml:"password_hash"`
	Shell                      string   `yaml:"shell"`
	PrimaryGroup               string   `yaml:"primary_group"`
	ExtraGroups                []string `yaml:"extra_groups"`
	CreateHome                 *bool    `yaml:"create_home"`
	GrantSudo                  *bool    `yaml:"grant_sudo"`
	SudoNoPasswd               *bool    `yaml:"sudo_nopasswd"`
	InstallControllerPublicKey *bool    `yaml:"install_controller_public_key"`
	ControllerPublicKeyPath    string   `yaml:"controller_public_key_path"`
	ControllerPublicKey        string   `yaml:"controller_public_key"`
	DisableRootSSH             *bool    `yaml:"disable_root_ssh"`
	SSHDConfigPath             string   `yaml:"sshd_config_path"`
	ResolvedPublicKey          string   `yaml:"-"`
}

// Storage 定义容器数据目录。
type Storage struct {
	GraphRoot       string `yaml:"graph_root"`
	CRIRoot         string `yaml:"cri_root"`
	StorageConfPath string `yaml:"storage_conf_path"`
	RunRoot         string `yaml:"run_root"`
	GraphDriver     string `yaml:"graph_driver"`
}

// Ulimit 定义系统级文件句柄和进程数限制。
type Ulimit struct {
	NoFile int `yaml:"nofile"`
	NProc  int `yaml:"nproc"`
}

// KernelNetwork 描述 Kubernetes 节点依赖的内核模块和 sysctl 参数。
type KernelNetwork struct {
	Modules []string          `yaml:"modules"`
	Sysctls map[string]string `yaml:"sysctls"`
}

// FirewallPolicy 描述初始化阶段如何处理宿主机防火墙。
type FirewallPolicy struct {
	Mode            string `yaml:"mode"`
	ManageFirewalld *bool  `yaml:"manage_firewalld"`
	ManageUFW       *bool  `yaml:"manage_ufw"`
	RequireIPTables *bool  `yaml:"require_iptables"`
}

// LoadInventory 读取并校验 inventory。
func LoadInventory(path string) (Inventory, error) {
	var inventory Inventory

	content, err := os.ReadFile(path)
	if err != nil {
		return inventory, fmt.Errorf("读取 inventory 失败: %w", err)
	}
	if err := yaml.Unmarshal(content, &inventory); err != nil {
		return inventory, fmt.Errorf("解析 inventory 失败: %w", err)
	}

	inventory.ApplyDefaults()
	if err := inventory.Validate(); err != nil {
		return inventory, err
	}
	return inventory, nil
}

// LoadProfile 读取并校验 profile。
func LoadProfile(path string) (Profile, error) {
	var profile Profile

	content, err := os.ReadFile(path)
	if err != nil {
		return profile, fmt.Errorf("读取 profile 失败: %w", err)
	}
	if err := yaml.Unmarshal(content, &profile); err != nil {
		return profile, fmt.Errorf("解析 profile 失败: %w", err)
	}

	profile.ApplyDefaults()
	if err := profile.ResolveRuntime(); err != nil {
		return profile, err
	}
	return profile, profile.Validate()
}

// ApplyDefaults 填充 inventory 默认值。
func (i *Inventory) ApplyDefaults() {
	if strings.TrimSpace(i.ClusterName) == "" {
		i.ClusterName = "bootstrap-cluster"
	}
	if i.Transport.SSHUser == "" {
		i.Transport.SSHUser = "root"
	}
	if i.Transport.SSHPort == 0 {
		i.Transport.SSHPort = 22
	}
	if i.Transport.Bastion != nil {
		applyBastionDefaults(i.Transport.Bastion, i.Transport)
	}

	for idx := range i.Nodes {
		node := &i.Nodes[idx]
		if node.SSHUser == "" {
			node.SSHUser = i.Transport.SSHUser
		}
		if node.SSHPort == 0 {
			node.SSHPort = i.Transport.SSHPort
		}
		if node.SSHPassword == "" {
			node.SSHPassword = i.Transport.SSHPassword
		}
		if node.SSHPrivateKey == "" {
			node.SSHPrivateKey = i.Transport.SSHPrivateKey
		}
		if node.UseSudo == nil {
			useSudo := i.Transport.UseSudo
			node.UseSudo = &useSudo
		}
		if node.Bastion == nil && i.Transport.Bastion != nil {
			bastionCopy := *i.Transport.Bastion
			node.Bastion = &bastionCopy
		}
		if node.Bastion != nil {
			applyBastionDefaults(node.Bastion, i.Transport)
		}
	}
}

func applyBastionDefaults(bastion *Bastion, transport Transport) {
	if bastion == nil {
		return
	}

	if strings.TrimSpace(bastion.SSHUser) == "" {
		if transport.Bastion != nil && strings.TrimSpace(transport.Bastion.SSHUser) != "" {
			bastion.SSHUser = transport.Bastion.SSHUser
		} else {
			bastion.SSHUser = transport.SSHUser
		}
	}
	if bastion.SSHPort == 0 {
		if transport.Bastion != nil && transport.Bastion.SSHPort != 0 {
			bastion.SSHPort = transport.Bastion.SSHPort
		} else {
			bastion.SSHPort = 22
		}
	}
	if bastion.SSHPassword == "" {
		if transport.Bastion != nil && transport.Bastion.SSHPassword != "" {
			bastion.SSHPassword = transport.Bastion.SSHPassword
		} else {
			bastion.SSHPassword = transport.SSHPassword
		}
	}
	if bastion.SSHPrivateKey == "" {
		if transport.Bastion != nil && transport.Bastion.SSHPrivateKey != "" {
			bastion.SSHPrivateKey = transport.Bastion.SSHPrivateKey
		} else {
			bastion.SSHPrivateKey = transport.SSHPrivateKey
		}
	}
}

// Validate 确保 inventory 满足最基本的执行条件。
func (i Inventory) Validate() error {
	if len(i.Nodes) == 0 {
		return fmt.Errorf("inventory 中至少需要一台节点")
	}

	seenNames := map[string]struct{}{}
	seenIPs := map[string]struct{}{}
	for _, node := range i.Nodes {
		if strings.TrimSpace(node.Name) == "" {
			return fmt.Errorf("节点名称不能为空")
		}
		if strings.TrimSpace(node.IP) == "" {
			return fmt.Errorf("节点 %s 的 IP 不能为空", node.Name)
		}
		if _, ok := seenNames[node.Name]; ok {
			return fmt.Errorf("节点名称重复: %s", node.Name)
		}
		if _, ok := seenIPs[node.IP]; ok {
			return fmt.Errorf("节点 IP 重复: %s", node.IP)
		}
		seenNames[node.Name] = struct{}{}
		seenIPs[node.IP] = struct{}{}
	}
	return nil
}

// ResolveNodes 生成应用完默认值后的连接视图。
func (i Inventory) ResolveNodes() []NodeConnection {
	resolved := make([]NodeConnection, 0, len(i.Nodes))
	for _, node := range i.Nodes {
		resolved = append(resolved, NodeConnection{
			Name:          node.Name,
			IP:            node.IP,
			HostIP:        node.HostIP,
			Roles:         append([]string(nil), node.Roles...),
			SSHUser:       node.SSHUser,
			SSHPort:       node.SSHPort,
			SSHPassword:   node.SSHPassword,
			SSHPrivateKey: node.SSHPrivateKey,
			UseSudo:       node.UseSudo != nil && *node.UseSudo,
			Bastion:       node.Bastion,
		})
	}
	return resolved
}

// ApplyDefaults 填充 profile 默认值。
func (p *Profile) ApplyDefaults() {
	if strings.TrimSpace(p.Name) == "" {
		p.Name = "k8s-host-init"
	}

	setBoolDefault := func(v **bool, defaultValue bool) {
		if *v == nil {
			value := defaultValue
			*v = &value
		}
	}

	setBoolDefault(&p.Features.SSHConnectivity, true)
	setBoolDefault(&p.Features.SSHAuthorizedKey, false)
	setBoolDefault(&p.Features.ManagedAdmin, false)
	setBoolDefault(&p.Features.Hostname, true)
	setBoolDefault(&p.Features.HostsFile, true)
	setBoolDefault(&p.Features.DisableSwap, true)
	setBoolDefault(&p.Features.DisableSELinux, true)
	setBoolDefault(&p.Features.Storage, true)
	setBoolDefault(&p.Features.Ulimit, true)
	setBoolDefault(&p.Features.KernelNetwork, true)
	setBoolDefault(&p.Features.Firewall, true)

	if strings.TrimSpace(p.Storage.GraphRoot) == "" {
		p.Storage.GraphRoot = "/data/graphroot"
	}
	if strings.TrimSpace(p.Storage.CRIRoot) == "" {
		p.Storage.CRIRoot = "/data/containerd"
	}
	if strings.TrimSpace(p.Storage.StorageConfPath) == "" {
		p.Storage.StorageConfPath = "/etc/containers/storage.conf"
	}
	if strings.TrimSpace(p.Storage.RunRoot) == "" {
		p.Storage.RunRoot = "/run/containers/storage"
	}
	if strings.TrimSpace(p.Storage.GraphDriver) == "" {
		p.Storage.GraphDriver = "overlay"
	}

	setBoolDefault(&p.SSHKey.EnableBastionHop, true)
	setBoolDefault(&p.SSHKey.AutoGenerate, true)
	if strings.TrimSpace(p.SSHKey.GeneratedKeyPath) == "" {
		p.SSHKey.GeneratedKeyPath = "~/.ssh/bootstrapctl_ed25519"
	}
	if strings.TrimSpace(p.SSHKey.BastionKeyPath) == "" {
		p.SSHKey.BastionKeyPath = "~/.ssh/bootstrapctl_ed25519"
	}

	if p.Ulimit.NoFile == 0 {
		p.Ulimit.NoFile = 1048576
	}
	if p.Ulimit.NProc == 0 {
		p.Ulimit.NProc = 1048576
	}

	if len(p.KernelNetwork.Modules) == 0 {
		p.KernelNetwork.Modules = []string{"overlay", "br_netfilter"}
	}
	if len(p.KernelNetwork.Sysctls) == 0 {
		p.KernelNetwork.Sysctls = map[string]string{
			"net.ipv4.ip_forward":                 "1",
			"net.bridge.bridge-nf-call-iptables":  "1",
			"net.bridge.bridge-nf-call-ip6tables": "1",
		}
	}

	setBoolDefault(&p.Firewall.ManageFirewalld, true)
	setBoolDefault(&p.Firewall.ManageUFW, true)
	setBoolDefault(&p.Firewall.RequireIPTables, true)
	if strings.TrimSpace(p.Firewall.Mode) == "" {
		p.Firewall.Mode = "iptables"
	}

	if strings.TrimSpace(p.ManagedAdmin.Username) == "" {
		p.ManagedAdmin.Username = "opsadmin"
	}
	if strings.TrimSpace(p.ManagedAdmin.Shell) == "" {
		p.ManagedAdmin.Shell = "/bin/bash"
	}
	if strings.TrimSpace(p.ManagedAdmin.SSHDConfigPath) == "" {
		p.ManagedAdmin.SSHDConfigPath = "/etc/ssh/sshd_config"
	}
	if len(p.ManagedAdmin.ExtraGroups) == 0 {
		p.ManagedAdmin.ExtraGroups = []string{"sudo", "wheel"}
	}
	setBoolDefault(&p.ManagedAdmin.CreateHome, true)
	setBoolDefault(&p.ManagedAdmin.GrantSudo, true)
	setBoolDefault(&p.ManagedAdmin.SudoNoPasswd, true)
	setBoolDefault(&p.ManagedAdmin.InstallControllerPublicKey, true)
	setBoolDefault(&p.ManagedAdmin.DisableRootSSH, true)
}

// ResolveRuntime 负责解析运行时依赖的本地资源，例如控制端公钥。
func (p *Profile) ResolveRuntime() error {
	if p.Features.SSHAuthorizedKeyEnabled() {
		publicKey, resolvedPath, err := p.SSHKey.ResolvePublicKey()
		if err != nil {
			return err
		}
		p.SSHKey.ResolvedPublicKey = publicKey
		if strings.TrimSpace(p.SSHKey.PublicKeyPath) == "" {
			p.SSHKey.PublicKeyPath = resolvedPath
		}
	}

	if p.Features.ManagedAdminEnabled() && p.ManagedAdmin.InstallControllerPublicKeyEnabled() {
		publicKey, resolvedPath, err := p.ManagedAdmin.ResolveControllerPublicKey(p.SSHKey.ResolvedPublicKey)
		if err != nil {
			return err
		}
		p.ManagedAdmin.ResolvedPublicKey = publicKey
		if strings.TrimSpace(p.ManagedAdmin.ControllerPublicKeyPath) == "" && strings.TrimSpace(p.ManagedAdmin.ControllerPublicKey) == "" {
			p.ManagedAdmin.ControllerPublicKeyPath = resolvedPath
		}
	}

	return nil
}

// Validate 校验 profile 里的参数合法性。
func (p Profile) Validate() error {
	if p.Ulimit.NoFile < 1024 {
		return fmt.Errorf("nofile 不能小于 1024")
	}
	if p.Ulimit.NProc < 1024 {
		return fmt.Errorf("nproc 不能小于 1024")
	}
	if strings.TrimSpace(p.Storage.GraphRoot) == "" || strings.TrimSpace(p.Storage.CRIRoot) == "" {
		return fmt.Errorf("storage.graph_root 和 storage.cri_root 不能为空")
	}
	if strings.TrimSpace(p.Storage.StorageConfPath) == "" || strings.TrimSpace(p.Storage.RunRoot) == "" {
		return fmt.Errorf("storage.storage_conf_path 和 storage.run_root 不能为空")
	}
	if strings.TrimSpace(p.Storage.GraphDriver) == "" {
		return fmt.Errorf("storage.graph_driver 不能为空")
	}
	if strings.TrimSpace(p.Firewall.Mode) == "" {
		return fmt.Errorf("firewall.mode 不能为空")
	}
	for _, module := range p.KernelNetwork.Modules {
		if strings.TrimSpace(module) == "" {
			return fmt.Errorf("kernel_network.modules 不能包含空值")
		}
	}
	for key, value := range p.KernelNetwork.Sysctls {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return fmt.Errorf("kernel_network.sysctls 不能包含空 key 或空 value")
		}
	}
	if p.Features.SSHAuthorizedKeyEnabled() && strings.TrimSpace(p.SSHKey.ResolvedPublicKey) == "" {
		return fmt.Errorf("ssh_authorized_key 已开启，但未解析到有效公钥")
	}
	if p.Features.ManagedAdminEnabled() {
		if strings.TrimSpace(p.ManagedAdmin.Username) == "" {
			return fmt.Errorf("managed_admin.username 不能为空")
		}
		if p.ManagedAdmin.InstallControllerPublicKeyEnabled() && strings.TrimSpace(p.ManagedAdmin.ResolvedPublicKey) == "" {
			return fmt.Errorf("managed_admin 已开启控制端公钥分发，但未解析到有效公钥")
		}
		if strings.TrimSpace(p.ManagedAdmin.Password) != "" && strings.TrimSpace(p.ManagedAdmin.PasswordHash) != "" {
			return fmt.Errorf("managed_admin.password 与 managed_admin.password_hash 不能同时设置")
		}
	}
	return nil
}

func (f FeatureFlags) SSHConnectivityEnabled() bool {
	return f.SSHConnectivity != nil && *f.SSHConnectivity
}

func (f FeatureFlags) SSHAuthorizedKeyEnabled() bool {
	return f.SSHAuthorizedKey != nil && *f.SSHAuthorizedKey
}

func (f FeatureFlags) ManagedAdminEnabled() bool {
	return f.ManagedAdmin != nil && *f.ManagedAdmin
}

func (f FeatureFlags) HostnameEnabled() bool {
	return f.Hostname != nil && *f.Hostname
}

func (f FeatureFlags) HostsFileEnabled() bool {
	return f.HostsFile != nil && *f.HostsFile
}

func (f FeatureFlags) DisableSwapEnabled() bool {
	return f.DisableSwap != nil && *f.DisableSwap
}

func (f FeatureFlags) DisableSELinuxEnabled() bool {
	return f.DisableSELinux != nil && *f.DisableSELinux
}

func (f FeatureFlags) StorageEnabled() bool {
	return f.Storage != nil && *f.Storage
}

func (f FeatureFlags) UlimitEnabled() bool {
	return f.Ulimit != nil && *f.Ulimit
}

func (f FeatureFlags) KernelNetworkEnabled() bool {
	return f.KernelNetwork != nil && *f.KernelNetwork
}

func (f FeatureFlags) FirewallEnabled() bool {
	return f.Firewall != nil && *f.Firewall
}

// SortedModules / SortedSysctlKeys 让任务与测试可以稳定输出配置内容。
func (k KernelNetwork) SortedModules() []string {
	modules := append([]string(nil), k.Modules...)
	sort.Strings(modules)
	return modules
}

func (k KernelNetwork) SortedSysctlKeys() []string {
	keys := make([]string, 0, len(k.Sysctls))
	for key := range k.Sysctls {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (p FirewallPolicy) ManageFirewalldEnabled() bool {
	return p.ManageFirewalld != nil && *p.ManageFirewalld
}

func (p FirewallPolicy) ManageUFWEnabled() bool {
	return p.ManageUFW != nil && *p.ManageUFW
}

func (p FirewallPolicy) RequireIPTablesEnabled() bool {
	return p.RequireIPTables != nil && *p.RequireIPTables
}

func (p SSHKeyPolicy) EnableBastionHopEnabled() bool {
	return p.EnableBastionHop != nil && *p.EnableBastionHop
}

func (p SSHKeyPolicy) AutoGenerateEnabled() bool {
	return p.AutoGenerate != nil && *p.AutoGenerate
}

func (p ManagedAdminPolicy) CreateHomeEnabled() bool {
	return p.CreateHome != nil && *p.CreateHome
}

func (p ManagedAdminPolicy) GrantSudoEnabled() bool {
	return p.GrantSudo != nil && *p.GrantSudo
}

func (p ManagedAdminPolicy) SudoNoPasswdEnabled() bool {
	return p.SudoNoPasswd != nil && *p.SudoNoPasswd
}

func (p ManagedAdminPolicy) InstallControllerPublicKeyEnabled() bool {
	return p.InstallControllerPublicKey != nil && *p.InstallControllerPublicKey
}

func (p ManagedAdminPolicy) DisableRootSSHEnabled() bool {
	return p.DisableRootSSH != nil && *p.DisableRootSSH
}

// ResolvePublicKey 优先使用内联公钥，其次从显式路径或默认 SSH 公钥路径读取。
func (p SSHKeyPolicy) ResolvePublicKey() (publicKey string, resolvedPath string, err error) {
	return resolvePublicKey(p.PublicKey, p.PublicKeyPath, []string{
		expandHome("~/.ssh/id_ed25519.pub"),
		expandHome("~/.ssh/id_rsa.pub"),
	}, p.AutoGenerateEnabled(), p.GeneratedKeyPath)
}

// ResolveControllerPublicKey 解析运维账号应安装的控制端公钥。
// 如果显式未提供，则优先复用 SSHKeyPolicy 已解析出的控制端公钥。
func (p ManagedAdminPolicy) ResolveControllerPublicKey(fallbackPublicKey string) (publicKey string, resolvedPath string, err error) {
	if normalized := normalizePublicKey(p.ControllerPublicKey); normalized != "" {
		return normalized, "inline", nil
	}
	if strings.TrimSpace(p.ControllerPublicKeyPath) != "" {
		return resolvePublicKey("", p.ControllerPublicKeyPath, nil, false, "")
	}
	if normalized := normalizePublicKey(fallbackPublicKey); normalized != "" {
		return normalized, "ssh_key", nil
	}
	return resolvePublicKey("", "", []string{
		expandHome("~/.ssh/id_ed25519.pub"),
		expandHome("~/.ssh/id_rsa.pub"),
	}, true, "~/.ssh/bootstrapctl_ed25519")
}

func resolvePublicKey(inlineValue string, explicitPath string, fallbackPaths []string, autoGenerate bool, generatedKeyPath string) (publicKey string, resolvedPath string, err error) {
	if normalized := normalizePublicKey(inlineValue); normalized != "" {
		return normalized, "inline", nil
	}

	candidates := make([]string, 0, 1+len(fallbackPaths))
	if strings.TrimSpace(explicitPath) != "" {
		candidates = append(candidates, expandHome(explicitPath))
	} else {
		if autoGenerate && strings.TrimSpace(generatedKeyPath) != "" {
			candidates = append(candidates, resolveGeneratedPublicKeyPath(generatedKeyPath))
		} else {
			candidates = append(candidates, fallbackPaths...)
		}
	}

	for _, candidate := range candidates {
		content, readErr := os.ReadFile(candidate)
		if readErr != nil {
			continue
		}
		if normalized := normalizePublicKey(string(content)); normalized != "" {
			return normalized, candidate, nil
		}
	}

	if autoGenerate {
		privateKeyPath := resolveGeneratedPrivateKeyPath(explicitPath, generatedKeyPath)
		publicKey, resolvedPath, err := ensureGeneratedPublicKey(privateKeyPath)
		if err == nil {
			return publicKey, resolvedPath, nil
		}
		return "", "", fmt.Errorf("自动生成控制端 SSH 专用密钥失败: %w", err)
	}

	return "", "", fmt.Errorf("未找到可用的 SSH 公钥，请通过 public_key 或 public_key_path 提供")
}

func normalizePublicKey(value string) string {
	return strings.TrimSpace(value)
}

func resolveGeneratedPrivateKeyPath(explicitPath string, generatedKeyPath string) string {
	if strings.TrimSpace(explicitPath) != "" {
		expanded := expandHome(explicitPath)
		if strings.HasSuffix(expanded, ".pub") {
			return strings.TrimSuffix(expanded, ".pub")
		}
		return expanded
	}

	if strings.TrimSpace(generatedKeyPath) != "" {
		expanded := expandHome(generatedKeyPath)
		if strings.HasSuffix(expanded, ".pub") {
			return strings.TrimSuffix(expanded, ".pub")
		}
		return expanded
	}

	return expandHome("~/.ssh/bootstrapctl_ed25519")
}

func resolveGeneratedPublicKeyPath(generatedKeyPath string) string {
	privateKeyPath := resolveGeneratedPrivateKeyPath("", generatedKeyPath)
	return privateKeyPath + ".pub"
}

func ensureGeneratedPublicKey(privateKeyPath string) (publicKey string, resolvedPath string, err error) {
	privateKeyPath = expandHome(privateKeyPath)
	publicKeyPath := privateKeyPath + ".pub"

	if content, readErr := os.ReadFile(publicKeyPath); readErr == nil {
		if normalized := normalizePublicKey(string(content)); normalized != "" {
			return normalized, publicKeyPath, nil
		}
	}

	if privateContent, readErr := os.ReadFile(privateKeyPath); readErr == nil {
		privateKey, parseErr := ssh.ParseRawPrivateKey(privateContent)
		if parseErr == nil {
			publicKeyBytes, marshalErr := marshalAuthorizedKeyFromPrivate(privateKey)
			if marshalErr == nil {
				if writeErr := writeControllerKeyPair(privateKeyPath, privateContent, publicKeyBytes, false); writeErr == nil {
					return strings.TrimSpace(string(publicKeyBytes)), publicKeyPath, nil
				}
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(privateKeyPath), 0o700); err != nil {
		return "", "", err
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}

	comment := "bootstrapctl@controller"
	block, err := ssh.MarshalPrivateKey(privateKey, comment)
	if err != nil {
		return "", "", err
	}

	privateKeyBytes := pem.EncodeToMemory(block)
	publicKeyBytes, err := marshalAuthorizedKeyFromPrivate(privateKey)
	if err != nil {
		return "", "", err
	}

	if err := writeControllerKeyPair(privateKeyPath, privateKeyBytes, publicKeyBytes, true); err != nil {
		return "", "", err
	}

	return strings.TrimSpace(string(publicKeyBytes)), publicKeyPath, nil
}

func marshalAuthorizedKeyFromPrivate(privateKey interface{}) ([]byte, error) {
	switch key := privateKey.(type) {
	case ed25519.PrivateKey:
		publicKey, err := ssh.NewPublicKey(key.Public())
		if err != nil {
			return nil, err
		}
		return ssh.MarshalAuthorizedKey(publicKey), nil
	default:
		signer, err := ssh.NewSignerFromKey(privateKey)
		if err != nil {
			return nil, err
		}
		return ssh.MarshalAuthorizedKey(signer.PublicKey()), nil
	}
}

func writeControllerKeyPair(privateKeyPath string, privateKeyBytes []byte, publicKeyBytes []byte, writePrivate bool) error {
	publicKeyPath := privateKeyPath + ".pub"
	if writePrivate {
		if err := os.WriteFile(privateKeyPath, privateKeyBytes, 0o600); err != nil {
			return err
		}
	}
	return os.WriteFile(publicKeyPath, publicKeyBytes, 0o644)
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") && path != "~" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
}
