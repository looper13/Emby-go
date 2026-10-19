package nfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiAudioFileInfoRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	if err := os.WriteFile(path, []byte(`<movie><title>Keep</title><custom>untouched</custom><fileinfo><streamdetails><audio><codec>aac</codec><streamindex>0</streamindex><title>First</title></audio><audio><codec>ac3</codec><streamindex>5</streamindex><title>Second</title><language>eng</language></audio></streamdetails></fileinfo></movie>`), 0600); err != nil {
		t.Fatal(err)
	}
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	audio := meta.FileInfo.StreamDetails.AllAudio()
	if len(audio) != 2 || audio[0].Index == nil || *audio[0].Index != 0 || *audio[1].Index != 5 || audio[1].Title != "Second" {
		t.Fatalf("parse: %+v", audio)
	}
	if err := SaveFileInfo(path, FileInfoMeta{ProbeVersion: 2}, meta.FileInfo.StreamDetails); err != nil {
		t.Fatal(err)
	}
	updated, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.FileInfo.StreamDetails.AllAudio()) != 2 || updated.FileInfo.StreamDetails.AllAudio()[1].Title != "Second" {
		t.Fatal("lost audio on NFO write")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "<streamindex>0</streamindex>") || !strings.Contains(string(data), "<custom>untouched</custom>") {
		t.Fatalf("lost source data: %s", data)
	}
}
