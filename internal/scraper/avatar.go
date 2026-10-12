package scraper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"emby-go/internal/actorassets"
	"emby-go/internal/avatar"
	"emby-go/internal/gfriends"
	"emby-go/internal/metatube"
	"emby-go/internal/nfo"
	"emby-go/internal/scanner"
	"emby-go/internal/store"
)

type AvatarResult struct {
	Actor     string
	Provider  string
	AvatarURL string
	Image     string
	Movies    int
	Status    string
	Reason    string
	Category  string // unmatched / ambiguous / unavailable / download_failed / partial
}
type avatarPrepared struct {
	result   AvatarResult
	manifest actorassets.Manifest
	bytes    []byte
	pending  *actorassets.Pending
	previous string
	existing bool
}

func (s *Scraper) assets() *actorassets.Store {
	dir := s.cfg.ActorsDir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(s.cfg.AvatarsDir), "actors")
	}
	return actorassets.New(dir)
}

// AvatarCandidates applies the limit after examining local files, tags and journals.
func (s *Scraper) AvatarCandidates(libraryID int64, limit int) ([]string, error) {
	if s.store == nil {
		return nil, errors.New("缺少演员存储")
	}
	names, err := s.store.ActorNames(libraryID)
	if err != nil {
		return nil, err
	}
	out := []string{}
	assets := s.assets()
	for _, name := range names {
		if _, err := assets.Pending(name); os.IsNotExist(err) {
			if _, tag, err := assets.Resolve(name); err == nil {
				a, err := s.store.ActorAvatar(name)
				if err == nil && a.AvatarTag == tag {
					continue
				}
			}
		}
		out = append(out, name)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
func (s *Scraper) RunAvatars(ctx context.Context, libraryID int64, limit int, onProgress func(Progress), onItem func(AvatarResult)) (Progress, error) {
	if !s.cfg.AvatarConfigured() {
		return Progress{}, errors.New("头像来源未配置")
	}
	names, err := s.AvatarCandidates(libraryID, limit)
	if err != nil {
		return Progress{}, err
	}
	return s.RunAvatarsFor(ctx, names, onProgress, onItem)
}
func (s *Scraper) RunAvatarsFor(ctx context.Context, names []string, onProgress func(Progress), onItem func(AvatarResult)) (Progress, error) {
	if !s.cfg.AvatarConfigured() {
		return Progress{}, errors.New("头像来源未配置")
	}
	return s.runActorAssets(ctx, names, false, onProgress, onItem)
}
func (s *Scraper) runActorAssets(ctx context.Context, names []string, importOnly bool, onProgress func(Progress), onItem func(AvatarResult)) (Progress, error) {
	if s.store == nil {
		return Progress{}, errors.New("缺少演员存储")
	}
	release, err := s.assets().Acquire(ctx)
	if err != nil {
		return Progress{}, err
	}
	defer release()
	return s.runActorAssetsLocked(ctx, names, importOnly, onProgress, onItem)
}

func (s *Scraper) runActorAssetsLocked(ctx context.Context, names []string, importOnly bool, onProgress func(Progress), onItem func(AvatarResult)) (Progress, error) {
	all, err := s.store.ActorNames(0)
	if err != nil {
		return Progress{}, err
	}
	collisions := legacyCollisions(all)
	seen := map[string]bool{}
	unique := []string{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" && !seen[n] {
			seen[n] = true
			unique = append(unique, n)
		}
	}
	progress := Progress{Total: len(unique)}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := gfriends.New(s.assets().Dir, s.cfg.GFriends, s.cfg.Timeout())
	var once sync.Once
	var index *gfriends.Index
	var indexErr error
	getIndex := func() (*gfriends.Index, error) {
		once.Do(func() { index, _, indexErr = client.Snapshot(workCtx, false) })
		return index, indexErr
	}
	jobs := make(chan string)
	ready := make(chan avatarPrepared, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				if workCtx.Err() != nil {
					return
				}
				p := s.prepareAvatar(workCtx, name, collisions[avatar.FileName(name)], importOnly, client, getIndex)
				select {
				case ready <- p:
				case <-workCtx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, name := range unique {
			select {
			case jobs <- name:
			case <-workCtx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(ready) }()
	aborted := false
	// This is the only publication/NFO/SQLite writer in the task.
	for prepared := range ready {
		if workCtx.Err() != nil {
			continue
		}
		result := prepared.result
		if result.Status == "" {
			result = s.commitAvatar(workCtx, prepared, importOnly)
		}
		progress.Done++
		progress.Current = result.Actor
		switch result.Status {
		case StatusSuccess:
			progress.Success++
			progress.Consecutive = 0
		case StatusSkipped:
			progress.Skipped++
			progress.Consecutive = 0
		default:
			progress.Failed++
			progress.Consecutive++
		}
		switch result.Category {
		case "unmatched":
			progress.Unmatched++
		case "ambiguous":
			progress.Ambiguous++
		case "unavailable":
			progress.Unavailable++
		case "download_failed":
			progress.DownloadFailed++
		case "partial":
			progress.Partial++
		}
		if onItem != nil {
			onItem(result)
		}
		if onProgress != nil {
			onProgress(progress)
		}
		if progress.Consecutive >= MaxConsecutiveFailures {
			aborted = true
			cancel()
		}
	}
	if ctx.Err() != nil {
		return progress, ctx.Err()
	}
	if aborted {
		return progress, errors.New("连续头像处理失败，任务已中止")
	}
	if progress.Failed > 0 {
		return progress, fmt.Errorf("%d 个演员处理失败，其中 %d 个部分完成，可再次运行恢复", progress.Failed, progress.Partial)
	}
	return progress, nil
}
func legacyCollisions(names []string) map[string]bool {
	counts := map[string]int{}
	for _, n := range names {
		counts[avatar.FileName(n)]++
	}
	out := map[string]bool{}
	for k, v := range counts {
		out[k] = v > 1
	}
	return out
}
func preparedFailure(p avatarPrepared, category, reason string, skip bool) avatarPrepared {
	p.result.Status = StatusFailed
	if skip {
		p.result.Status = StatusSkipped
	}
	p.result.Category = category
	p.result.Reason = reason
	return p
}
func (s *Scraper) prepareAvatar(ctx context.Context, name string, collision, importOnly bool, client *gfriends.Client, getIndex func() (*gfriends.Index, error)) avatarPrepared {
	p := avatarPrepared{result: AvatarResult{Actor: name}}
	assets := s.assets()
	if pending, err := assets.Pending(name); err == nil {
		if importOnly && pending.Manifest.Avatar.Origin != "legacy_local" {
			return preparedFailure(p, "partial", "存在网络头像待恢复，请先运行头像任务", true)
		}
		p.pending = pending
		p.manifest = pending.Manifest
		return p
	} else if !os.IsNotExist(err) {
		return preparedFailure(p, "partial", err.Error(), false)
	}
	if _, _, err := assets.Resolve(name); err == nil {
		p.manifest, _ = assets.Load(name)
		p.existing = true
		return p
	}
	if m, err := assets.Load(name); err == nil && m.Avatar != nil {
		p.previous = m.Avatar.SHA256
		if importOnly {
			return preparedFailure(p, "partial", "已有清单但图片缺失，请运行头像修复", false)
		}
		if m.Avatar.SourceURL == "" {
			return preparedFailure(p, "partial", "本地资产损坏且没有可恢复的来源地址", false)
		}
		data, err := client.Download(ctx, m.Avatar.SourceURL)
		if err != nil {
			return preparedFailure(p, "download_failed", err.Error(), false)
		}
		p.manifest, p.bytes, err = actorassets.Prepare(name, data, *m.Avatar, m.Aliases)
		if err != nil {
			return preparedFailure(p, "download_failed", err.Error(), false)
		}
		p.manifest.CreatedAt = m.CreatedAt
		p.manifest.NFOConflicts = m.NFOConflicts
		return p
	} else if err != nil && !os.IsNotExist(err) {
		return preparedFailure(p, "partial", err.Error(), false)
	}
	legacy, err := actorassets.ReadBounded(s.cfg.AvatarsDir, avatar.FileName(name), actorassets.MaxImageBytes)
	if err == nil {
		if collision {
			return preparedFailure(p, "ambiguous", "多个演员共享旧头像文件名，需人工确认", true)
		}
		p.manifest, p.bytes, err = actorassets.Prepare(name, legacy, actorassets.Avatar{Origin: "legacy_local"}, nil)
		if err != nil {
			return preparedFailure(p, "download_failed", "旧头像损坏: "+err.Error(), false)
		}
		return p
	}
	if !os.IsNotExist(err) {
		return preparedFailure(p, "download_failed", "读取旧头像失败: "+err.Error(), false)
	}
	if importOnly {
		return preparedFailure(p, "unmatched", "没有旧头像", true)
	}
	// Read current NFOs; a cached URL cannot overrule conflicting film selections.
	urls, err := s.actorThumbs(name)
	if err != nil {
		return preparedFailure(p, "partial", err.Error(), false)
	}
	if len(urls) > 1 {
		return preparedFailure(p, "ambiguous", "影片 NFO 中已有不同头像地址，保留原选择", true)
	}
	if len(urls) == 1 {
		for raw := range urls {
			data, err := client.Download(ctx, raw)
			if err != nil {
				return preparedFailure(p, "download_failed", err.Error(), false)
			}
			p.manifest, p.bytes, err = actorassets.Prepare(name, data, actorassets.Avatar{Origin: "nfo", SourceURL: raw}, nil)
			if err != nil {
				return preparedFailure(p, "download_failed", err.Error(), false)
			}
			return p
		}
	}
	if s.cfg.AvatarMode() != "metatube" {
		idx, err := getIndex()
		reason := "unavailable"
		if err == nil {
			list, status := idx.Match(name)
			reason = status
			if status == "ambiguous" {
				return preparedFailure(p, status, "GFriends 姓名指向多个人物，已跳过", true)
			}
			if status == "matched" {
				var last error
				for _, candidate := range list[:min(3, len(list))] {
					raw, e := gfriends.ImageURL(s.cfg.GFriends.Defaults().BaseURL, candidate)
					if e != nil {
						last = e
						continue
					}
					data, e := client.Download(ctx, raw)
					if e != nil {
						last = e
						continue
					}
					aliases := sourcedAliases(name, "gfriends/"+candidate.Group, candidate.Name, candidate.TargetName)
					p.manifest, p.bytes, e = actorassets.Prepare(name, data, actorassets.Avatar{Origin: "gfriends", SourceURL: raw, Group: candidate.Group, SourceFile: candidate.File, IndexVersion: idx.Version, IndexTimestamp: idx.Timestamp.String()}, aliases)
					if e == nil {
						return p
					}
					last = e
				}
				return preparedFailure(p, "download_failed", fmt.Sprint(last), false)
			}
		}
		if s.cfg.AvatarMode() != "gfriends_then_metatube" || !s.cfg.Configured() {
			message := "GFriends 无精确匹配"
			if err != nil {
				message = err.Error()
			}
			return preparedFailure(p, reason, message, reason == "unmatched")
		}
	}
	results, err := s.client.SearchActors(ctx, name, "", true)
	if err != nil {
		return preparedFailure(p, "unavailable", err.Error(), false)
	}
	match, ok := matchActor(results, name)
	if !ok {
		return preparedFailure(p, "unmatched", "MetaTube 无唯一姓名/别名匹配", true)
	}
	if len(match.Images) == 0 {
		return preparedFailure(p, "unmatched", "演员没有图片", true)
	}
	data, err := client.Download(ctx, match.Images[0])
	if err != nil {
		return preparedFailure(p, "download_failed", err.Error(), false)
	}
	aliases := sourcedAliases(name, "metatube/"+match.Provider, append([]string{match.Name}, match.Aliases...)...)
	p.manifest, p.bytes, err = actorassets.Prepare(name, data, actorassets.Avatar{Origin: "metatube", SourceURL: match.Images[0]}, aliases)
	if err != nil {
		return preparedFailure(p, "download_failed", err.Error(), false)
	}
	return p
}

// Only the uniquely matched provider record supplies aliases. Do not join other
// local actors or collect names from unrelated index entries.
func sourcedAliases(name, source string, names ...string) []actorassets.Alias {
	seen := map[string]bool{strings.TrimSpace(name): true}
	var aliases []actorassets.Alias
	for _, alias := range names {
		alias = strings.TrimSpace(alias)
		if alias != "" && !seen[alias] {
			aliases = append(aliases, actorassets.Alias{Name: alias, Source: source})
			seen[alias] = true
		}
	}
	return aliases
}

func (s *Scraper) actorThumbs(name string) (map[string]bool, error) {
	urls := map[string]bool{}
	movies, err := s.store.MoviesByActor(name)
	if err != nil {
		return nil, err
	}
	for _, movie := range movies {
		if err := s.validateActorMovie(movie); err != nil {
			return nil, err
		}
		meta, err := nfo.Read(movie.NFOPath)
		if err != nil {
			return nil, err
		}
		for _, a := range meta.Actors {
			if strings.TrimSpace(a.Name) == name && strings.TrimSpace(a.Thumb) != "" {
				urls[strings.TrimSpace(a.Thumb)] = true
			}
		}
	}
	return urls, nil
}
func (s *Scraper) validateActorMovie(movie store.Movie) error {
	lib, err := s.store.Library(movie.LibraryID)
	if err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(lib.Path)
	if err != nil {
		return err
	}
	for _, p := range []string{movie.SourcePath, movie.NFOPath} {
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || !filepath.IsLocal(rel) {
			return errors.New("影片路径不在当前媒体库内")
		}
	}
	return nil
}
func (s *Scraper) commitAvatar(ctx context.Context, p avatarPrepared, importOnly bool) AvatarResult {
	r := p.result
	assets := s.assets()
	fail := func(err error) AvatarResult {
		r.Status = StatusFailed
		r.Category = "partial"
		r.Reason = err.Error()
		return r
	}
	actor, err := s.store.ActorAvatar(r.Actor)
	if err != nil {
		return fail(err)
	}
	if actor.Name == "" {
		r.Status = StatusSkipped
		r.Reason = "演员已不在当前索引中"
		return r
	}
	if p.existing {
		_, tag, err := assets.Resolve(r.Actor)
		if err != nil {
			return fail(err)
		}
		if actor.AvatarTag != tag {
			if err := s.store.SetActorAvatar(r.Actor, "", tag); err != nil {
				return fail(err)
			}
			r.Status = StatusSuccess
			return r
		}
		r.Status = StatusSkipped
		r.Reason = "已有完整头像，已保留"
		return r
	}
	if p.pending == nil {
		pending, err := assets.Stage(p.manifest, p.bytes, p.previous)
		if err != nil {
			return fail(err)
		}
		p.pending = &pending
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := assets.Publish(p.pending); err != nil {
		return fail(err)
	}
	path, tag, err := assets.Resolve(r.Actor)
	if err != nil {
		return fail(err)
	}
	r.Image = path
	r.AvatarURL = p.manifest.Avatar.SourceURL
	r.Provider = p.manifest.Avatar.Origin
	// Imported images have no remote provenance and never rewrite film NFO.
	if !importOnly && p.manifest.Avatar.Origin != "legacy_local" && r.AvatarURL != "" {
		p.pending.Stage = "pending_nfo"
		if err := assets.SavePending(p.pending); err != nil {
			return fail(err)
		}
		movies, err := s.store.MoviesByActor(r.Actor)
		if err != nil {
			return fail(err)
		}
		p.pending.RemainingSources = nil
		for _, movie := range movies {
			p.pending.RemainingSources = append(p.pending.RemainingSources, actorassets.Hash([]byte(movie.SourcePath)))
		}
		if err := assets.SavePending(p.pending); err != nil {
			return fail(err)
		}
		var failures []error
		var conflicts []actorassets.ThumbConflict
		for _, movie := range movies {
			if ctx.Err() != nil {
				failures = append(failures, ctx.Err())
				break
			}
			if err := s.validateActorMovie(movie); err != nil {
				failures = append(failures, err)
				continue
			}
			meta, err := nfo.Read(movie.NFOPath)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			present := false
			for _, a := range meta.Actors {
				if strings.TrimSpace(a.Name) == r.Actor {
					present = true
					if strings.TrimSpace(a.Thumb) != "" && strings.TrimSpace(a.Thumb) != r.AvatarURL {
						conflicts = append(conflicts, actorassets.ThumbConflict{SourceKey: actorassets.Hash([]byte(movie.SourcePath)), URL: actorassets.SafeURL(a.Thumb)})
					}
				}
			}
			if !present {
				completeSource(p.pending, movie.SourcePath)
				continue
			}
			_, err = nfo.FillActorThumb(movie.NFOPath, r.Actor, r.AvatarURL)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			// Re-read even an unchanged NFO: a previous run may have stopped after writing it.
			if err := s.rescan(movie); err != nil {
				failures = append(failures, err)
				continue
			}
			r.Movies++
			completeSource(p.pending, movie.SourcePath)
			if err := assets.SavePending(p.pending); err != nil {
				failures = append(failures, err)
				break
			}
		}
		if err := assets.RecordConflicts(p.pending, conflicts); err != nil {
			failures = append(failures, err)
		}
		if err := errors.Join(failures...); err != nil {
			_ = s.store.SetActorAvatar(r.Actor, "", tag)
			return fail(err)
		}
		p.pending.Stage = "pending_rescan"
		if err := assets.SavePending(p.pending); err != nil {
			return fail(err)
		}
	}
	if err := s.store.SetActorAvatar(r.Actor, "", tag); err != nil {
		return fail(err)
	}
	p.pending.Stage = "complete"
	if err := assets.SavePending(p.pending); err != nil {
		return fail(err)
	}
	if err := assets.Complete(r.Actor); err != nil {
		return fail(err)
	}
	r.Status = StatusSuccess
	return r
}

func completeSource(p *actorassets.Pending, path string) {
	key := actorassets.Hash([]byte(path))
	remaining := p.RemainingSources[:0]
	for _, value := range p.RemainingSources {
		if value != key {
			remaining = append(remaining, value)
		}
	}
	p.RemainingSources = remaining
}
func (s *Scraper) rescan(movie store.Movie) error {
	library, err := s.store.Library(movie.LibraryID)
	if err != nil {
		return err
	}
	result, err := scanner.RescanOne(s.store, library, movie.SourcePath)
	if err == nil && result.Failed > 0 {
		return errors.New("头像写入后重扫失败")
	}
	return err
}
func foldName(raw string) string { return gfriends.Fold(raw) }
func matchActor(results []metatube.ActorSearchResult, query string) (metatube.ActorSearchResult, bool) {
	var found metatube.ActorSearchResult
	count := 0
	for _, item := range results {
		matched := foldName(item.Name) == foldName(query)
		for _, alias := range item.Aliases {
			matched = matched || foldName(alias) == foldName(query)
		}
		if matched {
			found = item
			count++
		}
	}
	return found, count == 1
}
