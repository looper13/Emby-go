package r18

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestR18CommunityMatchesCompleteIndexedNumberAndSearchPages(t *testing.T) {
	var calls atomic.Int64
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		q := req.URL.Query()
		if req.URL.Path != "/api/v1/javdb/search" || q.Get("q") != "test_001" || q.Get("type") != "movie" || q.Get("limit") != "50" {
			t.Errorf("番号匹配没有使用专用搜索条件: %s", req.URL)
		}
		movies := []map[string]any{{"id": "near", "number": "TEST-0010"}, {"id": "version", "number": "TEST-001-C"}}
		more := true
		if q.Get("page") == "2" {
			movies, more = []map[string]any{{"id": "matched", "number": "TEST-001"}}, false
		}
		writeFixtureResponse(w, map[string]any{"movies": movies, "current_page": mustCommunityPage(q.Get("page")), "has_next": more})
	})
	for range 2 {
		video, err := client.MatchNumber(context.Background(), "test_001")
		if err != nil || video.ContentID != "matched" || video.Number != "TEST-001" {
			t.Fatal("没有跨页选择精确番号", video, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("重复匹配没有复用已有搜索缓存", calls.Load())
	}
}

func mustCommunityPage(raw string) int { value, _ := strconv.Atoi(raw); return value }

func TestR18CommunityDoesNotGuessMissingAmbiguousOrUnboundedMatches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rows  []map[string]any
		more  bool
		want  error
		calls int64
	}{
		{"no-match", []map[string]any{{"id": "other", "number": "TEST-0001"}}, false, sql.ErrNoRows, 1},
		{"ambiguous", []map[string]any{{"id": "first", "number": "TEST-001"}, {"id": "second", "number": "test_001"}}, false, ErrAmbiguousNumber, 1},
		{"bounded", []map[string]any{{"id": "other", "number": "TEST-0010"}}, true, ErrNumberSearchLimit, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				writeFixtureResponse(w, map[string]any{"movies": tc.rows, "has_next": tc.more})
			})
			if _, err := client.MatchNumber(context.Background(), "TEST-001"); !errors.Is(err, tc.want) {
				t.Fatal("没有拒绝错误匹配", err)
			}
			if calls.Load() != tc.calls {
				t.Fatal("搜索请求没有遵守边界", calls.Load())
			}
			for _, number := range []string{"", "../TEST-001", "TEST\n001", strings.Repeat("T", 121)} {
				if _, err := client.MatchNumber(context.Background(), number); !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("无效番号被查询", err)
				}
			}
			if calls.Load() != tc.calls {
				t.Fatal("无效番号访问上游")
			}
		})
	}
}

func TestR18CommunityReviewsParsePublicFieldsAndFreshCache(t *testing.T) {
	var calls atomic.Int64
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		call := calls.Add(1)
		if req.URL.Path != "/api/v1/javdb/movies/film/reviews" || req.URL.Query().Get("page") != "2" || req.URL.Query().Get("limit") != "15" {
			t.Error("评论请求路径或分页不正确", req.URL)
		}
		writeFixtureResponse(w, map[string]any{
			"reviews": []map[string]any{{"id": 123, "user": map[string]any{"username": "评论作者", "token": "private-upstream-token"},
				"content": "第一行\n<script>仅作为文字展示</script>", "score": "4.5", "status_title": "看过", "created_at": "2026-11-18", "internal": call},
				{"id": "unrated", "content": "未评分的评论", "score": "  "}},
			"pagination": map[string]any{"total_count": "31"}, "current_page": 2, "has_next": true,
			"api_key": "private-upstream-key",
		})
	})
	for range 2 {
		page, err := client.Reviews(context.Background(), EncodeID("film", "javdb"), 2, 15)
		if err != nil || len(page.Items) != 2 || page.Total == nil || *page.Total != 31 || !page.HasMore || page.Items[1].Score != "" {
			t.Fatal("评论数据或分页未保留", page, err)
		}
		review := page.Items[0]
		if review.ID != "123" || review.Username != "评论作者" || review.Score != "4.5" || !strings.Contains(review.Content, "\n<script>") || review.Status != "看过" {
			t.Fatal("上游公开字段未映射", review)
		}
		raw, _ := json.Marshal(page)
		if strings.Contains(string(raw), "private-upstream") || strings.Contains(string(raw), `"internal"`) {
			t.Fatal("转发了上游私人字段")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("评论没有使用短期缓存", calls.Load())
	}
	if _, err := client.Reviews(WithFresh(context.Background()), EncodeID("film", "javdb"), 2, 15); err != nil || calls.Load() != 2 {
		t.Fatal("刷新没有绕过评论缓存", err, calls.Load())
	}
}

func TestR18CommunityRelatedListsKeepUnknownCountsAndValidatedIDs(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/javdb/lists/related" || req.URL.Query().Get("movie_id") != "film" {
			t.Error("清单没有按影片 ID 查询")
		}
		writeFixtureResponse(w, map[string]any{"lists": []map[string]any{{"id": "listA", "name": "相关清单"}, {"id": 456, "name": "另一清单", "movies_count": "32", "url": "javascript:alert(1)"}}})
	})
	page, err := client.RelatedLists(context.Background(), EncodeID("film", "javdb"), 1, 2)
	if err != nil || len(page.Items) != 2 || !page.HasMore || page.Total != nil {
		t.Fatal("缺省分页被伪造成全部结果", page, err)
	}
	if page.Items[0].MovieCount != nil || page.Items[1].MovieCount == nil || *page.Items[1].MovieCount != 32 || page.Items[1].ID != "456" {
		t.Fatal("未知数量或清单标识未按约定处理", page.Items)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), `"url"`) {
		t.Fatal("本库匹配清单仍返回外部跳转链接")
	}
}

