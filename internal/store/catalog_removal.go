package store

import (
	"context"
	"fmt"
)

// migrateDirectoryLibraries 升级普通媒体库字段，旧动漫结构由用户手动清理。
func (s *Store) migrateDirectoryLibraries() error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 先占用写锁，避免已有后台写入使读取后的 WAL 快照无法升级为写事务。
	if _, err = tx.ExecContext(ctx, `UPDATE schema_migrations SET version=version WHERE version=5`); err != nil {
		return err
	}
	columns := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(libraries)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return readErr
	}
	var hasCatalog bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE name GLOB 'anime_*')`).Scan(&hasCatalog); err != nil {
		return err
	}
	if columns["system_key"] || hasCatalog {
		return fmt.Errorf("数据库仍有在线动漫结构；请停止服务并备份后，手动执行 scripts/remove_anime.sql，再启动新版")
	}
	// 动漫功能加入前的数据库也需要普通媒体库的修订号和代理开关。
	for _, column := range []struct{ name, definition string }{
		{"source_type", "TEXT NOT NULL DEFAULT 'nfo_directory'"},
		{"revision", "INTEGER NOT NULL DEFAULT 1"},
		{"proxy_enabled", "INTEGER NOT NULL DEFAULT 0 CHECK(proxy_enabled IN (0,1))"},
		{"resolve_redirects", "INTEGER NOT NULL DEFAULT 0 CHECK(resolve_redirects IN (0,1))"},
	} {
		if !columns[column.name] {
			if _, err = tx.ExecContext(ctx, "ALTER TABLE libraries ADD COLUMN "+column.name+" "+column.definition); err != nil {
				return err
			}
		}
	}
	for _, column := range []string{"source_ip", "target_ip"} {
		if columns[column] {
			if _, err = tx.ExecContext(ctx, "ALTER TABLE libraries DROP COLUMN "+column); err != nil {
				return fmt.Errorf("remove retired library column %s: %w", column, err)
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version) VALUES(5)`); err != nil {
		return err
	}
	return tx.Commit()
}
