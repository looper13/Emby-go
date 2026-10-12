package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"emby-go/internal/actorassets"
	"emby-go/internal/avatar"
)

type ImportPreview struct {
	Total       int      `json:"total"`
	Ready       int      `json:"ready"`
	Existing    int      `json:"existing"`
	Conflicts   int      `json:"conflicts"`
	Damaged     int      `json:"damaged"`
	Missing     int      `json:"missing"`
	Samples     []string `json:"samples"`
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
}

// PreviewActorImport does not create directories, manifests, tasks or metadata.
func (s *Scraper) PreviewActorImport(ctx context.Context) (ImportPreview, error) {
	p := ImportPreview{Source: s.cfg.AvatarsDir, Destination: s.assets().Dir, Samples: []string{}}
	names, err := s.store.ActorNames(0)
	if err != nil {
		return p, err
	}
	p.Total = len(names)
	collisions := legacyCollisions(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		if pending, err := s.assets().Pending(name); err == nil {
			if pending.Manifest.Avatar.Origin == "legacy_local" {
				p.Ready++
				continue
			}
			p.Conflicts++
			if len(p.Samples) < 20 {
				p.Samples = append(p.Samples, name+"：网络头像任务尚待恢复")
			}
			continue
		} else if !os.IsNotExist(err) {
			p.Damaged++
			if len(p.Samples) < 20 {
				p.Samples = append(p.Samples, name+"："+err.Error())
			}
			continue
		}
		if _, _, err := s.assets().Resolve(name); err == nil {
			p.Existing++
			continue
		}
		if m, err := s.assets().Load(name); err == nil && m.Avatar != nil {
			p.Damaged++
			if len(p.Samples) < 20 {
				p.Samples = append(p.Samples, name+"：已有资料的图片损坏，请运行头像修复")
			}
			continue
		} else if err != nil && !os.IsNotExist(err) {
			p.Damaged++
			if len(p.Samples) < 20 {
				p.Samples = append(p.Samples, name+"："+err.Error())
			}
			continue
		}
		data, err := actorassets.ReadBounded(s.cfg.AvatarsDir, avatar.FileName(name), actorassets.MaxImageBytes)
		if os.IsNotExist(err) {
			p.Missing++
			continue
		}
		if collisions[avatar.FileName(name)] {
			p.Conflicts++
			if len(p.Samples) < 20 {
				p.Samples = append(p.Samples, name+"：旧文件名冲突")
			}
			continue
		}
		if err == nil {
			_, _, err = actorassets.Prepare(name, data, actorassets.Avatar{Origin: "legacy_local"}, nil)
		}
		if err != nil {
			p.Damaged++
			if len(p.Samples) < 20 {
				p.Samples = append(p.Samples, name+"："+err.Error())
			}
			continue
		}
		p.Ready++
	}
	return p, nil
}

type migration struct {
	SchemaVersion int                     `json:"schema_version"`
	OperationID   string                  `json:"operation_id"`
	Source        string                  `json:"source"`
	StartedAt     time.Time               `json:"started_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
	Status        string                  `json:"status"`
	Attempts      int                     `json:"attempts"`
	Results       map[string]AvatarResult `json:"results"`
}

// ImportActors uses the same serial writer as network tasks, but cannot modify NFOs.
// The source-derived journal is updated after every item; retry reconciles current
// manifests and source files instead of trusting old database IDs.
func (s *Scraper) ImportActors(ctx context.Context, onProgress func(Progress), onItem func(AvatarResult)) (Progress, error) {
	if s.store == nil {
		return Progress{}, errors.New("缺少演员存储")
	}
	release, err := s.assets().Acquire(ctx)
	if err != nil {
		return Progress{}, err
	}
	defer release()
	names, err := s.store.ActorNames(0)
	if err != nil {
		return Progress{}, err
	}
	source, err := filepath.Abs(s.cfg.AvatarsDir)
	if err != nil {
		return Progress{}, err
	}
	id := actorassets.Hash([]byte(source))
	rel := filepath.Join("migrations", id+".json")
	m := migration{SchemaVersion: 1, OperationID: id, Source: source, StartedAt: time.Now().UTC(), Status: "running", Results: map[string]AvatarResult{}}
	if data, readErr := actorassets.ReadBounded(s.assets().Dir, rel, 16<<20); readErr == nil {
		if err := json.Unmarshal(data, &m); err != nil {
			return Progress{}, err
		}
		if m.SchemaVersion != 1 || m.Source != source || m.OperationID != id {
			return Progress{}, errors.New("旧头像导入记录版本或来源不匹配")
		}
		if m.Results == nil {
			m.Results = map[string]AvatarResult{}
		}
	} else if !os.IsNotExist(readErr) {
		return Progress{}, readErr
	}
	m.Attempts++
	m.Status = "running"
	m.UpdatedAt = time.Now().UTC()
	if err := actorassets.WriteJSON(s.assets().Dir, rel, m); err != nil {
		return Progress{}, err
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var journalErr error
	progress, runErr := s.runActorAssetsLocked(workCtx, names, true, onProgress, func(result AvatarResult) {
		m.Results[result.Actor] = result
		m.UpdatedAt = time.Now().UTC()
		if err := actorassets.WriteJSON(s.assets().Dir, rel, m); err != nil {
			journalErr = err
			cancel()
		}
		if onItem != nil {
			onItem(result)
		}
	})
	m.Status = "complete"
	if runErr != nil {
		m.Status = "partial"
	}
	if ctx.Err() != nil {
		m.Status = "cancelled"
	}
	m.UpdatedAt = time.Now().UTC()
	return progress, errors.Join(runErr, journalErr, actorassets.WriteJSON(s.assets().Dir, rel, m))
}
