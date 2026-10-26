package scraper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"emby-go/internal/metatube"
	"emby-go/internal/nfo"
	"emby-go/internal/store"
)

func TestNormalizedNumberPreviewAndRun(t *testing.T) {
	for _, tc := range []struct{ indexed, returned, canonical string }{
		{"AEGE-0015", "AEGE-015", "AEGE-015"},
		{"AEGE-015", "AEGE-0015", "AEGE-015"},
		{"109IENFH-29503", "IENFH-29503", "IENFH-29503"},
	} {
		t.Run(tc.indexed, func(t *testing.T) {
			var details atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/movies/search":
					json.NewEncoder(w).Encode(map[string]any{"data": []metatube.MovieSearchResult{
						{Provider: "p", ID: "wrong", Number: "AEGE-016"},
						{Provider: "p", ID: "exact", Number: tc.returned},
					}})
				case "/v1/movies/p/exact":
					details.Add(1)
					json.NewEncoder(w).Encode(map[string]any{"data": metatube.MovieInfo{
						Provider: "p", ID: "exact", Number: tc.returned, Title: tc.indexed + " Title",
					}})
				default:
					t.Errorf("non-exact candidate requested: %s", r.URL.Path)
					http.Error(w, "unexpected", http.StatusBadRequest)
				}
			}))
			defer backend.Close()
			dir := t.TempDir()
			s := New(Config{BaseURL: backend.URL, Concurrency: 1}, nil)
			movie := store.Movie{Number: tc.indexed, SourcePath: filepath.Join(dir, tc.indexed+".strm")}
			preview, err := s.Preview(context.Background(), movie)
			if err != nil || !preview.Exact || preview.Expected != tc.canonical || preview.Recommended != 1 || len(preview.Candidates) != 2 || !preview.Candidates[1].Exact || details.Load() != 0 {
				t.Fatalf("preview=%+v details=%d err=%v", preview, details.Load(), err)
			}
			progress, err := s.RunMovies(context.Background(), []store.Movie{movie}, RunOptions{Overwrite: true})
			if err != nil || progress.Success != 1 || progress.Skipped != 0 || progress.Failed != 0 || details.Load() != 1 {
				t.Fatalf("run=%+v details=%d err=%v", progress, details.Load(), err)
			}
			meta, err := nfo.Read(filepath.Join(dir, tc.indexed+".nfo"))
			if err != nil || meta.Number != tc.canonical || meta.Title != tc.canonical+" Title" {
				t.Fatalf("metadata=%+v err=%v", meta, err)
			}
		})
	}
}

func TestNormalizedNumberSelectionKeepsBoundaries(t *testing.T) {
	results := []metatube.MovieSearchResult{
		{ID: "different", Number: "AEGE-016"},
		{ID: "suffix", Number: "AEGE-0015-UC"},
		{ID: "prefix", Number: "AEGE-00150"},
		{ID: "exact", Number: "AEGE-015"},
		{ID: "alias", Number: "AEGE-0000015"},
	}
	for _, expected := range []string{"AEGE-0015", "AEGE0015"} {
		chosen, ok := selectExact(results, expected)
		if !ok || chosen.ID != "exact" {
			t.Fatalf("selectExact(%q)=%+v %v", expected, chosen, ok)
		}
		matches := selectAllExact(results, expected)
		if len(matches) != 2 || matches[0].ID != "exact" || matches[1].ID != "alias" {
			t.Fatalf("selectAllExact(%q)=%+v", expected, matches)
		}
		picked := pickCandidates(results, expected, 2)
		if len(picked) != 2 || picked[0].ID != "exact" || picked[1].ID != "alias" {
			t.Fatalf("pickCandidates(%q)=%+v", expected, picked)
		}
	}
	if got := expectedNumber(store.Movie{SourcePath: filepath.Join("media", "AEGE-0015.strm")}); got != "AEGE-015" {
		t.Fatalf("filename expected number=%q", got)
	}
	if got := metatube.Normalize(" abf_00015 "); got != "ABF-015" {
		t.Fatalf("legacy separator normalization=%q", got)
	}
}
