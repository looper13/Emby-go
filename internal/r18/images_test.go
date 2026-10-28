package r18

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

type imageRoundTripFunc func(*http.Request) (*http.Response, error)

func (f imageRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestR18ImageTargetsAndRequestIsolation(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "::1", "0.0.0.0", "10.1.2.3", "192.168.1.2", "172.16.0.1", "169.254.169.254", "100.100.100.200", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "224.0.0.1", "64:ff9b::a00:1", "2002:7f00:1::1"} {
		if publicImageIP(net.ParseIP(s)) {
			t.Fatal("non-public address accepted", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "2606:4700::1111"} {
		if !publicImageIP(net.ParseIP(s)) {
			t.Fatal("public address rejected", s)
		}
	}
	req, err := ImageRequest(context.Background(), "https://images.example/cover.jpg")
	if err != nil || req.Header.Get("User-Agent") == "" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.Header.Get("X-Emby-Token") != "" {
		t.Fatal("image request inherited credentials", err)
	}
	for _, raw := range []string{"file:///cover.jpg", "https://user:secret@example.com/a", "javascript:alert(1)"} {
		if _, err := ImageRequest(context.Background(), raw); err == nil {
			t.Fatal("invalid source accepted", raw)
		}
	}
	called := 0
	transport := imageTransport{
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, nil
		},
		base: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			called++
			target, ok := req.Context().Value(imageTargetKey{}).(imageTarget)
			if !ok || target.host != "images.example" || len(target.ips) != 1 {
				t.Fatal("public DNS result not pinned", target)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("image")), Header: http.Header{}}, nil
		}),
	}
	if _, err := transport.RoundTrip(req); err == nil || called != 0 {
		t.Fatal("mixed public/private DNS reached upstream")
	}
	transport.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	resp, err := transport.RoundTrip(req)
	if err != nil || called != 1 {
		t.Fatal("public source failed", err)
	}
	resp.Body.Close()
}

func TestR18ImageHostNormalization(t *testing.T) {
	for _, raw := range []string{"https://IMAGES.EXAMPLE/a.jpg", "http://8.8.8.8/a.jpg", "https://[2606:4700::1111]/a.jpg", "https://b\u00fccher.example/a.jpg"} {
		t.Run(raw, func(t *testing.T) {
			transport := imageTransport{
				lookup: func(context.Context, string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
				},
				base: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					target := req.Context().Value(imageTargetKey{}).(imageTarget)
					if target.host != req.URL.Hostname() || strings.Contains(req.URL.Host, "\u00fc") {
						t.Fatal("normalized transport hostname differs from pinned host", req.URL, target)
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("image")), Header: http.Header{}}, nil
				}),
			}
			req, _ := ImageRequest(context.Background(), raw)
			resp, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		})
	}
}

func TestR18ImageRedirectsRevalidateTargets(t *testing.T) {
	client := NewImageClient()
	calls := 0
	client.Transport = imageTransport{
		lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
			if host == "private.example" {
				return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
			}
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		},
		base: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://private.example/image.jpg"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}),
	}
	req, _ := ImageRequest(context.Background(), "https://images.example/image.jpg")
	if _, err := client.Do(req); err == nil || calls != 1 {
		t.Fatal("private redirect fetched", err, calls)
	}
	client.Transport = imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://images.example/loop"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})
	if _, err := client.Do(req); err == nil {
		t.Fatal("unbounded redirects")
	}
}
