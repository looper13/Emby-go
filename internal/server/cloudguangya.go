package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/clouddrive"
	"emby-go/internal/cloudguangya"
	"emby-go/internal/offline"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *App) registerCloudGuangyaRoutes(r *gin.Engine) {
	admin := r.Group("/api/admin/guangya", a.requireAuth, a.requireAdministrator, noStore)
	admin.GET("/connection", a.bind((*App).cloudGuangyaConfig))
	admin.PUT("/connection", a.bind((*App).saveCloudGuangyaConfig))
	browse := r.Group("/api/r18/libraries/:lib/videos/:video/offline/guangya", a.requireAuth, noStore, func(c *gin.Context) {
		c.Set("offlineProvider", "guangya")
	})
	browse.GET("", a.bind((*App).r18Offline))
	browse.GET("/prepare", a.bind((*App).prepareR18Offline))
	browse.POST("", a.requireAdministrator, a.bind((*App).createR18Offline))
	browse.POST("/selection", a.requireAdministrator, a.bind((*App).selectR18Offline))
	browse.POST("/tasks/:task/:action", a.requireAdministrator, a.bind((*App).changeR18Offline))
}

func requestCloudProvider(c *gin.Context) string {
	if c.GetString("offlineProvider") == "guangya" {
		return "guangya"
	}
	return "115"
}

func cloudName(provider string) string {
	if provider == "guangya" {
		return "光鸭"
	}
	return "115"
}

func cloudSourceProvider(source string) (string, bool) {
	for _, provider := range []string{"115", "guangya"} {
		if strings.HasPrefix(source, provider+"-") {
			return provider, true
		}
	}
	return "", false
}

func (a *App) offlineConnection(provider string) (store.Cloud115Connection, error) {
	if provider == "guangya" {
		connection, err := a.db.CloudGuangyaConnection()
		return store.Cloud115Connection{AccountID: store.CloudGuangyaAccount(connection.AccountID), FolderID: connection.FolderID, Revision: connection.Revision}, err
	}
	return a.db.Cloud115Connection()
}

func (a *App) providerFor(provider string) (cloud115.Provider, string, error) {
	if provider == "guangya" {
		return a.guangyaProvider()
	}
	return a.cloudProvider()
}

func (a *App) offlineQueue(provider string) *offline.Service {
	if provider == "guangya" {
		return a.guangyaOffline
	}
	return a.offlineService
}

// 调用方持有 guangyaMu；光鸭密钥与既有 115 密钥分开保存。
func (a *App) guangyaSecretsVault() (*cloud115.Vault, error) {
	if a.guangyaVault != nil {
		return a.guangyaVault, nil
	}
	vault, err := cloud115.OpenVaultFor(filepath.Join(filepath.Dir(a.cfg.DBPath), ".guangya.key"), "guangya")
	if err == nil {
		a.guangyaVault = vault
	}
	return vault, err
}

// 装载失败只对外暴露固定原因，底层错误通过 Unwrap 保留，避免输出凭据内容。
type guangyaProviderLoadError struct {
	stage   string
	message string
	cause   error
}

func (e *guangyaProviderLoadError) Error() string { return e.message }
func (e *guangyaProviderLoadError) Unwrap() error { return e.cause }

func (a *App) guangyaProvider() (cloud115.Provider, string, error) {
	a.guangyaMu.Lock()
	defer a.guangyaMu.Unlock()
	connection, err := a.db.CloudGuangyaConnection()
	if err != nil {
		return nil, "", &guangyaProviderLoadError{"connection", "读取光鸭连接失败，请管理员检查配置", err}
	}
	account := store.CloudGuangyaAccount(connection.AccountID)
	if a.guangyaClient != nil && a.guangyaRevision == connection.Revision {
		return a.guangyaClient, account, nil
	}
	vault, err := a.guangyaSecretsVault()
	if err != nil {
		return nil, "", &guangyaProviderLoadError{"key", "光鸭密钥无法装载，请检查数据目录内的 .guangya.key 及其访问权限", err}
	}
	var tokens cloudguangya.Tokens
	if err = vault.OpenValue(connection.Secrets, &tokens); err != nil {
		return nil, "", &guangyaProviderLoadError{"credentials", "光鸭凭据无法解密或解析，请恢复与数据库匹配的 .guangya.key 或重新配置光鸭连接", err}
	}
	persist := func(tokens cloudguangya.Tokens) error {
		secrets, err := vault.SealValue(tokens)
		if err != nil {
			return err
		}
		return a.db.RefreshCloudGuangyaTokens(connection.AccountID, connection.Revision, secrets)
	}
	if a.guangyaOpen != nil {
		a.guangyaClient = a.guangyaOpen(tokens, persist)
	} else {
		a.guangyaClient = cloudguangya.New(tokens, persist)
	}
	if guard, ok := a.guangyaClient.(interface{ SetExpectedAccount(string) }); ok {
		guard.SetExpectedAccount(connection.AccountID)
	}
	a.guangyaRevision = connection.Revision
	return a.guangyaClient, account, nil
}

