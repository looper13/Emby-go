package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

const SourceDirectory = "nfo_directory"

const (
	LibraryMovies  = "movies"
	LibraryTVShows = "tvshows"
	LibraryMixed   = "mixed"
)

func (l Library) CollectionType() string {
	if l.Type == LibraryMixed {
		return ""
	}
	if l.Type == "" {
		return LibraryMovies
	}
	return l.Type
}

var ErrConflict = errors.New("数据已被修改，请刷新后重试")
var ErrLibraryCapability = errors.New("该媒体库不支持目录操作")

// 未填写来源类型的旧调用仍按目录库处理，目录列表必须非空。
func (l Library) IsDirectory() bool {
	return (l.SourceType == "" || l.SourceType == SourceDirectory) && len(l.DirectoryPaths()) > 0
}

const libraryColumns = `id,name,path,type,enabled,source_type,revision,COALESCE(proxy_enabled,0),resolve_redirects,paths,proxy_headers`

func readLibrary(row interface{ Scan(...any) error }) (Library, error) {
	var l Library
	var paths, headers string
	err := row.Scan(&l.ID, &l.Name, &l.Path, &l.Type, &l.Enabled, &l.SourceType, &l.Revision, &l.ProxyEnabled, &l.ResolveRedirects, &paths, &headers)
	if err != nil {
		return l, err
	}
	if err = json.Unmarshal([]byte(paths), &l.Paths); err != nil {
		return l, err
	}
	if err = json.Unmarshal([]byte(headers), &l.ProxyHeaders); err != nil {
		return l, err
	}
	if l.Paths == nil {
		l.Paths = []string{}
	}
	if (l.SourceType == "" || l.SourceType == SourceDirectory) && len(l.Paths) == 0 && l.Path != "" {
		l.Paths = []string{l.Path}
	}
	if l.ProxyHeaders == nil {
		l.ProxyHeaders = map[string]string{}
	}
	return l, err
}

func (s *Store) UpdateLibraryProxy(id int64, enabled bool, revision int64) (Library, error) {
	library, err := s.Library(id)
	if err != nil {
		return Library{}, err
	}
	return s.UpdateLibraryPlaybackSettings(id, enabled, library.ResolveRedirects, revision)
}

func (s *Store) UpdateLibraryPlaybackSettings(id int64, proxyEnabled, resolveRedirects bool, revision int64) (Library, error) {
	library, err := s.Library(id)
	if err != nil {
		return Library{}, err
	}
	return s.UpdateDirectoryLibrary(id, library.Name, library.DirectoryPaths(), proxyEnabled, resolveRedirects, library.ProxyHeaders, revision)
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
