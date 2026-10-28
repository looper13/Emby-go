package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"log/slog"
	"path/filepath"
	"time"

	"emby-go/internal/cache"
)

const (
	r18CoverTTL         = 24 * time.Hour
	r18CoverDiskBytes   = 512 << 20
	r18CoverDiskEntries = 10000
	r18CoverHotEntries  = 256
	r18CoverHotBytes    = 128 << 10
)

func (a *App) initR18CoverCache() {
	a.r18CoverOnce.Do(func() {
		dir := r18CoverCacheDirectory(a.configSnapshot().DBPath, a.serverID)
		a.r18CoverFiles = cache.NewFiles(dir, r18CoverDiskBytes, r18CoverDiskEntries, r18ImageLimit+256)
		a.r18CoverMemory = cache.NewManaged(cache.NewMemoryLimited(r18CoverHotEntries, r18CoverHotBytes))
	})
}

func r18CoverCacheDirectory(dbPath, serverID string) string {
	db, _ := filepath.Abs(dbPath)
	namespace := sha256.Sum256([]byte(db + ":" + serverID))
	return filepath.Join(filepath.Dir(db), ".work-cache", "r18-covers", hex.EncodeToString(namespace[:8]))
}

func (a *App) r18CoverCacheWarning(stage string, err error) {
	a.r18CoverWarnings.Do(func() {
		slog.Warn("R18 cover disk cache unavailable; using bounded memory cache", "stage", stage, "error", err)
	})
}

func coverPacket(value cache.FileValue) []byte {
	data := make([]byte, 8, len(value.Data)+8)
	binary.BigEndian.PutUint64(data, uint64(value.Expires.UnixNano()))
	return append(data, value.Data...)
}

// Stable asset keys live on disk; Managed provides hot caching and singleflight.
// Keep the original expiration in memory so a disk hit cannot extend freshness.
func (a *App) loadR18Cover(ctx context.Context, key string, load func() (cache.FileValue, error)) (cache.FileValue, error) {
	a.initR18CoverCache()
	for attempt := 0; attempt < 2; attempt++ {
		packet, err := a.r18CoverMemory.Load(ctx, key, r18CoverTTL, func() ([]byte, error) {
			value, hit, err := a.r18CoverFiles.Get(key)
			if err != nil {
				a.r18CoverCacheWarning("read", err)
			}
			if hit {
				return coverPacket(value), nil
			}
			value, err = load()
			if err != nil {
				return nil, err
			}
			if value.Expires.IsZero() {
				value.Expires = time.Now().Add(r18CoverTTL)
			}
			if err := a.r18CoverFiles.Set(key, value); err != nil {
				a.r18CoverCacheWarning("write", err)
			}
			return coverPacket(value), nil
		})
		if err != nil {
			return cache.FileValue{}, err
		}
		if len(packet) > 8 {
			expires := time.Unix(0, int64(binary.BigEndian.Uint64(packet[:8])))
			if expires.After(time.Now()) {
				return cache.FileValue{Data: packet[8:], Expires: expires}, nil
			}
		}
		a.r18CoverMemory.Delete(key)
	}
	return cache.FileValue{}, errors.New("invalid cover cache entry")
}
