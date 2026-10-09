package scanner

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"path/filepath"
	"sync/atomic"
	"time"

	"emby-go/internal/store"
)

const performanceLogInterval = 5 * time.Second

var scanSequence atomic.Uint64

type scanPerformance struct {
	logger                                      *slog.Logger
	started, lastReport, lastSlowBatch          time.Time
	wholeLibrary                                bool
	walk, index, source, prepare, verify        time.Duration
	cleanup, version                            time.Duration
	write                                       store.ScanWriteStats
	batches, processed, candidates, slowBatches int
	maxBatch                                    time.Duration
}

func newScanPerformance(lib store.Library, directory string, recursive, full bool) *scanPerformance {
	now := time.Now()
	p := &scanPerformance{
		logger: slog.Default().With("scan_id", scanSequence.Add(1), "library_id", lib.ID,
			"directory", directory, "recursive", recursive, "full", full),
		started: now, lastReport: now, wholeLibrary: recursive && filepath.Clean(directory) == filepath.Clean(lib.Path),
	}
	level := slog.LevelDebug
	if p.wholeLibrary {
		level = slog.LevelInfo
	}
	p.logger.Log(context.Background(), level, "扫描性能开始", "phase", PhaseWalk)
	return p
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

func writeTimingAttrs(stats store.ScanWriteStats) []any {
	return []any{
		"db_ms", milliseconds(stats.Total), "db_begin_ms", milliseconds(stats.Begin),
		"db_prepare_ms", milliseconds(stats.Prepare), "db_movie_ms", milliseconds(stats.Movie),
		"db_actors_ms", milliseconds(stats.Actors), "db_features_ms", milliseconds(stats.Features),
		"db_fingerprint_ms", milliseconds(stats.Fingerprint), "db_commit_ms", milliseconds(stats.Commit),
	}
}

func (p *scanPerformance) addWrite(stats store.ScanWriteStats, movies int, err error) {
	p.batches++
	p.write.Total += stats.Total
	p.write.Begin += stats.Begin
	p.write.Prepare += stats.Prepare
	p.write.Movie += stats.Movie
	p.write.Actors += stats.Actors
	p.write.Features += stats.Features
	p.write.Fingerprint += stats.Fingerprint
	p.write.Commit += stats.Commit
	if stats.Total > p.maxBatch {
		p.maxBatch = stats.Total
	}
	level := slog.LevelDebug
	if stats.Total >= time.Second {
		p.slowBatches++
		if time.Since(p.lastSlowBatch) >= performanceLogInterval {
			level = slog.LevelInfo
			p.lastSlowBatch = time.Now()
		}
	}
	if err != nil {
		level = slog.LevelWarn
	}
	// Avoid constructing attributes for the ordinary batches when debug is off.
	if p.logger.Enabled(context.Background(), level) {
		attrs := append([]any{"batch", p.batches, "movies", movies, "error", err}, writeTimingAttrs(stats)...)
		p.logger.Log(context.Background(), level, "扫描落库批次耗时", attrs...)
	}
}

func (p *scanPerformance) attrs(result Result) []any {
	total := time.Since(p.started)
	accounted := p.walk + p.index + p.source + p.prepare + p.verify + p.write.Total + p.cleanup + p.version
	other := max(time.Duration(0), total-accounted)
	attrs := []any{
		"total_ms", milliseconds(total), "walk_ms", milliseconds(p.walk),
		"index_ms", milliseconds(p.index), "source_ms", milliseconds(p.source),
		"prepare_ms", milliseconds(p.prepare), "verify_ms", milliseconds(p.verify),
		"cleanup_ms", milliseconds(p.cleanup), "version_ms", milliseconds(p.version),
		"other_ms", milliseconds(other), "db_pct", math.Round(float64(p.write.Total)*10000/float64(max(total, 1))) / 100,
		"batches", p.batches, "slow_batches", p.slowBatches, "max_batch_ms", milliseconds(p.maxBatch),
		"candidates", p.candidates, "processed", p.processed,
		"added", result.Added, "updated", result.Updated, "skipped", result.Skipped,
		"deleted", result.Deleted, "failed", result.Failed,
	}
	return append(attrs, writeTimingAttrs(p.write)...)
}

func (p *scanPerformance) report(phase string, result Result) {
	if time.Since(p.lastReport) < performanceLogInterval {
		return
	}
	p.lastReport = time.Now()
	attrs := append([]any{"phase", phase}, p.attrs(result)...)
	p.logger.Info("扫描性能进展", attrs...)
}

func (p *scanPerformance) phase(phase string) {
	level := slog.LevelDebug
	if p.wholeLibrary {
		level = slog.LevelInfo
	}
	p.logger.Log(context.Background(), level, "扫描性能阶段", "phase", phase,
		"elapsed_ms", milliseconds(time.Since(p.started)), "candidates", p.candidates, "processed", p.processed)
}

func (p *scanPerformance) finish(result Result, err error) {
	level, status := slog.LevelDebug, "success"
	if p.wholeLibrary || time.Since(p.started) >= time.Second {
		level = slog.LevelInfo
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		level, status = slog.LevelInfo, "cancelled"
	} else if err != nil || result.Failed > 0 {
		level, status = slog.LevelWarn, "failed"
	}
	attrs := append([]any{"status", status, "error", err}, p.attrs(result)...)
	p.logger.Log(context.Background(), level, "扫描性能汇总", attrs...)
}
