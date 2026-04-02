package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// ManagedAdminUserTask 负责在远端创建受控运维账号，并把 sudo、公钥等能力收敛到位。
// 它默认不会启用，只有 profile 明确打开 managed_admin 时才会进入任务链。
type ManagedAdminUserTask struct {
	NodeSpec         config.NodeConnection
	Username         string
	Password         string
	PasswordHash     string
	Shell            string
	PrimaryGroup     string
	ExtraGroups      []string
	CreateHome       bool
	GrantSudo        bool
	SudoNoPasswd     bool
	InstallPublicKey bool
	ControllerPubKey string
}

func (t *ManagedAdminUserTask) Key() string   { return "managed-admin-user" }
func (t *ManagedAdminUserTask) Title() string { return "创建受控运维账号" }
func (t *ManagedAdminUserTask) Node() string  { return t.NodeSpec.Name }

func (t *ManagedAdminUserTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	result, err := runScript(ctx, exec, t.NodeSpec, t.renderCheckScript())
	if err != nil {
		return CheckResult{}, err
	}

	output := parseStatusLine(result.Output, "OK", "OK:", "CHANGE:", "ERROR:")
	switch {
	case strings.HasPrefix(output, "OK"):
		return CheckResult{
			Needed:  false,
			Summary: fmt.Sprintf("受控运维账号 %s 已就绪", t.Username),
		}, nil
	case strings.HasPrefix(output, "CHANGE:"):
		reason := strings.TrimSpace(strings.TrimPrefix(output, "CHANGE:"))
		if reason == "" {
			reason = "需要创建或收敛运维账号"
		}
		return CheckResult{
			Needed:  true,
			Summary: fmt.Sprintf("受控运维账号 %s 待收敛: %s", t.Username, reason),
		}, nil
	default:
		return CheckResult{}, fmt.Errorf("无法解析受控运维账号检查结果: %s", output)
	}
}

func (t *ManagedAdminUserTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	result, err := runScript(ctx, exec, t.NodeSpec, t.renderApplyScript())
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("创建受控运维账号失败: %s", strings.TrimSpace(result.Output))
	}

	if t.InstallPublicKey {
		if _, err := applyAuthorizedKey(ctx, exec, t.NodeSpec, t.Username, t.ControllerPubKey); err != nil {
			return ApplyResult{}, err
		}
	}

	return ApplyResult{
		Changed: true,
		Summary: fmt.Sprintf("受控运维账号 %s 已收敛完成", t.Username),
	}, nil
}

func (t *ManagedAdminUserTask) renderCheckScript() string {
	extraGroups := strings.Join(t.ExtraGroups, "\n")
	expectedSudo := ""
	if t.GrantSudo {
		if t.SudoNoPasswd {
			expectedSudo = fmt.Sprintf("%s ALL=(ALL) NOPASSWD:ALL", t.Username)
		} else {
			expectedSudo = fmt.Sprintf("%s ALL=(ALL) ALL", t.Username)
		}
	}

	passwordHashCheck := "0"
	if strings.TrimSpace(t.PasswordHash) != "" {
		passwordHashCheck = "1"
	}
	publicKeyCheck := "0"
	if t.InstallPublicKey && strings.TrimSpace(t.ControllerPubKey) != "" {
		publicKeyCheck = "1"
	}

	return fmt.Sprintf(`
set -e
user="%s"
shell_expected="%s"
primary_group="%s"
create_home="%t"
check_password_hash="%s"
expected_password_hash="$(printf '%%s' '%s' | base64 -d)"
check_public_key="%s"
public_key="$(printf '%%s' '%s' | base64 -d)"
groups_raw="$(printf '%%s' '%s' | base64 -d)"
expected_sudo="$(printf '%%s' '%s' | base64 -d)"

reasons=""

if ! id "$user" >/dev/null 2>&1; then
  echo "CHANGE:user-missing"
  exit 0
fi

home_dir="$(getent passwd "$user" | cut -d: -f6)"
current_shell="$(getent passwd "$user" | cut -d: -f7)"
current_group="$(id -gn "$user")"
current_groups="$(id -nG "$user")"

if [ -n "$shell_expected" ] && [ "$current_shell" != "$shell_expected" ]; then
  reasons="$reasons shell"
fi

if [ -n "$primary_group" ] && [ "$current_group" != "$primary_group" ]; then
  reasons="$reasons primary-group"
fi

if [ "$create_home" = "true" ] && [ ! -d "$home_dir" ]; then
  reasons="$reasons home"
fi

if [ -n "$groups_raw" ]; then
  while IFS= read -r group_name; do
    [ -z "$group_name" ] && continue
    if getent group "$group_name" >/dev/null 2>&1; then
      if ! printf '%%s\n' "$current_groups" | tr ' ' '\n' | grep -Fxq "$group_name"; then
        reasons="$reasons group:$group_name"
      fi
    fi
  done <<'EOF_GROUPS'
$groups_raw
EOF_GROUPS
fi

if [ -n "$expected_sudo" ]; then
  sudoers_path="/etc/sudoers.d/bootstrapctl-$user"
  if [ ! -f "$sudoers_path" ]; then
    reasons="$reasons sudoers"
  else
    actual_sudo="$(tr -d '\r' < "$sudoers_path" | sed '/^[[:space:]]*$/d')"
    if [ "$actual_sudo" != "$expected_sudo" ]; then
      reasons="$reasons sudoers"
    fi
  fi
fi

if [ "$check_password_hash" = "1" ]; then
  current_hash="$(getent shadow "$user" | cut -d: -f2)"
  if [ "$current_hash" != "$expected_password_hash" ]; then
    reasons="$reasons password-hash"
  fi
fi

if [ "$check_public_key" = "1" ]; then
  authorized_keys="$home_dir/.ssh/authorized_keys"
  if [ ! -f "$authorized_keys" ] || ! grep -Fqx "$public_key" "$authorized_keys"; then
    reasons="$reasons authorized-keys"
  fi
fi

if [ -n "$reasons" ]; then
  echo "CHANGE:${reasons# }"
else
  echo "OK"
fi
`, t.Username, t.Shell, t.PrimaryGroup, t.CreateHome, passwordHashCheck, encodeBase64(t.PasswordHash), publicKeyCheck, encodeBase64(t.ControllerPubKey), encodeBase64(extraGroups), encodeBase64(expectedSudo))
}

