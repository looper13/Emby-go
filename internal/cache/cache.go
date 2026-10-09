package cache

import (
	"container/list"
	"sync"
	"time"
)

type Cache interface {
	Get(string) ([]byte, bool)
	Set(string, []byte, time.Duration)
	Delete(string)
	Clear()
}

// HealthReporter 由能报告运行时健康状况的后端实现（目前是 Redis）：
// 管理端设置页据此显示真实连通性与累计故障，而不是写死一个「在线」。
type HealthReporter interface {
	Health() (online bool, failures uint64)
}
type entry struct {
	key     string
	value   []byte
	expires time.Time
}
type Memory struct {
	mu    sync.Mutex
	max   int
	items map[string]*list.Element
	order *list.List
}

func NewMemory(max int) *Memory {
	if max < 1 {
		max = 256
	}
	return &Memory{max: max, items: map[string]*list.Element{}, order: list.New()}
}
func (m *Memory) Get(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.items[key]
	if !ok {
		return nil, false
	}
	v := e.Value.(entry)
	if !v.expires.IsZero() && time.Now().After(v.expires) {
		delete(m.items, key)
		m.order.Remove(e)
		return nil, false
	}
	m.order.MoveToFront(e)
	return append([]byte(nil), v.value...), true
}
func (m *Memory) Set(key string, value []byte, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.items[key]; ok {
		e.Value = entry{key: key, value: append([]byte(nil), value...), expires: expiry(ttl)}
		m.order.MoveToFront(e)
		return
	}
	e := m.order.PushFront(entry{key: key, value: append([]byte(nil), value...), expires: expiry(ttl)})
	m.items[key] = e
	if len(m.items) > m.max {
		old := m.order.Back()
		delete(m.items, old.Value.(entry).key)
		m.order.Remove(old)
	}
}
func expiry(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return time.Now().Add(ttl)
}
func (m *Memory) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.items[key]; ok {
		delete(m.items, key)
		m.order.Remove(e)
	}
}
func (m *Memory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = map[string]*list.Element{}
	m.order.Init()
}

// DeleteMatching removes local derived entries without discarding unrelated LRU data.
func (m *Memory) DeleteMatching(matches func(string) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, element := range m.items {
		if matches(key) {
			delete(m.items, key)
			m.order.Remove(element)
		}
	}
}
