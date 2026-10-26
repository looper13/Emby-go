package scanner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"emby-go/internal/nfo"
	"emby-go/internal/store"
)

type Result struct {
	Success      int       `json:"success"`
	Pending      int       `json:"pending"`
	Incompatible int       `json:"incompatible"`
	Failed       int       `json:"failed"`
	Added        int       `json:"added"`
	Updated      int       `json:"updated"`
	Skipped      int       `json:"skipped"`
	Deleted      int       `json:"deleted"`
	Failures     []Failure `json:"failures,omitempty"`
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
	result.addFailures(other.Failures)
}

type candidate struct {
	path     string
	base     string
	part     int
	groupKey string
}

// 扫描阶段。walk 阶段还没数完候选（Total 是已发现数），process 阶段的
// Total 才是本轮要处理的候选总数——管理端据此区分「在遍历」和「在处理」。
const (
	PhaseWalk    = "walk"
	PhaseProcess = "process"
)

// Progress 单次媒体库扫描的进度快照，供管理端轮询展示。
type Progress struct {
	LibraryID   int64  `json:"library_id"`
	LibraryName string `json:"library_name"`
	Phase       string `json:"phase,omitempty"`
	Total       int    `json:"total"`
	Done        int    `json:"done"`
	Current     string `json:"current"`
	Result      Result `json:"result"`
}

// walkProgressInterval 遍历阶段的进度回报间隔：大库首次遍历可能持续数分钟，
// 没有进度时管理端只能显示「0/0」，也不知道扫描是否还在跑。
const walkProgressInterval = 500 * time.Millisecond

var cdPartPattern = regexp.MustCompile(`(?i)^(.*?)[ ._-]+CD([1-9][0-9]*)$`)

// Scan 按 Emby 的 stacking 语义处理末尾 -CDn 文件：CD1 为逻辑影片，CD2..CDn 作为 AdditionalParts。
// ctx 取消时返回已完成的统计与取消错误：已入库的分批保留，删除阶段不执行。
func Scan(ctx context.Context, s *store.Store, lib store.Library) (Result, error) {
	return ScanWithProgress(ctx, s, lib, nil)
}

// ScanWithProgress 与 Scan 相同，但在遍历与处理过程中回调进度（可为 nil）。
func ScanWithProgress(ctx context.Context, s *store.Store, lib store.Library, onProgress func(Progress)) (Result, error) {
	return scanWithProgress(ctx, s, lib, false, onProgress)
}

func RebuildWithProgress(ctx context.Context, s *store.Store, lib store.Library, onProgress func(Progress)) (Result, error) {
	return scanWithProgress(ctx, s, lib, true, onProgress)
}

func scanWithProgress(ctx context.Context, s *store.Store, lib store.Library, full bool, onProgress func(Progress)) (result Result, scanErr error) {
	return scanDirectory(ctx, s, lib, lib.Path, true, full, nil, onProgress)
}

func RefreshDirectory(ctx context.Context, s *store.Store, lib store.Library, directory string, recursive bool, onProgress func(Progress), forceMetadata ...bool) (Result, error) {
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
	return scanDirectory(ctx, s, lib, filepath.Join(lib.Path, relative), recursive, len(forceMetadata) > 0 && forceMetadata[0], nil, onProgress)
}

// RefreshFiles explicitly rereads metadata, including NFO events preserving mtime.
func RefreshFiles(ctx context.Context, s *store.Store, lib store.Library, files []string, onProgress func(Progress)) (Result, error) {
	return refreshFiles(ctx, s, lib, files, true, onProgress)
}

// ReconcileFiles checks metadata and source membership without forcing NFO reads.
func ReconcileFiles(ctx context.Context, s *store.Store, lib store.Library, files []string, onProgress func(Progress)) (Result, error) {
	return refreshFiles(ctx, s, lib, files, false, onProgress)
}

