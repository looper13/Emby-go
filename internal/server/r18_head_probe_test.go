package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/cloudguangya"
	"emby-go/internal/offline"
	"emby-go/internal/r18"
)

// 使用真实视频样本和隔离 HTTP 服务，验证播放器取得的内容而非只检查 302。
type r18HEADVideoProvider struct {
	*r18NativeCloudProvider
	videoURL  string
	videoSize int64
}

func (p *r18HEADVideoProvider) Info(ctx context.Context, id string) (cloud115.File, error) {
	file, err := p.r18NativeCloudProvider.Info(ctx, id)
	if err == nil && strings.HasPrefix(id, "native-file-") {
		file.Name = strings.TrimPrefix(id, "native-file-") + ".mp4"
		file.Size = p.videoSize
	}
	return file, err
}

func (p *r18HEADVideoProvider) URL(ctx context.Context, code, ua string) (string, error) {
	if _, err := p.r18NativeCloudProvider.URL(ctx, code, ua); err != nil {
		return "", err
	}
	return p.videoURL + "/" + strings.TrimPrefix(code, "native-code-") + ".mp4", nil
}

func TestR18NativeHEADFirstClientFollowsRedirectAndReadsVideo(t *testing.T) {
	video, err := os.ReadFile("testdata/probe-sample.mp4")
	if err != nil || len(video) < 256 || string(video[4:8]) != "ftyp" {
		t.Fatal("读取 MP4 测试样本失败", err)
	}
	const ua = "HEAD-First-Native-Player/1.0"
	hash := strings.Repeat("b", 40)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/"+hash+".mp4" || req.UserAgent() != ua || req.Header.Get("Range") != "bytes=0-255" {
			http.Error(w, "视频版本、播放器身份或 Range 不正确", http.StatusBadRequest)
			return
		}
		if req.Header.Get("X-Emby-Token") != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.URL.RawQuery != "" {
			http.Error(w, "视频源收到本服务凭据", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, req, "sample.mp4", time.Unix(1, 0), bytes.NewReader(video))
	}))
	t.Cleanup(cdn.Close)
	h := newGuangyaServerHarness(t)
	catalog := newR18NativeHTTPSource(h)
	p := &r18HEADVideoProvider{
		r18NativeCloudProvider: &r18NativeCloudProvider{namedCloudFixture: h.guangya, tasks: map[string]r18NativeRemoteTask{}},
		videoURL:               cdn.URL, videoSize: int64(len(video)),
	}
	h.a.guangyaOpen = func(cloudguangya.Tokens, func(cloudguangya.Tokens) error) cloud115.Provider { return p }
	h.configure(t)
	h.a.guangyaOffline = offline.NewForProvider(h.a.rootCtx, h.a.db, h.a.guangyaProvider, "光鸭", func() (string, error) {
		connection, err := h.a.offlineConnection("guangya")
		return connection.AccountID, err
	})
	t.Cleanup(h.a.guangyaOffline.Close)
	_, reader := h.reader(t, "head-first-reader")
	item, sourceID := r18ItemID(h.library.ID, h.video.ID), r18MagnetSourceID("guangya", hash)
	_, playback := h.call(t, http.MethodPost, "/emby/Items/"+item+"/PlaybackInfo", reader, map[string]any{"MediaSourceId": sourceID}, http.StatusOK)
	stream := cloudResponseSource(t, playback, "MediaSources", sourceID)["DirectStreamUrl"].(string)
	client := &http.Client{Timeout: 10 * time.Second}
	for _, prefix := range []string{"", "/emby"} {
		for _, method := range []string{http.MethodHead, http.MethodGet} {
			req, err := http.NewRequest(method, h.server.URL+prefix+stream, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("User-Agent", ua)
			req.Header.Set("Range", "bytes=0-255")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			// 首次 HEAD 必须允许播放器继续 GET，且不能把未知长度声明成空视频。
			if method == http.MethodHead && prefix == "" {
				if resp.StatusCode != http.StatusOK || len(body) != 0 || resp.Header.Get("Content-Length") != "" {
					t.Fatalf("首次 HEAD 阻止播放器继续起播：状态=%d，长度=%q，正文=%s", resp.StatusCode, resp.Header.Get("Content-Length"), body)
				}
				if p.creates.Load() != 0 || p.urlAttempts.Load() != 0 || catalog.lookups.Load() != 1 {
					t.Fatal("HEAD 未仅核验当前磁力，或提前准备了下载和直链")
				}
				continue
			}
			if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Type") != "video/mp4" || resp.Header.Get("Content-Range") != "bytes 0-255/"+strconv.Itoa(len(video)) {
				t.Fatalf("播放器未取得所选版本视频分片：状态=%d，头=%v，正文=%s", resp.StatusCode, resp.Header, body)
			}
			if method == http.MethodGet && !bytes.Equal(body, video[:256]) || method == http.MethodHead && len(body) != 0 {
				t.Fatal("播放器取得的视频字节错误，或 HEAD 含视频正文")
			}
		}
	}
	if p.creates.Load() != 1 || p.legacy.Load() != 0 || h.legacy.urls != 0 {
		t.Fatal("HEAD/GET 请求重复下载或调用了其它网盘")
	}
	connection, _ := h.a.offlineConnection("guangya")
	bindings, err := h.a.db.OfflineBindings(h.library, h.video.ID, connection.AccountID)
	if err != nil || len(bindings) != 1 || bindings[0].Task.InfoHash != hash || bindings[0].Task.Status != "ready" {
		t.Fatal("实际视频起播没有只准备所选磁力", err)
	}
}

