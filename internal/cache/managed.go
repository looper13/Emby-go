package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Counters struct {
	Hits             uint64  `json:"hits"`
	Misses           uint64  `json:"misses"`
	Loads            uint64  `json:"loads"`
	Shared           uint64  `json:"shared"`
	LoadErrors       uint64  `json:"load_errors"`
	LoadMilliseconds float64 `json:"load_milliseconds"`
}
type counters struct{ hits, misses, loads, shared, errors, nanos atomic.Uint64 }
type flight struct {
	done chan struct{}
	data []byte
	err  error
}

// Managed separates response generations from authentication, measures lookups,
// and collapses concurrent loads without holding a lock during I/O.
type Managed struct {
	backend    Cache
	namespace  string
	generation atomic.Uint64
	mu         sync.Mutex
	flights    map[string]*flight
	metrics    sync.Map
}

func NewManaged(backend Cache) *Managed {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return &Managed{backend: backend, namespace: hex.EncodeToString(id[:]), flights: make(map[string]*flight)}
}
func (m *Managed) key(key string) string {
	if strings.HasPrefix(key, "token:") || strings.HasPrefix(key, "apikey:") {
		return key
	}
	return "responses:" + m.namespace + ":" + strconv.FormatUint(m.generation.Load(), 10) + ":" + key
}
func (m *Managed) metric(key string) *counters {
	category, _, _ := strings.Cut(key, ":")
	// Fixed categories: never expose user-supplied values or tokens in metrics.
	switch category {
	case "token", "apikey", "items", "adminitems", "item", "entities", "similar", "libcover":
	default:
		category = "other"
	}
	if v, ok := m.metrics.Load(category); ok {
		return v.(*counters)
	}
	v, _ := m.metrics.LoadOrStore(category, &counters{})
	return v.(*counters)
}
func (m *Managed) Get(key string) ([]byte, bool) {
	data, ok := m.backend.Get(m.key(key))
	c := m.metric(key)
	if ok {
		c.hits.Add(1)
	} else {
		c.misses.Add(1)
	}
	return data, ok
}
func (m *Managed) Set(key string, data []byte, ttl time.Duration) {
	if ttl <= 0 {
		ttl = time.Hour
	}
	m.backend.Set(m.key(key), data, ttl)
}
func (m *Managed) Delete(key string) { m.backend.Delete(m.key(key)) }

// Clear invalidates responses only. Existing entries expire normally; no SCAN/DEL.
func (m *Managed) Clear() { m.generation.Add(1) }

// Backend 暴露底层实现，供诊断端点读取 Redis 之类的运行时状态。
func (m *Managed) Backend() Cache { return m.backend }
func (m *Managed) Stats() map[string]Counters {
	out := map[string]Counters{}
	m.metrics.Range(func(k, v any) bool {
		c := v.(*counters)
		out[k.(string)] = Counters{c.hits.Load(), c.misses.Load(), c.loads.Load(), c.shared.Load(), c.errors.Load(), float64(c.nanos.Load()) / 1e6}
		return true
	})
	return out
}
func (m *Managed) Load(ctx context.Context, key string, ttl time.Duration, load func() ([]byte, error)) ([]byte, error) {
	// Capture the physical generation before loading so invalidation racing with
	// an old request can never publish its result into the new generation.
	physical := m.key(key)
	if data, ok := m.backend.Get(physical); ok {
		m.metric(key).hits.Add(1)
		return data, nil
	}
	c := m.metric(key)
	c.misses.Add(1)
	m.mu.Lock()
	if pending := m.flights[physical]; pending != nil {
		c.shared.Add(1)
		m.mu.Unlock()
		select {
		case <-pending.done:
			return pending.data, pending.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pending := &flight{done: make(chan struct{}), err: errors.New("cache loader interrupted")}
	m.flights[physical] = pending
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.flights, physical); close(pending.done); m.mu.Unlock() }()
	// Recheck after becoming leader, outside the mutex: a slow Redis read must
	// never block loads for different keys.
	if data, ok := m.backend.Get(physical); ok {
		pending.data, pending.err = data, nil
		return data, nil
	}
	if err := ctx.Err(); err != nil {
		pending.err = err
		return nil, err
	}
	start := time.Now()
	c.loads.Add(1)
	pending.data, pending.err = load()
	c.nanos.Add(uint64(time.Since(start)))
	if pending.err != nil {
		c.errors.Add(1)
	} else {
		m.backend.Set(physical, pending.data, ttl)
	}
	return pending.data, pending.err
}
