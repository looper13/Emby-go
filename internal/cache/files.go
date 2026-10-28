package cache

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const fileHeaderSize = 44

type FileValue struct {
	Data    []byte
	Expires time.Time
}

type fileEntry struct {
	name    string
	size    int64
	expires time.Time
	touched time.Time
}

// Files stores only hashed assets in a dedicated directory, never source paths.
// The bounded index is rebuilt lazily; complete files survive process restarts.
type Files struct {
	mu                      sync.Mutex
	dir                     string
	maxBytes, maxEntryBytes int64
	maxEntries              int
	total                   int64
	entries                 map[string]*list.Element
	order                   *list.List
	initialized             bool
	now                     func() time.Time
}

func NewFiles(dir string, maxBytes int64, maxEntries int, maxEntryBytes int64) *Files {
	return &Files{dir: dir, maxBytes: maxBytes, maxEntries: maxEntries, maxEntryBytes: maxEntryBytes,
		entries: map[string]*list.Element{}, order: list.New(), now: time.Now}
}

func fileName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:]) + ".cache"
}

func ownedFile(name string) bool {
	if len(name) != 70 || !strings.HasSuffix(name, ".cache") {
		return false
	}
	_, err := hex.DecodeString(name[:64])
	return err == nil && strings.ToLower(name) == name
}

func fileHeader(expires time.Time, data []byte) []byte {
	header := make([]byte, fileHeaderSize)
	copy(header, "EGC1")
	binary.BigEndian.PutUint64(header[4:12], uint64(expires.UnixNano()))
	sum := sha256.Sum256(data)
	copy(header[12:], sum[:])
	return header
}

func headerExpiry(header []byte) (time.Time, bool) {
	if len(header) < fileHeaderSize || string(header[:4]) != "EGC1" {
		return time.Time{}, false
	}
	return time.Unix(0, int64(binary.BigEndian.Uint64(header[4:12]))), true
}

