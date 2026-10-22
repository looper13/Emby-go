// Package backup 提供 SQLite 一致性快照与媒体库 NFO 副本，
// 保留最近 N 份并可按时间倒序清理。备份只读源库，不修改业务数据。
package backup

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Options 控制一次备份。
type Options struct {
	DBPath  string    // 源数据库
	OutDir  string    // 备份根目录
	Retain  int       // 保留份数（<=0 表示不清理）
	Now     time.Time // 备份时间（零值用 time.Now）
	NFORoot bool      // 是否复制媒体库目录中的 .nfo
}

// Result 描述本次备份产物，便于日志与测试断言。
type Result struct {
	Dir        string
	Database   string
	NFOCount   int
	Pruned     []string
	FinishedAt time.Time
}

// Run publishes a complete backup under its UTC timestamp. Existing backups are never replaced.
func Run(opts Options) (Result, error) {
	if strings.TrimSpace(opts.DBPath) == "" || strings.TrimSpace(opts.OutDir) == "" {
		return Result{}, fmt.Errorf("backup: db and out dir required")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return Result{}, err
	}
	outDir, err = filepath.EvalSymlinks(outDir)
	if err != nil {
		return Result{}, err
	}
	opts.OutDir = outDir
	dir := filepath.Join(outDir, now.UTC().Format("20060102T150405Z"))
	if _, err := os.Lstat(dir); err == nil {
		return Result{}, fmt.Errorf("backup: destination already exists: %s", dir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	stage, err := os.MkdirTemp(outDir, ".emby-backup-*.tmp")
	if err != nil {
		return Result{}, err
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(stage)
		}
	}()
	// 只读打开源库：备份不应有机会改写业务库；busy_timeout 给在线写入让路。
	db, err := sql.Open("sqlite", readonlyDSN(opts.DBPath))
	if err != nil {
		return Result{}, fmt.Errorf("backup: open db: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	dest := filepath.Join(stage, "db.sqlite")
	if _, err := db.Exec("VACUUM INTO ?", dest); err != nil {
		return Result{}, fmt.Errorf("backup: snapshot: %w", err)
	}
	if err := os.Chmod(dest, 0600); err != nil {
		return Result{}, err
	}
	if err := checkSnapshot(dest); err != nil {
		return Result{}, err
	}
	result := Result{Dir: dir, Database: filepath.Join(dir, "db.sqlite"), FinishedAt: now.UTC()}
	if opts.NFORoot {
		snapshot, err := sql.Open("sqlite", readonlyDSN(dest))
		if err != nil {
			return Result{}, err
		}
		count, err := copyNFO(snapshot, filepath.Join(stage, "nfo"), outDir)
		closeErr := snapshot.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return Result{}, err
		}
		result.NFOCount = count
	}
	manifest, err := json.Marshal(backupManifest{Format: backupFormat, Created: now.UTC(), NFOCount: result.NFOCount})
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), manifest, 0600); err != nil {
		return Result{}, err
	}
	if err := os.Rename(stage, dir); err != nil {
		return Result{}, fmt.Errorf("backup: publish: %w", err)
	}
	committed = true
	// 先发布再清理旧份，保留策略才会把本次备份计入份数。
	if opts.Retain > 0 {
		pruned, err := prune(opts.OutDir, opts.Retain)
		if err != nil {
			return Result{}, err
		}
		result.Pruned = pruned
	}
	return result, nil
}

// readonlyDSN 只开放读事务，并用 busy_timeout 等待在线写入者。
func readonlyDSN(path string) string {
	absolute, _ := filepath.Abs(path)
	absolute = filepath.ToSlash(absolute)
	if !strings.HasPrefix(absolute, "/") {
		absolute = "/" + absolute
	}
	return (&url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}).String()
}

// checkSnapshot 校验快照完整性；半成品或损坏的备份不允许发布。
func checkSnapshot(path string) error {
	db, err := sql.Open("sqlite", readonlyDSN(path))
	if err != nil {
		return fmt.Errorf("backup: verify snapshot: %w", err)
	}
	defer db.Close()
	rows, err := db.Query("PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("backup: verify snapshot: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var verdict string
		if err := rows.Scan(&verdict); err != nil {
			return fmt.Errorf("backup: verify snapshot: %w", err)
		}
		if verdict != "ok" {
			return fmt.Errorf("backup: integrity check failed: %s", verdict)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("backup: verify snapshot: %w", err)
	}
	return nil
}

// copyNFO 按库复制 .nfo 文件，保持库内相对路径；任何读写错误都让备份失败，
// 避免出现"数据库有、NFO 没有"的静默半份备份。
func copyNFO(db *sql.DB, root string, exclude string) (int, error) {
	rows, err := db.Query("SELECT id,path FROM libraries ORDER BY id")
	if err != nil {
		return 0, fmt.Errorf("backup: libraries: %w", err)
	}
	defer rows.Close()
	type library struct {
		id   int64
		path string
	}
	var libs []library
	for rows.Next() {
		var l library
		if err := rows.Scan(&l.id, &l.path); err != nil {
			return 0, fmt.Errorf("backup: libraries: %w", err)
		}
		libs = append(libs, l)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("backup: libraries: %w", err)
	}
	count := 0
	for _, lib := range libs {
		canonical, err := filepath.EvalSymlinks(lib.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("backup: library path: %w", err)
		}
		lib.path, err = filepath.Abs(canonical)
		if err != nil {
			return 0, err
		}
		info, err := os.Stat(lib.path)
		if errors.Is(err, os.ErrNotExist) || (err == nil && !info.IsDir()) {
			continue // 离线/移除的库不阻断备份，恢复时按 NFO 目录人工对账
		}
		if err != nil {
			return 0, fmt.Errorf("backup: library: %w", err)
		}
		excluded, err := os.Stat(exclude)
		if err != nil {
			return 0, err
		}
		err = filepath.Walk(lib.path, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() && os.SameFile(info, excluded) {
				return filepath.SkipDir
			}
			if !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".nfo") {
				return nil
			}
			rel, err := filepath.Rel(lib.path, path)
			if err != nil {
				return err
			}
			dst := filepath.Join(root, fmt.Sprintf("lib-%d", lib.id), rel)
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return err
			}
			if err := copyFile(path, dst); err != nil {
				return err
			}
			count++
			return nil
		})
		if err != nil {
			return 0, fmt.Errorf("backup: copy nfo: %w", err)
		}
	}
	return count, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// prune 只清理符合备份时间戳命名的目录，保留最新 retain 份。
func prune(outDir string, retain int) ([]string, error) {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return nil, fmt.Errorf("backup: prune: %w", err)
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := time.Parse("20060102T150405Z", entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(outDir, entry.Name(), "manifest.json"))
		var manifest backupManifest
		if err != nil || json.Unmarshal(data, &manifest) != nil || manifest.Format != backupFormat || manifest.Created.UTC().Format("20060102T150405Z") != entry.Name() {
			continue
		}
		dirs = append(dirs, entry.Name())
	}
	sort.Strings(dirs)
	if len(dirs) <= retain {
		return nil, nil
	}
	handle, err := os.OpenRoot(outDir)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	var pruned []string
	for _, name := range dirs[:len(dirs)-retain] {
		if err := handle.RemoveAll(name); err != nil {
			return pruned, fmt.Errorf("backup: prune %s: %w", name, err)
		}
		pruned = append(pruned, name)
	}
	return pruned, nil
}

const backupFormat = "emby-go-backup-v1"

type backupManifest struct {
	Format   string    `json:"format"`
	Created  time.Time `json:"created_at"`
	NFOCount int       `json:"nfo_count"`
}
