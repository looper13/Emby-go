package store

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Television belongs to a playable source, so rescans keep its item id and user
// data. Parent metadata shares the episode's NFO freshness/commit boundary.
type Television struct {
	SeriesPath    string `json:"series_path"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title"`
	Plot          string `json:"plot"`
	Year          int    `json:"year"`
	Season        int    `json:"season"`
	Episode       int    `json:"episode"`
}

func (m Movie) ItemType() string {
	if m.Television != nil {
		return "Episode"
	}
	return "Movie"
}

func mediaTypeClause(types []string) string {
	if len(types) == 0 {
		return ""
	}
	movie, episode := false, false
	for _, typ := range types {
		movie = movie || typ == "Movie" || typ == "Video"
		episode = episode || typ == "Episode" || typ == "Video"
	}
	if movie && episode {
		return ""
	}
	if movie {
		return " AND television='null'"
	}
	if episode {
		return " AND television<>'null'"
	}
	return " AND 0=1"
}

func (s *Store) migrateTelevision() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var done int
	if err = tx.QueryRow("SELECT count(*) FROM schema_migrations WHERE version=4").Scan(&done); err != nil || done != 0 {
		return err
	}
	if _, err = tx.Exec(`ALTER TABLE movies ADD COLUMN television TEXT NOT NULL DEFAULT 'null' CHECK(json_valid(television));
CREATE INDEX movies_television_series ON movies(library_id,json_extract(television,'$.series_path'),json_extract(television,'$.season'),json_extract(television,'$.episode'));
INSERT INTO schema_migrations(version) VALUES(4);`); err != nil {
		return err
	}
	return tx.Commit()
}

func TVSeriesID(libraryID int64, path string) string {
	return fmt.Sprintf("tv-series:%d:%s", libraryID, base64.RawURLEncoding.EncodeToString([]byte(filepath.Clean(path))))
}
func TVSeasonID(libraryID int64, path string, season int) string {
	return fmt.Sprintf("tv-season:%d:%d:%s", libraryID, season, base64.RawURLEncoding.EncodeToString([]byte(filepath.Clean(path))))
}
func ParseTVID(id string) (kind string, libraryID int64, path string, season int, ok bool) {
	parts := strings.Split(id, ":")
	if len(parts) < 3 {
		return
	}
	switch parts[0] {
	case "tv-series":
		if len(parts) != 3 {
			return
		}
		kind = "Series"
	case "tv-season":
		if len(parts) != 4 {
			return
		}
		kind = "Season"
		n, err := strconv.Atoi(parts[2])
		if err != nil || n < 0 {
			return
		}
		season = n
	default:
		return
	}
	libraryID, _ = strconv.ParseInt(parts[1], 10, 64)
	b64 := parts[len(parts)-1]
	b64 = strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(b64, "+", "-"), "/", "_"), "=")
	decoded, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil || len(decoded) == 0 || libraryID <= 0 {
		return
	}
	path, ok = string(decoded), true
	return
}

func (s *Store) TVEpisodes(libraryID int64, seriesPath string) ([]Movie, error) {
	where := `status IN ('success','manual') AND television <> 'null' AND library_id IN (SELECT id FROM ` + s.userLibrariesSQL() + ` WHERE source_type='nfo_directory')`
	args := []any{}
	if libraryID > 0 {
		where += " AND library_id=?"
		args = append(args, libraryID)
	}
	if seriesPath != "" {
		where += " AND json_extract(television,'$.series_path')=?"
		args = append(args, seriesPath)
	}
	rows, err := s.db.Query("SELECT "+movieCols+" FROM movies WHERE "+where+" ORDER BY library_id,json_extract(television,'$.series_path'),json_extract(television,'$.season'),json_extract(television,'$.episode'),id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Movie{}
	var previousLibrary int64
	previousPath := ""
	previousSeason, previousEpisode := -1, -1
	for rows.Next() {
		m, err := movieScan(rows)
		if err != nil {
			return nil, err
		}
		if m.LibraryID == previousLibrary && m.Television.SeriesPath == previousPath && m.Television.Season == previousSeason && m.Television.Episode == previousEpisode {
			continue
		}
		out = append(out, m)
		previousLibrary, previousPath = m.LibraryID, m.Television.SeriesPath
		previousSeason, previousEpisode = m.Television.Season, m.Television.Episode
	}
	return out, rows.Err()
}

// TVEpisodeVersions returns every playable source for the same logical episode.
// The earliest item remains the stable episode id in television listings.
func (s *Store) TVEpisodeVersions(id int64) ([]Movie, error) {
	m, err := s.Movie(id)
	if err != nil || m.Television == nil || !m.IsVisible() || !s.CanAccessLibrary(m.LibraryID) {
		return nil, sql.ErrNoRows
	}
	rows, err := s.db.Query("SELECT "+movieCols+` FROM movies WHERE library_id=? AND status IN ('success','manual') AND json_extract(television,'$.series_path')=? AND json_extract(television,'$.season')=? AND json_extract(television,'$.episode')=? ORDER BY id`, m.LibraryID, m.Television.SeriesPath, m.Television.Season, m.Television.Episode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Movie{}
	for rows.Next() {
		version, err := movieScan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, version)
	}
	return out, rows.Err()
}

func (s *Store) TVFolderEpisodes(id string) ([]Movie, error) {
	_, lib, path, _, ok := ParseTVID(id)
	if !ok {
		return nil, sql.ErrNoRows
	}
	items, err := s.TVEpisodes(lib, path)
	if err == nil && len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	return items, err
}

func televisionJSON(tv *Television) string { b, _ := json.Marshal(tv); return string(b) }

func (s *Store) SearchMediaTypes(types []string, libraryID int64, term, years, genre, tags, studios, person string, unplayed bool, favorite *bool, sortBy string, desc bool, limit, offset int, ratings string) ([]Movie, int, error) {
	return s.SearchMediaExcluding(nil, types, libraryID, term, years, genre, tags, studios, person, unplayed, favorite, sortBy, desc, limit, offset, ratings)
}
func (s *Store) SearchMediaExcluding(exclude []int64, types []string, libraryID int64, term, years, genre, tags, studios, person string, unplayed bool, favorite *bool, sortBy string, desc bool, limit, offset int, ratings string) ([]Movie, int, error) {
	return s.searchExcluding(exclude, libraryID, term, "", years, genre, tags, studios, person, "", "", "", unplayed, favorite, movieOrder{by: sortBy, desc: desc, itemTypes: types}, limit, offset, ratings)
}
