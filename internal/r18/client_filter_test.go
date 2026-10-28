package r18

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

type r18FilterDB struct {
	queries []string
	args    [][]any
}

func (d *r18FilterDB) Connect(context.Context) (driver.Conn, error) { return d, nil }
func (d *r18FilterDB) Driver() driver.Driver                        { return d }
func (d *r18FilterDB) Open(string) (driver.Conn, error)             { return d, nil }
func (d *r18FilterDB) Close() error                                 { return nil }
func (d *r18FilterDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (d *r18FilterDB) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (d *r18FilterDB) QueryContext(_ context.Context, query string, values []driver.NamedValue) (driver.Rows, error) {
	d.queries = append(d.queries, query)
	args := []any{}
	for _, value := range values {
		args = append(args, value.Value)
	}
	d.args = append(d.args, args)
	return r18FilterRows{}, nil
}

type r18FilterRows struct{}

func (r18FilterRows) Columns() []string         { return []string{"unused"} }
func (r18FilterRows) Close() error              { return nil }
func (r18FilterRows) Next([]driver.Value) error { return io.EOF }

func TestR18VideoFilterOnEveryRead(t *testing.T) {
	cursor := EncodeID("item", "mono")
	for _, tc := range []struct {
		name  string
		query Query
	}{
		{"default", Query{}},
		{"number", Query{Search: "TEST-001"}},
		{"content ID", Query{Search: "item"}},
		{"title and source", Query{Search: "title", SearchMode: "title", Service: "mono"}},
		{"ascending next", Query{Cursor: cursor, Order: "asc"}},
		{"ascending previous", Query{Cursor: cursor, Order: "asc", Before: true}},
		{"descending next", Query{Cursor: cursor, Order: "desc"}},
		{"descending previous", Query{Cursor: cursor, Order: "desc", Before: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &r18FilterDB{}
			c := &Client{db: sql.OpenDB(d)}
			defer c.Close()
			if _, err := c.List(context.Background(), tc.query); err != nil {
				t.Fatal(err)
			}
			if len(d.queries) != 1 || !strings.Contains(d.queries[0], " WHERE dvd_id IS NOT NULL AND TRIM(dvd_id)<>''") {
				t.Fatal("list must filter missing DVD IDs before search and pagination", d.queries)
			}
		})
	}
	t.Run("direct detail", func(t *testing.T) {
		d := &r18FilterDB{}
		c := &Client{db: sql.OpenDB(d)}
		defer c.Close()
		if _, err := c.Get(context.Background(), cursor); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("filtered detail must be absent", err)
		}
		if len(d.queries) != 1 || !strings.Contains(d.queries[0], " WHERE dvd_id IS NOT NULL AND TRIM(dvd_id)<>'' AND content_id=$1 AND service_code=$2") {
			t.Fatal("detail must use the same filter", d.queries)
		}
	})
}
