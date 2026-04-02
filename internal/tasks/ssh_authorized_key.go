package tasks

import (
	"context"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// SSHAuthorizedKeyTask 负责把当前执行机的公钥写入远端指定用户的 authorized_keys。
// 这是“控制端 -> 目标节点”免密的基础动作，也是后续跳板机链路的前提之一。
type SSHAuthorizedKeyTask struct {
	NodeSpec       config.NodeConnection
	AuthorizedUser string
	PublicKey      string
}

func (t *SSHAuthorizedKeyTask) Key() string   { return "ssh-authorized-key" }
func (t *SSHAuthorizedKeyTask) Title() string { return "分发控制端 SSH 公钥" }
func (t *SSHAuthorizedKeyTask) Node() string  { return t.NodeSpec.Name }

func (t *SSHAuthorizedKeyTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	return checkAuthorizedKey(ctx, exec, t.NodeSpec, t.AuthorizedUser, t.PublicKey)
}

func (t *SSHAuthorizedKeyTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	return applyAuthorizedKey(ctx, exec, t.NodeSpec, t.AuthorizedUser, t.PublicKey)
}
