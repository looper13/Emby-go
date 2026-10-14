package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/anime/catalog"
)

func waitArchiveJob(t *testing.T, a *App) *archiveJob {
	t.Helper()
	a.anime.mu.Lock()
	j := a.archiveJob
	a.anime.mu.Unlock()
	if j == nil {
		t.Fatal("archive job not started")
	}
	select {
	case <-j.done:
	case <-time.After(5 * time.Second):
		t.Fatal("archive job did not complete")
	}
	return j
}

func TestAnimeArchiveDownloadUpdateLifecycle(t *testing.T) {
	a, _, _ := animeFixtureApp(t)
	if err := a.db.SaveAccessToken("catalog-test"); err != nil {
		t.Fatal(err)
	}
	data, m := animeArchiveFixture(t)
	manifest, _ := json.Marshal(m)
	var downloads atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	a.archiveClient.Transport = animeTransport(func(r *http.Request) (*http.Response, error) {
		body := manifest
		if r.URL.String() == catalog.ManifestURL {
			select {
			case <-entered:
			default:
				close(entered)
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		} else if r.URL.String() == m.URL {
			downloads.Add(1)
			body = data
		} else {
			t.Errorf("unexpected archive URL: %s", r.URL)
			return nil, context.Canceled
		}
		return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})
	first := animeRequest(t, a, "POST", "/api/admin/anime/archive", "", 202)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("manifest request did not start")
	}
	duplicate := animeRequest(t, a, "POST", "/api/admin/anime/archive", "", 202)
	if first.Body.String() != duplicate.Body.String() {
		t.Fatal("duplicate click started another task")
	}
	progress := animeRequest(t, a, "GET", "/api/admin/anime/archive", "", 200)
	if !strings.Contains(progress.Body.String(), `"running":true`) {
		t.Fatal(progress.Body.String())
	}
	close(release)
	j := waitArchiveJob(t, a)
	if j.Status != "success" || j.UpToDate || downloads.Load() != 1 {
		t.Fatalf("first download: %+v, count=%d", j, downloads.Load())
	}
	snap := a.archive.Snapshot()
	if snap.Subjects != 1 || snap.Episodes != 1 {
		t.Fatal(snap)
	}
	items := animeRequest(t, a, "GET", "/api/admin/anime/bangumi", "", 200)
	if !strings.Contains(items.Body.String(), "Archive series") {
		t.Fatal(items.Body.String())
	}
	animeRequest(t, a, "POST", "/api/admin/anime/archive", "", 202)
	j = waitArchiveJob(t, a)
	if j.Status != "success" || !j.UpToDate || downloads.Load() != 1 || a.archive.Snapshot().File != snap.File {
		t.Fatalf("unnecessary redownload: %+v", j)
	}
	result := animeRequest(t, a, "GET", "/api/admin/anime/archive", "", 200)
	if !strings.Contains(result.Body.String(), `"up_to_date":true`) {
		t.Fatal(result.Body.String())
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(a.configSnapshot().DBPath), "anime-catalog", "download-*.zip"))
	if err != nil || len(files) != 0 {
		t.Fatal("temporary download not removed", files, err)
	}
}

type archiveCancelBody struct {
	ctx     context.Context
	entered chan struct{}
}

func (b archiveCancelBody) Read([]byte) (int, error) {
	close(b.entered)
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b archiveCancelBody) Close() error { return nil }

func TestAnimeArchiveCancelKeepsSnapshotAndCanRetry(t *testing.T) {
	for _, stage := range []string{"manifest-request", "manifest-body", "download-request", "download-body"} {
		t.Run(stage, func(t *testing.T) {
			a, _, _ := animeFixtureApp(t)
			if err := a.db.SaveAccessToken("catalog-test"); err != nil {
				t.Fatal(err)
			}
			importAnimeFixture(t, a)
			before := a.archive.Snapshot()
			data, m := animeArchiveFixture(t)
			m.Digest = "sha256:" + strings.Repeat("0", 64)
			manifest, _ := json.Marshal(m)
			entered := make(chan struct{})
			a.archiveClient.Transport = animeTransport(func(r *http.Request) (*http.Response, error) {
				body, phase := manifest, "manifest"
				if r.URL.String() != catalog.ManifestURL {
					body, phase = data, "download"
				}
				if stage == phase+"-request" {
					close(entered)
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				var reader io.ReadCloser = io.NopCloser(bytes.NewReader(body))
				if stage == phase+"-body" {
					reader = archiveCancelBody{r.Context(), entered}
				}
				return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Body: reader, Header: http.Header{}, Request: r}, nil
			})
			animeRequest(t, a, "POST", "/api/admin/anime/archive", "", 202)
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not reach cancellation stage")
			}
			animeRequest(t, a, "POST", "/api/admin/anime/archive/cancel", "", 204)
			j := waitArchiveJob(t, a)
			if j.Status != "cancelled" || j.Error != "" || a.archive.Snapshot().File != before.File {
				t.Fatalf("cancelled update changed snapshot: %+v", j)
			}
			files, err := filepath.Glob(filepath.Join(filepath.Dir(a.configSnapshot().DBPath), "anime-catalog", "download-*.zip"))
			if err != nil || len(files) != 0 {
				t.Fatal("partial download not removed", files, err)
			}
			manifest, _ = json.Marshal(before.Manifest)
			a.archiveClient.Transport = animeTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(manifest)), Header: http.Header{}, Request: r}, nil
			})
			animeRequest(t, a, "POST", "/api/admin/anime/archive", "", 202)
			if j = waitArchiveJob(t, a); j.Status != "success" || !j.UpToDate {
				t.Fatalf("could not retry: %+v", j)
			}
		})
	}
}
