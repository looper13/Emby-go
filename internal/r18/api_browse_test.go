package r18

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestR18APIBrowseModesForwardExactParameters(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		query      Query
		want       map[string]string
		absent     []string
	}{
		{"latest", "/api/v1/javdb/movies/latest", Query{}, map[string]string{"type": "all", "filter_by": "magnets", "sort_by": "release", "order_by": "desc", "page": "1", "limit": "24"}, []string{"movie_filter_by", "year"}},
		{"latest update", "/api/v1/javdb/movies/latest", Query{Sort: "update", Type: "4", Filter: "subtitle", Page: 2, Limit: 3}, map[string]string{"type": "4", "filter_by": "subtitle", "sort_by": "update", "page": "2", "limit": "3"}, []string{"movie_filter_by"}},
		{"latest advanced", "/api/v1/javdb/movies/tags", Query{Sort: "score", Year: "2024", Type: "2"}, map[string]string{"type": "2", "movie_filter_by": "magnets", "sort_by": "score", "year": "2024"}, []string{"filter_by"}},
		{"categories", "/api/v1/javdb/movies/tags", Query{Mode: "categories", TagIDs: " 1:7,0:23,1:7 ", MainFilter: "p,m,p", Year: "2024", Duration: "45-90", Month: "7", Type: "1", Filter: "can_play"}, map[string]string{"tag_ids": "0:23,1:7", "main_filter": "m,p", "year": "2024", "duration": "45-90", "month": "7", "type": "1", "movie_filter_by": "can_play", "sort_by": "release"}, []string{"filter_by"}},
		{"categories default", "/api/v1/javdb/movies/tags", Query{Mode: "categories"}, map[string]string{"main_filter": "all", "movie_filter_by": "magnets", "sort_by": "release"}, []string{"filter_by", "tag_ids"}},
		{"top250", "/api/v1/javdb/movies/top", Query{Mode: "top250", Year: "2022", IgnoreWatched: true, Page: 2, Limit: 3}, map[string]string{"type": "year", "type_value": "2022", "start_rank": "1", "ignore_watched": "true", "page": "2", "limit": "3"}, []string{"sort_by", "filter_by", "movie_filter_by", "year", "order_by"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != tc.path {
					t.Error("查询模式调用了错误的固定接口", req.URL.Path)
				}
				for key, want := range tc.want {
					if req.URL.Query().Get(key) != want {
						t.Error("查询参数转发错误", key, req.URL.Query().Get(key), want)
					}
				}
				for _, key := range tc.absent {
					if req.URL.Query().Has(key) {
						t.Error("不适用的参数发送到上游", key)
					}
				}
				page := tc.query.Page
				if page == 0 {
					page = 1
				}
				writeFixtureResponse(w, map[string]any{"movies": []apiMovie{}, "current_page": page})
			})
			if _, err := client.List(context.Background(), tc.query); err != nil {
				t.Fatal("合法查询未执行", err)
			}
		})
	}
}

func TestR18APIBrowseRejectsUnsupportedCombinationsBeforeRequest(t *testing.T) {
	hits := 0
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		hits++
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{}})
	})
	for _, query := range []Query{
		{Mode: "unknown"}, {Mode: "categories", TagIDs: "23"},
		{Mode: "categories", TagIDs: "0:1,0:2,0:3,0:4,0:5,0:6"},
		{Mode: "categories", TagIDs: "5:23"}, {Mode: "categories", TagIDs: "0:../23"},
		{Mode: "categories", TagIDs: "0:23", MainFilter: "evil"},
		{Mode: "categories", MainFilter: "p,all"}, {Mode: "categories", MainFilter: "p,,m"},
		{Mode: "categories", MainFilter: "v", Type: "4"}, {Mode: "categories", TagIDs: strings.Repeat("0:1,", 257)},
		{Mode: "categories", TagIDs: "0:23", Duration: "free-text"},
		{Mode: "categories", TagIDs: "0:23", Month: "13"},
		{Mode: "latest", TagIDs: "0:23"}, {Mode: "latest", Period: "daily"},
		{Mode: "latest", IgnoreWatched: true}, {Mode: "latest", Duration: "lt-45"},
		{Mode: "latest", Sort: "update", Year: "2024"},
		{Mode: "ranking", Type: "4"}, {Mode: "ranking", Type: "all"}, {Mode: "ranking", Filter: "magnets"},
		{Mode: "ranking", Sort: "score"}, {Mode: "ranking", Order: "asc"},
		{Mode: "ranking", Period: "yearly"}, {Mode: "ranking", Year: "2024"},
		{Mode: "playback", Type: "0"}, {Mode: "popular", Filter: "subtitle"},
		{Mode: "top250", Type: "4"}, {Mode: "top250", Type: "0", Year: "2024"},
		{Mode: "top250", Sort: "score"}, {Mode: "top250", Filter: "magnets"},
		{Mode: "top250", Page: 12, Limit: 24},
		{Mode: "categories", TagIDs: "0:23", Search: "TEST"},
		{Mode: "top250", Search: "TEST"}, {Search: "TEST", Year: "2024"},
	} {
		if err := ValidateQuery(query); err == nil {
			t.Error("接受了不受支持的查询组合", query)
		}
		if _, err := client.List(context.Background(), query); err == nil {
			t.Error("无效查询执行成功", query)
		}
	}
	if hits != 0 {
		t.Fatal("校验失败仍访问了上游", hits)
	}
}

