package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"emby-go/internal/librarywatch"
	"emby-go/internal/scanner"
	"emby-go/internal/store"
	"emby-go/internal/subtitle"
	"github.com/gin-gonic/gin"
)

func TestTelevisionLibraryAPIHierarchyMixedTypesAndPlayback(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	var discoveries atomic.Int32
	a.discoverSubtitles = func(path string) []subtitle.File { discoveries.Add(1); return subtitle.Discover(path) }
	call := func(method, path string, body any, want int) any {
		t.Helper()
		before := discoveries.Load()
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(string(data)))
		req.Header.Set("X-Emby-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var value any
		if resp.StatusCode != 204 {
			if err = json.NewDecoder(resp.Body).Decode(&value); err != nil {
				t.Fatal(path, err)
			}
		}
		if resp.StatusCode != want {
			t.Fatalf("%s: %d %v", path, resp.StatusCode, value)
		}
		if (strings.HasSuffix(strings.ToLower(req.URL.Path), "/items") || strings.Contains(strings.ToLower(req.URL.Path), "/shows/")) && discoveries.Load() != before {
			t.Fatalf("television list discovered subtitles: %s", path)
		}
		return value
	}
	root := t.TempDir()
	show := filepath.Join(root, "Drama")
	season := filepath.Join(show, "Season 1")
	if err := os.MkdirAll(season, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(show, "tvshow.nfo"), `<tvshow><title>Drama</title><plot>Overview</plot><year>2026</year></tvshow>`)
	for i := 1; i <= 2; i++ {
		name := "Drama.S01E0" + strconv.Itoa(i)
		writeFile(t, filepath.Join(season, name+".strm"), "https://media.test/episode.mp4")
		writeFile(t, filepath.Join(season, name+".nfo"), `<episodedetails><title>Episode `+strconv.Itoa(i)+`</title><season>1</season><episode>`+strconv.Itoa(i)+`</episode><runtime>30</runtime></episodedetails>`)
	}
	writeFile(t, filepath.Join(root, "Film.strm"), "https://media.test/film.mp4")
	writeFile(t, filepath.Join(root, "Film.nfo"), `<movie><title>Film</title></movie>`)
	library := call("POST", "/api/admin/libraries", ginBody("Mixed", root, "mixed"), 200).(map[string]any)
	lib := int64(library["Id"].(float64))
	if library["Type"] != "mixed" {
		t.Fatal(library)
	}
	call("POST", "/api/admin/libraries", ginBody("Wrong", root, "tvshows"), 400)
	call("POST", "/api/admin/libraries", ginBody("Wrong", t.TempDir(), "other"), 400)
	call("POST", "/api/admin/scan?library_id="+strconv.FormatInt(lib, 10), nil, 200)
	scope := externalLibraryID(lib)
	seriesID := store.TVSeriesID(lib, show)
	seasonID := store.TVSeasonID(lib, show, 1)
	for _, prefix := range []string{"", "/emby"} {
		result := call("GET", prefix+"/Users/1/Items?ParentId="+scope, nil, 200).(map[string]any)
		if result["TotalRecordCount"] != float64(2) {
			t.Fatal(result)
		}
		types := map[string]int{}
		for _, v := range result["Items"].([]any) {
			types[v.(map[string]any)["Type"].(string)]++
		}
		if types["Movie"] != 1 || types["Series"] != 1 {
			t.Fatal(types)
		}
		for _, query := range []string{"IncludeItemTypes=Movie", "IncludeItemTypes=Series"} {
			r := call("GET", prefix+"/Users/1/Items?ParentId="+scope+"&"+query, nil, 200).(map[string]any)
			if r["TotalRecordCount"] != float64(1) {
				t.Fatal(r)
			}
		}
	}
	result := call("GET", "/shows/"+url.PathEscape(seriesID)+"/seasons?UserId=1", nil, 200).(map[string]any)
	seasons := result["Items"].([]any)
	if len(seasons) != 1 || seasons[0].(map[string]any)["Id"] != seasonID {
		t.Fatal(result)
	}
	folder := call("GET", "/Items/"+url.PathEscape(seriesID), nil, 200).(map[string]any)
	if folder["Type"] != "Series" || folder["ChildCount"] != float64(1) || folder["RecursiveItemCount"] != float64(2) {
		t.Fatal(folder)
	}
	result = call("GET", "/Shows/"+url.PathEscape(seriesID)+"/Episodes?SeasonId="+url.QueryEscape(seasonID)+"&StartIndex=1&Limit=1", nil, 200).(map[string]any)
	items := result["Items"].([]any)
	if result["TotalRecordCount"] != float64(2) || len(items) != 1 {
		t.Fatal(result)
	}
	ep := items[0].(map[string]any)
	if ep["Type"] != "Episode" || ep["IndexNumber"] != float64(2) || ep["ParentIndexNumber"] != float64(1) || ep["ParentId"] != seasonID || ep["SeriesId"] != seriesID {
		t.Fatal(ep)
	}
	call("GET", "/Shows/"+url.PathEscape(seriesID)+"/Episodes?SeasonId="+url.QueryEscape(store.TVSeasonID(lib, filepath.Join(root, "Other"), 1)), nil, 404)
	call("GET", "/Items/"+url.PathEscape(store.TVSeasonID(lib, show, 99)), nil, 404)
	id := ep["Id"].(string)
	allEpisodes, err := a.db.TVEpisodes(lib, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.db.SetPlayed(allEpisodes[0].ID, true); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"", "?ParentId=" + scope, "?SeriesId=" + url.QueryEscape(seriesID)} {
		next := call("GET", "/Shows/NextUp"+query, nil, 200).(map[string]any)
		if next["TotalRecordCount"] != float64(1) || next["Items"].([]any)[0].(map[string]any)["Id"] != id {
			t.Fatal(next)
		}
	}
	if err = a.db.SavePlayback(allEpisodes[1].ID, 10000000, 1, "", -1); err != nil {
		t.Fatal(err)
	}
	resume := call("GET", "/Users/1/Items/Resume?ParentId="+scope+"&IncludeItemTypes=Episode", nil, 200).(map[string]any)
	if resume["TotalRecordCount"] != float64(1) || resume["Items"].([]any)[0].(map[string]any)["Type"] != "Episode" {
		t.Fatal(resume)
	}
	next := call("GET", "/Shows/NextUp?ParentId="+scope, nil, 200).(map[string]any)
	if next["TotalRecordCount"] != float64(0) {
		t.Fatal("in-progress episode must use resume", next)
	}
	latest := call("GET", "/Users/1/Items/Latest?ParentId="+scope+"&IncludeItemTypes=Movie", nil, 200).([]any)
	if len(latest) != 1 || latest[0].(map[string]any)["Type"] != "Movie" {
		t.Fatal(latest)
	}
	call("GET", "/Items/"+url.PathEscape(seriesID)+"/Images", nil, 200)
	play := call("POST", "/Items/"+id+"/PlaybackInfo", nil, 200).(map[string]any)
	if len(play["MediaSources"].([]any)) != 1 {
		t.Fatal(play)
	}
	detail := call("GET", "/api/admin/items/"+id+"/detail", nil, 200).(map[string]any)
	if !detail["playable"].(bool) || detail["movie"].(map[string]any)["television"].(map[string]any)["episode"] != float64(2) {
		t.Fatal(detail)
	}
	writeFile(t, allEpisodes[1].SourcePath, "ftp://media.test/episode.mp4")
	detail = call("GET", "/api/admin/items/"+id+"/detail", nil, 200).(map[string]any)
	if detail["playable"] != false || detail["movie"].(map[string]any)["Title"] != "Episode 2" {
		t.Fatal("unplayable source lost metadata", detail)
	}
	call("POST", "/Items/"+id+"/PlaybackInfo", nil, 404)
	writeFile(t, allEpisodes[1].SourcePath, "https://media.test/restored.mp4")
	play = call("POST", "/Items/"+id+"/PlaybackInfo", nil, 200).(map[string]any)
	if len(play["MediaSources"].([]any)) != 1 {
		t.Fatal("source recovery required a rescan", play)
	}
	call("POST", "/Sessions/Playing/Progress", map[string]any{"ItemId": id, "PositionTicks": 20000000}, 204)
	stored, err := a.db.Data(allEpisodes[1].ID)
	if err != nil || stored.PositionTicks != 20000000 {
		t.Fatal("episode playback did not persist", stored, err)
	}
	result = call("GET", "/api/admin/shows?library_id="+strconv.FormatInt(lib, 10)+"&search=Drama", nil, 200).(map[string]any)
	if result["TotalRecordCount"] != float64(1) {
		t.Fatal(result)
	}
	result = call("GET", "/api/admin/items?library_id="+strconv.FormatInt(lib, 10)+"&media_type=Movie", nil, 200).(map[string]any)
	if result["total"] != float64(1) {
		t.Fatal(result)
	}
	counts := call("GET", "/Items/Counts", nil, 200).(map[string]any)
	if counts["SeriesCount"] != float64(1) || counts["EpisodeCount"] != float64(2) || counts["MovieCount"] != float64(1) {
		t.Fatal(counts)
	}
	for _, raw := range call("GET", "/Users/1/Views", nil, 200).(map[string]any)["Items"].([]any) {
		item := raw.(map[string]any)
		if item["Id"] == scope {
			if _, ok := item["CollectionType"]; ok {
				t.Fatal("mixed library declared a movie-only collection", item)
			}
		}
	}
	// A shared NFO event with unchanged timestamps refreshes descendants.
	info, err := os.Stat(filepath.Join(show, "tvshow.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(show, "tvshow.nfo"), `<tvshow><title>Other</title><plot>Overview</plot><year>2026</year></tvshow>`)
	if err = os.Chtimes(filepath.Join(show, "tvshow.nfo"), info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	l, err := a.db.Library(lib)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.refreshLibraryChanges(context.Background(), l, []librarywatch.Change{{Path: filepath.Join(show, "tvshow.nfo")}}); err != nil {
		t.Fatal(err)
	}
	eps, err := a.db.TVEpisodes(lib, "")
	if err != nil || eps[0].Television.Title != "Other" || eps[1].Television.Title != "Other" {
		t.Fatal(eps, err)
	}
	result = call("GET", "/Users/1/Items?ParentId="+scope+"&IncludeItemTypes=Movie,Series&StartIndex=1&Limit=1", nil, 200).(map[string]any)
	if result["TotalRecordCount"] != float64(2) || len(result["Items"].([]any)) != 1 {
		t.Fatal(result)
	}
	if res, err := scanner.Scan(context.Background(), a.db, l); err != nil || res.Skipped != 3 {
		t.Fatal(res, err)
	}
}

func ginBody(name, path, typ string) map[string]string {
	return map[string]string{"Name": name, "Path": path, "Type": typ}
}

func TestTelevisionSeasonNamesOrderArtworkAndNextUpTypes(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	root := t.TempDir()
	l, err := a.db.AddLibrary("TV", root, store.LibraryTVShows)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(root, "Show")
	if err := os.MkdirAll(show, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(show, "tvshow.nfo"), `<tvshow><title>Show</title></tvshow>`)
	writeJPEGImage(t, filepath.Join(show, "poster.jpg"), 40, 80)
	for _, n := range []int{0, 1, 2, 10} {
		dir := filepath.Join(show, "Season "+strconv.Itoa(n))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		writeJPEGImage(t, filepath.Join(dir, "poster.jpg"), 40, 80)
		for i, title := range []string{"Zebra", "Alpha"} {
			name := "S" + strconv.Itoa(n) + "E" + strconv.Itoa(i+1)
			writeFile(t, filepath.Join(dir, name+".strm"), "https://media.test/episode.mp4")
			writeFile(t, filepath.Join(dir, name+".nfo"), `<episodedetails><title>`+title+`</title><season>`+strconv.Itoa(n)+`</season><episode>`+strconv.Itoa(i+1)+`</episode></episodedetails>`)
		}
	}
	if result, err := scanner.Scan(context.Background(), a.db, l); err != nil || result.Success != 8 {
		t.Fatal(result, err)
	}
	get := func(path string, want int) []byte {
		t.Helper()
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		req.Header.Set("X-Emby-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
		return body
	}
	list := func(path string) []map[string]any {
		t.Helper()
		var result struct {
			Items []map[string]any
		}
		if err := json.Unmarshal(get(path, 200), &result); err != nil {
			t.Fatal(err)
		}
		return result.Items
	}
	series := store.TVSeriesID(l.ID, show)
	seasons := list("/Shows/" + series + "/Seasons?UserId=1")
	if len(seasons) != 4 {
		t.Fatal(seasons)
	}
	for i, n := range []int{0, 1, 2, 10} {
		if seasons[i]["Name"] != televisionSeasonName(n) || seasons[i]["IndexNumber"] != float64(n) {
			t.Fatal(seasons)
		}
		get("/Items/"+seasons[i]["Id"].(string)+"/Images/Primary", 200)
	}
	get("/Items/"+series+"/Images/Primary", 200)
	eps := list("/Shows/" + series + "/Episodes?Season=2")
	if len(eps) != 2 || eps[0]["Name"] != "Zebra" || eps[1]["Name"] != "Alpha" || eps[0]["SeasonName"] != "第 2 季" {
		t.Fatal(eps)
	}
	get("/Shows/"+series+"/Seasons?UserId=2", 404)
	get("/Shows/"+series+"/Episodes?Season=-1", 400)
	get("/Shows/"+store.TVSeriesID(l.ID, filepath.Join(root, "missing"))+"/Seasons", 404)
	empty, err := a.db.AddLibrary("Empty", t.TempDir(), store.LibraryTVShows)
	if err != nil {
		t.Fatal(err)
	}
	if items := list("/Users/1/Items?ParentId=" + externalLibraryID(empty.ID)); len(items) != 0 {
		t.Fatal(items)
	}
	if items := list("/api/admin/shows?library_id=" + strconv.FormatInt(empty.ID, 10)); len(items) != 0 {
		t.Fatal(items)
	}
	id, _ := strconv.ParseInt(eps[0]["Id"].(string), 10, 64)
	if err := a.db.SetPlayed(id, true); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"Episode", "Video", "Movie"} {
		items := list("/Shows/NextUp?SeriesId=" + url.QueryEscape(series) + "&IncludeItemTypes=" + typ)
		want := 1
		if typ == "Movie" {
			want = 0
		}
		if len(items) != want {
			t.Fatal(typ, items)
		}
	}
	if items := list("/Shows/NextUp?IncludeItemTypes=Movie"); len(items) != 0 {
		t.Fatal("global movie-only next-up leaked episodes", items)
	}
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request, _ = http.NewRequest("GET", "/?IncludeItemTypes=Movie", nil)
	if televisionItemMatches(ctx, gin.H{"Type": "Episode"}) {
		t.Fatal("movie-only filter accepted an episode")
	}
}

func TestTelevisionEmbyClientContractRouteVariantsAndPlayback(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, path string, body any, want int) (*http.Response, []byte) {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(string(payload)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Emby-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != want {
			t.Fatalf("%s %s: status=%d body=%s err=%v", method, path, resp.StatusCode, raw, err)
		}
		return resp, raw
	}
	decode := func(method, path string, body any, target any) {
		t.Helper()
		resp, raw := request(method, path, body, 200)
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s must not cache playback or user state", path)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(path, err)
		}
	}
	list := func(path string) []map[string]any {
		t.Helper()
		var result struct {
			Items            []map[string]any
			TotalRecordCount int
			StartIndex       int
		}
		decode("GET", path, nil, &result)
		if result.Items == nil || result.StartIndex != 0 || result.TotalRecordCount != len(result.Items) {
			t.Fatalf("invalid unpaged result: %s %+v", path, result)
		}
		return result.Items
	}
	for _, typ := range []string{store.LibraryTVShows, store.LibraryMixed} {
		root := t.TempDir()
		show := filepath.Join(root, "Show")
		season := filepath.Join(show, "Season 2")
		if err := os.MkdirAll(season, 0755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(show, "tvshow.nfo"), `<tvshow><title>Contract Show</title></tvshow>`)
		for i := 1; i <= 2; i++ {
			base := filepath.Join(season, "Show.S02E0"+strconv.Itoa(i))
			writeFile(t, base+".strm", "https://media.test/episode.mp4")
			writeFile(t, base+".nfo", `<episodedetails><title>Episode `+strconv.Itoa(i)+`</title><season>2</season><episode>`+strconv.Itoa(i)+`</episode><runtime>1</runtime></episodedetails>`)
		}
		if typ == store.LibraryMixed {
			writeFile(t, filepath.Join(root, "Film.strm"), "https://media.test/film.mp4")
			writeFile(t, filepath.Join(root, "Film.nfo"), `<movie><title>Contract Film</title></movie>`)
		}
		l, err := a.db.AddLibrary(typ, root, typ)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := scanner.Scan(context.Background(), a.db, l); err != nil {
			t.Fatal(err)
		}
		scope, seriesID, seasonID := externalLibraryID(l.ID), store.TVSeriesID(l.ID, show), store.TVSeasonID(l.ID, show, 2)
		for _, prefix := range []string{"", "/emby"} {
			for _, lower := range []bool{false, true} {
				t.Run(typ+prefix+"/lower="+strconv.FormatBool(lower), func(t *testing.T) {
					// Change static route casing only; IDs and query values stay intact.
					p := func(route string, args ...any) string {
						if lower {
							route = strings.ToLower(route)
						}
						return prefix + fmt.Sprintf(route, args...)
					}
					var folders []map[string]any
					decode("GET", p("/Library/VirtualFolders"), nil, &folders)
					found := false
					for _, folder := range folders {
						if folder["Id"] == scope {
							found = folder["ItemId"] == scope && folder["CollectionType"] == l.CollectionType()
						}
					}
					if !found {
						t.Fatal("library not discoverable with its declared content type", folders)
					}
					found = false
					for _, view := range list(p("/Users/1/Views")) {
						if view["Id"] == scope {
							found = view["CollectionType"] == l.CollectionType() || (l.CollectionType() == "" && view["CollectionType"] == nil)
						}
					}
					if !found {
						t.Fatal("library missing from user views")
					}
					series := list(p("/Users/1/Items") + "?ParentId=" + scope + "&IncludeItemTypes=Series")
					seasons := list(p("/Shows/%s/Seasons", seriesID) + "?UserId=1")
					if len(series) != 1 || series[0]["Id"] != seriesID || series[0]["Type"] != "Series" || series[0]["IsFolder"] != true || len(seasons) != 1 || seasons[0]["Id"] != seasonID || seasons[0]["Type"] != "Season" || seasons[0]["ParentId"] != seriesID {
						t.Fatal("invalid hierarchy", series, seasons)
					}
					eps := list(p("/Shows/%s/Episodes", seriesID) + "?SeasonId=" + url.QueryEscape(seasonID))
					if len(eps) != 2 {
						t.Fatal(eps)
					}
					for i, ep := range eps {
						if ep["Type"] != "Episode" || ep["IsFolder"] != false || ep["SeriesId"] != seriesID || ep["SeasonId"] != seasonID || ep["ParentId"] != seasonID || ep["ParentIndexNumber"] != float64(2) || ep["IndexNumber"] != float64(i+1) {
							t.Fatal("invalid episode identity", ep)
						}
					}
					id := eps[0]["Id"].(string)
					request("DELETE", p("/Users/1/PlayedItems/%s", id), nil, 204)
					var play struct {
						MediaSources  []map[string]any
						PlaySessionId string
					}
					for _, route := range []string{p("/Items/%s/PlaybackInfo", id), p("/Users/1/Items/%s/PlaybackInfo", id)} {
						for _, method := range []string{"GET", "POST"} {
							decode(method, route, nil, &play)
							if len(play.MediaSources) != 1 || play.PlaySessionId == "" || play.MediaSources[0]["SupportsDirectStream"] != true || play.MediaSources[0]["SupportsTranscoding"] != false {
								t.Fatal("invalid playback source", play)
							}
						}
					}
					for _, method := range []string{"GET", "HEAD"} {
						resp, _ := request(method, p("/Videos/%s/stream.mp4", id), nil, 302)
						if resp.Header.Get("Location") != "https://media.test/episode.mp4" || resp.Header.Get("Cache-Control") != "no-store" {
							t.Fatal("invalid direct-stream redirect", resp.Header)
						}
					}
					for _, phase := range []string{"", "/Progress", "/Stopped"} {
						request("POST", p("/Sessions/Playing"+phase), map[string]any{"ItemId": id, "PositionTicks": 20000000}, 204)
					}
					resume := list(p("/Users/1/Items/Resume") + "?ParentId=" + scope + "&IncludeItemTypes=Episode")
					if len(resume) != 1 || resume[0]["Id"] != id || resume[0]["UserData"].(map[string]any)["PlaybackPositionTicks"] != float64(20000000) {
						t.Fatal("playback progress missing from resume", resume)
					}
					if next := list(p("/Shows/NextUp") + "?SeriesId=" + url.QueryEscape(seriesID)); len(next) != 0 {
						t.Fatal("resume episode also appeared in next-up", next)
					}
					request("POST", p("/Users/1/PlayedItems/%s", id), nil, 204)
					if next := list(p("/Shows/NextUp") + "?SeriesId=" + url.QueryEscape(seriesID)); len(next) != 1 || next[0]["Id"] != eps[1]["Id"] {
						t.Fatal("next-up did not advance after played", next)
					}
					for _, query := range []string{"", "?IncludeItemTypes=Video", "?ParentId=" + scope + "&IncludeItemTypes=Episode"} {
						if resume := list(p("/Users/1/Items/Resume") + query); len(resume) != 0 {
							t.Fatal("played episode still in resume", resume)
						}
					}
					request("DELETE", p("/Users/1/PlayedItems/%s", id), nil, 204)
					if resume := list(p("/Users/1/Items/Resume") + "?ParentId=" + scope); len(resume) != 1 || resume[0]["Id"] != id || resume[0]["UserData"].(map[string]any)["PlaybackPositionTicks"] != float64(20000000) {
						t.Fatal("unplayed episode lost its resume position", resume)
					}
					request("POST", p("/Users/1/Items/%s/HideFromResume", id)+"?Hide=true", nil, 200)
					if resume := list(p("/Users/1/Items/Resume") + "?ParentId=" + scope); len(resume) != 0 {
						t.Fatal("hidden episode still in resume", resume)
					}
					if next := list(p("/Shows/NextUp") + "?SeriesId=" + url.QueryEscape(seriesID)); len(next) != 0 {
						t.Fatal("hidden in-progress episode advanced next-up", next)
					}
					request("POST", p("/Users/1/Items/%s/HideFromResume", id)+"?Hide=false", nil, 200)
					if resume := list(p("/Users/1/Items/Resume") + "?ParentId=" + scope); len(resume) != 1 || resume[0]["Id"] != id {
						t.Fatal("unhidden episode missing from resume", resume)
					}
					request("POST", p("/Users/1/PlayedItems/%s", id), nil, 204)
					request("GET", p("/Users/2/Items")+"?ParentId="+scope, nil, 404)
					request("GET", p("/Items/%s/PlaybackInfo", seriesID), nil, 404)
				})
			}
		}
	}
}
