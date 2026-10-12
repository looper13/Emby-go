package server

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAnimeCoverCoalescesAndRejectsOldGeneration(t *testing.T) {
	for _, change := range []bool{false, true} {
		a, _, _ := animeFixtureApp(t)
		var data bytes.Buffer
		if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 3)), nil); err != nil {
			t.Fatal(err)
		}
		entered, release := make(chan struct{}), make(chan struct{})
		var count atomic.Int64
		a.anime.client.HTTP.Transport = animeTransport(func(r *http.Request) (*http.Response, error) {
			if count.Add(1) == 1 {
				close(entered)
			}
			<-release
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data.Bytes())), Request: r}, nil
		})
		var wg sync.WaitGroup
		results := make(chan bool, 4)
		n := 4
		if change {
			n = 1
		}
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("GET", "/cover", nil)
				results <- a.serveAnimeCover(c, "test", "https://lain.bgm.tv/pic/cover/l/test.jpg")
			}()
		}
		<-entered
		if change {
			a.anime.mu.Lock()
			cfg, _ := a.db.AnimeSettings()
			lib, _ := a.db.AnimeLibrary()
			cfg.MikanURL = "https://example.org"
			_, err := a.db.SaveAnimeSettings(cfg, lib.Name, false, lib.Revision)
			a.anime.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
		}
		close(release)
		wg.Wait()
		close(results)
		for served := range results {
			if served == change {
				t.Fatal("cover response did not respect source generation")
			}
		}
		if count.Load() != 1 {
			t.Fatal("duplicate cover fetch", count.Load())
		}
	}
}
