package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"emby-go/internal/cache"
	"emby-go/internal/config"
	"emby-go/internal/librarywatch"
	"emby-go/internal/logging"
	"emby-go/internal/metatube"
	"emby-go/internal/scheduler"
	"emby-go/internal/store"
	"emby-go/internal/subtitle"
)

type App struct {
	*appState
	db *store.Store
}

type appState struct {
	cfgMu           sync.RWMutex
	configEditMu    sync.Mutex // serializes file updates and monitor reconfiguration
	configWarnings  []string   // guarded by configEditMu; retained until monitor reconfiguration
	cfg             config.Config
	router          *gin.Engine
	cache           *cache.Managed
	scopeVersions   sync.Map
	allScopeVersion atomic.Uint64
	adminMu         sync.RWMutex // 保护 adminName：初始化/登录写，其它 handler 并发读
	adminName       string
	credentialMu    sync.Mutex
	serverID        string
	taskMu          sync.Mutex
	tasks           []task
	nextTaskID      int64

	// 进程内短缓存：图片 mtime tag 与 NFO 流信息解析结果，
	// 避免列表/详情请求对媒体盘反复 stat / XML 解析（媒体盘可能较慢）。
	tagMu        sync.Mutex
	tags         map[string]tagEntry
	diskVersions map[string]diskVersionRecord
	// diskVersionBase 为记录回收后的版本基线；diskVersionSeq 单调分配失效版本。
	diskVersionBase uint64
	diskVersionSeq  uint64
	nfoMu           sync.Mutex
	nfos            *nfoCache

	// 图片链路的进程内缓存：imgMeta 存「影片 id → 图片路径」「演员名 → 头像路径」
	// 这类每次请求都要的元信息（原本每张图一次 DB 查询，单连接下会串行排队）；
	// imgThumb 存按请求尺寸生成的缩略图字节。都只在本进程内有意义，
	// 走 Redis 每张图一次 RTT 不划算，故用本地 LRU。
	imgMeta     *cache.Memory
	imgThumb    *cache.Memory
	artworkOnce sync.Once
	artworks    *artworkCache
	// thumbSem 限制同时解码 + 缩放 + 编码的数量：纯 CPU 活，一次海报墙
	// 几百张不加限制会把 CPU 打满、拖慢其它请求。
	thumbSem chan struct{}
	// scrapeSem 限制同时向上游拉取的缩略图数量：刮削预览页一次十几张，
	// 不加限制就是十几路并发上游请求，慢上游会把 goroutine 堆起来。
	scrapeSem chan struct{}

	// 扫描进度：POST /scan 执行期间由进度回调写入，GET /scan/progress 轮询读取。
	scanMu     sync.RWMutex
	scanStatus scanStatus

	// 媒体信息探测任务状态。probeCtx 挂在 rootCtx 之下：
	// 任务在 goroutine 里跑，必须用常驻 context——用请求 context 会在 handler 返回时被取消。
	probeTaskMu sync.RWMutex
	probeStatus probeStatus
	probeCtx    context.Context
	probeCancel context.CancelFunc
	rootCtx     context.Context
	rootCancel  context.CancelFunc

	// 未实现端点的探测记录去重：客户端启动期会反复请求同一路径，
	// 只需记下「哪些端点被调用过」，避免每个 404 都写一次库。
	probeMu   sync.Mutex
	probeSeen map[string]struct{}

	// 计划任务调度器：定义存 DB，保存后 Reload 即时生效。
	sched    *scheduler.Scheduler
	watchMu  sync.Mutex
	monitors map[int64]*librarywatch.Monitor

	// nfoGate 是「扫库 / 探测 / 刮削」三者共用的 NFO 写入通道：
	// 三者都会「读全文 → 改局部 → 原子写」同一批 NFO，并发会互相覆盖；
	// 刮削与整库扫描并行时 DeleteMissingSources 还可能误删。nfoOwner 记录当前持有者。
	nfoGate  sync.Mutex
	nfoOwner string

	// 刮削任务状态。scratchCtx 挂在 rootCtx 之下，与探测任务同样的理由：
	// 任务在 goroutine 里跑，不能用请求 context（handler 返回即取消）。
	scrapeTaskMu       sync.RWMutex
	scrapeStatus       scrapeStatus
	scrapeCtx          context.Context
	scrapeCancel       context.CancelFunc
	mediaMu            sync.Mutex
	mediaProcesses     int
	mediaSessions      map[string]*audioSession
	mediaWG            sync.WaitGroup
	mediaSessionsWG    sync.WaitGroup
	subtitleSigningKey [32]byte
	subtitleBytes      *cache.Memory
	discoverSubtitles  func(string) []subtitle.File
}

