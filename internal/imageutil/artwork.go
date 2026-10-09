package imageutil

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func IsImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".webp", ".jpg", ".jpeg", ".png", ".gif", ".tbn":
		return true
	}
	return false
}

// ArtworkNames captures direct images and Emby's extrafanart images once per directory.
func ArtworkNames(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	return ArtworkNamesFromEntries(directory, entries)
}

func ArtworkNamesFromEntries(directory string, entries []os.DirEntry) ([]string, error) {
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && IsImage(entry.Name()) {
			names = append(names, entry.Name())
		} else if entry.IsDir() && strings.EqualFold(entry.Name(), "extrafanart") {
			extra, err := os.ReadDir(filepath.Join(directory, entry.Name()))
			if err != nil {
				return nil, err
			}
			for _, image := range extra {
				if !image.IsDir() && IsImage(image.Name()) {
					names = append(names, filepath.Join(entry.Name(), image.Name()))
				}
			}
		}
	}
	return names, nil
}

func backdropOrder(name string) (int, int, bool) {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	extra := strings.EqualFold(filepath.Dir(name), "extrafanart")
	for category, prefix := range []string{"backdrop", "fanart", "background", "art"} {
		if extra && prefix != "fanart" {
			continue
		}
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(base[len(prefix):], "-")
		if suffix == "" && !extra {
			return category, -1, true
		}
		if suffix == "" || strings.Trim(suffix, "0123456789") != "" {
			continue
		}
		number, err := strconv.Atoi(suffix)
		if err != nil {
			continue
		}
		if extra {
			category = 4
		}
		return category, number, true
	}
	return 0, 0, false
}

// IsLocalArtwork is shared with both realtime and polling monitors.
func IsLocalArtwork(path string) bool {
	if !IsImage(path) {
		return false
	}
	name := filepath.Base(path)
	if strings.EqualFold(filepath.Base(filepath.Dir(path)), "extrafanart") {
		name = filepath.Join("extrafanart", name)
	}
	if _, _, ok := backdropOrder(name); ok {
		return true
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	switch base {
	case "poster", "folder", "cover", "default", "movie", "thumb", "landscape":
		return true
	}
	if _, ok := MovieImageSource(path); ok {
		return true
	}
	// {name}.ext is primary artwork only when a matching source exists.
	stem := strings.TrimSuffix(path, filepath.Ext(path))
	for _, ext := range []string{".strm", ".STRM"} {
		if info, err := os.Stat(stem + ext); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

type ArtworkCatalog struct {
	names     map[string]string
	backdrops []string
}

func NewArtworkCatalog(names []string) *ArtworkCatalog {
	catalog := &ArtworkCatalog{names: make(map[string]string, len(names))}
	for _, name := range names {
		catalog.names[strings.ToLower(name)] = name
		if _, _, ok := backdropOrder(name); ok {
			catalog.backdrops = append(catalog.backdrops, name)
		}
	}
	sort.Slice(catalog.backdrops, func(i, j int) bool {
		left, ln, _ := backdropOrder(catalog.backdrops[i])
		right, rn, _ := backdropOrder(catalog.backdrops[j])
		if left != right {
			return left < right
		}
		if ln != rn {
			return ln < rn
		}
		return catalog.backdrops[i] < catalog.backdrops[j]
	})
	return catalog
}

func FindMovieImagesWithNames(source, directory string, exists func(string) bool, names []string) ImagePaths {
	return NewArtworkCatalog(names).FindMovieImages(source, directory, exists)
}

func (catalog *ArtworkCatalog) FindMovieImages(source, directory string, exists func(string) bool) ImagePaths {
	base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	find := func(bases ...string) string {
		for _, candidate := range bases {
			// Resolve actual casing from the snapshot on case-sensitive filesystems too.
			for _, ext := range imageExts {
				if name, ok := catalog.names[strings.ToLower(candidate+ext)]; ok && exists(filepath.Join(directory, name)) {
					return filepath.Join(directory, name)
				}
			}
			if path := findImageWith(directory, candidate, exists); path != "" {
				return path
			}
		}
		return ""
	}
	images := ImagePaths{
		Poster:    find(base+"-poster", base, base+"-cover", base+"-default", base+"-movie", "folder", "poster", "cover", "default", "movie"),
		Landscape: find(base+"-thumb", base+"-landscape", "thumb", "landscape"),
	}
	// Keep service-generated source-specific backdrops isolated from shared art.
	if path := find(base + "-fanart"); path != "" {
		images.Backdrops = []string{path}
		for _, name := range catalog.backdrops {
			if strings.EqualFold(filepath.Dir(name), "extrafanart") {
				path := filepath.Join(directory, name)
				if exists(path) {
					images.Backdrops = append(images.Backdrops, path)
				}
			}
		}
	} else {
		seen := make(map[string]bool)
		for _, name := range catalog.backdrops {
			path := filepath.Join(directory, name)
			if exists(path) {
				images.Backdrops = append(images.Backdrops, path)
				seen[strings.ToLower(path)] = true
			}
		}
		for _, base := range []string{"backdrop", "fanart", "background", "art"} {
			if path := find(base); path != "" && !seen[strings.ToLower(path)] {
				images.Backdrops = append(images.Backdrops, path)
			}
		}
		sort.SliceStable(images.Backdrops, func(i, j int) bool {
			left, _ := filepath.Rel(directory, images.Backdrops[i])
			right, _ := filepath.Rel(directory, images.Backdrops[j])
			lc, ln, _ := backdropOrder(left)
			rc, rn, _ := backdropOrder(right)
			if lc != rc {
				return lc < rc
			}
			if ln != rn {
				return ln < rn
			}
			return left < right
		})
	}
	if len(images.Backdrops) > 0 {
		images.Backdrop = images.Backdrops[0]
	}
	return images
}
