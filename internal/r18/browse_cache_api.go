package r18

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func cachedBrowseValue[T any](ctx context.Context, cache *browseCache, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, error) {
	var result T
	if cache == nil {
		return load(ctx)
	}
	data, err := cache.read(ctx, key, ttl, func(shared context.Context) ([]byte, error) {
		value, err := load(shared)
		if err != nil {
			return nil, err
		}
		// 仅保存已通过字段、数量、身份和排名校验的结果；不缓存失败或原始响应。
		return json.Marshal(value)
	})
	if err != nil {
		return result, err
	}
	if json.Unmarshal(data, &result) != nil {
		return result, errors.New("JavDB 浏览缓存格式不正确")
	}
	return result, nil
}

func (c *Client) movies(ctx context.Context, q Query) (moviesPayload, error) {
	path, params := browseRequest(q)
	key := "movies:" + path + "?" + params.Encode()
	return cachedBrowseValue(ctx, c.browse, key, browseCacheTTL, func(shared context.Context) (moviesPayload, error) {
		return c.moviesUncached(shared, q)
	})
}

func (c *Client) rankings(ctx context.Context, q Query) ([]Video, error) {
	// 同一榜单的不同页面共享完整上游快照，分页只切片，不发多次排行请求。
	key := "ranking:" + q.Mode + ":" + q.Type + ":" + q.Period
	return cachedBrowseValue(ctx, c.browse, key, browseCacheTTL, func(shared context.Context) ([]Video, error) {
		return c.rankingsUncached(shared, q)
	})
}

func (c *Client) cachedTags(ctx context.Context, movieType string) (TagCatalog, error) {
	movieType = strings.TrimSpace(movieType)
	if movieType == "" {
		movieType = "all"
	}
	if !contentType(movieType, true, true) {
		return TagCatalog{Categories: []TagCategory{}}, errors.New("无效的 JavDB 标签内容类型")
	}
	return cachedBrowseValue(ctx, c.browse, "tags:"+movieType, tagCatalogCacheTTL, func(shared context.Context) (TagCatalog, error) {
		return c.tagsUncached(shared, movieType)
	})
}
