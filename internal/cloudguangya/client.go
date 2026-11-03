package cloudguangya

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"emby-go/internal/clouddrive"
)

const (
	apiBase     = "https://api.guangyapan.com"
	accountBase = "https://account.guangyapan.com"
	clientID    = "aMe-8VSlkrbQXpUR"
	fileAPI     = "/nd.bizuserres.s/v1/"
	taskAPI     = "/nd.bizcloudcollection.s/v1/"
	pageSize    = 100
	maxPages    = 100
	userAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
)

type Tokens struct {
	Access   string `json:"access_token"`
	Refresh  string `json:"refresh_token"`
	DeviceID string `json:"device_id"`
}

// DefaultTokens 为新账号生成设备身份；保存规范化后的令牌可跨重启复用该身份。
func DefaultTokens() Tokens {
	id, _ := randomHex(16)
	return Tokens{DeviceID: id}
}

func NormalizeTokens(tokens Tokens) (Tokens, error) {
	tokens.Access = strings.TrimSpace(tokens.Access)
	tokens.Refresh = strings.TrimSpace(tokens.Refresh)
	if len(tokens.Access) >= 7 && strings.EqualFold(tokens.Access[:7], "Bearer ") {
		tokens.Access = strings.TrimSpace(tokens.Access[7:])
	}
	for _, value := range []string{tokens.Access, tokens.Refresh} {
		if len(value) > 32768 || strings.ContainsAny(value, "\r\n\t\x00") {
			return Tokens{}, &clouddrive.Error{Message: "光鸭访问令牌格式不正确", Auth: true}
		}
	}
	if tokens.Access == "" && tokens.Refresh == "" {
		return Tokens{}, &clouddrive.Error{Message: "请提供光鸭访问令牌或刷新令牌", Auth: true}
	}
	tokens.DeviceID = strings.ToLower(strings.TrimSpace(tokens.DeviceID))
	if tokens.DeviceID == "" {
		var err error
		tokens.DeviceID, err = randomHex(16)
		if err != nil {
			return Tokens{}, &clouddrive.Error{Message: "无法生成光鸭设备身份"}
		}
	}
	device, err := hex.DecodeString(tokens.DeviceID)
	if err != nil || len(device) != 16 {
		return Tokens{}, &clouddrive.Error{Message: "光鸭设备 ID 必须为 32 位十六进制字符"}
	}
	return tokens, nil
}

func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func SafeError(err error, tokens Tokens) error {
	return clouddrive.SafeError(err, tokens.Access, tokens.Refresh)
}

type submission struct {
	taskID, folder string
}

type Client struct {
	http            *http.Client
	tokens          Tokens
	persist         func(Tokens) error
	gate            chan struct{}
	interval        time.Duration
	nextRequest     time.Time
	expiresAt       time.Time
	initErr         error
	persistErr      error
	submissions     map[string]submission
	expectedAccount string
	checkedAccess   string
	pendingRefresh  bool
	pendingExpiry   time.Time
}

var _ clouddrive.CacheProvider = (*Client)(nil)
var _ clouddrive.PendingTaskFolderUnknown = (*Client)(nil)
var _ clouddrive.ConditionalTaskRemover = (*Client)(nil)

func New(tokens Tokens, persist func(Tokens) error) *Client {
	return newClient(tokens, persist, http.DefaultTransport, 500*time.Millisecond)
}

