package server

import (
	"crypto/rand"
	"database/sql"
	"emby-go/internal/store"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Cookie lifetime; persistent login tokens remain valid until revoked.
const accessTokenTTL = 7 * 24 * time.Hour

// setAdminName / currentAdminName 保护 adminName：初始化与登录会写，其它 handler 并发读。
func (a *App) setAdminName(name string) {
	a.adminMu.Lock()
	a.adminName = name
	a.adminMu.Unlock()
}

func (a *App) currentAdminName() string {
	a.adminMu.RLock()
	defer a.adminMu.RUnlock()
	return a.adminName
}

func requestToken(c *gin.Context) string {
	query := c.Request.URL.Query()
	token := c.GetHeader("X-Emby-Token")
	if token == "" {
		token = c.GetHeader("X-MediaBrowser-Token")
	}
	if token == "" {
		token = query.Get("api_key")
	}
	if token == "" {
		token = query.Get("ApiKey")
	}
	if token == "" {
		token = query.Get("X-Emby-Token")
	}
	if token == "" {
		authorization := c.GetHeader("X-Emby-Authorization")
		if authorization == "" {
			authorization = c.GetHeader("Authorization")
		}
		authorization = strings.TrimSpace(authorization)
		if i := strings.IndexAny(authorization, " \t"); i >= 0 && (strings.EqualFold(authorization[:i], "Emby") || strings.EqualFold(authorization[:i], "MediaBrowser")) {
			authorization = strings.TrimSpace(authorization[i+1:])
		}
		for _, part := range strings.Split(authorization, ",") {
			key, value, found := strings.Cut(strings.TrimSpace(part), "=")
			if found && strings.EqualFold(key, "Token") {
				token = strings.Trim(strings.TrimSpace(value), "\\\"")
				break
			}
		}
	}
	// Browser images cannot set custom headers. A first-party, HTTP-only
	// cookie is accepted for reads only; mutations still require an API token.
	if token == "" && (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) {
		token, _ = c.Cookie("emby_session")
	}
	return token
}

func (a *App) authOK(c *gin.Context) bool {
	token := requestToken(c)
	if token == "" {
		return false
	}
	// Always consult the persistent credential store so logout and password
	// changes cannot be bypassed by a warm or concurrently refilled cache.
	user, err := a.db.TokenUser(token)
	if err == nil {
		c.Set("accountUser", user)
		return true
	}
	if key, err := a.db.HasAPIKey(token); err == nil && key {
		user, err := a.db.User(1)
		if err != nil || user.Disabled {
			return false
		}
		c.Set("accountUser", user)
		return true
	}
	return false
}

func (a *App) requireAuth(c *gin.Context) {
	if !a.authOK(c) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.Next()
}

func (a *App) authStatus(c *gin.Context) {
	initialized, err := a.db.HasAdministrator()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"initialized": initialized})
}

func validCredentials(username, password string) bool {
	return len([]rune(strings.TrimSpace(username))) >= 3 && len([]rune(strings.TrimSpace(username))) <= 64 && len([]rune(password)) >= 9 && len(password) <= 72
}

func (a *App) initialize(c *gin.Context) {
	var req struct {
		Username string `json:"Username"`
		Pw       string `json:"Pw"`
	}
	if c.ShouldBindJSON(&req) != nil || !validCredentials(req.Username, req.Pw) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "账号须为 3-64 个字符，密码至少 9 个字符且不超过 72 字节"})
		return
	}
	initialized, err := a.db.HasAdministrator()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if initialized {
		c.JSON(http.StatusConflict, gin.H{"error": "管理员已初始化"})
		return
	}
	if err := a.db.InitializeAdministrator(strings.TrimSpace(req.Username), req.Pw); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "初始化失败，请更换账号后重试"})
		return
	}
	a.setAdminName(strings.TrimSpace(req.Username))
	c.Status(http.StatusNoContent)
}

func (a *App) authenticate(c *gin.Context) {
	var req struct {
		Username string `json:"Username" form:"Username"`
		Pw       string `json:"Pw" form:"Pw"`
	}
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid credentials payload"})
		return
	}
	a.authenticateCredentials(c, req.Username, req.Pw)
}

// authenticateByID is the password login variant used after selecting a user.
// User selection identifies a real account, never the original administrator.
func (a *App) authenticateByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("uid"), 10, 64)
	user, err := a.db.User(id)
	if err != nil || id <= 0 {
		c.Status(404)
		return
	}
	var req struct {
		Pw string `json:"Pw" form:"Pw"`
	}
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid credentials payload"})
		return
	}
	a.authenticateCredentials(c, user.Name, req.Pw)
}

func (a *App) authenticateCredentials(c *gin.Context, username, password string) {
	a.credentialMu.Lock()
	defer a.credentialMu.Unlock()
	ip := c.RemoteIP()
	if wait := a.authLimiter.retryAfter(ip, username); wait > 0 {
		c.Header("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "登录尝试过于频繁，请稍后重试"})
		return
	}
	initialized, err := a.db.HasAdministrator()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !initialized {
		c.JSON(http.StatusPreconditionRequired, gin.H{"error": "管理员尚未初始化"})
		return
	}
	user, err := a.db.AuthenticateUser(strings.TrimSpace(username), password)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrInvalidPassword) {
		a.authLimiter.recordFailure(ip, username)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "账号或密码错误"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	b := make([]byte, 24)
	a.authLimiter.reset(ip, username)
	if _, err = rand.Read(b); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成访问令牌失败"})
		return
	}
	token := hex.EncodeToString(b)
	if err := a.db.SaveUserToken(token, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存访问令牌失败"})
		return
	}
	setSessionCookie(c, token, int(accessTokenTTL.Seconds()))
	c.JSON(http.StatusOK, gin.H{
		"AccessToken": token,
		"ServerId":    a.serverID,
		"User":        (&App{appState: a.appState, db: a.db.ForUser(user.ID)}).userDto(),
	})
}