func TestR18APIRankingLocallyPagesCompleteOrderedArray(t *testing.T) {
	for _, mode := range []string{"ranking", "playback", "popular"} {
		t.Run(mode, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				q := req.URL.Query()
				if q.Has("page") || q.Has("limit") || q.Has("sort_by") || q.Get("period") != "weekly" {
					t.Error("全量榜单发送了不存在的上游分页或排序参数")
				}
				if mode == "ranking" {
					if req.URL.Path != "/api/v1/javdb/rankings" || q.Get("type") != "0" {
						t.Error("排行榜请求不正确")
					}
				} else {
					filter := "all"
					if mode == "popular" {
						filter = "high_score"
					}
					if req.URL.Path != "/api/v1/javdb/rankings/playback" || q.Get("filter_by") != filter || q.Has("type") {
						t.Error("热播或热门请求不正确")
					}
				}
				movies := []apiMovie{}
				for i := 0; i < 60; i++ {
					movies = append(movies, fixtureMovie(60-i))
				}
				writeFixtureResponse(w, map[string]any{"movies": movies})
			})
			page, err := client.List(context.Background(), Query{Mode: mode, Period: "weekly", Page: 3, Limit: 24})
			if err != nil || len(page.Items) != 12 || page.HasMore || !page.HasPrevious || page.Total == nil || *page.Total != 60 || page.Items[0].Rank != 49 || page.Items[0].Number != fixtureMovie(12).Number {
				t.Fatal("全量榜单分页、总数或上游排名顺序不正确", err)
			}
			encoded, _ := json.Marshal(page)
			if !strings.Contains(string(encoded), `"total":60`) {
				t.Fatal("完整榜单未提供真实总数")
			}
		})
	}
}

func TestR18APIRankingRejectsInvalidOrUnboundedArray(t *testing.T) {
	for _, kind := range []string{"oversize", "missing number", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			movies := []apiMovie{fixtureMovie(1), fixtureMovie(2)}
			switch kind {
			case "oversize":
				movies = nil
				for i := 0; i <= 1000; i++ {
					movies = append(movies, fixtureMovie(i))
				}
			case "missing number":
				movies[0].Number = ""
			case "duplicate":
				movies[1] = movies[0]
			}
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				writeFixtureResponse(w, map[string]any{"movies": movies})
			})
			if _, err := client.List(context.Background(), Query{Mode: "ranking"}); err == nil {
				t.Fatal("接受无界或错位榜单")
			}
		})
	}
}

func TestR18APITop250RetainsRankAndBoundsLastPage(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		movies := []apiMovie{}
		for rank := 241; rank <= 250; rank++ {
			m := fixtureMovie(rank)
			m.Ranking = rank
			movies = append(movies, m)
		}
		writeFixtureResponse(w, map[string]any{"movies": movies, "current_page": 11, "has_next": true})
	})
	page, err := client.List(context.Background(), Query{Mode: "top250", Page: 11, Limit: 24})
	if err != nil || len(page.Items) != 10 || page.Items[0].Rank != 241 || page.Items[9].Rank != 250 || page.HasMore || page.Total != nil {
		t.Fatal("TOP250 真实排名、末页或未知总数处理不正确", err)
	}
	for _, ranks := range [][]int{{0}, {251}, {2, 1}, {1, 1}} {
		t.Run(fmt.Sprint(ranks), func(t *testing.T) {
			bad := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				movies := []apiMovie{}
				for i, rank := range ranks {
					m := fixtureMovie(i)
					m.Ranking = rank
					movies = append(movies, m)
				}
				writeFixtureResponse(w, map[string]any{"movies": movies})
			})
			if _, err := bad.List(context.Background(), Query{Mode: "top250"}); err == nil {
				t.Fatal("接受非法、重复或逆序 TOP 排名")
			}
		})
	}
}