func (t *ManagedAdminUserTask) renderApplyScript() string {
	extraGroups := strings.Join(t.ExtraGroups, "\n")
	expectedSudo := ""
	if t.GrantSudo {
		if t.SudoNoPasswd {
			expectedSudo = fmt.Sprintf("%s ALL=(ALL) NOPASSWD:ALL", t.Username)
		} else {
			expectedSudo = fmt.Sprintf("%s ALL=(ALL) ALL", t.Username)
		}
	}

	passwordMode := "none"
	passwordValue := ""
	if strings.TrimSpace(t.PasswordHash) != "" {
		passwordMode = "hash"
		passwordValue = t.PasswordHash
	} else if strings.TrimSpace(t.Password) != "" {
		passwordMode = "plain"
		passwordValue = t.Password
	}

	return fmt.Sprintf(`
set -e
user="%s"
shell_expected="%s"
primary_group="%s"
create_home="%t"
password_mode="%s"
password_value="$(printf '%%s' '%s' | base64 -d)"
groups_raw="$(printf '%%s' '%s' | base64 -d)"
expected_sudo="$(printf '%%s' '%s' | base64 -d)"

create_home_flag="-m"
if [ "$create_home" != "true" ]; then
  create_home_flag="-M"
fi

if [ -z "$primary_group" ]; then
  primary_group="$user"
fi

getent group "$primary_group" >/dev/null 2>&1 || groupadd "$primary_group"

if ! id "$user" >/dev/null 2>&1; then
  useradd "$create_home_flag" -g "$primary_group" -s "$shell_expected" "$user"
fi

usermod -g "$primary_group" -s "$shell_expected" "$user"

if [ "$password_mode" = "hash" ] && [ -n "$password_value" ]; then
  usermod -p "$password_value" "$user"
fi

if [ "$password_mode" = "plain" ] && [ -n "$password_value" ]; then
  printf '%%s:%%s\n' "$user" "$password_value" | chpasswd
fi

if [ -n "$groups_raw" ]; then
  while IFS= read -r group_name; do
    [ -z "$group_name" ] && continue
    if getent group "$group_name" >/dev/null 2>&1; then
      usermod -aG "$group_name" "$user"
    fi
  done <<'EOF_GROUPS'
$groups_raw
EOF_GROUPS
fi

if [ -n "$expected_sudo" ]; then
  install -d -m 0750 /etc/sudoers.d
  sudoers_path="/etc/sudoers.d/bootstrapctl-$user"
  printf '%%s\n' "$expected_sudo" > "$sudoers_path"
  chmod 0440 "$sudoers_path"
  if command -v visudo >/dev/null 2>&1; then
    visudo -cf "$sudoers_path" >/dev/null
  fi
fi

echo "CHANGED"
`, t.Username, t.Shell, t.PrimaryGroup, t.CreateHome, passwordMode, encodeBase64(passwordValue), encodeBase64(extraGroups), encodeBase64(expectedSudo))
}
