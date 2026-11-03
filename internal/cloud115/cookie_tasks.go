package cloud115

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"emby-go/internal/clouddrive"
)

var _ clouddrive.StableTaskIDProvider = (*CookieClient)(nil)
var _ clouddrive.ConditionalTaskRemover = (*CookieClient)(nil)

type cookieSubmissionContextKey struct{}

// 在限速等待结束后记录真实传输开始，等待取消不能误判为已提交。
type cookieSubmissionTransport struct{ base http.RoundTripper }

func (t cookieSubmissionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if sent, ok := r.Context().Value(cookieSubmissionContextKey{}).(*bool); ok {
		*sent = true
	}
	return t.base.RoundTrip(r)
}

// TaskIDForHash 使用 115 的确定性任务身份，不访问网盘。
func (*CookieClient) TaskIDForHash(hash string) (string, error) {
	if len(hash) != 40 {
		return "", &Error{Message: "115 任务 InfoHash 格式不正确"}
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", &Error{Message: "115 任务 InfoHash 格式不正确"}
	}
	return strings.ToLower(hash), nil
}

func validCookieResourceID(id string) bool {
	if id == "" || strings.Trim(id, "0123456789") != "" {
		return false
	}
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0
}

func cookieSubmissionCode(codes ...scalar) (int64, error) {
	var code int64
	for _, raw := range codes {
		if raw == "" {
			continue
		}
		n, err := strconv.ParseInt(string(raw), 10, 64)
		if err != nil {
			return 0, &Error{Message: "115 创建回执状态码格式不正确", Retryable: true}
		}
		if n != 0 {
			code = n
		}
	}
	return code, nil
}

func cookieTaskRejection(code int64) error {
	auth := code == 99 || code == 911 || code == 990001 || code == 40101032 || code == 40101035 || code == 40101037
	return &Error{Message: fmt.Sprintf("115 创建离线任务被拒绝（%d）", code), Auth: auth, Retryable: auth || code == 429 || code >= 500 && code < 600}
}

// 外层成功和解密已由生产请求路径核验；纯解析不把重复任务拒绝当作新建成功。
func parseCookieTaskSubmission(payload []byte, hash string) error {
	var reply struct {
		State   *bool  `json:"state"`
		Code    scalar `json:"code"`
		Errno   scalar `json:"errno"`
		Errcode scalar `json:"errcode"`
		Result  []struct {
			State   *bool  `json:"state"`
			Hash    string `json:"info_hash"`
			URL     string `json:"url"`
			Code    scalar `json:"code"`
			Errno   scalar `json:"errno"`
			Errcode scalar `json:"errcode"`
		} `json:"result"`
	}
	if json.Unmarshal(payload, &reply) != nil {
		return &Error{Message: "115 创建回执格式不正确", Retryable: true}
	}
	code, err := cookieSubmissionCode(reply.Code, reply.Errno, reply.Errcode)
	if err != nil {
		return err
	}
	if code != 0 || reply.State != nil && !*reply.State {
		return cookieTaskRejection(code)
	}
	if len(reply.Result) != 1 {
		return &Error{Message: "115 创建回执缺少唯一任务身份", Retryable: true}
	}
	task := reply.Result[0]
	code, err = cookieSubmissionCode(task.Code, task.Errno, task.Errcode)
	if err != nil {
		return err
	}
	if code != 0 || task.State != nil && !*task.State {
		return cookieTaskRejection(code)
	}
	if !strings.EqualFold(task.Hash, hash) {
		return &Error{Message: "115 创建回执的 InfoHash 与本次提交不一致", Retryable: true}
	}
	if task.URL != "" {
		_, found, err := NormalizeMagnet(task.URL)
		if err != nil || found != hash {
			return &Error{Message: "115 创建回执的磁力与本次提交不一致", Retryable: true}
		}
	}
	return nil
}

// AddTask 直接添加磁力；发送后始终保留确定性身份，模糊回执只能先定位，不能自动重写。
func (c *CookieClient) AddTask(ctx context.Context, raw, folder string) (taskID string, returnErr error) {
	sent := false
	defer func() {
		if returnErr != nil && !sent {
			e := SafeError(returnErr, Tokens{Cookie: c.cookie}).(*Error)
			e.NotSubmitted = true
			returnErr = e
		}
	}()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	magnet, hash, err := NormalizeMagnet(raw)
	if err != nil {
		return "", &Error{Message: err.Error()}
	}
	if !validCookieResourceID(folder) {
		return "", &Error{Message: "115 新下载必须使用有效的独立任务缓存目录"}
	}
	cookie, err := NormalizeCookie(c.cookie)
	if err != nil {
		return "", &Error{Message: "115 Cookie 格式不正确，请重新验证连接", Auth: true}
	}
	uid := ""
	req := &http.Request{Header: http.Header{"Cookie": {cookie}}}
	for _, ck := range req.Cookies() {
		if ck.Name == "UID" {
			uid = strings.Split(ck.Value, "_")[0]
		}
	}
	if !validCookieResourceID(uid) {
		return "", &Error{Message: "115 Cookie 缺少有效账号身份，请重新验证连接", Auth: true}
	}
	ctx = context.WithValue(ctx, cookieSubmissionContextKey{}, &sent)
	var payload json.RawMessage
	// 此接口依赖应用请求头选择 RSA 协议，不能换成通用浏览器请求头。
	err = c.encrypted(ctx, "https://lixian.115.com/lixianssp/?ac=add_task_urls", map[string]string{
		"ac": "add_task_urls", "wp_path_id": folder, "app_ver": cookieOfflineAppVersion, "uid": uid, "url[0]": magnet,
	}, cookieOfflineUserAgent, &payload)
	if err != nil {
		return hash, err
	}
	return hash, parseCookieTaskSubmission(payload, hash)
}

