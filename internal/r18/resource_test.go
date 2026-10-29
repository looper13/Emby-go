package r18

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type resourceConnector struct {
	query func(string, []driver.NamedValue) (driver.Rows, error)
}

func (c resourceConnector) Connect(context.Context) (driver.Conn, error) { return resourceConn{c}, nil }
func (c resourceConnector) Driver() driver.Driver                        { return nil }

type resourceConn struct{ resourceConnector }

func (c resourceConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c resourceConn) Close() error              { return nil }
func (c resourceConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected write") }
func (c resourceConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.query(q, args)
}

type resourceRows struct {
	values []driver.Value
	read   bool
}

func (r *resourceRows) Columns() []string {
	return []string{"tid", "title", "number", "date", "magnet", "website", "detail_url", "size_bytes", "size", "tags"}
}
func (r *resourceRows) Close() error { return nil }
func (r *resourceRows) Next(dest []driver.Value) error {
	if r.read || r.values == nil {
		return io.EOF
	}
	r.read = true
	copy(dest, r.values)
	return nil
}

func TestR18ResourceDirectQuerySharesListScopeAndSanitization(t *testing.T) {
	queries := 0
	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)
	db := sql.OpenDB(resourceConnector{query: func(q string, args []driver.NamedValue) (driver.Rows, error) {
		queries++
		if strings.Contains(q, "OFFSET") || !strings.Contains(q, "WHERE tid=$1 AND number IN ($2,$3,$4,$5,$6)") || !strings.Contains(q, "COALESCE(rejected,false)=false AND magnet IS NOT NULL AND magnet<>''") {
			t.Fatal("lookup lost ID, ownership, or rejection filters", q)
		}
		got := []any{}
		for _, arg := range args {
			got = append(got, arg.Value)
		}
		want := []any{int64(77), "Test-001", "TEST-001", "test-001", "TEST001", "test001"}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("wrong lookup arguments", got)
		}
		return &resourceRows{values: []driver.Value{int64(77), "version", "TEST-001", "", magnet, "", "javascript:invalid", int64(-1), int64(-2), "null"}}, nil
	}})
	defer db.Close()
	c := &Client{db: db}
	r, err := c.Resource(context.Background(), Video{Number: "Test-001"}, "77")
	if err != nil || queries != 1 || r.ID != "77" || r.SizeBytes != 0 || r.SizeMB != 0 || r.DetailURL != "" || r.Tags == nil || r.Magnet != magnet {
		t.Fatal("direct lookup did not sanitize like the list", r, err, queries)
	}
	for _, invalid := range []string{"", "forged", "0", "-1", "9999999999999999999999999"} {
		if _, err := c.Resource(context.Background(), Video{Number: "Test-001"}, invalid); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("invalid ID reached SQL", invalid, err)
		}
	}
	if queries != 1 {
		t.Fatal("invalid ID queried database", queries)
	}
	listScope, listArgs := resourceScope(Video{Number: "Test-001"}, 1)
	if !strings.Contains(listScope, "number IN ($1,$2,$3,$4,$5)") || len(listArgs) != 5 {
		t.Fatal("list/direct scope drift", listScope)
	}
}
