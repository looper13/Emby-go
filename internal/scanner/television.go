package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"emby-go/internal/nfo"
	"emby-go/internal/store"
)

var episodePattern = regexp.MustCompile(`(?i)(?:^|[ ._-])S([0-9]{1,3})[ ._-]*E([0-9]{1,4})(?:$|[^0-9])`)
var seasonFolderPattern = regexp.MustCompile(`(?i)^(?:season|s)[ ._-]*([0-9]{1,3})$`)

func televisionRoot(lib store.Library, source string) string {
	dir := filepath.Dir(source)
	for current := dir; ; current = filepath.Dir(current) {
		if info, err := os.Stat(filepath.Join(current, "tvshow.nfo")); err == nil && !info.IsDir() {
			return current
		}
		if filepath.Clean(current) == filepath.Clean(lib.Path) || filepath.Dir(current) == current {
			break
		}
	}
	if seasonFolderPattern.MatchString(filepath.Base(dir)) || strings.EqualFold(filepath.Base(dir), "specials") {
		if filepath.Clean(dir) != filepath.Clean(lib.Path) {
			return filepath.Dir(dir)
		}
	}
	return dir
}

func applyTelevision(movie *store.Movie, meta nfo.MovieMeta, lib store.Library, read func(string) (nfo.MovieMeta, error)) error {
	episode := strings.EqualFold(meta.Root, "episodedetails") || meta.Season != nil || meta.Episode != nil
	if !episode && meta.Root != "movie" {
		return fmt.Errorf("源 NFO 必须是 movie 或 episodedetails: %s", movie.SourcePath)
	}
	if !episode {
		if lib.Type == store.LibraryTVShows {
			return fmt.Errorf("电视节目需要单集 NFO（episodedetails）: %s", movie.SourcePath)
		}
		return nil
	}
	if lib.Type != store.LibraryTVShows && lib.Type != store.LibraryMixed {
		return fmt.Errorf("单集不能加入影片库，请选择电视节目或混合内容: %s", movie.SourcePath)
	}
	season, number := -1, -1
	if match := episodePattern.FindStringSubmatch(strings.TrimSuffix(filepath.Base(movie.SourcePath), filepath.Ext(movie.SourcePath))); match != nil {
		season, _ = strconv.Atoi(match[1])
		number, _ = strconv.Atoi(match[2])
	}
	if meta.Season != nil {
		season = *meta.Season
	}
	if meta.Episode != nil {
		number = *meta.Episode
	}
	if season < 0 || season > 1000 || number < 1 || number > 10000 {
		return fmt.Errorf("单集缺少有效 season/episode: %s", movie.SourcePath)
	}
	root := televisionRoot(lib, movie.SourcePath)
	tv := &store.Television{SeriesPath: root, Title: strings.TrimSpace(meta.ShowTitle), Season: season, Episode: number}
	parent, err := read(filepath.Join(root, "tvshow.nfo"))
	if err == nil {
		if !strings.EqualFold(parent.Root, "tvshow") {
			return &fs.PathError{Op: "校验 NFO", Path: filepath.Join(root, "tvshow.nfo"), Err: fmt.Errorf("tvshow.nfo 根节点必须是 tvshow")}
		}
		if parent.Title != "" {
			tv.Title = parent.Title
		}
		tv.OriginalTitle, tv.Plot, tv.Year = parent.OriginalTitle, parent.Plot, parent.Year
		if len(movie.Genres) == 0 {
			movie.Genres = parent.Genres
		}
		if len(movie.Studios) == 0 {
			movie.Studios = parent.Studios
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if tv.Title == "" {
		tv.Title = filepath.Base(root)
	}
	if movie.Title == "" {
		movie.Title = fmt.Sprintf("第 %d 集", number)
	}
	if meta.Aired != "" {
		movie.Premiere = meta.Aired
	}
	if movie.Year == 0 {
		movie.Year = tv.Year
	}
	movie.Television = tv
	return nil
}

func readLibraryMetadataState(lib store.Library, source, fallback string) (metadataState, error) {
	if lib.Type != store.LibraryTVShows && lib.Type != store.LibraryMixed {
		return readMetadataState(source, fallback)
	}
	root := televisionRoot(lib, source)
	return readMetadataState(source, fallback, filepath.Join(root, "tvshow.nfo"))
}
