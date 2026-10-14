package anime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultMikanURL = "https://mikan.tangbai.cc"
const BangumiURL = "https://api.bgm.tv"
const responseLimit = 8 << 20

type Settings struct {
	MikanURL string `json:"mikan_url"`
	Revision int64  `json:"revision"`
}

func DefaultSettings() Settings { return Settings{MikanURL: DefaultMikanURL, Revision: 1} }

func ValidateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("来源地址须为不含账号、路径或查询参数的 HTTP/HTTPS 域名")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicIP(ip) {
		return errors.New("资料来源不能是私有网络地址")
	}
	return nil
}

func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}

// PublicClient fetches source metadata and images. Outbound traffic honours
// HTTP_PROXY/HTTPS_PROXY/NO_PROXY through http.ProxyFromEnvironment, the same
// way the MetaTube and translate clients do.
//
// A direct connection still validates every resolved IP in DialContext, so DNS
// rebinding cannot turn a saved metadata/image hostname into an internal
// network proxy. A proxied connection is dialled to the proxy instead, so the
// same guarantee moves one step earlier: the target hostname is resolved and
// checked before the request is handed over. The rebinding window widens from
// one connection to one request, since the proxy resolves the name again.
func PublicClient() *http.Client {
	direct := http.DefaultTransport.(*http.Transport).Clone()
	direct.Proxy = nil
	direct.DialContext = dialPublicIP
	via := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{Transport: publicTransport{direct: direct, via: via, proxy: http.ProxyFromEnvironment}, Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("source_redirect_limit")
		}
		if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			return errors.New("source_cross_host_redirect")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("source_invalid_redirect")
		}
		return nil
	}}
}

type publicTransport struct {
	direct *http.Transport
	via    *http.Transport
	proxy  func(*http.Request) (*url.URL, error)
}

func (t publicTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	proxyURL, err := t.proxy(req)
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		return t.direct.RoundTrip(req)
	}
	if _, err = resolvePublic(req.Context(), req.URL.Hostname()); err != nil {
		return nil, err
	}
	return t.via.RoundTrip(req)
}

// resolvePublic returns a host's addresses only when every one of them is
// public, so a proxied request cannot be pointed at the internal network
// either. IP literals are decided without a lookup.
func resolvePublic(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if !publicIP(ip) {
			return nil, errors.New("source_private_address")
		}
		return []net.IP{ip}, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, errors.New("source_dns_failed")
	}
	if len(addrs) == 0 {
		return nil, errors.New("source_dns_empty")
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if !publicIP(addr.IP) {
			return nil, errors.New("source_private_address")
		}
		ips = append(ips, addr.IP)
	}
	return ips, nil
}

func dialPublicIP(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := resolvePublic(ctx, host)
	if err != nil {
		return nil, err
	}
	var last error
	dialer := net.Dialer{Timeout: 8 * time.Second}
	for _, ip := range ips {
		conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if e == nil {
			return conn, nil
		}
		last = e
	}
	return nil, last
}

type Client struct {
	Context       context.Context
	HTTP          *http.Client
	MikanURL      string
	BangumiURL    string
	mu            sync.Mutex
	limits        map[string]*sourceLimit
	searches      map[string]searchSnapshot
	searchFlights map[string]*searchFlight
}

type searchFlight struct {
	done chan struct{}
	data []byte
	err  error
}

type searchSnapshot struct {
	data []byte
	at   time.Time
}

