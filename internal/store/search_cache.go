package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"emby-go/internal/cache"
)

type searchCountCache struct {
	once     sync.Once
	values   *cache.Memory
	revision atomic.Uint64
	users    sync.Map // user id -> revision of played/favorite filters
}

func (s *Store) touchSearchUserData() {
	v, _ := s.searchCounts.users.LoadOrStore(s.UserID(), &atomic.Uint64{})
	v.(*atomic.Uint64).Add(1)
}

// Counts depend on filters and visibility, not sort order, random seed or page.
// Revisions in the key keep an old concurrent fill from surviving a write.
func (s *Store) cachedSearchCount(clause string, args []any, userData bool) (int, error) {
	c := &s.searchCounts
	c.once.Do(func() { c.values = cache.NewMemory(256) })
	var userRevision uint64
	if userData {
		if v, ok := c.users.Load(s.UserID()); ok {
			userRevision = v.(*atomic.Uint64).Load()
		}
	}
	payload, err := json.Marshal(struct {
		Clause       string
		Args         []any
		Access       string
		Revision     uint64
		UserRevision uint64
	}{clause, args, s.AccessVersion(), c.revision.Load(), userRevision})
	if err != nil {
		return 0, err
	}
	digest := sha256.Sum256(payload)
	key := hex.EncodeToString(digest[:])
	if value, ok := c.values.Get(key); ok {
		if count, err := strconv.Atoi(string(value)); err == nil {
			return count, nil
		}
	}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM movies"+clause, args...).Scan(&count); err != nil {
		return 0, err
	}
	c.values.Set(key, []byte(strconv.Itoa(count)), 30*time.Second)
	return count, nil
}
