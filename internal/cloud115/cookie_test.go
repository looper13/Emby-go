package cloud115

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

const fixtureCookie = "UID=123_A1; CID=fixture-cid; SEID=fixture-seid"

func TestCookieOnlyAPIsDirectoryPaginationAndUserAgent(t *testing.T) {
	ctx := context.Background()
	var calls []string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Host+r.URL.Path)
		if r.Header.Get("Cookie") != fixtureCookie || r.Header.Get("Authorization") != "" {
			t.Fatal("wrong credentials", r.Header)
		}
		body := ""
		switch r.URL.Host + r.URL.Path {
		case "my.115.com/":
			body = `{"state":true,"data":{"user_id":"123"}}`
		case "webapi.115.com/files":
			if r.URL.Query().Get("cid") != "0" {
				t.Fatal("wrong folder")
			}
			body = `{"state":true,"cid":0,"offset":0,"count":1,"data":[{"cid":"12","pid":"0","n":"缓存-24h自动删除"}]}`
		case "webapi.115.com/files/get_info":
			body = `{"state":true,"data":[{"fid":"44","cid":"12","n":"movie.mkv","s":"100","sha":"sha","pc":"pick"}]}`
		case "lixian.115.com/lixian/":
			if r.URL.Query().Get("ac") == "task_lists" {
				if r.Method != "POST" || r.URL.Query().Get("page") == "" {
					t.Fatal("offline pagination missing")
				}
				if r.URL.Query().Get("page") == "1" {
					body = `{"state":true,"page_count":2,"tasks":[]}`
				} else {
					body = `{"state":true,"page_count":2,"tasks":[{"info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","status":2,"percentDone":100.0,"file_id":"44","wp_path_id":"12"}]}`
				}
			} else {
				r.ParseForm()
				if r.Form.Get("flag") != "0" || r.Form.Get("hash[0]") == "" {
					t.Fatal("removed files together with task")
				}
				body = `{"state":true}`
			}
		case "proapi.115.com/app/chrome/downurl":
			if r.Header.Get("User-Agent") != "Actual-Player-UA" {
				t.Fatal("lost playback UA")
			}
			r.ParseForm()
			if r.Form.Get("data") == "" || strings.Contains(r.Form.Get("data"), "pick") {
				t.Fatal("unencrypted download request")
			}
			body = `{"state":true,"data":"malformed"}`
		default:
			t.Fatal("unexpected endpoint", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})
	c := newCookieClient(fixtureCookie, transport, 0)
	account, err := c.Check(ctx, "")
	if err != nil || account != "123" {
		t.Fatal(account, err)
	}
	folder, err := EnsureCacheFolder(ctx, c)
	if err != nil || folder != "12" {
		t.Fatal(folder, err)
	}
	file, err := c.Info(ctx, "44")
	if err != nil || file.ParentID != "12" || !VideoFile(file) {
		t.Fatal(file, err)
	}
	remote, err := c.Find(ctx, strings.Repeat("a", 40))
	if err != nil || remote == nil || remote.FolderID != "12" || remote.ResultID != "44" {
		t.Fatal(remote, err)
	}
	if _, err := c.URL(ctx, "pick", "Actual-Player-UA"); err == nil {
		t.Fatal("malformed encrypted payload accepted")
	}
	if err := c.RemoveTask(ctx, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if len(calls) < 7 {
		t.Fatal(calls)
	}
	if _, ok := New(Tokens{Cookie: fixtureCookie}, nil).(*CookieClient); !ok {
		t.Fatal("Cookie did not select cookie provider")
	}
}

func TestCookieCreateCacheFolderAndDeleteBody(t *testing.T) {
	c := newCookieClient(fixtureCookie, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"state":true}`
		switch r.URL.Path {
		case "/files":
			body = `{"state":true,"cid":"0","offset":0,"count":0,"data":[]}`
		case "/files/add":
			r.ParseForm()
			if r.Form.Get("pid") != "0" || r.Form.Get("cname") != CacheFolderName {
				t.Fatal(r.Form)
			}
			body = `{"state":true,"cid":"12"}`
		case "/rb/delete":
			r.ParseForm()
			if r.Form.Get("fid[0]") != "44" {
				t.Fatal(r.Form)
			}
		default:
			t.Fatal(r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	}), 0)
	if id, err := EnsureCacheFolder(context.Background(), c); err != nil || id != "12" {
		t.Fatal(id, err)
	}
	if err := c.Delete(context.Background(), "44"); err != nil {
		t.Fatal(err)
	}
}

func TestCookieValidationErrorsRedactionAndMalformedRSA(t *testing.T) {
	for _, invalid := range []string{"", "UID=1; CID=2", fixtureCookie + "\r\nX-Evil: yes", fixtureCookie + "; UID=duplicate"} {
		if _, err := NormalizeCookie(invalid); err == nil {
			t.Fatal("invalid cookie accepted", invalid)
		}
	}
	if normalized, err := NormalizeCookie("Cookie: " + fixtureCookie + "; unrelated=ignore"); err != nil || normalized != fixtureCookie {
		t.Fatal(normalized, err)
	}
	c := newCookieClient(fixtureCookie, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"state":false,"errno":"99","error":"fixture-seid"}`)), Header: http.Header{}, Request: r}, nil
	}), 0)
	_, err := c.Check(context.Background(), "")
	var e *Error
	if !errors.As(err, &e) || !e.Auth || strings.Contains(err.Error(), "fixture-seid") {
		t.Fatal(err)
	}
	err = SafeError(&Error{Message: "fixture-seid fixture-cid " + fixtureCookie}, Tokens{Cookie: fixtureCookie})
	if strings.Contains(err.Error(), "fixture-") {
		t.Fatal("partial cookie leaked", err)
	}
	for _, payload := range []string{"", "AAAA", strings.Repeat("A", 172), base64.StdEncoding.EncodeToString(make([]byte, 128))} {
		if _, err := decodeCookiePayload(payload, [16]byte{}); err == nil {
			t.Fatal("bad RSA payload accepted")
		}
	}
}

func TestCookieEncryptedOfflineSubmissionRejectsQuotaFailure(t *testing.T) {
	c := newCookieClient(fixtureCookie, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "lixian.115.com" || r.URL.Path != "/lixianssp/" || r.URL.Query().Get("ac") != "add_task_urls" || r.URL.Query().Get("t") == "" || r.Method != "POST" {
			t.Fatal("wrong submit endpoint", r.URL)
		}
		r.ParseForm()
		if r.Form.Get("data") == "" || strings.Contains(r.Form.Encode(), "magnet") {
			t.Fatal("plaintext or missing encrypted submit")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"state":false,"errcode":10010,"error":"离线配额不足"}`)), Header: http.Header{}, Request: r}, nil
	}), 0)
	err := c.Add(context.Background(), "magnet:?xt=urn:btih:"+strings.Repeat("a", 40), "12")
	var e *Error
	if !errors.As(err, &e) || e.Retryable || !strings.Contains(err.Error(), "10010") {
		t.Fatal("submit rejection lost", err)
	}
}
