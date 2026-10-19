package subtitle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

func TestDiscoveryNamingAndConversion(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Movie.zh-CN.default.srt", "Movie.zh-TW.ass", "Movie.en.forced.srt", "Movie2.eng.srt", "Movie.eng.idx", "Movie.eng.sub"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files := Discover(filepath.Join(dir, "Movie.strm"))
	if len(files) != 3 {
		t.Fatalf("wrong matching: %+v", files)
	}
	if !files[0].Forced || files[0].Language != "eng" || !files[1].Default || files[1].Language != "zh-CN" {
		t.Fatalf("flags/language: %+v", files)
	}
	srt := "1\r\n00:00:01,250 --> 00:00:03,500\r\n<b>你好</b>\r\n"
	gb, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(srt))
	utf16, _ := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes([]byte(srt))
	for _, input := range [][]byte{[]byte(srt), append([]byte{0xef, 0xbb, 0xbf}, []byte(srt)...), gb, utf16} {
		out, err := Convert(input, "srt", "vtt")
		if err != nil || !strings.Contains(string(out), "WEBVTT\n\n00:00:01.250 --> 00:00:03.500\n你好") {
			t.Fatalf("conversion: %v %s", err, out)
		}
	}
	ass := "[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.25,0:00:03.50,Default,,0,0,0,,{\\b1}简体\\N第二行,逗号\n"
	out, err := Convert([]byte(ass), "ass", "vtt")
	if err != nil || !strings.Contains(string(out), "00:00:01.250 --> 00:00:03.500\n简体\n第二行,逗号") {
		t.Fatalf("ASS: %v %s", err, out)
	}
	if _, err := Convert([]byte("x"), "smi", "vtt"); err == nil {
		t.Fatal("accepted unsupported conversion")
	}
}

func TestReadLocalRejectsOutsideAndOversize(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.srt")
	os.WriteFile(outside, []byte("secret"), 0600)
	if _, err := ReadLocal(filepath.Join(dir, "Movie.strm"), outside); err == nil {
		t.Fatal("read outside media directory")
	}
	path := filepath.Join(dir, "Movie.srt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(MaxFileSize + 1)
	f.Close()
	if _, err := ReadLocal(filepath.Join(dir, "Movie.strm"), path); err == nil {
		t.Fatal("read oversize subtitle")
	}
	link := filepath.Join(dir, "Movie.eng.srt")
	if os.Symlink(outside, link) == nil {
		if _, err := ReadLocal(filepath.Join(dir, "Movie.strm"), link); err == nil {
			t.Fatal("followed outside symlink")
		}
	}
}
