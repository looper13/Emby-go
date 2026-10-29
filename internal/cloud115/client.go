package cloud115

import (
	"context"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/OpenListTeam/115-sdk-go"
)

type Tokens struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Cookie  string `json:"cookie,omitempty"`
}

type RemoteTask struct {
	Hash             string
	Status, Progress int
	ResultID         string
	FolderID         string
}

type File struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA1      string `json:"sha1"`
	PickCode  string `json:"pick_code"`
	Directory bool   `json:"directory"`
	Duration  int64  `json:"duration"`
}

type Provider interface {
	Check(context.Context, string) (string, error)
	Find(context.Context, string) (*RemoteTask, error)
	Add(context.Context, string, string) error
	Info(context.Context, string) (File, error)
	List(context.Context, string, int) ([]File, bool, error)
	URL(context.Context, string, string) (string, error)
}

type Error struct {
	Message                  string
	Auth, Retryable, Missing bool
}

func (e *Error) Error() string { return e.Message }

var signedURL = regexp.MustCompile(`https?://[^\s"<>]+`)

func SafeError(err error, tokens Tokens) error {
	if err == nil {
		return nil
	}
	var prior *Error
	hasPrior := errors.As(err, &prior)
	message := err.Error()
	secrets := []string{tokens.Access, tokens.Refresh, tokens.Cookie}
	for _, part := range strings.Split(tokens.Cookie, ";") {
		_, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && value != "" {
			secrets = append(secrets, value)
		}
	}
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	message = signedURL.ReplaceAllString(message, "[URL]")
	if len(message) > 500 {
		message = message[:500]
	}
	e := &Error{Message: message, Retryable: true}
	if hasPrior {
		e.Auth = prior.Auth
		e.Retryable = prior.Retryable
		e.Missing = prior.Missing
		return e
	}
	var api *sdk.Error
	if errors.As(err, &api) {
		e.Auth = api.Code == 99 || sdk.Is401Started(api.Code)
		e.Missing = errors.Is(api, sdk.ErrObjectNotFound)
		e.Retryable = e.Auth || api.Code == 429 || api.Code >= 500 && api.Code < 600
	}
	return e
}

func NormalizeMagnet(raw string) (string, string, error) {
	if len(raw) > 8192 {
		return "", "", errors.New("磁力链接过长")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "magnet" || u.Host != "" || u.Fragment != "" {
		return "", "", errors.New("请输入有效的 magnet 链接")
	}
	var hash string
	for _, xt := range u.Query()["xt"] {
		if !strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
			continue
		}
		value := xt[len("urn:btih:"):]
		var b []byte
		if len(value) == 40 {
			b, err = hex.DecodeString(value)
		} else if len(value) == 32 {
			b, err = base32.StdEncoding.DecodeString(strings.ToUpper(value))
		} else {
			err = errors.New("invalid hash")
		}
		if err != nil || len(b) != 20 {
			return "", "", errors.New("磁力 InfoHash 格式不正确")
		}
		next := hex.EncodeToString(b)
		if hash != "" && next != hash {
			return "", "", errors.New("磁力包含冲突的 InfoHash")
		}
		hash = next
	}
	if hash == "" {
		return "", "", errors.New("磁力缺少 BTIH InfoHash（暂不支持仅 BTv2 的磁力）")
	}
	q := u.Query()
	q.Set("xt", "urn:btih:"+hash)
	u.RawQuery = q.Encode()
	return u.String(), hash, nil
}

type Client struct {
	gate       chan struct{}
	sdk        *sdk.Client
	tokens     Tokens
	persist    func(Tokens) error
	persistErr error
}

// The SDK mutates tokens during refresh. Serialize it and rate-limit every HTTP
// request, including its internal refresh/retry requests, without logging bodies.
func New(tokens Tokens, persist func(Tokens) error) Provider {
	if tokens.Cookie != "" {
		return newCookieClient(tokens.Cookie, http.DefaultTransport, time.Second)
	}
	return newClient(tokens, persist, http.DefaultTransport, time.Second)
}
func newClient(tokens Tokens, persist func(Tokens) error, transport http.RoundTripper, interval time.Duration) *Client {
	c := &Client{tokens: tokens, persist: persist, gate: make(chan struct{}, 1)}
	c.sdk = sdk.New().SetHttpClient(&http.Client{Transport: &limitedTransport{base: transport, interval: interval}, Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}).SetAccessToken(tokens.Access).SetRefreshToken(tokens.Refresh)
	c.sdk.SetOnRefreshToken(func(access, refresh string) {
		c.tokens = Tokens{Access: access, Refresh: refresh}
		if c.persist != nil {
			c.persistErr = c.persist(c.tokens)
		}
	})
	return c
}

