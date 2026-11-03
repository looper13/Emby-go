package cloudguangya

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"emby-go/internal/clouddrive"
)

type offlineTask struct {
	ID       scalar  `json:"taskId"`
	Resource string  `json:"res"`
	URL      string  `json:"url"`
	Hash     string  `json:"infoHash"`
	FileID   scalar  `json:"fileId"`
	ParentID *scalar `json:"parentId"`
	Status   scalar  `json:"status"`
	Progress scalar  `json:"progress"`
}

func normalizeHash(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != 32 && len(value) != 40 {
		return "", errors.New("光鸭任务 InfoHash 格式不正确")
	}
	_, hash, err := clouddrive.NormalizeMagnet("magnet:?xt=urn:btih:" + value)
	return hash, err
}

func (t offlineTask) hash() (string, error) {
	hash := ""
	if t.Hash != "" {
		var err error
		hash, err = normalizeHash(t.Hash)
		if err != nil {
			return "", &clouddrive.Error{Message: "光鸭任务 InfoHash 格式不正确", Retryable: true}
		}
	}
	for _, resource := range []string{t.Resource, t.URL} {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resource)), "magnet:") {
			continue
		}
		_, found, err := clouddrive.NormalizeMagnet(resource)
		if err != nil || hash != "" && hash != found {
			return "", &clouddrive.Error{Message: "光鸭任务返回冲突或无效磁力身份", Retryable: true}
		}
		hash = found
	}
	if hash == "" && t.Resource == "" && t.URL == "" {
		return "", &clouddrive.Error{Message: "光鸭任务缺少可核验的资源身份，未重复提交", Retryable: true}
	}
	return hash, nil
}

func taskStatus(raw scalar) (int, error) {
	value, err := raw.integer()
	if err != nil {
		return 0, &clouddrive.Error{Message: "光鸭任务状态格式不正确", Retryable: true}
	}
	switch value {
	case 0, 1:
		return int(value), nil
	case 2, 5:
		return 2, nil
	case 3, 4, -1:
		return -1, nil
	default:
		return 0, &clouddrive.Error{Message: "光鸭任务返回未知状态", Retryable: true}
	}
}

// listTaskLocked 完整分页后才能认定目标任务不存在，避免重复创建云下载。
func (c *Client) listTaskLocked(ctx context.Context, hash string) (*offlineTask, error) {
	var matched *offlineTask
	seenTasks := map[string]bool{}
	seenCursors := map[string]bool{}
	cursor := ""
	useCursor := false
	count := int64(0)
	for page := 0; page < maxPages; page++ {
		body := map[string]any{"pageSize": pageSize, "status": []int{0, 1, 2, 3, 4, 5}}
		if useCursor {
			body["cursor"] = cursor
		} else {
			body["page"] = page
		}
		var result struct {
			Total  *scalar       `json:"total"`
			Cursor *scalar       `json:"cursor"`
			List   []offlineTask `json:"list"`
		}
		if err := c.requestLocked(ctx, http.MethodPost, apiBase+taskAPI+"list_task", body, false, &result); err != nil {
			return nil, err
		}
		if result.List == nil || len(result.List) > pageSize {
			return nil, &clouddrive.Error{Message: "光鸭任务列表不完整，未重复提交", Retryable: true}
		}
		for i := range result.List {
			task := result.List[i]
			if !validID(string(task.ID)) || seenTasks[string(task.ID)] {
				return nil, &clouddrive.Error{Message: "光鸭任务分页返回重复或无效身份，未重复提交", Retryable: true}
			}
			seenTasks[string(task.ID)] = true
			found, err := task.hash()
			if err != nil {
				return nil, err
			}
			if found != hash {
				continue
			}
			if matched != nil {
				return nil, &clouddrive.Error{Message: "光鸭存在多个相同磁力任务，无法确定独立结果目录", Retryable: true}
			}
			matched = &task
		}
		count += int64(len(result.List))
		more := len(result.List) == pageSize
		if result.Total != nil {
			total, err := result.Total.integer()
			if err != nil || total < count {
				return nil, &clouddrive.Error{Message: "光鸭任务分页总数不正确，未重复提交", Retryable: true}
			}
			more = count < total
		}
		if result.Cursor != nil {
			next := string(*result.Cursor)
			if next != "" {
				if seenCursors[next] {
					return nil, &clouddrive.Error{Message: "光鸭任务游标分页未前进，未重复提交", Retryable: true}
				}
				seenCursors[next], useCursor, cursor = true, true, next
				more = true
			} else if result.Total == nil {
				more = false
			} else if more {
				return nil, &clouddrive.Error{Message: "光鸭任务游标提前结束，未重复提交", Retryable: true}
			}
		}
		if !more {
			return matched, nil
		}
		if len(result.List) == 0 {
			return nil, &clouddrive.Error{Message: "光鸭任务分页未前进，未重复提交", Retryable: true}
		}
	}
	return nil, &clouddrive.Error{Message: "光鸭任务超过分页限制，无法完整对账，未重复提交", Retryable: true}
}

