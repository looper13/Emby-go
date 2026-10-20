package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"emby-go/internal/actorassets"
	"emby-go/internal/avatar"
	"emby-go/internal/store"
)

// personAvatars batches uncached reads. Admin inventory already has the actor
// rows; client pages supply nil to fetch only their cache misses in one batch.
func (a *App) personAvatars(ctx context.Context, names []string, known map[string]store.ActorRef) (map[string]actorassets.Resolved, error) {
	out := make(map[string]actorassets.Resolved, len(names))
	missing := make([]string, 0, len(names))
	version := a.db.ActorVersion()
	for _, name := range names {
		if raw, ok := a.imgMeta.Get("av:" + version + ":" + name); ok {
			path, tag, _ := strings.Cut(string(raw), "\n")
			out[name] = actorassets.Resolved{Path: path, Tag: tag}
		} else {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return out, ctx.Err()
	}
	if known == nil {
		var err error
		known, err = a.db.ActorAvatars(ctx, missing)
		if err != nil {
			return nil, err
		}
	}
	indexed := make([]string, 0, len(missing))
	for _, name := range missing {
		if known[name].Name != "" {
			indexed = append(indexed, name)
		}
	}
	resolved, _ := a.actorAssets().ResolveMany(ctx, indexed)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	legacyDir := ""
	var legacyFiles map[string]bool
	for _, name := range indexed {
		if resolved[name].Path != "" {
			continue
		}
		legacyDir = a.avatarsDir()
		entries, err := os.ReadDir(legacyDir)
		if err == nil || os.IsNotExist(err) {
			legacyFiles = make(map[string]bool, len(entries))
			for _, entry := range entries {
				legacyFiles[strings.ToLower(entry.Name())] = true
			}
		}
		break
	}
	for _, name := range missing {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value := resolved[name]
		actor := known[name]
		if value.Path == "" && actor.Name != "" {
			file := avatar.FileName(actor.Name)
			if legacyFiles == nil || legacyFiles[file] {
				path := filepath.Join(legacyDir, file)
				if info, err := os.Stat(path); err == nil && !info.IsDir() {
					tag := actor.AvatarTag
					if tag == "" {
						tag = a.posterTag(path)
					}
					value = actorassets.Resolved{Path: path, Tag: tag}
				}
			}
		}
		out[name] = value
		ttl := 5 * time.Minute
		if value.Path == "" {
			ttl = 30 * time.Second
		}
		a.imgMeta.Set("av:"+version+":"+name, []byte(value.Path+"\n"+value.Tag), ttl)
	}
	return out, nil
}

func (a *App) personImageTags(ctx context.Context, names []string) (map[string]string, error) {
	portraits, err := a.personAvatars(ctx, names, nil)
	if err != nil {
		return nil, err
	}
	tags := make(map[string]string, len(names))
	posters := make(map[string]string)
	missing := []string{}
	prefix := "ep:" + a.cacheScope(0) + ":Person:"
	for _, name := range names {
		if tag := portraits[name].Tag; tag != "" {
			tags[name] = tag
		} else if raw, ok := a.imgMeta.Get(prefix + name); ok {
			posters[name] = string(raw)
		} else {
			missing = append(missing, name)
		}
	}
	stored, err := a.db.PersonPosters(ctx, missing)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]string)
	for _, name := range missing {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		reference := stored[name]
		path, loaded := paths[reference]
		if !loaded {
			path = a.artReference(reference, false)
			paths[reference] = path
		}
		posters[name] = path
		a.imgMeta.Set(prefix+name, []byte(path), 5*time.Minute)
	}
	for name, path := range posters {
		if path != "" {
			tags[name] = a.posterTag(path)
		}
	}
	return tags, nil
}
