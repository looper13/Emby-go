package scanner

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"emby-go/internal/imageutil"
	"emby-go/internal/nfo"
	"emby-go/internal/store"
)

type Result struct {
	Success      int `json:"success"`
	Pending      int `json:"pending"`
	Incompatible int `json:"incompatible"`
	Failed       int `json:"failed"`
	Added        int `json:"added"`
	Updated      int `json:"updated"`
	Skipped      int `json:"skipped"`
	Deleted      int `json:"deleted"`
}

func (result *Result) add(other Result) {
	result.Success += other.Success
	result.Pending += other.Pending
	result.Incompatible += other.Incompatible
	result.Failed += other.Failed
	result.Added += other.Added
	result.Updated += other.Updated
	result.Skipped += other.Skipped
	result.Deleted += other.Deleted
}

type candidate struct {
	path     string
	info     os.FileInfo
	base     string
	part     int
	groupKey string
}

// Progress 单次媒体库扫描的进度快照，供管理端轮询展示。
type Progress struct {
	LibraryID   int64  `json:"library_id"`
	LibraryName string `json:"library_name"`
	Total       int    `json:"total"`
	Done        int    `json:"done"`
	Current     string `json:"current"`
	Result      Result `json:"result"`
}

var cdPartPattern = regexp.MustCompile(`(?i)^(.*?)[ ._-]+CD([1-9][0-9]*)$`)

// Scan 按 Emby 的 stacking 语义处理末尾 -CDn 文件：CD1 为逻辑影片，CD2..CDn 作为 AdditionalParts。
func Scan(s *store.Store, lib store.Library) (Result, error) {
	return ScanWithProgress(s, lib, nil)
}

// ScanWithProgress 与 Scan 相同，但在每个分组处理完后回调进度（可为 nil）。
func ScanWithProgress(s *store.Store, lib store.Library, onProgress func(Progress)) (Result, error) {
	return scanWithProgress(s, lib, false, onProgress)
}

func RebuildWithProgress(s *store.Store, lib store.Library, onProgress func(Progress)) (Result, error) {
	return scanWithProgress(s, lib, true, onProgress)
}

func scanWithProgress(s *store.Store, lib store.Library, full bool, onProgress func(Progress)) (result Result, scanErr error) {
	return scanDirectory(s, lib, lib.Path, true, full, nil, onProgress)
}

func RefreshDirectory(s *store.Store, lib store.Library, directory string, recursive bool, onProgress func(Progress)) (Result, error) {
	root, err := filepath.Abs(lib.Path)
	if err != nil {
		return Result{}, err
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return Result{}, err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Result{}, fmt.Errorf("刷新目录不在媒体库内: %s", directory)
	}
	return scanDirectory(s, lib, filepath.Join(lib.Path, relative), recursive, false, nil, onProgress)
}

func RefreshFiles(s *store.Store, lib store.Library, files []string, onProgress func(Progress)) (Result, error) {
	if len(files) == 0 {
		return Result{}, nil
	}
	root, err := filepath.Abs(lib.Path)
	if err != nil {
		return Result{}, err
	}
	normalized := make([]string, 0, len(files))
	directory := ""
	for _, file := range files {
		absolute, err := filepath.Abs(file)
		if err != nil {
			return Result{}, err
		}
		relative, err := filepath.Rel(root, absolute)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return Result{}, fmt.Errorf("刷新文件不在媒体库内: %s", file)
		}
		path := filepath.Join(lib.Path, relative)
		if directory != "" && directory != filepath.Dir(path) {
			return Result{}, errors.New("局部刷新文件必须位于同一目录")
		}
		directory = filepath.Dir(path)
		normalized = append(normalized, path)
	}
	return scanDirectory(s, lib, directory, false, true, normalized, onProgress)
}