func TestR18APITagCatalogOnlyPublicBoundedFields(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/javdb/tags" || req.URL.Query().Get("type") != "all" || req.URL.Query().Get("include_main") != "true" {
			t.Error("标签目录未读取含主分类的固定接口")
		}
		writeFixtureResponse(w, map[string]any{"categories": []any{
			map[string]any{"id": "main", "name": " 主分类 ", "type": "0", "private_token": "fixture-secret", "tags": []any{
				map[string]any{"id": "23", "name": " 标签 ", "type": "0"}, map[string]any{"id": "23", "name": "标签", "type": "0"},
			}},
		}})
	})
	catalog, err := client.Tags(context.Background(), "all")
	if err != nil || len(catalog.Categories) != 1 || catalog.Categories[0].Name != "主分类" || !reflect.DeepEqual(catalog.Categories[0].Tags, []Tag{{ID: "23", Name: "标签", Type: "0"}}) {
		t.Fatal("标签公开字段、清理或去重不正确", err)
	}
	encoded, _ := json.Marshal(catalog)
	if strings.Contains(string(encoded), "fixture-secret") || strings.Contains(string(encoded), "private_token") {
		t.Fatal("标签目录公开了非必要敏感字段")
	}
	if _, err := client.Tags(context.Background(), "5"); err == nil {
		t.Fatal("标签目录接受非法内容类型")
	}
}

func TestR18APIBrowseNormalizationIsStableAndIdempotent(t *testing.T) {
	query, err := NormalizeQuery(Query{Mode: " categories ", MainFilter: "v,p,m,p", TagIDs: "1:7,0:23,1:7", Year: " 2024 ", Month: " 12 ", Duration: "gt-120"})
	if err != nil || query.Mode != "categories" || query.MainFilter != "m,p,v" || query.TagIDs != "0:23,1:7" || query.Type != "all" || query.Filter != "magnets" || query.Sort != "release" || query.Order != "desc" || query.Page != 1 || query.Limit != 24 {
		t.Fatal("查询没有得到稳定的默认值与组合键", err)
	}
	again, err := NormalizeQuery(query)
	if err != nil || query != again {
		t.Fatal("已规范查询再次处理发生变化", err)
	}
	for _, mode := range []string{"ranking", "playback", "popular", "top250"} {
		q, err := NormalizeQuery(Query{Mode: mode})
		if err != nil || q.Filter != "all" || q.Sort != "" || q.Order != "desc" || (mode == "ranking" && q.Type != "0") || (mode != "ranking" && q.Type != "all") {
			t.Fatal("榜单模式默认值不正确", mode, err)
		}
	}
}

func TestR18APITagCatalogMergesDuplicateGroupsWithoutLosingTags(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		writeFixtureResponse(w, TagCatalog{Categories: []TagCategory{
			{ID: "subject", Name: "题材", Type: "0", Tags: []Tag{{ID: "1", Name: "标签一", Type: "0"}}},
			{ID: "subject", Name: "题材", Type: "0", Tags: []Tag{{ID: "1", Name: "标签一", Type: "0"}, {ID: "2", Name: "标签二", Type: "0"}}},
		}})
	})
	result, err := client.Tags(context.Background(), "0")
	if err != nil || len(result.Categories) != 1 || len(result.Categories[0].Tags) != 2 || result.Categories[0].Tags[1].ID != "2" {
		t.Fatal("重复分组去重丢失了后续有效标签", err)
	}
}

func TestR18APITagCatalogRejectsMalformedAndOversizedStructures(t *testing.T) {
	for _, kind := range []string{"null categories", "category count", "tag count", "unsafe group id", "missing tags", "empty name", "invalid type", "mixed type", "wrong requested type", "unsafe tag id"} {
		t.Run(kind, func(t *testing.T) {
			data := TagCatalog{Categories: []TagCategory{{ID: "subject", Name: "题材", Type: "0", Tags: []Tag{{ID: "1", Name: "标签", Type: "0"}}}}}
			requestedType := "all"
			switch kind {
			case "null categories":
				data.Categories = nil
			case "category count":
				for len(data.Categories) <= MaxTagCategories {
					data.Categories = append(data.Categories, data.Categories[0])
				}
			case "tag count":
				for len(data.Categories[0].Tags) <= MaxCatalogTags {
					data.Categories[0].Tags = append(data.Categories[0].Tags, Tag{ID: fmt.Sprint(len(data.Categories[0].Tags)), Name: "标签", Type: "0"})
				}
			case "unsafe group id":
				data.Categories[0].ID = "../private-token"
			case "missing tags":
				data.Categories[0].Tags = nil
			case "empty name":
				data.Categories[0].Name = "\n\t "
			case "invalid type":
				data.Categories[0].Type = "5"
			case "mixed type":
				data.Categories[0].Tags[0].Type = "1"
			case "wrong requested type":
				requestedType = "1"
			case "unsafe tag id":
				data.Categories[0].Tags[0].ID = "1&api_key=fixture-secret"
			}
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) { writeFixtureResponse(w, data) })
			if _, err := client.Tags(context.Background(), requestedType); err == nil || strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "private-token") {
				t.Fatal("非法结构未拒绝或错误回显了上游字段", err)
			}
		})
	}
}
