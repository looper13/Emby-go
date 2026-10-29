package r18

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"testing"
)

func TestR18APICatalogArbitraryOffsetsAcrossFiftyItemPages(t *testing.T) {
	requested := []int{}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		requested = append(requested, page)
		if req.URL.Path != "/api/v1/javdb/movies/tags" || req.URL.Query().Get("limit") != "50" || req.URL.Query().Get("sort_by") != "release" || req.URL.Query().Get("order_by") != "desc" {
			t.Error("Emby偏移没有映射到有界上游页码")
		}
		movies := []apiMovie{}
		for i := (page - 1) * 50; i < min(page*50, 75); i++ {
			movies = append(movies, fixtureMovie(i))
		}
		writeFixtureResponse(w, map[string]any{"movies": movies, "current_page": page, "has_next": page < 2})
	})
	for _, tc := range []struct {
		start, limit, size int
		more               bool
	}{{0, 2, 2, true}, {49, 4, 4, true}, {50, 25, 25, false}, {74, 4, 1, false}, {75, 4, 0, false}} {
		page, err := client.CatalogPage(context.Background(), CatalogQuery{Start: tc.start, Limit: tc.limit})
		if err != nil || len(page.Items) != tc.size || page.HasMore != tc.more {
			t.Fatal("跨上游50条边界的偏移分页错误", tc, err)
		}
		if tc.size > 0 && page.Items[0].ID != EncodeID(fixtureMovie(tc.start).ID, "javdb") {
			t.Fatal("偏移起点漂移", tc)
		}
	}
	if len(requested) != 6 || requested[1] != 1 || requested[2] != 2 {
		t.Fatal("跨页时上游请求范围不正确", requested)
	}
}

func TestR18APICatalogSupportedYearAndUnsupportedQueries(t *testing.T) {
	hits := 0
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		hits++
		if req.URL.Query().Get("year") != "2024" || req.URL.Query().Get("order_by") != "asc" {
			t.Error("单一年份或发行日期正序没有传到上游")
		}
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{}, "current_page": 1, "has_next": false})
	})
	if _, err := client.CatalogPage(context.Background(), CatalogQuery{Limit: 2, Sort: "PremiereDate", Years: []int{2024}}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []CatalogQuery{
		{Limit: 2, Sort: "SortName"}, {Limit: 2, Sort: "ContentId"}, {Limit: 2, Sort: "unsafe"},
		{Limit: 2, Years: []int{2023, 2024}}, {Limit: 2, Years: []int{0}}, {Limit: 2, Search: "TEST", Years: []int{2024}},
		{Limit: 2, Search: "TEST", Sort: "release"}, {Limit: 2, Search: "TEST", Sort: "PremiereDate"}, {Limit: 2, Search: "TEST", Sort: "CommunityRating"},
		{Limit: 2, NameStartsWith: "A"}, {Limit: 2, NameStartsWithOrGreater: "B"}, {Limit: 2, NameLessThan: "Z"},
	} {
		if _, err := client.CatalogPage(context.Background(), q); !errors.Is(err, ErrUnsupportedCatalogQuery) {
			t.Fatal("上游不支持的全局筛选/排序没有明确拒绝", q, err)
		}
	}
	for _, q := range []CatalogQuery{{Start: -1, Limit: 2}, {Start: 9999 * 50, Limit: 2}, {Limit: 0}, {Limit: 1001}} {
		if _, err := client.CatalogPage(context.Background(), q); err == nil {
			t.Fatal("接受了越界偏移或页大小")
		}
	}
	if hits != 1 {
		t.Fatal("无效或不支持的Emby查询访问了上游", hits)
	}
}

func TestR18APICatalogMapsEmbyRatingAndRelevanceSorts(t *testing.T) {
	current := CatalogQuery{}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if q.Get("movie_filter_by") != "magnets" || q.Get("limit") != "50" {
			t.Error("Emby 目录适配没有使用默认磁力筛选和固定上游页大小")
		}
		if current.Search == "" {
			if req.URL.Path != "/api/v1/javdb/movies/tags" || q.Get("sort_by") != "score" || q.Get("order_by") != "desc" {
				t.Error("Emby CommunityRating 没有映射到上游评分排序")
			}
		} else if req.URL.Path != "/api/v1/javdb/search" || q.Get("q") != current.Search || q.Get("movie_sort_by") != "relevance" || q.Has("sort_by") || q.Has("order_by") {
			t.Error("Emby 关键词搜索没有使用相关度排序")
		}
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{}, "current_page": 1, "has_next": false})
	})
	for _, query := range []CatalogQuery{
		{Limit: 2, Sort: "CommunityRating", Descending: true},
		{Limit: 2, Search: "Fixture", Sort: "Relevance"},
		{Limit: 2, Search: "Fixture"},
	} {
		current = query
		if _, err := client.CatalogPage(context.Background(), query); err != nil {
			t.Fatal("合法 Emby 评分或相关度排序被拒绝", query, err)
		}
	}
}

func TestR18APIClientDoesNotExposeFakeStatisticsOrLegacyCatalogCalls(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil))
	for _, removed := range []string{"Estimate", "CatalogCount", "CatalogList"} {
		if _, exists := clientType.MethodByName(removed); exists {
			t.Fatal("API 客户端仍暴露伪统计或旧目录兼容入口", removed)
		}
	}
}

func TestR18APICatalogRejectsInconsistentEmptyNextPage(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{}, "current_page": 1, "has_next": true})
	})
	if _, err := client.CatalogPage(context.Background(), CatalogQuery{Limit: 2}); err == nil {
		t.Fatal("空页宣称有下一页时没有停止，可能造成无界请求")
	}
}

func TestR18APICatalogRejectsFilteredOrShortIntermediatePages(t *testing.T) {
	invalidMovies := []apiMovie{}
	for i := 0; i < 50; i++ {
		invalidMovies = append(invalidMovies, fixtureMovie(i))
	}
	invalidMovies[49].Number = ""
	for _, tc := range []struct {
		name   string
		movies []apiMovie
	}{
		{"missing number", invalidMovies},
		{"short intermediate page", []apiMovie{fixtureMovie(0), fixtureMovie(1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				hits++
				writeFixtureResponse(w, map[string]any{"movies": tc.movies, "current_page": 1, "has_next": true})
			})
			if _, err := client.CatalogPage(context.Background(), CatalogQuery{Limit: 50}); err == nil || hits != 1 {
				t.Fatal("不能过滤补齐或跨过短页，否则后续 Emby 偏移会重复影片", err, hits)
			}
		})
	}
}
