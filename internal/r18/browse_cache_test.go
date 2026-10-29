package r18

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitBrowseCache(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("浏览缓存并发条件未按时满足")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestR18BrowseCacheHitExpiryFreshAndCallerIsolation(t *testing.T) {
	cache := newBrowseCache()
	t.Cleanup(cache.close)
	now := time.Unix(1_700_000_000, 0)
	cache.now = func() time.Time { return now }
	calls := 0
	load := func(context.Context) ([]byte, error) {
		calls++
		return []byte(fmt.Sprintf("page-%d", calls)), nil
	}
	read := func(ctx context.Context, key string) []byte {
		t.Helper()
		data, err := cache.read(ctx, key, browseCacheTTL, load)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := read(context.Background(), "mode=latest&page=1&limit=24")
	first[0] = 'X'
	if got := string(read(context.Background(), "mode=latest&page=1&limit=24")); got != "page-1" || calls != 1 {
		t.Fatal("缓存命中重复访问上游或调用者修改污染了缓存", got, calls)
	}
	if got := string(read(WithFresh(context.Background()), "mode=latest&page=1&limit=24")); got != "page-2" || calls != 2 {
		t.Fatal("显式刷新没有绕过缓存", got, calls)
	}
	if got := string(read(context.Background(), "mode=latest&page=1&limit=24")); got != "page-2" {
		t.Fatal("刷新结果没有替换旧页", got)
	}
	read(context.Background(), "mode=top250&page=1&limit=24")
	if calls != 3 {
		t.Fatal("不同浏览条件错误共用缓存")
	}
	now = now.Add(browseCacheTTL)
	if got := string(read(context.Background(), "mode=latest&page=1&limit=24")); got != "page-4" {
		t.Fatal("过期页面没有重新读取", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.read(ctx, "mode=latest&page=1&limit=24", browseCacheTTL, load); !errors.Is(err, context.Canceled) || calls != 4 {
		t.Fatal("取消请求仍返回缓存或触发上游", err, calls)
	}
}

func TestR18BrowseCacheCoalescesButKeepsWaiterCancellationIndependent(t *testing.T) {
	cache := newBrowseCache()
	t.Cleanup(cache.close)
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	load := func(ctx context.Context) ([]byte, error) {
		calls.Add(1)
		close(started)
		select {
		case <-finish:
			return []byte("shared"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := cache.read(ctx, "same-query", browseCacheTTL, load); first <- err }()
	<-started
	second := make(chan error, 1)
	go func() {
		data, err := cache.read(context.Background(), "same-query", browseCacheTTL, load)
		if err == nil && string(data) != "shared" {
			err = errors.New("共享结果不正确")
		}
		second <- err
	}()
	waitBrowseCache(t, func() bool {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		pending := cache.pending[browsePendingKey{query: "same-query"}]
		return pending != nil && pending.waiters == 2
	})
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal("取消者未立即结束", err)
	}
	close(finish)
	if err := <-second; err != nil || calls.Load() != 1 {
		t.Fatal("首位取消者影响了其它等待者，或同条件重复请求", err, calls.Load())
	}
}

func TestR18BrowseCacheCancelsWhenAllWaitersLeaveAndCanRestart(t *testing.T) {
	cache := newBrowseCache()
	t.Cleanup(cache.close)
	started, stopped := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := cache.read(ctx, "cancel-all", browseCacheTTL, func(shared context.Context) ([]byte, error) {
			close(started)
			<-shared.Done()
			close(stopped)
			return nil, shared.Err()
		})
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("所有等待者离开后仍持有上游请求")
	}
	data, err := cache.read(context.Background(), "cancel-all", browseCacheTTL, func(context.Context) ([]byte, error) { return []byte("new"), nil })
	if err != nil || string(data) != "new" {
		t.Fatal("新请求错误复用了已取消的共享操作", err)
	}
}

func TestR18BrowseCacheRefreshCannotBeOverwrittenByAnOlderPendingRead(t *testing.T) {
	cache := newBrowseCache()
	t.Cleanup(cache.close)
	started, oldFinish := make(chan struct{}), make(chan struct{})
	oldResult := make(chan error, 1)
	go func() {
		_, err := cache.read(context.Background(), "refresh-race", browseCacheTTL, func(context.Context) ([]byte, error) {
			close(started)
			<-oldFinish
			return []byte("old"), nil
		})
		oldResult <- err
	}()
	<-started
	data, err := cache.read(WithFresh(context.Background()), "refresh-race", browseCacheTTL, func(context.Context) ([]byte, error) { return []byte("fresh"), nil })
	if err != nil || string(data) != "fresh" {
		t.Fatal(err)
	}
	close(oldFinish)
	if err := <-oldResult; err != nil {
		t.Fatal(err)
	}
	data, err = cache.read(context.Background(), "refresh-race", browseCacheTTL, func(context.Context) ([]byte, error) { return nil, errors.New("不应重新访问上游") })
	if err != nil || string(data) != "fresh" {
		t.Fatal("旧请求晚到后覆盖了刷新结果", err, string(data))
	}
}

func TestR18BrowseCacheFailuresAndOversizedEntriesAreNotCached(t *testing.T) {
	cache := newBrowseCache()
	t.Cleanup(cache.close)
	calls := 0
	load := func(context.Context) ([]byte, error) {
		calls++
		return nil, errors.New("上游暂不可用")
	}
	for range 2 {
		if _, err := cache.read(context.Background(), "failed", browseCacheTTL, load); err == nil {
			t.Fatal("错误被吞掉")
		}
	}
	if calls != 2 {
		t.Fatal("失败结果被缓存")
	}
	calls = 0
	large := bytes.Repeat([]byte("x"), browseCacheEntrySize+1)
	for range 2 {
		if _, err := cache.read(context.Background(), "large", browseCacheTTL, func(context.Context) ([]byte, error) {
			calls++
			return large, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("超过单项预算仍被缓存")
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) != 0 || cache.bytes != 0 {
		t.Fatal("失败或超限数据占用了缓存预算")
	}
}

func TestR18BrowseCacheHasBoundedEntryAndByteBudgets(t *testing.T) {
	cache := newBrowseCache()
	t.Cleanup(cache.close)
	now := time.Unix(1_700_000_000, 0)
	cache.now = func() time.Time { return now }
	for index := range browseCacheEntries + 1 {
		now = now.Add(time.Millisecond)
		if _, err := cache.read(context.Background(), fmt.Sprintf("page-%d", index), browseCacheTTL, func(context.Context) ([]byte, error) { return []byte("small"), nil }); err != nil {
			t.Fatal(err)
		}
	}
	cache.mu.Lock()
	if len(cache.entries) != browseCacheEntries {
		t.Fatal("缓存条数没有限制")
	}
	if _, found := cache.entries["page-0"]; found {
		t.Fatal("没有淘汰最久未使用的页面")
	}
	cache.mu.Unlock()
	for index := range browseCacheBytes/browseCacheEntrySize + 2 {
		now = now.Add(time.Millisecond)
		if _, err := cache.read(context.Background(), fmt.Sprintf("large-%d", index), browseCacheTTL, func(context.Context) ([]byte, error) { return bytes.Repeat([]byte("z"), browseCacheEntrySize), nil }); err != nil {
			t.Fatal(err)
		}
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.bytes > browseCacheBytes || len(cache.entries) > browseCacheEntries {
		t.Fatal("缓存数据预算无界", cache.bytes, len(cache.entries))
	}
}

func TestR18BrowseCacheBoundsParallelReadsAndClosesPendingRequests(t *testing.T) {
	cache := newBrowseCache()
	var active, peak, entered atomic.Int32
	var wg sync.WaitGroup
	result := make(chan error, browseRequestLimit+3)
	for index := range browseRequestLimit + 3 {
		wg.Go(func() {
			_, err := cache.read(context.Background(), fmt.Sprintf("read-%d", index), browseCacheTTL, func(ctx context.Context) ([]byte, error) {
				now := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); now > old && !peak.CompareAndSwap(old, now); old = peak.Load() {
				}
				entered.Add(1)
				<-ctx.Done()
				return nil, ctx.Err()
			})
			result <- err
		})
	}
	waitBrowseCache(t, func() bool { return entered.Load() >= browseRequestLimit })
	cache.close()
	wg.Wait()
	close(result)
	for err := range result {
		if err == nil {
			t.Fatal("关闭客户端后仍成功返回进行中的查询")
		}
	}
	if peak.Load() > browseRequestLimit || active.Load() != 0 {
		t.Fatal("上游并发无界或客户端关闭后仍占用资源", peak.Load(), active.Load())
	}
	called := false
	if _, err := cache.read(context.Background(), "closed", browseCacheTTL, func(context.Context) ([]byte, error) { called = true; return nil, nil }); err == nil || called {
		t.Fatal("已关闭客户端重新请求了上游")
	}
}

func TestR18BrowseCacheBoundsSameQueryWaiters(t *testing.T) {
	cache := newBrowseCache()
	t.Cleanup(cache.close)
	started := make(chan struct{})
	var wg sync.WaitGroup
	for range browseWaiterLimit {
		wg.Go(func() {
			_, _ = cache.read(context.Background(), "waiters", browseCacheTTL, func(ctx context.Context) ([]byte, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			})
		})
	}
	<-started
	waitBrowseCache(t, func() bool {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		pending := cache.pending[browsePendingKey{query: "waiters"}]
		return pending != nil && pending.waiters == browseWaiterLimit
	})
	called := false
	if _, err := cache.read(context.Background(), "waiters", browseCacheTTL, func(context.Context) ([]byte, error) { called = true; return nil, nil }); err == nil || called {
		t.Fatal("同查询等待者超限仍接纳或重复读取上游")
	}
	cache.close()
	wg.Wait()
}
