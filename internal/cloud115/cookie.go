package cloud115

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/SheltonZhu/115driver/pkg/crypto/m115"
)

const CacheFolderName = "缓存-24h自动删除"
const CacheLifetime = 24 * time.Hour

const cookieOfflineAppVersion = "27.0.5.7"
const cookieOfflineUserAgent = "Mozilla/5.0 115disk/" + cookieOfflineAppVersion + " 115Browser/" + cookieOfflineAppVersion + " 115wangpan_android/" + cookieOfflineAppVersion

// Cache operations are separate from legacy OAuth providers. Only dedicated,
// service-owned children of the named cache root may be removed by the worker.
type CacheProvider interface {
	Provider
	Mkdir(context.Context, string, string) (string, error)
	Delete(context.Context, string) error
	RemoveTask(context.Context, string) error
}

func NormalizeCookie(raw string) (string, error) {
	if len(raw) > 16384 || strings.ContainsAny(raw, "\r\n") {
		return "", errors.New("115 Cookie 格式不正确")
	}
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "Cookie:"))
	req := &http.Request{Header: http.Header{"Cookie": {raw}}}
	values := map[string]string{}
	for _, c := range req.Cookies() {
		if _, duplicate := values[c.Name]; duplicate {
			return "", errors.New("115 Cookie 包含重复字段")
		}
		values[c.Name] = c.Value
	}
	parts := []string{}
	for _, name := range []string{"UID", "CID", "SEID", "KID"} {
		if values[name] == "" {
			if name == "KID" {
				continue
			}
			return "", errors.New("115 Cookie 必须包含 UID、CID 和 SEID")
		}
		parts = append(parts, name+"="+values[name])
	}
	return strings.Join(parts, "; "), nil
}

type CookieClient struct {
	http   *http.Client
	cookie string
}

