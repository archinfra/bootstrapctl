package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Report struct {
	RunID       string       `json:"run_id"`
	Command     string       `json:"command"`
	ClusterName string       `json:"cluster_name"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  time.Time    `json:"finished_at"`
	DryRun      bool         `json:"dry_run"`
	Results     []TaskResult `json:"results"`
}

type TaskResult struct {
	Node       string    `json:"node"`
	TaskKey    string    `json:"task_key"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	Summary    string    `json:"summary"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

func New(command, clusterName string, dryRun bool) *Report {
	return &Report{
		RunID:       strings.ReplaceAll(time.Now().Format("20060102-150405.000"), ".", "-"),
		Command:     command,
		ClusterName: clusterName,
		StartedAt:   time.Now(),
		DryRun:      dryRun,
	}
}

func (r *Report) Add(result TaskResult) {
	r.Results = append(r.Results, result)
}

func (r *Report) Finalize() {
	r.FinishedAt = time.Now()
}

func (r *Report) SaveJSON(reportDir string) (string, error) {
	r.Finalize()
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("创建报告目录失败: %w", err)
	}
	path := filepath.Join(reportDir, r.RunID+".json")
	content, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("生成 JSON 报告失败: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("写入报告失败: %w", err)
	}
	return path, nil
}