func scanDirectory(s *store.Store, lib store.Library, directory string, recursive, full bool, files []string, onProgress func(Progress)) (result Result, scanErr error) {
	rootInfo, err := os.Stat(lib.Path)
	if err != nil {
		return result, err
	}
	if !rootInfo.IsDir() {
		return result, fmt.Errorf("媒体库路径不是目录: %s", lib.Path)
	}
	groups := make(map[string][]candidate)
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == directory && filepath.Clean(directory) != filepath.Clean(lib.Path) && os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if path == directory && !entry.IsDir() {
			return fmt.Errorf("媒体库根目录无法遍历: %s", path)
		}
		if entry.IsDir() && path != directory && !recursive {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".strm") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		candidate := candidate{path: path, info: info, base: base, groupKey: groupKey(path)}
		if match := cdPartPattern.FindStringSubmatch(base); match != nil {
			candidate.base = match[1]
			candidate.part = parsePart(match[2])
			candidate.groupKey = filepath.Join(filepath.Dir(path), strings.ToLower(match[1]))
		}
		groups[candidate.groupKey] = append(groups[candidate.groupKey], candidate)
		return nil
	})
	if err != nil {
		return result, err
	}
	var fingerprints map[string]string
	if recursive && filepath.Clean(directory) == filepath.Clean(lib.Path) {
		fingerprints, err = s.ScanFingerprints(lib.ID)
	} else {
		fingerprints, err = s.DirectoryScanFingerprints(lib.ID, filepath.Clean(directory), recursive)
	}
	if err != nil {
		return result, err
	}
	if len(files) > 0 {
		keys, all := affectedGroups(files)
		if !all {
			for key := range groups {
				if !keys[key] {
					delete(groups, key)
				}
			}
			for path := range fingerprints {
				if !keys[groupKey(path)] {
					delete(fingerprints, path)
				}
			}
		}
	}
	defer func() {
		if result.Added+result.Updated+result.Deleted > 0 || scanErr != nil {
			scanErr = errors.Join(scanErr, s.BumpVersion(lib.ID))
		}
	}()

	total := len(groups)
	report := func(done int, current string) {
		if onProgress != nil {
			onProgress(Progress{LibraryID: lib.ID, LibraryName: lib.Name, Total: total, Done: done, Current: current, Result: result})
		}
	}
	report(0, "")

	paths := make(map[string]struct{})
	const batchSize = 100
	batch := make([]store.ScannedMovie, 0, batchSize)
	batchResult := Result{}
	lastFlush := time.Now()
	flush := func() error {
		if err := s.SaveScannedMovies(batch); err != nil {
			return err
		}
		result.add(batchResult)
		clear(batch)
		batch = batch[:0]
		batchResult = Result{}
		lastFlush = time.Now()
		return nil
	}
	images := &imageDirectory{}
	process := func(item candidate, parts []string, fallbackNFO string) error {
		paths[item.path] = struct{}{}
		before, err := readSourceState(item.path, parts, fallbackNFO, images)
		if err != nil {
			result.Failed++
			return nil
		}
		previous, exists := fingerprints[item.path]
		if !full && previous == before.Fingerprint {
			result.Skipped++
			return nil
		}
		item.info = before.Info
		entry, outcome := prepareCandidate(lib, item, parts, fallbackNFO, before.Images)
		if outcome.Failed != 0 {
			result.add(outcome)
			return nil
		}
		if exists {
			outcome.Updated++
		} else {
			outcome.Added++
		}
		if after, err := readSourceState(item.path, parts, fallbackNFO, images); err == nil && after.Fingerprint == before.Fingerprint {
			entry.Fingerprint = before.Fingerprint
		}
		batch = append(batch, entry)
		batchResult.add(outcome)
		if len(batch) >= batchSize || time.Since(lastFlush) >= 500*time.Millisecond {
			return flush()
		}
		return nil
	}
	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	done := 0
	for _, key := range groupKeys {
		group := groups[key]
		primary, parts, ok := stackedGroup(group)
		if !ok {
			for _, item := range group {
				if err := process(item, nil, ""); err != nil {
					return result, err
				}
			}
		} else {
			partPaths := make([]string, 0, len(parts))
			for _, part := range parts {
				partPaths = append(partPaths, part.path)
			}
			fallbackNFO := filepath.Join(filepath.Dir(primary.path), primary.base+".nfo")
			if err := process(primary, partPaths, fallbackNFO); err != nil {
				return result, err
			}
		}
		done++
		if len(batch) > 0 && time.Since(lastFlush) >= 500*time.Millisecond {
			if err := flush(); err != nil {
				return result, err
			}
		}
		report(done, key)
	}
	if err := flush(); err != nil {
		return result, err
	}

	if result.Failed == 0 {
		var missing []string
		for path := range fingerprints {
			if _, exists := paths[path]; !exists {
				missing = append(missing, path)
			}
		}
		result.Deleted, err = s.DeleteScannedSources(lib.ID, missing)
		if err != nil {
			return result, err
		}
	}
	report(done, "")
	return result, nil
}

