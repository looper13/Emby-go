package r18

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type Config struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"-"`
	SSLMode  string `json:"ssl_mode"`
}

func (c *Config) Validate() error {
	c.Host, c.Database, c.Username = strings.TrimSpace(c.Host), strings.TrimSpace(c.Database), strings.TrimSpace(c.Username)
	if c.Port == 0 {
		c.Port = 5432
	}
	if c.SSLMode == "" {
		c.SSLMode = "prefer"
	}
	if c.Host == "" || strings.ContainsAny(c.Host, "/\\\x00 \t\r\n") || c.Database == "" || c.Username == "" || c.Port < 1 || c.Port > 65535 {
		return errors.New("PostgreSQL host, port, database and username are required")
	}
	if c.SSLMode != "disable" && c.SSLMode != "prefer" && c.SSLMode != "require" && c.SSLMode != "verify-full" {
		return errors.New("invalid PostgreSQL SSL mode")
	}
	return nil
}

type Client struct{ db *sql.DB }

func Open(c Config) (*Client, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Path: "/" + c.Database, User: url.UserPassword(c.Username, c.Password)}
	q := u.Query()
	q.Set("sslmode", c.SSLMode)
	q.Set("connect_timeout", "5")
	u.RawQuery = q.Encode()
	cfg, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	// Enforce read-only and bounded queries on every pooled connection, not just probes.
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.RuntimeParams["statement_timeout"] = "6000"
	cfg.RuntimeParams["application_name"] = "emby-go-r18-catalog"
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(10 * time.Minute)
	return &Client{db: db}, nil
}

func (c *Client) Close() error { return c.db.Close() }
func (c *Client) Check(ctx context.Context) error {
	for _, query := range []string{
		"SELECT content_id,service_code,dvd_id,title_ja,title_en,release_date,jacket_full_url,gallery_full_first_url,gallery_full_last_url FROM public.r18_video LIMIT 0",
		"SELECT tid,number,title,publish_date,magnet,detail_url,size,size_bytes,website,resource_tags,rejected FROM public.article LIMIT 0",
	} {
		rows, err := c.db.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		rows.Close()
	}
	return nil
}

type Video struct {
	ID            string   `json:"id"`
	ContentID     string   `json:"content_id"`
	ServiceCode   string   `json:"service_code"`
	Number        string   `json:"number"`
	Title         string   `json:"title"`
	OriginalTitle string   `json:"original_title"`
	ReleaseDate   string   `json:"release_date"`
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

const videoColumns = `content_id,service_code,COALESCE(dvd_id,''),COALESCE(title_ja,''),COALESCE(title_en,''),COALESCE(to_char(release_date,'YYYY-MM-DD'),''),COALESCE(jacket_full_url,''),COALESCE(gallery_full_first_url,''),COALESCE(gallery_full_last_url,'')`
const videoFilter = `dvd_id IS NOT NULL AND TRIM(dvd_id)<>''`

func readVideo(row interface{ Scan(...any) error }) (Video, error) {
	var v Video
	var en, first, last string
	err := row.Scan(&v.ContentID, &v.ServiceCode, &v.Number, &v.Title, &en, &v.ReleaseDate, &v.CoverURL, &first, &last)
	v.ID = EncodeID(v.ContentID, v.ServiceCode)
	v.OriginalTitle = en
	if v.Title == "" {
		v.Title = en
	}
	if v.Title == "" {
		v.Title = v.Number
	}
	if v.Title == "" {
		v.Title = v.ContentID
	}
	v.CoverURL = SafeHTTPURL(v.CoverURL)
	v.Images = []string{}
	for _, s := range []string{first, last} {
		if u := SafeHTTPURL(s); u != "" && (len(v.Images) == 0 || v.Images[0] != u) {
			v.Images = append(v.Images, u)
		}
	}
	return v, err
}

type Query struct {
	Search, SearchMode, Service, Cursor, Order string
	Limit                                      int
	Before                                     bool
}
type Page struct {
	Items          []Video `json:"items"`
	NextCursor     string  `json:"next_cursor"`
	PreviousCursor string  `json:"previous_cursor"`
	HasMore        bool    `json:"has_more"`
	HasPrevious    bool    `json:"has_previous"`
}

func (c *Client) List(ctx context.Context, q Query) (Page, error) {
	page := Page{Items: []Video{}}
	if q.Limit <= 0 || q.Limit > 60 {
		q.Limit = 40
	}
	if len([]rune(q.Search)) > 120 || len(q.Service) > 64 {
		return page, errors.New("search is too long")
	}
	where := []string{videoFilter}
	args := []any{}
	bind := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	if q.Service != "" {
		where = append(where, "service_code="+bind(q.Service))
	}
	if term := strings.TrimSpace(q.Search); term != "" {
		if q.SearchMode == "title" {
			escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(term)
			p := bind("%" + escaped + "%")
			where = append(where, "(title_ja ILIKE "+p+" OR title_en ILIKE "+p+")")
		} else {
			p := bind(strings.ToUpper(term))
			l := bind(strings.ToLower(term))
			where = append(where, "(dvd_id="+p+" OR dvd_id="+l+" OR content_id="+l+" OR content_id="+p+")")
		}
	}
	order, op, nulls, nullDate := "DESC", "<", "LAST", "'-infinity'"
	if q.Order == "asc" {
		order, op, nullDate = "ASC", ">", "'infinity'"
	}
	if q.Before {
		nulls = "FIRST"
		if order == "ASC" {
			order, op = "DESC", "<"
		} else {
			order, op = "ASC", ">"
		}
	}
	if q.Cursor != "" {
		content, service, e := DecodeID(q.Cursor)
		if e != nil {
			return page, e
		}
		// Keep item-ID cursors compatible, resolving their date inside this read.
		// Infinity puts undated items last in either direction, even across pages.
		key := "COALESCE(release_date," + nullDate + "),content_id,service_code"
		anchor := "SELECT " + key + " FROM public.r18_video WHERE " + videoFilter + " AND content_id=" + bind(content) + " AND service_code=" + bind(service)
		where = append(where, "("+key+") "+op+" ("+anchor+")")
	}
	query := "SELECT " + videoColumns + " FROM public.r18_video WHERE " + strings.Join(where, " AND ") + " ORDER BY release_date " + order + " NULLS " + nulls + ",content_id " + order + ",service_code " + order + " LIMIT " + bind(q.Limit+1)
	rows, e := c.db.QueryContext(ctx, query, args...)
	if e != nil {
		return page, e
	}
	defer rows.Close()
	for rows.Next() {
		v, e := readVideo(rows)
		if e != nil {
			return page, e
		}
		page.Items = append(page.Items, v)
	}
	if e = rows.Err(); e != nil {
		return page, e
	}
	page.HasMore = len(page.Items) > q.Limit
	if page.HasMore {
		page.Items = page.Items[:q.Limit]
	}
	page.HasPrevious = q.Cursor != ""
	if q.Before {
		page.HasMore, page.HasPrevious = page.HasPrevious, page.HasMore
		slices.Reverse(page.Items)
	}
	if len(page.Items) > 0 {
		page.NextCursor = page.Items[len(page.Items)-1].ID
		page.PreviousCursor = page.Items[0].ID
	}
	return page, nil
}

func (c *Client) Get(ctx context.Context, id string) (Video, error) {
	content, service, e := DecodeID(id)
	if e != nil {
		return Video{}, e
	}
	return readVideo(c.db.QueryRowContext(ctx, "SELECT "+videoColumns+" FROM public.r18_video WHERE "+videoFilter+" AND content_id=$1 AND service_code=$2", content, service))
}

func (c *Client) Estimate(ctx context.Context) (int64, error) {
	var n int64
	err := c.db.QueryRowContext(ctx, "SELECT GREATEST(reltuples,0)::bigint FROM pg_class WHERE oid='public.r18_video'::regclass").Scan(&n)
	return n, err
}

type Resource struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Number    string   `json:"number"`
	Date      string   `json:"date"`
	Magnet    string   `json:"magnet"`
	Website   string   `json:"website"`
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

func NumberCandidates(v Video) []string {
	n := strings.TrimSpace(v.Number)
	if n == "" {
		n = v.ContentID
	}
	result := []string{}
	seen := map[string]bool{}
	for _, s := range []string{n, strings.ToUpper(n), strings.ToLower(n), strings.ReplaceAll(strings.ToUpper(n), "-", ""), strings.ReplaceAll(strings.ToLower(n), "-", "")} {
		if s != "" && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

const resourceColumns = `tid,COALESCE(title,''),COALESCE(number,''),COALESCE(to_char(publish_date,'YYYY-MM-DD'),''),magnet,COALESCE(website,''),COALESCE(detail_url,''),COALESCE(size_bytes,0),COALESCE(size,0),COALESCE(resource_tags,'[]')`

func resourceScope(v Video, first int) (string, []any) {
	args := []any{}
	holders := []string{}
	for _, n := range NumberCandidates(v) {
		holders = append(holders, fmt.Sprintf("$%d", first+len(args)))
		args = append(args, n)
	}
	if len(args) == 0 {
		return "false", args
	}
	return `number IN (` + strings.Join(holders, ",") + `) AND COALESCE(rejected,false)=false AND magnet IS NOT NULL AND magnet<>''`, args
}

func readResource(row interface{ Scan(...any) error }) (Resource, error) {
	var r Resource
	var id int64
	var tags string
	if err := row.Scan(&id, &r.Title, &r.Number, &r.Date, &r.Magnet, &r.Website, &r.DetailURL, &r.SizeBytes, &r.SizeMB, &tags); err != nil {
		return r, err
	}
	r.Magnet = SafeMagnet(r.Magnet)
	if r.Magnet == "" {
		return Resource{}, sql.ErrNoRows
	}
	r.ID, r.DetailURL = strconv.FormatInt(id, 10), SafeHTTPURL(r.DetailURL)
	r.SizeBytes, r.SizeMB = max(r.SizeBytes, 0), max(r.SizeMB, 0)
	_ = json.Unmarshal([]byte(tags), &r.Tags)
	if r.Tags == nil {
		r.Tags = []string{}
	}
	return r, nil
}

func (c *Client) Resources(ctx context.Context, v Video, offset int) (ResourcePage, error) {
	page := ResourcePage{Items: []Resource{}}
	if offset < 0 || offset > 10000 {
		return page, errors.New("invalid resource offset")
	}
	scope, args := resourceScope(v, 1)
	args = append(args, offset)
	query := `SELECT ` + resourceColumns + ` FROM public.article WHERE ` + scope + ` ORDER BY publish_date DESC NULLS LAST,tid DESC LIMIT 51 OFFSET ` + fmt.Sprintf("$%d", len(args))
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		r, err := readResource(rows)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return page, err
		}
		count++
		if count > 50 {
			page.HasMore = true
			break
		}
		if err == nil {
			page.Items = append(page.Items, r)
		}
	}
	page.NextOffset = offset + min(count, 50)
	page.HasMore = page.HasMore && page.NextOffset <= 10000
	return page, rows.Err()
}

// Resource shares list ownership/filter rules without replaying any pages.
func (c *Client) Resource(ctx context.Context, v Video, id string) (Resource, error) {
	tid, err := strconv.ParseInt(id, 10, 64)
	if err != nil || tid <= 0 {
		return Resource{}, sql.ErrNoRows
	}
	scope, args := resourceScope(v, 2)
	return readResource(c.db.QueryRowContext(ctx, `SELECT `+resourceColumns+` FROM public.article WHERE tid=$1 AND `+scope, append([]any{tid}, args...)...))
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