// FindTask 只查询同一规范 InfoHash，不扫描账号历史任务。
func (c *CookieClient) FindTask(ctx context.Context, value, taskID string) (*RemoteTask, error) {
	hash, err := c.TaskIDForHash(value)
	if err != nil {
		return nil, err
	}
	id, err := c.TaskIDForHash(taskID)
	if err != nil || id != hash {
		return nil, &Error{Message: "115 远程任务身份与本次磁力不一致", Retryable: true}
	}
	var reply struct {
		Page  scalar `json:"page"`
		Count scalar `json:"count"`
		Pages scalar `json:"page_count"`
		Tasks []struct {
			Hash     string  `json:"info_hash"`
			Status   int     `json:"status"`
			Progress float64 `json:"percentDone"`
			Result   scalar  `json:"file_id"`
			Folder   scalar  `json:"wp_path_id"`
		} `json:"tasks"`
	}
	q := url.Values{"ct": {"lixian"}, "ac": {"task_lists"}, "info_hash": {hash}, "page": {"1"}, "page_size": {"1"}}
	if err := c.request(ctx, "POST", "https://lixian.115.com/lixian/?"+q.Encode(), nil, "Mozilla/5.0", &reply); err != nil {
		return nil, err
	}
	page, pageErr := strconv.ParseInt(string(reply.Page), 10, 64)
	count, countErr := strconv.ParseInt(string(reply.Count), 10, 64)
	pages, pagesErr := strconv.ParseInt(string(reply.Pages), 10, 64)
	if pageErr != nil || countErr != nil || pagesErr != nil || page != 1 || count < 0 || count > 1 || pages < 0 || pages > 1 || reply.Tasks == nil || int64(len(reply.Tasks)) != count || count == 1 && pages != 1 {
		return nil, &Error{Message: "115 按 InfoHash 查询返回不完整或未过滤的任务列表", Retryable: true}
	}
	if count == 0 {
		return nil, nil
	}
	task := reply.Tasks[0]
	if !strings.EqualFold(task.Hash, hash) {
		return nil, &Error{Message: "115 未按 InfoHash 返回本次任务", Retryable: true}
	}
	if task.Status < -1 || task.Status > 2 || task.Progress < 0 || task.Progress > 100 {
		return nil, &Error{Message: "115 返回无效的任务状态或进度", Retryable: true}
	}
	result, folder := string(task.Result), string(task.Folder)
	if result != "" && result != "0" && !validCookieResourceID(result) || folder != "" && folder != "0" && !validCookieResourceID(folder) {
		return nil, &Error{Message: "115 返回无效的结果或保存目录身份", Retryable: true}
	}
	if task.Status == 2 && (!validCookieResourceID(result) || result == folder) {
		return nil, &Error{Message: "115 完成任务缺少独立结果身份", Retryable: true}
	}
	return &RemoteTask{Hash: hash, RemoteID: hash, Status: task.Status, Progress: int(task.Progress), ResultID: result, FolderID: folder}, nil
}

// RemoveTaskIf 使用新鲜身份核验缩小按 hash 删除的边界；上游没有原子的版本条件删除。
func (c *CookieClient) RemoveTaskIf(ctx context.Context, value string, expected RemoteTask) error {
	hash, err := c.TaskIDForHash(value)
	if err != nil || expected.Hash != hash || expected.RemoteID != hash || !validCookieResourceID(expected.FolderID) {
		return &Error{Message: "115 移除任务缺少已核验的磁力、远程身份或缓存归属", Retryable: true}
	}
	current, err := c.FindTask(ctx, hash, hash)
	if err != nil || current == nil {
		return err
	}
	if current.RemoteID != expected.RemoteID || current.Hash != expected.Hash || current.Status != expected.Status || current.ResultID != expected.ResultID || current.FolderID != expected.FolderID {
		return &Error{Message: "115 远程任务或结果归属已变化，未移除离线记录", Retryable: true}
	}
	if current.ResultID != "" && current.ResultID != "0" {
		file, err := c.Info(ctx, current.ResultID)
		if err != nil {
			var remote *Error
			if !errors.As(err, &remote) || !remote.Missing || remote.Auth || remote.Retryable {
				return err
			}
		} else if file.ID != current.ResultID || file.ParentID != current.FolderID {
			return &Error{Message: "115 任务结果不在已核验的缓存目录，未移除离线记录", Retryable: true}
		}
	}
	return c.request(ctx, "POST", "https://lixian.115.com/lixian/?ct=lixian&ac=task_del", url.Values{
		"hash[0]": {hash}, "flag": {"0"},
	}, "Mozilla/5.0", nil)
}
