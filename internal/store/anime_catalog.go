package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"emby-go/internal/anime"
)

func (s *Store) AnimeSettings() (anime.Settings, error) {
	v := anime.DefaultSettings()
	raw, err := s.Setting("anime.config")
	if err != nil {
		return v, err
	}
	if raw != "" {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return v, err
}

// Configuration and library ownership remain separate, but an explicit save is
// atomic across both records so a conflict cannot partially save the form.
func (s *Store) SaveAnimeSettings(cfg anime.Settings, name string, enabled bool, libRevision int64) (anime.Settings, error) {
	if err := anime.ValidateBaseURL(cfg.MikanURL); err != nil {
		return cfg, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return cfg, errors.New("库名称需要 1 至 100 个字符")
	}
	tx, err := s.beginAnimeWrite(context.Background())
	if err != nil {
		return cfg, err
	}
	defer tx.Rollback()
	current := anime.DefaultSettings()
	var raw string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key='anime.config'`).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return cfg, err
	}
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), &current); err != nil {
			return cfg, err
		}
	}
	if current.Revision != cfg.Revision {
		return cfg, ErrConflict
	}
	if err = affectedOne(tx.Exec(`UPDATE libraries SET name=?,enabled=?,revision=revision+1 WHERE system_key=? AND revision=?`, name, enabled, AnimeSystemKey, libRevision)); err != nil {
		return cfg, err
	}
	cfg.Revision++
	cfg.MikanURL = strings.TrimRight(cfg.MikanURL, "/")
	_, err = tx.Exec(`INSERT INTO settings(key,value) VALUES('anime.config',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, animeJSON(cfg))
	if err != nil {
		return cfg, err
	}
	if err = tx.Commit(); err != nil {
		return cfg, err
	}
	if l, e := s.AnimeLibrary(); e == nil {
		_ = s.BumpVersion(l.ID)
	}
	return cfg, nil
}

func upsertAnimeEntry(ctx context.Context, tx *sql.Tx, e anime.Entry) error {
	if e.ID <= 0 || strings.TrimSpace(e.Name) == "" {
		return errors.New("invalid mikan entry")
	}
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT metadata FROM anime_mikan_entries WHERE id=?`, e.ID).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		var old anime.Entry
		if err = json.Unmarshal([]byte(raw), &old); err != nil {
			return err
		}
		if e.Image == "" {
			e.Image = old.Image
		}
		if e.BangumiID == 0 {
			e.BangumiID = old.BangumiID
		}
		// Detail pages do not carry seasonal membership or a weekday.
		if e.Year == 0 {
			e.Year, e.Season, e.Weekday = old.Year, old.Season, old.Weekday
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO anime_mikan_entries(id,name,year,season,weekday,metadata,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,year=excluded.year,season=excluded.season,weekday=excluded.weekday,metadata=excluded.metadata,updated_at=excluded.updated_at`, e.ID, e.Name, e.Year, e.Season, e.Weekday, animeJSON(e), animeNow())
	return err
}
func (s *Store) SaveAnimeCatalog(ctx context.Context, entries []anime.Entry) error {
	if len(entries) == 0 || len(entries) > 2000 {
		return errors.New("invalid catalog size")
	}
	tx, err := s.beginAnimeWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range entries {
		if err = upsertAnimeEntry(ctx, tx, e); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO anime_catalog_entries(year,season,mikan_id,weekday) VALUES(?,?,?,?) ON CONFLICT(year,season,mikan_id) DO UPDATE SET weekday=excluded.weekday`, e.Year, e.Season, e.ID, e.Weekday); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) SaveAnimeEntry(ctx context.Context, e anime.Entry, releases []anime.Release) error {
	tx, err := s.beginAnimeWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = upsertAnimeEntry(ctx, tx, e); err != nil {
		return err
	}
	for _, r := range releases {
		if len(r.ID) != 40 || r.Title == "" {
			return errors.New("invalid release")
		}
		r.MikanID = e.ID
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT metadata FROM anime_releases WHERE id=?`, r.ID).Scan(&raw)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			var old anime.Release
			if err = json.Unmarshal([]byte(raw), &old); err != nil {
				return err
			}
			if r.Group == "" {
				r.Group = old.Group
			}
			if r.PublishedAt == "" {
				r.PublishedAt = old.PublishedAt
			}
			if r.Size == 0 {
				r.Size = old.Size
			}
			if r.SizeText == "" {
				r.SizeText = old.SizeText
			}
			if r.Torrent == "" {
				r.Torrent = old.Torrent
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO anime_releases(id,mikan_id,metadata,published_at) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata,published_at=CASE WHEN excluded.published_at='' THEN anime_releases.published_at ELSE excluded.published_at END`, r.ID, e.ID, animeJSON(r), r.PublishedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

const animeEntryColumns = `e.id,COALESCE(e.subject_id,0),e.metadata,e.manual,e.revision,e.updated_at`

func readAnimeEntry(row interface{ Scan(...any) error }) (anime.Entry, error) {
	var e, meta anime.Entry
	var raw string
	err := row.Scan(&e.ID, &e.SubjectID, &raw, &e.Manual, &e.Revision, &e.UpdatedAt)
	if err != nil {
		return e, err
	}
	if err = json.Unmarshal([]byte(raw), &meta); err != nil {
		return e, err
	}
	meta.ID, meta.SubjectID, meta.Manual, meta.Revision, meta.UpdatedAt = e.ID, e.SubjectID, e.Manual, e.Revision, e.UpdatedAt
	return meta, nil
}
func (s *Store) AnimeEntry(id int64) (anime.Entry, error) {
	return readAnimeEntry(s.db.QueryRow(`SELECT `+animeEntryColumns+` FROM anime_mikan_entries e WHERE e.id=?`, id))
}

type AnimeCatalogFilter struct {
	Source           string
	OnlyPlayable     bool
	PlayableSubjects []int64
}

func (s *Store) AnimeCatalog(year, season, weekday int, search string, limit, offset int, filters ...AnimeCatalogFilter) ([]anime.Entry, int, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where := ` FROM anime_mikan_entries e JOIN anime_catalog_entries c ON c.mikan_id=e.id WHERE c.year=? AND c.season=?`
	args := []any{year, season}
	if len(filters) > 0 {
		filter := filters[0]
		if filter.Source == "bangumi" {
			where += ` AND e.subject_id IS NOT NULL`
		}
		if filter.Source == "mikan" {
			where += ` AND e.subject_id IS NULL`
		}
		if filter.OnlyPlayable {
			where += ` AND e.subject_id IN (SELECT value FROM json_each(?))`
			args = append(args, animeJSON(filter.PlayableSubjects))
		}
	}
	if weekday >= 0 {
		where += ` AND c.weekday=?`
		args = append(args, weekday)
	}
	if search != "" {
		where += ` AND e.name LIKE ? ESCAPE '\'`
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		args = append(args, "%"+escaped+"%")
	}
	var total int
	if err := s.db.QueryRow(`SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT e.id,COALESCE(e.subject_id,0),json_set(e.metadata,'$.weekday',c.weekday),e.manual,e.revision,e.updated_at`+where+` ORDER BY c.weekday,e.name,e.id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []anime.Entry{}
	for rows.Next() {
		e, err := readAnimeEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		e.Year, e.Season = year, season
		out = append(out, e)
	}
	return out, total, rows.Err()
}
func (s *Store) MapAnimeEntry(id, subjectID, revision int64, manual bool) error {
	query := `UPDATE anime_mikan_entries SET subject_id=?,manual=?,revision=revision+1 WHERE id=? AND revision=?`
	if !manual {
		query += ` AND manual=0`
	}
	return affectedOne(s.db.Exec(query, subjectID, manual, id, revision))
}
func (s *Store) AnimeReleases(id int64, limit, offset int, filter string) ([]anime.Release, int, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where := ` WHERE mikan_id=?`
	args := []any{id}
	if filter != "" {
		where += ` AND (json_extract(metadata,'$.group')=? OR json_extract(metadata,'$.resolution')=? OR json_extract(metadata,'$.subtitle')=?)`
		args = append(args, filter, filter, filter)
	}
	var total int
	if err := s.db.QueryRow(`SELECT count(*) FROM anime_releases`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT metadata FROM anime_releases`+where+` ORDER BY published_at DESC,id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []anime.Release{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, 0, err
		}
		var r anime.Release
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

type AnimeSyncState struct {
	Scope         string `json:"scope"`
	Generation    int64  `json:"generation"`
	Status        string `json:"status"`
	UpdatedAt     string `json:"updated_at"`
	LastSuccessAt string `json:"last_success_at"`
	Error         string `json:"error"`
}

// The marker persists even when a user deletes the default task. Restart and
// future catalog refreshes must not silently recreate or re-enable it.
func (s *Store) EnsureAnimeSchedule() error {
	tx, err := s.beginAnimeWrite(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRow(`SELECT count(*) FROM settings WHERE key='anime.schedule_initialized'`).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return nil
	}
	now := animeNow()
	_, err = tx.Exec(`INSERT INTO scheduled_tasks(name,type,cron,params,enabled,created_at,updated_at) SELECT '动漫资料同步','anime_sync','0 */6 * * *','{}',1,?,? WHERE NOT EXISTS(SELECT 1 FROM scheduled_tasks WHERE type='anime_sync')`, now, now)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES('anime.schedule_initialized','1')`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetAnimeSyncState(scope string, generation int64, status, message string) error {
	now, success := animeNow(), ""
	if status == "success" {
		success = now
	}
	_, err := s.db.Exec(`INSERT INTO anime_sync_state(scope,generation,status,updated_at,error,last_success_at) VALUES(?,?,?,?,?,?) ON CONFLICT(scope) DO UPDATE SET generation=excluded.generation,status=excluded.status,updated_at=excluded.updated_at,error=excluded.error,last_success_at=CASE WHEN excluded.last_success_at<>'' THEN excluded.last_success_at ELSE anime_sync_state.last_success_at END`, scope, generation, status, now, message, success)
	return err
}
func (s *Store) AnimeSync(scope string) (AnimeSyncState, error) {
	var v AnimeSyncState
	err := s.db.QueryRow(`SELECT scope,generation,status,updated_at,error,last_success_at FROM anime_sync_state WHERE scope=?`, scope).Scan(&v.Scope, &v.Generation, &v.Status, &v.UpdatedAt, &v.Error, &v.LastSuccessAt)
	if err == sql.ErrNoRows {
		return v, nil
	}
	return v, err
}

// Refresh at most 100 previously opened or media-backed entries, oldest first.
// Attempt timestamps provide a durable cursor across runs and failed entries.
func (s *Store) AnimeRefreshEntries(year, season int) ([]int64, error) {
	rows, err := s.db.Query(`SELECT e.id FROM anime_mikan_entries e JOIN anime_catalog_entries c ON c.mikan_id=e.id LEFT JOIN anime_sync_state st ON st.scope='entry:'||e.id WHERE c.year=? AND c.season=? AND (EXISTS(SELECT 1 FROM anime_media_bindings b WHERE b.subject_id=e.subject_id AND b.movie_id IS NOT NULL) OR datetime(e.last_opened_at)>=datetime('now','-7 days')) ORDER BY COALESCE(st.updated_at,''),e.id LIMIT 100`, year, season)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// A scheduled refresh is not a user visit. Keep this separate from sync state
// so an entry ages out when the user stops opening it.
func (s *Store) TouchAnimeEntry(id int64) error {
	_, err := s.db.Exec(`UPDATE anime_mikan_entries SET last_opened_at=? WHERE id=? AND (last_opened_at='' OR datetime(last_opened_at)<datetime('now','-1 hour'))`, animeNow(), id)
	return err
}
