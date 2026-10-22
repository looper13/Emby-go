-- Emby-go 在线动漫功能退役：只对主 SQLite 数据库执行。
-- 执行前停止旧服务并备份；客户端必须遇错停止，sqlite3 使用 -bail。
-- 用法：sqlite3 -bail emby-go.db ".read scripts/remove_anime.sql"
-- 适用于已有动漫功能的数据库，也可对新版数据库重复执行。
-- 不对 anime-catalog/ 中的目录库或 anime-images/images.db 执行。

PRAGMA busy_timeout = 10000;
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;

-- 任一检查失败都回滚。运行客户端必须停止后续语句，不能忽略错误。
CREATE TEMP TABLE removal_checks (ok INTEGER NOT NULL);
CREATE TEMP TRIGGER removal_checks_abort
BEFORE INSERT ON removal_checks
WHEN NEW.ok <> 1
BEGIN
    SELECT RAISE(ROLLBACK, '动漫清理检查失败，已回滚；请核对数据库结构、原始媒体和外键');
END;

INSERT INTO removal_checks
SELECT count(*) = 8
FROM pragma_table_info('libraries')
WHERE name IN ('id', 'name', 'path', 'type', 'enabled', 'source_type', 'revision', 'proxy_enabled');

-- 原始媒体始终属于普通目录库；异常归属时拒绝清理。
INSERT INTO removal_checks
SELECT NOT EXISTS (
    SELECT 1 FROM movies
    WHERE library_id IN (SELECT id FROM libraries WHERE source_type = 'anime_catalog')
);

CREATE TEMP TABLE removal_library_ids AS
SELECT id FROM libraries WHERE source_type = 'anime_catalog';

CREATE TEMP TABLE removal_original_counts AS
SELECT
    (SELECT count(*) FROM movies) AS movies,
    (SELECT count(*) FROM userdata) AS userdata;

-- 保留自增序号，防止新媒体库复用退役库的身份。
CREATE TEMP TABLE removal_library_sequence AS
SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name = 'libraries'), 0) AS seq;

DROP TABLE IF EXISTS anime_tmdb_episodes;
DROP TABLE IF EXISTS anime_tmdb_series;
DROP TABLE IF EXISTS anime_tmdb_config;
DROP TABLE IF EXISTS anime_subject_sources;
DROP TABLE IF EXISTS anime_release_episodes;
DROP TABLE IF EXISTS anime_releases;
DROP TABLE IF EXISTS anime_catalog_entries;
DROP TABLE IF EXISTS anime_mikan_entries;
DROP TABLE IF EXISTS anime_media_bindings;
DROP TABLE IF EXISTS anime_episodes;
DROP TABLE IF EXISTS anime_subjects;
DROP TABLE IF EXISTS anime_sync_state;

DELETE FROM settings WHERE key GLOB 'anime.*';
DELETE FROM scheduled_tasks WHERE type IN ('anime_archive', 'anime_sync');
DELETE FROM item_favorites WHERE item_id GLOB 'anime-*';
DELETE FROM account_libraries WHERE library_id IN (SELECT id FROM removal_library_ids);
DELETE FROM user_library_grants WHERE library_id IN (SELECT id FROM removal_library_ids);
DELETE FROM kv WHERE key IN (SELECT 'lib:' || id || ':version' FROM removal_library_ids);

-- 先创建新表再删除旧表，避免重命名旧表时改写子表外键。
DROP VIEW IF EXISTS user_movies;
DROP VIEW IF EXISTS user_libraries;
CREATE TABLE libraries_directory_v5 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    path TEXT NOT NULL UNIQUE CHECK(length(trim(path)) > 0),
    type TEXT NOT NULL DEFAULT 'movies',
    enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
    source_type TEXT NOT NULL DEFAULT 'nfo_directory' CHECK(source_type = 'nfo_directory'),
    revision INTEGER NOT NULL DEFAULT 1,
    proxy_enabled INTEGER NOT NULL DEFAULT 0 CHECK(proxy_enabled IN (0, 1))
);
INSERT INTO libraries_directory_v5 (id, name, path, type, enabled, source_type, revision, proxy_enabled)
SELECT id, name, path, COALESCE(type, 'movies'), COALESCE(enabled, 1), source_type, revision, proxy_enabled
FROM libraries
WHERE id NOT IN (SELECT id FROM removal_library_ids);
DROP TABLE libraries;
ALTER TABLE libraries_directory_v5 RENAME TO libraries;

UPDATE sqlite_sequence
SET seq = MAX(seq, (SELECT seq FROM removal_library_sequence))
WHERE name = 'libraries';
INSERT INTO sqlite_sequence (name, seq)
SELECT 'libraries', seq FROM removal_library_sequence
WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'libraries');

CREATE VIEW user_libraries AS
SELECT * FROM libraries WHERE enabled = 1 AND (
    (SELECT enable_all_folders FROM account_policy WHERE id = 1) = 1
    OR id IN (SELECT library_id FROM account_libraries)
);
CREATE VIEW user_movies AS
SELECT * FROM movies WHERE library_id IN (SELECT id FROM user_libraries);

-- 提交前验证未丢失原始媒体和观看记录，且不存在残余表或坏外键。
INSERT INTO removal_checks
SELECT (SELECT count(*) FROM movies) = movies
    AND (SELECT count(*) FROM userdata) = userdata
FROM removal_original_counts;
INSERT INTO removal_checks
SELECT NOT EXISTS (SELECT 1 FROM sqlite_master WHERE name GLOB 'anime_*');
INSERT INTO removal_checks
SELECT NOT EXISTS (SELECT 1 FROM pragma_foreign_key_check);
INSERT INTO removal_checks
SELECT NOT EXISTS (SELECT 1 FROM pragma_integrity_check WHERE integrity_check <> 'ok');

CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY);
INSERT OR IGNORE INTO schema_migrations (version) VALUES (5);

DROP TRIGGER removal_checks_abort;
DROP TABLE removal_checks;
DROP TABLE removal_library_ids;
DROP TABLE removal_original_counts;
DROP TABLE removal_library_sequence;
COMMIT;
PRAGMA foreign_keys = ON;

SELECT '动漫专用表、设置、任务、虚拟收藏及内置库已清理，原始媒体和观看记录已保留' AS result;
PRAGMA foreign_key_check;
PRAGMA integrity_check;
