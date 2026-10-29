package r18

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func TestR18APIExplicitPagesAndFilterSortForwarding(t *testing.T) {
	requested := []int{}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		page, _ := strconv.Atoi(q.Get("page"))
		requested = append(requested, page)
		if req.URL.Path != "/api/v1/javdb/movies/tags" || q.Get("sort_by") != "score" || q.Get("order_by") != "asc" || q.Get("type") != "2" || q.Get("movie_filter_by") != "subtitle" || q.Get("year") != "2024" || q.Get("limit") != "2" {
			t.Error("页码、分类、筛选、年份或评分排序没有传到上游")
		}
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{fixtureMovie(page * 2), fixtureMovie(page*2 + 1)}, "current_page": page, "has_next": true})
	})
	query := Query{Page: 1, Limit: 2, Sort: "score", Order: "asc", Type: "2", Filter: "subtitle", Year: "2024"}
	first, err := client.List(context.Background(), query)
	if err != nil || first.Page != 1 || first.PageSize != 2 || first.HasPrevious || !first.HasMore {
		t.Fatal("首页页码、页大小或翻页状态不正确", err)
	}
	query.Page = 2
	second, err := client.List(context.Background(), query)
	if err != nil || second.Page != 2 || !second.HasPrevious || second.Items[0].ID == first.Items[0].ID {
		t.Fatal("下一页不正确", err)
	}
	query.Page = 1
	previous, err := client.List(context.Background(), query)
	if err != nil || previous.HasPrevious || previous.Items[0].ID != first.Items[0].ID {
		t.Fatal("上一页没有保持筛选和页码", err)
	}
	query.Page = 37
	jumped, err := client.List(context.Background(), query)
	if err != nil || jumped.Page != 37 || !jumped.HasPrevious {
		t.Fatal("任意页跳转没有应用页码", err)
	}
	query.Page = 9999
	last, err := client.List(context.Background(), query)
	if err != nil || last.Page != 9999 || last.HasMore || !last.HasPrevious {
		t.Fatal("最大页码仍允许越界下一页", err)
	}
	want := []int{1, 2, 1, 37, 9999}
	if len(requested) != len(want) {
		t.Fatal("显式页码访问次数不正确", requested)
	}
	for i, page := range requested {
		if page != want[i] {
			t.Fatal("页码跳转请求不正确", requested)
		}
	}
}

func TestR18APIQueryDefaultsAndPageResponse(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if q.Get("page") != "1" || q.Get("limit") != "24" || q.Get("type") != "all" || q.Get("movie_filter_by") != "magnets" || q.Get("sort_by") != "release" || q.Get("order_by") != "desc" || q.Has("year") {
			t.Error("默认列表没有采用第一页、24条、磁力筛选和发行倒序")
		}
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{fixtureMovie(1)}, "current_page": 1, "has_next": false})
	})
	if ValidateQuery(Query{}) != nil {
		t.Fatal("省略参数没有应用默认值")
	}
	page, err := client.List(context.Background(), Query{})
	if err != nil || page.Page != 1 || page.PageSize != 24 || page.HasMore || page.HasPrevious {
		t.Fatal("默认分页结果不正确", err)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	_ = json.Unmarshal(encoded, &response)
	if response["page"] != float64(1) || response["page_size"] != float64(24) {
		t.Fatal("分页响应没有提供明确页码和页大小")
	}
	for _, removed := range []string{"estimated_total", "total", "total_count", "next_cursor", "previous_cursor"} {
		if _, exists := response[removed]; exists {
			t.Fatal("在线分页仍提供不存在的统计或旧游标", removed)
		}
	}
}

func TestR18APIBrowseTypesFiltersAndSorts(t *testing.T) {
	current := Query{}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if q.Get("type") != current.Type || q.Get("movie_filter_by") != current.Filter || q.Get("sort_by") != current.Sort || q.Get("order_by") != current.Order {
			t.Error("筛选排序参数没有按请求传给上游")
		}
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{}, "current_page": 1, "has_next": false})
	})
	for _, movieType := range []string{"all", "0", "1", "2", "3"} {
		for _, filter := range []string{"all", "magnets", "subtitle", "can_play"} {
			current = Query{Type: movieType, Filter: filter, Sort: "release", Order: "desc", Limit: 2}
			if ValidateQuery(current) != nil {
				t.Fatal("拒绝合法分类或资源筛选", movieType, filter)
			}
			if _, err := client.List(context.Background(), current); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, sort := range []string{"release", "score", "hit", "want_watch_count", "watched_count"} {
		current = Query{Type: "all", Filter: "magnets", Sort: sort, Order: "asc", Limit: 2}
		if ValidateQuery(current) != nil {
			t.Fatal("拒绝合法列表排序", sort)
		}
		if _, err := client.List(context.Background(), current); err != nil {
			t.Fatal(err)
		}
	}
}

func TestR18APISearchUsesUpstreamRelevance(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if req.URL.Path != "/api/v1/javdb/search" || q.Get("q") != "Fixture & name" || q.Get("type") != "movie" || q.Get("movie_type") != "3" || q.Get("movie_filter_by") != "can_play" || q.Get("movie_sort_by") != "relevance" || q.Get("page") != "7" || q.Get("limit") != "2" || q.Has("order_by") || q.Has("year") {
			t.Error("关键词搜索、分类筛选、相关度排序或显式页码参数不正确")
		}
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{fixtureMovie(1)}, "current_page": 7})
	})
	page, err := client.List(context.Background(), Query{Search: "  Fixture & name  ", Type: "3", Filter: "can_play", Page: 7, Limit: 2, Order: "asc"})
	if err != nil || len(page.Items) != 1 || page.Page != 7 || page.PageSize != 2 || !page.HasPrevious || page.HasMore {
		t.Fatal("搜索显式分页不正确", err)
	}
}