// RescanOne 只重扫一个 .strm，用于单条刮削/编辑后立即刷新索引。
//
// 与 Scan 的两点关键差异：
//   - **不跑 DeleteMissingSources**：单文件重扫不该触发「按磁盘现状删索引」，
//     否则一次误传路径就能删掉整库索引；
//   - 只处理该文件所属的分组（CD1/CD2 同组一起重建 AdditionalParts），
//     批量场景下逐条调用它是 O(n²)，所以批量路径仍走整库 Scan。
func RescanOne(s *store.Store, lib store.Library, strmPath string) (Result, error) {
	var result Result
	info, err := os.Stat(strmPath)
	if err != nil {
		return result, err
	}
	if info.IsDir() || !strings.EqualFold(filepath.Ext(strmPath), ".strm") {
		return result, errors.New("不是 .strm 文件: " + strmPath)
	}

	base := strings.TrimSuffix(filepath.Base(strmPath), filepath.Ext(strmPath))
	candidate := candidate{path: strmPath, info: info, base: base, groupKey: strmPath}
	if match := cdPartPattern.FindStringSubmatch(base); match != nil {
		candidate.base = match[1]
		candidate.part = parsePart(match[2])
		candidate.groupKey = filepath.Join(filepath.Dir(strmPath), strings.ToLower(match[1]))
	}

	paths := make(map[string]struct{})
	group := collectGroup(candidate)
	if primary, parts, ok := stackedGroup(group); ok {
		partPaths := make([]string, 0, len(parts))
		for _, part := range parts {
			partPaths = append(partPaths, part.path)
		}
		fallbackNFO := filepath.Join(filepath.Dir(primary.path), primary.base+".nfo")
		if err := scanCandidate(s, lib, primary, partPaths, fallbackNFO, &result, paths); err != nil {
			return result, err
		}
	} else {
		for _, item := range group {
			if err := scanCandidate(s, lib, item, nil, "", &result, paths); err != nil {
				return result, err
			}
		}
	}
	return result, s.BumpVersion(lib.ID)
}

// collectGroup 找到与目标候选同一 CD 分组的所有文件；非分集文件只返回它自己。
// 分组必须从磁盘现读：AdditionalParts 依赖同目录里实际存在的 CD2..CDn。
func collectGroup(target candidate) []candidate {
	if target.part == 0 {
		return []candidate{target}
	}
	entries, err := os.ReadDir(filepath.Dir(target.path))
	if err != nil {
		return []candidate{target}
	}
	group := []candidate{target}
	want := strings.ToLower(target.base)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".strm") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		match := cdPartPattern.FindStringSubmatch(name)
		if match == nil || strings.ToLower(match[1]) != want {
			continue
		}
		path := filepath.Join(filepath.Dir(target.path), entry.Name())
		if path == target.path {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		group = append(group, candidate{path: path, info: info, base: match[1],
			part: parsePart(match[2]), groupKey: target.groupKey})
	}
	return group
}

func stackedGroup(group []candidate) (candidate, []candidate, bool) {
	var primary candidate
	var found bool
	for _, item := range group {
		if item.part == 1 {
			primary, found = item, true
			break
		}
	}
	if !found || len(group) < 2 {
		return candidate{}, nil, false
	}
	sort.Slice(group, func(i, j int) bool { return group[i].part < group[j].part })
	parts := make([]candidate, 0, len(group)-1)
	for _, item := range group {
		if item.path != primary.path {
			parts = append(parts, item)
		}
	}
	return primary, parts, true
}

