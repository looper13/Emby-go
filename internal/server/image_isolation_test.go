package server

import (
	"bytes"
	"image"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestImageUploadPreservesSiblingAndRescan(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		writeFile(t, filepath.Join(root, name+".strm"), "http://media.test/"+name+".mp4")
		writeFile(t, filepath.Join(root, name+".nfo"), "<movie><title>"+name+"</title></movie>")
	}
	writeJPEGImage(t, filepath.Join(root, "poster.jpg"), 80, 40)
	app, server, token := newProbeTestApp(t, root)
	aID, err := app.db.MovieIDByPath(filepath.Join(root, "a.strm"))
	if err != nil {
		t.Fatal(err)
	}
	bID, err := app.db.MovieIDByPath(filepath.Join(root, "b.strm"))
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "upload.jpg")
	writeJPEGImage(t, input, 20, 60)
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "upload.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", server.URL+"/api/admin/items/"+strconv.FormatInt(aID, 10)+"/images/poster", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("X-Emby-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("upload: %d %s", resp.StatusCode, raw)
	}
	if _, err := app.scanLibraries(0); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id            int64
		path          string
		width, height int
	}{
		{aID, filepath.Join(root, "a-poster.webp"), 20, 60},
		{bID, filepath.Join(root, "poster.jpg"), 80, 40},
	} {
		movie, err := app.db.Movie(item.id)
		if err != nil || movie.PosterPath != item.path {
			t.Fatalf("wrong indexed image: %+v, %v", movie, err)
		}
		resp, err := http.Get(server.URL + "/Items/" + strconv.FormatInt(item.id, 10) + "/Images/Primary")
		if err != nil {
			t.Fatal(err)
		}
		config, _, err := image.DecodeConfig(resp.Body)
		resp.Body.Close()
		if err != nil || config.Width != item.width || config.Height != item.height {
			t.Fatalf("wrong served image: %+v, %v", config, err)
		}
	}
}
