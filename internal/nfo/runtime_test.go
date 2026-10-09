package nfo

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRuntimeWithMinuteUnits(t *testing.T) {
	for _, value := range []string{"94", "94分", "94分钟", "94分鐘", " 94 分 ", "94 min", "94 MINS", "94 minute", "94 minutes"} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "movie.nfo")
			body := `<movie><title>Keep title</title><runtime>` + value + `</runtime><actor><name>Keep actor</name></actor><fileinfo><streamdetails><video><codec>hevc</codec><durationinseconds>5700</durationinseconds></video></streamdetails></fileinfo></movie>`
			if err := os.WriteFile(path, []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
			meta, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			if meta.Runtime != 94 || meta.RuntimeSeconds() != 5640 || meta.Title != "Keep title" || len(meta.Actors) != 1 || meta.Actors[0].Name != "Keep actor" {
				t.Fatalf("metadata = %+v", meta)
			}
			if meta.FileInfo == nil || meta.FileInfo.StreamDetails == nil || meta.FileInfo.StreamDetails.Video == nil || meta.FileInfo.StreamDetails.Video.Codec != "hevc" {
				t.Fatalf("stream details lost: %+v", meta.FileInfo)
			}
			if raw, err := os.ReadFile(path); err != nil || string(raw) != body {
				t.Fatalf("Read changed original NFO: %v", err)
			}
			if err := Save(path, meta); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(raw), "<runtime>94</runtime>") {
				t.Fatalf("Save must retain numeric runtime: %s %v", raw, err)
			}
		})
	}
}

func TestReadVideoDurationWithMinuteUnits(t *testing.T) {
	for _, value := range []string{"87", "87分", "87分钟", "87 min"} {
		t.Run(value, func(t *testing.T) {
			var meta MovieMeta
			body := `<movie><title>Keep</title><fileinfo><size>123</size><streamdetails><video><codec>h264</codec><width>1920</width><duration>` + value + `</duration></video></streamdetails></fileinfo></movie>`
			if err := xml.Unmarshal([]byte(body), &meta); err != nil {
				t.Fatal(err)
			}
			video := meta.FileInfo.StreamDetails.Video
			if meta.RuntimeSeconds() != 5220 || video.DurationMinutes != 87 || video.Width != 1920 || video.Codec != "h264" || meta.FileInfo.Size != 123 {
				t.Fatalf("metadata = %+v, video = %+v", meta, video)
			}
			video.DurationSeconds = 5240
			if meta.RuntimeSeconds() != 5240 {
				t.Fatal("durationinseconds precedence changed")
			}
		})
	}
}

func TestReadRuntimeRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"分", "94分unknown", "unknown", "1小时", "1:34", "94.5分", "9223372036854775808分"} {
		t.Run(value, func(t *testing.T) {
			for _, body := range []string{
				`<movie><runtime>` + value + `</runtime></movie>`,
				`<movie><fileinfo><streamdetails><video><duration>` + value + `</duration></video></streamdetails></fileinfo></movie>`,
			} {
				var meta MovieMeta
				if err := xml.Unmarshal([]byte(body), &meta); err == nil {
					t.Fatalf("invalid duration accepted: %s", body)
				}
			}
		})
	}
	for _, body := range []string{"<movie><runtime></movie>", "<movie><year>bad</year></movie>"} {
		var meta MovieMeta
		if err := xml.Unmarshal([]byte(body), &meta); err == nil {
			t.Fatalf("unrelated invalid NFO accepted: %s", body)
		}
	}
}

func TestReadEmptyRuntimeKeepsStreamFallback(t *testing.T) {
	for _, runtime := range []string{"", "<runtime/>", "<runtime> </runtime>", "<runtime>0</runtime>"} {
		var meta MovieMeta
		body := `<movie>` + runtime + `<fileinfo><streamdetails><video><durationinseconds>120</durationinseconds></video></streamdetails></fileinfo></movie>`
		if err := xml.Unmarshal([]byte(body), &meta); err != nil {
			t.Fatal(err)
		}
		if meta.RuntimeSeconds() != 120 {
			t.Fatalf("fallback = %d, want 120", meta.RuntimeSeconds())
		}
	}
}