func (f *Files) initLocked() error {
	if f.initialized {
		return nil
	}
	dir, err := filepath.Abs(f.dir)
	if err != nil {
		return err
	}
	if f.maxEntries < 1 || f.maxBytes <= fileHeaderSize || f.maxEntryBytes < 1 {
		return errors.New("invalid asset cache limits")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("asset cache must be a real directory")
	}
	f.dir = dir
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	entries := []fileEntry{}
	for _, file := range files {
		name := file.Name()
		if !file.Type().IsRegular() || (!ownedFile(name) && !(strings.HasPrefix(name, ".asset-") && strings.HasSuffix(name, ".tmp"))) {
			continue
		}
		path := filepath.Join(dir, name)
		if !ownedFile(name) {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		info, err := file.Info()
		if err != nil {
			return err
		}
		handle, err := os.Open(path)
		if err != nil {
			return err
		}
		header := make([]byte, fileHeaderSize)
		_, readErr := io.ReadFull(handle, header)
		handle.Close()
		expires, valid := headerExpiry(header)
		if readErr != nil || !valid || !expires.After(f.now()) || info.Size() <= fileHeaderSize || info.Size() > f.maxEntryBytes+fileHeaderSize {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		entries = append(entries, fileEntry{name, info.Size(), expires, info.ModTime()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].touched.Before(entries[j].touched) })
	f.entries, f.order, f.total = map[string]*list.Element{}, list.New(), 0
	for _, entry := range entries {
		f.entries[entry.name] = f.order.PushFront(entry)
		f.total += entry.size
	}
	if err := f.expireLocked(); err != nil {
		return err
	}
	if err := f.pruneLocked(f.maxBytes, f.maxEntries); err != nil {
		return err
	}
	f.initialized = true
	return nil
}

func (f *Files) removeLocked(element *list.Element) error {
	entry := element.Value.(fileEntry)
	path := filepath.Join(f.dir, entry.name)
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("asset cache entry is not a regular file")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	delete(f.entries, entry.name)
	f.order.Remove(element)
	f.total -= entry.size
	return nil
}

func (f *Files) expireLocked() error {
	for element := f.order.Back(); element != nil; {
		previous := element.Prev()
		if !element.Value.(fileEntry).expires.After(f.now()) {
			if err := f.removeLocked(element); err != nil {
				return err
			}
		}
		element = previous
	}
	return nil
}

func (f *Files) pruneLocked(maxBytes int64, maxEntries int) error {
	for f.total > maxBytes || len(f.entries) > maxEntries {
		if err := f.removeLocked(f.order.Back()); err != nil {
			return err
		}
	}
	return nil
}

func (f *Files) Get(key string) (FileValue, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.initLocked(); err != nil {
		return FileValue{}, false, err
	}
	element := f.entries[fileName(key)]
	if element == nil {
		return FileValue{}, false, nil
	}
	entry := element.Value.(fileEntry)
	path := filepath.Join(f.dir, entry.name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		_ = f.removeLocked(element)
		return FileValue{}, false, nil
	}
	if err != nil {
		return FileValue{}, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > f.maxEntryBytes+fileHeaderSize {
		return FileValue{}, false, errors.New("invalid asset cache file")
	}
	handle, err := os.Open(path)
	if err != nil {
		return FileValue{}, false, err
	}
	data, err := io.ReadAll(io.LimitReader(handle, f.maxEntryBytes+fileHeaderSize+1))
	handle.Close()
	expires, valid := headerExpiry(data)
	if err != nil {
		return FileValue{}, false, err
	}
	if valid && len(data) > fileHeaderSize && int64(len(data)) <= f.maxEntryBytes+fileHeaderSize {
		sum := sha256.Sum256(data[fileHeaderSize:])
		valid = bytes.Equal(data[12:fileHeaderSize], sum[:]) && expires.After(f.now())
	} else {
		valid = false
	}
	if !valid {
		if err := f.removeLocked(element); err != nil {
			return FileValue{}, false, err
		}
		return FileValue{}, false, nil
	}
	if f.now().Sub(entry.touched) > 5*time.Minute {
		_ = os.Chtimes(path, f.now(), f.now())
		entry.touched = f.now()
		element.Value = entry
	}
	f.order.MoveToFront(element)
	return FileValue{data[fileHeaderSize:], expires}, true, nil
}

func (f *Files) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.initLocked(); err != nil {
		return err
	}
	if element := f.entries[fileName(key)]; element != nil {
		return f.removeLocked(element)
	}
	return nil
}

func (f *Files) Set(key string, value FileValue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(value.Data) == 0 || int64(len(value.Data)) > f.maxEntryBytes || !value.Expires.After(f.now()) {
		return errors.New("invalid asset cache value")
	}
	if err := f.initLocked(); err != nil {
		return err
	}
	name, size := fileName(key), int64(len(value.Data)+fileHeaderSize)
	if size > f.maxBytes {
		return errors.New("asset exceeds cache capacity")
	}
	if err := f.expireLocked(); err != nil {
		return err
	}
	oldSize, oldCount := int64(0), 0
	if old := f.entries[name]; old != nil {
		oldSize, oldCount = old.Value.(fileEntry).size, 1
		f.order.MoveToFront(old)
	}
	if err := f.pruneLocked(f.maxBytes-size+oldSize, f.maxEntries-1+oldCount); err != nil {
		return err
	}
	file, err := os.CreateTemp(f.dir, ".asset-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(fileHeader(value.Expires, value.Data)); err == nil {
		_, err = file.Write(value.Data)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(file.Name(), filepath.Join(f.dir, name)); err != nil {
		return err
	}
	if old := f.entries[name]; old != nil {
		f.total -= old.Value.(fileEntry).size
		f.order.Remove(old)
	}
	f.entries[name] = f.order.PushFront(fileEntry{name, size, value.Expires, f.now()})
	f.total += size
	return nil
}
