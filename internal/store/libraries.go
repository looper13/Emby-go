package store

import (
	"database/sql"
	"errors"
	"net"
	"strings"
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

// Empty SourceType remains compatible with callers constructing directory
// library values themselves; path must still exist as a capability boundary.
func (l Library) IsDirectory() bool {
	return l.Path != "" && (l.SourceType == "" || l.SourceType == SourceDirectory)
}

const libraryColumns = `id,name,path,type,enabled,source_type,revision,COALESCE(proxy_enabled,0),COALESCE(source_ip,''),COALESCE(target_ip,'')`

func readLibrary(row interface{ Scan(...any) error }) (Library, error) {
	var l Library
	err := row.Scan(&l.ID, &l.Name, &l.Path, &l.Type, &l.Enabled, &l.SourceType, &l.Revision, &l.ProxyEnabled, &l.SourceIP, &l.TargetIP)
	return l, err
}

func (s *Store) UpdateLibraryProxy(id int64, enabled bool, revision int64) (Library, error) {
	library, err := s.Library(id)
	if err != nil {
		return Library{}, err
	}
	return s.UpdateLibrarySettings(id, enabled, library.SourceIP, library.TargetIP, revision)
}

func NormalizeIPRewrite(sourceIP, targetIP string) (string, string, error) {
	sourceIP, targetIP = strings.TrimSpace(sourceIP), strings.TrimSpace(targetIP)
	if sourceIP == "" && targetIP == "" {
		return "", "", nil
	}
	if sourceIP == "" || targetIP == "" {
		return "", "", errors.New("原始 IP 和替换 IP 必须同时填写")
	}
	source := net.ParseIP(sourceIP)
	target := net.ParseIP(targetIP)
	if source == nil || target == nil {
		return "", "", errors.New("原始 IP 和替换 IP 必须是有效的 IPv4 或 IPv6 地址")
	}
	return source.String(), target.String(), nil
}

func (s *Store) UpdateLibrarySettings(id int64, proxyEnabled bool, sourceIP, targetIP string, revision int64) (Library, error) {
	var err error
	sourceIP, targetIP, err = NormalizeIPRewrite(sourceIP, targetIP)
	if err != nil {
		return Library{}, err
	}
	result, err := s.db.Exec(`UPDATE libraries SET proxy_enabled=?,source_ip=?,target_ip=?,revision=revision+1 WHERE id=? AND enabled=1 AND source_type=? AND revision=?`, proxyEnabled, sourceIP, targetIP, id, SourceDirectory, revision)
	if err != nil {
		return Library{}, err
	}
	if err := affectedOne(result, nil); err != nil {
		return Library{}, err
	}
	if err := s.BumpVersion(id); err != nil {
		return Library{}, err
	}
	return s.Library(id)
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
