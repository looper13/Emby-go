package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"emby-go/internal/actorassets"
	"emby-go/internal/avatar"
	"emby-go/internal/gfriends"
	"emby-go/internal/nfo"
	"emby-go/internal/scanner"
	"emby-go/internal/store"
)

func actorFixture(t *testing.T, actors string) (*store.Store, Config, string) {
	t.Helper()
	root := t.TempDir()
	libDir := filepath.Join(root, "media")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib, err := db.AddLibrary("actors", libDir)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(libDir, "ABC-123.strm")
	path := filepath.Join(libDir, "ABC-123.nfo")
	if err := os.WriteFile(source, []byte("http://example.invalid/movie.mp4"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("<movie><title>Test</title><custom>keep</custom>"+actors+"</movie>"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.RescanOne(db, lib, source); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.ActorsDir = filepath.Join(root, "actors")
	cfg.AvatarsDir = filepath.Join(root, "avatars")
	cfg.AvatarSource = "gfriends"
	return db, cfg, path
}
func actorPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 6))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestActorGFriendsNoTokenSerialCommitAndRepair(t *testing.T) {
	db, cfg, path := actorFixture(t, `<actor><name>Alice</name><role>keep</role></actor><actor><name>Bob</name></actor>`)
	data := actorPNG(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("public download carried authorization")
		}
		if r.URL.Path == "/index" {
			w.Write([]byte(`{"Information":{"Timestamp":1.5},"Content":{"low":{"Alice.jpg":"Ally.jpg?t=7","Bob.jpg":"Bob.jpg"}}}`))
		} else {
			w.Write(data)
		}
	}))
	defer srv.Close()
	cfg.GFriends = gfriends.Config{IndexURL: srv.URL + "/index", BaseURL: srv.URL}
	s := New(cfg, db)
	p, err := s.RunAvatars(context.Background(), 0, 0, nil, nil)
	if err != nil || p.Success != 2 {
		t.Fatal(p, err)
	}
	meta, err := nfo.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range meta.Actors {
		if a.Thumb == "" {
			t.Fatal("lost actor update", a)
		}
	}
	b, _ := os.ReadFile(path)
	if !bytes.Contains(b, []byte("<role>keep</role>")) || !bytes.Contains(b, []byte("<custom>keep</custom>")) {
		t.Fatal("unknown tags lost")
	}
	for _, name := range []string{"Alice", "Bob"} {
		m, err := s.assets().Load(name)
		if err != nil || m.Avatar.Origin != "gfriends" {
			t.Fatal(m, err)
		}
		if m.Avatar.IndexTimestamp != "1.5" || m.Avatar.DownloadedAt == nil {
			t.Fatal("missing download provenance", m)
		}
		if name == "Alice" && (m.Avatar.SourceVersion != "7" || len(m.Aliases) != 1 || m.Aliases[0].Name != "Ally" || m.Aliases[0].Source != "gfriends/low") {
			t.Fatal("alias mapping lost its evidence", m)
		}
		if _, err := s.assets().Pending(name); !os.IsNotExist(err) {
			t.Fatal("completed journal left", err)
		}
	}
	before := requests.Load()
	p, err = s.RunAvatars(context.Background(), 0, 0, nil, nil)
	if err != nil || p.Total != 0 || requests.Load() != before {
		t.Fatal("repeat was not idempotent", p, err)
	}
	original, err := s.assets().Load("Alice")
	if err != nil {
		t.Fatal(err)
	}
	imagePath, _, _ := s.assets().Resolve("Alice")
	if err := os.Remove(imagePath); err != nil {
		t.Fatal(err)
	}
	p, err = s.RunAvatars(context.Background(), 0, 1, nil, nil)
	if err != nil || p.Success != 1 {
		t.Fatal("nonempty URL excluded repair", p, err)
	}
	repaired, err := s.assets().Load("Alice")
	if err != nil || !repaired.CreatedAt.Equal(original.CreatedAt) || repaired.Avatar.DownloadedAt == nil || !repaired.Avatar.DownloadedAt.After(*original.Avatar.DownloadedAt) || len(repaired.Aliases) != 1 || repaired.Aliases[0] != original.Aliases[0] {
		t.Fatal("repair changed identity history or lost provenance", repaired, err)
	}
}
func TestActorPartialResumePreservesNonemptyNFO(t *testing.T) {
	db, cfg, path := actorFixture(t, `<actor><name>Alice</name></actor>`)
	s := New(cfg, db)
	m, b, err := actorassets.Prepare("Alice", actorPNG(t), actorassets.Avatar{Origin: "gfriends", SourceURL: "https://images.invalid/a.jpg"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte("<broken>"), 0600); err != nil {
		t.Fatal(err)
	}
	r := s.commitAvatar(context.Background(), avatarPrepared{result: AvatarResult{Actor: "Alice"}, manifest: m, bytes: b}, false)
	if r.Status != StatusFailed || r.Category != "partial" {
		t.Fatal(r)
	}
	if _, _, err := s.assets().Resolve("Alice"); err != nil {
		t.Fatal("published asset should remain usable", err)
	}
	if _, err := s.assets().Pending("Alice"); err != nil {
		t.Fatal("missing recovery journal", err)
	}
	modified := strings.Replace(string(before), "</name>", "</name><thumb>https://user.invalid/chosen.jpg</thumb>", 1)
	if err := os.WriteFile(path, []byte(modified), 0600); err != nil {
		t.Fatal(err)
	}
	// No network server: journal recovery must only use durable local data.
	p, err := s.RunAvatars(context.Background(), 0, 0, nil, nil)
	if err != nil || p.Success != 1 {
		t.Fatal(p, err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != modified {
		t.Fatal("nonempty NFO overwritten")
	}
	if _, err := s.assets().Pending("Alice"); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
func TestActorLegacyPreviewCopyAndCollision(t *testing.T) {
	db, cfg, path := actorFixture(t, `<actor><name>Alice</name></actor><actor><name>Bob</name></actor><actor><name>bob</name></actor>`)
	s := New(cfg, db)
	if err := os.MkdirAll(cfg.AvatarsDir, 0755); err != nil {
		t.Fatal(err)
	}
	data := actorPNG(t)
	for _, name := range []string{"Alice", "Bob"} {
		if err := os.WriteFile(avatar.Path(cfg.AvatarsDir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(path)
	preview, err := s.PreviewActorImport(context.Background())
	if err != nil || preview.Ready != 1 || preview.Conflicts != 2 {
		t.Fatal(preview, err)
	}
	if _, err := os.Stat(cfg.ActorsDir); !os.IsNotExist(err) {
		t.Fatal("preview wrote assets")
	}
	for attempt := 0; attempt < 2; attempt++ {
		p, err := s.ImportActors(context.Background(), nil, nil)
		if err != nil || p.Success != 1-attempt || p.Skipped != 2+attempt || p.Ambiguous != 2 {
			t.Fatal(p, err)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("import modified NFO")
	}
	old, _ := os.ReadFile(avatar.Path(cfg.AvatarsDir, "Alice"))
	if !bytes.Equal(old, data) {
		t.Fatal("import modified source")
	}
	if _, _, err := s.assets().Resolve("Alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.assets().Resolve("bob"); !os.IsNotExist(err) {
		t.Fatal("ambiguous identity imported", err)
	}
}

func TestActorJournalBoundaryRecovery(t *testing.T) {
	for _, stage := range []string{"prepared", "asset_published", "pending_nfo", "pending_rescan", "complete"} {
		t.Run(stage, func(t *testing.T) {
			db, cfg, path := actorFixture(t, `<actor><name>Alice</name></actor>`)
			s := New(cfg, db)
			m, b, err := actorassets.Prepare("Alice", actorPNG(t), actorassets.Avatar{Origin: "gfriends", SourceURL: "https://example.invalid/a.jpg"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := s.assets().Stage(m, b, "")
			if err != nil {
				t.Fatal(err)
			}
			if stage != "prepared" {
				if err := s.assets().Publish(&pending); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "pending_rescan" || stage == "complete" {
				if _, err := nfo.FillActorThumb(path, "Alice", m.Avatar.SourceURL); err != nil {
					t.Fatal(err)
				}
			}
			pending.Stage = stage
			if err := s.assets().SavePending(&pending); err != nil {
				t.Fatal(err)
			}
			// New task object stands in for a restart; no reachable upstream is configured.
			p, err := New(cfg, db).RunAvatars(context.Background(), 0, 0, nil, nil)
			if err != nil || p.Success != 1 {
				t.Fatal(p, err)
			}
			if _, err := s.assets().Pending("Alice"); !os.IsNotExist(err) {
				t.Fatal(err)
			}
			a, err := db.ActorAvatar("Alice")
			if err != nil || a.AvatarTag != m.Avatar.SHA256[:16] || a.AvatarURL != m.Avatar.SourceURL {
				t.Fatal(a, err)
			}
		})
	}
}

func TestActorReadOnlyNFOAndCancellationResume(t *testing.T) {
	db, cfg, path := actorFixture(t, `<actor><name>Alice</name></actor>`)
	s := New(cfg, db)
	m, b, err := actorassets.Prepare("Alice", actorPNG(t), actorassets.Avatar{Origin: "gfriends", SourceURL: "https://example.invalid/a.jpg"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := s.commitAvatar(ctx, avatarPrepared{result: AvatarResult{Actor: "Alice"}, manifest: m, bytes: b}, false)
	if r.Status != StatusFailed {
		t.Fatal(r)
	}
	if _, _, err := s.assets().Resolve("Alice"); !os.IsNotExist(err) {
		t.Fatal("cancelled preparation published", err)
	}
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	p, err := s.RunAvatars(context.Background(), 0, 0, nil, nil)
	if err == nil || p.Partial != 1 {
		t.Fatal(p, err)
	}
	pending, err := s.assets().Pending("Alice")
	if err != nil || len(pending.RemainingSources) != 1 {
		t.Fatal(pending, err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	p, err = s.RunAvatars(context.Background(), 0, 0, nil, nil)
	if err != nil || p.Success != 1 {
		t.Fatal(p, err)
	}
}

func TestActorMetaTubeAndConfiguredFallback(t *testing.T) {
	for _, mode := range []string{"metatube", "gfriends_then_metatube"} {
		t.Run(mode, func(t *testing.T) {
			db, cfg, _ := actorFixture(t, `<actor><name>Alice</name></actor>`)
			data := actorPNG(t)
			var base string
			var searches atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/index":
					w.Write([]byte(`{"Information":{"Timestamp":1.5},"Content":{"source":{"Somebody.jpg":"Somebody.jpg"}}}`))
				case "/v1/actors/search":
					searches.Add(1)
					if r.Header.Get("Authorization") != "Bearer secret" {
						t.Error("MetaTube search missing token")
					}
					json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "1", "name": "Alice", "aliases": []string{"Ally", "Ally", "Alice"}, "provider": "fanza", "images": []string{base + "/image"}}}})
				case "/image":
					if r.Header.Get("Authorization") != "" {
						t.Error("public image received token")
					}
					w.Write(data)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			base = server.URL
			cfg.AvatarSource = mode
			cfg.BaseURL = base
			cfg.Token = "secret"
			cfg.GFriends = gfriends.Config{IndexURL: base + "/index", BaseURL: base}
			s := New(cfg, db)
			p, err := s.RunAvatars(context.Background(), 0, 0, nil, nil)
			if err != nil || p.Success != 1 || searches.Load() != 1 {
				t.Fatal(p, err, searches.Load())
			}
			m, err := s.assets().Load("Alice")
			if err != nil || m.Avatar.Origin != "metatube" {
				t.Fatal(m, err)
			}
			if len(m.Aliases) != 1 || m.Aliases[0].Name != "Ally" || m.Aliases[0].Source != "metatube/fanza" {
				t.Fatal("MetaTube alias provenance lost", m.Aliases)
			}
		})
	}
}

func TestActorImportPreviewIncludesPublishedPending(t *testing.T) {
	db, cfg, path := actorFixture(t, `<actor><name>Alice</name></actor>`)
	s := New(cfg, db)
	before, _ := os.ReadFile(path)
	m, b, err := actorassets.Prepare("Alice", actorPNG(t), actorassets.Avatar{Origin: "legacy_local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.assets().Stage(m, b, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.assets().Publish(&pending); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewActorImport(context.Background())
	if err != nil || preview.Ready != 1 {
		t.Fatal("UI cannot resume published import", preview, err)
	}
	p, err := s.ImportActors(context.Background(), nil, nil)
	if err != nil || p.Success != 1 {
		t.Fatal(p, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("import recovery modified NFO")
	}
}
