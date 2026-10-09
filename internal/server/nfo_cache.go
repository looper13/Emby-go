package server

import (
	"container/list"
	"time"

	"github.com/gin-gonic/gin"

	"emby-go/internal/nfo"
)

const (
	// nfoCacheMaxEntries NFO 解析结果缓存上限，超限时按 LRU 逐条淘汰。
	// 早期版本到上限后整表清空：一旦命中上限，刚访问过的热点也会一起丢掉，
	// 列表页紧接着要为整屏影片重新读盘 + 解析 XML。
	nfoCacheMaxEntries = 20000
	// nfoReadErrorTTL 读取/解析失败的短缓存寿命。失败结果必须能较快重试：
	// NFO 在被写入（刮削）前会长期不存在，若按 mtime 长期命中，文件出现后
	// 仍会一直返回通用流信息。正常 NFO 没有 <streamdetails> 不算失败，
	// 按 tag（mtime）长期命中，写入探测结果后 mtime 变化自然重读。
	nfoReadErrorTTL = 30 * time.Second
)

// nfoCacheEntry NFO 解析结果。tag/version 记录读入时的磁盘状态，
// 命中时用它校验条目是否还代表当前文件。
type nfoCacheEntry struct {
	tag     string
	version uint64
	streams []gin.H
	size    int64 // NFO <fileinfo><size>：媒体文件字节数（探测写入）
	probed  bool  // NFO 里有可用的 <streamdetails>（详情页「是否已探测」）
	readAt  time.Time
	failed  bool // 上次读取或解析失败（短 TTL）
}

// nfoCacheItem 让 LRU 元素能记住自己的键（淘汰时需要从索引里删掉）。
type nfoCacheItem struct {
	key   string
	entry nfoCacheEntry
}

// nfoFlightKey 只合并同一磁盘版本的读取，失效后的请求不加入旧读取。
type nfoFlightKey struct {
	path    string
	tag     string
	version uint64
}

// nfoFlight 同路径、同版本读取合并。
type nfoFlight struct {
	done  chan struct{}
	entry nfoCacheEntry
}

// nfoCache 按路径保存 NFO 解析结果的 LRU。
//
// 键是 NFO 路径而不是「路径 + mtime tag」：NFO 被刮削反复改写时，
// 旧版本条目会逐条替换掉而不是越积越多（历史实现会把每次改写都留一张新键）。
type nfoCache struct {
	max     int
	items   map[string]*list.Element
	order   *list.List
	flights map[nfoFlightKey]*nfoFlight
}

func newNFOCache(max int) *nfoCache {
	if max < 1 {
		max = 1
	}
	return &nfoCache{
		max:     max,
		items:   make(map[string]*list.Element),
		order:   list.New(),
		flights: make(map[nfoFlightKey]*nfoFlight),
	}
}

// get 返回仍然有效的条目：tag/version 与当前磁盘状态一致，且失败缓存未过期。
func (cache *nfoCache) get(key, tag string, version uint64, now time.Time) (nfoCacheEntry, bool) {
	element, ok := cache.items[key]
	if !ok {
		return nfoCacheEntry{}, false
	}
	item := element.Value.(*nfoCacheItem)
	if item.entry.tag != tag || item.entry.version != version {
		return nfoCacheEntry{}, false
	}
	if item.entry.failed && now.Sub(item.entry.readAt) >= nfoReadErrorTTL {
		return nfoCacheEntry{}, false
	}
	cache.order.MoveToFront(element)
	return item.entry, true
}

func (cache *nfoCache) put(key string, entry nfoCacheEntry) {
	if element, ok := cache.items[key]; ok {
		element.Value.(*nfoCacheItem).entry = entry
		cache.order.MoveToFront(element)
		return
	}
	cache.items[key] = cache.order.PushFront(&nfoCacheItem{key: key, entry: entry})
	if len(cache.items) > cache.max {
		if oldest := cache.order.Back(); oldest != nil {
			delete(cache.items, oldest.Value.(*nfoCacheItem).key)
			cache.order.Remove(oldest)
		}
	}
}

// deleteMatching 只丢弃匹配路径的条目（媒体库局部变化时用）。
func (cache *nfoCache) deleteMatching(matches func(string) bool) {
	for key, element := range cache.items {
		if matches(key) {
			delete(cache.items, key)
			cache.order.Remove(element)
		}
	}
}

func (cache *nfoCache) len() int { return len(cache.items) }

// readNFOEntry 读一次 NFO 并映射为缓存条目；不做锁与缓存判断。
func readNFOEntry(path string) nfoCacheEntry {
	entry := nfoCacheEntry{streams: genericVideoStream()}
	meta, err := nfo.Read(path)
	if err != nil {
		entry.failed = true
		return entry
	}
	if meta.FileInfo != nil {
		entry.size = meta.FileInfo.Size
		if details := meta.FileInfo.StreamDetails; details != nil {
			if out := buildStreams(details); len(out) > 0 {
				entry.streams, entry.probed = out, true
			}
		}
	}
	return entry
}
