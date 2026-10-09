package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
)

func capturePerformanceLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output
}

func performanceRecords(t *testing.T, output *bytes.Buffer, message string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		if record["msg"] == message {
			records = append(records, record)
		}
	}
	return records
}

func TestScanPerformanceSummaryIncludesWritesAndSkips(t *testing.T) {
	output := capturePerformanceLogs(t)
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title><genre>Drama</genre><actor><name>Actor</name></actor></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Success: 1})
	requireScan(t, database, library, Result{Skipped: 1})
	records := performanceRecords(t, output, "扫描性能汇总")
	if len(records) != 2 {
		t.Fatalf("expected one summary per scan: %s", output)
	}
	first, unchanged := records[0], records[1]
	if first["status"] != "success" || first["added"] != float64(1) || first["batches"] != float64(1) {
		t.Fatalf("initial scan summary: %v", first)
	}
	if first["scan_id"] == unchanged["scan_id"] || first["library_id"] != unchanged["library_id"] {
		t.Fatalf("scan correlation identifiers: %v / %v", first, unchanged)
	}
	if unchanged["skipped"] != float64(1) || unchanged["batches"] != float64(0) || unchanged["db_ms"] != float64(0) {
		t.Fatalf("unchanged scan reported unnecessary writes: %v", unchanged)
	}
	var accounted float64
	for _, field := range []string{"walk_ms", "index_ms", "source_ms", "prepare_ms", "verify_ms", "db_ms", "cleanup_ms", "version_ms", "other_ms"} {
		value, ok := first[field].(float64)
		if !ok || value < 0 {
			t.Fatalf("missing/non-numeric duration %s: %v", field, first)
		}
		accounted += value
	}
	if accounted > first["total_ms"].(float64)+0.01 || first["db_commit_ms"].(float64) < 0 {
		t.Fatalf("overlapping/missing stage times: %v", first)
	}
	if len(performanceRecords(t, output, "扫描落库批次耗时")) != 0 {
		t.Fatalf("ordinary batches should only be logged at debug: %s", output)
	}
}

func TestScanPerformanceSummaryOnCancellationAndWriteFailure(t *testing.T) {
	for _, mode := range []string{"cancelled", "failed"} {
		t.Run(mode, func(t *testing.T) {
			output := capturePerformanceLogs(t)
			root := t.TempDir()
			for _, name := range []string{"a", "b"} {
				writeScanFile(t, root, name+".strm", "http://media.test/"+name+".mp4\n")
				writeScanFile(t, root, name+".nfo", "<movie><title>Movie</title></movie>")
			}
			database, library := scanLibrary(t, root)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "failed" {
				// Force a real write failure after file processing.
				library.ID += 999
			}
			result, err := ScanWithProgress(ctx, database, library, func(progress Progress) {
				if mode == "cancelled" && progress.Phase == PhaseProcess && progress.Done == 1 {
					cancel()
				}
			})
			if err == nil || (mode == "cancelled" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("expected %s error: result=%+v err=%v", mode, result, err)
			}
			records := performanceRecords(t, output, "扫描性能汇总")
			if len(records) != 1 || records[0]["status"] != mode || records[0]["batches"] != float64(1) {
				t.Fatalf("missing final timing after cancellation/write failure: %s", output)
			}
			if mode == "cancelled" {
				if records[0]["added"] != float64(1) || len(visible(t, database)) != 1 {
					t.Fatalf("cancel summary did not include flushed batch: %v", records[0])
				}
			} else {
				if records[0]["added"] != float64(0) || records[0]["db_commit_ms"] != float64(0) || len(visible(t, database)) != 0 {
					t.Fatalf("failed batch reported success: %v", records[0])
				}
				batches := performanceRecords(t, output, "扫描落库批次耗时")
				if len(batches) != 1 || batches[0]["level"] != "WARN" || batches[0]["error"] == nil {
					t.Fatalf("write failure omitted batch diagnostics: %s", output)
				}
			}
		})
	}
}
