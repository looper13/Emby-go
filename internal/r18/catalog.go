package r18

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// CatalogQuery uses Emby's offset pagination without changing the web cursor API.
type CatalogQuery struct {
	Search, Sort, NameStartsWith, NameStartsWithOrGreater, NameLessThan string
	Years                                                               []int
	Descending                                                          bool
	Start, Limit                                                        int
}

const catalogName = `COALESCE(NULLIF(title_ja,''),NULLIF(title_en,''),dvd_id,content_id)`

func catalogWhere(q CatalogQuery) (string, []any, error) {
	args := []any{}
	bind := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	where := []string{videoFilter}
	for _, value := range []string{q.Search, q.NameStartsWith, q.NameStartsWithOrGreater, q.NameLessThan} {
		if len([]rune(value)) > 120 {
			return "", nil, errors.New("search is too long")
		}
	}
	escape := func(value string) string {
		return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
	}
	if term := strings.TrimSpace(q.Search); term != "" {
		pattern := bind("%" + escape(term) + "%")
		where = append(where, "(dvd_id ILIKE "+pattern+" OR content_id ILIKE "+pattern+" OR title_ja ILIKE "+pattern+" OR title_en ILIKE "+pattern+")")
	}
	if q.NameStartsWith != "" {
		where = append(where, catalogName+" ILIKE "+bind(escape(q.NameStartsWith)+"%"))
	}
	if q.NameStartsWithOrGreater != "" {
		where = append(where, "LOWER("+catalogName+") >= LOWER("+bind(q.NameStartsWithOrGreater)+")")
	}
	if q.NameLessThan != "" {
		where = append(where, "LOWER("+catalogName+") < LOWER("+bind(q.NameLessThan)+")")
	}
	if len(q.Years) > 100 {
		return "", nil, errors.New("too many years")
	}
	if len(q.Years) > 0 {
		parts := []string{}
		for _, year := range q.Years {
			if year < 1 || year > 9999 {
				return "", nil, errors.New("invalid year")
			}
			parts = append(parts, bind(year))
		}
		where = append(where, "EXTRACT(YEAR FROM release_date) IN ("+strings.Join(parts, ",")+")")
	}
	return strings.Join(where, " AND "), args, nil
}

func (c *Client) CatalogCount(ctx context.Context, q CatalogQuery) (int64, error) {
	where, args, err := catalogWhere(q)
	if err != nil {
		return 0, err
	}
	var total int64
	err = c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.r18_video WHERE "+where, args...).Scan(&total)
	return total, err
}

func (c *Client) CatalogList(ctx context.Context, q CatalogQuery) ([]Video, error) {
	items := []Video{}
	if q.Start < 0 || q.Limit < 1 || q.Limit > 1000 {
		return items, errors.New("invalid catalog page")
	}
	where, args, err := catalogWhere(q)
	if err != nil {
		return items, err
	}
	sortKey := strings.ToLower(q.Sort)
	order := "ASC"
	if q.Descending || sortKey == "" {
		order = "DESC"
	}
	tie := "content_id " + order + ",service_code " + order
	sort := "release_date " + order + " NULLS LAST," + tie
	switch sortKey {
	case "sortname", "name", "title":
		sort = "LOWER(" + catalogName + ") " + order + "," + tie
	case "contentid":
		sort = tie
	}
	// Sort keys are selected locally; client strings never become SQL identifiers.
	args = append(args, q.Limit, q.Start)
	query := "SELECT " + videoColumns + " FROM public.r18_video WHERE " + where + " ORDER BY " + sort + " LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return items, err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := readVideo(rows)
		if err != nil {
			return items, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
