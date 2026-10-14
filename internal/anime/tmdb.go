package anime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"
)

type TMDBSeries struct {
	ID           int64        `json:"id"`
	Name         string       `json:"name"`
	OriginalName string       `json:"original_name"`
	Date         string       `json:"first_air_date"`
	Overview     string       `json:"overview"`
	Seasons      []TMDBSeason `json:"seasons"`
}
type TMDBSeason struct {
	Number   int           `json:"season_number"`
	Name     string        `json:"name"`
	Episodes []TMDBEpisode `json:"episodes"`
}
type TMDBEpisode struct {
	ID       int64  `json:"id"`
	Number   int    `json:"episode_number"`
	Season   int    `json:"season_number"`
	Name     string `json:"name"`
	Date     string `json:"air_date"`
	Overview string `json:"overview"`
}
type TMDBClient struct {
	HTTP    *http.Client
	BaseURL string
	APIKey  string
	mu      sync.Mutex
	next    time.Time
	slots   chan struct{}
}

func NewTMDBClient(apiKey string) *TMDBClient {
	return &TMDBClient{HTTP: PublicClient(), BaseURL: "https://api.themoviedb.org/3", APIKey: apiKey, slots: make(chan struct{}, 2)}
}
func (c *TMDBClient) Validate(ctx context.Context) error {
	var result struct {
		Success bool `json:"success"`
	}
	if err := c.get(ctx, "/authentication", &result); err != nil {
		return err
	}
	if !result.Success {
		return errors.New("tmdb_authentication_failed")
	}
	return nil
}
func (c *TMDBClient) get(ctx context.Context, path string, out any) error {
	if c.APIKey == "" {
		return errors.New("tmdb_not_configured")
	}
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.slots }()
	c.mu.Lock()
	delay := max(0, time.Until(c.next))
	c.next = time.Now().Add(delay + 250*time.Millisecond)
	c.mu.Unlock()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+path, nil)
	if err != nil {
		return errors.New("tmdb_invalid_request")
	}
	query := req.URL.Query()
	query.Set("api_key", c.APIKey)
	req.URL.RawQuery = query.Encode()
	req.Header.Set("User-Agent", "Emby-go/1.0 (anime artwork)")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("tmdb_request_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("tmdb_http_%d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (3<<20)+1))
	if err != nil || len(b) > 3<<20 {
		return errors.New("tmdb_response_limit")
	}
	if err = json.Unmarshal(b, out); err != nil {
		return errors.New("tmdb_invalid_response")
	}
	return nil
}
func (c *TMDBClient) Search(ctx context.Context, q string) ([]TMDBSeries, error) {
	var v struct {
		Results []TMDBSeries `json:"results"`
	}
	err := c.get(ctx, "/search/tv?language=zh-CN&query="+url.QueryEscape(q), &v)
	if v.Results == nil {
		v.Results = []TMDBSeries{}
	}
	if len(v.Results) > 20 {
		v.Results = v.Results[:20]
	}
	return v.Results, err
}
func (c *TMDBClient) Series(ctx context.Context, id int64) (TMDBSeries, error) {
	var v TMDBSeries
	err := c.get(ctx, fmt.Sprintf("/tv/%d?language=zh-CN", id), &v)
	if err == nil && (v.ID != id || len(v.Seasons) > 1000) {
		err = errors.New("tmdb_wrong_series")
	}
	return v, err
}
func (c *TMDBClient) Season(ctx context.Context, id int64, season int) (TMDBSeason, error) {
	var v TMDBSeason
	err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d?language=zh-CN", id, season), &v)
	if err == nil && (v.Number != season || len(v.Episodes) > 2000) {
		err = errors.New("tmdb_wrong_season")
	}
	return v, err
}

var tmdbImagePath = regexp.MustCompile(`^/[a-zA-Z0-9_-]+\.(jpg|png|webp)$`)

func TMDBImageURL(path string) string {
	if !tmdbImagePath.MatchString(path) {
		return ""
	}
	return "https://image.tmdb.org/t/p/w780" + path
}
func (c *TMDBClient) Stills(ctx context.Context, series int64, season, episode int, expectedID int64) ([]string, error) {
	var v struct {
		ID     int64 `json:"id"`
		Stills []struct {
			Path string `json:"file_path"`
		} `json:"stills"`
	}
	err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d/episode/%d/images", series, season, episode), &v)
	if err != nil {
		return nil, err
	}
	if v.ID != expectedID || v.Stills == nil {
		return nil, errors.New("tmdb_wrong_episode_images")
	}
	paths := []string{}
	seen := map[string]bool{}
	for _, s := range v.Stills {
		if TMDBImageURL(s.Path) != "" && !seen[s.Path] {
			paths = append(paths, s.Path)
			seen[s.Path] = true
		}
		if len(paths) == 20 {
			break
		}
	}
	return paths, nil
}