func (c *Client) Find(ctx context.Context, value string) (*clouddrive.RemoteTask, error) {
	hash, err := normalizeHash(value)
	if err != nil {
		return nil, &clouddrive.Error{Message: "光鸭任务 InfoHash 格式不正确"}
	}
	if err := c.lock(ctx); err != nil {
		return nil, err
	}
	defer c.unlock()
	return c.findLocked(ctx, hash)
}

func (c *Client) findLocked(ctx context.Context, hash string) (*clouddrive.RemoteTask, error) {
	task, err := c.listTaskLocked(ctx, hash)
	if err != nil || task == nil {
		return nil, err
	}
	status, err := taskStatus(task.Status)
	if err != nil {
		return nil, err
	}
	progress := int64(0)
	if task.Progress != "" {
		progress, err = task.Progress.integer()
	}
	if err != nil || progress < 0 || progress > 100 {
		return nil, &clouddrive.Error{Message: "光鸭任务进度格式不正确", Retryable: true}
	}
	remote := &clouddrive.RemoteTask{Hash: hash, Status: status, Progress: int(progress), RemoteID: string(task.ID), ResultID: string(task.FileID)}
	if task.ParentID != nil {
		remote.FolderID = publicParent(string(*task.ParentID))
	}
	if prior, ok := c.submissions[hash]; ok && prior.taskID == string(task.ID) {
		if remote.FolderID != "" && remote.FolderID != prior.folder {
			return nil, &clouddrive.Error{Message: "光鸭任务返回错误下载目录"}
		}
		remote.FolderID = prior.folder
	}
	if status == 2 {
		if !validID(remote.ResultID) || remote.ResultID == "0" || remote.ResultID == remote.FolderID {
			return nil, &clouddrive.Error{Message: "光鸭完成任务缺少独立结果身份", Retryable: true}
		}
		file, err := c.infoLocked(ctx, remote.ResultID)
		if err != nil {
			var fileErr *clouddrive.Error
			if !errors.As(err, &fileErr) || !fileErr.Missing || fileErr.Auth {
				return nil, err
			}
			remote.ResultMissing = true
		} else {
			if remote.FolderID != "" && remote.FolderID != file.ParentID {
				return nil, &clouddrive.Error{Message: "光鸭任务结果不在声明的下载目录"}
			}
			remote.FolderID = file.ParentID
		}
	}
	if remote.FolderID != "" && !validID(remote.FolderID) {
		return nil, &clouddrive.Error{Message: "光鸭任务父目录身份无效"}
	}
	return remote, nil
}

