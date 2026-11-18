package r18

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

var ErrAmbiguousNumber = errors.New("JavDB 搜索返回多个相同番号的影片，无法确定评论归属")
var ErrNumberSearchLimit = errors.New("JavDB 番号搜索结果过多，请核对已入库的完整番号后重试")

type Review struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Content   string `json:"content"`
	Score     string `json:"score"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type ReviewPage struct {
	Items    []Review `json:"items"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	HasMore  bool     `json:"has_more"`
	Total    *int     `json:"total,omitempty"`
}

type RelatedList struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	MovieCount *int   `json:"movie_count,omitempty"`
}

type RelatedListPage struct {
	Items    []RelatedList `json:"items"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	HasMore  bool          `json:"has_more"`
	Total    *int          `json:"total,omitempty"`
}

// MatchNumber uses only the indexed number. It preserves digits and version
// suffixes, and never substitutes a title, filename, or fuzzy search result.
func (c *Client) MatchNumber(ctx context.Context, number string) (Video, error) {
	number = strings.TrimSpace(number)
	if number == "" || len(number) > 120 {
		return Video{}, sql.ErrNoRows
	}
	for _, ch := range number {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-_ ", ch)) {
			return Video{}, sql.ErrNoRows
		}
	}
	key := normalizedNumber(number)
	// List already shares bounded, validated search snapshots. Do not acquire a
	// cache request slot around another cached read.
	for page := 1; page <= 4; page++ {
		result, err := c.List(ctx, Query{Search: number, Filter: "all", Page: page, Limit: 50})
		if err != nil {
			return Video{}, err
		}
		var matched Video
		for _, candidate := range result.Items {
			if normalizedNumber(candidate.Number) != key {
				continue
			}
			if matched.ID != "" && matched.ID != candidate.ID {
				return Video{}, ErrAmbiguousNumber
			}
			matched = candidate
		}
		if matched.ID != "" {
			return matched, nil
		}
		if !result.HasMore {
			return Video{}, sql.ErrNoRows
		}
	}
	return Video{}, ErrNumberSearchLimit
}

// The published AVdb frontend reads reviews[].username/user.username,
// score, status_title, created_at and content; lists[].id/name/movies_count.
// Only these public presentation fields leave this client.
type apiReview struct {
	ID        json.RawMessage `json:"id"`
	Username  string          `json:"username"`
	Content   string          `json:"content"`
	Score     json.RawMessage `json:"score"`
	Status    string          `json:"status_title"`
	CreatedAt string          `json:"created_at"`
	User      struct {
		Username string `json:"username"`
	} `json:"user"`
}

type communityPagination struct {
	CurrentPage int             `json:"current_page"`
	HasNext     *bool           `json:"has_next"`
	Total       json.RawMessage `json:"total"`
	TotalCount  json.RawMessage `json:"total_count"`
	TotalItems  json.RawMessage `json:"total_items"`
	Pagination  *struct {
		Total      json.RawMessage `json:"total"`
		TotalCount json.RawMessage `json:"total_count"`
		TotalItems json.RawMessage `json:"total_items"`
	} `json:"pagination"`
}

func communityCount(raw json.RawMessage) (*int, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return nil, nil
	}
	text := string(raw)
	if raw[0] == '"' {
		if json.Unmarshal(raw, &text) != nil {
			return nil, errors.New("JavDB 数量格式不正确")
		}
	}
	count, err := strconv.ParseInt(strings.TrimSpace(text), 10, 32)
	if err != nil || count < 0 {
		return nil, errors.New("JavDB 数量格式不正确")
	}
	value := int(count)
	return &value, nil
}

func (p communityPagination) metadata(page, limit, length int) (*int, bool, error) {
	if length > limit || p.CurrentPage != 0 && p.CurrentPage != page {
		return nil, false, errors.New("JavDB 评论或清单分页格式不正确")
	}
	counts := []json.RawMessage{p.Total, p.TotalCount, p.TotalItems}
	if p.Pagination != nil {
		counts = append(counts, p.Pagination.Total, p.Pagination.TotalCount, p.Pagination.TotalItems)
	}
	var total *int
	for _, raw := range counts {
		count, err := communityCount(raw)
		if err != nil {
			return nil, false, err
		}
		if total == nil {
			total = count
		}
	}
	more := length >= limit
	if total != nil {
		more = page*limit < *total
	}
	if p.HasNext != nil {
		more = *p.HasNext
	}
	return total, page < 9999 && more, nil
}

func communityText(text string, maxRunes int, multiline bool) string {
	text = strings.Map(func(ch rune) rune {
		if unicode.IsControl(ch) && !(multiline && (ch == '\n' || ch == '\t')) {
			return ' '
		}
		return ch
	}, text)
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) > maxRunes {
		return strings.TrimSpace(string(runes[:maxRunes]))
	}
	return text
}

func communityIdentity(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var id string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &id) != nil {
			return ""
		}
	} else {
		count, err := communityCount(raw)
		if err != nil || count == nil {
			return ""
		}
		id = strconv.Itoa(*count)
	}
	if !movieIdentity.MatchString(id) {
		return ""
	}
	return id
}

func communityPageParams(id string, page, limit int) (string, url.Values, error) {
	content, err := javdbID(id)
	if err != nil {
		return "", nil, err
	}
	if page < 1 || page > 9999 || limit < 1 || limit > 50 {
		return "", nil, errors.New("无效的评论或清单分页条件")
	}
	return content, url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(limit)}}, nil
}

func (c *Client) Reviews(ctx context.Context, id string, page, limit int) (ReviewPage, error) {
	result := ReviewPage{Items: []Review{}, Page: page, PageSize: limit}
	content, params, err := communityPageParams(id, page, limit)
	if err != nil {
		return result, err
	}
	key := "reviews:" + content + "?" + params.Encode()
	return cachedBrowseValue(ctx, c.browse, key, browseCacheTTL, func(shared context.Context) (ReviewPage, error) {
		var data struct {
			communityPagination
			Reviews []apiReview `json:"reviews"`
		}
		if err := c.request(shared, "/api/v1/javdb/movies/"+content+"/reviews", params, &data); err != nil {
			return result, err
		}
		if data.Reviews == nil {
			return result, errors.New("JavDB 未返回评论列表，请检查上游 JavDB 登录状态及接口版本")
		}
		total, more, err := data.metadata(page, limit, len(data.Reviews))
		if err != nil {
			return result, err
		}
		result.Total, result.HasMore = total, more
		for index, row := range data.Reviews {
			username := communityText(row.Username, 120, false)
			if username == "" {
				username = communityText(row.User.Username, 120, false)
			}
			review := Review{ID: communityIdentity(row.ID), Username: username,
				Content: communityText(row.Content, 20000, true), Status: communityText(row.Status, 120, false),
				CreatedAt: communityText(row.CreatedAt, 120, false)}
			var scoreText string
			blankScore := json.Unmarshal(row.Score, &scoreText) == nil && strings.TrimSpace(scoreText) == ""
			if len(row.Score) > 0 && string(row.Score) != "null" && !blankScore {
				var score apiScore
				if err := json.Unmarshal(row.Score, &score); err != nil || score > 10 {
					return ReviewPage{}, errors.New("JavDB 评论评分格式不正确")
				}
				review.Score = strconv.FormatFloat(float64(score), 'f', -1, 64)
			}
			if review.ID == "" {
				review.ID = fmt.Sprintf("review-%d-%d", page, index)
			}
			if review.Username == "" {
				review.Username = "匿名用户"
			}
			result.Items = append(result.Items, review)
		}
		return result, nil
	})
}

func (c *Client) RelatedLists(ctx context.Context, id string, page, limit int) (RelatedListPage, error) {
	result := RelatedListPage{Items: []RelatedList{}, Page: page, PageSize: limit}
	content, params, err := communityPageParams(id, page, limit)
	if err != nil {
		return result, err
	}
	params.Set("movie_id", content)
	key := "related-lists:" + params.Encode()
	return cachedBrowseValue(ctx, c.browse, key, browseCacheTTL, func(shared context.Context) (RelatedListPage, error) {
		var data struct {
			communityPagination
			Lists []struct {
				ID         json.RawMessage `json:"id"`
				Name       string          `json:"name"`
				MovieCount json.RawMessage `json:"movies_count"`
			} `json:"lists"`
		}
		if err := c.request(shared, "/api/v1/javdb/lists/related", params, &data); err != nil {
			return result, err
		}
		if data.Lists == nil {
			return result, errors.New("JavDB 未返回相关清单，请检查上游 JavDB 登录状态及接口版本")
		}
		total, more, err := data.metadata(page, limit, len(data.Lists))
		if err != nil {
			return result, err
		}
		result.Total, result.HasMore = total, more
		for _, row := range data.Lists {
			id, name := communityIdentity(row.ID), communityText(row.Name, 300, false)
			count, err := communityCount(row.MovieCount)
			if id == "" || name == "" || err != nil {
				return RelatedListPage{}, errors.New("JavDB 相关清单格式不正确")
			}
			result.Items = append(result.Items, RelatedList{ID: id, Name: name, MovieCount: count})
		}
		return result, nil
	})
}

// ListMovies follows the list_id filter used by the published AVdb list page.
// It shares upstream snapshots only; local ownership is checked separately.
func (c *Client) ListMovies(ctx context.Context, listID string, page, limit int) (Page, error) {
	result := Page{Items: []Video{}, Page: page, PageSize: limit, HasPrevious: page > 1}
	if !movieIdentity.MatchString(listID) || page < 1 || page > 9999 || limit < 1 || limit > 50 {
		return result, errors.New("无效的 JavDB 清单影片查询")
	}
	params := url.Values{"list_id": {listID}, "main_filter": {"all"}, "movie_filter_by": {"all"},
		"type": {"all"}, "order_by": {"desc"}, "page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(limit)}}
	return cachedBrowseValue(ctx, c.browse, "list-movies:"+params.Encode(), browseCacheTTL, func(shared context.Context) (Page, error) {
		var data moviesPayload
		if err := c.request(shared, "/api/v1/javdb/movies/tags", params, &data); err != nil {
			return result, err
		}
		if data.Movies == nil || len(data.Movies) > limit || data.CurrentPage != 0 && data.CurrentPage != page {
			return result, errors.New("JavDB 清单影片分页格式不正确")
		}
		for _, movie := range data.Movies {
			video, valid := movie.video()
			if !valid {
				return result, errors.New("JavDB 清单包含无效影片番号或标识")
			}
			result.Items = append(result.Items, video)
		}
		result.HasMore = page < 9999 && hasNext(data, limit)
		return result, nil
	})
}
