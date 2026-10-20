package actorassets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

type Resolved struct {
	Path string
	Tag  string
}

// ResolveMany applies the same identity, containment and image validation as
// Resolve. A shard is listed once so missing portraits do not each open a
// manifest. Invalid individual assets are omitted, allowing legacy fallback.
func (s *Store) ResolveMany(ctx context.Context, names []string) (map[string]Resolved, error) {
	out := make(map[string]Resolved)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return out, nil
	}
	r, err := os.OpenRoot(s.Dir)
	if err != nil {
		return out, err
	}
	defer r.Close()
	if err := validateLayout(r); err != nil {
		return out, err
	}
	shards := make(map[string]map[string]bool)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := Key(name)
		shard := key[:2]
		entries, loaded := shards[shard]
		if !loaded {
			entries = make(map[string]bool)
			f, err := r.Open(filepath.Join("people", shard))
			if err == nil {
				children, readErr := f.Readdirnames(-1)
				f.Close()
				if readErr == nil {
					for _, child := range children {
						entries[strings.ToLower(child)] = true
					}
				} else {
					entries = nil
				}
			} else if !os.IsNotExist(err) {
				// A directory may allow opening a known file without listing it.
				entries = nil
			}
			shards[shard] = entries
		}
		if entries != nil && !entries[key] {
			continue
		}
		path, tag, err := s.resolve(r, name)
		if err == nil {
			out[name] = Resolved{Path: path, Tag: tag}
		}
	}
	return out, nil
}
