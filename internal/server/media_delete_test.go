package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"emby-go/internal/alist"
	"emby-go/internal/scanner"
	"emby-go/internal/store"
)

func TestAdminDeleteRemovesLocalFilesAndIndex(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	lib, err := a.db.AddLibrary("Movies", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Movie A files (flat directory)
	sourceA := filepath.Join(lib.Path, "ABC-123.strm")
	nfoA := filepath.Join(lib.Path, "ABC-123.nfo")
	posterA := filepath.Join(lib.Path, "ABC-123-poster.webp")
	fanartA := filepath.Join(lib.Path, "ABC-123-fanart.webp")
	stillA := filepath.Join(lib.Path, "ABC-123-fanart1.webp")
	thumbA := filepath.Join(lib.Path, "ABC-123-thumb.webp")
	trailerA := filepath.Join(lib.Path, "ABC-123-trailer.mp4")
	subA := filepath.Join(lib.Path, "ABC-123.zh-CN.srt")

	writeFile(t, sourceA, "https://media.test/video.mp4\n")
	writeFile(t, nfoA, "<movie><title>Movie A</title></movie>")
	writeFile(t, posterA, "poster")
	writeFile(t, fanartA, "fanart")
	writeFile(t, stillA, "still")
	writeFile(t, thumbA, "thumb")
	writeFile(t, trailerA, "trailer")
	writeFile(t, subA, "1\n00:00:01,000 --> 00:00:02,000\nSub\n")

	// Movie B files in the same directory (should be preserved)
	sourceB := filepath.Join(lib.Path, "DEF-456.strm")
	nfoB := filepath.Join(lib.Path, "DEF-456.nfo")
	posterB := filepath.Join(lib.Path, "DEF-456-poster.webp")
	writeFile(t, sourceB, "https://media.test/videoB.mp4\n")
	writeFile(t, nfoB, "<movie><title>Movie B</title></movie>")
	writeFile(t, posterB, "posterB")

	idA, err := a.db.UpsertMovie(store.Movie{
		LibraryID:    lib.ID,
		SourcePath:   sourceA,
		NFOPath:      nfoA,
		PosterPath:   posterA,
		BackdropPath: fanartA,
		Title:        "Movie A",
		Status:       "success",
	}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	idB, err := a.db.UpsertMovie(store.Movie{
		LibraryID:  lib.ID,
		SourcePath: sourceB,
		NFOPath:    nfoB,
		PosterPath: posterB,
		Title:      "Movie B",
		Status:     "success",
	}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// Delete movie A via /api/admin/items/:id
	resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/items/%d", idA), token, "")
	if resp.Code != http.StatusNoContent {
		t.Fatalf("DELETE item %d returned %d: %s", idA, resp.Code, resp.Body.String())
	}

	// Movie A files must all be gone
	for _, p := range []string{sourceA, nfoA, posterA, fanartA, stillA, thumbA, trailerA, subA} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("Movie A file %s still exists", p)
		}
	}

	// Movie B files must still exist
	for _, p := range []string{sourceB, nfoB, posterB} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("Movie B file %s was unexpectedly removed: %v", p, err)
		}
	}

	// Movie A must be gone from DB
	if _, err := a.db.Movie(idA); err == nil {
		t.Error("Movie A still in DB")
	}

	// Movie B must remain in DB
	if _, err := a.db.Movie(idB); err != nil {
		t.Errorf("Movie B not found in DB: %v", err)
	}

	// Rescan library: incremental scan must NOT restore movie A
	_, scanErr := scanner.Scan(context.Background(), a.db, lib)
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	if _, err := a.db.Movie(idA); err == nil {
		t.Error("Movie A reappeared after rescan")
	}
}

