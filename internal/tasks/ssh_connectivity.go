package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

type SSHConnectivityTask struct {
	NodeSpec config.NodeConnection
}

func (t *SSHConnectivityTask) Key() string   { return "ssh-connectivity" }
func (t *SSHConnectivityTask) Title() string { return "检查 SSH 连通性" }
func (t *SSHConnectivityTask) Node() string  { return t.NodeSpec.Name }

func (t *SSHConnectivityTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	result, err := runScript(ctx, exec, t.NodeSpec, "echo bootstrapctl-ssh-ok")
	if err != nil {
		return CheckResult{}, err
	}
	if result.ExitCode != 0 || !strings.Contains(result.Output, "bootstrapctl-ssh-ok") {
		return CheckResult{}, fmt.Errorf("SSH 连通性检查失败: %s", strings.TrimSpace(result.Output))
	}
	return CheckResult{
		Needed:  false,
		Summary: "SSH 连通正常",
	}, nil
}

func (t *SSHConnectivityTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	return ApplyResult{
		Changed: false,
		Summary: "SSH 连通性任务无需变更",
	}, nil
}
