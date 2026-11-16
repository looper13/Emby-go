package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// DirectoryPaths 返回目录库的根目录，保留只填写 Path 的历史调用方式。
func (l Library) DirectoryPaths() []string {
	if l.SourceType != "" && l.SourceType != SourceDirectory {
		return []string{}
	}
	paths := l.Paths
	if len(paths) == 0 || len(paths) == 1 && l.Path != "" && l.Path != paths[0] {
		paths = []string{l.Path}
	}
	result := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" && !seen[directoryPathKey(path)] {
			seen[directoryPathKey(path)] = true
			result = append(result, path)
		}
	}
	return result
}

// RootForPath 选择包含目标的最长根目录，避免嵌套配置使用错误的边界。
// 文件读取及删除仍须另外检查符号链接后的真实路径。
func (l Library) RootForPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	longest, length := "", -1
	for _, root := range l.DirectoryPaths() {
		key := directoryPathKey(root)
		if directoryContains(root, path) && len(key) > length {
			longest, length = root, len(key)
		}
	}
	return longest
}

// TraversalPaths 合并已被父目录覆盖的嵌套根，扫描与监听只遍历一次。
func (l Library) TraversalPaths() []string {
	paths := l.DirectoryPaths()
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		nested := false
		for _, parent := range paths {
			if directoryPathKey(parent) != directoryPathKey(path) && directoryContains(parent, path) {
				nested = true
				break
			}
		}
		if !nested {
			result = append(result, path)
		}
	}
	return result
}

func directoryPathKey(path string) string {
	path = filepath.Clean(path)
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func directoryContains(root, target string) bool {
	relative, err := filepath.Rel(directoryPathKey(root), directoryPathKey(target))
	return err == nil && (relative == "." || filepath.IsLocal(relative))
}

func overlappingDirectories(left, right string) bool {
	if directoryContains(left, right) || directoryContains(right, left) {
		return true
	}
	resolvedLeft, leftErr := filepath.EvalSymlinks(left)
	resolvedRight, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && (directoryContains(resolvedLeft, resolvedRight) || directoryContains(resolvedRight, resolvedLeft))
}

func normalizeLibraryPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("媒体库至少需要一个目录路径")
	}
	result := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || strings.ContainsRune(path, '\x00') {
			return nil, errors.New("媒体库目录路径不能为空或包含空字符")
		}
		key := directoryPathKey(path)
		if !seen[key] {
			seen[key] = true
			result = append(result, path)
		}
	}
	return result, nil
}

func sameDirectoryPaths(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if directoryPathKey(left[index]) != directoryPathKey(right[index]) {
			return false
		}
	}
	return true
}

func validateDirectoryLibrary(name, contentType string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("媒体库名称不能为空")
	}
	if contentType != LibraryMovies && contentType != LibraryTVShows && contentType != LibraryMixed {
		return errors.New("媒体库类型必须是影片、电视节目或混合内容")
	}
	return nil
}

// migrateLibraryPaths 在来源表升级后补充 JSON 字段，旧目录自动转换为单元素数组。
func (s *Store) migrateLibraryPaths() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE schema_migrations SET version=version WHERE version=6"); err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{"paths", "TEXT NOT NULL DEFAULT '[]'"},
		{"proxy_headers", "TEXT NOT NULL DEFAULT '{}'"},
	} {
		var exists bool
		if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM pragma_table_info('libraries') WHERE name=?)", column.name).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if _, err = tx.Exec("ALTER TABLE libraries ADD COLUMN " + column.name + " " + column.definition); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(`UPDATE libraries SET paths=json_array(path) WHERE source_type=? AND json_array_length(paths)=0`, SourceDirectory); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO schema_migrations(version) VALUES(6)"); err != nil {
		return err
	}
	return tx.Commit()
}

func checkDirectoryOverlap(tx *sql.Tx, id int64, paths []string) error {
	rows, err := tx.Query("SELECT "+libraryColumns+" FROM libraries WHERE source_type='nfo_directory' AND id<>?", id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		library, err := readLibrary(rows)
		if err != nil {
			return err
		}
		for _, existing := range library.DirectoryPaths() {
			for _, path := range paths {
				if overlappingDirectories(existing, path) {
					return fmt.Errorf("目录 %s 与媒体库「%s」的目录 %s 重叠，请使用独立目录", path, library.Name, existing)
				}
			}
		}
	}
	return rows.Err()
}