func newCookieClient(cookie string, transport http.RoundTripper, interval time.Duration) *CookieClient {
	return &CookieClient{cookie: cookie, http: &http.Client{
		Timeout:       30 * time.Second,
		Transport:     &limitedTransport{base: transport, interval: interval},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Web APIs mix strings and numbers for IDs, sizes and error codes.
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
func (s scalar) integer() int64 { n, _ := strconv.ParseInt(string(s), 10, 64); return n }

type cookieResponse struct {
	State    *bool           `json:"state"`
	Code     scalar          `json:"code"`
	Errno    scalar          `json:"errno"`
	Errcode  scalar          `json:"errcode"`
	Message  string          `json:"message"`
	Error    string          `json:"error"`
	ErrorMsg string          `json:"error_msg"`
	Data     json.RawMessage `json:"data"`
}

func (c *CookieClient) request(ctx context.Context, method, raw string, form url.Values, ua string, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, body)
	if err != nil {
		return &Error{Message: "115 请求地址无效"}
	}
	req.Header.Set("Cookie", c.cookie)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Referer", "https://115.com/")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return SafeError(err, Tokens{Cookie: c.cookie})
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &Error{Message: fmt.Sprintf("115 HTTP %d", resp.StatusCode), Retryable: resp.StatusCode >= 500}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return &Error{Message: "读取 115 响应失败", Retryable: true}
	}
	if len(b) > 8<<20 {
		return &Error{Message: "115 响应超过大小上限"}
	}
	var envelope cookieResponse
	if json.Unmarshal(b, &envelope) != nil || envelope.State == nil {
		return &Error{Message: "115 未返回有效响应，请检查 Cookie 或网盘验证要求", Auth: true}
	}
	if !*envelope.State {
		code := max(envelope.Code.integer(), envelope.Errno.integer(), envelope.Errcode.integer())
		auth := code == 99 || code == 911 || code == 990001 || code == 40101032 || code == 40101035 || code == 40101037
		missing := code == 20018 || code == 70005 || code == 231011 || code == 50015
		message := envelope.Message
		if message == "" {
			message = envelope.Error
		}
		if message == "" {
			message = envelope.ErrorMsg
		}
		if auth {
			message = "Cookie 已失效或需要网盘验证，请更新 Cookie 后重试"
		}
		return SafeError(&Error{Message: fmt.Sprintf("115 错误 %d：%s", code, message), Auth: auth, Missing: missing, Retryable: auth || code == 429 || code >= 500 && code < 600}, Tokens{Cookie: c.cookie})
	}
	if out != nil && json.Unmarshal(b, out) != nil {
		return &Error{Message: "115 响应字段格式不正确"}
	}
	return nil
}

func (c *CookieClient) Check(ctx context.Context, folder string) (string, error) {
	if _, err := NormalizeCookie(c.cookie); err != nil {
		return "", err
	}
	var r struct {
		Data struct {
			ID scalar `json:"user_id"`
		} `json:"data"`
	}
	if err := c.request(ctx, "GET", "https://my.115.com/?ct=ajax&ac=nav", nil, "Mozilla/5.0", &r); err != nil {
		return "", err
	}
	if r.Data.ID.integer() <= 0 {
		return "", &Error{Message: "115 未返回有效账号", Auth: true}
	}
	if folder != "" {
		f, err := c.Info(ctx, folder)
		if err != nil {
			return "", err
		}
		if !f.Directory || f.ID != folder {
			return "", &Error{Message: "115 缓存目录身份不匹配"}
		}
	}
	return string(r.Data.ID), nil
}

type webFile struct {
	ID       scalar `json:"fid"`
	CID      scalar `json:"cid"`
	PID      scalar `json:"pid"`
	Name     string `json:"n"`
	Size     scalar `json:"s"`
	SHA1     string `json:"sha"`
	PickCode string `json:"pc"`
	Duration scalar `json:"play_long"`
}

func (f webFile) file() File {
	directory := f.ID == ""
	id, parent := f.ID, f.CID
	if directory {
		id, parent = f.CID, f.PID
	}
	return File{ID: string(id), ParentID: string(parent), Name: f.Name, Size: f.Size.integer(), SHA1: f.SHA1, PickCode: f.PickCode, Directory: directory, Duration: f.Duration.integer()}
}
func (c *CookieClient) Info(ctx context.Context, id string) (File, error) {
	var r struct {
		Data []webFile `json:"data"`
	}
	if err := c.request(ctx, "GET", "https://webapi.115.com/files/get_info?"+url.Values{"file_id": {id}}.Encode(), nil, "Mozilla/5.0", &r); err != nil {
		return File{}, err
	}
	if len(r.Data) == 0 {
		return File{}, &Error{Message: "115 文件不存在", Missing: true}
	}
	if len(r.Data) != 1 || r.Data[0].file().ID != id {
		return File{}, &Error{Message: "115 返回的文件身份不匹配"}
	}
	return r.Data[0].file(), nil
}
func (c *CookieClient) List(ctx context.Context, id string, offset int) ([]File, bool, error) {
	var r struct {
		CID    scalar    `json:"cid"`
		Count  scalar    `json:"count"`
		Offset scalar    `json:"offset"`
		Data   []webFile `json:"data"`
	}
	q := url.Values{"cid": {id}, "offset": {strconv.Itoa(offset)}, "limit": {"1000"}, "show_dir": {"1"}, "o": {"file_name"}, "asc": {"1"}, "aid": {"1"}, "format": {"json"}}
	if err := c.request(ctx, "GET", "https://webapi.115.com/files?"+q.Encode(), nil, "Mozilla/5.0", &r); err != nil {
		return nil, false, err
	}
	if string(r.CID) != id || r.Offset.integer() != int64(offset) {
		return nil, false, &Error{Message: "115 返回错误目录或分页位置"}
	}
	files := make([]File, 0, len(r.Data))
	for _, f := range r.Data {
		files = append(files, f.file())
	}
	more := int64(offset+len(files)) < r.Count.integer()
	if more && len(files) == 0 {
		return nil, false, &Error{Message: "115 目录分页未前进"}
	}
	return files, more, nil
}
func (c *CookieClient) Mkdir(ctx context.Context, parent, name string) (string, error) {
	var r struct {
		CID scalar `json:"cid"`
	}
	if err := c.request(ctx, "POST", "https://webapi.115.com/files/add", url.Values{"pid": {parent}, "cname": {name}}, "Mozilla/5.0", &r); err != nil {
		return "", err
	}
	if r.CID.integer() <= 0 {
		return "", &Error{Message: "115 未返回有效缓存目录 ID"}
	}
	return string(r.CID), nil
}
func (c *CookieClient) Delete(ctx context.Context, id string) error {
	return c.request(ctx, "POST", "https://webapi.115.com/rb/delete", url.Values{"fid[0]": {id}}, "Mozilla/5.0", nil)
}
func (c *CookieClient) RemoveTask(ctx context.Context, hash string) error {
	return c.request(ctx, "POST", "https://lixian.115.com/lixian/?ct=lixian&ac=task_del", url.Values{"hash[0]": {hash}, "flag": {"0"}}, "Mozilla/5.0", nil)
}
func (c *CookieClient) Find(ctx context.Context, hash string) (*RemoteTask, error) {
	if len(hash) != 40 {
		return nil, &Error{Message: "115 任务 InfoHash 格式不正确"}
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return nil, &Error{Message: "115 任务 InfoHash 格式不正确"}
	}
	// InfoHash is unique and the server's filter is case-sensitive. Never fall
	// back to account history: an incomplete query is not evidence of absence.
	hash = strings.ToLower(hash)
	var r struct {
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
	if err := c.request(ctx, "POST", "https://lixian.115.com/lixian/?"+q.Encode(), nil, "Mozilla/5.0", &r); err != nil {
		return nil, err
	}
	page, pageErr := strconv.ParseInt(string(r.Page), 10, 64)
	count, countErr := strconv.ParseInt(string(r.Count), 10, 64)
	pages, pagesErr := strconv.ParseInt(string(r.Pages), 10, 64)
	if pageErr != nil || countErr != nil || pagesErr != nil || page != 1 || count < 0 || count > 1 || pages < 0 || pages > 1 || r.Tasks == nil || int64(len(r.Tasks)) != count || count == 1 && pages != 1 {
		return nil, &Error{Message: "115 按 InfoHash 查询返回不完整或未过滤的任务列表，未扫描历史、未重复提交", Retryable: true}
	}
	if count == 0 {
		return nil, nil
	}
	t := r.Tasks[0]
	if !strings.EqualFold(t.Hash, hash) {
		return nil, &Error{Message: "115 未按 InfoHash 返回目标任务，未扫描历史、未重复提交", Retryable: true}
	}
	return &RemoteTask{Hash: hash, Status: t.Status, Progress: int(t.Progress), ResultID: string(t.Result), FolderID: string(t.Folder)}, nil
}

// The upstream decoder panics on short/malformed RSA payloads; turn that into a
// bounded protocol error rather than crashing the service on a remote response.
func decodeCookiePayload(encoded string, key m115.Key) (plain []byte, err error) {
	defer func() {
		if recover() != nil {
			plain = nil
			err = errors.New("115 加密响应无效")
		}
	}()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data)%128 != 0 {
		return nil, errors.New("115 加密响应无效")
	}
	return m115.Decode(encoded, key)
}
func (c *CookieClient) encrypted(ctx context.Context, raw string, params any, ua string, out any) error {
	key := m115.GenerateKey()
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	var r struct {
		Data string `json:"data"`
	}
	if err := c.request(ctx, "POST", raw+"&t="+strconv.FormatInt(time.Now().Unix(), 10), url.Values{"data": {m115.Encode(b, key)}}, ua, &r); err != nil {
		return err
	}
	plain, err := decodeCookiePayload(r.Data, key)
	if err != nil || json.Unmarshal(plain, out) != nil {
		return &Error{Message: "115 加密响应字段无效", Retryable: true}
	}
	return nil
}
func (c *CookieClient) Add(ctx context.Context, magnet, folder string) error {
	uid := ""
	req := &http.Request{Header: http.Header{"Cookie": {c.cookie}}}
	for _, ck := range req.Cookies() {
		if ck.Name == "UID" {
			uid = strings.Split(ck.Value, "_")[0]
		}
	}
	var r struct {
		State  *bool `json:"state"`
		Result []struct {
			State *bool  `json:"state"`
			Hash  string `json:"info_hash"`
			Code  scalar `json:"errcode"`
		} `json:"result"`
	}
	// This endpoint selects its RSA protocol using the app User-Agent. A generic
	// browser UA returns plaintext "decode fail!" even with a valid Cookie.
	err := c.encrypted(ctx, "https://lixian.115.com/lixianssp/?ac=add_task_urls", map[string]string{"ac": "add_task_urls", "wp_path_id": folder, "app_ver": cookieOfflineAppVersion, "uid": uid, "url[0]": magnet}, cookieOfflineUserAgent, &r)
	if err != nil {
		return err
	}
	_, hash, err := NormalizeMagnet(magnet)
	if err != nil || r.State != nil && !*r.State || len(r.Result) != 1 || r.Result[0].State != nil && !*r.Result[0].State || !strings.EqualFold(r.Result[0].Hash, hash) {
		return &Error{Message: "115 提交结果不明确或被拒绝，将先对账而非再次提交", Retryable: true}
	}
	return nil
}
func (c *CookieClient) URL(ctx context.Context, code, ua string) (string, error) {
	var r map[string]struct {
		PickCode string `json:"pick_code"`
		URL      struct {
			URL string `json:"url"`
		} `json:"url"`
	}
	if err := c.encrypted(ctx, "https://proapi.115.com/app/chrome/downurl?", map[string]string{"pickcode": code}, ua, &r); err != nil {
		return "", err
	}
	if len(r) != 1 {
		return "", &Error{Message: "115 下载地址数量不正确"}
	}
	for _, f := range r {
		u, err := url.Parse(f.URL.URL)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Scheme != "http" && u.Scheme != "https" || f.PickCode != code || strings.ContainsAny(f.URL.URL, "\r\n\\") {
			return "", &Error{Message: "115 未返回可用 HTTP 下载地址"}
		}
		return f.URL.URL, nil
	}
	return "", &Error{Message: "115 未返回下载地址"}
}

func EnsureCacheFolder(ctx context.Context, p CacheProvider) (string, error) {
	for offset := 0; offset < 10000; {
		files, more, err := p.List(ctx, "0", offset)
		if err != nil {
			return "", err
		}
		for _, f := range files {
			if f.Name == CacheFolderName {
				if !f.Directory || f.ID == "" || f.ID == "0" {
					return "", &Error{Message: "同名缓存路径不是文件夹"}
				}
				return f.ID, nil
			}
		}
		if !more {
			return p.Mkdir(ctx, "0", CacheFolderName)
		}
		if len(files) == 0 {
			return "", &Error{Message: "115 根目录分页未前进"}
		}
		offset += len(files)
	}
	return "", &Error{Message: "115 根目录超过 10000 项，无法确认缓存目录"}
}
