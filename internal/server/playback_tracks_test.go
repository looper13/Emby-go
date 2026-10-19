package server

import (
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"emby-go/internal/subtitle"
)

const multiTrackNFO = `<movie><title>ABF-018</title><fileinfo><streamdetails>
<video><codec>h264</codec><streamindex>0</streamindex></video>
<audio><codec>aac</codec><streamindex>2</streamindex><language>jpn</language><title>日语原声</title><default>false</default></audio>
<audio><codec>ac3</codec><streamindex>5</streamindex><language>chi</language><title>国语</title><default>true</default></audio>
<subtitle><codec>subrip</codec><streamindex>7</streamindex><language>eng</language></subtitle>
</streamdetails></fileinfo></movie>`

func playbackSource(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	sources, ok := body["MediaSources"].([]any)
	if !ok || len(sources) == 0 {
		t.Fatalf("missing media source: %+v", body)
	}
	return sources[0].(map[string]any)
}

func findSubtitle(t *testing.T, source map[string]any, language string) map[string]any {
	t.Helper()
	for _, raw := range source["MediaStreams"].([]any) {
		stream := raw.(map[string]any)
		if stream["Type"] == "Subtitle" && stream["Language"] == language && stream["IsExternal"] == true {
			return stream
		}
	}
	t.Fatalf("missing external %s: %+v", language, source)
	return nil
}

func TestSubtitlesDiscoveredOnlyForDetailsAndPlayback(t *testing.T) {
	app, dir := detailTestApp(t, t.TempDir(), multiTrackNFO)
	if _, err := app.db.AddLibrary("L", dir); err != nil {
		t.Fatal(err)
	}
	var discoveries atomic.Int32
	app.discoverSubtitles = func(path string) []subtitle.File { discoveries.Add(1); return subtitle.Discover(path) }
	ts, token := detailServer(t, app)
	scanOnce(t, ts, token)
	if discoveries.Load() != 0 {
		t.Fatal("library scan discovered subtitles")
	}
	writeFile(t, filepath.Join(dir, "ABF-018.zh-CN.default.srt"), "1\n00:00:01,000 --> 00:00:03,000\n你好，世界\n")
	writeFile(t, filepath.Join(dir, "ABF-018.eng.flac"), "audio fixture")
	writeFile(t, filepath.Join(dir, "ABF-0182.eng.srt"), "wrong movie")
	for _, path := range []string{"/Items", "/Items?Fields=MediaSources", "/Users/1/Items?Fields=MediaSources", "/emby/items?fields=mediasources", "/api/admin/items"} {
		resp, _ := embyCall(t, ts, "GET", path, token, "")
		if resp.StatusCode != 200 {
			t.Fatalf("list %s: %d", path, resp.StatusCode)
		}
		if discoveries.Load() != 0 {
			t.Fatalf("list scanned subtitles: %s", path)
		}
	}
	for _, path := range []string{"/Items/1", "/Users/1/Items/1", "/emby/items/1", "/Items/1/PlaybackInfo"} {
		before := discoveries.Load()
		resp, body := embyCall(t, ts, "GET", path, token, "")
		if resp.StatusCode != 200 || discoveries.Load() != before+1 {
			t.Fatalf("detail %s: %d discoveries=%d -> %d", path, resp.StatusCode, before, discoveries.Load())
		}
		source := playbackSource(t, body)
		sub := findSubtitle(t, source, "zh-CN")
		if source["DefaultAudioStreamIndex"] != float64(2) || source["DefaultSubtitleStreamIndex"] != sub["Index"] {
			t.Fatalf("defaults: %+v", source)
		}
		if len(source["MediaStreams"].([]any)) != 6 {
			t.Fatalf("tracks missing or wrongly matched: %+v", source)
		}
		encoded, _ := json.Marshal(source["MediaStreams"])
		var streams []tsukimiMediaStream
		if err := json.Unmarshal(encoded, &streams); err != nil {
			t.Fatal(err)
		}
		assertClientStreamIndexes(t, streams)
		for i, expected := range []float64{0, 2, 5, 7} {
			if source["MediaStreams"].([]any)[i].(map[string]any)["ContainerIndex"] != expected {
				t.Fatalf("lost container index: %+v", source)
			}
		}
		delivery, _ := url.Parse(sub["DeliveryUrl"].(string))
		delivery.Path = strings.TrimSuffix(delivery.Path, ".srt") + ".vtt"
		response, data := embyRaw(t, ts, "GET", delivery.String(), "", "")
		if response.StatusCode != 200 || !strings.Contains(string(data), "WEBVTT") || !strings.Contains(string(data), "你好，世界") {
			t.Fatalf("subtitle %d %s", response.StatusCode, data)
		}
		if resp, _ := embyRaw(t, ts, "GET", delivery.Path, "", ""); resp.StatusCode != 401 {
			t.Fatalf("subtitle auth: %d", resp.StatusCode)
		}
	}
	// Detail JSON cache must see additions/deletions without an NFO write or scan.
	writeFile(t, filepath.Join(dir, "ABF-018.zh-TW.srt"), "1\n00:00:01,000 --> 00:00:03,000\n繁體\n")
	_, body := embyCall(t, ts, "GET", "/Items/1", token, "")
	findSubtitle(t, playbackSource(t, body), "zh-TW")
	if err := os.Remove(filepath.Join(dir, "ABF-018.zh-CN.default.srt")); err != nil {
		t.Fatal(err)
	}
	_, body = embyCall(t, ts, "GET", "/Items/1", token, "")
	if playbackSource(t, body)["DefaultSubtitleStreamIndex"] != float64(-1) {
		t.Fatal("deleted subtitle remained cached")
	}
	before := discoveries.Load()
	embyCall(t, ts, "GET", "/Items?Fields=MediaSources", token, "")
	if discoveries.Load() != before {
		t.Fatal("list after details discovered subtitles")
	}
	resp, body := embyCall(t, ts, "POST", "/Items/1/PlaybackInfo", token, `{"AudioStreamIndex":1,"SubtitleStreamIndex":-1}`)
	if resp.StatusCode != 200 || playbackSource(t, body)["DefaultAudioStreamIndex"] != float64(1) {
		t.Fatalf("selection: %d %+v", resp.StatusCode, body)
	}
	if resp, _ := embyCall(t, ts, "POST", "/Items/1/PlaybackInfo", token, `{"AudioStreamIndex":3}`); resp.StatusCode != 400 {
		t.Fatal("accepted missing track index")
	}
	embyRaw(t, ts, "POST", "/Sessions/Playing/Progress", token, `{"ItemId":"1","PositionTicks":20000000}`)
	_, body = embyCall(t, ts, "GET", "/Items/1/PlaybackInfo", token, "")
	if playbackSource(t, body)["ResumePositionTicks"] != float64(20000000) {
		t.Fatal("resume position missing")
	}
	if err := app.db.SetPlayed(1, true); err != nil {
		t.Fatal(err)
	}
	_, body = embyCall(t, ts, "GET", "/Items/1/PlaybackInfo", token, "")
	if playbackSource(t, body)["ResumePositionTicks"] != nil {
		t.Fatal("completed media resumes at its end")
	}
}

