package alist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

var ErrNotFound = errors.New("AList 文件不存在")

type File struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"is_dir"`
	Modified string `json:"modified"`
}

type Client struct {
	base  *url.URL
	token string
	http  *http.Client
}

func New(address, token string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(address, "/"))
	if err != nil || u == nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("请先在设置中配置有效的 AList 服务地址")
	}
	if strings.ContainsAny(token, "\x00\r\n") {
		return nil, errors.New("AList Token 格式不正确")
	}
	return &Client{base: u, token: token, http: &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}, nil
}

// Decode exactly once and require this configured AList origin. Never send the
// management token to a STRM URL or follow its redirects to a cloud provider.
func (c *Client) Resolve(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.User != nil || u.Fragment != "" || !strings.EqualFold(u.Scheme, c.base.Scheme) || !strings.EqualFold(u.Host, c.base.Host) {
		return "", errors.New("STRM 不是所配置 AList 地址的链接，无法定位远程文件")
	}
	prefix := strings.TrimRight(c.base.EscapedPath(), "/")
	for _, route := range []string{"/d/", "/p/", "/dav/"} {
		if !strings.HasPrefix(u.EscapedPath(), prefix+route) {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(u.EscapedPath(), prefix+route), "/")
		for i, segment := range parts {
			decoded, err := url.PathUnescape(segment)
			if err != nil || decoded == "" || decoded == "." || decoded == ".." || strings.ContainsAny(decoded, "/\\\x00\r\n") {
				return "", errors.New("STRM 中的 AList 文件路径不安全或不完整")
			}
			parts[i] = decoded
		}
		return "/" + strings.Join(parts, "/"), nil
	}
	return "", errors.New("只支持 AList 的 /d/、/p/ 或 /dav/ 文件链接，网盘直链无法反推路径")
}

func (c *Client) call(ctx context.Context, endpoint string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + "/api/fs/" + endpoint
	u.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return errors.New("AList 请求创建失败")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("AList 连接失败或请求超时")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("AList 接口 HTTP %d，请检查地址与权限", resp.StatusCode)
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&envelope); err != nil {
		return errors.New("AList 返回格式不正确")
	}
	if envelope.Code != 200 {
		message := strings.ToLower(strings.TrimSpace(envelope.Message))
		if endpoint == "get" && (envelope.Code == 404 || envelope.Code == 500) &&
			(strings.Contains(message, "object not found") || message == "file not found") {
			return ErrNotFound
		}
		// Upstream messages can contain credentials/URLs. Report the code only.
		return fmt.Errorf("AList 接口返回 %d，请检查 Token、路径与删除权限", envelope.Code)
	}
	if output != nil {
		if err := json.Unmarshal(envelope.Data, output); err != nil {
			return errors.New("AList 文件信息格式不正确")
		}
	}
	return nil
}

// Ask AList for this exact path. Directory listing and pagination are not
// needed to locate a file whose path already comes from the STRM.
func (c *Client) Stat(ctx context.Context, filePath string) (File, error) {
	var file File
	if err := c.call(ctx, "get", map[string]any{"path": filePath, "password": ""}, &file); err != nil {
		return File{}, err
	}
	if file.Name != path.Base(filePath) || file.IsDir || file.Size < 0 || file.Modified == "" {
		return File{}, errors.New("AList 目标不是可核验的普通文件")
	}
	return file, nil
}

func (c *Client) Remove(ctx context.Context, filePath string) error {
	return c.call(ctx, "remove", map[string]any{"dir": path.Dir(filePath), "names": []string{path.Base(filePath)}}, nil)
}
