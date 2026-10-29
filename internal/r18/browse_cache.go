package r18

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	browseCacheTTL       = 2 * time.Minute
	tagCatalogCacheTTL   = 5 * time.Minute
	browseCacheEntries   = 128
	browseCacheBytes     = 16 << 20
	browseCacheEntrySize = 1 << 20
	browsePendingLimit   = 32
	browseWaiterLimit    = 64
	browseRequestLimit   = 4
)

type freshBrowseContextKey struct{}

// WithFresh 只绕过本服务的浏览缓存，不向 AVdb 发送未约定的刷新参数。
func WithFresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, freshBrowseContextKey{}, true)
}

type browseCacheEntry struct {
	data    []byte
	expires time.Time
	used    time.Time
}

type browsePendingKey struct {
	query string
	fresh bool
}

type browsePending struct {
	key       browsePendingKey
	done      chan struct{}
	cancel    context.CancelFunc
	waiters   int
	cacheable bool
	data      []byte
	err       error
}

type browseCache struct {
	mu      sync.Mutex
	entries map[string]browseCacheEntry
	pending map[browsePendingKey]*browsePending
	slots   chan struct{}
	bytes   int
	closed  bool
	now     func() time.Time
}

func newBrowseCache() *browseCache {
	return &browseCache{
		entries: map[string]browseCacheEntry{},
		pending: map[browsePendingKey]*browsePending{},
		slots:   make(chan struct{}, browseRequestLimit),
		now:     time.Now,
	}
}

func (c *browseCache) removeLocked(key string) {
	if old, found := c.entries[key]; found {
		c.bytes -= len(old.data)
		delete(c.entries, key)
	}
}

func (c *browseCache) pruneLocked(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			c.removeLocked(key)
		}
	}
}

func (c *browseCache) storeLocked(key string, data []byte, ttl time.Duration) {
	if len(data) > browseCacheEntrySize || ttl <= 0 || c.closed {
		return
	}
	now := c.now()
	c.pruneLocked(now)
	c.removeLocked(key)
	for len(c.entries) >= browseCacheEntries || c.bytes+len(data) > browseCacheBytes {
		oldestKey := ""
		var oldest time.Time
		for candidate, entry := range c.entries {
			if oldestKey == "" || entry.used.Before(oldest) {
				oldestKey, oldest = candidate, entry.used
			}
		}
		if oldestKey == "" {
			return
		}
		c.removeLocked(oldestKey)
	}
	c.entries[key] = browseCacheEntry{data: append([]byte(nil), data...), expires: now.Add(ttl), used: now}
	c.bytes += len(data)
}

func (c *browseCache) read(ctx context.Context, key string, ttl time.Duration, load func(context.Context) ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fresh, _ := ctx.Value(freshBrowseContextKey{}).(bool)
	pendingKey := browsePendingKey{query: key, fresh: fresh}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("JavDB 浏览连接已关闭")
	}
	// 刷新中的查询优先供新读者复用，避免旧请求晚到后覆盖新结果。
	pending := c.pending[browsePendingKey{query: key, fresh: true}]
	if pending == nil && !fresh {
		now := c.now()
		if entry, found := c.entries[key]; found {
			if now.Before(entry.expires) {
				entry.used = now
				c.entries[key] = entry
				data := append([]byte(nil), entry.data...)
				c.mu.Unlock()
				return data, nil
			}
			c.removeLocked(key)
		}
		pending = c.pending[pendingKey]
	}
	if pending == nil {
		if len(c.pending) >= browsePendingLimit {
			c.mu.Unlock()
			return nil, errors.New("JavDB 浏览请求过多，请稍后重试")
		}
		if fresh {
			c.removeLocked(key)
			if previous := c.pending[browsePendingKey{query: key}]; previous != nil {
				previous.cacheable = false
			}
		}
		// 每位等待者独立取消；只有全部离开时才取消共享上游请求。
		shared, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		pending = &browsePending{key: pendingKey, done: make(chan struct{}), cancel: cancel, cacheable: true}
		c.pending[pendingKey] = pending
		go c.fetch(shared, pendingKey, pending, ttl, load)
	}
	if pending.waiters >= browseWaiterLimit {
		c.mu.Unlock()
		return nil, errors.New("JavDB 相同查询等待过多，请稍后重试")
	}
	pending.waiters++
	c.mu.Unlock()
	defer c.release(pending)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-pending.done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return append([]byte(nil), pending.data...), pending.err
	}
}

func (c *browseCache) fetch(ctx context.Context, key browsePendingKey, pending *browsePending, ttl time.Duration, load func(context.Context) ([]byte, error)) {
	defer pending.cancel()
	var data []byte
	var err error
	select {
	case c.slots <- struct{}{}:
		data, err = load(ctx)
		<-c.slots
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err == nil {
		err = ctx.Err()
	}
	c.mu.Lock()
	if err == nil && pending.cacheable {
		c.storeLocked(key.query, data, ttl)
	}
	pending.data, pending.err = append([]byte(nil), data...), err
	if c.pending[key] == pending {
		delete(c.pending, key)
	}
	close(pending.done)
	c.mu.Unlock()
}

func (c *browseCache) release(pending *browsePending) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending.waiters--
	if pending.waiters == 0 {
		if c.pending[pending.key] == pending {
			pending.cacheable = false
			delete(c.pending, pending.key)
		}
		pending.cancel()
	}
}

func (c *browseCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for _, pending := range c.pending {
		pending.cancel()
	}
	clear(c.entries)
	c.bytes = 0
}
