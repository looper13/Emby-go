package store

import "fmt"

// CatalogNames returns distinct filter values from visible indexed movies.
// Aggregation stays in SQLite so large libraries do not materialize every movie
// or its metadata in the application. SQL identifiers only come from this switch.
func (s *Store) CatalogNames(kind string, libraryID int64, collection string) ([]string, error) {
	var expression, from, extra string
	from = "movies m"
	switch kind {
	case "Tag", "Studio":
		column := "tags"
		if kind == "Studio" {
			column = "studios"
		}
		from += ", json_each(m." + column + ") j"
		expression = "TRIM(CAST(j.value AS TEXT))"
		extra = " AND j.type='text'"
	case "Year":
		expression = "CAST(m.year AS TEXT)"
		extra = " AND m.year>0"
	case "OfficialRating":
		expression = "TRIM(m.official_rating)"
	default:
		return nil, fmt.Errorf("unsupported catalog kind: %s", kind)
	}
	query := "SELECT DISTINCT " + expression + " FROM " + from + " WHERE m.status IN ('success','manual') AND " + expression + "<>''" + extra
	args := []any{}
	if libraryID > 0 {
		query += " AND m.library_id=?"
		args = append(args, libraryID)
	}
	if collection == "*" {
		query += " AND COALESCE(m.collection,'')<>''"
	} else if collection != "" {
		query += " AND m.collection=?"
		args = append(args, collection)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
