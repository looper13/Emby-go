package cache

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryCopiesRemainIsolatedDuringReplacement(t *testing.T) {
	m := NewMemoryLimited(16, 1024)
	input := bytes.Repeat([]byte{7}, 1024)
	m.Set("key", input, time.Hour)
	input[0] = 9
	got, ok := m.Get("key")
	if !ok || got[0] != 7 {
		t.Fatal("Set must own an isolated copy")
	}
	got[0] = 3
	if next, _ := m.Get("key"); next[0] != 7 {
		t.Fatal("Get must return an isolated copy")
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Go(func() {
			for i := 0; i < 100; i++ {
				value := bytes.Repeat([]byte{byte(i)}, 1024)
				m.Set("key", value, time.Hour)
				if copy, ok := m.Get("key"); ok {
					if len(copy) != 1024 || !bytes.Equal(copy, bytes.Repeat(copy[:1], 1024)) {
						t.Error("replacement changed an already published buffer")
					}
					copy[0]++
				}
			}
		})
	}
	wg.Wait()
	m.Set("key", make([]byte, 1025), time.Hour)
	if _, ok := m.Get("key"); ok {
		t.Fatal("oversized replacement must evict the old entry")
	}
	for i := 0; i < 20; i++ {
		m.Set(fmt.Sprint(i), nil, time.Hour)
	}
	m.Clear()
	for i := 0; i < 20; i++ {
		if _, ok := m.Get(fmt.Sprint(i)); ok {
			t.Fatal("Clear left an entry")
		}
	}
}
