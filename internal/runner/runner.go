package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/yuanyp8/bootstrapctl/internal/remote"
	"github.com/yuanyp8/bootstrapctl/internal/report"
	"github.com/yuanyp8/bootstrapctl/internal/tasks"
	"github.com/yuanyp8/bootstrapctl/internal/ui"
)

type Engine struct {
	Executor remote.Executor
	Console  *ui.Console
}

// Run 是任务引擎的统一入口。
// 它负责：
// 1. 对每个任务先执行 Check
// 2. 根据执行模式决定是仅展示、校验漂移还是正式 Apply
// 3. 将结果写入统一报告
func (e *Engine) Run(ctx context.Context, mode tasks.Mode, taskList []tasks.Task, dryRun bool, rep *report.Report) error {
	for _, task := range taskList {
		started := time.Now()
		e.Console.Info("[%s/%s] %s", task.Node(), task.Key(), task.Title())

		check, err := task.Check(ctx, e.Executor)
		if err != nil {
			rep.Add(report.TaskResult{
				Node:       task.Node(),
				TaskKey:    task.Key(),
				Title:      task.Title(),
				Status:     "failed",
				Summary:    err.Error(),
				StartedAt:  started,
				FinishedAt: time.Now(),
			})
			return fmt.Errorf("任务 %s 失败: %w", task.Key(), err)
		}

		switch mode {
		case tasks.ModePlan:
			status := "ok"
			if check.Needed {
				status = "needs-change"
				e.Console.Warn("[%s/%s] %s", task.Node(), task.Key(), check.Summary)
			} else {
				e.Console.Success("[%s/%s] %s", task.Node(), task.Key(), check.Summary)
			}
			rep.Add(report.TaskResult{
				Node:       task.Node(),
				TaskKey:    task.Key(),
				Title:      task.Title(),
				Status:     status,
				Summary:    check.Summary,
				StartedAt:  started,
				FinishedAt: time.Now(),
			})
		case tasks.ModeVerify:
			status := "ok"
			if check.Needed {
				status = "drift"
				e.Console.Warn("[%s/%s] %s", task.Node(), task.Key(), check.Summary)
			} else {
				e.Console.Success("[%s/%s] %s", task.Node(), task.Key(), check.Summary)
			}
			rep.Add(report.TaskResult{
				Node:       task.Node(),
				TaskKey:    task.Key(),
				Title:      task.Title(),
				Status:     status,
				Summary:    check.Summary,
				StartedAt:  started,
				FinishedAt: time.Now(),
			})
		case tasks.ModeApply:
			if !check.Needed {
				e.Console.Success("[%s/%s] %s", task.Node(), task.Key(), check.Summary)
				rep.Add(report.TaskResult{
					Node:       task.Node(),
					TaskKey:    task.Key(),
					Title:      task.Title(),
					Status:     "ok",
					Summary:    check.Summary,
					StartedAt:  started,
					FinishedAt: time.Now(),
				})
				continue
			}

			if dryRun {
				e.Console.Warn("[%s/%s] dry-run: %s", task.Node(), task.Key(), check.Summary)
				rep.Add(report.TaskResult{
					Node:       task.Node(),
					TaskKey:    task.Key(),
					Title:      task.Title(),
					Status:     "would-change",
					Summary:    check.Summary,
					StartedAt:  started,
					FinishedAt: time.Now(),
				})
				continue
			}

			applyResult, err := task.Apply(ctx, e.Executor)
			if err != nil {
				rep.Add(report.TaskResult{
					Node:       task.Node(),
					TaskKey:    task.Key(),
					Title:      task.Title(),
					Status:     "failed",
					Summary:    err.Error(),
					StartedAt:  started,
					FinishedAt: time.Now(),
				})
				return fmt.Errorf("任务 %s 执行失败: %w", task.Key(), err)
			}
			status := "ok"
			if applyResult.Changed {
				status = "changed"
			}
			e.Console.Success("[%s/%s] %s", task.Node(), task.Key(), applyResult.Summary)
			rep.Add(report.TaskResult{
				Node:       task.Node(),
				TaskKey:    task.Key(),
				Title:      task.Title(),
				Status:     status,
				Summary:    applyResult.Summary,
				StartedAt:  started,
				FinishedAt: time.Now(),
			})
		default:
			return fmt.Errorf("不支持的执行模式: %s", mode)
		}
	}

	return nil
}
