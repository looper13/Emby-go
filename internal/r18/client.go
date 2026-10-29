package r18

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

type Video struct {
	ID            string   `json:"id"`
	ContentID     string   `json:"content_id"`
	ServiceCode   string   `json:"service_code"`
	Number        string   `json:"number"`
	Title         string   `json:"title"`
	OriginalTitle string   `json:"original_title"`
	ReleaseDate   string   `json:"release_date"`
	Duration      int      `json:"duration"`
	Score         float64  `json:"score"`
	MagnetsCount  int      `json:"magnets_count"`
	Rank          int      `json:"rank,omitempty"`
	HasLocal      bool     `json:"has_local"`
	Tags          []string `json:"tags"`
	HasPreview    bool     `json:"has_preview"`
	PreviewURL    string   `json:"preview_url"`
	CoverURL      string   `json:"cover_url"`
	Images        []string `json:"images"`
}

func EncodeID(content, service string) string {
	b, _ := json.Marshal([]string{content, service})
	return base64.RawURLEncoding.EncodeToString(b)
}
func DecodeID(id string) (string, string, error) {
	if len(id) > 1024 {
		return "", "", errors.New("invalid R18 identity")
	}
	b, e := base64.RawURLEncoding.DecodeString(id)
	var p []string
	if e != nil || json.Unmarshal(b, &p) != nil || len(p) != 2 || p[0] == "" || p[1] == "" || len(p[0]) > 256 || len(p[1]) > 64 {
		return "", "", errors.New("invalid R18 identity")
	}
	return p[0], p[1], nil
}

type Query struct {
	Search, Type, Filter, Sort, Order, Year string
	Mode, Period, TagIDs, MainFilter        string
	Duration, Month                         string
	IgnoreWatched                           bool
	Page, Limit                             int
}
type Page struct {
	Items       []Video `json:"items"`
	Page        int     `json:"page"`
	PageSize    int     `json:"page_size"`
	HasMore     bool    `json:"has_more"`
	HasPrevious bool    `json:"has_previous"`
	Total       *int    `json:"total,omitempty"`
}

type TagCatalog struct {
	Categories []TagCategory `json:"categories"`
}

type TagCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Tags []Tag  `json:"tags"`
}

type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Resource struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Number    string   `json:"number"`
	Date      string   `json:"date"`
	Magnet    string   `json:"magnet"`
	Website   string   `json:"website"`
	Origins   []string `json:"origins"`
	DetailURL string   `json:"detail_url"`
	SizeBytes int64    `json:"size_bytes"`
	SizeMB    int64    `json:"size_mb"`
	Tags      []string `json:"tags"`
}
type ResourcePage struct {
	Items      []Resource `json:"items"`
	HasMore    bool       `json:"has_more"`
	NextOffset int        `json:"next_offset"`
}

func SafeHTTPURL(raw string) string {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	return u.String()
}

var torrentHash = regexp.MustCompile(`(?i)^urn:(btih:([0-9a-f]{40}|[a-z2-7]{32})|btmh:1220[0-9a-f]{64})$`)

func SafeMagnet(raw string) string {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u.Scheme != "magnet" || u.Host != "" || u.Path != "" || u.Opaque != "" || u.Fragment != "" {
		return ""
	}
	query, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return ""
	}
	for _, xt := range query["xt"] {
		if torrentHash.MatchString(xt) {
			return u.String()
		}
	}
	return ""
}