func scanCandidate(s *store.Store, lib store.Library, item candidate, additionalParts []string, fallbackNFO string, result *Result, paths map[string]struct{}) error {
	paths[item.path] = struct{}{}
	images := imageutil.FindImages(filepath.Dir(item.path), func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
	entry, outcome := prepareCandidate(lib, item, additionalParts, fallbackNFO, images)
	if outcome.Failed == 0 {
		if err := s.SaveScannedMovies([]store.ScannedMovie{entry}); err != nil {
			return err
		}
	}
	result.add(outcome)
	return nil
}

func prepareCandidate(lib store.Library, item candidate, additionalParts []string, fallbackNFO string, images imageutil.ImagePaths) (store.ScannedMovie, Result) {
	result := Result{}
	movie := store.Movie{LibraryID: lib.ID, SourcePath: item.path, OutputDir: filepath.Dir(item.path), Status: "pending", AdditionalParts: additionalParts}
	line, err := ReadSource(item.path)
	if err != nil {
		result.Failed++
		return store.ScannedMovie{}, result
	}
	setSource(&movie, line)
	var actors []store.ActorRef
	if ValidHTTP(line) {
		nfoPath := strings.TrimSuffix(item.path, filepath.Ext(item.path)) + ".nfo"
		meta, nfoErr := nfo.Read(nfoPath)
		if nfoErr != nil && fallbackNFO != "" {
			nfoPath = fallbackNFO
			meta, nfoErr = nfo.Read(fallbackNFO)
		}
		var pathErr *os.PathError
		if errors.As(nfoErr, &pathErr) && !os.IsNotExist(nfoErr) {
			result.Failed++
			return store.ScannedMovie{}, result
		}
		if nfoErr == nil {
			applyMeta(&movie, meta, nfoPath)
			// NFO 的 <actor><thumb> 是头像真源：一并带进索引，删库重建后仍可恢复。
			for _, actor := range meta.Actors {
				actors = append(actors, store.ActorRef{Name: actor.Name, AvatarURL: strings.TrimSpace(actor.Thumb)})
			}
			// 图片与元数据同一趟写入，避免成功影片入库两次。
			movie.PosterPath = images.Poster
			movie.BackdropPath = images.Backdrop
			movie.LandscapePath = images.Landscape
			result.Success++
		} else {
			result.Pending++
		}
	} else {
		movie.Status = "incompatible"
		result.Incompatible++
	}
	return store.ScannedMovie{Movie: movie, Size: item.info.Size(), ModTime: item.info.ModTime(), Actors: actors}, result
}

func applyMeta(movie *store.Movie, meta nfo.MovieMeta, nfoPath string) {
	movie.Status, movie.NFOPath = "success", nfoPath
	movie.Number, movie.Title, movie.OriginalTitle, movie.Plot = meta.Number, meta.Title, meta.OriginalTitle, meta.Plot
	movie.Year, movie.Premiere, movie.Rating = meta.Year, firstNonEmpty(meta.Premiered, meta.ReleaseDate), meta.Rating
	movie.Director, movie.Series, movie.Maker, movie.Label = meta.Director, meta.Series, meta.Maker, meta.Label
	movie.Collection, movie.OfficialRating, movie.SortName = meta.Collection(), meta.Mpaa, meta.SortTitle
	movie.Taglines, movie.ProviderID = meta.TaglineList(), meta.ProviderID()
	movie.Genres, movie.Tags, movie.Studios, movie.RuntimeSeconds = meta.Genres, meta.Tags, meta.Studios, meta.RuntimeSeconds()
}

func parsePart(raw string) int {
	var value int
	for _, digit := range raw {
		value = value*10 + int(digit-'0')
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func setSource(movie *store.Movie, raw string) {
	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" {
		movie.SourceProtocol = strings.ToLower(u.Scheme)
		movie.SourceContainer = container(u.Path)
		return
	}
	if index := strings.Index(raw, "://"); index > 0 {
		movie.SourceProtocol = strings.ToLower(raw[:index])
	}
}

func container(path string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
}

func ReadSource(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadString('\n')
	if errors.Is(err, io.EOF) && strings.TrimSpace(line) != "" {
		err = nil
	}
	return strings.TrimSpace(line), err
}

func ValidHTTP(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// SourceStat 返回 strm 源文件的大小与修改时间；文件不可读（外部库被移动/删除）时
// 返回零值，让索引里的元数据仍可更新而不至于 panic。
func SourceStat(path string) (int64, time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}
	}
	return info.Size(), info.ModTime()
}