func TestAdminDeleteDedicatedDirectory(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	lib, err := a.db.AddLibrary("Movies", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(lib.Path, "XYZ-789")
	if err := os.MkdirAll(filepath.Join(dir, "extrafanart"), 0755); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(dir, "XYZ-789.strm")
	nfo := filepath.Join(dir, "movie.nfo")
	poster := filepath.Join(dir, "poster.jpg")
	fanart := filepath.Join(dir, "fanart.jpg")
	extra := filepath.Join(dir, "extrafanart", "fanart1.jpg")

	writeFile(t, source, "https://media.test/xyz.mp4")
	writeFile(t, nfo, "<movie><title>XYZ</title></movie>")
	writeFile(t, poster, "poster")
	writeFile(t, fanart, "fanart")
	writeFile(t, extra, "extra")

	id, err := a.db.UpsertMovie(store.Movie{
		LibraryID:  lib.ID,
		SourcePath: source,
		OutputDir:  dir,
		NFOPath:    nfo,
		PosterPath: poster,
		Title:      "XYZ",
		Status:     "success",
	}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/items/%d", id), token, "")
	if resp.Code != http.StatusNoContent {
		t.Fatalf("DELETE returned %d: %s", resp.Code, resp.Body.String())
	}

	// The dedicated folder XYZ-789 and extrafanart must both be deleted
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Dedicated movie directory %s still exists", dir)
	}

	if _, err := a.db.Movie(id); err == nil {
		t.Error("Movie XYZ still in DB")
	}
}

func TestAdminDeleteWithAListRemoteDeletion(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	f := &remoteDeleteFixture{files: map[string]alist.File{}, failed: map[string]bool{}}
	ts := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(ts.Close)

	a.cfgMu.Lock()
	a.cfg.AListURL, a.cfg.AListToken = ts.URL, "fixture-alist-token"
	a.cfgMu.Unlock()

	lib, err := a.db.AddLibrary("AListMovies", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	remote1 := "/115/ALIST-001-CD1.mp4"
	remote2 := "/115/ALIST-001-CD2.mp4"
	f.files[remote1] = alist.File{Name: "ALIST-001-CD1.mp4", ID: "1", Size: 1000, Modified: "2026-10-27T00:00:00Z"}
	f.files[remote2] = alist.File{Name: "ALIST-001-CD2.mp4", ID: "2", Size: 2000, Modified: "2026-10-27T00:00:00Z"}

	cd1 := filepath.Join(lib.Path, "ALIST-001-CD1.strm")
	cd2 := filepath.Join(lib.Path, "ALIST-001-CD2.strm")
	nfo := filepath.Join(lib.Path, "ALIST-001.nfo")
	poster := filepath.Join(lib.Path, "ALIST-001-poster.webp")

	writeFile(t, cd1, ts.URL+"/d"+remote1+"?sign=123")
	writeFile(t, cd2, ts.URL+"/d"+remote2+"?sign=456")
	writeFile(t, nfo, "<movie><title>AList Movie</title></movie>")
	writeFile(t, poster, "img")

	id, err := a.db.UpsertMovie(store.Movie{
		LibraryID:       lib.ID,
		SourcePath:      cd1,
		AdditionalParts: []string{cd2},
		NFOPath:         nfo,
		PosterPath:      poster,
		Number:          "ALIST-001",
		Title:           "AList Movie",
		Status:          "success",
	}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/items/%d", id), token, "")
	if resp.Code != http.StatusNoContent {
		t.Fatalf("DELETE returned %d: %s", resp.Code, resp.Body.String())
	}

	// Verify both remote files in AList were deleted
	f.mu.Lock()
	if len(f.removed) != 2 || f.removed[0] != remote1 || f.removed[1] != remote2 {
		t.Errorf("AList removed paths = %v, expected [%s, %s]", f.removed, remote1, remote2)
	}
	f.mu.Unlock()

	// Verify local files are deleted
	for _, p := range []string{cd1, cd2, nfo, poster} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("Local file %s still exists", p)
		}
	}

	// Verify DB record is deleted
	if _, err := a.db.Movie(id); err == nil {
		t.Error("Movie still in DB")
	}
}

func TestAdminDeleteWithAListFailureStillDeletesLocalAndDB(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	f := &remoteDeleteFixture{files: map[string]alist.File{}, failed: map[string]bool{}}
	ts := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(ts.Close)

	a.cfgMu.Lock()
	a.cfg.AListURL, a.cfg.AListToken = ts.URL, "fixture-alist-token"
	a.cfgMu.Unlock()

	lib, err := a.db.AddLibrary("AListMovies", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	remote := "/115/FAIL-001.mp4"
	f.files[remote] = alist.File{Name: "FAIL-001.mp4", ID: "fail", Size: 1000, Modified: "2026-10-27T00:00:00Z"}
	f.failed[remote] = true // force 403 failure on remote delete

	source := filepath.Join(lib.Path, "FAIL-001.strm")
	nfo := filepath.Join(lib.Path, "FAIL-001.nfo")
	writeFile(t, source, ts.URL+"/d"+remote)
	writeFile(t, nfo, "<movie><title>Fail Movie</title></movie>")

	id, err := a.db.UpsertMovie(store.Movie{
		LibraryID:  lib.ID,
		SourcePath: source,
		NFOPath:    nfo,
		Number:     "FAIL-001",
		Title:      "Fail Movie",
		Status:     "success",
	}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/items/%d", id), token, "")
	if resp.Code != http.StatusNoContent {
		t.Fatalf("DELETE should succeed with 204 No Content even when AList fails, got %d: %s", resp.Code, resp.Body.String())
	}

	// Local files and DB record must be deleted even if AList remote deletion failed!
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Errorf("Source file %s still exists", source)
	}
	if _, err := os.Stat(nfo); !os.IsNotExist(err) {
		t.Errorf("NFO file %s still exists", nfo)
	}

	if _, err := a.db.Movie(id); err == nil {
		t.Error("Movie still in DB despite deletion")
	}
}

func TestAdminDeleteAlistUnconfiguredStillDeletesLocalAndDB(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)

	// Ensure AList is not configured
	a.cfgMu.Lock()
	a.cfg.AListURL, a.cfg.AListToken = "", ""
	a.cfgMu.Unlock()

	lib, err := a.db.AddLibrary("Movies", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(lib.Path, "UNCFG-001.strm")
	nfo := filepath.Join(lib.Path, "UNCFG-001.nfo")
	writeFile(t, source, "http://192.168.1.50:5244/d/cloud/movie.mp4")
	writeFile(t, nfo, "<movie><title>Unconfigured</title></movie>")

	id, err := a.db.UpsertMovie(store.Movie{
		LibraryID:  lib.ID,
		SourcePath: source,
		NFOPath:    nfo,
		Title:      "Unconfigured",
		Status:     "success",
	}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/items/%d", id), token, "")
	if resp.Code != http.StatusNoContent {
		t.Fatalf("DELETE should succeed with 204 No Content even when AList is unconfigured, got %d: %s", resp.Code, resp.Body.String())
	}

	// Local files and DB record must be deleted
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Errorf("Source file %s still exists", source)
	}
	if _, err := os.Stat(nfo); !os.IsNotExist(err) {
		t.Errorf("NFO file %s still exists", nfo)
	}
	if _, err := a.db.Movie(id); err == nil {
		t.Error("Movie still in DB")
	}
}

func TestEmbyDeleteItemEndpoint(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	lib, err := a.db.AddLibrary("Movies", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(lib.Path, "EMBY-001.strm")
	nfo := filepath.Join(lib.Path, "EMBY-001.nfo")
	writeFile(t, source, "https://media.test/emby.mp4")
	writeFile(t, nfo, "<movie><title>Emby</title></movie>")

	id, err := a.db.UpsertMovie(store.Movie{
		LibraryID:  lib.ID,
		SourcePath: source,
		NFOPath:    nfo,
		Title:      "Emby",
		Status:     "success",
	}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// DELETE /Items/:id
	resp := accountRequest(a, "DELETE", fmt.Sprintf("/Items/%d", id), token, "")
	if resp.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Items/%d returned %d: %s", id, resp.Code, resp.Body.String())
	}

	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Error("source not deleted")
	}
	if _, err := a.db.Movie(id); err == nil {
		t.Error("movie still in DB")
	}
}
