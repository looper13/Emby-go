package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"emby-go/internal/store"
)

func TestUpstreamAuthorizationWhitespace(t *testing.T) {
	for _, header := range []string{`Emby Token="secret"`, "  Emby\tToken=\"secret\"  ", "MediaBrowser\tClient=\"Example\", Token=\"secret\""} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/Items", nil)
		c.Request.Header.Set("Authorization", header)
		if got := requestToken(c); got != "secret" {
			t.Fatalf("%q => %q", header, got)
		}
	}
}

func TestUpstreamDiagnosticBudgetExpiryAndClearEpoch(t *testing.T) {
	a := accountApp(t)
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	for i := range int(probeWriteBurst) {
		if allowed, _ := a.observeProbe(fmt.Sprint(i), now); !allowed {
			t.Fatal("burst prematurely exhausted")
		}
	}
	if allowed, _ := a.observeProbe("exhausted", now); allowed {
		t.Fatal("unbounded write burst")
	}
	if allowed, _ := a.observeProbe("exhausted", now.Add(time.Second)); !allowed {
		t.Fatal("token did not refill")
	}
	for i := range 200 {
		a.observeProbe(fmt.Sprint(i), now.Add(time.Duration(i+2)*time.Second))
	}
	if len(a.probeSeen) > probeSeenLimit {
		t.Fatal("unbounded dedup memory")
	}
	key := "GET /StaleReservation"
	allowed, epoch := a.observeProbe(key, now.Add(7*time.Hour))
	if !allowed {
		t.Fatal("expired window did not permit observation")
	}
	if err := a.clearDiagnosticProbes(); err != nil {
		t.Fatal(err)
	}
	a.persistDiagnosticProbe("GET", "/StaleReservation", "", epoch)
	if rows, err := a.db.Probes(10); err != nil || len(rows) != 0 {
		t.Fatalf("old observation survived clear: %v %v", rows, err)
	}
}

