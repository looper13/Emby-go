package r18

import (
	"context"
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
	var readonly string
	if e = c.db.QueryRowContext(ctx, "SHOW transaction_read_only").Scan(&readonly); e != nil || readonly != "on" {
		t.Fatal("not read-only", readonly, e)
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
	last, e := c.List(ctx, Query{Limit: 3, Order: "desc"})
	if e != nil || len(last.Items) != 3 || !last.HasMore || last.HasPrevious {
		t.Fatal("descending first page", e)
	}
	descNext, e := c.List(ctx, Query{Limit: 3, Order: "desc", Cursor: last.NextCursor})
	if e != nil || len(descNext.Items) != 3 || descNext.Items[0].ID == last.Items[0].ID {
		t.Fatal("descending next page", e)
	}
	descBack, e := c.List(ctx, Query{Limit: 3, Order: "desc", Cursor: descNext.PreviousCursor, Before: true})
	if e != nil || !reflect.DeepEqual(last.Items, descBack.Items) || descBack.HasPrevious {
		t.Fatal("descending previous page", e)
	}
	detail, e := c.Get(ctx, first.Items[0].ID)
	if e != nil || detail.ID != first.Items[0].ID {
		t.Fatal("detail", e)
	}
	matched, e := c.List(ctx, Query{Search: "DASS-385", SearchMode: "number", Limit: 40})
	if e != nil || len(matched.Items) == 0 {
		t.Fatal("number lookup", e)
	}
	resources, e := c.Resources(ctx, matched.Items[0], 0)
	if e != nil || len(resources.Items) == 0 {
		t.Fatal("article association", e)
	}
	t.Logf("read-only pagination, detail and article matching passed; %d source variants, %d resources", len(matched.Items), len(resources.Items))
}
