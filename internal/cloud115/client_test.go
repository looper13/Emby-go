package cloud115

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMagnetNormalization(t *testing.T) {
	a, hash, err := NormalizeMagnet("magnet:?dn=test&xt=urn:btih:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err != nil || hash != strings.Repeat("0", 40) || !strings.Contains(a, "btih%3A"+hash) {
		t.Fatal(a, hash, err)
	}
	for _, raw := range []string{"https://example.org", "magnet:?xt=urn:btmh:123", "magnet:?xt=urn:btih:123", "magnet:?xt=urn:btih:" + strings.Repeat("a", 40) + "&xt=urn:btih:" + strings.Repeat("b", 40)} {
		if _, _, err := NormalizeMagnet(raw); err == nil {
			t.Fatal("accepted", raw)
		}
	}
}
func TestVaultPersistenceAndTamper(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".115.key")
	v, err := OpenVault(file)
	if err != nil {
		t.Fatal(err)
	}
	tokens := Tokens{"access-secret", "refresh-secret"}
	b, err := v.Seal(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), tokens.Access) {
		t.Fatal("plaintext token")
	}
	again, err := OpenVault(file)
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.Open(b)
	if err != nil || got != tokens {
		t.Fatal(got, err)
	}
	b[len(b)-1] ^= 1
	if _, err = again.Open(b); err == nil {
		t.Fatal("tamper accepted")
	}
}
func TestSDKRefreshPaginationAddErrorsAndURL(t *testing.T) {
	var requests []string
	refreshed := false
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.Path)
		body := ""
		switch r.URL.Path {
		case "/open/offline/get_task_list":
			if r.URL.Query().Get("page") == "" {
				t.Fatal("page missing from GET query")
			}
			if r.Header.Get("Authorization") == "Bearer old" {
				body = `{"state":false,"code":40100001,"message":"expired"}`
			} else if r.URL.Query().Get("page") == "1" {
				body = `{"state":true,"data":{"page_count":2,"tasks":[]}}`
			} else {
				body = `{"state":true,"data":{"page_count":2,"tasks":[{"info_hash":"0000000000000000000000000000000000000000","status":2,"file_id":"44"}]}}`
			}
		case "/open/refreshToken":
			body = `{"code":0,"data":{"access_token":"new","refresh_token":"new-refresh"}}`
		case "/open/offline/add_task_urls":
			body = `{"state":true,"data":[{"state":false,"code":123,"message":"quota denied"}]}`
		case "/open/ufile/downurl":
			if r.UserAgent() != "Player-UA" {
				t.Fatal("wrong UA", r.UserAgent())
			}
			body = `{"state":true,"data":{"55":{"pick_code":"pick","url":{"url":"https://cdn.example/video?signature=secret"}}}}`
		default:
			t.Fatal("unexpected SDK URL", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	c := newClient(Tokens{"old", "refresh"}, func(tokens Tokens) error { refreshed = tokens.Access == "new"; return nil }, transport, 0)
	remote, err := c.Find(context.Background(), strings.Repeat("0", 40))
	if err != nil || remote == nil || remote.ResultID != "44" || !refreshed {
		t.Fatal(remote, err, refreshed, requests)
	}
	err = c.Add(context.Background(), "magnet:?xt=urn:btih:"+strings.Repeat("0", 40), "folder")
	var api *Error
	if !errors.As(err, &api) || api.Retryable || !strings.Contains(err.Error(), "123") {
		t.Fatal("per-item error lost", err)
	}
	raw, err := c.URL(context.Background(), "pick", "Player-UA")
	if err != nil || !strings.Contains(raw, "cdn.example") {
		t.Fatal(raw, err)
	}
}
func TestRateLimiterCancellationAndRedaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transport := &limitedTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("called cancelled request"); return nil, nil }), next: time.Now().Add(time.Minute)}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://example.org", nil)
	if _, err := transport.RoundTrip(req); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	err := SafeError(&Error{Message: "access-secret https://cdn.example/?token=signed", Auth: true}, Tokens{"access-secret", ""})
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "signed") {
		t.Fatal(err)
	}
	c := newClient(Tokens{}, nil, http.DefaultTransport, 0)
	if err := c.lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.unlock()
	if _, err := c.Info(ctx, "file"); !errors.Is(err, context.Canceled) {
		t.Fatal("queued SDK call ignored cancellation", err)
	}
}

type discoveryFixture struct {
	InfoFile File
	Batches  map[string][]File
}

func (f *discoveryFixture) Check(context.Context, string) (string, error)     { return "1", nil }
func (f *discoveryFixture) Find(context.Context, string) (*RemoteTask, error) { return nil, nil }
func (f *discoveryFixture) Add(context.Context, string, string) error         { return nil }
func (f *discoveryFixture) Info(context.Context, string) (File, error)        { return f.InfoFile, nil }
func (f *discoveryFixture) List(_ context.Context, id string, _ int) ([]File, bool, error) {
	return f.Batches[id], false, nil
}
func (f *discoveryFixture) URL(context.Context, string, string) (string, error) { return "", nil }
func TestDiscoverKeepsCandidatesAndRejectsIncompleteScan(t *testing.T) {
	f := &discoveryFixture{InfoFile: File{ID: "root", Name: "result", Directory: true}, Batches: map[string][]File{"root": {{ID: "sub", Name: "sub", Directory: true}, {ID: "a", Name: "movie.mkv", PickCode: "a", Size: 100}, {ID: "s", Name: "sample.mp4", PickCode: "s", Size: 100}}, "sub": {{ID: "b", Name: "other.mp4", PickCode: "b", Size: 200}}}}
	files, err := Discover(context.Background(), f, "root", "work")
	if err != nil || len(files) != 2 || files[1].Path != "result/sub/other.mp4" {
		t.Fatal(files, err)
	}
	f.Batches["sub"] = []File{{ID: "root", Name: "loop", Directory: true}}
	if _, err = Discover(context.Background(), f, "root", "work"); err == nil {
		t.Fatal("cycle accepted")
	}
	if _, err = Discover(context.Background(), f, "work", "work"); err == nil {
		t.Fatal("whole workdir scan accepted")
	}
}
