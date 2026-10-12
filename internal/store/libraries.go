package store

import (
	"database/sql"
	"errors"
	"strings"
)

const SourceDirectory = "nfo_directory"
const SourceAnime = "anime_catalog"
const AnimeSystemKey = "builtin:anime"

var ErrConflict = errors.New("数据已被修改，请刷新后重试")
var ErrLibraryCapability = errors.New("该媒体库不支持目录操作")
var ErrSystemLibrary = errors.New("系统动漫库不能删除，请在设置中关闭")

// Empty SourceType remains compatible with callers constructing directory
// library values themselves; path must still exist as a capability boundary.
func (l Library) IsDirectory() bool {
	return l.Path != "" && (l.SourceType == "" || l.SourceType == SourceDirectory)
}

const libraryColumns = `id,name,COALESCE(path,''),type,enabled,source_type,COALESCE(system_key,''),revision`

func readLibrary(row interface{ Scan(...any) error }) (Library, error) {
	var l Library
	err := row.Scan(&l.ID, &l.Name, &l.Path, &l.Type, &l.Enabled, &l.SourceType, &l.SystemKey, &l.Revision)
	return l, err
}

func (s *Store) EnsureAnimeLibrary() (Library, error) {
	_, err := s.db.Exec(`INSERT INTO libraries(name,path,type,source_type,system_key) SELECT '动漫',NULL,'','anime_catalog',? WHERE NOT EXISTS(SELECT 1 FROM libraries WHERE system_key=?) ON CONFLICT(system_key) DO NOTHING`, AnimeSystemKey, AnimeSystemKey)
	if err != nil {
		return Library{}, err
	}
	return s.AnimeLibrary()
}

// Includes the disabled system library; used for settings, never playback.
func (s *Store) AnimeLibrary() (Library, error) {
	return readLibrary(s.db.QueryRow(`SELECT `+libraryColumns+` FROM libraries WHERE system_key=?`, AnimeSystemKey))
}

func (s *Store) listLibraries(where string) ([]Library, error) {
	rows, err := s.db.Query(`SELECT ` + libraryColumns + ` FROM libraries ` + where + ` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Library{}
	for rows.Next() {
		l, err := readLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (s *Store) AllLibraries() ([]Library, error) { return s.listLibraries("") }
func (s *Store) DirectoryLibraries() ([]Library, error) {
	return s.listLibraries(`WHERE enabled=1 AND source_type='nfo_directory'`)
}

func (s *Store) UpdateAnimeLibrary(name string, enabled bool, revision int64) (Library, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return Library{}, errors.New("库名称需要 1 至 100 个字符")
	}
	result, err := s.db.Exec(`UPDATE libraries SET name=?,enabled=?,revision=revision+1 WHERE system_key=? AND revision=?`, name, enabled, AnimeSystemKey, revision)
	if err != nil {
		return Library{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Library{}, err
	}
	if n == 0 {
		return Library{}, ErrConflict
	}
	l, err := s.AnimeLibrary()
	if err == nil {
		err = s.BumpVersion(l.ID)
	}
	return l, err
}

func (s *Store) RequireDirectory(id int64) error {
	l, err := s.Library(id)
	if err != nil {
		return err
	}
	if !l.IsDirectory() {
		return ErrLibraryCapability
	}
	return nil
}

func affectedOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
