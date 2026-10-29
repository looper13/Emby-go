package r18

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	MaxCategoryTags  = 5
	MaxRankingMovies = 1000
	MaxTagCategories = 100
	MaxCatalogTags   = 2000
)

var categoryTagID = regexp.MustCompile(`^[0-4]:[A-Za-z0-9_-]{1,120}$`)
var catalogTagID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,120}$`)

func rankedMode(mode string) bool {
	return mode == "ranking" || mode == "playback" || mode == "popular"
}

func contentType(value string, allowAll, allowAnime bool) bool {
	return (allowAll && value == "all") || value == "0" || value == "1" || value == "2" || value == "3" || (allowAnime && value == "4")
}

func positiveNumber(value string, maximum int) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n >= 1 && n <= maximum && strconv.Itoa(n) == value
}

func normalizeQuery(q Query) (Query, error) {
	q.Search, q.Type, q.Filter, q.Sort = strings.TrimSpace(q.Search), strings.TrimSpace(q.Type), strings.TrimSpace(q.Filter), strings.TrimSpace(q.Sort)
	q.Order, q.Year, q.Mode, q.Period = strings.TrimSpace(q.Order), strings.TrimSpace(q.Year), strings.TrimSpace(q.Mode), strings.TrimSpace(q.Period)
	q.TagIDs, q.MainFilter, q.Duration, q.Month = strings.TrimSpace(q.TagIDs), strings.TrimSpace(q.MainFilter), strings.TrimSpace(q.Duration), strings.TrimSpace(q.Month)
	if q.Mode == "" {
		q.Mode = "latest"
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.Limit == 0 {
		q.Limit = 24
	}
	if q.Order == "" {
		q.Order = "desc"
	}
	if q.Type == "" {
		q.Type = "all"
		if q.Mode == "ranking" {
			q.Type = "0"
		}
	}
	if q.Filter == "" {
		q.Filter = "magnets"
		if rankedMode(q.Mode) || q.Mode == "top250" {
			q.Filter = "all"
		}
	}
	if q.Page < 1 || q.Page > 9999 || q.Limit < 1 || q.Limit > 50 || len([]rune(q.Search)) > 120 || strings.ContainsAny(q.Search, "\x00\r\n") ||
		(q.Order != "asc" && q.Order != "desc") || !contentType(q.Type, true, true) ||
		(q.Filter != "all" && q.Filter != "magnets" && q.Filter != "subtitle" && q.Filter != "can_play") {
		return q, errors.New("无效的 JavDB 查询条件")
	}
	if q.Year != "" && !positiveNumber(q.Year, 9999) {
		return q, errors.New("JavDB 年份必须为 1 到 9999 的整数")
	}
	if q.Mode != "top250" && q.IgnoreWatched {
		return q, errors.New("仅 TOP250 支持忽略已看筛选")
	}
	if q.Mode != "categories" && (q.TagIDs != "" || q.MainFilter != "" || q.Duration != "" || q.Month != "") {
		return q, errors.New("分类标签、主分类、时长和月份仅支持类别浏览")
	}
	if !rankedMode(q.Mode) && q.Period != "" {
		return q, errors.New("仅排行榜、热播和热门影片支持周期筛选")
	}
	if q.Search != "" {
		if q.Sort == "" {
			q.Sort = "relevance"
		}
		if q.Mode != "latest" || q.Sort != "relevance" || q.Year != "" {
			return q, errors.New("JavDB 搜索仅支持普通影片相关度排序，不支持年份或类别榜单组合")
		}
		return q, nil
	}
	switch q.Mode {
	case "latest", "categories":
		if q.Sort == "" {
			q.Sort = "release"
		}
		switch q.Sort {
		case "release", "score", "hit", "want_watch_count", "watched_count":
		case "update":
			if q.Mode != "latest" || q.Year != "" {
				return q, errors.New("磁链更新时间排序不支持分类或年份筛选")
			}
		default:
			return q, errors.New("无效的 JavDB 列表排序")
		}
		if q.Mode == "categories" {
			return normalizeCategories(q)
		}
	case "top250":
		if !contentType(q.Type, true, false) || (q.Year != "" && q.Type != "all") || q.Filter != "all" || q.Sort != "" || q.Order != "desc" || (q.Page-1)*q.Limit >= 250 {
			return q, errors.New("无效的 JavDB TOP250 筛选、排序或页码")
		}
	case "ranking", "playback", "popular":
		if q.Filter != "all" || q.Sort != "" || q.Order != "desc" || q.Year != "" ||
			(q.Mode == "ranking" && !contentType(q.Type, false, false)) || (q.Mode != "ranking" && q.Type != "all") {
			return q, errors.New("榜单仅支持其内容类型和周期筛选，不能重排或过滤榜内影片")
		}
		if q.Period == "" {
			q.Period = "daily"
		}
		if q.Period != "daily" && q.Period != "weekly" && q.Period != "monthly" {
			return q, errors.New("无效的 JavDB 榜单周期")
		}
	default:
		return q, errors.New("无效的 JavDB 浏览模式")
	}
	return q, nil
}

func normalizeCategories(q Query) (Query, error) {
	if q.MainFilter == "" {
		q.MainFilter = "all"
	}
	if q.MainFilter != "all" {
		if len(q.MainFilter) > 128 {
			return q, errors.New("JavDB 主分类条件过长")
		}
		filters, seen := []string{}, map[string]bool{}
		for _, raw := range strings.Split(q.MainFilter, ",") {
			filter := strings.TrimSpace(raw)
			if filter != "p" && filter != "m" && filter != "c" && filter != "s" && filter != "i" && filter != "v" {
				return q, errors.New("无效的 JavDB 主分类")
			}
			if (q.Type == "3" || q.Type == "4") && filter == "v" {
				return q, errors.New("此影片类型不支持当前主分类")
			}
			if !seen[filter] {
				seen[filter] = true
				filters = append(filters, filter)
			}
		}
		if len(filters) > 6 {
			return q, errors.New("最多选择 6 个 JavDB 主分类")
		}
		sort.Strings(filters)
		q.MainFilter = strings.Join(filters, ",")
	}
	if q.Duration != "" && q.Duration != "lt-45" && q.Duration != "45-90" && q.Duration != "90-120" && q.Duration != "gt-120" {
		return q, errors.New("无效的 JavDB 时长筛选")
	}
	if q.Month != "" && !positiveNumber(q.Month, 12) {
		return q, errors.New("JavDB 月份必须为 1 到 12 的整数")
	}
	if len(q.TagIDs) > 1024 {
		return q, errors.New("JavDB 类别标签条件过长")
	}
	if q.TagIDs != "" {
		ids, seen := []string{}, map[string]bool{}
		for _, raw := range strings.Split(q.TagIDs, ",") {
			id := strings.TrimSpace(raw)
			if !categoryTagID.MatchString(id) {
				return q, errors.New("JavDB 标签必须使用类型前缀和有效标识")
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) > MaxCategoryTags {
			return q, errors.New("最多选择 5 个 JavDB 题材标签")
		}
		// 标签组合按稳定顺序规范化，避免同一条件产生不同的查询缓存键。
		sort.Strings(ids)
		q.TagIDs = strings.Join(ids, ",")
	}
	return q, nil
}

func ValidateQuery(q Query) error {
	_, err := normalizeQuery(q)
	return err
}

func NormalizeQuery(q Query) (Query, error) {
	return normalizeQuery(q)
}

func browseRequest(q Query) (string, url.Values) {
	params := url.Values{"page": {strconv.Itoa(q.Page)}, "limit": {strconv.Itoa(q.Limit)}}
	if q.Search != "" {
		params.Set("q", q.Search)
		params.Set("type", "movie")
		params.Set("movie_type", q.Type)
		params.Set("movie_filter_by", q.Filter)
		params.Set("movie_sort_by", q.Sort)
		return "/api/v1/javdb/search", params
	}
	params.Set("type", q.Type)
	if q.Mode == "top250" {
		params.Set("start_rank", "1")
		params.Set("ignore_watched", strconv.FormatBool(q.IgnoreWatched))
		if q.Year != "" {
			params.Set("type", "year")
			params.Set("type_value", q.Year)
		}
		return "/api/v1/javdb/movies/top", params
	}
	params.Set("sort_by", q.Sort)
	params.Set("order_by", q.Order)
	if q.Mode == "latest" && q.Year == "" && (q.Sort == "release" || q.Sort == "update") {
		params.Set("filter_by", q.Filter)
		return "/api/v1/javdb/movies/latest", params
	}
	params.Set("movie_filter_by", q.Filter)
	for key, value := range map[string]string{"year": q.Year, "tag_ids": q.TagIDs, "main_filter": q.MainFilter, "duration": q.Duration, "month": q.Month} {
		if value != "" {
			params.Set(key, value)
		}
	}
	return "/api/v1/javdb/movies/tags", params
}

func (c *Client) rankingPage(ctx context.Context, q Query) (Page, error) {
	result := Page{Items: []Video{}, Page: q.Page, PageSize: q.Limit, HasPrevious: q.Page > 1}
	videos, err := c.rankings(ctx, q)
	if err != nil {
		return result, err
	}
	// 榜单接口不接受分页；保留原始完整顺序，不能重新排序或静默跳过记录。
	total := len(videos)
	start := min((q.Page-1)*q.Limit, total)
	end := min(start+q.Limit, total)
	result.Items, result.Total, result.HasMore = videos[start:end], &total, end < total
	return result, nil
}

func (c *Client) rankingsUncached(ctx context.Context, q Query) ([]Video, error) {
	params := url.Values{"period": {q.Period}}
	path := "/api/v1/javdb/rankings"
	if q.Mode == "ranking" {
		params.Set("type", q.Type)
	} else {
		path += "/playback"
		filter := "all"
		if q.Mode == "popular" {
			filter = "high_score"
		}
		params.Set("filter_by", filter)
	}
	var data moviesPayload
	if err := c.request(ctx, path, params, &data); err != nil {
		return nil, err
	}
	if data.Movies == nil || len(data.Movies) > MaxRankingMovies {
		return nil, errors.New("JavDB 完整榜单格式错误或超过数量限制")
	}
	videos, seen := make([]Video, 0, len(data.Movies)), map[string]bool{}
	for i, m := range data.Movies {
		v, ok := m.video()
		if !ok || seen[m.ID] {
			return nil, errors.New("JavDB 榜单包含无效或重复影片，无法保证排名一致性")
		}
		seen[m.ID], v.Rank = true, i+1
		videos = append(videos, v)
	}
	return videos, nil
}

func validateTopMovies(movies []apiMovie) error {
	previous, seen := 0, map[string]bool{}
	for _, m := range movies {
		if m.Ranking < 1 || m.Ranking > 250 || m.Ranking <= previous || seen[m.ID] {
			return errors.New("JavDB TOP250 包含无效或重复排名")
		}
		if _, ok := m.video(); !ok {
			return errors.New("JavDB TOP250 包含无效影片，无法保证排名一致性")
		}
		previous, seen[m.ID] = m.Ranking, true
	}
	return nil
}

func catalogLabel(raw string) string {
	name := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, raw)
	name = strings.Join(strings.Fields(name), " ")
	runes := []rune(name)
	if len(runes) > MaxMovieTagRunes {
		name = strings.TrimSpace(string(runes[:MaxMovieTagRunes]))
	}
	return name
}

func (c *Client) Tags(ctx context.Context, movieType string) (TagCatalog, error) {
	return c.cachedTags(ctx, movieType)
}

func (c *Client) tagsUncached(ctx context.Context, movieType string) (TagCatalog, error) {
	result := TagCatalog{Categories: []TagCategory{}}
	movieType = strings.TrimSpace(movieType)
	if movieType == "" {
		movieType = "all"
	}
	if !contentType(movieType, true, true) {
		return result, errors.New("无效的 JavDB 标签内容类型")
	}
	var data TagCatalog
	if err := c.request(ctx, "/api/v1/javdb/tags", url.Values{"type": {movieType}, "include_main": {"true"}}, &data); err != nil {
		return result, err
	}
	if data.Categories == nil || len(data.Categories) > MaxTagCategories {
		return result, errors.New("JavDB 标签目录格式错误或超过分类数量限制")
	}
	count, categoryIndexes := 0, map[string]int{}
	for _, group := range data.Categories {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		count += len(group.Tags)
		if count > MaxCatalogTags || group.Tags == nil || !catalogTagID.MatchString(group.ID) || !contentType(group.Type, false, true) || (movieType != "all" && group.Type != movieType) || catalogLabel(group.Name) == "" {
			return result, errors.New("JavDB 标签分类格式错误或超过标签数量限制")
		}
		category := TagCategory{ID: group.ID, Name: catalogLabel(group.Name), Type: group.Type, Tags: []Tag{}}
		categoryKey := category.Type + ":" + category.ID
		seenTags := map[string]bool{}
		if index, exists := categoryIndexes[categoryKey]; exists {
			category = result.Categories[index]
			for _, tag := range category.Tags {
				seenTags[tag.Type+":"+tag.ID] = true
			}
		}
		for _, tag := range group.Tags {
			name := catalogLabel(tag.Name)
			if !catalogTagID.MatchString(tag.ID) || !contentType(tag.Type, false, true) || tag.Type != group.Type || name == "" {
				return result, errors.New("JavDB 标签字段格式不正确")
			}
			key := tag.Type + ":" + tag.ID
			if !seenTags[key] {
				seenTags[key] = true
				category.Tags = append(category.Tags, Tag{ID: tag.ID, Name: name, Type: tag.Type})
			}
		}
		if index, exists := categoryIndexes[categoryKey]; exists {
			result.Categories[index] = category
		} else {
			categoryIndexes[categoryKey] = len(result.Categories)
			result.Categories = append(result.Categories, category)
		}
	}
	return result, nil
}
