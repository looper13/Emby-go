package store

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Synthetic scale from the plan: 5,000 subjects, 100,000 episodes, 200,000
// releases. This measures local SQLite queries, excluding HTTP/images/STRM I/O.
func BenchmarkAnimeCatalogScale(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "scale.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	s.waitFeaturesReady()
	_, err = s.db.Exec(`
BEGIN;
WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<5000)
INSERT INTO anime_subjects(id,bangumi_id,name,platform,metadata,updated_at)
SELECT x,x,'Subject '||x,'TV',json_object('name','Subject '||x,'platform','TV'),'2026-10-12T00:00:00Z' FROM n;
INSERT INTO anime_mikan_entries(id,subject_id,name,year,season,weekday,metadata,updated_at)
SELECT id,id,name,2026,4,id%7,json_object('name',name,'year',2026,'season',4),'2026-10-12T00:00:00Z' FROM anime_subjects;
INSERT INTO anime_catalog_entries SELECT 2026,4,id,id%7 FROM anime_subjects;
WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<100000)
INSERT INTO anime_episodes(id,subject_id,bangumi_id,sort,season,emby_index,metadata)
SELECT x,(x-1)/20+1,x,CAST((x-1)%20+1 AS TEXT),1,(x-1)%20+1,json_object('name','Episode '||x,'sort',(x-1)%20+1,'season',1) FROM n;
WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<200000)
INSERT INTO anime_releases(id,mikan_id,metadata,published_at)
SELECT printf('%040x',x),(x-1)/40+1,json_object('title','[Group] Release '||x,'id',printf('%040x',x),'mikan_id',(x-1)/40+1),'2026-10-12T00:00:00Z' FROM n;
COMMIT;
`)
	if err != nil {
		b.Fatal(err)
	}
	cases := []struct {
		name  string
		query func() error
	}{
		{"catalog50", func() error { _, _, e := s.AnimeCatalog(2026, 4, -1, "", 50, 0); return e }},
		{"episodes20", func() error { _, e := s.AnimeEpisodes(2500); return e }},
		{"releases50", func() error { _, _, e := s.AnimeReleases(2500, 50, 0, ""); return e }},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			times := make([]int64, b.N)
			b.ResetTimer()
			for i := range b.N {
				start := time.Now()
				if err := tc.query(); err != nil {
					b.Fatal(err)
				}
				times[i] = time.Since(start).Nanoseconds()
			}
			b.StopTimer()
			slices.Sort(times)
			b.ReportMetric(float64(times[(len(times)-1)*95/100])/1e6, "p95-ms")
		})
	}
}