func refreshFiles(ctx context.Context, s *store.Store, lib store.Library, files []string, full bool, onProgress func(Progress)) (Result, error) {
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
	return scanDirectory(ctx, s, lib, directory, false, full, normalized, onProgress)
}

func scanDirectory(ctx context.Context, s *store.Store, lib store.Library, directory string, recursive, full bool, files []string, onProgress func(Progress)) (result Result, scanErr error) {
	if !lib.IsDirectory() {
		return result, store.ErrLibraryCapability
	}
	perf := newScanPerformance(lib, directory, recursive, full)
	defer func() { perf.finish(result, scanErr) }()
	walkStarted := time.Now()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	rootInfo, err := os.Stat(lib.Path)
	perf.walk = time.Since(walkStarted)
	if err != nil {
		return result, err
	}
	if !rootInfo.IsDir() {
		return result, fmt.Errorf("媒体库路径不是目录: %s", lib.Path)
	}
	groups := make(map[string][]candidate)
	keys := affectedGroups(files)
	if len(files) > 0 && len(keys) == 0 {
		return result, nil
	}
	targeted := len(files) > 0
	lastWalkReport := time.Now()
	reportWalk := func(current string) {
		if onProgress != nil {
			onProgress(Progress{LibraryID: lib.ID, LibraryName: lib.Name, Phase: PhaseWalk, Total: len(groups), Current: current, Result: result})
		}
	}
	// 遍历一开始先报一次：管理端立刻切到「正在遍历目录」，不用等第一个 500ms 周期。
	reportWalk(directory)
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if time.Since(lastWalkReport) >= walkProgressInterval {
			lastWalkReport = time.Now()
			perf.walk = time.Since(walkStarted)
			perf.candidates = len(groups)
			perf.report(PhaseWalk, result)
			reportWalk(path)
		}
		if walkErr != nil {
			if path == directory && filepath.Clean(directory) != filepath.Clean(lib.Path) && os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if path == directory && !entry.IsDir() {
			return fmt.Errorf("媒体库根目录无法遍历: %s", path)
		}
		if entry.IsDir() && path != directory && (!recursive || strings.EqualFold(entry.Name(), "extrafanart")) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".strm") {
			return nil
		}
		if targeted && !keys[groupKey(path)] {
			return nil
		}
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		candidate := candidate{path: path, base: base, groupKey: groupKey(path)}
		if match := cdPartPattern.FindStringSubmatch(base); match != nil {
			candidate.base = match[1]
			candidate.part = parsePart(match[2])
			candidate.groupKey = filepath.Join(filepath.Dir(path), strings.ToLower(match[1]))
		}
		groups[candidate.groupKey] = append(groups[candidate.groupKey], candidate)
		return nil
	})
	perf.walk = time.Since(walkStarted)
	perf.candidates = len(groups)
	if err != nil {
		return result, err
	}
	var fingerprints map[string]store.ScannedSource
	perf.phase("index")
	stageStarted := time.Now()
	if targeted {
		prefixes := make([]string, 0, len(keys))
		for key := range keys {
			prefixes = append(prefixes, strings.TrimSuffix(key, ".strm"))
		}
		fingerprints, err = s.SourcePrefixScanSources(lib.ID, filepath.Clean(directory), prefixes)
	} else if recursive && filepath.Clean(directory) == filepath.Clean(lib.Path) {
		fingerprints, err = s.ScanSources(lib.ID)
	} else {
		fingerprints, err = s.DirectoryScanSources(lib.ID, filepath.Clean(directory), recursive)
	}
	perf.index = time.Since(stageStarted)
	if err != nil {
		return result, err
	}
	if targeted {
		for path := range fingerprints {
			if !keys[groupKey(path)] {
				delete(fingerprints, path)
			}
		}
	}
	defer func() {
		if result.Added+result.Updated+result.Deleted > 0 || scanErr != nil {
			started := time.Now()
			scanErr = errors.Join(scanErr, s.BumpVersion(lib.ID))
			if result.Added+result.Updated+result.Deleted > 0 {
				s.OptimizeSearchIndexes()
			}
			perf.version += time.Since(started)
		}
	}()

	if err := ctx.Err(); err != nil {
		return result, err
	}

	total := len(groups)
	perf.phase(PhaseProcess)
	report := func(done int, current string) {
		if onProgress != nil {
			onProgress(Progress{LibraryID: lib.ID, LibraryName: lib.Name, Phase: PhaseProcess, Total: total, Done: done, Current: current, Result: result})
		}
	}
	report(0, "")

	paths := make(map[string]struct{})
	const batchSize = 100
	batch := make([]store.ScannedMovie, 0, batchSize)
	batchResult := Result{}
	lastFlush := time.Now()
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		stats, err := s.SaveScannedMoviesWithStats(batch)
		perf.addWrite(stats, len(batch), err)
		if err != nil {
			return err
		}
		result.add(batchResult)
		clear(batch)
		batch = batch[:0]
		batchResult = Result{}
		lastFlush = time.Now()
		return nil
	}
	type scanTask struct {
		index       int
		groupKey    string
		item        candidate
		parts       []string
		fallbackNFO string
	}
	type scanTaskResult struct {
		task        scanTask
		entry       store.ScannedMovie
		outcome     Result
		hasEntry    bool
		sourceTime  time.Duration
		prepareTime time.Duration
		verifyTime  time.Duration
		prepStats   preparationStats
	}
	processTask := func(task scanTask) scanTaskResult {
		res := scanTaskResult{task: task}
		started := time.Now()
		before, err := readLibraryMetadataState(lib, task.item.path, task.fallbackNFO)
		res.sourceTime = time.Since(started)
		if err != nil {
			res.outcome = failedCandidate(lib, task.item.path, "", "读取文件状态", err)
			return res
		}
		previous, exists := fingerprints[task.item.path]
		if !full && previous.Fingerprint == before.Fingerprint && slices.Equal(previous.AdditionalParts, task.parts) {
			res.outcome.Skipped++
			return res
		}
		started = time.Now()
		entry, outcome := prepareCandidate(lib, task.item, task.parts, task.fallbackNFO, &res.prepStats)
		res.prepareTime = time.Since(started)
		if outcome.Failed != 0 {
			res.outcome = outcome
			return res
		}
		if exists {
			outcome.Updated++
		} else {
			outcome.Added++
		}
		started = time.Now()
		if after, err := readLibraryMetadataState(lib, task.item.path, task.fallbackNFO); err == nil && after.Fingerprint == before.Fingerprint {
			entry.Fingerprint = before.Fingerprint
		}
		res.verifyTime = time.Since(started)
		res.hasEntry = true
		res.entry = entry
		res.outcome = outcome
		return res
	}
	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	tasks := make([]scanTask, 0, len(groups))
	groupPending := make(map[string]int, len(groups))
	for _, key := range groupKeys {
		group := groups[key]
		primary, parts, ok := stackedGroup(group)
		if !ok {
			for _, item := range group {
				tasks = append(tasks, scanTask{groupKey: key, item: item})
				groupPending[key]++
			}
		} else {
			partPaths := make([]string, 0, len(parts))
			for _, part := range parts {
				partPaths = append(partPaths, part.path)
			}
			fallbackNFO := filepath.Join(filepath.Dir(primary.path), primary.base+".nfo")
			tasks = append(tasks, scanTask{groupKey: key, item: primary, parts: partPaths, fallbackNFO: fallbackNFO})
			groupPending[key]++
		}
	}
	done := 0
	handleResult := func(res scanTaskResult) error {
		paths[res.task.item.path] = struct{}{}
		perf.source += res.sourceTime
		perf.prepare += res.prepareTime
		perf.verify += res.verifyTime
		perf.preparation.addNFO(res.prepStats.nfo)
		perf.preparation.metadata += res.prepStats.metadata
		perf.processed++
		perf.report(PhaseProcess, result)

		if res.hasEntry {
			batch = append(batch, res.entry)
			batchResult.add(res.outcome)
			if len(batch) >= batchSize || time.Since(lastFlush) >= 500*time.Millisecond {
				if err := flush(); err != nil {
					return err
				}
			}
		} else {
			result.add(res.outcome)
		}

		groupPending[res.task.groupKey]--
		if groupPending[res.task.groupKey] <= 0 {
			done++
			report(done, res.task.groupKey)
		}
		return nil
	}
	if len(tasks) <= 1 {
		for _, task := range tasks {
			if ctxErr := ctx.Err(); ctxErr != nil {
				flushErr := flush()
				return result, errors.Join(ctxErr, flushErr)
			}
			if err := handleResult(processTask(task)); err != nil {
				return result, err
			}
		}
	} else {
		workers := min(8, max(2, runtime.GOMAXPROCS(0)))
		if workers > len(tasks) {
			workers = len(tasks)
		}
		taskCh := make(chan scanTask, workers*2)
		resCh := make(chan scanTaskResult, workers*2)
		// Bound prepared results while an earlier, slower task is outstanding.
		slots := make(chan struct{}, workers*2)
		workerCtx, stopWorkers := context.WithCancel(ctx)
		// A failed batch must also stop producers and workers. Drain until all
		// goroutines exit before returning or releasing the scan's ownership.
		defer func() {
			stopWorkers()
			for range resCh {
			}
		}()

		var wg sync.WaitGroup
		wg.Add(workers + 1)
		go func() {
			defer wg.Done()
			defer close(taskCh)
			for index, task := range tasks {
				select {
				case slots <- struct{}{}:
				case <-workerCtx.Done():
					return
				}
				task.index = index
				select {
				case taskCh <- task:
				case <-workerCtx.Done():
					return
				}
			}
		}()

		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				for task := range taskCh {
					if workerCtx.Err() != nil {
						return
					}
					res := processTask(task)
					select {
					case resCh <- res:
					case <-workerCtx.Done():
						return
					}
				}
			}()
		}
		go func() {
			wg.Wait()
			close(resCh)
		}()

		pending := make(map[int]scanTaskResult, workers*2)
		next := 0
		for res := range resCh {
			pending[res.task.index] = res
			for {
				if ctxErr := ctx.Err(); ctxErr != nil {
					flushErr := flush()
					return result, errors.Join(ctxErr, flushErr)
				}
				res, ready := pending[next]
				if !ready {
					break
				}
				if err := handleResult(res); err != nil {
					return result, err
				}
				delete(pending, next)
				next++
				<-slots
			}
		}
	}
	if err := flush(); err != nil {
		return result, err
	}
	// 遍历已经完整，但取消信号可能刚好落在删除阶段之前；此时按索引缺失删除
	// 仍然是安全的，只是没必要在停机/取消路径上继续写库，故直接返回。
	if err := ctx.Err(); err != nil {
		return result, err
	}

	if result.Failed == 0 {
		perf.phase("cleanup")
		started := time.Now()
		var missing []string
		for path := range fingerprints {
			if _, exists := paths[path]; !exists {
				missing = append(missing, path)
			}
		}
		result.Deleted, err = s.DeleteScannedSources(lib.ID, missing)
		perf.cleanup += time.Since(started)
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
	if !lib.IsDirectory() {
		return Result{}, store.ErrLibraryCapability
	}
	var result Result
	info, err := os.Stat(strmPath)
	if err != nil {
		return result, err
	}
	if info.IsDir() || !strings.EqualFold(filepath.Ext(strmPath), ".strm") {
		return result, errors.New("不是 .strm 文件: " + strmPath)
	}

	base := strings.TrimSuffix(filepath.Base(strmPath), filepath.Ext(strmPath))
	candidate := candidate{path: strmPath, base: base, groupKey: strmPath}
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
		group = append(group, candidate{path: path, base: match[1],
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
	entry, outcome := prepareCandidate(lib, item, additionalParts, fallbackNFO, nil)
	if outcome.Failed == 0 {
		if err := s.SaveScannedMovies([]store.ScannedMovie{entry}); err != nil {
			return err
		}
	}
	result.add(outcome)
	return nil
}

func prepareCandidate(lib store.Library, item candidate, additionalParts []string, fallbackNFO string, timing *preparationStats) (store.ScannedMovie, Result) {
	result := Result{}
	movie := store.Movie{LibraryID: lib.ID, SourcePath: item.path, OutputDir: filepath.Dir(item.path), Status: "pending", AdditionalParts: additionalParts}
	var actors []store.ActorRef
	nfoPath := strings.TrimSuffix(item.path, filepath.Ext(item.path)) + ".nfo"
	readNFO := func(path string) (nfo.MovieMeta, error) {
		var meta nfo.MovieMeta
		var err error
		if timing == nil {
			meta, err = nfo.Read(path)
		} else {
			var stats nfo.ReadStats
			meta, stats, err = nfo.ReadWithStats(path)
			timing.addNFO(stats)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			err = &fs.PathError{Op: "读取或解析 NFO", Path: path, Err: err}
		}
		return meta, err
	}
	meta, nfoErr := readNFO(nfoPath)
	if nfoErr != nil && fallbackNFO != "" {
		nfoPath = fallbackNFO
		meta, nfoErr = readNFO(fallbackNFO)
	}
	switch {
	case nfoErr == nil:
		metadataStarted := time.Now()
		applyMeta(&movie, meta, nfoPath)
		if err := applyTelevision(&movie, meta, lib, readNFO); err != nil {
			return store.ScannedMovie{}, failedCandidate(lib, item.path, nfoPath, "校验媒体元数据", err)
		}
		// NFO 的 <actor><thumb> 是头像真源：一并带进索引，删库重建后仍可恢复。
		for _, actor := range meta.Actors {
			actors = append(actors, store.ActorRef{Name: actor.Name, AvatarURL: strings.TrimSpace(actor.Thumb)})
		}
		if timing != nil {
			timing.metadata += time.Since(metadataStarted)
		}
		result.Success++
	case errors.Is(nfoErr, fs.ErrNotExist):
		// 没有 NFO 文件＝待补录：保留 pending 状态等刮削补齐标题等元数据。
		result.Pending++
	default:
		// NFO 存在但读不了或 XML 非法：不能当成「待补录」——那会用空标题覆盖
		// 已经入库的好元数据，并写入本轮指纹让后续扫描直接跳过（影片就此隐没）。
		// 这里保留旧索引、记下具体错误，且不写指纹，下一轮仍会重试；
		// NFO 被修好（刮削重写）后自然恢复为 success。
		return store.ScannedMovie{}, failedCandidate(lib, item.path, nfoPath, "读取或解析 NFO", nfoErr)
	}
	return store.ScannedMovie{Movie: movie, Actors: actors}, result
}

func applyMeta(movie *store.Movie, meta nfo.MovieMeta, nfoPath string) {
	movie.Status, movie.NFOPath = "success", nfoPath
	movie.Number, movie.Title, movie.OriginalTitle, movie.Plot = meta.Number, meta.Title, meta.OriginalTitle, meta.Plot
	movie.Year, movie.Premiere, movie.Rating = meta.Year, firstNonEmpty(meta.Premiered, meta.ReleaseDate), meta.Rating
	movie.Director, movie.Series, movie.Maker, movie.Label = meta.Director, meta.Series, meta.Maker, meta.Label
	movie.Collection, movie.OfficialRating, movie.SortName = meta.Collection(), meta.Mpaa, meta.SortTitle
	movie.Taglines, movie.ProviderID = meta.TaglineList(), meta.ProviderID()
	movie.Genres, movie.Tags, movie.Studios, movie.RuntimeSeconds = meta.Genres, meta.Tags, meta.Studios, meta.RuntimeSeconds()
	movie.TrailerURL, movie.CoverURL = meta.TrailerURL(), meta.CoverURL()
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
