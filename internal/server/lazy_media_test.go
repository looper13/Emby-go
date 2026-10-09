package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"emby-go/internal/librarywatch"
)

func TestPlaybackReadsCurrentSTRMWithoutMetadataWrites(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "movie.strm")
	writeFile(t, source, "ed2k://file/movie")
	writeFile(t, filepath.Join(root, "movie.nfo"), "<movie><title>Movie</title></movie>")
	app, server, token := newProbeTestApp(t, root)
	id, err := app.db.MovieIDByPath(source)
	if err != nil {
		t.Fatal(err)
	}
	base := "/Items/" + strconv.FormatInt(id, 10)
	movie, _ := app.db.Movie(id)
	library, _ := app.db.Library(movie.LibraryID)
	version := app.db.MovieMetadataVersion(id)
	if resp, _ := embyRaw(t, server, http.MethodPost, base+"/PlaybackInfo", token, "{}"); resp.StatusCode != 404 {
		t.Fatalf("invalid STRM playback status = %d", resp.StatusCode)
	}
	writeFile(t, source, "https://media.test/current.mkv?token=example")
	if err := app.refreshLibraryChanges(context.Background(), library, []librarywatch.Change{{Path: source}}); err != nil {
		t.Fatal(err)
	}
	resp, raw := embyRaw(t, server, http.MethodPost, base+"/PlaybackInfo", token, "{}")
	var playback struct {
		Sources []struct{ Container, Name string } `json:"MediaSources"`
	}
	if err := json.Unmarshal(raw, &playback); err != nil || resp.StatusCode != 200 || len(playback.Sources) != 1 || playback.Sources[0].Container != "mkv" || playback.Sources[0].Name != "current" {
		t.Fatalf("current source missing: %s %v", raw, err)
	}
	if app.db.MovieMetadataVersion(id) != version {
		t.Fatal("STRM event rewrote metadata")
	}
	writeFile(t, source, "")
	if resp, _ := embyRaw(t, server, http.MethodPost, base+"/PlaybackInfo", token, "{}"); resp.StatusCode != 404 {
		t.Fatalf("empty STRM playback status = %d", resp.StatusCode)
	}
}

func TestArtworkLoadsOnDemandAndEventsPreserveMetadata(t *testing.T) {
	root := t.TempDir()
	source, nfo := filepath.Join(root, "movie.strm"), filepath.Join(root, "movie.nfo")
	writeFile(t, source, "https://media.test/movie.mp4")
	writeFile(t, nfo, "<movie><title>Original</title></movie>")
	app, server, token := newProbeTestApp(t, root)
	id, _ := app.db.MovieIDByPath(source)
	movie, _ := app.db.Movie(id)
	library, _ := app.db.Library(movie.LibraryID)
	version := app.db.MovieMetadataVersion(id)
	base := "/Items/" + strconv.FormatInt(id, 10)
	// Prime both a detail response and an empty artwork catalog.
	embyRaw(t, server, http.MethodGet, base, token, "")
	app.invalidateArtworkPaths([]string{source}, false)
	app.movieCovers(movie)
	catalog, err := app.artworkCache().catalog(root, app.diskVersion(root), false)
	if err != nil || catalog.backdrops {
		t.Fatal("list loaded stills")
	}
	poster, still := filepath.Join(root, "movie-poster.jpg"), filepath.Join(root, "extrafanart", "fanart1.jpg")
	if err := os.MkdirAll(filepath.Dir(still), 0755); err != nil {
		t.Fatal(err)
	}
	writeJPEGImage(t, poster, 40, 80)
	writeFile(t, still, "still")
	// This NFO change has no NFO event: an image event must not read it.
	writeFile(t, nfo, "<movie><title>Broken")
	if err := app.refreshLibraryChanges(context.Background(), library, []librarywatch.Change{{Path: poster}, {Path: still}}); err != nil {
		t.Fatal(err)
	}
	stored, _ := app.db.Movie(id)
	if stored.Title != "Original" || app.db.MovieMetadataVersion(id) != version {
		t.Fatal("artwork event rewrote metadata")
	}
	resp, raw := embyRaw(t, server, http.MethodGet, base, token, "")
	var detail struct {
		Tags      map[string]string `json:"ImageTags"`
		Backdrops []string          `json:"BackdropImageTags"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil || resp.StatusCode != 200 || detail.Tags["Primary"] == "" || len(detail.Backdrops) != 1 {
		t.Fatalf("cached detail did not refresh artwork: %s %v", raw, err)
	}
	if len(app.movieArtwork(stored).Backdrops()) != 1 {
		t.Fatal("detail omitted still")
	}
}