func bumpDirectoryVersion(tx *sql.Tx, id int64) error {
	if _, err := tx.Exec(`INSERT INTO kv(key,value) VALUES('g:version','1') ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+1`); err != nil {
		return err
	}
	key := "lib:" + strconv.FormatInt(id, 10) + ":version"
	_, err := tx.Exec(`INSERT INTO kv(key,value) VALUES(?, '1') ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+1`, key)
	return err
}

// 调用方持有 versionMu，内存版本只在配置事务提交后推进。
func (s *Store) directoryVersionCommitted(id int64) {
	s.gversion.Add(1)
	s.libraryVersions["lib:"+strconv.FormatInt(id, 10)+":version"]++
	s.searchCounts.revision.Add(1)
	s.InvalidateLibraryMovies(id)
}

func (s *Store) AddDirectoryLibrary(name string, paths []string, contentType string, proxyEnabled, resolveRedirects bool, headers map[string]string) (Library, error) {
	name = strings.TrimSpace(name)
	if contentType == "" {
		contentType = LibraryMovies
	}
	if err := validateDirectoryLibrary(name, contentType); err != nil {
		return Library{}, err
	}
	paths, err := normalizeLibraryPaths(paths)
	if err != nil {
		return Library{}, err
	}
	headers, err = NormalizeProxyHeaders(headers)
	if err != nil {
		return Library{}, err
	}
	s.versionMu.Lock()
	defer s.versionMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Library{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE schema_migrations SET version=version WHERE version=6"); err != nil {
		return Library{}, err
	}
	if err = checkDirectoryOverlap(tx, 0, paths); err != nil {
		return Library{}, err
	}
	var id int64
	if err = tx.QueryRow(`INSERT INTO libraries(name,path,paths,type,proxy_enabled,resolve_redirects,proxy_headers) VALUES(?,?,?,?,?,?,?) RETURNING id`, name, paths[0], jsonText(paths), contentType, proxyEnabled, resolveRedirects, jsonText(headers)).Scan(&id); err != nil {
		return Library{}, err
	}
	if err = bumpDirectoryVersion(tx, id); err != nil {
		return Library{}, err
	}
	library, err := readLibrary(tx.QueryRow("SELECT "+libraryColumns+" FROM libraries WHERE id=?", id))
	if err != nil {
		return Library{}, err
	}
	if err = tx.Commit(); err != nil {
		return Library{}, err
	}
	s.directoryVersionCommitted(id)
	return library, nil
}