func (a *App) cloudGuangyaConfig(c *gin.Context) {
	connection, err := a.db.CloudGuangyaConnection()
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusOK, gin.H{"configured": false, "account_id": "", "folder_id": "", "folder_name": cloud115.CacheFolderName, "revision": 0})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取光鸭配置失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"configured": true, "account_id": connection.AccountID, "folder_id": connection.FolderID, "folder_name": cloud115.CacheFolderName, "revision": connection.Revision})
}

func (a *App) saveCloudGuangyaConfig(c *gin.Context) {
	var req struct {
		Access   string `json:"access_token"`
		Refresh  string `json:"refresh_token"`
		DeviceID string `json:"device_id"`
		Revision int64  `json:"revision"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Access) > 16384 || len(req.Refresh) > 16384 || len(req.DeviceID) > 128 || req.Revision < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "光鸭配置格式不正确"})
		return
	}
	a.guangyaMu.Lock()
	defer a.guangyaMu.Unlock()
	old, err := a.db.CloudGuangyaConnection()
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取光鸭配置失败"})
		return
	}
	if old.Revision != req.Revision {
		c.JSON(http.StatusConflict, gin.H{"error": "光鸭配置已变化，请刷新后重试"})
		return
	}
	vault, err := a.guangyaSecretsVault()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法读取或保护光鸭密钥，请检查数据目录权限"})
		return
	}
	// 刷新回调会替换候选凭证，整次验证都保留新旧秘密用于统一脱敏。
	sensitive := []string{req.Access, req.Refresh, req.DeviceID}
	rememberTokens := func(candidate cloudguangya.Tokens) {
		sensitive = append(sensitive, candidate.Access, candidate.Refresh, candidate.DeviceID)
	}
	safeReason := func(err error) string {
		secrets := append([]string(nil), sensitive...)
		// 先隐藏较长凭证，避免短旧令牌恰好是新令牌前缀时留下新令牌后缀。
		sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
		return clouddrive.SafeError(err, secrets...).Error()
	}
	tokens := cloudguangya.Tokens{Access: strings.TrimSpace(req.Access), Refresh: strings.TrimSpace(req.Refresh), DeviceID: strings.TrimSpace(req.DeviceID)}
	rememberTokens(tokens)
	if old.Revision > 0 && (tokens.Access == "" || tokens.Refresh == "" || tokens.DeviceID == "") {
		var prior cloudguangya.Tokens
		if err = vault.OpenValue(old.Secrets, &prior); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "既有凭证无法解密，请恢复光鸭密钥或重新填写全部凭证"})
			return
		}
		rememberTokens(prior)
		if tokens.Access == "" {
			// 新填刷新令牌时立即验证它，避免继续用旧访问令牌掩盖错误配置。
			if tokens.Refresh == "" {
				tokens.Access = prior.Access
			}
		}
		if tokens.Refresh == "" {
			tokens.Refresh = prior.Refresh
		}
		if tokens.DeviceID == "" {
			tokens.DeviceID = prior.DeviceID
		}
	}
	rememberTokens(tokens)
	tokens, err = cloudguangya.NormalizeTokens(tokens)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rememberTokens(tokens)
	persist := func(fresh cloudguangya.Tokens) error {
		rememberTokens(fresh)
		tokens = fresh
		return nil
	}
	var provider cloud115.Provider
	if a.guangyaOpen != nil {
		provider = a.guangyaOpen(tokens, persist)
	} else {
		provider = cloudguangya.New(tokens, persist)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	account, err := provider.Check(ctx, "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "光鸭账号验证失败：" + safeReason(err)})
		return
	}
	account = strings.TrimSpace(account)
	if account == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "光鸭账号验证失败：未返回有效账号身份"})
		return
	}
	if old.Revision > 0 && old.AccountID != account {
		c.JSON(http.StatusConflict, gin.H{"error": "当前仅支持同一光鸭账号更新凭证，不能把既有任务切换到其它账号"})
		return
	}
	cache, ok := provider.(cloud115.CacheProvider)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "光鸭连接不支持专属任务缓存目录"})
		return
	}
	folder, err := cloud115.EnsureCacheFolder(ctx, cache)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "光鸭缓存目录创建或验证失败：" + safeReason(err)})
		return
	}
	root, err := provider.Info(ctx, folder)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "光鸭缓存目录信息读取失败：" + safeReason(err)})
		return
	}
	var mismatch string
	switch {
	case root.ID != folder:
		mismatch = "返回的目录 ID 与缓存目录不一致"
	case !root.Directory:
		mismatch = "返回的缓存路径不是文件夹"
	case root.ParentID != "0":
		mismatch = "缓存目录不位于网盘根目录"
	case root.Name != cloud115.CacheFolderName:
		mismatch = "缓存目录名称不匹配"
	}
	if mismatch != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "光鸭缓存目录身份核验失败：" + mismatch})
		return
	}
	secrets, err := vault.SealValue(tokens)
	if err == nil {
		err = a.db.SaveCloudGuangyaConnection(store.CloudGuangyaConnection{AccountID: account, FolderID: folder, Secrets: secrets}, req.Revision)
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "保存光鸭配置失败，请刷新后重试"})
		return
	}
	a.guangyaClient = nil
	a.guangyaRevision = 0
	a.guangyaOffline.Wake()
	c.Status(http.StatusNoContent)
}
