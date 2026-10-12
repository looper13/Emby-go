package anime

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var mikanEntryPath = regexp.MustCompile(`^/Home/Bangumi/([1-9][0-9]*)$`)
var releasePath = regexp.MustCompile(`(?i)/Home/Episode/([0-9a-f]{40})$`)
var subjectPath = regexp.MustCompile(`^/subject/([1-9][0-9]*)$`)
var resolutionPattern = regexp.MustCompile(`(?i)\b(2160|1080|720|480)[pi]\b|\b4K\b`)
var episodeHintPattern = regexp.MustCompile(`(?:\[| - |第)([0-9]{1,4}(?:\.[0-9]+)?)(?:\]|话|集|\s)`)
var ambiguousPattern = regexp.MustCompile(`(?i)合集|全集|特别篇|\bSP\b|\bOVA\b|\bOAD\b|\bBOX\b|\d+\s*[-~～]\s*\d+`)

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func hasClass(n *html.Node, name string) bool {
	for _, v := range strings.Fields(attr(n, "class")) {
		if v == name {
			return true
		}
	}
	return false
}
func walk(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}
func nodeText(n *html.Node) string {
	var out strings.Builder
	walk(n, func(x *html.Node) {
		if x.Type == html.TextNode {
			out.WriteString(x.Data)
		}
	})
	return strings.TrimSpace(out.String())
}
func ancestor(n *html.Node, tag string) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Data == tag {
			return p
		}
	}
	return n
}
func safeLink(base, raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u = b.ResolveReference(u)
	if (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return ""
	}
	return u.String()
}
func pathID(raw string, re *regexp.Regexp) int64 {
	u, err := url.Parse(raw)
	if err != nil {
		return 0
	}
	match := re.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return 0
	}
	id, _ := strconv.ParseInt(match[1], 10, 64)
	return id
}

func ParseSeason(data []byte, base string, year, season int) ([]Entry, error) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	seen := map[int64]bool{}
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "a" {
			return
		}
		id := pathID(attr(n, "href"), mikanEntryPath)
		if id == 0 || seen[id] {
			return
		}
		day := -1
		for p := n.Parent; p != nil; p = p.Parent {
			if value := attr(p, "data-dayofweek"); value != "" {
				day, _ = strconv.Atoi(value)
				break
			}
		}
		if day < 0 || day > 8 {
			return
		}
		name := attr(n, "title")
		if name == "" {
			name = nodeText(n)
		}
		if name == "" {
			return
		}
		var img string
		walk(ancestor(n, "li"), func(x *html.Node) {
			if v := attr(x, "data-src"); v != "" && img == "" {
				img = safeLink(base, v)
			}
		})
		seen[id] = true
		out = append(out, Entry{ID: id, Name: name, Image: img, Year: year, Season: season, Weekday: day})
	})
	// Announced entries with no releases have a grey poster and no anchor.
	walk(doc, func(n *html.Node) {
		if n.Data != "span" || attr(n, "data-bangumiid") == "" {
			return
		}
		id, _ := strconv.ParseInt(attr(n, "data-bangumiid"), 10, 64)
		if id <= 0 || seen[id] {
			return
		}
		day := -1
		for p := n.Parent; p != nil; p = p.Parent {
			if v := attr(p, "data-dayofweek"); v != "" {
				day, _ = strconv.Atoi(v)
				break
			}
		}
		if day < 0 || day > 8 {
			return
		}
		name := ""
		walk(ancestor(n, "li"), func(x *html.Node) {
			if hasClass(x, "date-text") && attr(x, "title") != "" {
				name = attr(x, "title")
			}
		})
		if name == "" {
			return
		}
		seen[id] = true
		out = append(out, Entry{ID: id, Name: name, Image: safeLink(base, attr(n, "data-src")), Year: year, Season: season, Weekday: day})
	})
	if len(out) == 0 {
		return nil, errors.New("mikan_catalog_empty_or_changed")
	}
	return out, nil
}

func ParseMikanDetail(data []byte, base string, id int64) (Entry, []Release, error) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return Entry{}, nil, err
	}
	e := Entry{ID: id, Weekday: -1}
	walk(doc, func(n *html.Node) {
		if hasClass(n, "bangumi-title") {
			e.Name = nodeText(n)
		}
		if n.Data == "title" && e.Name == "" {
			if name, ok := strings.CutPrefix(nodeText(n), "Mikan Project - "); ok {
				e.Name = strings.TrimSpace(name)
			}
		}
		if n.Data == "a" {
			u, err := url.Parse(attr(n, "href"))
			if err == nil && (u.Hostname() == "bgm.tv" || u.Hostname() == "bangumi.tv" || u.Hostname() == "chii.in") {
				if sid := pathID(u.String(), subjectPath); sid != 0 {
					e.BangumiID = sid
				}
			}
		}
		if hasClass(n, "bangumi-poster") {
			raw := attr(n, "src")
			if raw == "" {
				raw = attr(n, "data-src")
			}
			e.Image = safeLink(base, raw)
		}
	})
	if e.Name == "" {
		return e, nil, errors.New("mikan_detail_changed")
	}
	releases := parseReleaseRows(doc, base, id)
	return e, releases, nil
}

