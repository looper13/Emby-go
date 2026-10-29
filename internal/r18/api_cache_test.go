package r18

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestR18BrowseAPIConnectionsDoNotSharePrivateCache(t *testing.T) {
	var callsA, callsB atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		index := 0
		switch req.Header.Get("X-API-Key") {
		case "fixture-key-a":
			callsA.Add(1)
			index = 1
		case "fixture-key-b":
			callsB.Add(1)
			index = 2
		default:
			t.Error("连接使用了其它连接的凭据")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{fixtureMovie(index)}, "current_page": 1, "has_next": false})
	}))
	t.Cleanup(upstream.Close)
	open := func(key string) *Client {
		t.Helper()
		client, err := Open(Config{BaseURL: upstream.URL, APIKey: key})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
	a, b := open("fixture-key-a"), open("fixture-key-b")
	for range 2 {
		first, err := a.List(context.Background(), Query{})
		if err != nil || len(first.Items) != 1 || first.Items[0].Number != fixtureMovie(1).Number {
			t.Fatal("第一个连接缓存混入了其它凭据的结果", err)
		}
		second, err := b.List(context.Background(), Query{})
		if err != nil || len(second.Items) != 1 || second.Items[0].Number != fixtureMovie(2).Number {
			t.Fatal("第二个连接复用了其它连接缓存", err)
		}
	}
	if callsA.Load() != 1 || callsB.Load() != 1 {
		t.Fatal("连接内部没有缓存或连接间错误共享了请求", callsA.Load(), callsB.Load())
	}
	_ = a.Close()
	if _, err := a.List(context.Background(), Query{}); err == nil {
		t.Fatal("已关闭连接仍可读取旧缓存")
	}
	if _, err := b.List(context.Background(), Query{}); err != nil || callsB.Load() != 1 {
		t.Fatal("关闭其它连接使当前连接缓存失效", err)
	}
}

func TestR18BrowseAPICategoryKeysIncludeAllNormalizedConditions(t *testing.T) {
	var calls atomic.Int32
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		index := int(calls.Add(1))
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{fixtureMovie(index)}, "current_page": 1, "has_next": false})
	})
	base := Query{Mode: "categories", TagIDs: "1:7,0:23", MainFilter: "p,m"}
	first, err := client.List(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	equivalent := base
	equivalent.TagIDs, equivalent.MainFilter = " 0:23,1:7,0:23 ", "m,p,m"
	second, err := client.List(context.Background(), equivalent)
	if err != nil || first.Items[0].ID != second.Items[0].ID || calls.Load() != 1 {
		t.Fatal("规范等价的类别组合没有合并缓存键", err)
	}
	for _, change := range []func(*Query){
		func(q *Query) { q.TagIDs = "0:24" }, func(q *Query) { q.MainFilter = "c" },
		func(q *Query) { q.Year = "2024" }, func(q *Query) { q.Duration = "gt-120" },
		func(q *Query) { q.Month = "12" }, func(q *Query) { q.Type = "4" },
		func(q *Query) { q.Filter = "subtitle" }, func(q *Query) { q.Sort = "score" },
		func(q *Query) { q.Order = "asc" }, func(q *Query) { q.Limit = 2 },
	} {
		query := base
		change(&query)
		page, err := client.List(context.Background(), query)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID == first.Items[0].ID {
			t.Fatal("不同类别条件错误复用了旧缓存", query, err)
		}
	}
	if calls.Load() != 11 {
		t.Fatal("类别缓存漏掉了有效查询条件", calls.Load())
	}
	for _, query := range []Query{{Sort: "score"}, {Mode: "categories", Sort: "score"}, {Mode: "latest"}, {Mode: "categories"}} {
		if _, err := client.List(context.Background(), query); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 15 {
		t.Fatal("模式或固定上游路径不在缓存键内", calls.Load())
	}
}

func TestR18BrowseAPIRankingsShareWholeSnapshotAcrossPageSizes(t *testing.T) {
	var calls atomic.Int32
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		if req.URL.Query().Has("page") || req.URL.Query().Has("limit") {
			t.Error("榜单缓存回源错误发送了本地分页")
		}
		movies := []apiMovie{}
		for i := 0; i < 60; i++ {
			movies = append(movies, fixtureMovie(60-i))
		}
		writeFixtureResponse(w, map[string]any{"movies": movies})
	})
	first, err := client.List(context.Background(), Query{Mode: "ranking", Period: "weekly", Limit: 24})
	if err != nil || len(first.Items) != 24 || first.Total == nil || *first.Total != 60 {
		t.Fatal("完整榜单首页失败", err)
	}
	first.Items[0].Title, first.Items[0].Images = "调用者修改", []string{"https://fixture.invalid/private"}
	*first.Total = 1
	second, err := client.List(context.Background(), Query{Mode: "ranking", Period: "weekly", Page: 2, Limit: 12})
	if err != nil || len(second.Items) != 12 || second.Items[0].Rank != 13 || second.Items[0].Number != fixtureMovie(48).Number || second.Total == nil || *second.Total != 60 || calls.Load() != 1 {
		t.Fatal("跨页或跨每页条数没有共享同一完整榜单", err)
	}
	again, err := client.List(context.Background(), Query{Mode: "ranking", Period: "weekly", Limit: 24})
	if err != nil || again.Items[0].Title == "调用者修改" || len(again.Items[0].Images) != 0 || *again.Total != 60 {
		t.Fatal("榜单返回值修改污染了后续页或真实总数", err)
	}
	for _, query := range []Query{
		{Mode: "ranking", Period: "daily"}, {Mode: "ranking", Period: "weekly", Type: "1"},
		{Mode: "playback", Period: "weekly"}, {Mode: "popular", Period: "weekly"},
	} {
		if _, err := client.List(context.Background(), query); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 5 {
		t.Fatal("榜单模式、类型或周期错误共享快照", calls.Load())
	}
	if _, err := client.List(WithFresh(context.Background()), Query{Mode: "ranking", Period: "weekly", Page: 3, Limit: 24}); err != nil || calls.Load() != 6 {
		t.Fatal("强制刷新榜单没有重新获取完整快照", err)
	}
}