func newClient(tokens Tokens, persist func(Tokens) error, transport http.RoundTripper, interval time.Duration) *Client {
	normalized, err := NormalizeTokens(tokens)
	return &Client{
		tokens: normalized, persist: persist, initErr: err, interval: interval,
		gate: make(chan struct{}, 1), submissions: map[string]submission{},
		http: &http.Client{
			Transport: transport, Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// 光鸭未完成的任务可能暂时没有 parentId；完成后必须从结果文件核验实际父目录。
func (*Client) PendingTaskFolderUnknown() bool { return true }

// SetExpectedAccount 绑定已保存连接的账号，防止令牌刷新后跨账号操作缓存。
func (c *Client) SetExpectedAccount(account string) {
	c.gate <- struct{}{}
	defer c.unlock()
	account = strings.TrimSpace(account)
	if account == "" {
		return
	}
	if c.expectedAccount != "" && c.expectedAccount != account {
		c.initErr = &clouddrive.Error{Message: "光鸭连接绑定的账号身份发生冲突，请重新授权", Auth: true}
		return
	}
	c.expectedAccount = account
}

func (c *Client) lock(ctx context.Context) error {
	select {
	case c.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			c.unlock()
			return err
		}
		if c.initErr != nil {
			c.unlock()
			return c.initErr
		}
		if c.persistErr != nil {
			c.unlock()
			return &clouddrive.Error{Message: "保存刷新后的光鸭凭证失败，请重新授权", Auth: true}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) unlock() { <-c.gate }

type scalar string

func (s *scalar) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*s = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, (*string)(s))
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*s = scalar(n.String())
	return nil
}

func (s scalar) integer() (int64, error) { return strconv.ParseInt(string(s), 10, 64) }

type response struct {
	Code      scalar          `json:"code"`
	Message   string          `json:"msg"`
	Error     string          `json:"error"`
	ErrorCode scalar          `json:"error_code"`
	Data      json.RawMessage `json:"data"`
}

func (c *Client) rawRequestLocked(ctx context.Context, method, endpoint string, body any, account bool, onSend ...func()) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, &clouddrive.Error{Message: "光鸭请求参数格式不正确"}
		}
		reader = strings.NewReader(string(b))
	}
	if delay := time.Until(c.nextRequest); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-timer.C:
		}
	}
	c.nextRequest = time.Now().Add(c.interval)
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, 0, &clouddrive.Error{Message: "光鸭请求地址无效"}
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://www.guangyapan.com")
	req.Header.Set("Referer", "https://www.guangyapan.com/")
	req.Header.Set("User-Agent", userAgent)
	if c.tokens.Access != "" {
		req.Header.Set("Authorization", "Bearer "+c.tokens.Access)
	}
	traceID, traceErr := randomHex(16)
	spanID, spanErr := randomHex(8)
	if traceErr != nil || spanErr != nil {
		return nil, 0, &clouddrive.Error{Message: "无法生成光鸭请求身份"}
	}
	req.Header.Set("traceparent", "00-"+traceID+"-"+spanID+"-01")
	if account {
		sign, err := randomHex(16)
		if err != nil {
			return nil, 0, &clouddrive.Error{Message: "无法生成光鸭账号请求身份"}
		}
		for key, value := range map[string]string{
			"X-Client-Id": clientID, "X-Client-Version": "0.0.1", "X-Device-Id": c.tokens.DeviceID,
			"X-Device-Model": "chrome%2F147.0.0.0", "X-Device-Name": "PC-Chrome",
			"X-Device-Sign":   "wdi10." + c.tokens.DeviceID + sign,
			"X-Net-Work-Type": "NONE", "X-OS-Version": "MacIntel", "X-Platform-Version": "1",
			"X-Protocol-Version": "301", "X-Provider-Name": "NONE", "X-SDK-Version": "9.0.2",
		} {
			req.Header.Set(key, value)
		}
	} else {
		req.Header.Set("Did", c.tokens.DeviceID)
		req.Header.Set("Dt", "4")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	for _, notify := range onSend {
		notify()
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, &clouddrive.Error{Message: "光鸭网络请求失败，请先核验远端任务后重试", Retryable: true}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil || len(b) > 4*1024*1024 {
		return nil, resp.StatusCode, &clouddrive.Error{Message: "光鸭响应读取失败或超过限制", Retryable: true}
	}
	return b, resp.StatusCode, nil
}

func statusError(status int) error {
	return &clouddrive.Error{
		Message:   fmt.Sprintf("光鸭 HTTP 请求失败（%d）", status),
		Auth:      status == http.StatusUnauthorized || status == http.StatusForbidden,
		Retryable: status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests || status >= 500,
	}
}

func (c *Client) refreshLocked(ctx context.Context) error {
	if c.pendingRefresh {
		return c.finishRefreshLocked(ctx)
	}
	if c.tokens.Refresh == "" {
		return &clouddrive.Error{Message: "光鸭凭证已失效，请更新访问令牌", Auth: true}
	}
	b, status, err := c.rawRequestLocked(ctx, http.MethodPost, accountBase+"/v1/auth/token", map[string]string{
		"client_id": clientID, "grant_type": "refresh_token", "refresh_token": c.tokens.Refresh,
	}, true)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return statusError(status)
	}
	var result struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Expires scalar `json:"expires_in"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(b, &result) != nil || result.Error != "" || strings.TrimSpace(result.Access) == "" {
		return &clouddrive.Error{Message: "光鸭刷新凭证失败，请重新授权", Auth: true}
	}
	next := c.tokens
	next.Access = result.Access
	if result.Refresh != "" {
		next.Refresh = result.Refresh
	}
	next, err = NormalizeTokens(next)
	if err != nil {
		return &clouddrive.Error{Message: "光鸭刷新返回无效凭证", Auth: true}
	}
	c.tokens = next
	c.pendingRefresh = true
	c.pendingExpiry = time.Time{}
	if seconds, err := result.Expires.integer(); err == nil && seconds > 0 && seconds <= 365*24*3600 {
		c.pendingExpiry = time.Now().Add(time.Duration(seconds) * time.Second)
	}
	return c.finishRefreshLocked(ctx)
}

func (c *Client) finishRefreshLocked(ctx context.Context) error {
	// 刷新令牌可能已经旋转；候选凭证只留在内存，账号验证通过前不保存也不访问资源。
	if _, err := c.verifyAccountLocked(ctx); err != nil {
		return err
	}
	if c.persist != nil {
		c.persistErr = c.persist(c.tokens)
		if c.persistErr != nil {
			return &clouddrive.Error{Message: "保存刷新后的光鸭凭证失败，请重新授权", Auth: true}
		}
	}
	c.pendingRefresh = false
	c.expiresAt = c.pendingExpiry
	return nil
}

func (c *Client) verifyAccountLocked(ctx context.Context) (string, error) {
	b, status, err := c.rawRequestLocked(ctx, http.MethodGet, accountBase+"/v1/user/me", nil, true)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", statusError(status)
	}
	var account struct {
		Sub   string `json:"sub"`
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &account) != nil || account.Error != "" || strings.TrimSpace(account.Sub) == "" {
		return "", &clouddrive.Error{Message: "光鸭账号验证失败，请重新授权", Auth: true}
	}
	if c.expectedAccount != "" && c.expectedAccount != account.Sub {
		return "", &clouddrive.Error{Message: "光鸭访问令牌与连接账号不匹配，请重新授权", Auth: true}
	}
	c.expectedAccount = account.Sub
	c.checkedAccess = c.tokens.Access
	return account.Sub, nil
}

func responseError(r response) error {
	message := strings.ToLower(strings.TrimSpace(r.Message))
	successCode := r.Code == "" || r.Code == "0" || r.Code == "200"
	successErrorCode := r.ErrorCode == "" || r.ErrorCode == "0"
	if r.Error == "" && successCode && successErrorCode && (message == "success" || message == "") {
		return nil
	}
	code, _ := r.Code.integer()
	errorCode, _ := r.ErrorCode.integer()
	auth := code == 401 || code == 403 || errorCode == 401 || errorCode == 403 || r.Error == "invalid_token" || r.Error == "unauthorized"
	retry := auth || code == 429 || errorCode == 429 || code >= 500 && code < 600 || errorCode >= 500 && errorCode < 600 || strings.Contains(message, "频繁") || strings.Contains(message, "rate limit")
	// 失败代码优先于 success 文本；成功主代码不能掩盖实际认证或服务错误。
	if (code == 0 || code == 200) && errorCode != 0 {
		code = errorCode
	}
	missing := !retry && (message == "file not found" || message == "file_not_found" || message == "文件不存在" || message == "文件已删除")
	return &clouddrive.Error{Message: fmt.Sprintf("光鸭接口返回失败（%d）", code), Auth: auth, Retryable: retry, Missing: missing}
}

// 只在明确的认证拒绝后刷新重试；超时、断连和不完整创建响应交给上层先对账。
func (c *Client) requestLocked(ctx context.Context, method, endpoint string, body any, account bool, out any, onSend ...func()) error {
	refreshed := false
	if c.pendingRefresh {
		if err := c.finishRefreshLocked(ctx); err != nil {
			return err
		}
	}
	if c.tokens.Access == "" || !c.expiresAt.IsZero() && !time.Now().Before(c.expiresAt) {
		if err := c.refreshLocked(ctx); err != nil {
			return err
		}
		refreshed = true
	}
	if !account && c.expectedAccount != "" && c.checkedAccess != c.tokens.Access {
		if _, err := c.verifyAccountLocked(ctx); err != nil {
			return err
		}
	}
	for {
		b, status, err := c.rawRequestLocked(ctx, method, endpoint, body, account, onSend...)
		if err != nil {
			return err
		}
		var envelope response
		if status >= 200 && status < 300 {
			if json.Unmarshal(b, &envelope) != nil {
				return &clouddrive.Error{Message: "光鸭响应格式不正确，需先核验远端状态", Retryable: true}
			}
			err = responseError(envelope)
		} else {
			err = statusError(status)
		}
		var remote *clouddrive.Error
		if errors.As(err, &remote) && remote.Auth && !refreshed && c.tokens.Refresh != "" {
			if err := c.refreshLocked(ctx); err != nil {
				return err
			}
			refreshed = true
			continue
		}
		if err != nil {
			return err
		}
		if !account && strings.TrimSpace(envelope.Message) == "" && strings.TrimSpace(string(envelope.Code)) == "" {
			return &clouddrive.Error{Message: "光鸭响应未确认操作结果，需先核验远端状态", Retryable: true}
		}
		if out != nil {
			payload := envelope.Data
			if account {
				payload = b
			}
			if len(payload) == 0 || string(payload) == "null" || json.Unmarshal(payload, out) != nil {
				return &clouddrive.Error{Message: "光鸭响应字段不完整，需先核验远端状态", Retryable: true}
			}
		}
		return nil
	}
}

func validID(id string) bool {
	return id != "" && len(id) <= 512 && !strings.ContainsAny(id, "\r\n\t\x00/\\")
}

func apiParent(id string) string {
	if id == "0" {
		return ""
	}
	return id
}

func publicParent(id string) string {
	if id == "" {
		return "0"
	}
	return id
}

func (c *Client) Check(ctx context.Context, folder string) (string, error) {
	if err := c.lock(ctx); err != nil {
		return "", err
	}
	defer c.unlock()
	var account struct {
		Sub string `json:"sub"`
	}
	if err := c.requestLocked(ctx, http.MethodGet, accountBase+"/v1/user/me", nil, true, &account); err != nil {
		return "", err
	}
	if strings.TrimSpace(account.Sub) == "" {
		return "", &clouddrive.Error{Message: "光鸭未返回有效账号身份", Auth: true}
	}
	if c.expectedAccount != "" && c.expectedAccount != account.Sub {
		return "", &clouddrive.Error{Message: "光鸭访问令牌与连接账号不匹配，请重新授权", Auth: true}
	}
	c.expectedAccount = account.Sub
	c.checkedAccess = c.tokens.Access
	if folder != "" {
		file, err := c.infoLocked(ctx, folder)
		if err != nil {
			return "", err
		}
		if !file.Directory || file.ID != folder {
			return "", &clouddrive.Error{Message: "光鸭工作目录必须为有效文件夹"}
		}
	}
	return account.Sub, nil
}

type fileItem struct {
	ID       scalar  `json:"fileId"`
	ParentID *scalar `json:"parentId"`
	Name     string  `json:"fileName"`
	Size     scalar  `json:"fileSize"`
	ResType  scalar  `json:"resType"`
	SHA1     string  `json:"sha1"`
	Duration scalar  `json:"duration"`
}

func (item fileItem) file(fallbackParent string) (clouddrive.File, error) {
	id := string(item.ID)
	if !validID(id) || id == "0" || item.Name == "" || path.Base(item.Name) != item.Name || item.Name == "." || item.Name == ".." || strings.ContainsAny(item.Name, "\\\r\n\x00") {
		return clouddrive.File{}, &clouddrive.Error{Message: "光鸭返回无效文件身份或名称"}
	}
	typeID, err := item.ResType.integer()
	if err != nil || typeID != 1 && typeID != 2 {
		return clouddrive.File{}, &clouddrive.Error{Message: "光鸭返回未知文件资源类型"}
	}
	parent := fallbackParent
	if item.ParentID != nil {
		parent = publicParent(string(*item.ParentID))
	}
	if !validID(parent) {
		return clouddrive.File{}, &clouddrive.Error{Message: "光鸭文件缺少可核验的父目录", Retryable: true}
	}
	size := int64(0)
	if item.Size != "" {
		size, err = item.Size.integer()
		if err != nil || size < 0 {
			return clouddrive.File{}, &clouddrive.Error{Message: "光鸭文件大小格式不正确"}
		}
	}
	duration, _ := item.Duration.integer()
	return clouddrive.File{ID: id, ParentID: parent, Name: item.Name, Size: size, SHA1: item.SHA1, PickCode: id, Directory: typeID == 2, Duration: duration}, nil
}

func (c *Client) infoLocked(ctx context.Context, id string) (clouddrive.File, error) {
	if !validID(id) {
		return clouddrive.File{}, &clouddrive.Error{Message: "光鸭文件 ID 无效"}
	}
	if id == "0" {
		return clouddrive.File{ID: "0", Name: "/", Path: "/", Directory: true}, nil
	}
	var item fileItem
	if err := c.requestLocked(ctx, http.MethodPost, apiBase+"/userres/v1/file/get_info_by_file_id", map[string]string{"fileId": id}, false, &item); err != nil {
		return clouddrive.File{}, err
	}
	if item.ID == "" {
		return clouddrive.File{}, &clouddrive.Error{Message: "光鸭文件详情缺少文件身份", Retryable: true}
	}
	if string(item.ID) != id {
		return clouddrive.File{}, &clouddrive.Error{Message: "光鸭返回的文件身份不匹配"}
	}
	if item.ParentID != nil {
		return item.file("")
	}
	file, err := item.file("0")
	if err != nil {
		return clouddrive.File{}, err
	}
	// 服务端会省略根资源的空父字段；必须用当前根目录成员身份确认归属。
	if err := c.verifyRootFileLocked(ctx, file); err != nil {
		return clouddrive.File{}, err
	}
	return file, nil
}

func (c *Client) verifyRootFileLocked(ctx context.Context, expected clouddrive.File) error {
	seen := map[string]bool{}
	matched := false
	offset := 0
	var knownTotal *int64
	for page := 0; page < maxPages; page++ {
		result, err := c.listLocked(ctx, "0", offset)
		if err != nil {
			var remote *clouddrive.Error
			if errors.As(err, &remote) && remote.Missing {
				// 根列表失败只说明归属未证实，不能推翻已取得的目标文件身份。
				next := *remote
				next.Missing, next.Retryable = false, true
				return &next
			}
			return err
		}
		if result.total != nil {
			if knownTotal != nil && *knownTotal != *result.total {
				return &clouddrive.Error{Message: "光鸭根目录分页总数发生变化，需重新核验归属", Retryable: true}
			}
			knownTotal = result.total
		}
		if result.more && len(result.files) != pageSize {
			return &clouddrive.Error{Message: "光鸭根目录分页不完整，无法核验归属", Retryable: true}
		}
		for _, file := range result.files {
			if seen[file.ID] {
				return &clouddrive.Error{Message: "光鸭根目录分页返回重复身份，无法核验归属", Retryable: true}
			}
			seen[file.ID] = true
			if file.ID == expected.ID {
				if file.Name != expected.Name || file.Directory != expected.Directory {
					return &clouddrive.Error{Message: "光鸭根目录返回不一致的文件身份，无法核验归属", Retryable: true}
				}
				matched = true
			}
		}
		offset += len(result.files)
		if !result.more {
			if knownTotal != nil && int64(offset) != *knownTotal {
				return &clouddrive.Error{Message: "光鸭根目录分页提前结束，无法核验归属", Retryable: true}
			}
			if matched {
				return nil
			}
			return &clouddrive.Error{Message: "光鸭文件缺少可核验的父目录", Retryable: true}
		}
	}
	return &clouddrive.Error{Message: "光鸭根目录超过分页限制，无法完整核验归属", Retryable: true}
}

func (c *Client) Info(ctx context.Context, id string) (clouddrive.File, error) {
	if err := c.lock(ctx); err != nil {
		return clouddrive.File{}, err
	}
	defer c.unlock()
	return c.infoLocked(ctx, id)
}

func (c *Client) List(ctx context.Context, id string, offset int) ([]clouddrive.File, bool, error) {
	if !validID(id) || offset < 0 {
		return nil, false, &clouddrive.Error{Message: "光鸭目录或分页位置无效"}
	}
	if err := c.lock(ctx); err != nil {
		return nil, false, err
	}
	defer c.unlock()
	result, err := c.listLocked(ctx, id, offset)
	return result.files, result.more, err
}

type filePage struct {
	files []clouddrive.File
	more  bool
	total *int64
}

func (c *Client) listLocked(ctx context.Context, id string, offset int) (filePage, error) {
	var result struct {
		Total *scalar    `json:"total"`
		List  []fileItem `json:"list"`
	}
	var payload json.RawMessage
	// 公共层按偏移量续页，光鸭按固定页大小翻页，末页可能不足一整页。
	if err := c.requestLocked(ctx, http.MethodPost, apiBase+fileAPI+"file/get_file_list", map[string]any{
		"parentId": apiParent(id), "page": offset / pageSize, "pageSize": pageSize, "orderBy": 0, "sortType": 0,
	}, false, &payload); err != nil {
		return filePage{}, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil || fields == nil || json.Unmarshal(payload, &result) != nil {
		return filePage{}, &clouddrive.Error{Message: "光鸭目录响应字段不完整", Retryable: true}
	}
	if result.List == nil {
		// 明确成功的空对象和 total=0 可以省略零值字段，未知结构不能当作空目录。
		_, listPresent := fields["list"]
		empty := !listPresent && len(fields) == 0
		if !listPresent && len(fields) == 1 && result.Total != nil {
			total, err := result.Total.integer()
			empty = err == nil && total == 0
		}
		if !empty {
			return filePage{}, &clouddrive.Error{Message: "光鸭目录分页不完整", Retryable: true}
		}
		result.List = []fileItem{}
	}
	if len(result.List) > pageSize {
		return filePage{}, &clouddrive.Error{Message: "光鸭目录分页不完整", Retryable: true}
	}
	skip := offset % pageSize
	if skip > len(result.List) {
		return filePage{}, &clouddrive.Error{Message: "光鸭目录分页位置未前进", Retryable: true}
	}
	files := make([]clouddrive.File, 0, len(result.List)-skip)
	seen := map[string]bool{}
	for _, item := range result.List[skip:] {
		file, err := item.file(id)
		if err != nil {
			return filePage{}, err
		}
		if file.ParentID != id || seen[file.ID] {
			return filePage{}, &clouddrive.Error{Message: "光鸭返回错误目录或重复文件身份"}
		}
		seen[file.ID] = true
		files = append(files, file)
	}
	more := len(result.List) == pageSize
	var knownTotal *int64
	if result.Total != nil {
		total, err := result.Total.integer()
		if err != nil || total < 0 || total < int64(offset+len(files)) {
			return filePage{}, &clouddrive.Error{Message: "光鸭目录总数格式不正确", Retryable: true}
		}
		knownTotal = &total
		more = int64(offset+len(files)) < total
	}
	if more && len(files) == 0 {
		return filePage{}, &clouddrive.Error{Message: "光鸭目录分页未前进", Retryable: true}
	}
	return filePage{files: files, more: more, total: knownTotal}, nil
}

func (c *Client) Mkdir(ctx context.Context, parent, name string) (string, error) {
	if !validID(parent) || name == "" || name == "." || name == ".." || path.Base(name) != name || strings.ContainsAny(name, "\\\r\n\x00") {
		return "", &clouddrive.Error{Message: "光鸭目录名称或父目录无效"}
	}
	if err := c.lock(ctx); err != nil {
		return "", err
	}
	defer c.unlock()
	var result struct {
		ID scalar `json:"fileId"`
	}
	if err := c.requestLocked(ctx, http.MethodPost, apiBase+fileAPI+"file/create_dir", map[string]any{
		"parentId": apiParent(parent), "dirName": name, "failIfNameExist": true,
	}, false, &result); err != nil {
		return "", err
	}
	if !validID(string(result.ID)) || result.ID == "0" || string(result.ID) == parent {
		return "", &clouddrive.Error{Message: "光鸭未返回独立缓存目录身份", Retryable: true}
	}
	return string(result.ID), nil
}

func (c *Client) URL(ctx context.Context, id, _ string) (string, error) {
	if !validID(id) || id == "0" {
		return "", &clouddrive.Error{Message: "光鸭下载文件 ID 无效"}
	}
	if err := c.lock(ctx); err != nil {
		return "", err
	}
	defer c.unlock()
	var result struct {
		SignedURL string `json:"signedURL"`
		URL       string `json:"downloadUrl"`
	}
	if err := c.requestLocked(ctx, http.MethodPost, apiBase+fileAPI+"get_res_download_url", map[string]string{"fileId": id}, false, &result); err != nil {
		return "", err
	}
	raw := strings.TrimSpace(result.SignedURL)
	if raw == "" {
		raw = strings.TrimSpace(result.URL)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Scheme != "https" && u.Scheme != "http" || strings.ContainsAny(raw, "\r\n\\") {
		return "", &clouddrive.Error{Message: "光鸭未返回可用 HTTP 下载地址"}
	}
	return raw, nil
}

func (c *Client) Delete(ctx context.Context, id string) error {
	if !validID(id) || id == "0" {
		return &clouddrive.Error{Message: "拒绝删除光鸭根目录或无效文件"}
	}
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer c.unlock()
	var result struct {
		TaskID scalar `json:"taskId"`
	}
	if err := c.requestLocked(ctx, http.MethodPost, apiBase+fileAPI+"file/delete_file", map[string]any{"fileIds": []string{id}}, false, &result); err != nil {
		return err
	}
	if result.TaskID == "" {
		return nil
	}
	for attempt := 0; attempt < 30; attempt++ {
		var status struct {
			Status scalar `json:"status"`
		}
		if err := c.requestLocked(ctx, http.MethodPost, apiBase+fileAPI+"get_task_status", map[string]string{"taskId": string(result.TaskID)}, false, &status); err != nil {
			return err
		}
		value, err := status.Status.integer()
		if err != nil || value < -1 || value > 3 {
			return &clouddrive.Error{Message: "光鸭删除任务返回未知状态", Retryable: true}
		}
		if value == 2 {
			return nil
		}
		if value == -1 || value == 3 {
			return &clouddrive.Error{Message: "光鸭缓存删除任务失败", Retryable: true}
		}
		if attempt < 29 {
			timer := time.NewTimer(300 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return &clouddrive.Error{Message: "光鸭缓存删除仍未完成，将稍后核验", Retryable: true}
}
