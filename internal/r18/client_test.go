package r18

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestR18IdentitiesAndSafeResources(t *testing.T) {
	a, b := EncodeID("same", "digital"), EncodeID("same", "mono")
	if a == b {
		t.Fatal("service code must participate in identity")
	}
	content, service, e := DecodeID(a)
	if e != nil || content != "same" || service != "digital" {
		t.Fatal("identity round trip", e)
	}
	for _, s := range []string{"bad", "W10", strings.Repeat("a", 1025)} {
		if _, _, e := DecodeID(s); e == nil {
			t.Fatal("accepted invalid identity", s)
		}
	}
	for _, s := range []string{"javascript:alert(1)", "data:text/html,test", "https://user:secret@example.com/a", "//example.com/a"} {
		if SafeHTTPURL(s) != "" {
			t.Fatal("unsafe HTTP URL", s)
		}
	}
	if SafeHTTPURL("https://example.com/cover.jpg") == "" {
		t.Fatal("valid image rejected")
	}
	if SafeMagnet("javascript:alert(1)") != "" || SafeMagnet("https://example.com/") != "" {
		t.Fatal("unsafe resource scheme")
	}
	if SafeMagnet("magnet:?xt=urn:btih:0123456789012345678901234567890123456789") == "" {
		t.Fatal("valid magnet rejected")
	}
	for _, s := range []string{"magnet:?xt=urn:btih:garbage", "magnet:?xt=urn:btih:" + strings.Repeat("0", 39), "magnet://example.com/?xt=urn:btih:" + strings.Repeat("0", 40), "magnet:?xt=urn:btih:" + strings.Repeat("0", 40) + "&dn=%ZZ"} {
		if SafeMagnet(s) != "" {
			t.Fatal("invalid torrent hash or URI accepted", s)
		}
	}
	for _, hash := range []string{"urn:btih:" + strings.Repeat("A", 32), "urn:btmh:1220" + strings.Repeat("a", 64)} {
		if SafeMagnet("magnet:?xt="+hash) == "" {
			t.Fatal("valid torrent hash rejected", hash)
		}
	}
	if got := NumberCandidates(Video{Number: "SSIS-001"}); !reflect.DeepEqual(got, []string{"SSIS-001", "ssis-001", "SSIS001", "ssis001"}) {
		t.Fatal(got)
	}
}

