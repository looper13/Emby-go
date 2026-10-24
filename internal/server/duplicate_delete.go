package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"emby-go/internal/alist"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

type deletionFile struct {
	LocalPath  string     `json:"local_path"`
	RemotePath string     `json:"remote_path"`
	Digest     string     `json:"digest"`
	Remote     alist.File `json:"remote"`
	Attempted  bool       `json:"attempted"`
	Removed    bool       `json:"removed"`
}

type movieDeletion struct {
	MovieID     int64          `json:"movie_id"`
	Key         string         `json:"key"`
	LibraryID   int64          `json:"library_id"`
	LibraryPath string         `json:"library_path"`
	AListURL    string         `json:"alist_url"`
	Sources     []string       `json:"sources"`
	Files       []deletionFile `json:"files"`
}

func readDeletionSTRM(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, errors.New("STRM 不是普通文件或超过 64 KiB")
	}
	return io.ReadAll(io.LimitReader(f, (64<<10)+1))
}

func strmLink(data []byte) string {
	return strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
}
func deletionDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func deletionLocalPath(root, source string, allowMissing bool) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return "", err
	}
	inside := func(base, target string) bool {
		rel, err := filepath.Rel(base, target)
		return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	}
	if !inside(root, source) || !strings.EqualFold(filepath.Ext(source), ".strm") {
		return "", errors.New("只允许删除当前媒体库内的 STRM 文件")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("媒体库目录无法访问")
	}
	resolved, err := filepath.EvalSymlinks(source)
	if allowMissing && errors.Is(err, os.ErrNotExist) {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(source))
		if parentErr != nil {
			return "", errors.New("STRM 父目录无法访问")
		}
		resolved, err = filepath.Join(parent, filepath.Base(source)), nil
	}
	if err != nil || !inside(resolvedRoot, resolved) {
		return "", errors.New("STRM 不存在、无法访问或符号链接指向媒体库外")
	}
	return source, nil
}

func (a *App) deletionPlan(ctx context.Context, id, libraryID int64, key string) (movieDeletion, *alist.Client, error) {
	plan := movieDeletion{}
	movie, err := a.db.Movie(id)
	if err != nil || !a.db.CanAccessLibrary(movie.LibraryID) || libraryID != 0 && libraryID != movie.LibraryID {
		return plan, nil, errors.New("影片不存在或不可访问")
	}
	if key == "" || store.NormalizeDuplicateNumber(movie.Number) != key || movie.Television != nil {
		return plan, nil, errors.New("影片已不属于该番号分组，请重新查重")
	}
	lib, err := a.db.Library(movie.LibraryID)
	if err != nil {
		return plan, nil, errors.New("媒体库不存在")
	}
	cfg := a.configSnapshot()
	client, err := alist.New(cfg.AListURL, cfg.AListToken)
	if err != nil {
		return plan, nil, err
	}
	sources := append([]string{movie.SourcePath}, movie.AdditionalParts...)
	if len(sources) > 64 {
		return plan, nil, errors.New("影片分段超过 64 个，请拆分处理")
	}
	state, err := a.db.MovieDeletionState(id)
	if err != nil {
		return plan, nil, errors.New("读取删除进度失败")
	}
	if state != "" {
		if json.Unmarshal([]byte(state), &plan) != nil || plan.MovieID != id || plan.Key != key || plan.LibraryID != movie.LibraryID || plan.LibraryPath != lib.Path || plan.AListURL != cfg.AListURL || !reflect.DeepEqual(plan.Sources, sources) || len(plan.Files) != len(sources) {
			return plan, nil, errors.New("未完成的删除记录与当前影片或 AList 配置不同，请恢复原配置后重试")
		}
	} else {
		members, err := a.db.DuplicateMovieMembers(ctx, libraryID, key, 1, 0)
		if err != nil || members.Total < 2 {
			return plan, nil, errors.New("该番号当前不足两份，请重新查重")
		}
		plan = movieDeletion{MovieID: id, Key: key, LibraryID: movie.LibraryID, LibraryPath: lib.Path, AListURL: cfg.AListURL, Sources: sources, Files: []deletionFile{}}
	}
	for i, source := range sources {
		var file deletionFile
		if state != "" {
			file = plan.Files[i]
		}
		local, err := deletionLocalPath(lib.Path, source, file.Removed)
		if err != nil {
			return plan, nil, err
		}
		data, err := readDeletionSTRM(local)
		if !(file.Removed && errors.Is(err, os.ErrNotExist)) {
			if err != nil {
				return plan, nil, fmt.Errorf("无法读取 STRM：%s", local)
			}
			remote, err := client.Resolve(strmLink(data))
			if err != nil {
				return plan, nil, err
			}
			if state != "" && (file.LocalPath != local || file.RemotePath != remote || file.Digest != deletionDigest(data)) {
				return plan, nil, errors.New("STRM 已发生变化，请恢复原文件后再处理未完成的删除")
			}
			file.LocalPath, file.RemotePath, file.Digest = local, remote, deletionDigest(data)
		}
		if !file.Removed {
			remote, err := client.Stat(ctx, file.RemotePath)
			if file.Attempted && errors.Is(err, alist.ErrNotFound) {
				file.Removed = true
			} else if err != nil {
				return plan, nil, fmt.Errorf("检查远程文件 %s 失败：%w", file.RemotePath, err)
			} else if state != "" && remote != file.Remote {
				return plan, nil, errors.New("远程文件已被替换，停止删除")
			} else {
				file.Remote = remote
			}
		}
		if state == "" {
			plan.Files = append(plan.Files, file)
		} else {
			plan.Files[i] = file
		}
	}
	locals, remotes := map[string]bool{}, map[string]bool{}
	for _, file := range plan.Files {
		resolved, err := filepath.EvalSymlinks(file.LocalPath)
		if err == nil {
			locals[cachePathKey(resolved)] = true
		}
		remotes[file.RemotePath] = true
	}
	err = a.db.VisitOtherMovieSources(ctx, id, func(otherID int64, source string) error {
		resolved, err := filepath.EvalSymlinks(source)
		if err == nil && locals[cachePathKey(resolved)] {
			return fmt.Errorf("本地 STRM 也被影片 %d 引用，不能删除", otherID)
		}
		data, err := readDeletionSTRM(source)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("无法核验影片 %d 的 STRM 引用，请修复文件后重试", otherID)
		}
		remote, err := client.Resolve(strmLink(data))
		if err == nil && remotes[remote] {
			return fmt.Errorf("远程文件 %s 也被影片 %d 引用，不能删除该片源", remote, otherID)
		}
		return nil
	})
	return plan, client, err
}

