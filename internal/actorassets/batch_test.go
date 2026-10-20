package actorassets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveManyMatchesSingleValidation(t *testing.T) {
	s := New(t.TempDir())
	names := []string{"Valid", "Corrupt", "Unsafe", "Future", "Missing"}
	for _, name := range names[:4] {
		m, data, err := Prepare(name, picture(t), Avatar{Origin: "legacy_local"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		pending, err := s.Stage(m, data, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Publish(&pending); err != nil {
			t.Fatal(err)
		}
		switch name {
		case "Corrupt":
			if err := os.WriteFile(filepath.Join(s.Dir, PersonDir(name), filepath.FromSlash(m.Avatar.File)), []byte("bad"), 0600); err != nil {
				t.Fatal(err)
			}
		case "Unsafe":
			m.Avatar.File = "../../escape.webp"
		case "Future":
			m.SchemaVersion++
		}
		if err := WriteJSON(s.Dir, filepath.Join(PersonDir(name), "actor.json"), m); err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		t.Helper()
		batch, err := s.ResolveMany(context.Background(), names)
		if err != nil || len(batch) != 1 {
			t.Fatalf("%+v %v", batch, err)
		}
		for _, name := range names {
			path, tag, err := s.Resolve(name)
			if err != nil {
				if _, ok := batch[name]; ok {
					t.Fatal("invalid asset accepted", name)
				}
			} else if batch[name] != (Resolved{Path: path, Tag: tag}) {
				t.Fatal("batch/single mismatch", name)
			}
		}
	}
	check()
	if err := os.Remove(filepath.Join(s.Dir, "layout.json")); err != nil {
		t.Fatal(err)
	}
	check()
	if err := WriteJSON(s.Dir, "layout.json", map[string]int{"schema_version": 9}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveMany(context.Background(), names); err == nil {
		t.Fatal("future layout accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ResolveMany(ctx, names); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func TestResolveManyDirectoryCase(t *testing.T) {
	s := New(t.TempDir())
	m, data, err := Prepare("Case", picture(t), Avatar{Origin: "legacy_local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.Stage(m, data, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(&pending); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Dir, PersonDir(m.Name))
	if err := os.Rename(dir, filepath.Join(filepath.Dir(dir), strings.ToUpper(filepath.Base(dir)))); err != nil {
		t.Fatal(err)
	}
	path, tag, singleErr := s.Resolve(m.Name)
	batch, err := s.ResolveMany(context.Background(), []string{m.Name})
	if err != nil {
		t.Fatal(err)
	}
	if singleErr == nil {
		if batch[m.Name] != (Resolved{Path: path, Tag: tag}) {
			t.Fatal("case-insensitive directory lookup diverged", batch)
		}
	} else if len(batch) != 0 {
		t.Fatal("case-sensitive directory lookup diverged", batch)
	}
}

func TestResolveManyRejectsEscapingShard(t *testing.T) {
	outside := New(t.TempDir())
	m, data, err := Prepare("Escape", picture(t), Avatar{Origin: "legacy_local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := outside.Stage(m, data, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := outside.Publish(&pending); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir())
	if err := os.MkdirAll(filepath.Join(s.Dir, "people"), 0700); err != nil {
		t.Fatal(err)
	}
	shard := Key("Escape")[:2]
	if err := os.Symlink(filepath.Join(outside.Dir, "people", shard), filepath.Join(s.Dir, "people", shard)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result, err := s.ResolveMany(context.Background(), []string{"Escape"})
	if err != nil || len(result) != 0 {
		t.Fatal("escaped actor root", result, err)
	}
}