type limitedTransport struct {
	mu       sync.Mutex
	base     http.RoundTripper
	next     time.Time
	interval time.Duration
}

func (t *limitedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	wait := time.Until(t.next)
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-r.Context().Done():
			timer.Stop()
			t.mu.Unlock()
			return nil, r.Context().Err()
		case <-timer.C:
		}
	}
	t.next = time.Now().Add(t.interval)
	t.mu.Unlock()
	response, err := t.base.RoundTrip(r)
	if err == nil && response.StatusCode >= 400 {
		response.Body.Close()
		return nil, &Error{Message: fmt.Sprintf("115 HTTP %d", response.StatusCode), Auth: response.StatusCode == 401, Retryable: response.StatusCode == 401 || response.StatusCode == 429 || response.StatusCode >= 500}
	}
	return response, err
}
func (c *Client) finish(err error) error {
	if c.persistErr != nil {
		return &Error{Message: "保存刷新后的 115 凭证失败，请重新授权", Auth: true}
	}
	e := SafeError(err, c.tokens)
	// passport refresh errors are untyped in this SDK.
	var api *sdk.Error
	if e != nil && !errors.As(err, &api) && (strings.Contains(e.Error(), "refresh") || strings.Contains(e.Error(), "401") || strings.HasPrefix(e.Error(), "code:") || strings.HasPrefix(e.Error(), "error:")) {
		return &Error{Message: e.Error(), Auth: true}
	}
	return e
}
func (c *Client) lock(ctx context.Context) error {
	select {
	case c.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			c.unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *Client) unlock() { <-c.gate }
func (c *Client) Check(ctx context.Context, folder string) (string, error) {
	if err := c.lock(ctx); err != nil {
		return "", err
	}
	defer c.unlock()
	user, err := c.sdk.UserInfo(ctx)
	if err = c.finish(err); err != nil {
		return "", err
	}
	if user.UserID <= 0 {
		return "", errors.New("115 未返回有效账号")
	}
	info, err := c.sdk.GetFolderInfo(ctx, folder)
	if err = c.finish(err); err != nil {
		return "", err
	}
	if info.FileCategory != "0" || info.FileID != folder {
		return "", errors.New("115 工作目录必须是现有文件夹")
	}
	return strconv.FormatInt(user.UserID, 10), nil
}
func (c *Client) Find(ctx context.Context, hash string) (*RemoteTask, error) {
	if err := c.lock(ctx); err != nil {
		return nil, err
	}
	defer c.unlock()
	for page := 1; page <= 100; page++ {
		var resp sdk.OfflineTaskListResp
		// SDK OfflineTaskList uses form data on GET; this endpoint expects a query.
		_, err := c.sdk.AuthRequest(ctx, sdk.ApiOfflineList, http.MethodGet, &resp, sdk.ReqWithQuery(sdk.Form{"page": strconv.Itoa(page)}))
		if err = c.finish(err); err != nil {
			return nil, err
		}
		for _, task := range resp.Tasks {
			if strings.EqualFold(task.InfoHash, hash) {
				return &RemoteTask{Hash: hash, Status: task.Status, Progress: task.PercentDone, ResultID: task.FileID}, nil
			}
		}
		if page >= resp.PageCount {
			return nil, nil
		}
	}
	return nil, &Error{Message: "115 任务超过 100 页，无法完整对账，未重复提交"}
}
func (c *Client) Add(ctx context.Context, magnet, folder string) error {
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer c.unlock()
	var resp []sdk.AddOfflineTaskURIsResp
	_, err := c.sdk.AuthRequest(ctx, sdk.ApiAddOffline, http.MethodPost, &resp, sdk.ReqWithForm(sdk.Form{"urls": magnet, "wp_path_id": folder}))
	if err = c.finish(err); err != nil {
		return err
	}
	if len(resp) != 1 {
		return &Error{Message: "115 提交结果不明确，将先对账而非再次提交", Retryable: true}
	}
	if !resp[0].State || resp[0].InfoHash == "" {
		return c.finish(&sdk.Error{Code: resp[0].Code, Message: resp[0].Message})
	}
	_, hash, err := NormalizeMagnet(magnet)
	if err != nil || !strings.EqualFold(resp[0].InfoHash, hash) {
		return &Error{Message: "115 返回的任务 InfoHash 不匹配"}
	}
	return nil
}
func (c *Client) Info(ctx context.Context, id string) (File, error) {
	if err := c.lock(ctx); err != nil {
		return File{}, err
	}
	defer c.unlock()
	f, err := c.sdk.GetFolderInfo(ctx, id)
	if err = c.finish(err); err != nil {
		return File{}, err
	}
	return File{ID: f.FileID, Name: f.FileName, Size: f.SizeByte, SHA1: f.Sha1, PickCode: f.PickCode, Directory: f.FileCategory == "0", Duration: f.PlayLong}, nil
}
func (c *Client) List(ctx context.Context, id string, offset int) ([]File, bool, error) {
	if err := c.lock(ctx); err != nil {
		return nil, false, err
	}
	defer c.unlock()
	r, err := c.sdk.GetFiles(ctx, &sdk.GetFilesReq{CID: id, Offset: int64(offset), Limit: 1000, Cur: 1, ShowDir: true, Stdir: 1, O: "file_name", ASC: true})
	if err = c.finish(err); err != nil {
		return nil, false, err
	}
	files := make([]File, 0, len(r.Data))
	for _, f := range r.Data {
		duration, _ := f.PlayLong.Int64()
		file := File{ID: f.Fid, ParentID: f.Pid, Name: f.Fn, Size: f.FS, SHA1: f.Sha1, PickCode: f.Pc, Directory: f.Fc == "0", Duration: duration}
		if f.Aid != "" && f.Aid != "1" {
			file.Directory = false
			file.Size = 0
			file.PickCode = ""
		}
		files = append(files, file)
	}
	more := int64(offset+len(r.Data)) < r.Count
	if more && len(r.Data) == 0 {
		return nil, false, &Error{Message: "115 目录分页未前进"}
	}
	return files, more, nil
}
func (c *Client) URL(ctx context.Context, code, ua string) (string, error) {
	if err := c.lock(ctx); err != nil {
		return "", err
	}
	defer c.unlock()
	r, err := c.sdk.DownURL(ctx, code, ua)
	if err = c.finish(err); err != nil {
		return "", err
	}
	if len(r) != 1 {
		return "", &Error{Message: "115 下载地址数量不正确"}
	}
	raw := ""
	for _, item := range r {
		if item.PickCode != "" && item.PickCode != code {
			return "", &Error{Message: "115 返回的文件提取码不匹配"}
		}
		raw = item.URL.URL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(raw, "\r\n\\") {
		return "", &Error{Message: "115 未返回可用 HTTP 下载地址"}
	}
	return u.String(), nil
}

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

func Discover(ctx context.Context, p Provider, root, workFolder string) ([]File, error) {
	if root == "" || root == "0" || root == workFolder {
		return nil, &Error{Message: "115 未提供独立结果目录，拒绝扫描整个工作目录", Retryable: true}
	}
	first, err := p.Info(ctx, root)
	if err != nil {
		return nil, err
	}
	if first.ID != root {
		return nil, &Error{Message: "115 返回的结果文件身份不匹配"}
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
					return nil, &Error{Message: "115 返回无效文件身份或名称"}
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
						return nil, &Error{Message: "115 分页重复文件，扫描未完成"}
					}
					seen[f.ID] = true
					files = append(files, f)
				}
			}
			if !more {
				break
			}
			if len(batch) == 0 {
				return nil, &Error{Message: "115 分页未前进"}
			}
			offset += len(batch)
		}
	}
	if len(files) == 0 {
		return nil, &Error{Message: "任务完成，但未找到可用视频（图片、压缩包和样片不作为正片）", Retryable: true, Missing: scanned == 0}
	}
	return files, nil
}