func TestR18CommunityRejectsMissingMalformedAndFailedUpstreamData(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  any
		lists bool
	}{
		{"missing-reviews", map[string]any{}, false},
		{"wrong-page", map[string]any{"reviews": []any{}, "current_page": 2}, false},
		{"bad-score", map[string]any{"reviews": []any{map[string]any{"score": "NaN"}}}, false},
		{"bad-total", map[string]any{"reviews": []any{}, "total": -1}, false},
		{"missing-lists", map[string]any{}, true},
		{"bad-list-id", map[string]any{"lists": []any{map[string]any{"id": "../elsewhere", "name": "清单"}}}, true},
		{"bad-list-count", map[string]any{"lists": []any{map[string]any{"id": "valid", "name": "清单", "movies_count": -1}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) { calls.Add(1); writeFixtureResponse(w, tc.data) })
			for range 2 {
				var err error
				if tc.lists {
					_, err = client.RelatedLists(context.Background(), EncodeID("film", "javdb"), 1, 15)
				} else {
					_, err = client.Reviews(context.Background(), EncodeID("film", "javdb"), 1, 15)
				}
				if err == nil {
					t.Fatal("异常上游数据被当成空结果")
				}
			}
			if calls.Load() != 2 {
				t.Fatal("失败被缓存，无法单独重试")
			}
		})
	}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("private-upstream-key"))
	})
	_, err := client.Reviews(context.Background(), EncodeID("film", "javdb"), 1, 15)
	if err == nil || strings.Contains(err.Error(), "private-upstream-key") || !strings.Contains(err.Error(), "401") {
		t.Fatal("上游登录失败没有安全返回", err)
	}
}

func TestR18CommunityInvalidPaginationDoesNotContactUpstream(t *testing.T) {
	var calls atomic.Int64
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) { calls.Add(1) })
	for _, values := range [][2]int{{0, 15}, {10000, 15}, {1, 0}, {1, 51}} {
		if _, err := client.Reviews(context.Background(), EncodeID("film", "javdb"), values[0], values[1]); err == nil {
			t.Fatal("接受无效评论分页")
		}
		if _, err := client.RelatedLists(context.Background(), EncodeID("film", "javdb"), values[0], values[1]); err == nil {
			t.Fatal("接受无效清单分页")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("无效分页访问上游")
	}
}

func TestR18CommunityListMoviesUsesListFilterAndValidatedCache(t *testing.T) {
	var calls atomic.Int64
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		q := req.URL.Query()
		if req.URL.Path != "/api/v1/javdb/movies/tags" || q.Get("list_id") != "list1" || q.Get("main_filter") != "all" || q.Get("movie_filter_by") != "all" || q.Get("type") != "all" || q.Get("page") != "2" || q.Get("limit") != "50" {
			t.Error("没有使用上游清单专用分页筛选", req.URL)
		}
		writeFixtureResponse(w, map[string]any{"movies": []any{map[string]any{"id": "film1", "number": "TEST-002", "title": "上游清单影片"}}, "current_page": 2, "has_next": true})
	})
	for range 2 {
		page, err := client.ListMovies(context.Background(), "list1", 2, 50)
		if err != nil || len(page.Items) != 1 || page.Items[0].Number != "TEST-002" || page.Page != 2 || page.PageSize != 50 || !page.HasMore {
			t.Fatal("清单影片解析失败", page, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("上游清单页未复用缓存", calls.Load())
	}
	if _, err := client.ListMovies(WithFresh(context.Background()), "list1", 2, 50); err != nil || calls.Load() != 2 {
		t.Fatal("清单影片刷新未绕过缓存", err)
	}
	for _, values := range [][2]int{{0, 50}, {10000, 50}, {1, 0}, {1, 51}} {
		if _, err := client.ListMovies(context.Background(), "list1", values[0], values[1]); err == nil {
			t.Fatal("接受了无效清单分页")
		}
	}
	if _, err := client.ListMovies(context.Background(), "../list1", 2, 50); err == nil {
		t.Fatal("接受了无效清单 ID")
	}
	if calls.Load() != 2 {
		t.Fatal("非法清单请求联系了上游")
	}
}

func TestR18CommunityListMoviesRejectsInvalidResponsesWithoutCaching(t *testing.T) {
	for _, data := range []map[string]any{
		{}, {"movies": []any{}, "current_page": 2}, {"movies": []any{map[string]any{"id": "film", "number": ""}}},
	} {
		var calls atomic.Int64
		client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) { calls.Add(1); writeFixtureResponse(w, data) })
		for range 2 {
			if _, err := client.ListMovies(context.Background(), "list1", 1, 50); err == nil {
				t.Fatal("非法响应被当成本库未匹配", data)
			}
		}
		if calls.Load() != 2 {
			t.Fatal("非法清单响应被缓存")
		}
	}
}