func (a *App) deletionToken(plan movieDeletion, libraryID, expires int64) string {
	mac := hmac.New(sha256.New, a.subtitleSigningKey[:])
	data, _ := json.Marshal(plan)
	fmt.Fprintf(mac, "duplicate-delete-v1:%d:%d:%s:", libraryID, expires, a.configSnapshot().AListToken)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *App) adminDuplicateDeletePreview(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	libraryID, libErr := strconv.ParseInt(c.DefaultQuery("library_id", "0"), 10, 64)
	if err != nil || id <= 0 || libErr != nil || libraryID < 0 {
		c.JSON(400, gin.H{"error": "无效的影片或媒体库参数"})
		return
	}
	if !a.claimNFORequest(c, "delete") {
		return
	}
	defer a.releaseNFO("delete")
	a.configEditMu.Lock()
	defer a.configEditMu.Unlock()
	plan, _, err := a.deletionPlan(c.Request.Context(), id, libraryID, c.Query("key"))
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	expires := time.Now().Add(10 * time.Minute).Unix()
	c.JSON(200, gin.H{"movie_id": id, "alist_url": plan.AListURL, "files": plan.Files, "expires_at": expires, "preview_token": a.deletionToken(plan, libraryID, expires)})
}

func (a *App) adminDuplicateDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Key       string `json:"key"`
		LibraryID int64  `json:"library_id"`
		Expires   int64  `json:"expires_at"`
		Token     string `json:"preview_token"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err != nil || id <= 0 || c.ShouldBindJSON(&req) != nil || req.LibraryID < 0 || req.Expires < time.Now().Unix() || req.Expires > time.Now().Add(10*time.Minute).Unix() || req.Token == "" {
		c.JSON(400, gin.H{"error": "删除确认无效或已过期，请重新预览"})
		return
	}
	if !a.claimNFORequest(c, "delete") {
		return
	}
	defer a.releaseNFO("delete")
	a.configEditMu.Lock()
	defer a.configEditMu.Unlock()
	// Once confirmed, finish bookkeeping even if the browser disconnects.
	ctx, cancel := context.WithTimeout(a.rootCtx, 3*time.Minute)
	defer cancel()
	plan, client, err := a.deletionPlan(ctx, id, req.LibraryID, req.Key)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	if !hmac.Equal([]byte(req.Token), []byte(a.deletionToken(plan, req.LibraryID, req.Expires))) {
		c.JSON(409, gin.H{"error": "影片、STRM 或远程文件在预览后发生变化，请重新预览"})
		return
	}
	save := func() error { data, _ := json.Marshal(plan); return a.db.SaveMovieDeletionState(id, string(data)) }
	fail := func(err error) {
		removed := []string{}
		for _, file := range plan.Files {
			if file.Removed {
				removed = append(removed, file.RemotePath)
			}
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "删除未完成：" + err.Error() + "。本地剩余 STRM 与索引保留，可重新预览后继续。", "removed_remote_paths": removed})
	}
	if err := save(); err != nil {
		fail(errors.New("保存删除进度失败，未执行删除"))
		return
	}
	for i := range plan.Files {
		file := &plan.Files[i]
		if file.Removed {
			continue
		}
		current, err := client.Stat(ctx, file.RemotePath)
		if err != nil || current != file.Remote {
			fail(errors.New("远程文件无法核验或已变化"))
			return
		}
		file.Attempted = true
		if err := save(); err != nil {
			fail(errors.New("保存删除进度失败"))
			return
		}
		if err := client.Remove(ctx, file.RemotePath); err != nil {
			fail(err)
			return
		}
		for j := range plan.Files {
			if plan.Files[j].RemotePath == file.RemotePath {
				plan.Files[j].Attempted, plan.Files[j].Removed = true, true
			}
		}
		if err := save(); err != nil {
			fail(errors.New("远程已删除，但保存进度失败"))
			return
		}
	}
	for _, file := range plan.Files {
		local, err := deletionLocalPath(plan.LibraryPath, file.LocalPath, true)
		if err != nil {
			fail(err)
			return
		}
		data, err := readDeletionSTRM(local)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || deletionDigest(data) != file.Digest {
			fail(errors.New("本地 STRM 已变化，停止清理"))
			return
		}
		if err := os.Remove(local); err != nil {
			fail(fmt.Errorf("清理本地 STRM 失败：%s", local))
			return
		}
	}
	if err := a.db.DeleteMovie(id); err != nil {
		fail(errors.New("清理影片索引失败"))
		return
	}
	_ = a.db.BumpVersion(plan.LibraryID)
	c.Status(http.StatusNoContent)
}
