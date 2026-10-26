package scanner

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"

	"emby-go/internal/store"
)

const FailureSampleLimit = 20

type Failure struct {
	Source string `json:"source"`
	Path   string `json:"path"`
	Stage  string `json:"stage"`
	Error  string `json:"error"`
}

func failedCandidate(lib store.Library, source, path, stage string, err error) Result {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		path = pathErr.Path
	}
	if path == "" {
		path = source
	}
	failure := Failure{Source: source, Path: path, Stage: stage, Error: err.Error()}
	// Keep every failure in the runtime log, even when task samples are full.
	slog.Warn("扫描条目处理失败，保留原索引待下轮重试", "library_id", lib.ID,
		"directory", filepath.Dir(path), "source", source, "path", path, "stage", stage, "error", err)
	return Result{Failed: 1, Failures: []Failure{failure}}
}

func (result *Result) addFailures(failures []Failure) {
	remaining := FailureSampleLimit - len(result.Failures)
	if remaining > 0 {
		result.Failures = append(result.Failures, failures[:min(remaining, len(failures))]...)
	}
}

// AddFailureSamples merges diagnostics without changing the caller's counters.
func (result *Result) AddFailureSamples(other Result) {
	result.addFailures(other.Failures)
}

func (result Result) FailureError(operation, root string) error {
	if result.Failed == 0 {
		return nil
	}
	var message strings.Builder
	fmt.Fprintf(&message, "%s有 %d 个条目处理失败: %s", operation, result.Failed, root)
	fmt.Fprintf(&message, "\n失败明细（显示 %d / %d 条；全部失败项见运行日志）：", len(result.Failures), result.Failed)
	for index, failure := range result.Failures {
		fmt.Fprintf(&message, "\n\n%d. 目录: %s\n文件: %s", index+1, filepath.Dir(failure.Path), failure.Path)
		if failure.Source != failure.Path {
			fmt.Fprintf(&message, "\n关联 STRM: %s", failure.Source)
		}
		fmt.Fprintf(&message, "\n阶段: %s\n原因: %s", failure.Stage, failure.Error)
	}
	return errors.New(message.String())
}
