package anime

import (
	"context"
	"sync/atomic"
)

type requestMetricsKey struct{}

// Metrics belong to a refresh context, so concurrent searches or cover reads
// cannot inflate a task's request counts. No URL or credentials are retained.
type RequestMetrics struct {
	requests, successes, failures, cacheHits atomic.Int64
}

type RequestStats struct {
	Requests  int64 `json:"requests"`
	Successes int64 `json:"successes"`
	Failures  int64 `json:"failures"`
	CacheHits int64 `json:"cache_hits"`
}

func MeasureRequests(ctx context.Context) (context.Context, *RequestMetrics) {
	m := &RequestMetrics{}
	return context.WithValue(ctx, requestMetricsKey{}, m), m
}

func (m *RequestMetrics) Snapshot() RequestStats {
	return RequestStats{m.requests.Load(), m.successes.Load(), m.failures.Load(), m.cacheHits.Load()}
}

func RecordCacheHit(ctx context.Context) {
	if m, ok := ctx.Value(requestMetricsKey{}).(*RequestMetrics); ok {
		m.cacheHits.Add(1)
	}
}
