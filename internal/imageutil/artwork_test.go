package imageutil

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEmbyLocalArtworkNamesAndOrder(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"backdrop.jpg", "backdrop10.png", "backdrop2.jpg", "fanart-2.gif", "background-3.tbn", "art-1.jpg", "extrafanart/fanart10.jpg", "extrafanart/fanart2.JPG", "extrafanart/fanart1.jpg", "extrafanart/random.jpg", "fanart-other.jpg", "poster.jpg", "thumb.jpg", "landscape.jpg"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("image"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	images := FindMovieImages(filepath.Join(root, "Movie.strm"), root, imageExists)
	want := []string{"backdrop.jpg", "backdrop2.jpg", "backdrop10.png", "fanart-2.gif", "background-3.tbn", "art-1.jpg", "extrafanart/fanart1.jpg", "extrafanart/fanart2.JPG", "extrafanart/fanart10.jpg"}
	for i := range want {
		want[i] = filepath.Join(root, want[i])
	}
	if !reflect.DeepEqual(images.Backdrops, want) || images.Landscape != filepath.Join(root, "thumb.jpg") {
		t.Fatalf("wrong artwork: %+v", images)
	}
	for _, name := range []string{"Movie-thumb.tbn", "Movie-cover.jpg", "Movie-fanart.webp"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("image"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	images = FindMovieImages(filepath.Join(root, "Movie.strm"), root, imageExists)
	if images.Poster != filepath.Join(root, "Movie-cover.jpg") || images.Landscape != filepath.Join(root, "Movie-thumb.tbn") || len(images.Backdrops) != 4 || filepath.Base(images.Backdrops[0]) != "Movie-fanart.webp" {
		t.Fatalf("source art priority or extrafanart lost: %+v", images)
	}
}

func TestMovieNumberedStillsAreOrderedAndIsolated(t *testing.T) {
	root := t.TempDir()
	names := []string{"Movie-fanart.webp", "Movie-fanart10.webp", "Movie-fanart2.webp", "Movie-fanart1.webp", "Other-fanart1.webp", "Movie-CD1-fanart1.webp"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, name), []byte("image"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	images := FindMovieImages(filepath.Join(root, "Movie.strm"), root, imageExists)
	want := []string{"Movie-fanart.webp", "Movie-fanart1.webp", "Movie-fanart2.webp", "Movie-fanart10.webp"}
	for i := range want {
		want[i] = filepath.Join(root, want[i])
	}
	if !reflect.DeepEqual(images.Backdrops, want) {
		t.Fatalf("wrong source stills or order: %+v", images.Backdrops)
	}
	for _, name := range names {
		path := filepath.Join(root, name)
		if !IsLocalArtwork(path) {
			t.Fatalf("watcher does not recognize %s", name)
		}
		source, ok := MovieImageSource(path)
		expected := filepath.Join(root, name[:strings.LastIndex(name, "-fanart")])
		if !ok || source != expected {
			t.Fatalf("wrong watcher source for %s: %s", name, source)
		}
	}
}
