package remote

import (
	"context"

	"github.com/yuanyp8/bootstrapctl/internal/config"
)

// Result 表示一次远程脚本执行的标准化结果。
type Result struct {
	Output   string
	ExitCode int
}

// Executor 抽象了远程执行能力，方便后续切换 SSH、WinRM 或本地 dry-run。
type Executor interface {
	Run(ctx context.Context, node config.NodeConnection, script string) (Result, error)
}

// ExecutorFunc 让测试可以直接用闭包模拟远程执行器。
type ExecutorFunc func(ctx context.Context, node config.NodeConnection, script string) (Result, error)

func (f ExecutorFunc) Run(ctx context.Context, node config.NodeConnection, script string) (Result, error) {
	return f(ctx, node, script)
}
