package anime

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
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

// The proxy branch is driven through the transport's own proxy function, since
// http.ProxyFromEnvironment caches the environment for the whole process.
func TestPublicClientRoutesThroughProxy(t *testing.T) {
	var seen []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.String())
		_, _ = w.Write([]byte("via-proxy"))
	}))
	defer proxy.Close()
	target, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := PublicClient()
	transport := client.Transport.(publicTransport)
	transport.proxy = func(*http.Request) (*url.URL, error) { return target, nil }
	// Production leaves this as http.ProxyFromEnvironment; both consult the same
	// cached environment, so they always agree. They are stubbed together here.
	transport.via.Proxy = func(*http.Request) (*url.URL, error) { return target, nil }
	client.Transport = transport
	resp, err := client.Get("http://1.1.1.1/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := make([]byte, 16)
	n, _ := resp.Body.Read(body)
	if string(body[:n]) != "via-proxy" || len(seen) != 1 || seen[0] != "http://1.1.1.1/cover.jpg" {
		t.Fatal(string(body[:n]), seen)
	}
	if _, err = client.Get("http://10.0.0.5/cover.jpg"); err == nil || !strings.Contains(err.Error(), "source_private_address") {
		t.Fatal("proxied request to a private address was not refused", err)
	}
	if len(seen) != 1 {
		t.Fatal("private address reached the proxy", seen)
	}
}

func TestPublicClientRefusesPrivateAddressWithoutProxy(t *testing.T) {
	reached := false
	origin := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer origin.Close()
	client := PublicClient()
	transport := client.Transport.(publicTransport)
	transport.proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	client.Transport = transport
	if _, err := client.Get(origin.URL + "/cover.jpg"); err == nil || !strings.Contains(err.Error(), "source_private_address") {
		t.Fatal("direct request to a loopback address was not refused", err)
	}
	if reached {
		t.Fatal("loopback origin was reached")
	}
}