func (c *Client) Add(ctx context.Context, raw, folder string) (returnErr error) {
	createSent := false
	defer func() {
		if returnErr != nil && !createSent {
			// 未发送创建请求的失败可以安全回退到队列并重新解析；已发送的请求必须先对账。
			annotated := clouddrive.SafeError(returnErr).(*clouddrive.Error)
			annotated.NotSubmitted = true
			returnErr = annotated
		}
	}()
	magnet, hash, err := clouddrive.NormalizeMagnet(raw)
	if err != nil {
		return &clouddrive.Error{Message: err.Error()}
	}
	if !validID(folder) || folder == "0" {
		return &clouddrive.Error{Message: "光鸭云下载必须使用独立任务目录"}
	}
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer c.unlock()
	var resolved struct {
		URL string `json:"url"`
		BT  *struct {
			Hash     string `json:"infoHash"`
			Name     string `json:"fileName"`
			Subfiles []struct {
				Index *int `json:"fileIndex"`
			} `json:"subfiles"`
			Excluded []int `json:"excludeIndices"`
		} `json:"btResInfo"`
	}
	if err := c.requestLocked(ctx, http.MethodPost, apiBase+taskAPI+"resolve_res", map[string]string{"url": magnet}, false, &resolved); err != nil {
		return err
	}
	if resolved.BT == nil {
		return &clouddrive.Error{Message: "光鸭尚未解析出可核验的 BT 资源，请选择其它磁力", Retryable: true}
	}
	resolvedHash, err := normalizeHash(resolved.BT.Hash)
	if err != nil || resolvedHash != hash {
		return &clouddrive.Error{Message: "光鸭解析返回的磁力 InfoHash 不匹配"}
	}
	if strings.HasPrefix(strings.ToLower(resolved.URL), "magnet:") {
		_, found, err := clouddrive.NormalizeMagnet(resolved.URL)
		if err != nil || found != hash {
			return &clouddrive.Error{Message: "光鸭解析返回冲突的下载资源"}
		}
	}
	body := map[string]any{"url": magnet, "parentId": folder}
	if resolved.BT.Name != "" {
		if strings.ContainsAny(resolved.BT.Name, "/\\\r\n\x00") || resolved.BT.Name == "." || resolved.BT.Name == ".." {
			return &clouddrive.Error{Message: "光鸭解析返回无效资源名称"}
		}
		body["newName"] = resolved.BT.Name
	}
	if len(resolved.BT.Subfiles) > 0 {
		excluded := map[int]bool{}
		for _, index := range resolved.BT.Excluded {
			excluded[index] = true
		}
		indexes := []int{}
		seen := map[int]bool{}
		for i, file := range resolved.BT.Subfiles {
			index := i
			if file.Index != nil {
				index = *file.Index
			}
			if index < 0 || seen[index] {
				return &clouddrive.Error{Message: "光鸭 BT 子文件索引格式不正确"}
			}
			seen[index] = true
			if !excluded[index] {
				indexes = append(indexes, index)
			}
		}
		if len(indexes) == 0 {
			return &clouddrive.Error{Message: "光鸭解析出的 BT 文件均不可下载"}
		}
		body["fileIndexes"] = indexes
		if len(resolved.BT.Excluded) > 0 {
			body["excludeIndices"] = resolved.BT.Excluded
		}
	}
	var result struct {
		TaskID scalar `json:"taskId"`
		URL    string `json:"url"`
	}
	if err := c.requestLocked(ctx, http.MethodPost, apiBase+taskAPI+"create_task", body, false, &result, func() { createSent = true }); err != nil {
		return err
	}
	if !validID(string(result.TaskID)) {
		return &clouddrive.Error{Message: "光鸭提交结果不明确，将先对账而非再次提交", Retryable: true}
	}
	if strings.HasPrefix(strings.ToLower(result.URL), "magnet:") {
		_, found, err := clouddrive.NormalizeMagnet(result.URL)
		if err != nil || found != hash {
			return &clouddrive.Error{Message: "光鸭创建任务返回冲突的资源身份"}
		}
	}
	c.submissions[hash] = submission{taskID: string(result.TaskID), folder: folder}
	return nil
}

func (c *Client) RemoveTask(ctx context.Context, value string) error {
	hash, err := normalizeHash(value)
	if err != nil {
		return &clouddrive.Error{Message: "光鸭任务 InfoHash 格式不正确"}
	}
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer c.unlock()
	task, err := c.listTaskLocked(ctx, hash)
	if err != nil || task == nil {
		return err
	}
	return c.removeTaskLocked(ctx, hash, string(task.ID))
}

func (c *Client) RemoveTaskIf(ctx context.Context, value string, expected clouddrive.RemoteTask) error {
	hash, err := normalizeHash(value)
	if err != nil || expected.Hash != hash || !validID(expected.RemoteID) || !validID(expected.FolderID) || expected.FolderID == "0" {
		return &clouddrive.Error{Message: "光鸭取消任务缺少已核验的账号缓存归属与远程身份"}
	}
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer c.unlock()
	current, err := c.findLocked(ctx, hash)
	if err != nil || current == nil {
		return err
	}
	identityMatches := current.RemoteID == expected.RemoteID && current.ResultID == expected.ResultID && current.Status == expected.Status
	folderMatches := current.FolderID == expected.FolderID
	// 缺失结果只允许借用上层曾持久化核验的归属；任务 ID 与结果 ID 必须仍完全相同。
	if current.FolderID == "" && current.ResultMissing && expected.ResultMissing {
		folderMatches = true
	}
	if !identityMatches || !folderMatches {
		return &clouddrive.Error{Message: "光鸭远程任务或结果归属已变化，未取消新任务", Retryable: true}
	}
	return c.removeTaskLocked(ctx, hash, current.RemoteID)
}

func (c *Client) removeTaskLocked(ctx context.Context, hash, taskID string) error {
	// 此端点仅删除云下载任务记录；结果文件由独立的缓存删除流程处理。
	if err := c.requestLocked(ctx, http.MethodPost, apiBase+"/cloudcollection/v2/delete_task", map[string]any{
		"taskIds": []string{taskID},
	}, false, nil); err != nil {
		return err
	}
	if prior, ok := c.submissions[hash]; ok && prior.taskID == taskID {
		delete(c.submissions, hash)
	}
	return nil
}