func TestR18BrowseAPIFailuresAndMalformedIdentityAreNotCached(t *testing.T) {
	for _, kind := range []string{"HTTP", "identity", "rank", "tag"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				first := calls.Add(1) == 1
				if kind == "HTTP" && first {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if kind == "tag" {
					name := "题材"
					if first {
						name = " "
					}
					writeFixtureResponse(w, TagCatalog{Categories: []TagCategory{{ID: "subject", Name: name, Type: "0", Tags: []Tag{}}}})
					return
				}
				m := fixtureMovie(1)
				if kind == "identity" && first {
					m.ID = "../bad"
				}
				if kind == "rank" && first {
					m.Number = ""
				}
				writeFixtureResponse(w, map[string]any{"movies": []apiMovie{m}, "current_page": 1, "has_next": false})
			})
			read := func() error {
				if kind == "tag" {
					_, err := client.Tags(context.Background(), "0")
					return err
				}
				query := Query{}
				if kind == "rank" {
					query.Mode = "ranking"
				}
				_, err := client.List(context.Background(), query)
				return err
			}
			if read() == nil {
				t.Fatal("非法上游结果未报错")
			}
			if err := read(); err != nil {
				t.Fatal("失败被缓存导致下一次不能重试", err)
			}
			if err := read(); err != nil || calls.Load() != 2 {
				t.Fatal("恢复后的成功结果未缓存", err, calls.Load())
			}
		})
	}
}

func TestR18BrowseAPIReadersJoinActiveFreshRequest(t *testing.T) {
	var calls atomic.Int32
	started, finish := make(chan struct{}), make(chan struct{})
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		index := int(calls.Add(1))
		if index == 2 {
			close(started)
			select {
			case <-finish:
			case <-req.Context().Done():
				return
			}
		}
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{fixtureMovie(index)}, "current_page": 1, "has_next": false})
	})
	if _, err := client.List(context.Background(), Query{}); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	read := func(ctx context.Context) {
		defer wg.Done()
		page, err := client.List(ctx, Query{})
		if err == nil && (len(page.Items) != 1 || page.Items[0].Number != fixtureMovie(2).Number) {
			err = errors.New("新读者未复用刷新中的新结果")
		}
		results <- err
	}
	wg.Add(1)
	go read(WithFresh(context.Background()))
	<-started
	wg.Add(1)
	go read(context.Background())
	waitBrowseCache(t, func() bool {
		client.browse.mu.Lock()
		defer client.browse.mu.Unlock()
		for key, pending := range client.browse.pending {
			if key.fresh && pending.waiters == 2 {
				return true
			}
		}
		return false
	})
	close(finish)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("活跃刷新与普通读者重复访问上游", calls.Load())
	}
}
