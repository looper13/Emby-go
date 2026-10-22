package store

import "log/slog"

func (s *Store) migrateSearchIndexes() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Visible rows are the wall's default scope. Keep both whole-catalog and
	// library ordering available without indexing pending metadata four times.
	for _, query := range []string{
		`CREATE INDEX IF NOT EXISTS idx_movies_visible_title ON movies(title,id) WHERE status IN ('success','manual')`,
		`CREATE INDEX IF NOT EXISTS idx_movies_visible_library_title ON movies(library_id,title,id) WHERE status IN ('success','manual')`,
		`CREATE INDEX IF NOT EXISTS idx_movies_visible_created ON movies(id) WHERE status IN ('success','manual')`,
		`CREATE INDEX IF NOT EXISTS idx_movies_visible_library_created ON movies(library_id,id) WHERE status IN ('success','manual')`,
		// Keep case-insensitive wall filters indexed without changing the
		// case-sensitive features used for similarity scoring.
		`CREATE INDEX IF NOT EXISTS idx_movie_features_lookup_nocase ON movie_features(kind,value COLLATE NOCASE,movie_id)`,
	} {
		if _, err := tx.Exec(query); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.OptimizeSearchIndexes()
	return nil
}

// OptimizeSearchIndexes refreshes planner statistics at startup and after scan
// jobs, not on list requests or playback updates. SQLite bounds the analysis
// and skips tables that do not need it.
func (s *Store) OptimizeSearchIndexes() {
	if _, err := s.db.Exec("PRAGMA optimize=0x10002"); err != nil {
		slog.Warn("更新查询索引统计失败", "error", err)
	}
}