// Search snapshots are disposable, private to one source client, and limited to
// eight responses. Changing the configured source creates a new client/cache.
func (c *Client) searchRequest(ctx context.Context, method, endpoint string, body []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := method + ":" + endpoint + ":" + string(body)
	c.mu.Lock()
	old, ok := c.searches[key]
	if ok && time.Since(old.at) < 15*time.Minute {
		c.mu.Unlock()
		RecordCacheHit(ctx)
		return old.data, nil
	}
	flight := c.searchFlights[key]
	if flight == nil {
		if len(c.searchFlights) >= 8 {
			c.mu.Unlock()
			return nil, errors.New("source_search_busy")
		}
		if c.searchFlights == nil {
			c.searchFlights = map[string]*searchFlight{}
		}
		flight = &searchFlight{done: make(chan struct{})}
		c.searchFlights[key] = flight
		owner := c.Context
		if owner == nil {
			owner = context.Background()
		}
		if metrics := ctx.Value(requestMetricsKey{}); metrics != nil {
			owner = context.WithValue(owner, requestMetricsKey{}, metrics)
		}
		// A leaving page must not cancel another reader's shared search. Work is
		// bounded by both the application's lifetime and a one-minute deadline.
		go func() {
			requestCtx, cancel := context.WithTimeout(owner, time.Minute)
			defer cancel()
			data, err := c.request(requestCtx, method, endpoint, body)
			c.mu.Lock()
			defer c.mu.Unlock()
			if err == nil {
				if c.searches == nil {
					c.searches = map[string]searchSnapshot{}
				}
				if len(c.searches) >= 8 {
					oldestKey, oldest := "", time.Now()
					for k, v := range c.searches {
						if v.at.Before(oldest) {
							oldestKey, oldest = k, v.at
						}
					}
					delete(c.searches, oldestKey)
				}
				c.searches[key] = searchSnapshot{data: data, at: time.Now()}
			}
			flight.data, flight.err = data, err
			delete(c.searchFlights, key)
			close(flight.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		return flight.data, flight.err
	}
}

type sourceLimit struct {
	next  time.Time
	slots chan struct{}
}

func NewClient(mikan string) *Client {
	return &Client{HTTP: PublicClient(), MikanURL: strings.TrimRight(mikan, "/"), BangumiURL: BangumiURL}
}

// Each client is shared by the service. Spaced requests and bounded retries
// apply equally to pagination, detail refreshes, and connection tests.
func (c *Client) request(ctx context.Context, method, endpoint string, body []byte) ([]byte, error) {
	return c.requestBounded(ctx, method, endpoint, body, responseLimit)
}

func (c *Client) requestBounded(ctx context.Context, method, endpoint string, body []byte, limit int) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.New("source_invalid_request")
	}
	c.mu.Lock()
	if c.limits == nil {
		c.limits = map[string]*sourceLimit{}
	}
	limiter := c.limits[u.Host]
	if limiter == nil {
		limiter = &sourceLimit{slots: make(chan struct{}, 2)}
		c.limits[u.Host] = limiter
	}
	c.mu.Unlock()
	select {
	case limiter.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-limiter.slots }()
	var retryAfter time.Duration
	for attempt := 0; attempt < 3; attempt++ {
		c.mu.Lock()
		delay := time.Until(limiter.next)
		if delay < 0 {
			delay = 0
		}
		if attempt > 0 {
			delay = max(delay, time.Duration(1<<attempt)*time.Second, retryAfter)
		}
		limiter.next = time.Now().Add(delay + time.Second)
		c.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("source_invalid_request")
		}
		req.Header.Set("User-Agent", "Emby-go/1.0 (anime-catalog; public metadata)")
		req.Header.Set("Accept", "application/json, text/html, application/rss+xml")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		metrics, _ := ctx.Value(requestMetricsKey{}).(*RequestMetrics)
		if metrics != nil {
			metrics.requests.Add(1)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if metrics != nil {
				metrics.failures.Add(1)
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < 2 {
				continue
			}
			return nil, errors.New("source_request_failed")
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			if metrics != nil {
				metrics.failures.Add(1)
			}
			resp.Body.Close()
			retryAfter = 0
			if seconds, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && seconds > 0 {
				retryAfter = time.Duration(seconds) * time.Second
			} else if when, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
				retryAfter = max(0, time.Until(when))
			}
			// Long upstream cooldowns belong to a later refresh, not a held request.
			if retryAfter > time.Minute {
				return nil, fmt.Errorf("source_http_%d", resp.StatusCode)
			}
			if attempt < 2 {
				continue
			}
			return nil, fmt.Errorf("source_http_%d", resp.StatusCode)
		}
		if resp.StatusCode != 200 {
			if metrics != nil {
				metrics.failures.Add(1)
			}
			resp.Body.Close()
			return nil, fmt.Errorf("source_http_%d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
		resp.Body.Close()
		if err != nil {
			if metrics != nil {
				metrics.failures.Add(1)
			}
			return nil, errors.New("source_read_failed")
		}
		if len(data) > limit {
			if metrics != nil {
				metrics.failures.Add(1)
			}
			return nil, errors.New("source_response_too_large")
		}
		if metrics != nil {
			metrics.successes.Add(1)
		}
		return data, nil
	}
	return nil, errors.New("source_retry_exhausted")
}

type bgmSubject struct {
	ID       int64  `json:"id"`
	Type     int    `json:"type"`
	Name     string `json:"name"`
	NameCN   string `json:"name_cn"`
	Summary  string `json:"summary"`
	Platform string `json:"platform"`
	Date     string `json:"date"`
	Episodes int    `json:"total_episodes"`
	Images   struct {
		Common string `json:"common"`
		Large  string `json:"large"`
	} `json:"images"`
	Rating struct {
		Score float64 `json:"score"`
	} `json:"rating"`
}

func (b bgmSubject) subject() Subject {
	name := b.NameCN
	if name == "" {
		name = b.Name
	}
	img := b.Images.Common
	if img == "" {
		img = b.Images.Large
	}
	return Subject{BangumiID: b.ID, Name: name, OriginalName: b.Name, Summary: b.Summary, Platform: b.Platform, Image: img, Date: b.Date, Rating: b.Rating.Score, Episodes: b.Episodes}
}
func ParseSubject(data []byte) (Subject, error) {
	var b bgmSubject
	if err := json.Unmarshal(data, &b); err != nil {
		return Subject{}, err
	}
	if b.ID <= 0 || b.Type != 2 || b.Name == "" {
		return Subject{}, errors.New("invalid_bangumi_animation")
	}
	return b.subject(), nil
}

type bgmEpisode struct {
	ID        int64       `json:"id"`
	SubjectID int64       `json:"subject_id"`
	Name      string      `json:"name"`
	NameCN    string      `json:"name_cn"`
	Summary   string      `json:"desc"`
	Sort      json.Number `json:"sort"`
	Number    json.Number `json:"ep"`
	Type      int         `json:"type"`
	Date      string      `json:"airdate"`
}

func ParseEpisodes(data []byte, subjectID int64) ([]Episode, int, int, error) {
	var page struct {
		Data   []bgmEpisode `json:"data"`
		Total  int          `json:"total"`
		Offset int          `json:"offset"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, 0, 0, err
	}
	if page.Data == nil || page.Total < 0 || page.Total > 10000 {
		return nil, 0, 0, errors.New("invalid_episode_page")
	}
	out := []Episode{}
	for _, e := range page.Data {
		if e.SubjectID != subjectID {
			return nil, 0, 0, errors.New("wrong_episode_subject")
		}
		season := 0
		number, _ := e.Sort.Float64()
		if e.Type == 0 && number == math.Trunc(number) {
			season = 1
		}
		name := e.NameCN
		if name == "" {
			name = e.Name
		}
		ep := Episode{BangumiID: e.ID, Name: name, OriginalName: e.Name, Summary: e.Summary, Sort: e.Sort, Number: e.Number, Active: true, Type: e.Type, Season: season, AirDate: e.Date}
		if err := ValidateEpisode(ep); err != nil {
			return nil, 0, 0, err
		}
		out = append(out, ep)
	}
	return out, page.Total, page.Offset, nil
}
func (c *Client) Subject(ctx context.Context, id int64) (Subject, []Episode, error) {
	if id <= 0 {
		return Subject{}, nil, errors.New("invalid_subject_id")
	}
	data, err := c.request(ctx, "GET", c.BangumiURL+"/v0/subjects/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return Subject{}, nil, err
	}
	sub, err := ParseSubject(data)
	if err != nil {
		return sub, nil, err
	}
	if sub.BangumiID != id {
		return sub, nil, errors.New("wrong_subject")
	}
	episodes := []Episode{}
	seen := map[int64]bool{}
	expectedTotal := -1
	for offset := 0; offset < 10000; {
		data, err = c.request(ctx, "GET", fmt.Sprintf("%s/v0/episodes?subject_id=%d&limit=100&offset=%d", c.BangumiURL, id, offset), nil)
		if err != nil {
			return sub, nil, err
		}
		page, total, actual, e := ParseEpisodes(data, id)
		if e != nil {
			return sub, nil, e
		}
		if actual != offset || (expectedTotal >= 0 && expectedTotal != total) {
			return sub, nil, errors.New("episode_pages_changed")
		}
		expectedTotal = total
		for _, ep := range page {
			if seen[ep.BangumiID] {
				return sub, nil, errors.New("duplicate_episode_page")
			}
			seen[ep.BangumiID] = true
		}
		episodes = append(episodes, page...)
		offset += len(page)
		if offset >= total {
			return sub, episodes, nil
		}
		if len(page) == 0 {
			return sub, nil, errors.New("incomplete_episode_pages")
		}
	}
	return sub, nil, errors.New("episode_limit")
}
func (c *Client) SearchSubjects(ctx context.Context, keyword string, offset int) ([]Subject, int, error) {
	body, _ := json.Marshal(map[string]any{"keyword": keyword, "sort": "match", "filter": map[string]any{"type": []int{2}}})
	data, err := c.searchRequest(ctx, "POST", fmt.Sprintf("%s/v0/search/subjects?limit=20&offset=%d", c.BangumiURL, offset), body)
	if err != nil {
		return nil, 0, err
	}
	var page struct {
		Data  []bgmSubject `json:"data"`
		Total int          `json:"total"`
	}
	if err = json.Unmarshal(data, &page); err != nil || page.Data == nil {
		return nil, 0, errors.New("invalid_search_response")
	}
	out := []Subject{}
	for _, s := range page.Data {
		if s.Type == 2 && s.ID > 0 {
			out = append(out, s.subject())
		}
	}
	return out, page.Total, nil
}
