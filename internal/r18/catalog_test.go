package r18

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

func TestR18CatalogQueryFilterAndBoundParameters(t *testing.T) {
	q := CatalogQuery{Search: `Test'_%\`, Years: []int{2024, 2025}, NameStartsWith: "A%", NameLessThan: "Z", NameStartsWithOrGreater: "B"}
	where, args, err := catalogWhere(q)
	if err != nil || !strings.HasPrefix(where, videoFilter+" AND ") || strings.Contains(where, q.Search) || !strings.Contains(where, "EXTRACT(YEAR FROM release_date) IN ($5,$6)") {
		t.Fatal("unsafe or inconsistent filter", where, args, err)
	}
	if !reflect.DeepEqual(args, []any{`%Test'\_\%\\%`, `A\%%`, "B", "Z", 2024, 2025}) {
		t.Fatal("search wildcard escaping", args)
	}
	for _, q := range []CatalogQuery{{Search: strings.Repeat("a", 121)}, {Years: []int{0}}, {Years: make([]int, 101)}} {
		if _, _, err := catalogWhere(q); err == nil {
			t.Fatal("accepted invalid query", q)
		}
	}
	for _, q := range []CatalogQuery{
		{Start: 100, Limit: 50, Sort: "SortName", Descending: true},
		{Start: 0, Limit: 20, Sort: "PremiereDate"},
		{Start: 0, Limit: 20, Sort: "DateCreated; DROP TABLE r18_video"},
	} {
		d := &r18FilterDB{}
		c := &Client{db: sql.OpenDB(d)}
		if _, err := c.CatalogList(context.Background(), q); err != nil {
			t.Fatal(err)
		}
		c.Close()
		if len(d.queries) != 1 || !strings.Contains(d.queries[0], " WHERE "+videoFilter) || !strings.Contains(d.queries[0], " LIMIT $1 OFFSET $2") || strings.Contains(d.queries[0], "DROP TABLE") {
			t.Fatal("unsafe catalog pagination", d.queries)
		}
	}
}