func parseReleaseRows(doc *html.Node, base string, id int64) []Release {
	out := []Release{}
	seen := map[string]bool{}
	walk(doc, func(row *html.Node) {
		if row.Data != "tr" {
			return
		}
		r := Release{MikanID: id}
		walk(row, func(n *html.Node) {
			if n.Data == "a" {
				href := safeLink(base, attr(n, "href"))
				u, _ := url.Parse(href)
				if u != nil {
					match := releasePath.FindStringSubmatch(u.Path)
					if len(match) == 2 {
						r.ID = strings.ToLower(match[1])
						r.Title = nodeText(n)
						r.URL = href
					}
					if strings.HasSuffix(u.Path, ".torrent") {
						r.Torrent = href
					}
					if strings.HasPrefix(u.Path, "/Home/PublishGroup/") {
						r.Group = nodeText(n)
					}
				}
			}
			for _, key := range []string{"data-magnet", "data-clipboard-text"} {
				value := attr(n, key)
				if strings.HasPrefix(value, "magnet:?xt=urn:btih:") {
					r.Magnet = value
				}
			}
		})
		if r.ID == "" || r.Title == "" || seen[r.ID] {
			return
		}
		tds := []string{}
		for n := row.FirstChild; n != nil; n = n.NextSibling {
			if n.Data == "td" {
				tds = append(tds, nodeText(n))
			}
		}
		if len(tds) >= 4 {
			r.SizeText = tds[2]
			r.PublishedAt = mikanTime(tds[3])
		}
		parseReleaseTitle(&r)
		seen[r.ID] = true
		out = append(out, r)
	})
	return out
}

func ParseMikanSearch(data []byte, base string) ([]Release, error) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	known := false
	walk(doc, func(n *html.Node) {
		if hasClass(n, "tbl-border") {
			known = true
		}
	})
	if !known {
		return nil, errors.New("mikan_search_changed")
	}
	return parseReleaseRows(doc, base, 0), nil
}

func parseReleaseTitle(r *Release) {
	r.Resolution = strings.ToUpper(resolutionPattern.FindString(r.Title))
	r.Ambiguous = ambiguousPattern.MatchString(r.Title)
	if match := episodeHintPattern.FindStringSubmatch(r.Title); len(match) > 1 {
		r.EpisodeHint = match[1]
	}
	for _, s := range []string{"简繁", "简日", "繁日", "简体", "繁体", "CHS", "CHT", "简中", "繁中"} {
		if strings.Contains(strings.ToUpper(r.Title), s) {
			r.Subtitle = s
			break
		}
	}
	if r.Group == "" && strings.HasPrefix(r.Title, "[") {
		if end := strings.Index(r.Title, "]"); end > 1 {
			r.Group = r.Title[1:end]
		}
	}
	// Only expose canonical magnet identity; trackers from HTML are untrusted and
	// unnecessary for display/copying. Releases never become playback sources.
	if r.ID != "" {
		r.Magnet = "magnet:?xt=urn:btih:" + r.ID
	}
}

func mikanTime(raw string) string {
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	for _, format := range []string{time.RFC3339, "2006-01-02T15:04:05.999999999", "2006/01/02 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(format, strings.TrimSpace(raw), zone); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func ParseRSS(data []byte, id int64) ([]Release, error) {
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Items []struct {
				Title   string `xml:"title"`
				Link    string `xml:"link"`
				Torrent struct {
					Length int64  `xml:"contentLength"`
					Date   string `xml:"pubDate"`
				} `xml:"torrent"`
				Enclosure struct {
					URL    string `xml:"url,attr"`
					Length int64  `xml:"length,attr"`
				} `xml:"enclosure"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, err
	}
	out := []Release{}
	seen := map[string]bool{}
	for _, item := range feed.Channel.Items {
		u, err := url.Parse(item.Link)
		if err != nil {
			return nil, errors.New("mikan_rss_link")
		}
		match := releasePath.FindStringSubmatch(u.Path)
		if len(match) != 2 || item.Title == "" {
			return nil, errors.New("mikan_rss_changed")
		}
		key := strings.ToLower(match[1])
		if seen[key] {
			continue
		}
		seen[key] = true
		r := Release{ID: key, MikanID: id, Title: item.Title, URL: safeLink(item.Link, item.Link), Torrent: safeLink(item.Link, item.Enclosure.URL), Size: item.Torrent.Length, PublishedAt: mikanTime(item.Torrent.Date)}
		if r.Size == 0 {
			r.Size = item.Enclosure.Length
		}
		parseReleaseTitle(&r)
		out = append(out, r)
	}
	return out, nil
}

func (c *Client) Season(ctx context.Context, year, season int) ([]Entry, error) {
	if year < 1900 || year > time.Now().Year()+1 || season < 1 || season > 4 {
		return nil, errors.New("invalid_season")
	}
	names := []string{"冬", "春", "夏", "秋"}
	data, err := c.request(ctx, "GET", fmt.Sprintf("%s/Home/BangumiCoverFlowByDayOfWeek?year=%d&seasonStr=%s", c.MikanURL, year, url.QueryEscape(names[season-1])), nil)
	if err != nil {
		return nil, err
	}
	return ParseSeason(data, c.MikanURL, year, season)
}
func (c *Client) MikanDetail(ctx context.Context, id int64) (Entry, []Release, error) {
	data, err := c.request(ctx, "GET", fmt.Sprintf("%s/Home/Bangumi/%d", c.MikanURL, id), nil)
	if err != nil {
		return Entry{}, nil, err
	}
	return ParseMikanDetail(data, c.MikanURL, id)
}
func (c *Client) Releases(ctx context.Context, id int64) ([]Release, error) {
	data, err := c.request(ctx, "GET", fmt.Sprintf("%s/RSS/Bangumi?bangumiId=%d", c.MikanURL, id), nil)
	if err != nil {
		return nil, err
	}
	return ParseRSS(data, id)
}
func (c *Client) SearchMikan(ctx context.Context, keyword string) ([]Release, error) {
	data, err := c.searchRequest(ctx, "GET", c.MikanURL+"/Home/Search?searchstr="+url.QueryEscape(keyword), nil)
	if err != nil {
		return nil, err
	}
	return ParseMikanSearch(data, c.MikanURL)
}
