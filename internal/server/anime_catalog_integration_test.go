package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emby-go/internal/anime"
	"emby-go/internal/anime/catalog"
	"emby-go/internal/store"
)

func importAnimeFixture(t *testing.T, a *App) {
	t.Helper()
	data, m := animeArchiveFixture(t)
	p := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.archive.Import(context.Background(), p, m, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func animeArchiveFixture(t *testing.T) ([]byte, catalog.Manifest) {
	t.Helper()
	files := map[string]string{
		"subject.jsonlines":            `{"id":10,"type":2,"name":"Archive series","platform":1,"date":"2026-10-01"}`,
		"episode.jsonlines":            `{"id":100,"subject_id":10,"name":"New episode title","sort":1,"type":0}`,
		"character.jsonlines":          `{"id":7,"name":"Character"}`,
		"person.jsonlines":             `{"id":7,"name":"Person"}`,
		"subject-characters.jsonlines": `{"subject_id":10,"character_id":7,"type":1,"order":1}`,
		"subject-persons.jsonlines":    `{"subject_id":10,"person_id":7,"position":4}`,
		"person-characters.jsonlines":  `{"subject_id":10,"person_id":7,"character_id":7}`,
		"subject-relations.jsonlines":  `{"subject_id":10,"related_subject_id":99,"relation_type":1}`,
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, body := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(body + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	m := catalog.Manifest{Name: "dump-test.zip", URL: "https://github.com/bangumi/Archive/releases/download/archive/dump-test.zip", Size: int64(b.Len()), Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(b.Bytes())), CreatedAt: "2030-01-01T00:00:00Z"}
	return b.Bytes(), m
}
func animeRequest(t *testing.T, a *App, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Emby-Token", "catalog-test")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: %d expected %d: %s", method, path, w.Code, status, w.Body.String())
	}
	return w
}
func TestAnimeArchiveCanonicalKeepsBindingAndUserState(t *testing.T) {
	a, _, _ := animeFixtureApp(t)
	if err := a.db.SaveAccessToken("catalog-test"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sid, err := a.db.SaveAnimeSubject(ctx, anime.Subject{BangumiID: 10, Name: "Existing", Platform: "TV"}, []anime.Episode{{BangumiID: 100, Name: "Old", Sort: json.Number("1"), Season: 1}})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := a.db.AnimeEpisodes(sid)
	lib, err := a.db.AddLibrary("local", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mid, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, "episode.strm"), Title: "Local", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.BindAnime(sid, eps[0].ID, mid, 0); err != nil {
		t.Fatal(err)
	}
	if err = a.db.SavePlayback(mid, 12345, 2, "2026-10-01", -1); err != nil {
		t.Fatal(err)
	}
	importAnimeFixture(t, a)
	w := animeRequest(t, a, "GET", "/api/admin/anime/bangumi?year=2026&season=4", "", 200)
	if !strings.Contains(w.Body.String(), "Archive series") {
		t.Fatal(w.Body.String())
	}
	w = animeRequest(t, a, "GET", "/api/admin/anime/subjects/10", "", 200)
	var result struct {
		Subject  anime.Subject `json:"subject"`
		Episodes []struct {
			Episode anime.Episode `json:"episode"`
			Binding anime.Binding `json:"binding"`
		} `json:"episodes"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Subject.ID != sid || result.Episodes[0].Episode.ID != eps[0].ID || result.Episodes[0].Episode.Index != eps[0].Index || result.Episodes[0].Binding.MovieID != mid {
		t.Fatal(result)
	}
	data, err := a.db.Data(mid)
	if err != nil || data.PositionTicks != 12345 || data.PlayCount != 2 {
		t.Fatal(data, err)
	}
	animeRequest(t, a, "GET", "/api/admin/anime/subjects/999999", "", 404)
}
func TestAnimeTMDBHTTPMappingAndTypedArtwork(t *testing.T) {
	a, _, _ := animeFixtureApp(t)
	if err := a.db.SaveAccessToken("catalog-test"); err != nil {
		t.Fatal(err)
	}
	importAnimeFixture(t, a)
	sub, err := a.loadBangumiSubject(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := a.db.AnimeEpisodes(sub.ID)
	var pngData bytes.Buffer
	if err = png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	a.anime.client.HTTP.Transport = animeTransport(func(r *http.Request) (*http.Response, error) {
		body := pngData.Bytes()
		if r.URL.Host == "api.bgm.tv" {
			id := "7"
			if strings.Contains(r.URL.Path, "subjects") {
				id = "10"
			}
			body = []byte(`{"id":` + id + `,"images":{"medium":"https://lain.bgm.tv/pic/crt/l/test.png","common":"https://lain.bgm.tv/pic/cover/c/test.png"}}`)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})
	for _, kind := range []string{"person", "character"} {
		w := animeRequest(t, a, "GET", "/api/anime/people/"+kind+"/7/image", "", 200)
		if w.Header().Get("Content-Type") != "image/png" {
			t.Fatal(w.Header())
		}
	}
	animeRequest(t, a, "GET", "/api/anime/people/subject/7/image", "", 404)
	animeRequest(t, a, "GET", "/api/anime/subjects/10/image?size=bad", "", 400)
	w := animeRequest(t, a, "PUT", "/api/admin/anime/tmdb/settings", `{"api_key":"fixture-secret","revision":1}`, 200)
	if strings.Contains(w.Body.String(), "fixture-secret") {
		t.Fatal("credential leaked")
	}
	client := anime.NewTMDBClient("fixture-secret")
	client.HTTP.Transport = animeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("api_key") != "fixture-secret" || r.Header.Get("Authorization") != "" {
			t.Error("missing auth")
		}
		body := `{"id":55,"name":"TV","seasons":[{"season_number":0,"name":"Specials"}]}`
		if strings.HasSuffix(r.URL.Path, "/season/0") {
			body = `{"season_number":0,"episodes":[{"id":555,"season_number":0,"episode_number":3,"name":"Special"}]}`
		}
		if strings.HasSuffix(r.URL.Path, "/images") {
			body = `{"id":555,"stills":[{"file_path":"/still.png"}]}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	a.anime.tmdb = client
	animeRequest(t, a, "PUT", "/api/admin/anime/subjects/10/tmdb", `{"series_id":55,"revision":0}`, 200)
	path := fmt.Sprintf("/api/admin/anime/episodes/%d/tmdb", eps[0].ID)
	animeRequest(t, a, "PUT", path, `{"season":0,"number":1,"series_revision":1,"revision":0}`, 400)
	animeRequest(t, a, "PUT", path, `{"season":0,"number":3,"series_revision":1,"revision":0}`, 200)
	imagePath := fmt.Sprintf("/api/anime/episodes/%d/stills/1/0", eps[0].ID)
	animeRequest(t, a, "GET", imagePath, "", 200)
	animeRequest(t, a, "DELETE", path, `{"series_revision":1,"revision":1}`, 200)
	animeRequest(t, a, "GET", imagePath, "", 404)
	w = animeRequest(t, a, "GET", "/api/admin/anime/subjects/10", "", 200)
	if !strings.Contains(w.Body.String(), `"stills":[]`) {
		t.Fatal(w.Body.String())
	}
}

func TestAnimeArtworkBatchCancelAndBounds(t *testing.T) {
	a, _, _ := animeFixtureApp(t)
	if err := a.db.SaveAccessToken("catalog-test"); err != nil {
		t.Fatal(err)
	}
	importAnimeFixture(t, a)
	entered, release := make(chan struct{}), make(chan struct{})
	a.anime.client.HTTP.Transport = animeTransport(func(r *http.Request) (*http.Response, error) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-release:
		}
		return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("missing")), Request: r}, nil
	})
	defer close(release)
	animeRequest(t, a, "POST", "/api/admin/anime/artwork", `{"subject_ids":[]}`, 400)
	animeRequest(t, a, "POST", "/api/admin/anime/artwork", `{"subject_ids":[999]}`, 404)
	animeRequest(t, a, "POST", "/api/admin/anime/artwork", `{"subject_ids":[10]}`, 202)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("image worker did not start")
	}
	animeRequest(t, a, "POST", "/api/admin/anime/artwork", `{"subject_ids":[10]}`, 409)
	animeRequest(t, a, "POST", "/api/admin/anime/artwork/cancel", `{}`, 204)
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.anime.mu.Lock()
		done := !a.artworkJob.Running
		status := a.artworkJob.Status
		a.anime.mu.Unlock()
		if done {
			if status != "cancelled" {
				t.Fatal(status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	animeRequest(t, a, "GET", "/api/admin/anime/artwork", "", 200)
}
