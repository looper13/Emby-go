package store

import "context"

// ActorSummary only includes actors attached to visible movies. MovieCount is
// scoped to the requested library; portraits themselves are shared globally.
type ActorSummary struct {
	Name       string
	MovieCount int
}

func (s *Store) ActorSummaries(ctx context.Context, libraryID int64) ([]ActorSummary, error) {
	query := `SELECT a.name, COUNT(DISTINCT m.id) FROM actors a
		JOIN movie_actors ma ON ma.actor_name=a.name
		JOIN movies m ON m.id=ma.movie_id
		WHERE m.status IN ('success','manual')`
	args := []any{}
	if libraryID > 0 {
		query += " AND m.library_id=?"
		args = append(args, libraryID)
	}
	query += " GROUP BY a.name ORDER BY a.name"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActorSummary
	for rows.Next() {
		var item ActorSummary
		if err := rows.Scan(&item.Name, &item.MovieCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
