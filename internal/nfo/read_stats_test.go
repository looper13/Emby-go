package nfo

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadWithStatsPreservesReadResults(t *testing.T) {
	for _, mode := range []string{"valid", "malformed", "empty", "missing"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "movie.nfo")
			body := ""
			switch mode {
			case "valid":
				body = "<movie><title>Movie</title><runtime>94分</runtime><actor><name>Actor</name></actor></movie>"
			case "malformed":
				body = "<movie><title>Broken"
			}
			if mode != "missing" {
				if err := os.WriteFile(path, []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			ordinary, ordinaryErr := Read(path)
			timed, stats, timedErr := ReadWithStats(path)
			if !reflect.DeepEqual(ordinary, timed) || (ordinaryErr == nil) != (timedErr == nil) {
				t.Fatalf("timing changed the read: %+v %v / %+v %v", ordinary, ordinaryErr, timed, timedErr)
			}
			if ordinaryErr != nil && ordinaryErr.Error() != timedErr.Error() {
				t.Fatal("read error changed")
			}
			parses := 1
			if mode == "missing" {
				parses = 0
			}
			if stats.Reads != 1 || stats.Parses != parses || stats.Bytes != int64(len([]byte(body))) || stats.Read < 0 || stats.Parse < 0 {
				t.Fatalf("missing attempts/bytes/timings: %+v", stats)
			}
			if mode == "missing" && stats.Parse != 0 {
				t.Fatal("missing file was parsed")
			}
		})
	}
}
