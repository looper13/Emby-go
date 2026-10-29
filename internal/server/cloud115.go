package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *App) registerCloud115Routes(r *gin.Engine) {
	admin := r.Group("/api/admin/115", a.requireAuth, a.requireAdministrator, noStore)
	admin.GET("/connection", a.bind((*App).cloud115Config))
	admin.PUT("/connection", a.bind((*App).saveCloud115Config))
	browse := r.Group("/api/r18/libraries/:lib/videos/:video/offline", a.requireAuth, noStore)
	browse.GET("", a.bind((*App).r18Offline))
	browse.POST("", a.requireAdministrator, a.bind((*App).createR18Offline))
	browse.POST("/selection", a.requireAdministrator, a.bind((*App).selectR18Offline))
	browse.POST("/tasks/:task/:action", a.requireAdministrator, a.bind((*App).changeR18Offline))
}
func (a *App) cloudVault() (*cloud115.Vault, error) {
	if a.cloud115Vault != nil {
		return a.cloud115Vault, nil
	}
	v, err := cloud115.OpenVault(filepath.Join(filepath.Dir(a.cfg.DBPath), ".115.key"))
	if err == nil {
		a.cloud115Vault = v
	}
	return v, err
}
func (a *App) cloudProvider() (cloud115.Provider, string, error) {
	a.cloud115Mu.Lock()
	defer a.cloud115Mu.Unlock()
	c, err := a.db.Cloud115Connection()
	if err != nil {
		return nil, "", err
	}
	if a.cloud115Client != nil && a.cloud115Revision == c.Revision {
		return a.cloud115Client, c.AccountID, nil
	}
	v, err := a.cloudVault()
	if err != nil {
		return nil, "", err
	}
	tokens, err := v.Open(c.Secrets)
	if err != nil {
		return nil, "", err
	}
	persist := func(t cloud115.Tokens) error {
		b, err := v.Seal(t)
		if err != nil {
			return err
		}
		return a.db.RefreshCloud115Tokens(c.AccountID, c.Revision, b)
	}
	if a.cloud115Open != nil {
		a.cloud115Client = a.cloud115Open(tokens, persist)
	} else {
		a.cloud115Client = cloud115.New(tokens, persist)
	}
	a.cloud115Revision = c.Revision
	return a.cloud115Client, c.AccountID, nil
}
func (a *App) cloud115Config(c *gin.Context) {
	conn, err := a.db.Cloud115Connection()
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(200, gin.H{"configured": false, "account_id": "", "folder_id": "", "folder_name": cloud115.CacheFolderName, "revision": 0})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "读取 115 配置失败"})
		return
	}
	c.JSON(200, gin.H{"configured": true, "account_id": conn.AccountID, "folder_id": conn.FolderID, "folder_name": cloud115.CacheFolderName, "revision": conn.Revision})
}
func (a *App) saveCloud115Config(c *gin.Context) {
	var req struct {
		Cookie   string `json:"cookie"`
		Revision int64  `json:"revision"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Cookie) > 16384 {
		c.JSON(400, gin.H{"error": "115 配置格式不正确"})
		return
	}
	a.cloud115Mu.Lock()
	defer a.cloud115Mu.Unlock()
	v, err := a.cloudVault()
	if err != nil {
		c.JSON(500, gin.H{"error": "无法读取或保护 115 密钥，请检查数据目录权限"})
		return
	}
	tokens := cloud115.Tokens{Cookie: strings.TrimSpace(req.Cookie)}
	old, err := a.db.Cloud115Connection()
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		c.JSON(500, gin.H{"error": "读取 115 配置失败"})
		return
	}
	if old.Revision != req.Revision {
		c.JSON(409, gin.H{"error": "115 配置已变化，请刷新后重试"})
		return
	}
	if old.Revision > 0 && tokens.Cookie == "" {
		prior, e := v.Open(old.Secrets)
		if e != nil {
			c.JSON(400, gin.H{"error": "既有凭证无法解密，请重新填写 Cookie"})
			return
		}
		tokens.Cookie = prior.Cookie
	}
	if tokens.Cookie == "" {
		c.JSON(400, gin.H{"error": "请填写 115 Cookie（UID、CID、SEID），旧 Token 配置需改用 Cookie"})
		return
	}
	tokens.Cookie, err = cloud115.NormalizeCookie(tokens.Cookie)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	persist := func(t cloud115.Tokens) error { tokens = t; return nil }
	var p cloud115.Provider
	if a.cloud115Open != nil {
		p = a.cloud115Open(tokens, persist)
	} else {
		p = cloud115.New(tokens, persist)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	account, err := p.Check(ctx, "")
	if err != nil {
		c.JSON(400, gin.H{"error": "115 验证失败：" + cloud115.SafeError(err, tokens).Error()})
		return
	}
	if old.Revision > 0 && old.AccountID != account {
		c.JSON(409, gin.H{"error": "当前仅支持同一 115 账号更新凭证，不能把既有任务切换到其它账号"})
		return
	}
	cache, ok := p.(cloud115.CacheProvider)
	if !ok {
		c.JSON(400, gin.H{"error": "115 连接不支持自动缓存目录，请使用 Cookie"})
		return
	}
	folder, err := cloud115.EnsureCacheFolder(ctx, cache)
	if err != nil {
		c.JSON(400, gin.H{"error": "115 缓存目录创建或验证失败：" + cloud115.SafeError(err, tokens).Error()})
		return
	}
	b, err := v.Seal(tokens)
	if err == nil {
		err = a.db.SaveCloud115Connection(store.Cloud115Connection{AccountID: account, FolderID: folder, Secrets: b}, req.Revision)
	}
	if err != nil {
		c.JSON(409, gin.H{"error": "保存 115 配置失败，请刷新后重试"})
		return
	}
	a.cloud115Client = nil
	a.cloud115Revision = 0
	a.offlineService.Wake()
	c.Status(204)
}
func (a *App) offlineItem(c *gin.Context) (store.Library, r18.Video, r18Source, bool) {
	l, ok := a.r18Library(c)
	if !ok {
		return l, r18.Video{}, nil, false
	}
	client, err := a.r18Client(l)
	if err != nil {
		a.r18Failure(c, "连接", err)
		return l, r18.Video{}, nil, false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	v, err := client.Get(ctx, c.Param("video"))
	if err != nil {
		a.r18Failure(c, "详情查询", err)
		return l, v, client, false
	}
	return l, v, client, true
}
func (a *App) r18Offline(c *gin.Context) {
	l, ok := a.r18Library(c)
	if !ok {
		return
	}
	conn, err := a.db.Cloud115Connection()
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(200, gin.H{"configured": false, "tasks": []gin.H{}, "sources": []gin.H{}})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "读取 115 状态失败"})
		return
	}
	bindings, err := a.db.OfflineBindings(l, c.Param("video"), conn.AccountID)
	if err != nil {
		c.JSON(500, gin.H{"error": "读取离线任务失败"})
		return
	}
	user, _ := a.db.User(a.db.UserID())
	tasks := []gin.H{}
	if user.Administrator {
		for _, b := range bindings {
			files := []gin.H{}
			for _, f := range b.Task.Files {
				if b.Task.CacheExpired(time.Now().Unix()) {
					break
				}
				files = append(files, gin.H{"id": f.ID, "name": f.Name, "path": f.Path, "size": f.Size})
			}
			t := b.Task
			status, message, retryable := t.Status, t.Error, t.Retryable
			if t.CacheExpired(time.Now().Unix()) && status != "expired" {
				status, message, retryable = "cleanup_pending", "缓存已满 24 小时，正在自动删除；清理完成后可重新提交下载", false
				if t.Stage == "cleanup" {
					message = t.Error
				}
			}
			tasks = append(tasks, gin.H{"id": t.ID, "resource_id": b.ResourceID, "status": status, "progress": t.Progress, "stage": t.Stage, "error": message, "retryable": retryable, "version": t.Version, "files": files, "file_id": b.FileID, "expires_at": t.ExpiresAt})
		}
	}
	c.JSON(200, gin.H{"configured": true, "tasks": tasks, "sources": a.r18ReadySources(c, l, c.Param("video"))})
}
func (a *App) createR18Offline(c *gin.Context) {
	l, v, client, ok := a.offlineItem(c)
	if !ok {
		return
	}
	var req struct {
		ResourceID string `json:"resource_id"`
		Magnet     string `json:"magnet"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.ResourceID) > 256 || req.ResourceID != "" && req.Magnet != "" {
		c.JSON(400, gin.H{"error": "请指定一个资源 ID 或手动磁力"})
		return
	}
	magnet := req.Magnet
	if req.ResourceID != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
		defer cancel()
		found := false
		for offset := 0; offset <= 10000; {
			page, err := client.Resources(ctx, v, offset)
			if err != nil {
				a.r18Failure(c, "磁力资源查询", err)
				return
			}
			for _, resource := range page.Items {
				if resource.ID == req.ResourceID {
					magnet = resource.Magnet
					found = true
					break
				}
			}
			if found || !page.HasMore {
				break
			}
			if page.NextOffset <= offset {
				break
			}
			offset = page.NextOffset
		}
		if !found {
			c.JSON(404, gin.H{"error": "资源不存在、已拒绝或不属于当前影片"})
			return
		}
	}
	magnet, hash, err := cloud115.NormalizeMagnet(magnet)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	conn, err := a.db.Cloud115Connection()
	if err != nil {
		c.JSON(409, gin.H{"error": "请先配置 115 连接"})
		return
	}
	_, err = a.db.CreateOfflineTask(conn.AccountID, conn.FolderID, magnet, hash, l, v.ID, req.ResourceID)
	if err != nil {
		c.JSON(500, gin.H{"error": "保存离线任务失败"})
		return
	}
	a.offlineService.Wake()
	a.r18Offline(c)
}
func (a *App) boundTask(c *gin.Context, l store.Library, id string) bool {
	conn, err := a.db.Cloud115Connection()
	if err != nil {
		return false
	}
	list, err := a.db.OfflineBindings(l, c.Param("video"), conn.AccountID)
	if err != nil {
		return false
	}
	for _, b := range list {
		if b.Task.ID == id {
			return true
		}
	}
	return false
}
func (a *App) selectR18Offline(c *gin.Context) {
	l, _, _, ok := a.offlineItem(c)
	if !ok {
		return
	}
	var req struct {
		Task    string `json:"task_id"`
		File    string `json:"file_id"`
		Default bool   `json:"default"`
	}
	if c.ShouldBindJSON(&req) != nil || !a.boundTask(c, l, req.Task) {
		c.JSON(404, gin.H{"error": "任务不属于当前影片"})
		return
	}
	if err := a.db.SelectOfflineFile(l, c.Param("video"), req.Task, req.File, req.Default); err != nil {
		c.JSON(409, gin.H{"error": "所选文件不属于该任务或任务状态已变化"})
		return
	}
	a.r18Offline(c)
}
func (a *App) changeR18Offline(c *gin.Context) {
	l, ok := a.r18Library(c)
	if !ok {
		return
	}
	id := c.Param("task")
	if !a.boundTask(c, l, id) {
		c.JSON(404, gin.H{"error": "任务不属于当前影片"})
		return
	}
	var req struct {
		Version int64 `json:"version"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "缺少任务版本"})
		return
	}
	var err error
	switch c.Param("action") {
	case "retry":
		err = a.offlineService.Retry(id, req.Version)
	case "cancel":
		err = a.offlineService.Cancel(id, req.Version)
	default:
		c.Status(404)
		return
	}
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	a.r18Offline(c)
}

type cloudTicket struct {
	User             int64
	Item, Source     string
	Revision, Expiry int64
}

func (a *App) cloudStreamURL(l store.Library, video, source string) string {
	id := r18ItemID(l.ID, video)
	t := cloudTicket{a.db.UserID(), id, source, l.Revision, time.Now().Add(6 * time.Hour).Unix()}
	data, _ := json.Marshal(t)
	mac := hmac.New(sha256.New, a.subtitleSigningKey[:])
	mac.Write(data)
	ticket := base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return streamURL(id) + "?" + url.Values{"MediaSourceId": {source}, "playback_token": {ticket}, "Static": {"true"}}.Encode()
}
func (a *App) r18ReadySources(c *gin.Context, l store.Library, video string) []gin.H {
	list := []gin.H{}
	conn, err := a.db.Cloud115Connection()
	if err != nil {
		return list
	}
	bindings, err := a.db.OfflineBindings(l, video, conn.AccountID)
	if err != nil {
		return list
	}
	for _, b := range bindings {
		if b.Task.Status != "ready" || b.Task.CacheExpired(time.Now().Unix()) {
			continue
		}
		for _, f := range b.Task.Files {
			if f.ID != b.FileID || !cloud115.VideoFile(f) {
				continue
			}
			id := "115-" + b.Task.ID
			u := a.cloudStreamURL(l, video, id)
			source := gin.H{"Id": id, "Name": f.Name, "Type": "Default", "Protocol": "Http", "Container": strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Name)), "."), "Size": f.Size, "Path": u, "DirectStreamUrl": u, "SupportsDirectPlay": true, "SupportsDirectStream": true, "SupportsTranscoding": false, "IsRemote": true, "AddApiKeyToDirectStreamUrl": false, "RequiresOpening": false, "RequiresClosing": false, "MediaStreams": []gin.H{{"Type": "Video", "Index": 0, "IsDefault": true}}, "Default": b.Default}
			if f.Duration > 0 && f.Duration <= 922337203685 {
				source["RunTimeTicks"] = f.Duration * 10000000
			}
			list = append(list, source)
		}
	}
	return list
}
func (a *App) r18Stream(c *gin.Context) {
	library, video, ok := parseR18ItemID(c.Param("id"))
	if !ok {
		c.Status(404)
		return
	}
	// Legacy local streams accept anonymous clients. Shared-cloud sources do not:
	// only a real authenticated account or an item/source/user-bound ticket enters.
	if _, authenticated := c.Get("accountUser"); !authenticated {
		parts := strings.Split(c.Query("playback_token"), ".")
		if len(parts) != 2 || len(parts[0]) > 2048 {
			c.JSON(401, gin.H{"error": "115 播放需要登录或有效播放票据"})
			return
		}
		data, e := base64.RawURLEncoding.DecodeString(parts[0])
		sig, e2 := base64.RawURLEncoding.DecodeString(parts[1])
		mac := hmac.New(sha256.New, a.subtitleSigningKey[:])
		mac.Write(data)
		var t cloudTicket
		if e != nil || e2 != nil || !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(data, &t) != nil || t.Expiry <= time.Now().Unix() || t.Item != c.Param("id") || t.Source != c.Query("MediaSourceId") {
			c.Status(401)
			return
		}
		user, e := a.db.User(t.User)
		l, e2 := a.db.Library(library)
		if e != nil || user.Disabled || e2 != nil || l.Revision != t.Revision {
			c.Status(401)
			return
		}
		a = &App{appState: a.appState, db: a.db.ForUser(user.ID)}
	}
	l, ok := a.r18CompatLibrary(c, library)
	if !ok {
		return
	}
	client, err := a.r18Client(l)
	if err != nil {
		c.JSON(502, gin.H{"error": "无法确认 R18 条目"})
		return
	}
	verifyCtx, verifyCancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	_, err = client.Get(verifyCtx, video)
	verifyCancel()
	if err != nil {
		c.Status(404)
		return
	}
	p, account, err := a.cloudProvider()
	if err != nil {
		c.JSON(503, gin.H{"error": "115 凭证不可用，请管理员检查配置"})
		return
	}
	bindings, err := a.db.OfflineBindings(l, video, account)
	if err != nil {
		c.Status(500)
		return
	}
	var chosen *cloud115.File
	var task store.OfflineTask
	requested := c.Query("MediaSourceId")
	for _, b := range bindings {
		if b.Task.Status != "ready" || b.Task.CacheExpired(time.Now().Unix()) || requested != "" && requested != "115-"+b.Task.ID {
			continue
		}
		for _, f := range b.Task.Files {
			if f.ID == b.FileID && cloud115.VideoFile(f) {
				copy := f
				chosen = &copy
				task = b.Task
				break
			}
		}
		if chosen != nil {
			break
		}
	}
	if chosen == nil {
		c.JSON(404, gin.H{"error": "当前影片没有所选的就绪播放源"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	fresh, err := p.Info(ctx, chosen.ID)
	if err == nil && (fresh.ID != chosen.ID || fresh.Directory || fresh.PickCode == "" || chosen.SHA1 != "" && fresh.SHA1 != chosen.SHA1) {
		err = &cloud115.Error{Message: "115 文件身份变化或文件不可播放"}
	}
	if err != nil {
		var e *cloud115.Error
		if errors.As(err, &e) && e.Missing {
			task.Status = "failed"
			task.Resume = "discovering"
			task.Stage = "file"
			task.Error = "115 文件已删除，请恢复文件后重试确认"
			task.Retryable = true
			_ = a.db.UpdateOfflineTask(task, task.Version)
		}
		c.JSON(502, gin.H{"error": "115 文件确认失败：" + cloud115.SafeError(err, cloud115.Tokens{}).Error()})
		return
	}
	raw, err := p.URL(ctx, fresh.PickCode, c.Request.UserAgent())
	if err != nil {
		c.JSON(502, gin.H{"error": "115 直链获取失败：" + cloud115.SafeError(err, cloud115.Tokens{}).Error()})
		return
	}
	// No long-lived signed URL cache; each playback uses the actual client's UA.
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(raw, "\r\n\\") {
		c.Status(502)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Location", raw)
	c.Status(http.StatusFound)
}
