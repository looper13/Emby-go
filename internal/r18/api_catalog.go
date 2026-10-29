package r18

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

var ErrUnsupportedCatalogQuery = errors.New("JavDB API 不支持此列表筛选或排序：浏览可按发行日期或评分排序、筛选单一年份；搜索仅支持相关度排序")

func catalogQuery(q CatalogQuery) (Query, error) {
	query := Query{Mode: "latest", Search: strings.TrimSpace(q.Search), Type: "all", Filter: "magnets", Limit: 50, Page: 1, Order: "asc"}
	if q.Descending || q.Sort == "" {
		query.Order = "desc"
	}
	if q.Start < 0 || q.Start >= 9999*50 || q.Limit < 1 || q.Limit > 1000 || len([]rune(q.Search)) > 120 {
		return query, errors.New("无效的 JavDB 列表分页")
	}
	if q.NameStartsWith != "" || q.NameStartsWithOrGreater != "" || q.NameLessThan != "" || len(q.Years) > 1 {
		return query, ErrUnsupportedCatalogQuery
	}
	if query.Search != "" {
		if q.Sort != "" && !strings.EqualFold(q.Sort, "Relevance") {
			return query, ErrUnsupportedCatalogQuery
		}
		query.Sort = "relevance"
	} else {
		switch strings.ToLower(q.Sort) {
		case "", "premieredate", "productionyear", "datecreated", "release":
			query.Sort = "release"
		case "communityrating", "score":
			query.Sort = "score"
		case "hit", "want_watch_count", "watched_count":
			query.Sort = strings.ToLower(q.Sort)
		default:
			return query, ErrUnsupportedCatalogQuery
		}
	}
	if len(q.Years) == 1 {
		if q.Years[0] < 1 || q.Years[0] > 9999 || query.Search != "" {
			return query, ErrUnsupportedCatalogQuery
		}
		query.Year = strconv.Itoa(q.Years[0])
	}
	return normalizeQuery(query)
}

func (c *Client) CatalogPage(ctx context.Context, q CatalogQuery) (CatalogPage, error) {
	result := CatalogPage{Items: []Video{}}
	query, err := catalogQuery(q)
	if err != nil {
		return result, err
	}
	page, skip := q.Start/50+1, q.Start%50
	// 一次最多取 1000 条；多取一条确认任意偏移处的下一页。
	for fetched := 0; page <= 9999; fetched++ {
		if fetched >= 22 {
			return result, errors.New("JavDB 列表有效记录不足，超过单次分页请求上限")
		}
		query.Page = page
		data, err := c.movies(ctx, query)
		if err != nil {
			return result, err
		}
		// 偏移必须与上游原始位置一致；过滤坏记录后补齐会导致跨页重复。
		videos := make([]Video, 0, len(data.Movies))
		for _, m := range data.Movies {
			v, ok := m.video()
			if !ok {
				return result, errors.New("JavDB 列表包含无效影片，无法保证偏移分页一致性")
			}
			videos = append(videos, v)
		}
		if hasNext(data, query.Limit) && len(videos) != query.Limit {
			return result, errors.New("JavDB 上游页大小不一致，无法保证偏移分页一致性")
		}
		for _, v := range videos[min(skip, len(videos)):] {
			result.Items = append(result.Items, v)
			if len(result.Items) > q.Limit {
				result.Items = result.Items[:q.Limit]
				result.HasMore = true
				return result, nil
			}
		}
		if !hasNext(data, query.Limit) {
			return result, nil
		}
		skip = 0
		page++
		if len(result.Items) == q.Limit {
			result.HasMore = page <= 9999
			return result, nil
		}
		if len(data.Movies) == 0 {
			return result, errors.New("JavDB 上游返回空页但声明存在下一页")
		}
	}
	return result, nil
}
