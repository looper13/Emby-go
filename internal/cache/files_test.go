package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryLimitedEntrySizeAndLRU(t *testing.T) {
	m := NewMemoryLimited(2, 3)
	m.Set("first", []byte("one"), time.Hour)
	m.Set("second", []byte("two"), time.Hour)
	m.Set("large", []byte("four"), time.Hour)
	if _, ok := m.Get("large"); ok {
		t.Fatal("stored an oversized hot entry")
	}
	m.Set("first", []byte("four"), time.Hour)
	if _, ok := m.Get("first"); ok {
		t.Fatal("oversized replacement left stale data")
	}
	m.Set("third", []byte("yes"), time.Hour)
	m.Get("second")
	m.Set("fourth", []byte("new"), time.Hour)
	if _, ok := m.Get("third"); ok {
		t.Fatal("entry count no longer bounded")
	}
}

func TestFilesRestartExpiryIntegrityAndUnknownFiles(t *testing.T) {
	dir := t.TempDir()
	unknown := filepath.Join(dir, "keep-user-file.txt")
	if err := os.WriteFile(unknown, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".asset-interrupted.tmp"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	newCache := func() *Files {
		f := NewFiles(dir, 4096, 4, 1024)
		f.now = func() time.Time { return clock }
		return f
	}
	key := "https://private.example/image?secret=not-a-filename"
	value := FileValue{Data: []byte("image bytes"), Expires: clock.Add(time.Hour)}
	f := newCache()
	if err := f.Set(key, value); err != nil {
		t.Fatal(err)
	}
	f = newCache()
	got, hit, err := f.Get(key)
	if err != nil || !hit || !bytes.Equal(got.Data, value.Data) || !got.Expires.Equal(value.Expires) {
		t.Fatal("restart did not reuse complete file", got, hit, err)
	}
	files, _ := os.ReadDir(dir)
	for _, file := range files {
		if file.Name() != "keep-user-file.txt" && !ownedFile(file.Name()) {
			t.Fatal("source URL or temp file persisted", file.Name())
		}
	}
	path := filepath.Join(dir, fileName(key))
	raw, _ := os.ReadFile(path)
	raw[len(raw)-1] ^= 1
	_ = os.WriteFile(path, raw, 0600)
	if _, hit, err := f.Get(key); err != nil || hit {
		t.Fatal("corrupt cache bytes were served", hit, err)
	}
	if err := f.Set(key, value); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Hour)
	if _, hit, err := f.Get(key); err != nil || hit {
		t.Fatal("expired value was served", hit, err)
	}
	if raw, err := os.ReadFile(unknown); err != nil || string(raw) != "keep" {
		t.Fatal("cache cleanup touched an unknown file", err)
	}
}

func TestFilesByteAndEntryBoundsAndExpiredReplacement(t *testing.T) {
	clock := time.Now()
	f := NewFiles(t.TempDir(), 2*(fileHeaderSize+10), 2, 40)
	f.now = func() time.Time { return clock }
	set := func(key string, length int, ttl time.Duration) {
		t.Helper()
		if err := f.Set(key, FileValue{bytes.Repeat([]byte("x"), length), clock.Add(ttl)}); err != nil {
			t.Fatal(err)
		}
		if f.total > f.maxBytes || len(f.entries) > f.maxEntries {
			t.Fatal("cache exceeded its bounds", f.total, len(f.entries))
		}
	}
	set("a", 10, time.Hour)
	set("b", 10, time.Hour)
	f.Get("a")
	set("c", 10, time.Hour)
	if _, hit, _ := f.Get("b"); hit {
		t.Fatal("did not evict least recently read asset")
	}
	set("a", 40, time.Hour)
	if _, hit, _ := f.Get("c"); hit {
		t.Fatal("did not enforce byte limit on replacement")
	}
	set("a", 10, time.Nanosecond)
	set("c", 10, time.Hour)
	clock = clock.Add(time.Second)
	set("a", 40, time.Hour)
	if f.total > f.maxBytes {
		t.Fatal("expired replacement incorrectly retained old-size credit")
	}
	if err := f.Set("huge", FileValue{bytes.Repeat([]byte("x"), 41), clock.Add(time.Hour)}); err == nil {
		t.Fatal("accepted an oversized single file")
	}
	// Rebuilding an index must enforce the new limits too.
	reopened := NewFiles(f.dir, fileHeaderSize+10, 1, 40)
	if _, _, err := reopened.Get("a"); err != nil || reopened.total > reopened.maxBytes {
		t.Fatal("restart exceeded capacity", err)
	}
}

func TestFilesInvalidDirectoryAndSymlinkIsolation(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "not-a-directory")
	_ = os.WriteFile(file, []byte("keep"), 0600)
	f := NewFiles(file, 4096, 2, 1024)
	if _, _, err := f.Get("a"); err == nil {
		t.Fatal("invalid directory did not fail gracefully")
	}
	dir := filepath.Join(root, "cache")
	_ = os.Mkdir(dir, 0700)
	link := filepath.Join(dir, fileName("link"))
	if err := os.Symlink(file, link); err != nil {
		t.Log("symlink fixture unavailable", err)
		return
	}
	f = NewFiles(dir, 4096, 2, 1024)
	if _, hit, err := f.Get("link"); hit || err != nil {
		t.Fatal("followed a symlink while rebuilding cache", hit, err)
	}
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "keep" {
		t.Fatal("touched symlink target", err)
	}
}
