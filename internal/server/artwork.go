package server

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"emby-go/internal/imageutil"
	"emby-go/internal/store"
)

// Catalogs belong to requests, not the metadata index. Concurrent requests for
// the same directory share a load; invalidation separates old and new loads.
type artworkCatalog struct {
	images       *imageutil.ArtworkCatalog
	names        map[string]bool
	loaded, used time.Time
	version      uint64
	backdrops    bool
}
type artworkLoad struct {
	done    chan struct{}
	catalog *artworkCatalog
	err     error
}
type artworkCache struct {
	mu         sync.Mutex
	entries    map[string]*artworkCatalog
	loads      map[artworkLoadKey]*artworkLoad
	generation uint64
}
type artworkLoadKey struct {
	directory           string
	backdrops           bool
	generation, version uint64
}

func (a *App) artworkCache() *artworkCache {
	a.artworkOnce.Do(func() {
		a.artworks = &artworkCache{entries: map[string]*artworkCatalog{}, loads: map[artworkLoadKey]*artworkLoad{}}
	})
	return a.artworks
}

func (cache *artworkCache) catalog(directory string, version uint64, backdrops bool) (*artworkCatalog, error) {
	path, now := cachePathKey(directory), time.Now()
	cache.mu.Lock()
	if entry := cache.entries[path]; entry != nil && entry.version == version && (!backdrops || entry.backdrops) && now.Sub(entry.loaded) < imageMetaTTL {
		entry.used = now
		cache.mu.Unlock()
		return entry, nil
	}
	key := artworkLoadKey{directory: path, generation: cache.generation, version: version, backdrops: backdrops}
	if pending := cache.loads[key]; pending != nil {
		cache.mu.Unlock()
		<-pending.done
		return pending.catalog, pending.err
	}
	pending := &artworkLoad{done: make(chan struct{})}
	cache.loads[key] = pending
	cache.mu.Unlock()
	var names []string
	var err error
	if backdrops {
		names, err = imageutil.ArtworkNames(directory)
	} else {
		names, err = imageutil.CoverNames(directory)
	}
	if err == nil {
		entry := &artworkCatalog{images: imageutil.NewArtworkCatalog(names), names: make(map[string]bool, len(names)), loaded: now, used: now, version: version, backdrops: backdrops}
		for _, name := range names {
			entry.names[cachePathKey(filepath.Join(directory, name))] = true
		}
		pending.catalog = entry
	}
	pending.err = err
	cache.mu.Lock()
	existing := cache.entries[path]
	if err == nil && cache.generation == key.generation && (backdrops || existing == nil || !existing.backdrops || existing.version != version) {
		count := len(pending.catalog.names)
		for existing, entry := range cache.entries {
			if existing == path || now.Sub(entry.loaded) >= imageMetaTTL {
				delete(cache.entries, existing)
			} else {
				count += len(entry.names)
			}
		}
		// Bound both directory count and total image names.
		for len(cache.entries) >= 256 || count > 200000 {
			oldest := ""
			for existing, entry := range cache.entries {
				if oldest == "" || entry.used.Before(cache.entries[oldest].used) {
					oldest = existing
				}
			}
			if oldest == "" {
				break
			}
			count -= len(cache.entries[oldest].names)
			delete(cache.entries, oldest)
		}
		if count <= 200000 {
			cache.entries[path] = pending.catalog
		}
	}
	delete(cache.loads, key)
	close(pending.done)
	cache.mu.Unlock()
	return pending.catalog, err
}

func (a *App) invalidateArtworkPaths(paths []string, recursive bool) {
	cache := a.artworkCache()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.generation++
	for _, path := range paths {
		directory := path
		if !recursive {
			directory = filepath.Dir(path)
		}
		if strings.EqualFold(filepath.Base(directory), "extrafanart") {
			directory = filepath.Dir(directory)
		}
		for existing := range cache.entries {
			if cachePathKey(directory) == existing || (recursive && withinCacheDirectory(cachePathKey(directory), existing)) {
				delete(cache.entries, existing)
			}
		}
	}
}

func (a *App) movieArtwork(movie store.Movie) store.Movie {
	return a.resolveMovieArtwork(movie, true)
}

func (a *App) movieCovers(movie store.Movie) store.Movie {
	return a.resolveMovieArtwork(movie, false)
}

func (a *App) resolveMovieArtwork(movie store.Movie, backdrops bool) store.Movie {
	if movie.SourcePath == "" {
		return movie
	}
	directory := movie.OutputDir
	if directory == "" {
		directory = filepath.Dir(movie.SourcePath)
	}
	catalog, err := a.artworkCache().catalog(directory, a.diskVersion(directory), backdrops)
	if err != nil && backdrops {
		// Unavailable extra stills must not hide otherwise readable covers.
		catalog, err = a.artworkCache().catalog(directory, a.diskVersion(directory), false)
	}
	if err != nil {
		return movie
	}
	exists := func(path string) bool { return catalog.names[cachePathKey(path)] && a.posterTag(path) != "0" }
	images := catalog.images.FindMovieCovers(movie.SourcePath, directory, exists)
	if backdrops {
		images = catalog.images.FindMovieImages(movie.SourcePath, directory, exists)
	}
	if movie.Status == "manual" {
		if images.Poster == "" && movie.PosterPath != "" && a.posterTag(movie.PosterPath) != "0" {
			images.Poster = movie.PosterPath
		}
		if images.Landscape == "" && movie.LandscapePath != "" && a.posterTag(movie.LandscapePath) != "0" {
			images.Landscape = movie.LandscapePath
		}
		if backdrops && len(images.Backdrops) == 0 {
			for _, path := range movie.Backdrops() {
				if a.posterTag(path) != "0" {
					images.Backdrops = append(images.Backdrops, path)
				}
			}
			if len(images.Backdrops) > 0 {
				images.Backdrop = images.Backdrops[0]
			}
		}
	}
	movie.PosterPath, movie.LandscapePath = images.Poster, images.Landscape
	movie.BackdropPath, movie.BackdropPaths = images.Backdrop, images.Backdrops
	return movie
}

func (a *App) artReference(path string, wide bool) string {
	if !strings.EqualFold(filepath.Ext(path), ".strm") {
		return path
	}
	movie := a.resolveMovieArtwork(store.Movie{SourcePath: path, OutputDir: filepath.Dir(path)}, wide)
	if wide && movie.BackdropPath != "" {
		return movie.BackdropPath
	}
	return movie.PosterPath
}