func TestSubtitleCDPartIsolationAndRawProbeUpgrade(t *testing.T) {
	app, dir := detailTestApp(t, t.TempDir(), multiTrackNFO)
	if err := os.Rename(filepath.Join(dir, "ABF-018.strm"), filepath.Join(dir, "ABF-018-CD1.strm")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "ABF-018-CD2.strm"), "https://media.test/cd2.mp4")
	writeFile(t, filepath.Join(dir, "ABF-018-CD1.eng.srt"), "1\n00:00:01,000 --> 00:00:02,000\nCD1\n")
	writeFile(t, filepath.Join(dir, "ABF-018-CD2.zh-CN.srt"), "1\n00:00:01,000 --> 00:00:02,000\nCD2\n")
	app.db.AddLibrary("L", dir)
	ts, token := detailServer(t, app)
	scanOnce(t, ts, token)
	resp, body := embyCall(t, ts, "GET", "/Items/part-1-2/PlaybackInfo", token, "")
	if resp.StatusCode != 200 {
		t.Fatalf("part: %d %+v", resp.StatusCode, body)
	}
	source := playbackSource(t, body)
	sub := findSubtitle(t, source, "zh-CN")
	if strings.Contains(sub["DeliveryUrl"].(string), "/1/") {
		t.Fatal("part subtitle points at main file")
	}
	if resp, data := embyRaw(t, ts, "GET", sub["DeliveryUrl"].(string), token, ""); resp.StatusCode != 200 || !strings.Contains(string(data), "CD2") {
		t.Fatalf("part subtitle: %d %s", resp.StatusCode, data)
	}
	// A saved pre-upgrade raw probe restores all audio tracks and real indexes locally.
	path := filepath.Join(dir, "ABF-018-CD1.strm")
	raw := json.RawMessage(`{"streams":[{"index":0,"codec_type":"video","codec_name":"h264"},{"index":4,"codec_type":"audio","codec_name":"aac","tags":{"language":"jpn"}},{"index":9,"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}}]}`)
	if err := saveMediaInfo(path, "http://media.test/ABF-018.mp4", raw); err != nil {
		t.Fatal(err)
	}
	_, body = embyCall(t, ts, "GET", "/Items/1/PlaybackInfo", token, "")
	streams := playbackSource(t, body)["MediaStreams"].([]any)
	if streams[1].(map[string]any)["Index"] != float64(1) || streams[2].(map[string]any)["Index"] != float64(2) || streams[1].(map[string]any)["ContainerIndex"] != float64(4) || streams[2].(map[string]any)["ContainerIndex"] != float64(9) {
		t.Fatalf("lost real probe indexes: %+v", streams)
	}
}