// scanStatus 管理端可轮询的扫描进度快照。
type scanStatus struct {
	Running      bool   `json:"running"`
	LibraryIndex int    `json:"library_index"`
	Libraries    int    `json:"libraries"`
	LibraryName  string `json:"library_name"`
	Phase        string `json:"phase,omitempty"` // walk=正在遍历目录，process=正在处理候选
	Total        int    `json:"total"`
	Done         int    `json:"done"`
	Current      string `json:"current"`
	Success      int    `json:"success"`
	Pending      int    `json:"pending"`
	Incompatible int    `json:"incompatible"`
	Failed       int    `json:"failed"`
	Added        int    `json:"added"`
	Updated      int    `json:"updated"`
	Skipped      int    `json:"skipped"`
	Deleted      int    `json:"deleted"`
	StartedAt    string `json:"started_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
	Cancelled    bool   `json:"cancelled,omitempty"`
	Error        string `json:"error,omitempty"`
}

// tagEntry 图片 mtime tag 的短缓存条目（neg 表示路径不存在，TTL 更短）。
type tagEntry struct {
	tag     string
	ts      time.Time
	neg     bool
	version uint64
}

// New 强制 Redis 为缓存后端：redis_addr 必填，连接失败拒绝启动。
func New(cfg config.Config) (*App, error) {
	if cfg.RedisAddr == "" {
		return nil, errors.New("缺少 redis_addr：Redis 为必选缓存后端")
	}
	redisCache := cache.NewRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err := redisCache.Ping(); err != nil {
		return nil, fmt.Errorf("redis 连接失败(%s): %w", cfg.RedisAddr, err)
	}
	app, err := newApp(cfg, redisCache)
	if err != nil {
		return nil, err
	}
	app.startLibraryMonitoring()
	return app, nil
}

// newApp 供测试注入任意 cache 实现（如内存版），生产路径不走这里。
func newApp(cfg config.Config, cacheStore cache.Cache) (*App, error) {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	a := &App{db: db, appState: &appState{
		cfg: cfg, cache: cache.NewManaged(cacheStore),
		tags: make(map[string]tagEntry), nfos: newNFOCache(nfoCacheMaxEntries), probeSeen: make(map[string]struct{}),
		imgMeta: cache.NewMemory(imageMetaCacheSize), imgThumb: cache.NewMemory(imageThumbCacheSize),
		thumbSem: make(chan struct{}, maxThumbConcurrency), scrapeSem: make(chan struct{}, maxScrapeImageConcurrency),
	}}
	if _, err := rand.Read(a.subtitleSigningKey[:]); err != nil {
		db.Close()
		return nil, fmt.Errorf("生成字幕签名密钥失败: %w", err)
	}
	a.rootCtx, a.rootCancel = context.WithCancel(context.Background())
	ready := false
	defer func() {
		if !ready {
			a.rootCancel()
		}
	}()
	a.serverID = cfg.ServerID
	if a.serverID == "" {
		// 未显式配置则在本库生成稳定 UUID，避免重启漂移（客户端以 ServerId 识别实例）。
		a.serverID = a.db.Version("server:id")
		if a.serverID == "" {
			buf := make([]byte, 16)
			if _, err := rand.Read(buf); err == nil {
				a.serverID = hex.EncodeToString(buf)
				_ = a.db.SetKV("server:id", a.serverID)
			} else {
				a.serverID = "emby-go"
			}
		}
		// 回写配置文件，方便纳入版本管理/模板；失败不阻断启动（DB 仍是真源）。
		if err := cfg.PersistServerID(a.serverID); err != nil {
			slog.Warn("无法回写 server_id 到配置文件", "error", err)
		}
	}
	if initialized, err := db.HasAdministrator(); err != nil {
		db.Close()
		return nil, err
	} else if initialized {
		if a.adminName, err = db.AdministratorName(); err != nil {
			db.Close()
			return nil, err
		}
	}
	if cfg.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	middlewares := []gin.HandlerFunc{requestLogger()}
	if cfg.Debug {
		middlewares = append(middlewares, requestContentLogger())
	}
	// 恢复中间件在 panic 时打印堆栈，写入程序日志出口（同时保留控制台），
	// 否则崩溃信息只会留在终端、落不进日志文件。
	middlewares = append(middlewares, requestRecoveryTo(logging.AppWriter()))
	r.Use(middlewares...)
	a.router = r
	a.routes()

	// 计划任务调度器挂在常驻 rootCtx 之下：Close 取消 ctx 可中止在跑的计划任务。
	a.sched = scheduler.New(a.rootCtx, db, a.runScheduledTask)
	if err := a.sched.Reload(); err != nil {
		slog.Warn("装载计划任务失败", "error", err)
	}
	a.sched.Start()

	// 启动自检：MetaTube 不可达只告警不阻断启动（与 ffprobe 探测的做法一致）——
	// 元数据服务是外部依赖，它挂了不该让整个媒体库服务起不来。
	go a.checkScrapeBackend()
	ready = true
	return a, nil
}

// checkScrapeBackend 启动时探一次 MetaTube 连通性。
func (a *App) checkScrapeBackend() {
	cfg := a.scrapeConfig()
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(a.rootCtx, 10*time.Second)
	defer cancel()
	if err := metatube.New(cfg.BaseURL, cfg.Token, cfg.Timeout()).Health(ctx); err != nil {
		slog.Warn("启动自检：MetaTube 不可达（不影响启动，刮削任务会失败）", "url", cfg.BaseURL, "error", err)
		return
	}
	slog.Info("启动自检：MetaTube 可达", "url", cfg.BaseURL)
}

// Close 先取消 rootCtx 再停调度与后台任务，最后关库，避免后台 goroutine 继续写 NFO / 访问已关闭的 DB。
//
// 顺序很关键：计划任务与探测/刮削/扫描的 context 都挂在 rootCtx 下，
// 而 scheduler.Stop 会等正在执行的任务结束——先取消才能让在跑的扫描提前收尾，
// 否则一次大库扫描会让停机一直卡到它自然结束。
func (a *App) Close() {
	a.rootCancel()
	a.mediaMu.Lock()
	a.mediaMu.Unlock()
	a.mediaWG.Wait()
	a.mediaSessionsWG.Wait()
	if a.sched != nil {
		a.sched.Stop()
	}
	a.probeTaskMu.Lock()
	if a.probeCancel != nil {
		a.probeCancel()
	}
	a.probeTaskMu.Unlock()
	a.closeLibraryMonitors()
	a.db.Close()
}
func (a *App) Handler() http.Handler { return a.router }

func (a *App) routes() {
	r := a.router

	// Web 管理前端与内部初始化端点（无 /emby 前缀）
	r.GET("/favicon.ico", a.bind((*App).favicon))
	r.HEAD("/favicon.ico", a.bind((*App).favicon))
	r.GET("/web/login.js", a.bind((*App).webAsset))
	r.GET("/web/app.js", a.bind((*App).webAsset))
	r.GET("/web/style.css", a.bind((*App).webAsset))
	r.GET("/web/vendor/artplayer.min.js", a.bind((*App).webAsset))
	registerWebUIRoutes(r, webUIFiles)
	r.GET("/api/auth/status", noStore, a.bind((*App).authStatus))
	r.POST("/api/auth/initialize", noStore, a.bind((*App).initialize))
	r.GET("/api/browse/libraries", a.requireAuth, noStore, a.bind((*App).browseLibraries))
	// Browser image requests use the read-only session cookie.
	r.GET("/api/admin/scrape/image", a.requireAuth, a.requireAdministrator, a.bind((*App).adminScrapeImage))

	admin := r.Group("/api/admin", a.requireAuth, a.adminBoundary, noStore)
	admin.GET("/account", a.bind((*App).account))
	admin.PUT("/account", a.bind((*App).updateAccount))
	admin.GET("/users", a.bind((*App).adminUsers))
	admin.POST("/users", a.bind((*App).createUser))
	admin.PUT("/users/:uid", a.bind((*App).editUser))
	admin.DELETE("/users/:uid", a.bind((*App).deleteUser))
	admin.GET("/libraries", a.bind((*App).adminLibraries))
	admin.POST("/libraries", a.bind((*App).adminAddLibrary))
	admin.PUT("/libraries/:id", a.bind((*App).adminUpdateLibrary))
	admin.GET("/shows", a.bind((*App).adminTelevision))
	admin.DELETE("/libraries/:id", a.bind((*App).adminDeleteLibrary))
	admin.POST("/scan", a.bind((*App).adminScan))
	admin.POST("/tasks/scan", a.bind((*App).adminScan))
	admin.GET("/scan/progress", a.bind((*App).adminScanProgress))
	// 媒体信息探测（与扫库相互独立）。路径用 probe/media 以区别于已有的
	// /probe——后者记录客户端调用过的未实现端点，语义完全不同。
	admin.POST("/probe/media", a.bind((*App).adminProbeMedia))
	admin.GET("/probe/media/progress", a.bind((*App).adminProbeMediaProgress))
	admin.POST("/probe/media/cancel", a.bind((*App).adminProbeMediaCancel))
	admin.POST("/reindex", a.bind((*App).adminReindex))
	admin.GET("/items", a.bind((*App).adminItems))
	admin.GET("/items/:id/detail", a.requireLibraryAccess, a.bind((*App).adminItemDetail))
	admin.DELETE("/items/:id", a.bind((*App).adminDelete))
	admin.GET("/probe", a.bind((*App).adminProbe))
	admin.DELETE("/probe", a.bind((*App).adminClearProbes))
	admin.GET("/apikeys", a.bind((*App).adminAPIKeys))
	admin.POST("/apikeys", a.bind((*App).adminCreateAPIKey))
	admin.DELETE("/apikeys/:key", a.bind((*App).adminDeleteAPIKey))
	admin.GET("/status", a.bind((*App).adminStatus))
	admin.GET("/settings", a.bind((*App).adminSettings))
	admin.GET("/config", a.bind((*App).adminConfig))
	admin.PUT("/config", a.bind((*App).adminSaveConfig))
	admin.GET("/tasks", a.bind((*App).adminTasks))
	// 刮削（R4）：配置 + 批量/头像任务 + 单条预览确认 + 图片代理。
	admin.GET("/scrape/settings", a.bind((*App).adminScrapeSettings))
	admin.PUT("/scrape/settings", a.bind((*App).adminSaveScrapeSettings))
	admin.POST("/scrape/test", a.bind((*App).adminScrapeTest))
	admin.GET("/actors", a.bind((*App).adminActors))
	admin.POST("/actors/import/preview", a.bind((*App).adminActorImportPreview))
	admin.POST("/actors/import", a.bind((*App).adminActorImport))
	admin.GET("/scrape/candidates", a.bind((*App).adminScrapeCandidates))
	admin.POST("/scrape/run", a.bind((*App).adminScrapeRun))
	admin.POST("/scrape/avatars", a.bind((*App).adminScrapeAvatars))
	admin.GET("/scrape/progress", a.bind((*App).adminScrapeProgress))
	admin.POST("/scrape/cancel", a.bind((*App).adminScrapeCancel))
	// 计划任务（cron）：保存/启停后调度器即时重建，无需重启。
	admin.GET("/scheduled", a.bind((*App).adminScheduledTasks))
	admin.POST("/scheduled", a.bind((*App).adminCreateScheduledTask))
	admin.POST("/scheduled/validate", a.bind((*App).adminValidateCron))
	admin.PUT("/scheduled/:id", a.bind((*App).adminUpdateScheduledTask))
	admin.DELETE("/scheduled/:id", a.bind((*App).adminDeleteScheduledTask))
	admin.POST("/scheduled/:id/toggle", a.bind((*App).adminToggleScheduledTask))
	admin.POST("/scheduled/:id/run", a.bind((*App).adminRunScheduledTask))
	admin.POST("/items/manual", a.bind((*App).adminManual))
	admin.PUT("/items/:id", a.bind((*App).adminEdit))
	admin.POST("/items/:id/reread", a.bind((*App).adminReread))
	admin.POST("/items/:id/probe", a.bind((*App).adminProbeMediaItem))
	// 单条手动刮削：预览 → 看 diff → 确认写入（无状态，取消零副作用）。
	admin.GET("/items/:id/scrape/preview", a.bind((*App).adminScrapePreview))
	admin.POST("/items/:id/scrape/inspect", a.bind((*App).adminScrapeInspect))
	admin.POST("/items/:id/scrape", a.bind((*App).adminScrapeConfirm))
	admin.POST("/items/:id/images/:kind", a.bind((*App).adminImage))

	// Emby 兼容 API：裸前缀与 /emby 前缀共用同一注册表。
	// 部分客户端以 /emby 为 base（如 https://host/emby），也有直连根路径的。
	a.registerEmby(r.Group(""))
	a.registerEmby(r.Group("/emby"))

	r.NoRoute(a.noRoute)
}

// embyRoute 一条 Emby 兼容端点；auth 为真时挂 requireAuth。
type embyRoute struct {
	method  string
	path    string
	auth    bool
	handler gin.HandlerFunc
}

// embyRoutes 声明全部 Emby 兼容端点。路径按 PascalCase 写，注册时会额外生成
// 全小写变体：真实 Emby 路由大小写不敏感，实测同一端点会被不同客户端以
// /Users/... 与 /users/... 两种写法请求，而 Gin 路由是大小写敏感的。
func (a *App) embyRoutes() []embyRoute {
	return []embyRoute{
		{"POST", "/Users/AuthenticateByName", false, a.authenticate},
		{"POST", "/Users/:uid/Authenticate", false, a.authenticateByID},
		{"GET", "/Users", true, a.users},
		{"GET", "/Users/Public", false, a.publicUsers},
		{"GET", "/Users/Me", true, a.me},
		{"GET", "/Users/:uid", true, a.userByID},
		{"GET", "/Users/:uid/Items/Latest", true, a.latest},
		{"GET", "/Users/:uid/Suggestions", true, a.suggestions},
		{"GET", "/System/Info/Public", false, a.publicInfo},
		{"GET", "/System/Info", true, a.info},
		{"GET", "/System/Configuration", true, a.configuration},
		{"GET", "/System/Endpoint", true, a.endpoint},
		{"GET", "/System/Ping", false, a.ping},
		{"POST", "/System/Ping", false, a.ping},
		{"POST", "/Sessions/Playing/Ping", true, a.noContent},
		{"POST", "/Sessions/Capabilities", true, a.noContent},
		{"POST", "/Sessions/Capabilities/Full", true, a.noContent},
		{"POST", "/Sessions/Logout", true, a.logout},
		{"POST", "/Users/:uid/Policy", true, a.updateUserPolicy},
		{"GET", "/DisplayPreferences/:pref", true, a.displayPreferences},
		{"POST", "/DisplayPreferences/:pref", true, a.displayPreferences},
		{"GET", "/System/Ext/ServerDomains", true, a.serverDomains},
		{"GET", "/Items/Counts", true, a.counts},
		{"GET", "/Search/Hints", true, a.searchHints},
		{"GET", "/Genres", true, a.genres},
		{"GET", "/Tags", true, a.tagsList},
		{"GET", "/Studios", true, a.studios},
		{"GET", "/Persons", true, a.persons},
		{"GET", "/Genres/:name", true, func(c *gin.Context) { a.namedItemDetail(c, "Genre") }},
		{"GET", "/Persons/:name", true, func(c *gin.Context) { a.namedItemDetail(c, "Person") }},
		{"GET", "/Studios/:name", true, func(c *gin.Context) { a.namedItemDetail(c, "Studio") }},
		{"GET", "/Years", true, a.years},
		{"GET", "/OfficialRatings", true, a.officialRatings},
		{"GET", "/Library/VirtualFolders", true, a.virtualFolders},
		{"GET", "/Library/VirtualFolders/Query", true, a.virtualFoldersQuery},
		{"GET", "/Users/:uid/Items/Resume", true, a.resume},
		{"GET", "/Shows/NextUp", true, a.nextUp},
		{"GET", "/Shows/:id/Seasons", true, a.televisionSeasons},
		{"GET", "/Shows/:id/Episodes", true, a.televisionEpisodes},
		{"GET", "/Users/:uid/Views", true, a.views},
		{"GET", "/Users/:uid/Items", true, a.items},
		{"GET", "/Items", true, a.rootItems},
		{"GET", "/Users/:uid/Items/:id", true, a.item},
		{"GET", "/Items/:id", true, a.item},
		{"GET", "/Items/:id/Images/:kind", true, a.image},
		{"GET", "/Items/:id/Images/:kind/:index", true, a.image},
		{"GET", "/Items/:id/Images", true, a.imageInfo},
		{"GET", "/Items/:id/ThumbnailSet", true, a.thumbnailSet},
		{"GET", "/Items/:id/Similar", true, a.similar},
		{"GET", "/Items/:id/PlaybackInfo", true, a.playback},
		{"POST", "/Items/:id/PlaybackInfo", true, a.playback},
		// 带用户前缀的同一端点：参考客户端（iPlay / openemby_tv / tsukimi）用的是上面那种，
		// 但真实 Emby 两种都提供，部分客户端（如 Yamby）走这种，补上不增加维护成本。
		{"GET", "/Users/:uid/Items/:id/PlaybackInfo", true, a.playback},
		{"POST", "/Users/:uid/Items/:id/PlaybackInfo", true, a.playback},
		{"GET", "/Videos/:id/AdditionalParts", true, a.additionalParts},
		// The handler accepts account authentication or a scoped delivery signature.
		{"GET", "/Videos/:id/:source/Subtitles/:index/Stream.:format", false, a.subtitleStream},
		{"HEAD", "/Videos/:id/:source/Subtitles/:index/Stream.:format", false, a.subtitleStream},
		{"GET", "/Videos/:id/:source/Subtitles/:index/:ticks/Stream.:format", false, a.subtitleStream},
		{"HEAD", "/Videos/:id/:source/Subtitles/:index/:ticks/Stream.:format", false, a.subtitleStream},
		{"POST", "/Videos/:id/Playback", true, a.startAudioPlayback},
		{"GET", "/Videos/:id/master.m3u8", true, a.audioMaster},
		{"DELETE", "/Videos/:id/Playback/:session", true, a.stopAudioPlayback},
		{"PATCH", "/Videos/:id/Playback/:session", true, a.touchAudioPlayback},
		{"GET", "/Episode/:id/IntroSkipperSegments", true, a.emptyList},
		{"GET", "/MediaSegments/:id", true, a.emptyList},
		{"POST", "/Sessions/Playing", true, a.playing},
		{"POST", "/Sessions/Playing/Progress", true, a.playing},
		{"POST", "/Sessions/Playing/Stopped", true, a.playing},
		{"POST", "/Users/:uid/PlayedItems/:id", true, a.played},
		{"DELETE", "/Users/:uid/PlayedItems/:id", true, a.unplayed},
		{"POST", "/Users/:uid/FavoriteItems/:id", true, a.favorite},
		{"DELETE", "/Users/:uid/FavoriteItems/:id", true, a.unfavorite},
		{"POST", "/Users/:uid/FavoriteItems/:id/Delete", true, a.unfavorite},
		{"POST", "/Users/:uid/Items/:id/Rating", true, a.rating},
		{"DELETE", "/Users/:uid/Items/:id/Rating", true, a.unrate},
		{"POST", "/Users/:uid/Items/:id/Rating/Delete", true, a.unrate},
		{"POST", "/Users/:uid/Items/:id/HideFromResume", true, a.hideFromResume},
	}
}

// noStore 给动态响应加禁止缓存头。Emby 客户端会缓存「无 Cache-Control」的 JSON，
// PlaybackInfo / Items 这类响应一旦被缓存，媒体源与播放进度就不再刷新；
// 视频流的 302 与代理字节流同理，必须禁止客户端与中间层缓存。
func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
}

// registerEmby 注册全部 Emby 兼容路由，每条同时挂 PascalCase 与全小写变体。
func (a *App) registerEmby(g *gin.RouterGroup) {
	for index, route := range a.embyRoutes() {
		handlers := []gin.HandlerFunc{normalizeEmbyQuery}
		// 图片端点除外：serveImage 自己带 public/max-age 与 ETag，挂 noStore 会把
		// 海报墙的缓存打掉（每张图全量重下）；其余 API 一律禁止缓存。
		// imageInfo（/Items/:id/Images 结尾、无斜杠）不在排除之列，走 noStore。
		if !strings.Contains(route.path, "/Images/") {
			handlers = append(handlers, noStore)
		}
		if route.auth {
			handlers = append(handlers, a.requireAuth)
		}
		if !strings.Contains(route.path, "/Authenticate") && !strings.Contains(route.path, "/Subtitles/") && route.path != "/Users/:uid/Policy" && route.path != "/Users/:uid" {
			handlers = append(handlers, a.requireLibraryAccess)
		}
		handlers = append(handlers, func(c *gin.Context) { a.forRequest(c).embyRoutes()[index].handler(c) })
		g.Handle(route.method, route.path, handlers...)
		if lower := strings.ToLower(route.path); lower != route.path {
			g.Handle(route.method, lower, handlers...)
		}
	}
	streams := g.Group("", a.requireAuth, a.requireLibraryAccess)
	registerStreamRoutes(streams, a.bind((*App).stream))
	registerProxyRoutes(streams, a.bind((*App).proxyStream))
	registerTrailerRoutes(streams, a.bind((*App).trailer))
	for _, base := range []string{"/Videos/:id/hls/:session/:file", "/videos/:id/hls/:session/:file"} {
		g.GET(base, noStore, a.bind((*App).hlsResource))
	}
}

// registerProxyRoutes 网页播放器代理端点（透传 Range），同样注册大小写与可选扩展名。
func registerProxyRoutes(g *gin.RouterGroup, h gin.HandlerFunc) {
	for _, base := range []string{"/Videos/:id/proxy", "/videos/:id/proxy"} {
		g.GET(base, noStore, h)
		g.HEAD(base, noStore, h)
		g.GET(base+".:ext", noStore, h)
		g.HEAD(base+".:ext", noStore, h)
	}
}

// registerTrailerRoutes 详情页预告片代理端点，同样注册大小写与可选扩展名。
func registerTrailerRoutes(g *gin.RouterGroup, h gin.HandlerFunc) {
	for _, base := range []string{"/Videos/:id/trailer", "/videos/:id/trailer"} {
		g.GET(base, noStore, h)
		g.HEAD(base, noStore, h)
		g.GET(base+".:ext", noStore, h)
		g.HEAD(base+".:ext", noStore, h)
	}
}

// registerStreamRoutes 同时注册 /Videos 与 /videos 大小写，吞掉可选扩展名。
func registerStreamRoutes(g *gin.RouterGroup, h gin.HandlerFunc) {
	for _, base := range []string{"/Videos/:id/stream", "/videos/:id/stream"} {
		g.GET(base, noStore, h)
		g.HEAD(base, noStore, h)
		g.GET(base+".:ext", noStore, h)
		g.HEAD(base+".:ext", noStore, h)
	}
}

func (a *App) noRoute(c *gin.Context) {
	// 客户端会探测式地反复请求未实现端点；404 一旦被浏览器/中间层缓存，
	// 后续补上实现客户端也拿不到——明确禁止缓存。
	c.Header("Cache-Control", "no-store")
	if !strings.HasPrefix(c.Request.URL.Path, "/api/") {
		key := c.Request.Method + " " + c.Request.URL.Path
		a.probeMu.Lock()
		_, seen := a.probeSeen[key]
		if !seen {
			a.probeSeen[key] = struct{}{}
		}
		a.probeMu.Unlock()
		// 只记首次：客户端探测期同一端点会反复命中，逐次写库会拖慢 404 响应。
		if !seen {
			body, _ := io.ReadAll(io.LimitReader(c.Request.Body, 4096))
			_ = a.db.Probe(c.Request.Method, c.Request.URL.Path, c.Request.URL.RawQuery, string(body))
		}
	}
	c.JSON(404, gin.H{"error": "not found"})
}
