package nfo

import (
	"hash/fnv"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Fixed stripes bound memory even when a library contains millions of paths.
// Normalize aliases so relative/absolute and Windows case variants serialize.
var pathLocks [256]sync.Mutex

func lockNFOPath(path string) func() {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(path))
	mu := &pathLocks[hash.Sum32()%uint32(len(pathLocks))]
	mu.Lock()
	return mu.Unlock
}