func TestUpstreamCollectionOwnershipAndParentRoundTrip(t *testing.T) {
	a := accountApp(t)
	admin := accountLogin(t, a)
	uid, viewer := createViewer(t, a, admin, "collection-viewer")
	first, err := a.db.AddLibrary("First", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.db.AddLibrary("Second", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetSetting(settingCollectionMinMovies, "1"); err != nil {
		t.Fatal(err)
	}
	policy := fmt.Sprintf(`{"EnableAllFolders":false,"EnabledFolders":[%q]}`, externalLibraryID(first.ID))
	if w := accountRequest(a, "POST", "/Users/"+uid+"/Policy", admin, policy); w.Code != 204 {
		t.Fatalf("grant: %d %s", w.Code, w.Body)
	}
	var ids []int64
	for i, lib := range []store.Library{first, second} {
		id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, "movie.strm"), Title: lib.Name, Status: "success", Collection: []string{"First collection", "Second collection"}[i]}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	for _, entry := range []struct {
		token, path string
		want        int
	}{
		{admin, fmt.Sprintf("/Items/%d/Collections", ids[0]), 1},
		{viewer, fmt.Sprintf("/emby/items/%d/collections", ids[0]), 1},
		{admin, "/Items?IncludeItemTypes=BoxSet&Ids=", 0},
		{admin, "/Items?IncludeItemTypes=BoxSet&ListItemIds=", 0},
		{admin, fmt.Sprintf("/Items?IncludeItemTypes=BoxSet&ListItemIds=%d", ids[0]), 1},
		{viewer, fmt.Sprintf("/Items?IncludeItemTypes=BoxSet&ListItemIds=%d", ids[1]), 0},
		{admin, "/Items?IncludeItemTypes=BoxSet&ParentId=" + externalLibraryID(first.ID), 1},
		{admin, "/Items?IncludeItemTypes=BoxSet&Ids=" + url.QueryEscape(boxsetID("First collection")), 1},
	} {
		w := accountRequest(a, "GET", entry.path, entry.token, "")
		var result struct {
			Items            []json.RawMessage
			TotalRecordCount int
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.TotalRecordCount != entry.want || len(result.Items) != entry.want {
			t.Fatalf("%s: %d %s", entry.path, w.Code, w.Body)
		}
		if entry.want == 1 && strings.Contains(w.Body.String(), "Second collection") {
			t.Fatalf("foreign collection leaked: %s", w.Body)
		}
	}
	if w := accountRequest(a, "GET", fmt.Sprintf("/Items/%d/Collections", ids[1]), viewer, ""); w.Code != 404 {
		t.Fatalf("foreign ownership status=%d", w.Code)
	}
	w := accountRequest(a, "GET", fmt.Sprintf("/Items/%d", ids[0]), viewer, "")
	var movie struct {
		ParentId      string
		CollectionIds []string
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &movie) != nil || movie.ParentId != externalLibraryID(first.ID) || len(movie.CollectionIds) != 1 {
		t.Fatalf("DTO=%s", w.Body)
	}
	if w := accountRequest(a, "GET", "/Items/"+movie.ParentId, viewer, ""); w.Code != 200 {
		t.Fatalf("parent cannot resolve: %d %s", w.Code, w.Body)
	}
}

func TestUpstreamVersionLogsAndRequestBudgets(t *testing.T) {
	a := accountApp(t)
	admin := accountLogin(t, a)
	w := accountRequest(a, "GET", "/api/version", "", "")
	var build map[string]string
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &build) != nil || build["version"] != BuildVersion || build["commit"] != BuildCommit {
		t.Fatalf("build: %d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/api/version", "/not-implemented"} {
		w = accountRequest(a, "GET", path, "", "")
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("missing headers: %v", w.Header())
		}
	}
	_, viewer := createViewer(t, a, admin, "log-viewer")
	if w := accountRequest(a, "GET", "/api/admin/logs", viewer, ""); w.Code != 403 {
		t.Fatalf("viewer logs: %d", w.Code)
	}
	if w := accountRequest(a, "GET", "/api/admin/logs", admin, ""); w.Code != 200 {
		t.Fatalf("admin logs: %d %s", w.Code, w.Body)
	}
	if w := accountRequest(a, "GET", "/api/admin/metrics", viewer, ""); w.Code != 403 {
		t.Fatalf("viewer metrics: %d", w.Code)
	}
	if w := accountRequest(a, "GET", "/api/admin/metrics", admin, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "goroutines") {
		t.Fatalf("admin metrics: %d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/auth/initialize", strings.NewReader(strings.Repeat("x", int(defaultRequestBodyLimit)+1)))
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large request: %d", w.Code)
	}
}

func TestUpstreamAuthRateLimitAndDiagnosticPrivacy(t *testing.T) {
	a := accountApp(t)
	admin := accountLogin(t, a)
	for range authAccountLimit {
		if w := accountRequest(a, "POST", "/Users/AuthenticateByName", "", `{"Username":"admin","Pw":"wrong-password"}`); w.Code != 401 {
			t.Fatalf("failure status=%d %s", w.Code, w.Body)
		}
	}
	w := accountRequest(a, "POST", "/Users/AuthenticateByName", "", `{"Username":"admin","Pw":"password-1234"}`)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("limit: %d %s", w.Code, w.Body)
	}
	a.authLimiter.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if w := accountRequest(a, "POST", "/Users/AuthenticateByName", "", `{"Username":"admin","Pw":"password-1234"}`); w.Code != 200 {
		t.Fatalf("expired limit: %d", w.Code)
	}
	accountRequest(a, "POST", "/UnknownEndpoint?api_key=private-key", admin, `{"Pw":"private-password"}`)
	accountRequest(a, "GET", "/.git/config", admin, "")
	rows, err := a.db.Probes(20)
	if err != nil || len(rows) != 1 {
		t.Fatalf("probes=%v %v", rows, err)
	}
	data, _ := json.Marshal(rows)
	if strings.Contains(string(data), "private-key") || strings.Contains(string(data), "private-password") {
		t.Fatalf("credentials persisted: %s", data)
	}
	if w := accountRequest(a, "DELETE", "/api/admin/probe", admin, ""); w.Code != 204 {
		t.Fatalf("clear: %d", w.Code)
	}
	accountRequest(a, "GET", "/UnknownEndpoint", admin, "")
	if rows, err := a.db.Probes(20); err != nil || len(rows) != 1 {
		t.Fatalf("clear did not reset dedup: %v %v", rows, err)
	}
}
