package server

import (
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/anime"
	"emby-go/internal/cache"
	"emby-go/internal/config"
	"emby-go/internal/store"
)

func TestAnimeMixedLibraryPlaybackContract(t *testing.T) {
	root := t.TempDir()
	a, err := newApp(config.Config{DBPath: filepath.Join(root, "db")}, cache.NewMemory(64))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err = a.db.SaveAccessToken("anime-test"); err != nil {
		t.Fatal(err)
	}
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(206)
		w.Write(append([]byte{0, 0, 0, 24}, []byte("ftypisom")...))
	}))
	defer media.Close()
	l, err := a.db.AddLibrary("source", root)
	if err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(root, "poster.jpg")
	f, err := os.Create(poster)
	if err != nil {
		t.Fatal(err)
	}
	if err = jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 3)), nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	makeMedia := func(name string) int64 {
		p := filepath.Join(root, name+".strm")
		if err := os.WriteFile(p, []byte(media.URL+"/"+name+".mp4"), 0600); err != nil {
			t.Fatal(err)
		}
		id, err := a.db.UpsertMovie(store.Movie{LibraryID: l.ID, SourcePath: p, Title: name, Status: "success", PosterPath: poster, RuntimeSeconds: 1000}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	one, two := makeMedia("episode"), makeMedia("film")
	sid, err := a.db.SaveAnimeSubject(context.Background(), anime.Subject{BangumiID: 101, Name: "TV", Platform: "TV"}, []anime.Episode{{BangumiID: 201, Name: "第一集", Sort: json.Number("1"), Season: 1}, {BangumiID: 202, Name: "SP", Sort: json.Number("1.5"), Season: 0}})
	if err != nil {
		t.Fatal(err)
	}
	film, err := a.db.SaveAnimeSubject(context.Background(), anime.Subject{BangumiID: 102, Name: "Film", Platform: "剧场版"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	eps, err := a.db.AnimeEpisodes(sid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.BindAnime(sid, eps[0].ID, one, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.BindAnime(film, 0, two, 0); err != nil {
		t.Fatal(err)
	}
	epID, filmID := anime.ID("episode", eps[0].ID), anime.ID("movie", film)
	for _, id := range []string{epID, filmID} {
		if err = a.checkAnimeMedia(context.Background(), id, true); err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Emby-Token", "anime-test")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	obj := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var v map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	builtin, _ := a.db.AnimeLibrary()
	view := obj(request("GET", "/Items/"+externalLibraryID(builtin.ID), "", 200))
	if _, set := view["CollectionType"]; set {
		t.Fatal("mixed library must use unset content type")
	}
	list := obj(request("GET", "/Users/1/Items?ParentId="+externalLibraryID(builtin.ID), "", 200))
	if list["TotalRecordCount"] != float64(2) {
		t.Fatal(list)
	}
	seriesID := anime.ID("series", sid)
	seasons := obj(request("GET", "/emby/shows/"+seriesID+"/seasons", "", 200))
	if seasons["TotalRecordCount"] != float64(1) {
		t.Fatal(seasons)
	}
	list = obj(request("GET", "/Shows/"+seriesID+"/Episodes", "", 200))
	if list["TotalRecordCount"] != float64(1) {
		t.Fatal(list)
	}
	for _, id := range []string{seriesID, anime.SeasonID(sid, 1), epID, filmID} {
		item := obj(request("GET", "/Items/"+id, "", 200))
		if item["Id"] != id {
			t.Fatal(item)
		}
		request("GET", "/Items/"+id+"/Images/Primary", "", 200)
	}
	for _, prefix := range []string{"", "/emby"} {
		for _, id := range []string{epID, filmID} {
			info := obj(request("GET", prefix+"/items/"+id+"/playbackinfo", "", 200))
			if len(info["MediaSources"].([]any)) != 1 {
				t.Fatal(info)
			}
			w := request("HEAD", prefix+"/videos/"+id+"/stream", "", 302)
			if !strings.HasPrefix(w.Header().Get("Location"), media.URL) {
				t.Fatal(w.Header())
			}
		}
	}
	request("POST", "/emby/sessions/playing/stopped", `{"ItemId":"`+epID+`","PositionTicks":12345}`, 204)
	d, err := a.db.Data(one)
	if err != nil || d.PositionTicks != 12345 || d.PlayCount != 1 {
		t.Fatal(d, err)
	}
	request("POST", "/Users/1/FavoriteItems/"+epID, "", 200)
	d, _ = a.db.Data(one)
	if !d.IsFavorite {
		t.Fatal("split favorite state")
	}
	request("POST", "/Users/1/PlayedItems/"+epID, "", 204)
	d, _ = a.db.Data(one)
	if !d.Played {
		t.Fatal("split played state")
	}
	// A complete second refresh adds an episode without changing published IDs.
	_, err = a.db.SaveAnimeSubject(context.Background(), anime.Subject{BangumiID: 101, Name: "TV", Platform: "TV"}, []anime.Episode{{BangumiID: 201, Name: "第一集", Sort: "1", Season: 1}, {BangumiID: 203, Name: "第二集", Sort: "2", Season: 1}})
	if err != nil {
		t.Fatal(err)
	}
	nextEps, _ := a.db.AnimeEpisodes(sid)
	nextMedia := makeMedia("episode-two")
	if _, err = a.db.BindAnime(sid, nextEps[1].ID, nextMedia, 0); err != nil {
		t.Fatal(err)
	}
	nextID := anime.ID("episode", nextEps[1].ID)
	if err = a.checkAnimeMedia(context.Background(), nextID, true); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]int{
		"IncludeItemTypes=Movie,Series&Recursive=true": 2,
		"IncludeItemTypes=Series&Recursive=true":       1,
		"IncludeItemTypes=Episode&Recursive=true":      2,
		"IncludeItemTypes=Movie":                       1,
		"IncludeItemTypes=Movie,Series&SearchTerm=TV":  1,
	} {
		result := obj(request("GET", "/Users/1/Items?"+query, "", 200))
		if result["TotalRecordCount"] != float64(want) {
			t.Fatal(query, result)
		}
	}
	nextUp := obj(request("GET", "/Shows/NextUp?UserId=1", "", 200))
	if nextUp["TotalRecordCount"] != float64(1) || nextUp["Items"].([]any)[0].(map[string]any)["Id"] != nextID {
		t.Fatal(nextUp)
	}
	request("POST", "/Sessions/Playing/Progress", `{"ItemId":"`+nextID+`","PositionTicks":100000000}`, 204)
	nextUp = obj(request("GET", "/Shows/NextUp?UserId=1", "", 200))
	if nextUp["TotalRecordCount"] != float64(0) {
		t.Fatal("in-progress episode belongs in Resume", nextUp)
	}
	resume := obj(request("GET", "/Users/1/Items/Resume?ParentId="+externalLibraryID(builtin.ID), "", 200))
	if resume["TotalRecordCount"] != float64(1) || resume["Items"].([]any)[0].(map[string]any)["Id"] != nextID {
		t.Fatal(resume)
	}
	// A binding replacement changes the source of user state, never copies it.
	replacement := makeMedia("replacement")
	binding, _ := a.db.AnimeBinding(sid, nextEps[1].ID)
	binding, err = a.db.BindAnime(sid, nextEps[1].ID, replacement, binding.Revision)
	if err != nil {
		t.Fatal(err)
	}
	previous, _ := a.db.Data(nextMedia)
	fresh, _ := a.db.Data(replacement)
	if previous.PositionTicks != 100000000 || fresh.PositionTicks != 0 {
		t.Fatal("replacement copied or cleared history")
	}
	if err = a.db.DeleteMovie(replacement); err != nil {
		t.Fatal(err)
	}
	binding, _ = a.db.AnimeBinding(sid, nextEps[1].ID)
	if binding.MovieID != 0 || binding.Status != "unbound" {
		t.Fatal("deleted media retained a binding", binding)
	}
	request("GET", "/Shows/anime-series:999999/Seasons", "", 404)
	request("GET", "/Shows/"+seriesID+"/Episodes?SeasonId="+anime.SeasonID(film, 1), "", 404)
	idsResult := obj(request("GET", "/Items?Ids="+epID+",anime-episode:999999", "", 200))
	if idsResult["TotalRecordCount"] != float64(1) {
		t.Fatal(idsResult)
	}
	request("GET", "/Items/anime-episode:999999/PlaybackInfo", "", 404)
	request("POST", "/Sessions/Playing", `{"ItemId":"anime-series:`+strconv.FormatInt(sid, 10)+`","PositionTicks":999}`, 404)
	// A changed source disappears immediately without waiting for check TTL.
	m, _ := a.db.Movie(one)
	if err = os.WriteFile(m.SourcePath, []byte(media.URL+"/changed.mp4"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.animePlayable(epID); ok {
		t.Fatal("changed source used stale availability")
	}
	if _, err = a.db.UpdateAnimeLibrary(builtin.Name, false, builtin.Revision); err != nil {
		t.Fatal(err)
	}
	request("GET", "/Items/"+filmID+"/PlaybackInfo", "", 404)
}

func BenchmarkAnimeWarmCatalog(b *testing.B) {
	a, err := newApp(config.Config{DBPath: filepath.Join(b.TempDir(), "db")}, cache.NewMemory(64))
	if err != nil {
		b.Fatal(err)
	}
	defer a.Close()
	entries := make([]anime.Entry, 2000)
	for i := range entries {
		entries[i] = anime.Entry{ID: int64(i + 1), Name: "Catalog " + strconv.Itoa(i), Year: 2026, Season: 4, Weekday: i % 9}
	}
	if err = a.db.SaveAnimeCatalog(context.Background(), entries); err != nil {
		b.Fatal(err)
	}
	if err = a.db.SetAnimeSyncState("catalog:2026:4", 1, "success", ""); err != nil {
		b.Fatal(err)
	}
	if err = a.db.SaveAccessToken("benchmark"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/admin/anime/catalog?year=2026&season=4&limit=50", nil)
		req.Header.Set("X-Emby-Token", "benchmark")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		if w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}
