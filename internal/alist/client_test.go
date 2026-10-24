package alist

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestResolveSTRMPath(t *testing.T) {
	c, _ := New("http://alist.test/proxy", "secret")
	for _, route := range []string{"d", "p", "dav"} {
		got, err := c.Resolve("http://alist.test/proxy/" + route + "/115/ALDN%20001%23%E4%B8%AD.mp4?sign=private")
		if err != nil || got != "/115/ALDN 001#中.mp4" {
			t.Fatalf("resolve: %q %v", got, err)
		}
	}
	for _, link := range []string{
		"http://evil.test/proxy/d/file.mp4", "http://alist.test:8080/proxy/d/file.mp4", "https://alist.test/proxy/d/file.mp4",
		"http://user:pass@alist.test/proxy/d/file.mp4", "http://alist.test/proxy/d/../file.mp4",
		"http://alist.test/proxy/d/%2e%2e/file.mp4", "http://alist.test/proxy/d/a%2Fb.mp4", "http://alist.test/proxy/d/a%5Cb.mp4",
		"http://alist.test/proxy/d/", "http://alist.test/proxy/d/file.mp4/", "http://alist.test/proxy/d/a%00.mp4",
		"http://alist.test/proxy/dd/file.mp4", "http://alist.test/proxy/d//file.mp4", "http://alist.test/file.mp4",
	} {
		if _, err := c.Resolve(link); err == nil {
			t.Fatalf("accepted %s", link)
		}
	}
}

func TestAListRequestsAndFailureBoundaries(t *testing.T) {
	var requests []string
	remote := File{Name: "movie #.mp4", ID: "file-id", Size: 100, Modified: "2026-10-24T00:00:00Z"}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.Header.Get("Authorization") != "secret" || r.Header.Get("X-Emby-Token") != "" || r.Method != "POST" {
			t.Error("incorrect credentials or method")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path == "/proxy/api/fs/list" {
			if body["path"] != "/115" || body["refresh"] != true {
				t.Error("missing refresh")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": []File{remote}, "total": 1}})
		} else {
			if body["dir"] != "/115" || !reflect.DeepEqual(body["names"], []any{"movie #.mp4"}) {
				t.Errorf("unsafe remove body: %v", body)
			}
			_, _ = w.Write([]byte(`{"code":200,"data":null}`))
		}
	}))
	defer ts.Close()
	c, _ := New(ts.URL+"/proxy", "secret")
	if got, err := c.Stat(context.Background(), "/115/movie #.mp4"); err != nil || got != remote {
		t.Fatalf("stat: %v %v", got, err)
	}
	if err := c.Remove(context.Background(), "/115/movie #.mp4"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatal(requests)
	}
	for _, payload := range []string{`{"code":403,"message":"secret"}`, `{"code":200,"data":{}}`, `{"code":200,"data":{"total":1,"content":[]}}`, `{"code":200,"data":{"total":1,"content":[{"name":"movie.mp4","is_dir":true,"modified":"today"}]}}`, `bad JSON`} {
		t.Run(payload, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) }))
			defer s.Close()
			c, _ := New(s.URL, "secret")
			if _, err := c.Stat(context.Background(), "/movie.mp4"); err == nil {
				t.Fatal("invalid stat accepted")
			}
		})
	}
}

func TestAListNeverFollowsManagementRedirects(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Error("credential request followed redirect")
	}))
	defer upstream.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, upstream.URL, 307) }))
	defer s.Close()
	c, _ := New(s.URL, "secret")
	if err := c.Remove(context.Background(), "/movie.mp4"); err == nil || called {
		t.Fatal("redirect accepted")
	}
}
