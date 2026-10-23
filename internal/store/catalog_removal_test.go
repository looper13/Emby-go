package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirectoryDatabaseOmitsOnlineCatalog(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.waitFeaturesReady(); s.Close() }()
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name GLOB 'anime_*'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("新库不应创建在线动漫表：count=%d err=%v", count, err)
	}
	if _, err = s.db.Exec(`INSERT INTO libraries(name,path) VALUES('无目录',NULL)`); err == nil {
		t.Fatal("普通媒体库必须有目录")
	}
}

func TestDirectoryMigrationAddsPlaybackColumns(t *testing.T) {
	path, _ := legacyCatalogFixture(t, false)
	if err := runCatalogRemovalSQL(path); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lib, err := s.Library(1)
	if err != nil || lib.ResolveRedirects {
		t.Fatalf("migrated library defaults: %+v err=%v", lib, err)
	}
}

// 夹具模拟已经启用在线库的数据库，同时保存正常媒体、播放状态及授权。
func legacyCatalogFixture(t *testing.T, unexpectedMedia bool) (string, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := s.AddLibrary("电视", "/tv", LibraryTVShows)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.UpsertMovie(Movie{LibraryID: lib.ID, SourcePath: "/tv/pilot.strm", Title: "保留媒体", Status: "success", Television: &Television{SeriesPath: "/tv/Show", Title: "保留剧集", Season: 2, Episode: 1}}, 42, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.waitFeaturesReady()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`PRAGMA foreign_keys=OFF;
DROP VIEW user_movies;
DROP VIEW user_libraries;
CREATE TABLE libraries_legacy(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,path TEXT UNIQUE,type TEXT NOT NULL DEFAULT 'movies',enabled INTEGER NOT NULL DEFAULT 1,source_type TEXT NOT NULL DEFAULT 'nfo_directory',system_key TEXT UNIQUE,revision INTEGER NOT NULL DEFAULT 1,proxy_enabled INTEGER NOT NULL DEFAULT 0);
INSERT INTO libraries_legacy(id,name,path,type,enabled,source_type,revision,proxy_enabled) SELECT id,name,path,type,enabled,source_type,revision,proxy_enabled FROM libraries;
DROP TABLE libraries;
ALTER TABLE libraries_legacy RENAME TO libraries;
INSERT INTO libraries(id,name,path,type,source_type,system_key) VALUES(99,'动漫',NULL,'','anime_catalog','builtin:anime');
CREATE VIEW user_libraries AS SELECT * FROM libraries WHERE enabled=1;
CREATE VIEW user_movies AS SELECT * FROM movies WHERE library_id IN (SELECT id FROM user_libraries);
DELETE FROM schema_migrations WHERE version=5;
CREATE TABLE anime_subjects(id INTEGER PRIMARY KEY);
INSERT INTO anime_subjects VALUES(1);
CREATE TABLE anime_episodes(id INTEGER PRIMARY KEY,subject_id INTEGER REFERENCES anime_subjects(id));
INSERT INTO anime_episodes VALUES(2,1);
CREATE TABLE anime_mikan_entries(id INTEGER PRIMARY KEY,subject_id INTEGER REFERENCES anime_subjects(id));
CREATE TABLE anime_catalog_entries(mikan_id INTEGER REFERENCES anime_mikan_entries(id));
CREATE TABLE anime_releases(id TEXT PRIMARY KEY,mikan_id INTEGER REFERENCES anime_mikan_entries(id));
CREATE TABLE anime_release_episodes(release_id TEXT REFERENCES anime_releases(id),episode_id INTEGER REFERENCES anime_episodes(id));
CREATE TABLE anime_media_bindings(subject_id INTEGER REFERENCES anime_subjects(id),episode_id INTEGER REFERENCES anime_episodes(id),movie_id INTEGER REFERENCES movies(id));
CREATE TABLE anime_subject_sources(subject_id INTEGER REFERENCES anime_subjects(id));
CREATE TABLE anime_tmdb_config(api_key TEXT);
INSERT INTO anime_tmdb_config VALUES('fixture-key');
CREATE TABLE anime_tmdb_series(subject_id INTEGER REFERENCES anime_subjects(id));
CREATE TABLE anime_tmdb_episodes(episode_id INTEGER REFERENCES anime_episodes(id));
CREATE TABLE anime_sync_state(scope TEXT);
INSERT INTO settings VALUES('anime.config','{}'),('anime.archive_schedule_initialized','1'),('other.config','保留');
INSERT INTO scheduled_tasks(name,type,cron,created_at,updated_at) VALUES('归档','anime_archive','0 * * * *','',''),('蜜柑','anime_sync','0 * * * *','',''),('扫描','scan','0 * * * *','','');
INSERT INTO item_favorites(user_id,item_id) VALUES(1,'anime-series-1'),(1,'tv-series:fixture');
INSERT INTO account_libraries VALUES(99);
INSERT INTO user_library_grants VALUES(1,99);
INSERT INTO kv VALUES('lib:99:version','8');
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_libraries VALUES(?);
INSERT INTO user_library_grants VALUES(1,?);
INSERT INTO userdata(user_id,movie_id,position_ticks,play_count,is_favorite) VALUES(1,?,123456,2,1);
INSERT INTO anime_media_bindings VALUES(1,2,?);`, lib.ID, lib.ID, id, id); err != nil {
		t.Fatal(err)
	}
	if unexpectedMedia {
		if _, err = db.Exec(`UPDATE movies SET library_id=99 WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
	}
	return path, id
}

// 按 SQLite 客户端遇错停止的语义逐条执行，确保 RAISE(ROLLBACK) 后不继续。
func runCatalogRemovalSQL(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "remove_anime.sql"))
	if err != nil {
		return err
	}
	var statement strings.Builder
	inTrigger := false
	for _, line := range strings.Split(string(script), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") || trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "CREATE TEMP TRIGGER") {
			inTrigger = true
		}
		statement.WriteString(line)
		statement.WriteByte('\n')
		if strings.HasSuffix(trimmed, ";") && (!inTrigger || trimmed == "END;") {
			if _, err = db.Exec(statement.String()); err != nil {
				return err
			}
			statement.Reset()
			inTrigger = false
		}
	}
	return nil
}

func TestRemoveOnlineCatalogPreservesMediaAndUsers(t *testing.T) {
	path, id := legacyCatalogFixture(t, false)
	if err := runCatalogRemovalSQL(path); err != nil {
		t.Fatalf("手动清理脚本失败：%v", err)
	}
	if err := runCatalogRemovalSQL(path); err != nil {
		t.Fatalf("手动清理脚本重复执行失败：%v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.waitFeaturesReady(); s.Close() }()
	movie, err := s.Movie(id)
	if err != nil || movie.Title != "保留媒体" || movie.Television == nil || movie.Television.Season != 2 {
		t.Fatalf("原始媒体发生变化：%+v err=%v", movie, err)
	}
	var position int64
	var plays, favorite int
	if err = s.db.QueryRow(`SELECT position_ticks,play_count,is_favorite FROM userdata WHERE movie_id=?`, id).Scan(&position, &plays, &favorite); err != nil || position != 123456 || plays != 2 || favorite != 1 {
		t.Fatalf("观看状态发生变化：%d %d %d err=%v", position, plays, favorite, err)
	}
	for query, want := range map[string]int{
		`SELECT count(*) FROM sqlite_master WHERE name GLOB 'anime_*'`:                0,
		`SELECT count(*) FROM settings WHERE key GLOB 'anime.*'`:                      0,
		`SELECT count(*) FROM settings WHERE key='other.config'`:                      1,
		`SELECT count(*) FROM scheduled_tasks`:                                        1,
		`SELECT count(*) FROM item_favorites WHERE item_id GLOB 'anime-*'`:            0,
		`SELECT count(*) FROM item_favorites`:                                         1,
		`SELECT count(*) FROM libraries`:                                              1,
		`SELECT count(*) FROM account_libraries`:                                      1,
		`SELECT count(*) FROM user_library_grants`:                                    1,
		`SELECT count(*) FROM kv WHERE key='lib:99:version'`:                          0,
		`SELECT count(*) FROM pragma_table_info('libraries') WHERE name='system_key'`: 0,
		`SELECT count(*) FROM pragma_foreign_key_check`:                               0,
	} {
		var count int
		if err = s.db.QueryRow(query).Scan(&count); err != nil || count != want {
			t.Fatalf("清理结果不符合预期：%s count=%d want=%d err=%v", query, count, want, err)
		}
	}
	lib, err := s.AddLibrary("新库", "/new")
	if err != nil || lib.ID <= 99 {
		t.Fatalf("媒体库序号不应复用被删除的身份：%+v err=%v", lib, err)
	}
	s.waitFeaturesReady()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatalf("再次打开数据库失败：%v", err)
	}
	s.waitFeaturesReady()
}

func TestRemoveOnlineCatalogRollsBackUnexpectedMedia(t *testing.T) {
	path, _ := legacyCatalogFixture(t, true)
	if err := runCatalogRemovalSQL(path); err == nil {
		t.Fatal("内置库存在原始媒体时应拒绝清理脚本")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range []string{
		`SELECT count(*) FROM anime_subjects`,
		`SELECT count(*) FROM anime_tmdb_config`,
		`SELECT count(*) FROM libraries WHERE id=99`,
		`SELECT count(*) FROM movies WHERE library_id=99`,
		`SELECT count(*) FROM settings WHERE key='anime.config'`,
	} {
		var count int
		if err = db.QueryRow(query).Scan(&count); err != nil || count != 1 {
			t.Fatalf("失败后未完整回滚：%s count=%d err=%v", query, count, err)
		}
	}
}

func TestDirectoryStartupRequiresManualCatalogCleanup(t *testing.T) {
	path, _ := legacyCatalogFixture(t, false)
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("旧库启动应提示手动执行 SQL")
	} else if !strings.Contains(err.Error(), "scripts/remove_anime.sql") {
		t.Fatalf("启动错误未说明清理脚本：%v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM anime_tmdb_config`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("程序不得自动清理旧库：count=%d err=%v", count, err)
	}
}

func TestDirectoryMigrationSupportsDatabaseBeforeOnlineCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "original.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE libraries(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,path TEXT UNIQUE NOT NULL,type TEXT DEFAULT 'movies',enabled INTEGER DEFAULT 1);
INSERT INTO libraries VALUES(7,'原有库','/original','movies',1);`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.waitFeaturesReady(); s.Close() }()
	lib, err := s.Library(7)
	if err != nil || lib.Path != "/original" || lib.SourceType != SourceDirectory || lib.Revision != 1 {
		t.Fatalf("早期库迁移后不可用：%+v err=%v", lib, err)
	}
	if _, err = s.UpdateLibraryProxy(7, true, 1); err != nil {
		t.Fatalf("早期库迁移后无法设置代理：%v", err)
	}
}
