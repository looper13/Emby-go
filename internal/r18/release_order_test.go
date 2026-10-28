package r18

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

func TestR18ReleaseDateOrderAndCursorSQL(t *testing.T) {
	cursor := EncodeID("anchor", "mono")
	for _, tc := range []struct {
		q                      Query
		order, nullDate, bound string
	}{
		{Query{}, "DESC NULLS LAST", "", ""},
		{Query{Cursor: cursor}, "DESC NULLS LAST", "'-infinity'", " < (SELECT "},
		{Query{Cursor: cursor, Before: true}, "ASC NULLS FIRST", "'-infinity'", " > (SELECT "},
		{Query{Order: "asc", Cursor: cursor}, "ASC NULLS LAST", "'infinity'", " > (SELECT "},
		{Query{Order: "asc", Cursor: cursor, Before: true}, "DESC NULLS FIRST", "'infinity'", " < (SELECT "},
	} {
		d := &r18FilterDB{}
		c := &Client{db: sql.OpenDB(d)}
		_, err := c.List(context.Background(), tc.q)
		c.Close()
		if err != nil || len(d.queries) != 1 {
			t.Fatal("query failed", tc.q, err)
		}
		query := d.queries[0]
		if !strings.Contains(query, "ORDER BY release_date "+tc.order+",content_id ") {
			t.Fatal("not ordered by release date", query)
		}
		if tc.q.Cursor != "" && (!strings.Contains(query, "COALESCE(release_date,"+tc.nullDate+")") || !strings.Contains(query, tc.bound) || strings.Contains(query, "anchor")) {
			t.Fatal("unsafe/null-inconsistent cursor boundary", query)
		}
	}
	for _, q := range []CatalogQuery{{Limit: 2}, {Limit: 2, Sort: "PremiereDate", Descending: true}, {Limit: 2, Sort: "DateCreated", Descending: true}} {
		d := &r18FilterDB{}
		c := &Client{db: sql.OpenDB(d)}
		_, err := c.CatalogList(context.Background(), q)
		c.Close()
		if err != nil || !strings.Contains(d.queries[0], "ORDER BY release_date DESC NULLS LAST,content_id DESC,service_code DESC") {
			t.Fatal("Emby default/latest must use release date", d.queries, err)
		}
	}
}

// CTE fixtures validate actual PostgreSQL comparisons without writing source data.
func checkLiveReleaseDatePagination(t *testing.T, client *Client, ctx context.Context) {
	t.Helper()
	fixture := `WITH sample AS (
SELECT content_id,service_code,dvd_id,''::text title_ja,''::text title_en,release_date,
''::text jacket_full_url,''::text gallery_full_first_url,''::text gallery_full_last_url
FROM (VALUES
('new','mono','NEW-001',DATE '2028-01-01'),
('a','digital','A-001',DATE '2026-05-01'),
('a','mono','A-001',DATE '2026-05-01'),
('z','mono','Z-001',DATE '2024-01-01'),
('c','mono','C-001',NULL::date),('d','mono','D-001',NULL::date),
('excluded','mono',NULL::text,DATE '2099-01-01')) v(content_id,service_code,dvd_id,release_date)) `
	id := func(content, service string) string { return EncodeID(content, service) }
	for _, tc := range []struct {
		q    Query
		want []string
	}{
		{Query{}, []string{id("new", "mono"), id("a", "mono")}},
		{Query{Cursor: id("a", "mono")}, []string{id("a", "digital"), id("z", "mono")}},
		{Query{Cursor: id("z", "mono")}, []string{id("d", "mono"), id("c", "mono")}},
		{Query{Cursor: id("d", "mono"), Before: true}, []string{id("a", "digital"), id("z", "mono")}},
		{Query{Cursor: id("c", "mono")}, []string{}},
		{Query{Order: "asc"}, []string{id("z", "mono"), id("a", "digital")}},
		{Query{Order: "asc", Cursor: id("a", "digital")}, []string{id("a", "mono"), id("new", "mono")}},
		{Query{Order: "asc", Cursor: id("new", "mono")}, []string{id("c", "mono"), id("d", "mono")}},
		{Query{Order: "asc", Cursor: id("c", "mono"), Before: true}, []string{id("a", "mono"), id("new", "mono")}},
		{Query{Cursor: id("excluded", "mono")}, []string{}},
	} {
		tc.q.Limit = 2
		d := &r18FilterDB{}
		recorder := &Client{db: sql.OpenDB(d)}
		_, err := recorder.List(ctx, tc.q)
		recorder.Close()
		if err != nil {
			t.Fatal(err)
		}
		query := fixture + strings.ReplaceAll(d.queries[0], "public.r18_video", "sample")
		rows, err := client.db.QueryContext(ctx, query, d.args[0]...)
		if err != nil {
			t.Fatal("release-date cursor SQL", err)
		}
		got := []string{}
		for rows.Next() {
			v, err := readVideo(rows)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, v.ID)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if len(got) > tc.q.Limit {
			got = got[:tc.q.Limit]
		}
		if tc.q.Before {
			for i, j := 0, len(got)-1; i < j; i, j = i+1, j-1 {
				got[i], got[j] = got[j], got[i]
			}
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatal("release-date/null/tie pagination", tc.q, got, tc.want)
		}
	}
}
