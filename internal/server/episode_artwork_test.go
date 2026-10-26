package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"emby-go/internal/scanner"
	"emby-go/internal/store"
)

func TestEpisodeThumbnailPrimaryContract(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	root := t.TempDir()
	library, err := a.db.AddLibrary("Episode artwork", root, store.LibraryMixed)
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(root, "Show")
	season := filepath.Join(show, "Season 1")
	if err := os.MkdirAll(season, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(show, "tvshow.nfo"), `<tvshow><title>Show</title></tvshow>`)
	writeJPEGImage(t, filepath.Join(show, "poster.jpg"), 40, 80)
	// No shared season poster: each episode must resolve its own thumbnail.
	for i := 1; i <= 4; i++ {
		base := filepath.Join(season, fmt.Sprintf("Show - S01E%02d - Episode %d", i, i))
		writeFile(t, base+".strm", "https://media.test/episode.mp4")
		writeFile(t, base+".nfo", fmt.Sprintf(`<episodedetails><title>Episode %d</title><season>1</season><episode>%d</episode></episodedetails>`, i, i))
		if i != 3 {
			writeJPEGImage(t, base+"-thumb.jpg", 120+i, 60)
		}
		if i == 4 {
			writeJPEGImage(t, base+"-poster.jpg", 40, 80)
		}
	}
	writeFile(t, filepath.Join(root, "Film.strm"), "https://media.test/film.mp4")
	writeFile(t, filepath.Join(root, "Film.nfo"), `<movie><title>Film</title></movie>`)
	writeJPEGImage(t, filepath.Join(root, "Film-thumb.jpg"), 150, 75)
	if result, err := scanner.Scan(context.Background(), a.db, library); err != nil || result.Success != 5 {
		t.Fatalf("scan: %+v %v", result, err)
	}
	seriesID := store.TVSeriesID(library.ID, show)
	seasonID := store.TVSeasonID(library.ID, show, 1)
	for _, prefix := range []string{"", "/emby"} {
		for _, lower := range []bool{false, true} {
			route := func(path string) string {
				if lower {
					path = strings.NewReplacer("/Shows/", "/shows/", "/Episodes", "/episodes", "/Items/", "/items/", "/Images", "/images").Replace(path)
				}
				return prefix + path
			}
			listPath := route("/Shows/" + seriesID + "/Episodes?SeasonId=" + seasonID)
			resp, raw := embyRaw(t, server, http.MethodGet, listPath, token, "")
			var list struct {
				Items []struct {
					Id        string
					ImageTags map[string]string
				}
			}
			if err := json.Unmarshal(raw, &list); err != nil || resp.StatusCode != 200 || len(list.Items) != 4 {
				t.Fatalf("%s: status=%d body=%s err=%v", listPath, resp.StatusCode, raw, err)
			}
			for i, item := range list.Items {
				base := "/Items/" + item.Id
				if i == 2 {
					if item.ImageTags["Primary"] != "" || item.ImageTags["Thumb"] != "" {
						t.Fatalf("imageless episode borrowed a sibling's cover: %+v", item)
					}
					resp, _ := embyRaw(t, server, http.MethodGet, route(base+"/Images/Primary"), token, "")
					if resp.StatusCode != 404 {
						t.Fatalf("imageless episode Primary: %d", resp.StatusCode)
					}
					continue
				}
				if item.ImageTags["Primary"] == "" || item.ImageTags["Thumb"] == "" || (i < 2 && item.ImageTags["Primary"] != item.ImageTags["Thumb"]) {
					t.Fatalf("inconsistent episode image tags: %+v", item)
				}
				resp, raw = embyRaw(t, server, http.MethodGet, route(base+"/Images"), token, "")
				var inventory []struct{ ImageType, ImageTag, Filename string }
				if err := json.Unmarshal(raw, &inventory); err != nil || resp.StatusCode != 200 || len(inventory) != 2 || inventory[0].ImageType != "Primary" || inventory[0].ImageTag != item.ImageTags["Primary"] || inventory[1].ImageType != "Thumb" || inventory[1].ImageTag != item.ImageTags["Thumb"] {
					t.Fatalf("episode image inventory: %s %v", raw, err)
				}
				for _, kind := range []string{"Primary", "Thumb"} {
					imagePath := route(base + "/Images/" + kind)
					resp, raw = embyRaw(t, server, http.MethodGet, imagePath, token, "")
					config, _, err := image.DecodeConfig(bytes.NewReader(raw))
					wantWidth := 121 + i
					if i == 3 && kind == "Primary" {
						wantWidth = 40
					}
					if err != nil || resp.StatusCode != 200 || config.Width != wantWidth {
						t.Fatalf("%s: status=%d width=%d want=%d err=%v", imagePath, resp.StatusCode, config.Width, wantWidth, err)
					}
					resp, raw = embyRaw(t, server, http.MethodHead, imagePath+"?maxWidth=60", token, "")
					if resp.StatusCode != 200 || len(raw) != 0 || resp.Header.Get("ETag") == "" || resp.Header.Get("Content-Length") == "" {
						t.Fatalf("episode thumbnail HEAD: status=%d body=%d headers=%v", resp.StatusCode, len(raw), resp.Header)
					}
				}
			}
		}
	}
	filmID, err := a.db.MovieIDByPath(filepath.Join(root, "Film.strm"))
	if err != nil {
		t.Fatal(err)
	}
	// The episode fallback must not alter movies or synthetic series/season art.
	for _, item := range []struct {
		id   string
		want int
	}{
		{strconv.FormatInt(filmID, 10), 404},
		{seriesID, 200},
		{seasonID, 404},
	} {
		resp, _ := embyRaw(t, server, http.MethodGet, "/Items/"+item.id+"/Images/Primary", token, "")
		if resp.StatusCode != item.want {
			t.Fatalf("non-episode %s Primary changed: %d want=%d", item.id, resp.StatusCode, item.want)
		}
	}
}
