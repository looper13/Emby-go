package r18

import (
	"context"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const MaxImages = 100
const MaxMovieTags = 64
const MaxMovieTagRunes = 120
const apiBodyLimit = 8 << 20

type Config struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"-"`
}

func (c *Config) Validate() error {
	c.BaseURL, c.APIKey = strings.TrimSpace(c.BaseURL), strings.TrimSpace(c.APIKey)
	u, err := url.Parse(c.BaseURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(c.BaseURL, "\\\x00\r\n\t ") {
		return errors.New("请填写有效的 AVdb HTTP/HTTPS 基础地址，不包含凭据、查询参数或片段")
	}
	if c.APIKey == "" || len(c.APIKey) > 4096 || strings.ContainsAny(c.APIKey, "\r\n\x00") {
		return errors.New("请填写有效的 AVdb API Key")
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return nil
}

type Client struct {
	cfg    Config
	http   *http.Client
	browse *browseCache
}

func Open(c Config) (*Client, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &Client{cfg: c, browse: newBrowseCache(), http: &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		// API 凭据只发给配置的服务，不跟随上游重定向。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) Close() error {
	if c.browse != nil {
		c.browse.close()
	}
	c.http.CloseIdleConnections()
	return nil
}

func (c *Client) request(ctx context.Context, path string, params url.Values, out any) error {
	u, err := url.Parse(c.cfg.BaseURL + path)
	if err != nil {
		return errors.New("AVdb 接口地址无效")
	}
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New("AVdb 请求创建失败")
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// 不回传可能含凭据或内部地址的上游错误正文。
		return errors.New("AVdb 请求失败或超时")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return sql.ErrNoRows
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("AVdb 接口返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, apiBodyLimit+1))
	if err != nil || len(body) > apiBodyLimit {
		return errors.New("AVdb 响应读取失败或超过大小限制")
	}
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Code == nil {
		return errors.New("AVdb 响应格式不正确")
	}
	if *envelope.Code != 0 {
		return fmt.Errorf("AVdb 业务请求失败（代码 %d）", *envelope.Code)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, out) != nil {
		return errors.New("AVdb 数据格式不正确")
	}
	return nil
}

type apiMovie struct {
	ID           string   `json:"id"`
	Number       string   `json:"number"`
	Title        string   `json:"title"`
	OriginTitle  string   `json:"origin_title"`
	ReleaseDate  string   `json:"release_date"`
	CoverURL     string   `json:"cover_url"`
	ThumbURL     string   `json:"thumb_url"`
	Duration     int      `json:"duration"`
	Score        apiScore `json:"score"`
	MagnetsCount int      `json:"magnets_count"`
	Ranking      int      `json:"ranking"`
	Tags         []struct {
		Name string `json:"name"`
	} `json:"tags"`
	HasPreviewVideo bool   `json:"has_preview_video"`
	PreviewVideoURL string `json:"preview_video_url"`
	Images          []struct {
		LargeURL string `json:"large_url"`
		ThumbURL string `json:"thumb_url"`
	} `json:"preview_images"`
}

type apiScore float64

func (s *apiScore) UnmarshalJSON(data []byte) error {
	var value float64
	var err error
	// JavDB 详情返回字符串评分，列表可能返回数值或 null。
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err = json.Unmarshal(data, &text); err == nil {
			text = strings.TrimSpace(text)
			if text != "" {
				value, err = strconv.ParseFloat(text, 64)
			}
		}
	} else {
		err = json.Unmarshal(data, &value)
	}
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return errors.New("JavDB 评分格式不正确")
	}
	*s = apiScore(max(value, 0))
	return nil
}

var movieIdentity = regexp.MustCompile(`^[A-Za-z0-9_-]{1,120}$`)

func (m apiMovie) video() (Video, bool) {
	v := Video{ContentID: m.ID, ServiceCode: "javdb", Number: strings.TrimSpace(m.Number), Title: m.Title,
		OriginalTitle: m.OriginTitle, ReleaseDate: m.ReleaseDate, CoverURL: SafeHTTPURL(m.CoverURL), Images: []string{},
		Tags: []string{}, HasPreview: m.HasPreviewVideo, PreviewURL: SafeHTTPURL(m.PreviewVideoURL),
		Duration: max(m.Duration, 0), Score: max(float64(m.Score), 0), MagnetsCount: max(m.MagnetsCount, 0), Rank: max(m.Ranking, 0)}
	if !movieIdentity.MatchString(m.ID) || v.Number == "" {
		return v, false
	}
	v.ID = EncodeID(m.ID, "javdb")
	if v.Title == "" {
		v.Title = v.OriginalTitle
	}
	if v.Title == "" {
		v.Title = v.Number
	}
	if v.CoverURL == "" {
		v.CoverURL = SafeHTTPURL(m.ThumbURL)
	}
	// 影片题材只取详情 tags[].name，与磁力资源的字幕、清晰度标签分开。
	seenTags := map[string]bool{}
	for _, tag := range m.Tags {
		name := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, tag.Name)
		name = strings.Join(strings.Fields(name), " ")
		runes := []rune(name)
		if len(runes) > MaxMovieTagRunes {
			name = strings.TrimSpace(string(runes[:MaxMovieTagRunes]))
		}
		if name != "" && !seenTags[name] {
			seenTags[name] = true
			v.Tags = append(v.Tags, name)
		}
		if len(v.Tags) == MaxMovieTags {
			break
		}
	}
	seen := map[string]bool{}
	for _, image := range m.Images {
		raw := SafeHTTPURL(image.LargeURL)
		if raw == "" {
			raw = SafeHTTPURL(image.ThumbURL)
		}
		if raw != "" && !seen[raw] {
			seen[raw] = true
			v.Images = append(v.Images, raw)
		}
		if len(v.Images) == MaxImages {
			break
		}
	}
	return v, true
}

type moviesPayload struct {
	Movies      []apiMovie `json:"movies"`
	CurrentPage int        `json:"current_page"`
	HasNext     *bool      `json:"has_next"`
}

func (c *Client) moviesUncached(ctx context.Context, q Query) (moviesPayload, error) {
	var data moviesPayload
	path, params := browseRequest(q)
	if err := c.request(ctx, path, params, &data); err != nil {
		return data, err
	}
	maxItems := q.Limit
	if q.Mode == "top250" {
		maxItems = min(maxItems, 250-(q.Page-1)*q.Limit)
	}
	if data.Movies == nil || len(data.Movies) > maxItems || (data.CurrentPage != 0 && data.CurrentPage != q.Page) {
		return data, errors.New("JavDB 列表响应格式不正确")
	}
	if q.Mode == "top250" {
		if err := validateTopMovies(data.Movies); err != nil {
			return data, err
		}
	} else {
		for _, m := range data.Movies {
			if _, ok := m.video(); !ok {
				return data, errors.New("JavDB 列表包含无效影片身份或番号")
			}
		}
	}
	return data, nil
}

func hasNext(data moviesPayload, limit int) bool {
	if data.HasNext != nil {
		return *data.HasNext
	}
	return len(data.Movies) >= limit
}

func (c *Client) Check(ctx context.Context) error {
	_, err := c.List(ctx, Query{Limit: 1})
	return err
}

func (c *Client) List(ctx context.Context, q Query) (Page, error) {
	result := Page{Items: []Video{}}
	q, err := normalizeQuery(q)
	if err != nil {
		return result, err
	}
	result.Page, result.PageSize = q.Page, q.Limit
	if rankedMode(q.Mode) {
		return c.rankingPage(ctx, q)
	}
	data, err := c.movies(ctx, q)
	if err != nil {
		return result, err
	}
	for _, m := range data.Movies {
		if v, ok := m.video(); ok {
			result.Items = append(result.Items, v)
		}
	}
	result.HasMore = q.Page < 9999 && hasNext(data, q.Limit)
	if q.Mode == "top250" {
		lastRank := 0
		if len(data.Movies) > 0 {
			lastRank = data.Movies[len(data.Movies)-1].Ranking
		}
		result.HasMore = result.HasMore && q.Page*q.Limit < 250 && lastRank < 250
	}
	result.HasPrevious = q.Page > 1
	return result, nil
}

func javdbID(id string) (string, error) {
	content, service, err := DecodeID(id)
	if err != nil || service != "javdb" || !movieIdentity.MatchString(content) {
		return "", sql.ErrNoRows
	}
	return content, nil
}

func (c *Client) Get(ctx context.Context, id string) (Video, error) {
	content, err := javdbID(id)
	if err != nil {
		return Video{}, err
	}
	var data struct {
		Movie *apiMovie `json:"movie"`
	}
	if err = c.request(ctx, "/api/v1/javdb/movies/"+content, nil, &data); err != nil {
		return Video{}, err
	}
	if data.Movie == nil || data.Movie.ID != content {
		return Video{}, sql.ErrNoRows
	}
	v, ok := data.Movie.video()
	if !ok {
		return Video{}, sql.ErrNoRows
	}
	return v, nil
}

type apiMagnet struct {
	Name       string   `json:"name"`
	MagnetURL  string   `json:"magnet_url"`
	Number     string   `json:"number"`
	CreatedAt  string   `json:"created_at"`
	Size       float64  `json:"size"`
	SizeBytes  int64    `json:"size_bytes"`
	Tags       []string `json:"tags"`
	CNSub      bool     `json:"cnsub"`
	HD         bool     `json:"hd"`
	UHD        bool     `json:"uhd"`
	Uncensored bool     `json:"uncensored"`
}

func magnetID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, xt := range u.Query()["xt"] {
		xt = strings.ToLower(xt)
		if !torrentHash.MatchString(xt) {
			continue
		}
		if strings.HasPrefix(xt, "urn:btih:") {
			hash := strings.TrimPrefix(xt, "urn:btih:")
			if len(hash) == 32 {
				b, err := base32.StdEncoding.DecodeString(strings.ToUpper(hash))
				if err != nil {
					continue
				}
				hash = hex.EncodeToString(b)
			}
			return "btih-" + hash
		}
		return "btmh-" + strings.TrimPrefix(xt, "urn:btmh:")
	}
	return ""
}

func normalizedNumber(raw string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.TrimSpace(raw)))
}

func (c *Client) magnetResources(ctx context.Context, v Video) ([]Resource, error) {
	content, err := javdbID(v.ID)
	if err != nil || v.Number == "" {
		return nil, sql.ErrNoRows
	}
	var data struct {
		OnlineMagnets  []apiMagnet `json:"online_magnets"`
		LibraryMagnets []apiMagnet `json:"library_magnets"`
	}
	if err := c.request(ctx, "/api/v1/javdb/movies/"+content+"/magnets", url.Values{"number": {v.Number}}, &data); err != nil {
		return nil, err
	}
	// 两种来源只读取专用字段；本地资源库磁力不表示已经下载或存在 STRM。
	if data.OnlineMagnets == nil && data.LibraryMagnets == nil {
		return nil, errors.New("AVdb 未返回在线或本地资源库磁力列表")
	}
	resources := []Resource{}
	seen := map[string]int{}
	// 固定在线优先、本地其次，保证分页顺序和双来源标记不随重复行改变。
	for _, source := range []struct {
		origin, website string
		items           []apiMagnet
	}{{"online", "JavDB 在线", data.OnlineMagnets}, {"library", "AVdb 本地资源库", data.LibraryMagnets}} {
		for _, m := range source.items {
			magnet := SafeMagnet(m.MagnetURL)
			id := magnetID(magnet)
			if id == "" || (m.Number != "" && normalizedNumber(m.Number) != normalizedNumber(v.Number)) {
				continue
			}
			tags := []string{}
			seenTags := map[string]bool{}
			addTag := func(raw string) {
				tag := strings.TrimSpace(raw)
				if tag != "" && !seenTags[tag] {
					seenTags[tag] = true
					tags = append(tags, tag)
				}
			}
			for _, tag := range m.Tags {
				addTag(tag)
			}
			for _, tag := range []struct {
				yes  bool
				name string
			}{{m.CNSub, "中文字幕"}, {m.HD, "HD"}, {m.UHD, "4K"}, {m.Uncensored, "破解"}} {
				if tag.yes {
					addTag(tag.name)
				}
			}
			if index, exists := seen[id]; exists {
				r := &resources[index]
				if r.Origins[len(r.Origins)-1] != source.origin {
					r.Origins = append(r.Origins, source.origin)
					r.Website += " / " + source.website
				}
				for _, tag := range r.Tags {
					delete(seenTags, tag)
				}
				for _, tag := range tags {
					if seenTags[tag] {
						r.Tags = append(r.Tags, tag)
					}
				}
				continue
			}
			size := int64(0)
			if m.Size > 0 && m.Size < float64(math.MaxInt64) {
				size = int64(m.Size)
			}
			seen[id] = len(resources)
			resources = append(resources, Resource{ID: id, Title: m.Name, Number: v.Number, Date: m.CreatedAt,
				Magnet: magnet, Website: source.website, Origins: []string{source.origin}, SizeBytes: max(m.SizeBytes, 0), SizeMB: size, Tags: tags})
		}
	}
	return resources, nil
}

func (c *Client) Resources(ctx context.Context, v Video, offset int) (ResourcePage, error) {
	page := ResourcePage{Items: []Resource{}}
	if offset < 0 || offset > 10000 {
		return page, errors.New("无效的磁力分页位置")
	}
	items, err := c.magnetResources(ctx, v)
	if err != nil {
		return page, err
	}
	start := min(offset, len(items))
	end := min(start+50, len(items))
	page.Items = items[start:end]
	page.NextOffset = end
	page.HasMore = end < len(items) && end <= 10000
	return page, nil
}

func (c *Client) Resource(ctx context.Context, v Video, id string) (Resource, error) {
	if !strings.HasPrefix(id, "btih-") && !strings.HasPrefix(id, "btmh-") {
		return Resource{}, sql.ErrNoRows
	}
	items, err := c.magnetResources(ctx, v)
	if err != nil {
		return Resource{}, err
	}
	for _, r := range items {
		if r.ID == id {
			return r, nil
		}
	}
	return Resource{}, sql.ErrNoRows
}