// userDto 组装 Emby 的 UserDto。
//
// Policy 不能给空对象：tsukimi 用 serde 严格反序列化，`Policy { IsAdministrator: bool }`
// 是**非 Option** 字段——返回 {} 会让整个登录响应解析失败（登录直接不可用）。
// 可配置字段读取当前账号；尚未实现的能力策略仅保留兼容字段。
func (a *App) userDto() gin.H {
	user, _ := a.db.User(a.db.UserID())
	policy, err := a.db.LibraryAccessPolicy()
	if err != nil {
		policy.EnableAllFolders = false
		policy.LibraryIDs = nil
	}
	folders := make([]string, 0, len(policy.LibraryIDs))
	for _, id := range policy.LibraryIDs {
		folders = append(folders, externalLibraryID(id))
	}
	return gin.H{
		"Id": strconv.FormatInt(a.db.UserID(), 10), "Name": user.Name, "ServerName": a.currentServerName(),
		"ServerId": a.serverID, "HasPassword": true,
		"HasConfiguredPassword": true, "HasConfiguredEasyPassword": false,
		"EnableAutoLogin": false, "LastLoginDate": "", "LastActivityDate": "",
		"Configuration": gin.H{},
		"Policy": gin.H{
			"IsAdministrator": user.Administrator, "IsHidden": false, "IsDisabled": user.Disabled,
			"MaxActiveSessions": 0,
			// 播放/下载能力：客户端据此决定是否允许直接播放与转码。
			"EnableMediaPlayback":             true,
			"EnableAudioPlaybackTranscoding":  true,
			"EnableVideoPlaybackTranscoding":  true,
			"EnablePlaybackRemuxing":          true,
			"EnableContentDownloading":        true,
			"EnableSubtitleDownloading":       true,
			"EnableSubtitleManagement":        user.Administrator,
			"EnableAllFolders":                policy.EnableAllFolders,
			"EnableAllChannels":               true,
			"EnableAllDevices":                true,
			"EnableSyncTranscoding":           true,
			"EnableRemoteAccess":              true,
			"EnableRemoteControlOfOtherUsers": false,
			"EnableSharedDeviceControl":       false,
			"EnableLiveTvManagement":          false,
			"EnableLiveTvAccess":              false,
			"EnableContentDeletion":           false,
			"EnablePublicSharing":             false,
			"BlockedChannels":                 []string{},
			"BlockedMediaFolders":             []string{},
			"BlockedTags":                     []string{},
			"AllowedTags":                     []string{},
			"BlockUnratedItems":               []string{},
			"EnabledDevices":                  []string{},
			"EnabledChannels":                 []string{},
			"EnabledFolders":                  folders,
			"AuthenticationProviderId":        "",
			"PasswordResetProviderId":         "",
		},
	}
}

func (a *App) me(c *gin.Context) {
	if valid, err := a.db.HasAccessToken(requestToken(c)); err == nil && valid {
		setSessionCookie(c, requestToken(c), int(accessTokenTTL.Seconds()))
	}
	c.JSON(http.StatusOK, a.userDto())
}

func setSessionCookie(c *gin.Context, token string, maxAge int) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("emby_session", token, maxAge, "/", "", requestScheme(c) == "https", true)
}

// userByID 处理 GET /Users/{uid}：tsukimi 登录后立刻调它读 Policy.IsAdministrator。
func (a *App) userByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("uid"), 10, 64)
	user, err := a.db.User(id)
	current, _ := a.db.User(a.db.UserID())
	if err != nil || (!current.Administrator && current.ID != user.ID) {
		c.Status(404)
		return
	}
	a = &App{appState: a.appState, db: a.db.ForUser(id)}
	c.JSON(http.StatusOK, a.userDto())
}

func (a *App) validUser(c *gin.Context) bool {
	if c.Param("uid") != strconv.FormatInt(a.db.UserID(), 10) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return false
	}
	return true
}

func (a *App) forRequest(c *gin.Context) *App {
	if value, ok := c.Get("accountUser"); ok {
		user := value.(store.User)
		return &App{appState: a.appState, db: a.db.ForUser(user.ID)}
	}
	return a
}
func (a *App) bind(handler func(*App, *gin.Context)) gin.HandlerFunc {
	return func(c *gin.Context) { handler(a.forRequest(c), c) }
}
func (a *App) requireAdministrator(c *gin.Context) {
	value, ok := c.Get("accountUser")
	if !ok || !value.(store.User).Administrator {
		c.AbortWithStatusJSON(403, gin.H{"error": "需要管理员权限"})
		return
	}
	c.Next()
}
func (a *App) validQueryUser(c *gin.Context) bool {
	// Season/episode adapters rewrite RawQuery before common item browsing.
	uid := c.Request.URL.Query().Get("UserId")
	return uid == "" || uid == strconv.FormatInt(a.db.UserID(), 10)
}
