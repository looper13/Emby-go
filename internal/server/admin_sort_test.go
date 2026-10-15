package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"emby-go/internal/store"
)

func TestAdminSortSeedAndResponseCache(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("movie-%02d", i)
		writeFile(t, filepath.Join(root, name+".strm"), "http://media.test/movie.mp4")
		writeFile(t, filepath.Join(root, name+".nfo"), fmt.Sprintf("<movie><title>%s</title><premiered>2026-01-%02d</premiered></movie>", name, i+1))
	}
	_, server, token := newProbeTestApp(t, root)
	read := func(query string) []int64 {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/admin/items?limit=1000&"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Emby-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result struct {
			Items []store.Movie `json:"items"`
			Total int           `json:"total"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || result.Total != 25 {
			t.Fatalf("status=%d total=%d", resp.StatusCode, result.Total)
		}
		ids := make([]int64, len(result.Items))
		for i, movie := range result.Items {
			ids[i] = movie.ID
		}
		return ids
	}
	first := read("sort=random&seed=12345")
	if !reflect.DeepEqual(first, read("sort=random&seed=12345")) {
		t.Fatal("cached random order changed")
	}
	if reflect.DeepEqual(first, read("sort=random&seed=98765")) {
		t.Fatal("cache mixed random seeds")
	}
	defaultOrder := read("sort=random&seed=1")
	for _, invalid := range []string{"", "0", "-1", "2147483648", "invalid"} {
		if !reflect.DeepEqual(defaultOrder, read("sort=random&seed="+invalid)) {
			t.Fatalf("invalid seed %q not normalized", invalid)
		}
	}
	dates := read("sort=date&order=desc")
	if dates[0] != 25 || dates[24] != 1 {
		t.Fatalf("premiere order %v", dates)
	}
}
