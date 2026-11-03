package clouddrive

import (
	"context"
	"path"
	"regexp"
	"strings"
)

var sampleName = regexp.MustCompile(`(?i)(^|[\s._\-\[\]()])(sample|trailer|preview)([\s._\-\[\]()]|$)`)

func VideoFile(f File) bool {
	if f.Directory || f.Size <= 0 || f.ID == "" || f.PickCode == "" || sampleName.MatchString(strings.TrimSuffix(f.Name, path.Ext(f.Name))) {
		return false
	}
	switch strings.ToLower(path.Ext(f.Name)) {
	case ".mp4", ".mkv", ".avi", ".mov", ".m4v", ".ts", ".m2ts", ".wmv", ".webm", ".flv", ".mpg", ".mpeg":
		return true
	}
	return false
}

func providerName(names []string) string {
	if len(names) > 0 && strings.TrimSpace(names[0]) != "" {
		return strings.TrimSpace(names[0])
	}
	return "网盘"
}

func Discover(ctx context.Context, p Provider, root, workFolder string, name ...string) ([]File, error) {
	if root == "" || root == "0" || root == workFolder {
		return nil, &Error{Message: providerName(name) + " 未提供独立结果目录，拒绝扫描整个工作目录", Retryable: true}
	}
	first, err := p.Info(ctx, root)
	if err != nil {
		return nil, err
	}
	return DiscoverFrom(ctx, p, root, workFolder, first, name...)
}

// DiscoverFrom 复用本次请求的新鲜元数据；调用方仍需核验缓存目录祖先。
func DiscoverFrom(ctx context.Context, p Provider, root, workFolder string, first File, name ...string) ([]File, error) {
	brand := providerName(name)
	if root == "" || root == "0" || root == workFolder {
		return nil, &Error{Message: brand + " 未提供独立结果目录，拒绝扫描整个工作目录", Retryable: true}
	}
	if first.ID != root {
		return nil, &Error{Message: brand + " 返回的结果文件身份不匹配"}
	}
	first.Path = first.Name
	if !first.Directory {
		if VideoFile(first) {
			return []File{first}, nil
		}
		return nil, &Error{Message: "任务完成，但结果不是可用视频"}
	}
	type dir struct {
		id, prefix string
		depth      int
	}
	queue := []dir{{root, first.Name, 0}}
	seen := map[string]bool{root: true}
	files := []File{}
	scanned := 0
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		for offset := 0; ; {
			batch, more, err := p.List(ctx, d.id, offset)
			if err != nil {
				return nil, err
			}
			scanned += len(batch)
			if scanned > 10000 {
				return nil, &Error{Message: "结果目录超过 10000 项，扫描未完成"}
			}
			for _, f := range batch {
				if f.ID == "" || path.Base(f.Name) != f.Name || f.Name == "." || f.Name == ".." || strings.Contains(f.Name, "\\") {
					return nil, &Error{Message: brand + " 返回无效文件身份或名称"}
				}
				f.Path = path.Join(d.prefix, f.Name)
				if f.Directory {
					if seen[f.ID] || d.depth >= 16 || f.ID == workFolder || f.ID == "0" {
						return nil, &Error{Message: "结果目录循环或超过 16 层，扫描未完成"}
					}
					seen[f.ID] = true
					queue = append(queue, dir{f.ID, f.Path, d.depth + 1})
				} else if VideoFile(f) {
					if seen[f.ID] {
						return nil, &Error{Message: brand + " 分页重复文件，扫描未完成"}
					}
					seen[f.ID] = true
					files = append(files, f)
				}
			}
			if !more {
				break
			}
			if len(batch) == 0 {
				return nil, &Error{Message: brand + " 分页未前进"}
			}
			offset += len(batch)
		}
	}
	if len(files) == 0 {
		return nil, &Error{Message: "任务完成，但未找到可用视频（图片、压缩包和样片不作为正片）", Retryable: true, Missing: scanned == 0}
	}
	return files, nil
}
