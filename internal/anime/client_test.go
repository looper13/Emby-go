package anime

import (
	"os"
	"testing"
)

func fixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestMikanFixtures(t *testing.T) {
	entries, err := ParseSeason(fixture(t, "mikan/home-season.html"), DefaultMikanURL, 2026, 4)
	if err != nil || len(entries) < 10 {
		t.Fatal(len(entries), err)
	}
	days := map[int]int{}
	for _, e := range entries {
		if e.ID == 0 || e.Name == "" {
			t.Fatal(e)
		}
		days[e.Weekday]++
	}
	if days[0] == 0 || days[7] == 0 || days[8] == 0 {
		t.Fatal("lost non-weekday or OVA entries", days)
	}
	e, rows, err := ParseMikanDetail(fixture(t, "mikan/bangumi-page-tv-4063.html"), DefaultMikanURL, 4063)
	if err != nil || e.BangumiID != 418229 || len(rows) == 0 {
		t.Fatal(e, len(rows), err)
	}
	rss, err := ParseRSS(fixture(t, "mikan/rss-bangumi-4063.xml"), 4063)
	if err != nil || len(rss) == 0 {
		t.Fatal(err)
	}
	for _, r := range rss {
		if len(r.ID) != 40 || r.Size <= 0 || r.PublishedAt == "" || r.Magnet == "" {
			t.Fatal(r)
		}
	}
	search, err := ParseMikanSearch(fixture(t, "mikan/search-keyword.html"), DefaultMikanURL)
	if err != nil || len(search) == 0 {
		t.Fatal(err)
	}
	_, long, err := ParseMikanDetail(fixture(t, "mikan/bangumi-page-long-run-227.html"), DefaultMikanURL, 227)
	if err != nil {
		t.Fatal(err)
	}
	ambiguous := false
	for _, r := range long {
		ambiguous = ambiguous || r.Ambiguous
	}
	if !ambiguous {
		t.Fatal("lost batch releases")
	}
	for _, invalid := range []string{"<html>login</html>", ""} {
		if _, err := ParseSeason([]byte(invalid), DefaultMikanURL, 2026, 4); err == nil {
			t.Fatal("accepted invalid catalog")
		}
	}
}

func TestBangumiFixtures(t *testing.T) {
	tv, err := ParseSubject(fixture(t, "bangumi/subject-tv-418229.json"))
	if err != nil || tv.IsMovie() || tv.BangumiID != 418229 {
		t.Fatal(tv, err)
	}
	movie, err := ParseSubject(fixture(t, "bangumi/subject-movie-3485.json"))
	if err != nil || !movie.IsMovie() {
		t.Fatal(movie, err)
	}
	sp, total, _, err := ParseEpisodes(fixture(t, "bangumi/episodes-sp-899.json"), 899)
	if err != nil || total == 0 || sp[0].Season != 0 || sp[0].Sort.String() != "570.5" {
		t.Fatal(sp, total, err)
	}
	if _, _, _, err = ParseEpisodes(fixture(t, "bangumi/episodes-sp-899.json"), 1); err == nil {
		t.Fatal("cross-subject episodes accepted")
	}
}
