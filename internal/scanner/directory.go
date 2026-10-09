package scanner

import (
	"os"
	"path/filepath"
	"strings"

	"emby-go/internal/imageutil"
)

type imageDirectory struct {
	path         string
	names        map[string]bool
	reuse        bool
	large        bool
	statOnly     bool
	imageInfo    map[string]os.FileInfo
	artworkNames []string
	catalog      *imageutil.ArtworkCatalog
}

func (directory *imageDirectory) loadArtwork(entries []os.DirEntry) error {
	artworkNames, err := imageutil.ArtworkNamesFromEntries(directory.path, entries)
	if err != nil {
		return err
	}
	directory.artworkNames = artworkNames
	directory.catalog = imageutil.NewArtworkCatalog(artworkNames)
	for _, name := range artworkNames {
		directory.names[strings.ToLower(name)] = true
	}
	return nil
}

func (directory *imageDirectory) load(path string) error {
	if directory.statOnly && directory.path == path {
		if directory.imageInfo == nil {
			directory.imageInfo = make(map[string]os.FileInfo)
		}
		return nil
	}
	if directory.path == path && directory.reuse && directory.imageInfo != nil {
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	artworkNames, err := imageutil.ArtworkNamesFromEntries(path, entries)
	if err != nil {
		return err
	}
	names := make(map[string]bool)
	for _, name := range artworkNames {
		names[strings.ToLower(name)] = true
	}
	directory.artworkNames = artworkNames
	directory.catalog = imageutil.NewArtworkCatalog(artworkNames)
	directory.path, directory.names = path, names
	directory.large = len(entries) > 64
	directory.imageInfo = make(map[string]os.FileInfo)
	return nil
}

// Merge freshly observed candidates into the scan snapshot without enumerating
// a large directory again for every movie.
func (directory *imageDirectory) acceptFresh(fresh *imageDirectory, before imageutil.ImagePaths) {
	for _, path := range append([]string{before.Poster, before.Landscape}, before.Backdrops...) {
		if path != "" {
			if _, exists := fresh.imageInfo[path]; !exists {
				delete(directory.imageInfo, path)
				relative, _ := filepath.Rel(directory.path, path)
				delete(directory.names, strings.ToLower(relative))
			}
		}
	}
	for path, info := range fresh.imageInfo {
		directory.imageInfo[path] = info
		relative, _ := filepath.Rel(directory.path, path)
		directory.names[strings.ToLower(relative)] = true
	}
}

func (directory *imageDirectory) contains(path string) bool {
	relative, err := filepath.Rel(directory.path, path)
	return directory.names == nil || (err == nil && directory.names[strings.ToLower(relative)])
}

// statFile 读取图片文件属性。测试替换它来统计探测次数（大目录里对不存在的
// 候选图片名做属性查询是纯浪费），生产路径就是 os.Stat。
var statFile = os.Stat

// selectImages shares a snapshot only within one scan. Stability checks use
// a fresh imageDirectory even when directory mtime was preserved.
func (directory *imageDirectory) selectImages(source string) (imageutil.ImagePaths, map[string]os.FileInfo, error) {
	path := filepath.Dir(source)
	if err := directory.load(path); err != nil {
		return imageutil.ImagePaths{}, nil, err
	}
	info := directory.imageInfo
	selected := directory.catalog.FindMovieImages(source, path, func(image string) bool {
		if _, ok := info[image]; ok {
			return true
		}
		if !directory.contains(image) {
			return false
		}
		stamp, err := statFile(image)
		if err == nil && !stamp.IsDir() {
			info[image] = stamp
		}
		return err == nil && !stamp.IsDir()
	})
	return selected, info, nil
}
