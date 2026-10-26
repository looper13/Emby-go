package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emby-go/internal/store"
)

func TestFailureSamplesAreBoundedButLogsAreComplete(t *testing.T) {
	output := capturePerformanceLogs(t)
	root := t.TempDir()
	const count = FailureSampleLimit + 5
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("folder-%02d/movie", index)
		writeScanFile(t, root, name+".strm", "https://media.test/movie.mp4")
		writeScanFile(t, root, name+".nfo", "<movie><title>Broken")
	}
	database, library := scanLibrary(t, root)
	var progress Progress
	result, err := ScanWithProgress(context.Background(), database, library, func(p Progress) { progress = p })
	if err != nil || result.Failed != count || len(result.Failures) != FailureSampleLimit || len(progress.Result.Failures) != FailureSampleLimit {
		t.Fatalf("failure samples: %+v, %v", result, err)
	}
	for index, failure := range result.Failures {
		want := filepath.Join(root, fmt.Sprintf("folder-%02d/movie.nfo", index))
		if failure.Path != want || failure.Source != strings.TrimSuffix(want, ".nfo")+".strm" || failure.Stage != "读取或解析 NFO" || !strings.Contains(failure.Error, "XML syntax error") {
			t.Errorf("incorrect sample: %+v", failure)
		}
	}
	logs := performanceRecords(t, output, "扫描条目处理失败，保留原索引待下轮重试")
	if len(logs) != count {
		t.Fatalf("expected every failure in logs, got %d", len(logs))
	}
	for _, log := range logs {
		path, _ := log["path"].(string)
		if log["directory"] != filepath.Dir(path) || log["source"] == "" || log["stage"] == "" || log["error"] == "" {
			t.Errorf("incomplete failure log: %v", log)
		}
	}
	message := result.FailureError("局部刷新", root).Error()
	if !strings.Contains(message, "显示 20 / 25 条") || !strings.Contains(message, result.Failures[0].Path) || strings.Contains(message, "folder-24") {
		t.Fatalf("incorrect bounded error summary: %s", message)
	}
	merged := Result{Failed: count}
	merged.AddFailureSamples(result)
	merged.AddFailureSamples(result)
	if merged.Failed != count || len(merged.Failures) != FailureSampleLimit {
		t.Fatalf("merge exceeded limit or changed counters: %+v", merged)
	}
	if (Result{}).FailureError("局部刷新", root) != nil {
		t.Fatal("successful scan invented a failure")
	}
}

func TestMetadataStatFailureRecordsExactDependencyAndReason(t *testing.T) {
	output := capturePerformanceLogs(t)
	root := t.TempDir()
	writeScanFile(t, root, "show/Season 1/S01E01.strm", "https://media.test/episode.mp4")
	writeScanFile(t, root, "show/Season 1/S01E01.nfo", "<episodedetails><title>Episode</title><season>1</season><episode>1</episode></episodedetails>")
	writeScanFile(t, root, "show/tvshow.nfo", "<tvshow><title>Show</title></tvshow>")
	database, library := scanLibrary(t, root)
	library.Type = store.LibraryTVShows
	dependency := filepath.Join(root, "show", "tvshow.nfo")
	metadataStat = func(path string) (os.FileInfo, error) {
		if path == dependency {
			return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrPermission}
		}
		return os.Stat(path)
	}
	t.Cleanup(func() { metadataStat = os.Stat })
	result, err := Scan(context.Background(), database, library)
	if err != nil || result.Failed != 1 || len(result.Failures) != 1 {
		t.Fatalf("stat failure: %+v, %v", result, err)
	}
	failure := result.Failures[0]
	if failure.Path != dependency || failure.Stage != "读取文件状态" || !strings.Contains(failure.Error, "permission denied") {
		t.Fatalf("stat dependency/reason lost: %+v", failure)
	}
	if len(performanceRecords(t, output, "扫描条目处理失败，保留原索引待下轮重试")) != 1 {
		t.Fatal("metadata stat failure was not logged")
	}
}

func TestPreparationFailureRecordsParentNFOAndFallback(t *testing.T) {
	for _, mode := range []string{"parent-xml", "parent-root", "type-mismatch", "fallback"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "Season 1", "S01E01.strm")
			writeScanFile(t, root, "Season 1/S01E01.strm", "https://media.test/episode.mp4")
			writeScanFile(t, root, "Season 1/S01E01.nfo", "<episodedetails><season>1</season><episode>1</episode></episodedetails>")
			library := store.Library{ID: 1, Path: root, Type: store.LibraryTVShows}
			wantPath, wantError := filepath.Join(root, "tvshow.nfo"), "XML syntax error"
			fallback := ""
			switch mode {
			case "parent-xml":
				writeScanFile(t, root, "tvshow.nfo", "<tvshow><title>Broken")
			case "parent-root":
				writeScanFile(t, root, "tvshow.nfo", "<movie><title>Not a show</title></movie>")
				wantError = "根节点必须是 tvshow"
			case "type-mismatch":
				library.Type = store.LibraryMovies
				wantPath, wantError = strings.TrimSuffix(path, ".strm")+".nfo", "单集不能加入影片库"
			case "fallback":
				path = filepath.Join(root, "movie-CD1.strm")
				fallback = filepath.Join(root, "movie.nfo")
				wantPath = fallback
				writeScanFile(t, root, "movie.nfo", "<movie><title>Broken")
			}
			_, result := prepareCandidate(library, candidate{path: path}, nil, fallback, nil)
			if result.Failed != 1 || len(result.Failures) != 1 || result.Failures[0].Path != wantPath || !strings.Contains(result.Failures[0].Error, wantError) {
				t.Fatalf("preparation failure: %+v", result)
			}
		})
	}
}


func TestMissingParentNFODoesNotBecomeFailure(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "show/Season 1/S01E01.strm", "https://media.test/episode.mp4")
	writeScanFile(t, root, "show/Season 1/S01E01.nfo", "<episodedetails><showtitle>Show</showtitle><season>1</season><episode>1</episode></episodedetails>")
	database, library := scanLibrary(t, root)
	library.Type = store.LibraryTVShows
	requireScan(t, database, library, Result{Success: 1, Added: 1})
	if movie := visible(t, database)[0]; movie.Television == nil || movie.Television.Title != "Show" {
		t.Fatalf("optional missing parent changed scan semantics: %+v", movie)
	}
}
