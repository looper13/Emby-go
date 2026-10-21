package store

import "fmt"

// CatalogNames returns distinct filter values from visible indexed movies.
// Aggregation stays in SQLite so large libraries do not materialize every movie
// or its metadata in the application. SQL identifiers only come from this switch.
func (s *Store) CatalogNames(kind string, libraryID int64, collection string) ([]string, error) {
	var expression, from, extra string
	from = s.userMoviesSQL() + " m"
	switch kind {
	case "Tag", "Studio", "Genre":
		column := "tags"
		if kind == "Studio" {
			column = "studios"
		} else if kind == "Genre" {
			column = "genres"
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

func (s *Store) EntityExists(kind, name string) (bool, error) {
	from, predicate := s.userMoviesSQL()+" m", ""
	switch kind {
	case "Person":
		predicate = "(TRIM(m.director)=? OR EXISTS(SELECT 1 FROM movie_actors a WHERE a.movie_id=m.id AND a.actor_name=?))"
	case "Genre", "Tag", "Studio":
		column := map[string]string{"Genre": "genres", "Tag": "tags", "Studio": "studios"}[kind]
		from += ", json_each(m." + column + ") j"
		predicate = "j.type='text' AND TRIM(CAST(j.value AS TEXT))=?"
	default:
		return false, nil
	}
	var exists bool
	args := []any{name}
	if kind == "Person" {
		args = append(args, name)
	}
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM "+from+" WHERE m.status IN ('success','manual') AND "+predicate+")", args...).Scan(&exists)
	return exists, err
}
