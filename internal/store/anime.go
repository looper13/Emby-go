package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"emby-go/internal/anime"
)

func animeJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func animeNow() string       { return time.Now().UTC().Format(time.RFC3339) }

// Reserve SQLite's writer before reading a snapshot. A deferred transaction
// cannot upgrade a stale WAL snapshot if a scanner writes between read and save.
func (s *Store) beginAnimeWrite(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE schema_migrations SET version=version WHERE version=1`); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// SaveAnimeSubject commits complete episode pages together. Refresh never
// deletes bindings, rewrites existing display indices, or merges sequels.
func (s *Store) SaveAnimeSubject(ctx context.Context, subject anime.Subject, episodes []anime.Episode) (int64, error) {
	return s.saveAnimeSubject(ctx, subject, episodes, "", "")
}

func (s *Store) saveAnimeSubject(ctx context.Context, subject anime.Subject, episodes []anime.Episode, archiveDigest, publishedAt string) (int64, error) {
	if subject.BangumiID <= 0 || strings.TrimSpace(subject.Name) == "" {
		return 0, errors.New("invalid anime subject")
	}
	seen := map[int64]bool{}
	for _, e := range episodes {
		if err := anime.ValidateEpisode(e); err != nil {
			return 0, err
		}
		if seen[e.BangumiID] {
			return 0, errors.New("duplicate episode")
		}
		seen[e.BangumiID] = true
	}
	sort.SliceStable(episodes, func(i, j int) bool {
		a, _ := episodes[i].Sort.Float64()
		b, _ := episodes[j].Sort.Float64()
		if a == b {
			return episodes[i].BangumiID < episodes[j].BangumiID
		}
		return a < b
	})
	tx, err := s.beginAnimeWrite(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if archiveDigest != "" {
		var id int64
		var previous, apiTime string
		err = tx.QueryRowContext(ctx, `SELECT s.id,COALESCE(o.archive_digest,''),COALESCE(o.api_updated_at,'') FROM anime_subjects s LEFT JOIN anime_subject_sources o ON o.subject_id=s.id WHERE s.bangumi_id=?`, subject.BangumiID).Scan(&id, &previous, &apiTime)
		if err != nil && err != sql.ErrNoRows {
			return 0, err
		}
		if err == nil && (previous == archiveDigest || apiTime >= publishedAt) {
			return id, nil
		}
	}
	var oldRaw string
	err = tx.QueryRowContext(ctx, `SELECT metadata FROM anime_subjects WHERE bangumi_id=?`, subject.BangumiID).Scan(&oldRaw)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if err == nil {
		var old anime.Subject
		if err = json.Unmarshal([]byte(oldRaw), &old); err != nil {
			return 0, err
		}
		if subject.Image == "" {
			subject.Image = old.Image
		}
		if subject.Summary == "" {
			subject.Summary = old.Summary
		}
		if subject.OriginalName == "" {
			subject.OriginalName = old.OriginalName
		}
		if subject.Platform == "" {
			subject.Platform = old.Platform
		}
		if subject.Date == "" {
			subject.Date = old.Date
		}
		if subject.Episodes == 0 {
			subject.Episodes = old.Episodes
		}
	}
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO anime_subjects(bangumi_id,name,platform,metadata,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(bangumi_id) DO UPDATE SET name=excluded.name,platform=excluded.platform,metadata=excluded.metadata,updated_at=excluded.updated_at RETURNING id`, subject.BangumiID, subject.Name, subject.Platform, animeJSON(subject), animeNow()).Scan(&id)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE anime_episodes SET active=0 WHERE subject_id=?`, id); err != nil {
		return 0, err
	}
	for _, e := range episodes {
		// The current season/index remain stable for existing IDs, even if upstream
		// renumbers episodes. An explicit future migration may handle corrections.
		var owner int64
		err = tx.QueryRowContext(ctx, `SELECT subject_id FROM anime_episodes WHERE bangumi_id=?`, e.BangumiID).Scan(&owner)
		if err != nil && err != sql.ErrNoRows {
			return 0, err
		}
		if err == nil && owner != id {
			return 0, errors.New("episode belongs to another subject")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO anime_episodes(subject_id,bangumi_id,sort,season,emby_index,metadata) VALUES(?,?,?,?,(SELECT COALESCE(MAX(emby_index),0)+1 FROM anime_episodes WHERE subject_id=? AND season=?),?) ON CONFLICT(bangumi_id) DO UPDATE SET sort=excluded.sort,metadata=excluded.metadata,active=1`, id, e.BangumiID, e.Sort.String(), e.Season, id, e.Season, animeJSON(e))
		if err != nil {
			return 0, err
		}
	}
	if archiveDigest == "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO anime_subject_sources(subject_id,api_updated_at) VALUES(?,?) ON CONFLICT(subject_id) DO UPDATE SET api_updated_at=excluded.api_updated_at`, id, animeNow())
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO anime_subject_sources(subject_id,archive_digest) VALUES(?,?) ON CONFLICT(subject_id) DO UPDATE SET archive_digest=excluded.archive_digest`, id, archiveDigest)
	}
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) AnimeSubject(id int64) (anime.Subject, error) {
	var v anime.Subject
	var raw string
	err := s.db.QueryRow(`SELECT id,bangumi_id,metadata,updated_at FROM anime_subjects WHERE id=?`, id).Scan(&v.ID, &v.BangumiID, &raw, &v.UpdatedAt)
	if err != nil {
		return v, err
	}
	var meta anime.Subject
	if err = json.Unmarshal([]byte(raw), &meta); err != nil {
		return v, err
	}
	meta.ID, meta.BangumiID, meta.UpdatedAt = v.ID, v.BangumiID, v.UpdatedAt
	return meta, nil
}

func readEpisode(row interface{ Scan(...any) error }) (anime.Episode, error) {
	var v anime.Episode
	var raw, number string
	err := row.Scan(&v.ID, &v.SubjectID, &v.BangumiID, &number, &v.Season, &v.Index, &raw, &v.Active)
	if err != nil {
		return v, err
	}
	var meta anime.Episode
	if err = json.Unmarshal([]byte(raw), &meta); err != nil {
		return v, err
	}
	meta.ID, meta.SubjectID, meta.BangumiID, meta.Sort, meta.Season, meta.Index = v.ID, v.SubjectID, v.BangumiID, json.Number(number), v.Season, v.Index
	meta.Active = v.Active
	return meta, nil
}

const episodeColumns = `id,subject_id,bangumi_id,sort,season,emby_index,metadata,active`

func (s *Store) AnimeEpisode(id int64) (anime.Episode, error) {
	return readEpisode(s.db.QueryRow(`SELECT `+episodeColumns+` FROM anime_episodes WHERE id=?`, id))
}
func (s *Store) AnimeEpisodes(subjectID int64) ([]anime.Episode, error) {
	rows, err := s.db.Query(`SELECT `+episodeColumns+` FROM anime_episodes WHERE subject_id=? ORDER BY season DESC,CAST(sort AS REAL),id`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []anime.Episode{}
	for rows.Next() {
		e, err := readEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

const bindingColumns = `id,subject_id,COALESCE(episode_id,0),COALESCE(movie_id,0),revision,status,source_version,checked_at,error`

func readBinding(row interface{ Scan(...any) error }) (anime.Binding, error) {
	var b anime.Binding
	err := row.Scan(&b.ID, &b.SubjectID, &b.EpisodeID, &b.MovieID, &b.Revision, &b.Status, &b.SourceVersion, &b.CheckedAt, &b.Error)
	if b.MovieID == 0 {
		b.Status = "unbound"
	}
	return b, err
}
func (s *Store) AnimeBinding(subjectID, episodeID int64) (anime.Binding, error) {
	b, err := readBinding(s.db.QueryRow(`SELECT `+bindingColumns+` FROM anime_media_bindings WHERE subject_id=? AND COALESCE(episode_id,0)=?`, subjectID, episodeID))
	if err == sql.ErrNoRows {
		return anime.Binding{SubjectID: subjectID, EpisodeID: episodeID, Status: "unbound"}, nil
	}
	return b, err
}

// BindAnime uses a compare-and-swap revision and SQL uniqueness so two tabs
// cannot silently replace each other's media. A tombstone keeps unbind revisions.
func (s *Store) BindAnime(subjectID, episodeID, movieID, revision int64) (anime.Binding, error) {
	sub, err := s.AnimeSubject(subjectID)
	if err != nil {
		return anime.Binding{}, err
	}
	if episodeID != 0 {
		ep, err := s.AnimeEpisode(episodeID)
		if err != nil || ep.SubjectID != subjectID || !sub.IsSeries() {
			return anime.Binding{}, errors.New("invalid episode target")
		}
	} else if !sub.IsMovie() {
		return anime.Binding{}, errors.New("series must bind an episode")
	}
	if movieID != 0 {
		m, err := s.Movie(movieID)
		if err != nil {
			return anime.Binding{}, err
		}
		if !m.IsVisible() || !strings.EqualFold(filepath.Ext(m.SourcePath), ".strm") || len(m.AdditionalParts) != 0 {
			return anime.Binding{}, errors.New("请选择可见的单文件 STRM 媒体")
		}
		if err = s.RequireDirectory(m.LibraryID); err != nil {
			return anime.Binding{}, err
		}
	}
	tx, err := s.beginAnimeWrite(context.Background())
	if err != nil {
		return anime.Binding{}, err
	}
	defer tx.Rollback()
	var ep, media any
	if episodeID != 0 {
		ep = episodeID
	}
	if movieID != 0 {
		media = movieID
	}
	var result sql.Result
	if revision == 0 {
		result, err = tx.Exec(`INSERT INTO anime_media_bindings(subject_id,episode_id,movie_id) VALUES(?,?,?)`, subjectID, ep, media)
	} else {
		result, err = tx.Exec(`UPDATE anime_media_bindings SET movie_id=?,revision=revision+1,status='unchecked',source_version='',checked_at='',error='' WHERE subject_id=? AND COALESCE(episode_id,0)=? AND revision=?`, media, subjectID, episodeID, revision)
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return anime.Binding{}, fmt.Errorf("%w：该媒体已关联或条目已有绑定", ErrConflict)
		}
		return anime.Binding{}, err
	}
	if err = affectedOne(result, nil); err != nil {
		return anime.Binding{}, err
	}
	if err = tx.Commit(); err != nil {
		return anime.Binding{}, err
	}
	return s.AnimeBinding(subjectID, episodeID)
}

func (s *Store) SetAnimeAvailability(b anime.Binding, status, sourceVersion, message string) error {
	return affectedOne(s.db.Exec(`UPDATE anime_media_bindings SET status=?,source_version=?,checked_at=?,error=? WHERE id=? AND revision=? AND movie_id=?`, status, sourceVersion, animeNow(), message, b.ID, b.Revision, b.MovieID))
}

func (s *Store) AnimeTarget(raw string) (anime.Subject, anime.Episode, anime.Binding, error) {
	var sub anime.Subject
	var ep anime.Episode
	var b anime.Binding
	kind, id, _, ok := anime.ParseID(raw)
	if !ok || (kind != "movie" && kind != "episode") {
		return sub, ep, b, sql.ErrNoRows
	}
	var err error
	if kind == "episode" {
		ep, err = s.AnimeEpisode(id)
		if err != nil {
			return sub, ep, b, err
		}
		id = ep.SubjectID
	}
	sub, err = s.AnimeSubject(id)
	if err != nil {
		return sub, ep, b, err
	}
	if sub.IsMovie() != (kind == "movie") || kind == "episode" && !sub.IsSeries() {
		return sub, ep, b, sql.ErrNoRows
	}
	b, err = s.AnimeBinding(id, ep.ID)
	return sub, ep, b, err
}

func (s *Store) AnimeSubjectIDs() ([]int64, error) {
	rows, err := s.db.Query(`SELECT DISTINCT subject_id FROM anime_media_bindings WHERE movie_id IS NOT NULL ORDER BY subject_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