func (s *Store) UpdateDirectoryLibrary(id int64, name string, paths []string, proxyEnabled, resolveRedirects bool, headers map[string]string, revision int64) (Library, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Library{}, errors.New("媒体库名称不能为空")
	}
	paths, err := normalizeLibraryPaths(paths)
	if err != nil {
		return Library{}, err
	}
	headers, err = NormalizeProxyHeaders(headers)
	if err != nil {
		return Library{}, err
	}
	s.versionMu.Lock()
	defer s.versionMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Library{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE libraries SET revision=revision WHERE id=?", id); err != nil {
		return Library{}, err
	}
	library, err := readLibrary(tx.QueryRow("SELECT "+libraryColumns+" FROM libraries WHERE id=? AND enabled=1", id))
	if err != nil {
		return Library{}, err
	}
	if !library.IsDirectory() {
		return Library{}, ErrLibraryCapability
	}
	if library.Revision != revision {
		return Library{}, ErrConflict
	}
	if err = checkDirectoryOverlap(tx, id, paths); err != nil {
		return Library{}, err
	}
	result, err := tx.Exec(`UPDATE libraries SET name=?,path=?,paths=?,proxy_enabled=?,resolve_redirects=?,proxy_headers=?,revision=revision+1 WHERE id=? AND enabled=1 AND source_type=? AND revision=?`, name, paths[0], jsonText(paths), proxyEnabled, resolveRedirects, jsonText(headers), id, SourceDirectory, revision)
	if err = affectedOne(result, err); err != nil {
		return Library{}, err
	}
	pathsChanged := !sameDirectoryPaths(library.DirectoryPaths(), paths)
	library.Path, library.Paths = paths[0], paths
	// 仅移除不再被配置根覆盖的索引，外键同步清理播放记录，磁盘文件保持原样。
	var removed []int64
	type removedFavorite struct {
		userID int64
		itemID string
	}
	var removedFavorites []removedFavorite
	if pathsChanged {
		rows, err := tx.Query("SELECT id,source_path FROM movies WHERE library_id=?", id)
		if err != nil {
			return Library{}, err
		}
		for rows.Next() {
			var movieID int64
			var source string
			if err = rows.Scan(&movieID, &source); err != nil {
				rows.Close()
				return Library{}, err
			}
			if library.RootForPath(source) == "" {
				removed = append(removed, movieID)
			}
		}
		readErr := rows.Err()
		rows.Close()
		if readErr != nil {
			return Library{}, readErr
		}
		favorites, err := tx.Query("SELECT user_id,item_id FROM item_favorites WHERE item_id LIKE 'tv-%'")
		if err != nil {
			return Library{}, err
		}
		for favorites.Next() {
			var favorite removedFavorite
			if err = favorites.Scan(&favorite.userID, &favorite.itemID); err != nil {
				favorites.Close()
				return Library{}, err
			}
			_, favoriteLibrary, seriesPath, _, ok := ParseTVID(favorite.itemID)
			if ok && favoriteLibrary == id && library.RootForPath(seriesPath) == "" {
				removedFavorites = append(removedFavorites, favorite)
			}
		}
		favoritesErr := favorites.Err()
		favorites.Close()
		if favoritesErr != nil {
			return Library{}, favoritesErr
		}
	}
	for _, movieID := range removed {
		if _, err = tx.Exec("DELETE FROM movies WHERE id=? AND library_id=?", movieID, id); err != nil {
			return Library{}, err
		}
	}
	for _, favorite := range removedFavorites {
		if _, err = tx.Exec("DELETE FROM item_favorites WHERE user_id=? AND item_id=?", favorite.userID, favorite.itemID); err != nil {
			return Library{}, err
		}
	}
	if err = bumpDirectoryVersion(tx, id); err != nil {
		return Library{}, err
	}
	library, err = readLibrary(tx.QueryRow("SELECT "+libraryColumns+" FROM libraries WHERE id=?", id))
	if err != nil {
		return Library{}, err
	}
	if err = tx.Commit(); err != nil {
		return Library{}, err
	}
	s.directoryVersionCommitted(id)
	for _, movieID := range removed {
		s.TouchMovie(movieID)
	}
	if len(removedFavorites) > 0 {
		s.itemFavoritesMu.Lock()
		for _, favorite := range removedFavorites {
			delete(s.itemFavorites, strconv.FormatInt(favorite.userID, 10)+":"+favorite.itemID)
		}
		s.itemFavoriteVersion.Add(1)
		s.itemFavoritesMu.Unlock()
	}
	return library, nil
}

// registerLibrary 适配启动配置和旧单目录 API，重注册不会覆盖已有其他根和请求头。
func (s *Store) registerLibrary(name, path string, proxy, redirects *bool, contentType ...string) (Library, error) {
	path = strings.TrimSpace(path)
	if strings.TrimSpace(name) == "" || path == "" {
		return Library{}, errors.New("媒体库名称和路径不能为空")
	}
	libraries, err := s.AllLibraries()
	if err != nil {
		return Library{}, err
	}
	for _, library := range libraries {
		for _, root := range library.DirectoryPaths() {
			if directoryPathKey(root) != directoryPathKey(path) {
				continue
			}
			if len(contentType) > 0 && contentType[0] != library.Type {
				return Library{}, errors.New("该路径已属于其他类型的媒体库，请使用独立目录")
			}
			proxyEnabled, resolveRedirects := library.ProxyEnabled, library.ResolveRedirects
			if proxy != nil {
				proxyEnabled = *proxy
			}
			if redirects != nil {
				resolveRedirects = *redirects
			}
			return s.UpdateDirectoryLibrary(library.ID, name, library.DirectoryPaths(), proxyEnabled, resolveRedirects, library.ProxyHeaders, library.Revision)
		}
	}
	typ := LibraryMovies
	if len(contentType) > 0 {
		typ = contentType[0]
	}
	proxyEnabled, resolveRedirects := false, false
	if proxy != nil {
		proxyEnabled = *proxy
	}
	if redirects != nil {
		resolveRedirects = *redirects
	}
	return s.AddDirectoryLibrary(name, []string{path}, typ, proxyEnabled, resolveRedirects, nil)
}
