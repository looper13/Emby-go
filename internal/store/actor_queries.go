package store

import (
	"context"
	"strings"
)

const actorQueryBatchSize = 500

// ActorAvatars resolves page-sized sets without one query per person. Missing
// actors (including director-only identities) are deliberately absent.
func (s *Store) ActorAvatars(ctx context.Context, names []string) (map[string]ActorRef, error) {
	out := make(map[string]ActorRef, len(names))
	for start := 0; start < len(names); start += actorQueryBatchSize {
		batch := names[start:min(start+actorQueryBatchSize, len(names))]
		args := make([]any, len(batch))
		for i, name := range batch {
			args[i] = name
		}
		rows, err := s.db.QueryContext(ctx, `SELECT name, COALESCE(avatar_url,''), COALESCE(avatar_tag,'')
			FROM actors WHERE name IN (`+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var actor ActorRef
			if err := rows.Scan(&actor.Name, &actor.AvatarURL, &actor.AvatarTag); err != nil {
				rows.Close()
				return nil, err
			}
			out[actor.Name] = actor
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PersonPosters preserves EntityPoster's global, latest-visible-movie fallback,
// but visits only the requested actors' relations instead of scanning movies
// separately for every person on a page.
func (s *Store) PersonPosters(ctx context.Context, names []string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	for start := 0; start < len(names); start += actorQueryBatchSize {
		batch := names[start:min(start+actorQueryBatchSize, len(names))]
		args := make([]any, len(batch))
		for i, name := range batch {
			args[i] = name
		}
		rows, err := s.db.QueryContext(ctx, `WITH latest AS (
			SELECT ma.actor_name, MAX(ma.movie_id) AS movie_id FROM movie_actors ma
			JOIN movies m ON m.id=ma.movie_id
			WHERE ma.actor_name IN (`+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+`)
			AND m.status IN ('success','manual') GROUP BY ma.actor_name
		) SELECT latest.actor_name, COALESCE(NULLIF(m.poster_path,''),m.source_path)
		FROM latest JOIN movies m ON m.id=latest.movie_id`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var name, path string
			if err := rows.Scan(&name, &path); err != nil {
				rows.Close()
				return nil, err
			}
			out[name] = path
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
