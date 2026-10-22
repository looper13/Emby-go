package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchIndexesUpgradeAndOrderedPlans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lib, err := s.AddLibrary("Library", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10000)
INSERT INTO movies(library_id,source_path,status,title,created_at,updated_at)
SELECT ?,printf('/test/%d.strm',x),CASE WHEN x%7=0 THEN 'manual' ELSE 'success' END,printf('Title %d',x),'','' FROM n`, lib.ID); err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{"idx_movies_visible_title", "idx_movies_visible_library_title", "idx_movies_visible_created", "idx_movies_visible_library_created", "idx_movie_features_lookup_nocase"} {
		if _, err := s.db.Exec("DROP INDEX " + index); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var indexes int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE type='index' AND name GLOB 'idx_movies_visible_*'").Scan(&indexes); err != nil || indexes != 4 {
		t.Fatalf("upgraded indexes=%d err=%v", indexes, err)
	}
	var id, parent, unused int
	var featurePlan string
	if err := s.db.QueryRow("EXPLAIN QUERY PLAN SELECT movie_id FROM movie_features WHERE kind=? AND value COLLATE NOCASE IN (?)", FeatureGenre, "drama").Scan(&id, &parent, &unused, &featurePlan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(featurePlan, "idx_movie_features_lookup_nocase") || !strings.Contains(featurePlan, "value=?") {
		t.Fatalf("case-insensitive filter does not look up values through upgraded index: %s", featurePlan)
	}
	for _, library := range []bool{false, true} {
		for _, order := range []string{"title,id", "id DESC"} {
			where := "library_id IN (SELECT id FROM " + s.userLibrariesSQL() + ") AND status IN ('success','manual')"
			args := []any{}
			if library {
				where = "EXISTS (SELECT 1 FROM " + s.userLibrariesSQL() + " WHERE id=?) AND status IN ('success','manual') AND library_id=?"
				args = append(args, lib.ID, lib.ID)
			}
			rows, err := s.db.Query("EXPLAIN QUERY PLAN SELECT "+movieCols+" FROM movies WHERE "+where+" ORDER BY "+order+" LIMIT 100", args...)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				if strings.Contains(detail, "TEMP B-TREE") {
					rows.Close()
					t.Fatalf("library=%v order=%s sorts catalog: %s", library, order, detail)
				}
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			rows.Close()
		}
	}
}
