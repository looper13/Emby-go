package r18

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestR18APIScoreAcceptsRealDetailStringAndMissingListScore(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want float64
	}{
		{`"4.19"`, 4.19}, {`4.5`, 4.5}, {`null`, 0}, {`""`, 0}, {`" 4.19 "`, 4.19}, {`-1`, 0},
	} {
		var score apiScore
		if err := json.Unmarshal([]byte(tc.raw), &score); err != nil || float64(score) != tc.want {
			t.Fatal("真实 API 字符串评分或缺失评分解析失败", tc.raw, err)
		}
	}
	for _, raw := range []string{`"NaN"`, `"Infinity"`, `"not-a-score"`, `[]`, `{}`, `true`} {
		var score apiScore
		if json.Unmarshal([]byte(raw), &score) == nil {
			t.Fatal("接受了无效评分", raw)
		}
	}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		writeFixtureResponse(w, map[string]any{"movie": map[string]any{
			"id": "movie001", "number": "TEST-001", "duration": 150, "score": "4.19", "magnets_count": 13,
		}})
	})
	video, err := client.Get(context.Background(), EncodeID("movie001", "javdb"))
	if err != nil || video.Score != 4.19 || video.Duration != 150 || video.MagnetsCount != 13 {
		t.Fatal("真实详情评分、时长和磁力数没有正确映射", err)
	}
}
