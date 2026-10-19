package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emby-go/internal/probe"
)

func TestAudioPlaybackFFmpegAndSubtitleExtraction(t *testing.T) {
	program, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	root := t.TempDir()
	media := filepath.Join(root, "fixture.mkv")
	srt := filepath.Join(root, "source.srt")
	writeFile(t, srt, "1\n00:00:00,200 --> 00:00:02,500\nembedded subtitle\n")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, "-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=blue:s=160x90:r=24:d=3", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-f", "lavfi", "-i", "sine=frequency=880:duration=3", "-i", srt,
		"-map", "0:v", "-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "0:v", "-map", "3:s", "-c:v", "libx264", "-preset", "ultrafast", "-g", "24", "-c:a", "aac", "-c:s", "srt", "-metadata:s:a:0", "language=jpn", "-metadata:s:a:1", "language=eng", "-t", "3", media)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v %s", err, out)
	}
	origin := httptest.NewServer(http.FileServer(http.Dir(root)))
	defer origin.Close()
	url := origin.URL + "/fixture.mkv"
	app, dir := detailTestApp(t, filepath.Join(root, "app"), `<movie><title>Film</title><runtime>1</runtime></movie>`)
	writeFile(t, filepath.Join(dir, "ABF-018.strm"), url)
	app.cfg.FFmpegEnabled, app.cfg.FFmpegPath, app.cfg.PlaybackConcurrency = true, program, 1
	app.db.AddLibrary("L", dir)
	ts, token := detailServer(t, app)
	scanOnce(t, ts, token)
	result, err := probe.Run(ctx, ffprobe, url, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveMediaInfo(filepath.Join(dir, "ABF-018.strm"), url, result.Raw); err != nil {
		t.Fatal(err)
	}
	resp, info := embyCall(t, ts, "GET", "/Items/1/PlaybackInfo", token, "")
	if resp.StatusCode != 200 {
		t.Fatalf("playback: %+v", info)
	}
	var subtitleURL string
	for _, value := range playbackSource(t, info)["MediaStreams"].([]any) {
		stream := value.(map[string]any)
		if stream["Type"] == "Subtitle" {
			subtitleURL, _ = stream["DeliveryUrl"].(string)
			if stream["Index"] != float64(3) || stream["ContainerIndex"] != float64(5) {
				t.Fatalf("subtitle public/container index: %+v", stream)
			}
		}
	}
	if subtitleURL == "" {
		t.Fatal("embedded subtitle has no delivery URL")
	}
	if resp, out := embyRaw(t, ts, "GET", strings.Replace(subtitleURL, "/Stream.", "/10000000/Stream.", 1), "", ""); resp.StatusCode != 200 || !strings.Contains(string(out), "embedded subtitle") {
		t.Fatalf("extract: %d %s", resp.StatusCode, out)
	}
	release, ok := app.acquireMediaProcess()
	if !ok {
		t.Fatal("process permit unavailable after extraction")
	}
	if resp, _ := embyCall(t, ts, "POST", "/Videos/1/Playback", token, `{"AudioStreamIndex":2}`); resp.StatusCode != 429 {
		release()
		t.Fatal("active process permit did not limit playback")
	}
	release()
	resp, body := embyCall(t, ts, "POST", "/Videos/1/Playback", token, `{"AudioStreamIndex":2,"StartTimeTicks":0}`)
	if resp.StatusCode != 200 {
		t.Fatalf("remux start: %d %+v", resp.StatusCode, body)
	}
	session := body["SessionId"].(string)
	app.mediaMu.Lock()
	sessionDir := app.mediaSessions[session].dir
	app.mediaMu.Unlock()
	entries, _ := os.ReadDir(sessionDir)
	for _, entry := range entries {
		t.Log("session file", entry.Name())
	}
	if resp, data := embyRaw(t, ts, "GET", body["Url"].(string), "", ""); resp.StatusCode != 200 || !strings.Contains(string(data), "#EXTM3U") {
		t.Fatalf("manifest: %d %s", resp.StatusCode, data)
	}
	if resp, _ := embyRaw(t, ts, "GET", strings.Replace(body["Url"].(string), "/Videos/1/", "/Videos/999/", 1), "", ""); resp.StatusCode != 404 {
		t.Fatal("session usable for another item")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		app.mediaMu.Lock()
		count := app.mediaProcesses
		app.mediaMu.Unlock()
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completed FFmpeg held a process slot")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if resp, next := embyCall(t, ts, "POST", "/Videos/1/Playback", token, `{"AudioStreamIndex":1}`); resp.StatusCode != 200 {
		t.Fatalf("completed clip blocked the next playback: %d %+v", resp.StatusCode, next)
	} else {
		embyRaw(t, ts, "DELETE", "/Videos/1/Playback/"+next["SessionId"].(string), token, "")
	}
	// Probe the actual remuxed stream, not just its advertised metadata.
	output, err := probe.Run(ctx, ffprobe, ts.URL+body["Url"].(string), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if output.Info.Video.Codec != "h264" || len(output.Info.Audios) != 1 || output.Info.Audio.Codec != "aac" || output.Info.Audio.Language != "eng" {
		t.Fatalf("wrong remux output: %+v", output.Info)
	}
	resp, seek := embyCall(t, ts, "POST", "/Videos/1/Playback", token, `{"AudioStreamIndex":1,"StartTimeTicks":10000000,"PreviousSession":"`+session+`"}`)
	if resp.StatusCode != 200 || seek["StartTimeTicks"] != float64(10000000) {
		t.Fatalf("restart seek: %d %+v", resp.StatusCode, seek)
	}
	if resp, _ := embyRaw(t, ts, "GET", body["Url"].(string), "", ""); resp.StatusCode != 404 {
		t.Fatal("old session still usable")
	}
	if resp, _ := embyRaw(t, ts, "DELETE", "/Videos/1/Playback/"+seek["SessionId"].(string), token, ""); resp.StatusCode != 204 {
		t.Fatal("stop failed")
	}
	if resp, _ := embyRaw(t, ts, "GET", seek["Url"].(string), "", ""); resp.StatusCode != 404 {
		t.Fatal("stopped session still usable")
	}
	app.mediaMu.Lock()
	count := app.mediaProcesses
	app.mediaMu.Unlock()
	if count != 0 {
		t.Fatalf("leaked media processes: %d", count)
	}
	// Local audio is matched only on details; selection combines it with video.
	flac := filepath.Join(dir, "ABF-018.eng.flac")
	if out, err := exec.CommandContext(ctx, program, "-v", "error", "-y", "-i", media, "-map", "0:a:1", "-c:a", "flac", flac).CombinedOutput(); err != nil {
		t.Fatalf("external fixture: %v %s", err, out)
	}
	_, info = embyCall(t, ts, "GET", "/Items/1/PlaybackInfo", token, "")
	index := -1
	for _, value := range playbackSource(t, info)["MediaStreams"].([]any) {
		track := value.(map[string]any)
		if track["Type"] == "Audio" && track["IsExternal"] == true {
			index = int(track["Index"].(float64))
		}
	}
	if index < 0 {
		t.Fatal("external audio missing")
	}
	resp, body = embyCall(t, ts, "POST", "/Videos/1/Playback", token, fmt.Sprintf(`{"AudioStreamIndex":%d}`, index))
	if resp.StatusCode != 200 {
		t.Fatalf("external audio: %d %+v", resp.StatusCode, body)
	}
	if _, err := probe.Run(ctx, ffprobe, ts.URL+body["Url"].(string), 15*time.Second); err != nil {
		t.Fatalf("external audio output: %v", err)
	}
	embyRaw(t, ts, "DELETE", "/Videos/1/Playback/"+body["SessionId"].(string), token, "")
	// Emby-compatible master URLs reuse sessions when a client seeks.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, start := range []string{"0", "10000000"} {
		request, _ := http.NewRequest("GET", ts.URL+"/Videos/1/master.m3u8?AudioStreamIndex=2&PlaySessionId=client-session&StartTimeTicks="+start, nil)
		request.Header.Set("X-Emby-Token", token)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 302 || !strings.Contains(response.Header.Get("Location"), "/hls/") {
			t.Fatalf("master URL: %+v", response)
		}
	}
	// Keep the raw cache shape backwards compatible.
	var raw map[string]any
	if json.Unmarshal(result.Raw, &raw) != nil {
		t.Fatal("invalid raw probe")
	}
}

func TestAudioPlaybackProcessPermitLifetime(t *testing.T) {
	app, _ := detailTestApp(t, t.TempDir(), multiTrackNFO)
	app.cfg.PlaybackConcurrency = 1
	release, ok := app.acquireMediaProcess()
	if !ok {
		t.Fatal("first permit unavailable")
	}
	t.Cleanup(release)
	if extra, ok := app.acquireMediaProcess(); ok {
		extra()
		t.Fatal("active process exceeded concurrency limit")
	}
	release()
	release() // Process exit and session cleanup may both release the same permit.
	next, ok := app.acquireMediaProcess()
	if !ok {
		t.Fatal("released permit unavailable")
	}
	next()
	app.rootCancel()
	if extra, ok := app.acquireMediaProcess(); ok {
		extra()
		t.Fatal("acquired permit during shutdown")
	}
}
