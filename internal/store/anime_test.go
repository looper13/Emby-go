package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"emby-go/internal/anime"
)

func TestAnimeRefreshEligibilityUsesVisitsNotSyncAttempts(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entries := []anime.Entry{
		{ID: 1, Name: "Unopened", Year: 2026, Season: 4},
		{ID: 2, Name: "Visited", Year: 2026, Season: 4},
		{ID: 3, Name: "Old visit", Year: 2026, Season: 4},
	}
	if err = s.SaveAnimeCatalog(context.Background(), entries); err != nil {
		t.Fatal(err)
	}
	if err = s.TouchAnimeEntry(2); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	if _, err = s.db.Exec(`UPDATE anime_mikan_entries SET last_opened_at=? WHERE id=3`, old); err != nil {
		t.Fatal(err)
	}
	if err = s.SetAnimeSyncState("entry:3", 1, "success", ""); err != nil {
		t.Fatal(err)
	}
	ids, err := s.AnimeRefreshEntries(2026, 4)
	if err != nil || !slices.Equal(ids, []int64{2}) {
		t.Fatal("background sync kept an old visit alive", ids, err)
	}
	lib, err := s.EnsureAnimeLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MoviesForScrape(lib.ID, false, 10); !errors.Is(err, ErrLibraryCapability) {
		t.Fatal("online scrape was accepted", err)
	}
	if _, err = s.CountMoviesForScrape(lib.ID, false); !errors.Is(err, ErrLibraryCapability) {
		t.Fatal("online scrape count was accepted", err)
	}
}

func TestAnimeMigrationPreservesLegacyAndSystemLibrary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE libraries(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,path TEXT UNIQUE NOT NULL,type TEXT DEFAULT 'movies',enabled INTEGER DEFAULT 1);
INSERT INTO libraries(id,name,path) VALUES(17,'legacy','/media'),(50,'deleted','/deleted'); DELETE FROM libraries WHERE id=50;`)
	if err != nil {
		t.Fatal(err)
	}
	legacy := &Store{db: db}
	if err = legacy.init(); err != nil {
		t.Fatal(err)
	}
	// These are the three additive movie columns from the pre-anime Open path;
	// init alone intentionally represents an older initial schema.
	if _, err = db.Exec(`ALTER TABLE movies ADD COLUMN backdrop_paths TEXT NOT NULL DEFAULT '[]'; ALTER TABLE movies ADD COLUMN trailer_url TEXT; ALTER TABLE movies ADD COLUMN cover_url TEXT;`); err != nil {
		t.Fatal(err)
	}
	var mid int64
	if err = db.QueryRow(upsertMovieSQL, movieValues(Movie{LibraryID: 17, SourcePath: "/media/a.strm", Title: "old", Status: "success"}, 0, time.Now(), animeNow())...).Scan(&mid); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO userdata(movie_id,position_ticks,play_count) VALUES(?,123,2)`, mid); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.waitFeaturesReady()
	lib, err := s.Library(17)
	if err != nil || lib.Path != "/media" || lib.SourceType != SourceDirectory {
		t.Fatalf("legacy=%+v err=%v", lib, err)
	}
	if movie, err := s.Movie(mid); err != nil || movie.Title != "old" || movie.LibraryID != 17 {
		t.Fatal("legacy movie lost during migration", movie, err)
	}
	a, err := s.EnsureAnimeLibrary()
	if err != nil || a.ID <= 50 || a.Path != "" || a.SourceType != SourceAnime {
		t.Fatalf("system=%+v err=%v", a, err)
	}
	var isNull bool
	if err = s.db.QueryRow(`SELECT path IS NULL FROM libraries WHERE id=?`, a.ID).Scan(&isNull); err != nil || !isNull {
		t.Fatal("online path must be SQL NULL", err)
	}
	a, err = s.UpdateAnimeLibrary("我的动画", false, a.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateAnimeLibrary("stale", true, a.Revision-1); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err = s.DeleteLibrary(a.ID); !errors.Is(err, ErrSystemLibrary) {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.waitFeaturesReady()
	again, err := s.EnsureAnimeLibrary()
	if err != nil || again.ID != a.ID || again.Name != a.Name || again.Enabled {
		t.Fatalf("reopened=%+v %v", again, err)
	}
	data, err := s.Data(mid)
	if err != nil || data.PositionTicks != 123 || data.PlayCount != 2 {
		t.Fatalf("lost user data: %+v %v", data, err)
	}
	rows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("broken foreign keys")
	}
	dirs, err := s.DirectoryLibraries()
	if err != nil || len(dirs) != 1 || dirs[0].ID != 17 {
		t.Fatal(dirs, err)
	}
}

func TestAnimeMigrationFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.db")
	db, _ := sql.Open("sqlite", path)
	_, err := db.Exec(`CREATE TABLE libraries(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,path TEXT UNIQUE NOT NULL,type TEXT,enabled INTEGER); INSERT INTO libraries VALUES(9,'invalid','','movies',1)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("expected constraint failure")
	}
	db, _ = sql.Open("sqlite", path)
	defer db.Close()
	var name string
	if err := db.QueryRow(`SELECT name FROM libraries WHERE id=9`).Scan(&name); err != nil || name != "invalid" {
		t.Fatal("legacy table changed", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestAnimeBindingsAndEpisodeIdentity(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.waitFeaturesReady()
	l, err := s.AddLibrary("media", "/media")
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.UpsertMovie(Movie{LibraryID: l.ID, SourcePath: "/media/a.strm", Title: "A", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sub := anime.Subject{BangumiID: 42, Name: "A", Platform: "TV"}
	sid, err := s.SaveAnimeSubject(context.Background(), sub, []anime.Episode{{BangumiID: 8, Sort: json.Number("570.5"), Season: 0}, {BangumiID: 9, Sort: json.Number("1"), Season: 1}})
	if err != nil {
		t.Fatal(err)
	}
	eps, err := s.AnimeEpisodes(sid)
	if err != nil || len(eps) != 2 || eps[1].Sort.String() != "570.5" {
		t.Fatal(eps, err)
	}
	b, err := s.BindAnime(sid, eps[0].ID, m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BindAnime(sid, eps[1].ID, m, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate media: %v", err)
	}
	if _, err = s.BindAnime(sid, eps[0].ID, m, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale binding: %v", err)
	}
	if err = s.SetAnimeAvailability(b, "playable", "v1", ""); err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveAnimeSubject(context.Background(), sub, []anime.Episode{{BangumiID: 8, Sort: json.Number("570.5"), Season: 0}, {BangumiID: 9, Sort: json.Number("2"), Season: 1}})
	if err != nil {
		t.Fatal(err)
	}
	next, _ := s.AnimeEpisodes(sid)
	if next[0].ID != eps[0].ID || next[0].Index != eps[0].Index {
		t.Fatal("unstable episode identity")
	}
	b, err = s.BindAnime(sid, eps[0].ID, 0, b.Revision)
	if err != nil || b.Status != "unbound" {
		t.Fatal(b, err)
	}
	if err = s.SetAnimeAvailability(anime.Binding{ID: b.ID, Revision: b.Revision - 1, MovieID: m}, "playable", "stale", ""); !errors.Is(err, ErrConflict) {
		t.Fatal("stale check committed", err)
	}
	if _, err = s.Movie(m); err != nil {
		t.Fatal("unbind removed original media", err)
	}
}

func TestAnimePartialMetadataPreservesSnapshot(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	entry := anime.Entry{ID: 7, Name: "Series", Image: "https://example.org/cover.jpg", Year: 2026, Season: 4, Weekday: 3, BangumiID: 8}
	if err = s.SaveAnimeCatalog(ctx, []anime.Entry{entry}); err != nil {
		t.Fatal(err)
	}
	release := anime.Release{ID: strings.Repeat("a", 40), Title: "Ep 1", Group: "Group", PublishedAt: "2026-10-01T00:00:00Z"}
	if err = s.SaveAnimeEntry(ctx, entry, []anime.Release{release}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAnimeEntry(ctx, anime.Entry{ID: 7, Name: "Updated"}, []anime.Release{{ID: release.ID, Title: release.Title}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.AnimeEntry(7)
	if got.Image != entry.Image || got.Year != 2026 || got.Weekday != 3 || got.BangumiID != 8 {
		t.Fatal(got)
	}
	releases, total, _ := s.AnimeReleases(7, 50, 0, "")
	if total != 1 || releases[0].Group != "Group" || releases[0].PublishedAt != release.PublishedAt {
		t.Fatal(releases)
	}
}

func TestAnimeBackupRestoreAndInterruptedSync(t *testing.T) {
	root := t.TempDir()
	s, err := Open(filepath.Join(root, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.waitFeaturesReady()
	lib, err := s.EnsureAnimeLibrary()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.AnimeSettings()
	if _, err = s.SaveAnimeSettings(cfg, "Retained", false, lib.Revision); err != nil {
		t.Fatal(err)
	}
	directory, _ := s.AddLibrary("NFO", filepath.Join(root, "media"))
	mid, err := s.UpsertMovie(Movie{LibraryID: directory.ID, SourcePath: filepath.Join(root, "a.strm"), Status: "success", Title: "Original"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sid, err := s.SaveAnimeSubject(context.Background(), anime.Subject{BangumiID: 10, Name: "Film", Platform: "剧场版"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BindAnime(sid, 0, mid, 0); err != nil {
		t.Fatal(err)
	}
	if err = s.SetFavorite(mid, true); err != nil {
		t.Fatal(err)
	}
	if err = s.SetAnimeSyncState("catalog:2026:4", 2, "running", ""); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backup.db")
	if _, err = s.db.Exec(`VACUUM INTO ?`, backup); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.RecoverAnimeSync(); err != nil {
		t.Fatal(err)
	}
	got, _ := restored.EnsureAnimeLibrary()
	binding, _ := restored.AnimeBinding(sid, 0)
	data, _ := restored.Data(mid)
	state, _ := restored.AnimeSync("catalog:2026:4")
	if got.ID != lib.ID || got.Enabled || got.Name != "Retained" || binding.MovieID != mid || !data.IsFavorite || state.Status != "interrupted" {
		t.Fatal(got, binding, data, state)
	}
	var integrity string
	if err = restored.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal(integrity, err)
	}
	rows, err := restored.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
}
