package runner

import (
	"context"
	"testing"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
	"github.com/yuanyp8/bootstrapctl/internal/report"
	"github.com/yuanyp8/bootstrapctl/internal/tasks"
	"github.com/yuanyp8/bootstrapctl/internal/ui"
)

type fakeTask struct {
	key      string
	title    string
	node     string
	check    tasks.CheckResult
	checkErr error
	apply    tasks.ApplyResult
	applyErr error
}

func (f fakeTask) Key() string   { return f.key }
func (f fakeTask) Title() string { return f.title }
func (f fakeTask) Node() string  { return f.node }
func (f fakeTask) Check(ctx context.Context, exec remote.Executor) (tasks.CheckResult, error) {
	return f.check, f.checkErr
}
func (f fakeTask) Apply(ctx context.Context, exec remote.Executor) (tasks.ApplyResult, error) {
	return f.apply, f.applyErr
}

func newEngine() *Engine {
	return &Engine{
		Executor: remote.ExecutorFunc(func(ctx context.Context, node config.NodeConnection, script string) (remote.Result, error) {
			return remote.Result{}, nil
		}),
		Console: ui.NewConsole(),
	}
}

func TestEnginePlanRecordsNeedsChange(t *testing.T) {
	rep := report.New("plan", "demo", false)
	err := newEngine().Run(context.Background(), tasks.ModePlan, []tasks.Task{
		fakeTask{
			key:   "hostname",
			title: "设置主机名",
			node:  "node-1",
			check: tasks.CheckResult{Needed: true, Summary: "需要修改"},
		},
	}, false, rep)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != "needs-change" {
		t.Fatalf("expected needs-change result, got %+v", rep.Results)
	}
}

func TestEngineApplyDryRunDoesNotInvokeApply(t *testing.T) {
	rep := report.New("apply", "demo", true)
	err := newEngine().Run(context.Background(), tasks.ModeApply, []tasks.Task{
		fakeTask{
			key:   "swap",
			title: "关闭 SWAP",
			node:  "node-1",
			check: tasks.CheckResult{Needed: true, Summary: "需要关闭"},
		},
	}, true, rep)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != "would-change" {
		t.Fatalf("expected would-change result, got %+v", rep.Results)
	}
}

func TestEngineApplyChanged(t *testing.T) {
	rep := report.New("apply", "demo", false)
	err := newEngine().Run(context.Background(), tasks.ModeApply, []tasks.Task{
		fakeTask{
			key:   "ulimit",
			title: "写入 ulimit 配置",
			node:  "node-1",
			check: tasks.CheckResult{Needed: true, Summary: "需要更新"},
			apply: tasks.ApplyResult{Changed: true, Summary: "已写入"},
		},
	}, false, rep)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != "changed" {
		t.Fatalf("expected changed result, got %+v", rep.Results)
	}
}
