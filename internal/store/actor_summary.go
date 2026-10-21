package store

import "context"

// ActorSummary only includes actors attached to visible movies. MovieCount is
// scoped to the requested library; portraits themselves are shared globally.
type ActorSummary struct {
	Name       string
	MovieCount int
	AvatarTag  string
}

func (s *Store) ActorSummaries(ctx context.Context, libraryID int64) ([]ActorSummary, error) {
	// The movie_actors primary key already makes each movie unique per actor.
	query := `SELECT a.name, COUNT(*), COALESCE(a.avatar_tag,'') FROM actors a
		JOIN movie_actors ma ON ma.actor_name=a.name
		JOIN ` + s.userMoviesSQL() + ` m ON m.id=ma.movie_id
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
		if err := rows.Scan(&item.Name, &item.MovieCount, &item.AvatarTag); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