func TestR18MagnetHEADProbeRejectsUnavailableResourcesWithoutDownload(t *testing.T) {
	for _, change := range []string{"removed", "different-number", "different-hash"} {
		t.Run(change, func(t *testing.T) {
			f := newR18MagnetPlaybackFixture(t)
			resource := f.catalog.resources["btih-"+r18PlaybackHashA]
			switch change {
			case "removed":
				delete(f.catalog.resources, resource.ID)
			case "different-number":
				resource.Number = "OTHER-001"
				f.catalog.resources[resource.ID] = resource
			case "different-hash":
				resource.Magnet = "magnet:?xt=urn:btih:" + r18PlaybackHashB
				f.catalog.resources[resource.ID] = resource
			}
			result := f.invoke(context.Background(), http.MethodHead, r18MagnetSourceID("guangya", r18PlaybackHashA))
			if result.ok || result.recorder.Code != http.StatusNotFound || f.cloud.creates.Load() != 0 || f.cloud.urlCalls.Load() != 0 {
				t.Fatalf("HEAD 未拒绝当前无效磁力，或触发下载：状态=%d", result.recorder.Code)
			}
		})
	}
}

func TestR18MagnetHEADProbeRechecksStoppedTasksAfterResourceLookup(t *testing.T) {
	for _, state := range []string{"needs_auth", "failed", "cleanup_pending", "cache-expired"} {
		t.Run(state, func(t *testing.T) {
			f := newR18MagnetPlaybackFixture(t)
			task := f.seed(t, r18PlaybackHashA, "downloading", nil, "", false)
			var updateErr error
			f.catalog.hook = func(context.Context, r18.Video, string) {
				if state == "cache-expired" {
					task.ExpiresAt = time.Now().Add(-time.Second).Unix()
				} else {
					task.Status = state
				}
				updateErr = f.app.db.UpdateOfflineTask(task, task.Version)
			}
			result := f.invoke(context.Background(), http.MethodHead, r18MagnetSourceID("guangya", r18PlaybackHashA))
			want := http.StatusServiceUnavailable
			if state == "failed" {
				want = http.StatusConflict
			}
			if updateErr != nil || result.recorder.Code != want || result.ok || f.cloud.creates.Load() != 0 || f.cloud.urlCalls.Load() != 0 {
				t.Fatalf("HEAD 未复核已停止任务：状态=%d，预期=%d，更新错误=%v", result.recorder.Code, want, updateErr)
			}
		})
	}
}
