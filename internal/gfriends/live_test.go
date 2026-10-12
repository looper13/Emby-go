package gfriends

import (
	"context"
	"emby-go/internal/actorassets"
	"os"
	"testing"
	"time"
)

// Explicit opt-in only. Normal regression never contacts upstream or retains real portraits.
func TestUpstreamIndexSnapshot(t *testing.T) {
	path := os.Getenv("GFRIENDS_INDEX_FILE")
	if path == "" {
		t.Skip("set GFRIENDS_INDEX_FILE to a separately fetched index")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	idx, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, status string }{{"三上悠亜", "matched"}, {"妃月るい", "matched"}, {"あかり", "ambiguous"}, {"三上悠亚", "unmatched"}} {
		list, status := idx.Match(test.name)
		if status != test.status {
			t.Fatalf("%s: %s candidates=%d", test.name, status, len(list))
		}
	}
	t.Logf("index: %d groups, %d entries, %d bytes, parse %v, timestamp %s", idx.Groups, idx.Entries, len(b), time.Since(started), idx.Timestamp)
}
func TestLivePortrait(t *testing.T) {
	if os.Getenv("GFRIENDS_LIVE") != "1" {
		t.Skip("GFRIENDS_LIVE=1 explicitly enables bounded upstream verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := New(t.TempDir(), Config{}, 20*time.Second)
	idx, _, err := client.Snapshot(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	list, status := idx.Match("三上悠亜")
	if status != "matched" {
		t.Fatal(status)
	}
	raw, err := ImageURL(DefaultBaseURL, list[0])
	if err != nil {
		t.Fatal(err)
	}
	b, err := client.Download(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	m, encoded, err := actorassets.Prepare("三上悠亜", b, actorassets.Avatar{Origin: "gfriends", SourceURL: raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("portrait verified: %dx%d, input %d bytes, WebP %d bytes, source %s", m.Avatar.Width, m.Avatar.Height, len(b), len(encoded), list[0].Group)
}
