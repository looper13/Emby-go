package nfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEpisodeNFOReadSaveAndProbePreserveRootAndNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "episode.nfo")
	text := `<episodedetails><title>Pilot</title><showtitle>Drama</showtitle><season>0</season><episode>2</episode><aired>2026-10-16</aired><runtime>45 min</runtime><customtag>keep</customtag></episodedetails>`
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := Read(path)
	if err != nil || m.Root != "episodedetails" || m.Season == nil || *m.Season != 0 || m.Episode == nil || *m.Episode != 2 || m.RuntimeSeconds() != 2700 || m.ShowTitle != "Drama" {
		t.Fatal(m, err)
	}
	if err = SaveFileInfo(path, FileInfoMeta{Size: 42}, &StreamDetails{Video: &VideoStream{Codec: "h264", Width: 1920, Height: 1080}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "<customtag>keep</customtag>") || !strings.Contains(string(raw), "</episodedetails>") || strings.Contains(string(raw), "</movie>") {
		t.Fatal(string(raw))
	}
	if err = SaveAtomic(path, m); err != nil {
		t.Fatal(err)
	}
	copy, err := Read(path)
	if err != nil || copy.Root != "episodedetails" || *copy.Season != 0 || *copy.Episode != 2 {
		t.Fatal(copy, err)
	}
}
