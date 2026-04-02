package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuanyp8/bootstrapctl/internal/config"
	"github.com/yuanyp8/bootstrapctl/internal/remote"
)

// StorageLayoutTask 负责收敛容器运行时相关目录和 containers/storage 配置。
// 它不仅创建 graphroot / cri root 目录，还会写 legacy init.sh 同源的
// /etc/containers/storage.conf，保证 graphroot 真正被运行时配置消费。
type StorageLayoutTask struct {
	NodeSpec        config.NodeConnection
	GraphRoot       string
	CRIRoot         string
	StorageConfPath string
	RunRoot         string
	GraphDriver     string
}

func (t *StorageLayoutTask) Key() string   { return "storage-layout" }
func (t *StorageLayoutTask) Title() string { return "准备容器存储目录与 graphroot 配置" }
func (t *StorageLayoutTask) Node() string  { return t.NodeSpec.Name }

func (t *StorageLayoutTask) Check(ctx context.Context, exec remote.Executor) (CheckResult, error) {
	expectedGraphRoot := t.effectiveContainersGraphRoot()
	expectedConfigB64 := encodeBase64(t.renderStorageConfig())
	script := fmt.Sprintf(`
set -e
expected_config="$(printf '%%s' '%s' | base64 -d)"
current_config=""
if [ -f "%s" ]; then
  current_config="$(cat "%s")"
fi

missing=()
[ -d "%s" ] || missing+=("graph-root")
[ -d "%s" ] || missing+=("containers-graphroot")
[ -d "%s" ] || missing+=("cri-root")
[ -d "%s" ] || missing+=("runroot")
[ -d "$(dirname "%s")" ] || missing+=("containers-conf-dir")

if [ "$current_config" != "$expected_config" ]; then
  missing+=("storage-conf")
fi

if [ ${#missing[@]} -eq 0 ]; then
  echo "OK"
else
  printf 'CHANGE:%%s\n' "${missing[*]}"
fi
`, expectedConfigB64, t.StorageConfPath, t.StorageConfPath, t.GraphRoot, expectedGraphRoot, t.CRIRoot, t.RunRoot, t.StorageConfPath)

	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return CheckResult{}, err
	}

	output := parseStatusLine(result.Output, "OK", "CHANGE:")
	if output == "OK" {
		return CheckResult{Needed: false, Summary: "容器存储目录和 graphroot 配置已收敛"}, nil
	}
	if strings.HasPrefix(output, "CHANGE:") {
		return CheckResult{
			Needed:  true,
			Summary: fmt.Sprintf("容器存储仍需收敛: %s", strings.TrimPrefix(output, "CHANGE:")),
		}, nil
	}
	return CheckResult{}, fmt.Errorf("无法解析存储目录检查结果: %s", output)
}

func (t *StorageLayoutTask) Apply(ctx context.Context, exec remote.Executor) (ApplyResult, error) {
	expectedGraphRoot := t.effectiveContainersGraphRoot()
	expectedConfigB64 := encodeBase64(t.renderStorageConfig())
	script := fmt.Sprintf(`
set -e
expected_config="$(printf '%%s' '%s' | base64 -d)"

mkdir -p "%s" "%s" "%s" "%s" "$(dirname "%s")"
chmod 755 "%s" "%s" "%s" "%s"
printf '%%s\n' "$expected_config" > "%s"
chmod 644 "%s"

echo "CHANGED"
`, expectedConfigB64, t.GraphRoot, expectedGraphRoot, t.CRIRoot, t.RunRoot, t.StorageConfPath, t.GraphRoot, expectedGraphRoot, t.CRIRoot, t.RunRoot, t.StorageConfPath, t.StorageConfPath)

	result, err := runScript(ctx, exec, t.NodeSpec, script)
	if err != nil {
		return ApplyResult{}, err
	}
	if result.ExitCode != 0 {
		return ApplyResult{}, fmt.Errorf("收敛容器存储配置失败: %s", strings.TrimSpace(result.Output))
	}
	return ApplyResult{
		Changed: true,
		Summary: "容器存储目录和 graphroot 配置已完成",
	}, nil
}

func (t *StorageLayoutTask) effectiveContainersGraphRoot() string {
	return strings.TrimRight(t.GraphRoot, "/") + "/containers/storage"
}

func (t *StorageLayoutTask) renderStorageConfig() string {
	return renderContainersStorageConfig(t.GraphDriver, t.RunRoot, t.effectiveContainersGraphRoot())
}