func TestSubtitleDeliveryCapabilitiesAndTicksRoutes(t *testing.T) {
	app, dir := detailTestApp(t, t.TempDir(), multiTrackNFO)
	app.db.AddLibrary("L", dir)
	writeFile(t, filepath.Join(dir, "ABF-018.eng.srt"), "1\n00:00:01,000 --> 00:00:03,000\noriginal\n")
	ts, token := detailServer(t, app)
	scanOnce(t, ts, token)
	_, body := embyCall(t, ts, "GET", "/Items/1", token, "")
	source := playbackSource(t, body)
	delivery := findSubtitle(t, source, "eng")["DeliveryUrl"].(string)
	u, _ := url.Parse(delivery)
	if strings.Contains(delivery, token) || u.Query().Get("ApiKey") != "" {
		t.Fatal("account credential included in cached delivery URL")
	}
	for _, prefix := range []string{"", "/emby", "/emby-lower"} {
		variant := *u
		variant.Path = strings.Replace(variant.Path, "/Stream.", "/10000000/Stream.", 1)
		if prefix == "/emby-lower" {
			variant.Path = "/emby" + strings.ToLower(variant.Path)
		} else {
			variant.Path = prefix + variant.Path
		}
		for _, method := range []string{"GET", "HEAD"} {
			resp, out := embyRaw(t, ts, method, variant.String(), "", "")
			if resp.StatusCode != 200 || method == "GET" && !strings.Contains(string(out), "original") {
				t.Fatalf("ticks delivery %s %s: %d %s", method, variant.String(), resp.StatusCode, out)
			}
		}
	}
	for _, mutate := range []func(*url.URL){
		func(u *url.URL) { u.RawQuery = "" },
		func(u *url.URL) { u.Path = strings.Replace(u.Path, "/Subtitles/4/", "/Subtitles/3/", 1) },
		func(u *url.URL) { u.Path = strings.Replace(u.Path, "/Videos/1/", "/Videos/2/", 1) },
		func(u *url.URL) {
			q := u.Query()
			q.Set("expires", "1")
			q.Set("SubtitleToken", hex.EncodeToString(app.subtitleSignature("1", "mediasource_1", "4", q.Get("revision"), "1")))
			u.RawQuery = q.Encode()
		},
	} {
		variant := *u
		mutate(&variant)
		if resp, _ := embyRaw(t, ts, "GET", variant.String(), "", ""); resp.StatusCode != 401 {
			t.Fatalf("accepted invalid delivery capability: %d %s", resp.StatusCode, variant.String())
		}
	}
	if resp, _ := embyRaw(t, ts, "GET", strings.Replace(delivery, "/Stream.", "/-1/Stream.", 1), "", ""); resp.StatusCode != 400 {
		t.Fatalf("accepted negative ticks: %d", resp.StatusCode)
	}
	// A new earlier file shifts public offsets: old capabilities must not read it.
	writeFile(t, filepath.Join(dir, "ABF-018.chi.srt"), "1\n00:00:01,000 --> 00:00:03,000\nnew file\n")
	if resp, _ := embyRaw(t, ts, "GET", delivery, "", ""); resp.StatusCode != 404 {
		t.Fatalf("old URL selected a different file: %d", resp.StatusCode)
	}
	_, body = embyCall(t, ts, "GET", "/Items/1", token, "")
	current := playbackSource(t, body)
	if source["TrackRevision"] == current["TrackRevision"] {
		t.Fatal("offset changes did not invalidate saved track choices")
	}
	if resp, out := embyRaw(t, ts, "GET", findSubtitle(t, current, "eng")["DeliveryUrl"].(string), "", ""); resp.StatusCode != 200 || !strings.Contains(string(out), "original") {
		t.Fatalf("refreshed URL: %d %s", resp.StatusCode, out)
	}
}

func TestPlaybackDefaultAudioPreservesDirectPlay(t *testing.T) {
	app, dir := detailTestApp(t, t.TempDir(), multiTrackNFO)
	app.cfg.FFmpegEnabled = true
	app.db.AddLibrary("L", dir)
	writeFile(t, filepath.Join(dir, "ABF-018.eng.flac"), "audio fixture")
	ts, token := detailServer(t, app)
	scanOnce(t, ts, token)
	for _, test := range []struct {
		path, body          string
		transcode, external bool
	}{
		{"/Items/1/PlaybackInfo", `{}`, false, false},
		{"/Items/1/PlaybackInfo", `{"AudioStreamIndex":2}`, false, false},
		{"/Items/1/PlaybackInfo?AudioStreamIndex=2", `{"AudioStreamIndex":1}`, false, false},
		{"/Items/1/PlaybackInfo", `{"AudioStreamIndex":1}`, true, false},
		{"/Items/1/PlaybackInfo?AudioStreamIndex=1", `{}`, true, false},
		{"/Items/1/PlaybackInfo", `{"AudioStreamIndex":4}`, true, true},
	} {
		resp, body := embyCall(t, ts, "POST", test.path, token, test.body)
		if resp.StatusCode != 200 {
			t.Fatalf("playback: %d %+v", resp.StatusCode, body)
		}
		source := playbackSource(t, body)
		if source["SupportsTranscoding"] != test.transcode || (source["TranscodingUrl"] != nil) != test.transcode {
			t.Fatalf("unexpected processing for %s: %+v", test.body, source)
		}
		if source["SupportsDirectPlay"] != !test.external {
			t.Fatalf("direct play: %+v", source)
		}
	}
}
