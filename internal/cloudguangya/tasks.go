package cloudguangya

import (
	"context"
	"encoding/json"
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

var _ clouddrive.TaskIDProvider = (*Client)(nil)

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
	if hash == "" {
		return "", &clouddrive.Error{Message: "光鸭任务缺少可核验的磁力身份", Retryable: true}
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

func validTaskID(id string) bool {
	return validID(id) && id != "0" && strings.TrimSpace(id) == id
}

func validTaskStatusCounts(raw json.RawMessage) bool {
	var counts []map[string]json.RawMessage
	if json.Unmarshal(raw, &counts) != nil || counts == nil {
		return false
	}
	for _, fields := range counts {
		if fields == nil {
			return false
		}
		for key, value := range fields {
			if key != "status" && key != "count" || string(value) == "null" {
				return false
			}
			var number scalar
			if json.Unmarshal(value, &number) != nil {
				return false
			}
			n, err := number.integer()
			if err != nil || key == "count" && n < 0 {
				return false
			}
		}
	}
	return true
}

// taskByIDLocked 只核验本次提交的任务；历史数量和非空游标不能触发全库扫描。
func (c *Client) taskByIDLocked(ctx context.Context, hash, taskID string) (*offlineTask, error) {
	var result struct {
		Total   *scalar       `json:"total"`
		HasMore bool          `json:"hasMore"`
		Cursor  string        `json:"cursor"`
		List    []offlineTask `json:"list"`
	}
	var payload json.RawMessage
	if err := c.requestLocked(ctx, http.MethodPost, apiBase+taskAPI+"list_task", map[string]any{
		"taskIds": []string{taskID}, "pageSize": pageSize, "mode": "cursor",
	}, false, &payload); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil || fields == nil || json.Unmarshal(payload, &result) != nil {
		return nil, &clouddrive.Error{Message: "光鸭按任务 ID 查询返回无效结构", Retryable: true}
	}
	for _, key := range []string{"list", "total", "cursor", "hasMore"} {
		if raw, ok := fields[key]; ok && string(raw) == "null" {
			return nil, &clouddrive.Error{Message: "光鸭按任务 ID 查询返回空值字段", Retryable: true}
		}
	}
	if len(result.List) > 1 || result.HasMore {
		return nil, &clouddrive.Error{Message: "光鸭按任务 ID 查询返回不完整或未筛选的结果", Retryable: true}
	}
	if len(result.List) == 0 {
		// 已核验成功的空查询会省略零值，只接受已实测的分页字段和状态计数。
		for key := range fields {
			switch key {
			case "list", "total", "cursor", "hasMore", "statusCounts":
			default:
				return nil, &clouddrive.Error{Message: "光鸭任务空查询返回未知字段", Retryable: true}
			}
		}
		// 聚合计数可省略零值并包含重复状态，只核查结构，不推导目标任务是否存在。
		if raw, ok := fields["statusCounts"]; ok && !validTaskStatusCounts(raw) {
			return nil, &clouddrive.Error{Message: "光鸭任务状态计数格式不正确", Retryable: true}
		}
		if result.Cursor != "" {
			return nil, &clouddrive.Error{Message: "光鸭任务空查询仍返回后续游标", Retryable: true}
		}
		if result.Total != nil {
			total, err := result.Total.integer()
			if err != nil || total != 0 {
				return nil, &clouddrive.Error{Message: "光鸭任务空查询返回非零或无效数量", Retryable: true}
			}
		}
		return nil, nil
	}
	if result.Total == nil {
		return nil, &clouddrive.Error{Message: "光鸭按任务 ID 查询缺少任务数量", Retryable: true}
	}
	total, err := result.Total.integer()
	if err != nil || total != int64(len(result.List)) {
		return nil, &clouddrive.Error{Message: "光鸭按任务 ID 查询返回不一致的任务数量", Retryable: true}
	}
	task := result.List[0]
	if !validTaskID(string(task.ID)) || string(task.ID) != taskID {
		return nil, &clouddrive.Error{Message: "光鸭返回的任务 ID 与本次提交不一致", Retryable: true}
	}
	found, err := task.hash()
	if err != nil {
		return nil, err
	}
	if found != hash {
		return nil, &clouddrive.Error{Message: "光鸭返回的任务磁力身份与本次提交不一致", Retryable: true}
	}
	return &task, nil
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
	prior, ok := c.submissions[hash]
	if !ok || !validTaskID(prior.taskID) {
		return nil, &clouddrive.Error{Message: "光鸭任务缺少已保存的远程任务 ID，请核验本次提交结果后重试", Retryable: true}
	}
	return c.findTaskLocked(ctx, hash, prior.taskID)
}

// FindTask 可使用持久化的任务 ID 在重启后继续轮询，不搜索相同磁力的历史任务。
func (c *Client) FindTask(ctx context.Context, value, taskID string) (*clouddrive.RemoteTask, error) {
	hash, err := normalizeHash(value)
	if err != nil || !validTaskID(taskID) {
		return nil, &clouddrive.Error{Message: "光鸭任务缺少有效的磁力身份或远程任务 ID", Retryable: true}
	}
	if err := c.lock(ctx); err != nil {
		return nil, err
	}
	defer c.unlock()
	return c.findTaskLocked(ctx, hash, taskID)
}

func (c *Client) findTaskLocked(ctx context.Context, hash, taskID string) (*clouddrive.RemoteTask, error) {
	task, err := c.taskByIDLocked(ctx, hash, taskID)
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

func (c *Client) Add(ctx context.Context, raw, folder string) error {
	_, err := c.AddTask(ctx, raw, folder)
	return err
}

// AddTask 直接创建新任务并返回远程身份；是否已有资源由本地任务库判断。
func (c *Client) AddTask(ctx context.Context, raw, folder string) (taskID string, returnErr error) {
	createSent := false
	defer func() {
		if returnErr != nil && !createSent {
			// 未发送创建请求的失败可以安全重试；已发送但身份不明时不能自动再次创建。
			annotated := clouddrive.SafeError(returnErr).(*clouddrive.Error)
			annotated.NotSubmitted = true
			returnErr = annotated
		}
	}()
	magnet, hash, err := clouddrive.NormalizeMagnet(raw)
	if err != nil {
		return "", &clouddrive.Error{Message: err.Error()}
	}
	if !validID(folder) || folder == "0" {
		return "", &clouddrive.Error{Message: "光鸭云下载必须使用独立任务目录"}
	}
	if err := c.lock(ctx); err != nil {
		return "", err
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
		return "", err
	}
	if resolved.BT == nil {
		return "", &clouddrive.Error{Message: "光鸭尚未解析出可核验的 BT 资源，请选择其它磁力", Retryable: true}
	}
	resolvedHash, err := normalizeHash(resolved.BT.Hash)
	if err != nil || resolvedHash != hash {
		return "", &clouddrive.Error{Message: "光鸭解析返回的磁力 InfoHash 不匹配"}
	}
	if strings.HasPrefix(strings.ToLower(resolved.URL), "magnet:") {
		_, found, err := clouddrive.NormalizeMagnet(resolved.URL)
		if err != nil || found != hash {
			return "", &clouddrive.Error{Message: "光鸭解析返回冲突的下载资源"}
		}
	}
	body := map[string]any{"url": magnet, "parentId": folder}
	if resolved.BT.Name != "" {
		if strings.ContainsAny(resolved.BT.Name, "/\\\r\n\x00") || resolved.BT.Name == "." || resolved.BT.Name == ".." {
			return "", &clouddrive.Error{Message: "光鸭解析返回无效资源名称"}
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
				return "", &clouddrive.Error{Message: "光鸭 BT 子文件索引格式不正确"}
			}
			seen[index] = true
			if !excluded[index] {
				indexes = append(indexes, index)
			}
		}
		if len(indexes) == 0 {
			return "", &clouddrive.Error{Message: "光鸭解析出的 BT 文件均不可下载"}
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
		return "", err
	}
	if !validTaskID(string(result.TaskID)) {
		return "", &clouddrive.Error{Message: "光鸭提交结果缺少有效任务 ID，未自动再次创建", Retryable: true}
	}
	if strings.HasPrefix(strings.ToLower(result.URL), "magnet:") {
		_, found, err := clouddrive.NormalizeMagnet(result.URL)
		if err != nil || found != hash {
			return "", &clouddrive.Error{Message: "光鸭创建任务返回冲突的资源身份"}
		}
	}
	c.submissions[hash] = submission{taskID: string(result.TaskID), folder: folder}
	return string(result.TaskID), nil
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
	prior, ok := c.submissions[hash]
	if !ok || !validTaskID(prior.taskID) {
		return &clouddrive.Error{Message: "光鸭任务缺少已保存的远程任务 ID，未搜索或删除历史任务", Retryable: true}
	}
	task, err := c.taskByIDLocked(ctx, hash, prior.taskID)
	if err != nil || task == nil {
		return err
	}
	return c.removeTaskLocked(ctx, hash, string(task.ID))
}

func (c *Client) RemoveTaskIf(ctx context.Context, value string, expected clouddrive.RemoteTask) error {
	hash, err := normalizeHash(value)
	if err != nil || expected.Hash != hash || !validTaskID(expected.RemoteID) || !validID(expected.FolderID) || expected.FolderID == "0" {
		return &clouddrive.Error{Message: "光鸭取消任务缺少已核验的账号缓存归属与远程身份"}
	}
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer c.unlock()
	current, err := c.findTaskLocked(ctx, hash, expected.RemoteID)
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