// Opt-in, read-only acceptance against a real source. No credentials are fixtures.
func TestR18LiveReadOnlyPaginationAndResources(t *testing.T) {
	host := os.Getenv("EMBY_R18_TEST_HOST")
	if host == "" {
		t.Skip("set EMBY_R18_TEST_HOST and PostgreSQL credentials for read-only acceptance")
	}
	c, e := Open(Config{Host: host, Port: 5432, Database: os.Getenv("EMBY_R18_TEST_DATABASE"), Username: os.Getenv("EMBY_R18_TEST_USERNAME"), Password: os.Getenv("EMBY_R18_TEST_PASSWORD"), SSLMode: "prefer"})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if e = c.Check(ctx); e != nil {
		t.Fatal(e)
	}
	checkLiveReleaseDatePagination(t, c, ctx)
	var readonly string
	if e = c.db.QueryRowContext(ctx, "SHOW transaction_read_only").Scan(&readonly); e != nil || readonly != "on" {
		t.Fatal("not read-only", readonly, e)
	}
	var valid int
	if e = c.db.QueryRowContext(ctx, "WITH videos(dvd_id) AS (VALUES (NULL::text),(''),('   '),('TEST-001')) SELECT COUNT(*) FROM videos WHERE "+videoFilter).Scan(&valid); e != nil || valid != 1 {
		t.Fatal("DVD ID filter must exclude NULL, empty and space-only values", valid, e)
	}
	first, e := c.List(ctx, Query{Limit: 3})
	if e != nil || len(first.Items) != 3 || !first.HasMore || first.HasPrevious {
		t.Fatal("first page", e)
	}
	next, e := c.List(ctx, Query{Limit: 3, Cursor: first.NextCursor})
	if e != nil || len(next.Items) != 3 || next.Items[0].ID == first.Items[0].ID {
		t.Fatal("next page", e)
	}
	back, e := c.List(ctx, Query{Limit: 3, Cursor: next.PreviousCursor, Before: true})
	if e != nil || !reflect.DeepEqual(first.Items, back.Items) || back.HasPrevious {
		t.Fatal("previous page", e)
	}
	last, e := c.List(ctx, Query{Limit: 3, Order: "asc"})
	if e != nil || len(last.Items) != 3 || !last.HasMore || last.HasPrevious {
		t.Fatal("ascending first page", e)
	}
	ascNext, e := c.List(ctx, Query{Limit: 3, Order: "asc", Cursor: last.NextCursor})
	if e != nil || len(ascNext.Items) != 3 || ascNext.Items[0].ID == last.Items[0].ID {
		t.Fatal("ascending next page", e)
	}
	for _, page := range []Page{first, next, back, last, ascNext} {
		for _, v := range page.Items {
			if strings.TrimSpace(v.Number) == "" {
				t.Fatal("listed a video without a DVD ID", v.ID)
			}
		}
	}
	ascBack, e := c.List(ctx, Query{Limit: 3, Order: "asc", Cursor: ascNext.PreviousCursor, Before: true})
	if e != nil || !reflect.DeepEqual(last.Items, ascBack.Items) || ascBack.HasPrevious {
		t.Fatal("ascending previous page", e)
	}
	detail, e := c.Get(ctx, first.Items[0].ID)
	if e != nil || detail.ID != first.Items[0].ID {
		t.Fatal("detail", e)
	}
	var excludedContent, excludedService string
	e = c.db.QueryRowContext(ctx, "SELECT content_id,service_code FROM public.r18_video WHERE NOT ("+videoFilter+") LIMIT 1").Scan(&excludedContent, &excludedService)
	if e == nil {
		if _, e = c.Get(ctx, EncodeID(excludedContent, excludedService)); !errors.Is(e, sql.ErrNoRows) {
			t.Fatal("direct detail returned a video without a DVD ID", e)
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("checking excluded video", e)
	}
	matched, e := c.List(ctx, Query{Search: "DASS-385", SearchMode: "number", Limit: 40})
	if e != nil || len(matched.Items) == 0 {
		t.Fatal("number lookup", e)
	}
	resources, e := c.Resources(ctx, matched.Items[0], 0)
	if e != nil || len(resources.Items) == 0 {
		t.Fatal("article association", e)
	}
	total, e := c.CatalogCount(ctx, CatalogQuery{})
	if e != nil || total < 3 {
		t.Fatal("filtered Emby catalog count", total, e)
	}
	catalogFirst, e := c.CatalogList(ctx, CatalogQuery{Limit: 3})
	if e != nil || !reflect.DeepEqual(catalogFirst, first.Items) {
		t.Fatal("Emby first page", e)
	}
	catalogNext, e := c.CatalogList(ctx, CatalogQuery{Start: 3, Limit: 3})
	if e != nil || !reflect.DeepEqual(catalogNext, next.Items) {
		t.Fatal("Emby offset page", e)
	}
	search := CatalogQuery{Search: "DASS-385", Limit: 40, Sort: "SortName"}
	searchCount, e := c.CatalogCount(ctx, search)
	if e != nil || searchCount < 1 {
		t.Fatal("Emby search count", searchCount, e)
	}
	searchItems, e := c.CatalogList(ctx, search)
	if e != nil || int64(len(searchItems)) != searchCount {
		t.Fatal("Emby search page", e)
	}
	t.Logf("Emby catalog count=%d, offset pagination and title/number search passed", total)
	t.Logf("read-only pagination, detail and article matching passed; %d source variants, %d resources", len(matched.Items), len(resources.Items))
}
