package cloud115

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"emby-go/internal/clouddrive"
	sdk "github.com/OpenListTeam/115-sdk-go"
)

type Tokens struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Cookie  string `json:"cookie,omitempty"`
}

type RemoteTask = clouddrive.RemoteTask
type File = clouddrive.File
type Provider = clouddrive.Provider
type Error = clouddrive.Error

func SafeError(err error, tokens Tokens) error {
	if err == nil {
		return nil
	}
	var prior *Error
	hasPrior := errors.As(err, &prior)
	secrets := []string{tokens.Access, tokens.Refresh, tokens.Cookie}
	for _, part := range strings.Split(tokens.Cookie, ";") {
		_, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && value != "" {
			secrets = append(secrets, value)
		}
	}
	e := clouddrive.SafeError(err, secrets...).(*Error)
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
	return clouddrive.NormalizeMagnet(raw)
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
	queue    []*transportWaiter
	changed  chan struct{}
	bypasses int
}

func (t *limitedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	started := time.Now()
	if err := t.admit(r.Context()); err != nil {
		logRequestTiming(r, time.Since(started), 0, true)
		return nil, err
	}
	waited := time.Since(started)
	network := time.Now()
	response, err := t.base.RoundTrip(r)
	logRequestTiming(r, waited, time.Since(network), err != nil || response != nil && response.StatusCode >= 400)
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

func VideoFile(f File) bool {
	return clouddrive.VideoFile(f)
}

func Discover(ctx context.Context, p Provider, root, workFolder string) ([]File, error) {
	return clouddrive.Discover(ctx, p, root, workFolder, "115")
}

// DiscoverFrom 复用本次请求的新鲜元数据，保留原有的身份核验和扫描边界。
func DiscoverFrom(ctx context.Context, p Provider, root, workFolder string, first File) ([]File, error) {
	return clouddrive.DiscoverFrom(ctx, p, root, workFolder, first, "115")
}
