package server

import (
	"path/filepath"
	"testing"
)

func TestExternalPlaybackSourceIsAuthorizedAndNotCachedInLists(t *testing.T) {
	app, dir := detailTestApp(t, t.TempDir(), multiTrackNFO)
	app.db.AddLibrary("L", dir)
	ts, token := detailServer(t, app)
	scanOnce(t, ts, token)
	const raw = "https://media.test/movie.mp4?signature=a%2Bb&part=1"
	writeFile(t, filepath.Join(dir, "ABF-018.strm"), raw)
	for _, endpoint := range []string{"/Items/1/PlaybackInfo", "/emby/items/1/playbackinfo", "/Users/1/Items/1/PlaybackInfo"} {
		unauthorized, _ := embyCall(t, ts, "POST", endpoint, "", "")
		if unauthorized.StatusCode != 401 {
			t.Fatalf("unauthorized external URL: %d", unauthorized.StatusCode)
		}
		resp, body := embyCall(t, ts, "POST", endpoint, token, "")
		if resp.StatusCode != 200 || playbackSource(t, body)["ExternalStreamUrl"] != raw {
			t.Fatalf("external source differs from authorized redirect: %d %+v", resp.StatusCode, body)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("playback source must not be cached")
		}
	}
	// Playback-only fields must not mutate the shared single-item/list DTOs.
	_, item := embyCall(t, ts, "GET", "/Items/1", token, "")
	if playbackSource(t, item)["ExternalStreamUrl"] != nil {
		t.Fatal("external URL leaked into shared detail projection")
	}
	_, list := embyCall(t, ts, "GET", "/Items?Fields=MediaSources", token, "")
	for _, value := range list["Items"].([]any) {
		if playbackSource(t, value.(map[string]any))["ExternalStreamUrl"] != nil {
			t.Fatal("external URL leaked into list projection")
		}
	}
	writeFile(t, filepath.Join(dir, "ABF-018.strm"), "file:///private/movie.mkv")
	resp, _ := embyCall(t, ts, "POST", "/Items/1/PlaybackInfo", token, "")
	if resp.StatusCode != 404 {
		t.Fatalf("non-HTTP external source accepted: %d", resp.StatusCode)
	}
}
