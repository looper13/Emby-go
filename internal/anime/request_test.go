package anime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAnimeSourceHTTPBounds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		retryAfter string
		body       string
		want       string
	}{
		{"forbidden", 403, "", "forbidden", "source_http_403"},
		{"long cooldown", 429, "120", "limited", "source_http_429"},
		{"oversized", 200, "", strings.Repeat("x", 100), "source_response_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", tc.retryAfter)
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer ts.Close()
			c := &Client{HTTP: ts.Client()}
			ctx, metrics := MeasureRequests(context.Background())
			_, err := c.requestBounded(ctx, "GET", ts.URL, nil, 16)
			if err == nil || err.Error() != tc.want || calls.Load() != 1 {
				t.Fatal(err, calls.Load())
			}
			if got := metrics.Snapshot(); got.Requests != 1 || got.Failures != 1 || got.Successes != 0 {
				t.Fatal(got)
			}
		})
	}
	t.Run("cancel retry backoff", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
		defer ts.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		c := &Client{HTTP: ts.Client()}
		_, err := c.request(ctx, "GET", ts.URL, nil)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
}

func TestMikanMissingMappingSynthetic(t *testing.T) {
	// Deliberately synthetic, separate from the captured fixture provenance.
	entry, releases, err := ParseMikanDetail([]byte(`<html><h1 class="bangumi-title">Unmapped animation</h1></html>`), DefaultMikanURL, 42)
	if err != nil || entry.Name != "Unmapped animation" || entry.BangumiID != 0 || len(releases) != 0 {
		t.Fatal(entry, releases, err)
	}
}
