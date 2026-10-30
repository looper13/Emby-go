package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func r18ItemID(library int64, video string) string {
	return "r18-" + strconv.FormatInt(library, 10) + "-" + video
}

func parseR18ItemID(raw string) (int64, string, bool) {
	if !strings.HasPrefix(raw, "r18-") {
		return 0, "", false
	}
	lib, video, found := strings.Cut(strings.TrimPrefix(raw, "r18-"), "-")
	id, err := strconv.ParseInt(lib, 10, 64)
	if !found || err != nil || id <= 0 || strconv.FormatInt(id, 10) != lib {
		return 0, "", false
	}
	if _, _, err := r18.DecodeID(video); err != nil {
		return 0, "", false
	}
	return id, video, true
}

func (a *App) r18CompatLibrary(c *gin.Context, id int64) (store.Library, bool) {
	if !a.db.CanAccessLibrary(id) {
		c.JSON(404, gin.H{"error": "item not found"})
		return store.Library{}, false
	}
	l, err := a.db.Library(id)
	if err != nil || l.SourceType != store.SourceR18 {
		c.JSON(404, gin.H{"error": "R18 library not found"})
		return l, false
	}
	c.Set("r18LibraryID", id)
	return l, true
}

func (a *App) r18CompatVideo(c *gin.Context) (store.Library, r18.Video, bool) {
	lib, id, ok := parseR18ItemID(c.Param("id"))
	if !ok {
		c.JSON(404, gin.H{"error": "item not found"})
		return store.Library{}, r18.Video{}, false
	}
	l, ok := a.r18CompatLibrary(c, lib)
	if !ok {
		return l, r18.Video{}, false
	}
	client, err := a.r18Client(l)
	if err != nil {
		a.r18Failure(c, "连接", err)
		return l, r18.Video{}, false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	v, err := client.Get(ctx, id)
	if err != nil {
		a.r18Failure(c, "详情查询", err)
		return l, v, false
	}
	if err = a.prepareR18LocalSources(c, []r18.Video{v}); err != nil {
		a.r18Failure(c, "本地播放源查询", err)
		return l, v, false
	}
	a.cacheR18Video(l, v, true)
	return l, v, true
}

func r18ArtworkTag(l store.Library, raw, kind string) string {
	if kind == "poster" {
		kind = "poster-v2"
	}
	sum := sha256.Sum256([]byte(strconv.FormatInt(l.Revision, 10) + ":" + kind + ":" + raw))
	return hex.EncodeToString(sum[:16])
}

func (a *App) r18ItemDTO(c *gin.Context, l store.Library, v r18.Video) gin.H {
	id := r18ItemID(l.ID, v.ID)
	item := gin.H{
		"Id": id, "Name": v.Title, "SortName": v.Title, "OriginalTitle": v.OriginalTitle,
		"Type": "Movie", "MediaType": "Video", "IsFolder": false,
		"ParentId": externalLibraryID(l.ID), "ServerId": a.serverID,
		"CanDelete": false, "CanDownload": false, "SupportsSync": false,
		"IsPlaceHolder": true, "PlayAccess": "None", "LocationType": "Virtual",
		"MediaSources": []gin.H{}, "MediaStreams": []gin.H{}, "Chapters": []gin.H{},
		"ImageTags": gin.H{}, "BackdropImageTags": []string{}, "PrimaryImageAspectRatio": 2.0 / 3.0,
		"UserData":    gin.H{"Key": id, "IsFavorite": false, "Played": false, "PlayCount": 0, "PlaybackPositionTicks": 0},
		"ProviderIds": gin.H{"JavDB": v.ContentID, "DVD": v.Number},
		"Overview":    v.Number + " · " + l.Name + "\nJavDB 在线资料条目。在线磁力请在网页详情查看，管理员可通过 115 准备播放源。",
		"People":      []gin.H{}, "Genres": []string{}, "Tags": []string{}, "Studios": []gin.H{},
	}
	if len(v.Tags) > 0 {
		item["Tags"] = v.Tags
	}
	if date, err := time.Parse("2006-01-02", v.ReleaseDate); err == nil {
		item["PremiereDate"], item["ProductionYear"] = normalizeDate(v.ReleaseDate), date.Year()
	}
	if v.Duration > 0 && int64(v.Duration) <= math.MaxInt64/600000000 {
		// API 时长单位为分钟，Emby 使用百纳秒。
		item["RunTimeTicks"] = int64(v.Duration) * 600000000
	}
	if v.Score > 0 && !math.IsNaN(v.Score) && !math.IsInf(v.Score, 0) {
		item["CommunityRating"] = v.Score
	}
	if !strings.EqualFold(c.Query("EnableImages"), "false") {
		if v.CoverURL != "" {
			item["ImageTags"] = gin.H{"Primary": r18ArtworkTag(l, v.CoverURL, "poster"), "Thumb": r18ArtworkTag(l, v.CoverURL, "cover")}
		}
		backdrops := []string{}
		for _, raw := range v.Images {
			backdrops = append(backdrops, r18ArtworkTag(l, raw, "backdrop"))
		}
		item["BackdropImageTags"] = backdrops
	}
	// 仅缓存私有元数据，客户端接收图片标记，不接收上游地址。
	a.r18Presentation(l, v)
	matches, localErr := a.r18LocalMatches(c, v)
	sources := a.r18LocalMediaSources(c, matches)
	if len(sources) == 0 && localErr == nil {
		sources = a.r18ReadySources(c, l, v.ID)
	}
	if len(sources) > 0 {
		item["IsPlaceHolder"] = false
		item["PlayAccess"] = "Full"
		item["MediaSources"] = sources
		item["LocationType"] = "Remote"
		item["MediaStreams"] = sources[0]["MediaStreams"]
		if len(matches) > 0 {
			item["Overview"] = v.Number + " · " + l.Name + "\n本地 STRM 已存在，直接沿用目录媒体库播放，不需要 115 下载。"
		} else {
			item["Overview"] = v.Number + " · " + l.Name + "\n115 已就绪播放源；下载和版本选择请在网页详情中管理。"
		}
	}
	return item
}

func (a *App) r18CollectionDTO(l store.Library) gin.H {
	return gin.H{
		"Id": externalLibraryID(l.ID), "Name": l.Name, "Type": "CollectionFolder",
		"CollectionType": "movies", "ServerId": a.serverID, "IsFolder": true,
		"DisplayPreferencesId": externalLibraryID(l.ID), "ImageTags": gin.H{}, "BackdropImageTags": []string{},
		"UserData":  gin.H{"IsFavorite": false, "Played": false, "PlaybackPositionTicks": 0},
		"CanDelete": false, "CanDownload": false, "SupportsSync": false,
	}
}

func r18CatalogQuery(c *gin.Context) (r18.CatalogQuery, error) {
	search := strings.TrimSpace(c.Query("SearchTerm"))
	defaultSort := "PremiereDate"
	if search != "" {
		defaultSort = "Relevance"
	}
	order := c.DefaultQuery("SortOrder", "Descending")
	if !strings.EqualFold(order, "Ascending") && !strings.EqualFold(order, "Descending") {
		return r18.CatalogQuery{}, errors.New("无效的 JavDB 排序方向")
	}
	q := r18.CatalogQuery{Search: search,
		Sort: firstSortKey(c.DefaultQuery("SortBy", defaultSort)), Descending: strings.EqualFold(order, "Descending"),
		NameStartsWith: c.Query("NameStartsWith"), NameStartsWithOrGreater: c.Query("NameStartsWithOrGreater"), NameLessThan: c.Query("NameLessThan")}
	var err error
	if q.Start, err = strconv.Atoi(c.DefaultQuery("StartIndex", "0")); err != nil || q.Start < 0 || q.Start >= 9999*50 {
		return q, errors.New("无效的 JavDB 列表偏移")
	}
	if q.Limit, err = strconv.Atoi(c.DefaultQuery("Limit", "100")); err != nil || q.Limit < 1 || q.Limit > 1000 {
		return q, errors.New("无效的 JavDB 每页条数")
	}
	for _, raw := range splitQueryTypes(c.Query("Years")) {
		year, err := strconv.Atoi(raw)
		if err != nil || year < 1 || year > 9999 {
			return q, errors.New("invalid year")
		}
		q.Years = append(q.Years, year)
	}
	if len(q.Years) > 100 {
		return q, errors.New("too many years")
	}
	for _, value := range []string{q.Search, q.NameStartsWith, q.NameStartsWithOrGreater, q.NameLessThan} {
		if len([]rune(value)) > 120 {
			return q, errors.New("search is too long")
		}
	}
	return q, nil
}

func r18CatalogSupported(c *gin.Context) bool {
	if !catalogIncludesMovies(c) || strings.EqualFold(c.Query("IsFolder"), "true") {
		return false
	}
	if favorite := favoriteFilter(c); favorite != nil && *favorite {
		return false
	}
	for _, filter := range splitQueryTypes(c.Query("Filters")) {
		if !strings.EqualFold(filter, "IsUnplayed") && !strings.EqualFold(filter, "IsNotFolder") {
			return false
		}
	}
	// 上游尚未提供这些用户状态和目录属性。
	for _, key := range []string{"Genres", "GenreIds", "Tags", "TagIds", "Studios", "StudioIds", "Person", "PersonIds", "OfficialRatings"} {
		if c.Query(key) != "" {
			return false
		}
	}
	return true
}

const (
	r18PlayableBatchSize  = 50
	r18PlayableMaxBatches = 40
)

var errR18PlayableScanLimit = errors.New("JavDB 播放状态筛选超过单次扫描或请求时间上限，请缩小关键词或年份范围后重试")

func r18PlaceholderFilter(c *gin.Context) (*bool, error) {
	values, present := c.Request.URL.Query()["IsPlaceHolder"]
	if !present {
		return nil, nil
	}
	if len(values) != 1 || !strings.EqualFold(values[0], "true") && !strings.EqualFold(values[0], "false") {
		return nil, errors.New("IsPlaceHolder 必须为 true 或 false")
	}
	value := strings.EqualFold(values[0], "true")
	return &value, nil
}

func r18PlaceholderMatches(item gin.H, filter *bool) bool {
	// 普通本地影片未必显式设置此字段，按 Emby 默认值 false 处理。
	return filter == nil || (item["IsPlaceHolder"] == true) == *filter
}

func (a *App) r18FilteredCatalog(c *gin.Context, ctx context.Context, client r18Source, l store.Library, q r18.CatalogQuery, placeholder *bool) ([]gin.H, int, error) {
	items := make([]gin.H, 0, min(q.Limit, r18PlayableBatchSize))
	total := 0
	seen := map[string]bool{}
	// 偏移属于过滤后的全局顺序；从上游起点扫描，找到额外匹配项才声明下一页。
	for batch := 0; batch < r18PlayableMaxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		query := q
		query.Start, query.Limit = batch*r18PlayableBatchSize, r18PlayableBatchSize
		page, err := client.CatalogPage(ctx, query)
		if err != nil {
			return nil, 0, err
		}
		if len(page.Items) > r18PlayableBatchSize || page.HasMore && len(page.Items) != r18PlayableBatchSize {
			return nil, 0, errors.New("JavDB 上游分页无效，无法保证播放筛选的偏移一致性")
		}
		for _, video := range page.Items {
			if _, _, err := r18.DecodeID(video.ID); err != nil || seen[video.ID] {
				return nil, 0, errors.New("JavDB 上游条目无效或重复，无法保证播放筛选的偏移一致性")
			}
			seen[video.ID] = true
		}
		if err := a.prepareR18LocalSources(c, page.Items); err != nil {
			return nil, 0, err
		}
		for _, video := range page.Items {
			if err := ctx.Err(); err != nil {
				return nil, 0, err
			}
			item := a.r18ItemDTO(c, l, video)
			if !r18PlaceholderMatches(item, placeholder) {
				continue
			}
			total++
			if total <= q.Start {
				continue
			}
			if len(items) == q.Limit {
				return items, total, nil
			}
			items = append(items, item)
		}
		if !page.HasMore {
			return items, total, nil
		}
	}
	// 无法确认真实末尾时明确失败，不把不足一页或空页冒充过滤后的结尾。
	return nil, 0, errR18PlayableScanLimit
}

func r18AppliedSort(q r18.CatalogQuery) string {
	if strings.TrimSpace(q.Search) != "" {
		return "Relevance"
	}
	switch strings.ToLower(q.Sort) {
	case "", "release", "premieredate", "productionyear", "datecreated":
		return "PremiereDate"
	case "score", "communityrating":
		return "CommunityRating"
	default:
		return q.Sort
	}
}

func (a *App) r18CatalogResponse(c *gin.Context, l store.Library, latest bool) {
	q, err := r18CatalogQuery(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	placeholder, err := r18PlaceholderFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !r18CatalogSupported(c) {
		if latest {
			c.JSON(200, []gin.H{})
		} else {
			c.JSON(200, gin.H{"Items": []gin.H{}, "TotalRecordCount": 0, "StartIndex": q.Start})
		}
		return
	}
	if latest {
		q.Start, q.Sort, q.Descending = 0, "PremiereDate", true
		q.Limit, _ = strconv.Atoi(c.DefaultQuery("Limit", "20"))
		if q.Limit < 1 || q.Limit > 100 {
			q.Limit = 20
		}
	}
	client, err := a.r18Client(l)
	if err != nil {
		a.r18Failure(c, "连接", err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if placeholder != nil {
		// 本地批量匹配的五秒子预算也必须受同一个二十秒请求预算约束。
		c.Request = c.Request.WithContext(ctx)
		items, total, err := a.r18FilteredCatalog(c, ctx, client, l, q, placeholder)
		if err != nil {
			if errors.Is(err, r18.ErrUnsupportedCatalogQuery) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else if errors.Is(err, errR18PlayableScanLimit) || errors.Is(err, context.DeadlineExceeded) {
				c.JSON(http.StatusBadGateway, gin.H{"error": errR18PlayableScanLimit.Error()})
			} else {
				a.r18Failure(c, "播放状态筛选", err)
			}
			return
		}
		if latest {
			c.JSON(http.StatusOK, items)
		} else {
			c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": total, "StartIndex": q.Start,
				"TotalRecordCountIsEstimated": true, "TotalRecordCountIsKnown": false, "AppliedSortBy": r18AppliedSort(q)})
		}
		return
	}
	page, err := client.CatalogPage(ctx, q)
	if err != nil {
		if errors.Is(err, r18.ErrUnsupportedCatalogQuery) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			a.r18Failure(c, "在线目录查询", err)
		}
		return
	}
	if err = a.prepareR18LocalSources(c, page.Items); err != nil {
		a.r18Failure(c, "本地播放源查询", err)
		return
	}
	items := []gin.H{}
	for _, video := range page.Items {
		items = append(items, a.r18ItemDTO(c, l, video))
	}
	if latest {
		c.JSON(200, items)
		return
	}
	// Emby 需要数量判断翻页；这里只提供当前可访问条数下界，不请求全库计数。
	total := 0
	if len(items) > 0 || page.HasMore {
		total = q.Start + len(items)
	}
	if page.HasMore {
		total++
	}
	c.JSON(200, gin.H{"Items": items, "TotalRecordCount": total, "StartIndex": q.Start,
		"TotalRecordCountIsEstimated": true, "TotalRecordCountIsKnown": false, "AppliedSortBy": r18AppliedSort(q)})
}

// 先分派外部资料库范围，避免本地索引先分页。
func (a *App) r18CompatItems(c *gin.Context) bool {
	parentRaw := strings.TrimSpace(c.Query("ParentId"))
	parent, _ := parseParentScope(parentRaw)
	ids := splitQueryTypes(c.Query("Ids"))
	if parent > 0 && len(ids) == 0 {
		lib := parent
		if l, err := a.db.Library(lib); err == nil && l.SourceType == store.SourceR18 {
			c.Set("r18LibraryID", lib)
			a.r18CatalogResponse(c, l, false)
			return true
		}
	}
	if len(ids) == 0 {
		if c.Query("ParentId") == "" || c.Query("ParentId") == "0" {
			include := splitQueryTypes(c.Query("IncludeItemTypes"))
			if len(include) == 1 && strings.EqualFold(include[0], "CollectionFolder") {
				libs, err := a.db.UserLibraries()
				if err != nil {
					c.JSON(500, gin.H{"error": "library lookup failed"})
					return true
				}
				items := []gin.H{}
				for _, l := range libs {
					item := a.collectionFolderDTO(l)
					if favoriteCandidateMatches(c, item) {
						items = append(items, item)
					}
				}
				sortMediaItems(c, items)
				writeMediaItems(c, items, len(items))
				return true
			}
		}
		return false
	}
	hasR18 := false
	for _, id := range ids {
		hasR18 = hasR18 || strings.HasPrefix(id, "r18-")
	}
	if !hasR18 {
		return false
	}
	if parentRaw != "" && parentRaw != "0" && parent <= 0 {
		a.emptyItems(c)
		return true
	}
	if len(ids) > 1000 {
		c.JSON(400, gin.H{"error": "too many item IDs"})
		return true
	}
	placeholder, err := r18PlaceholderFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return true
	}
	items := []gin.H{}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if placeholder != nil {
		c.Request = c.Request.WithContext(ctx)
	}
	seen := map[string]bool{}
	for _, raw := range ids {
		if placeholder != nil && ctx.Err() != nil {
			a.r18Failure(c, "播放状态筛选", ctx.Err())
			return true
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		if !strings.HasPrefix(raw, "r18-") {
			if m, err := a.resolveUserMedia(raw); err == nil && (parent == 0 || m.LibraryID == parent) {
				d, _ := a.db.Data(m.ID)
				item := a.embyItem(m, d)
				a.applyItemFields(item, m, requestedFields(c), c)
				if r18PlaceholderMatches(item, placeholder) && favoriteCandidateMatches(c, item) {
					items = append(items, item)
				}
			}
			continue
		}
		lib, id, ok := parseR18ItemID(raw)
		if !ok || !r18CatalogSupported(c) || (parent > 0 && lib != parent) || !a.db.CanAccessLibrary(lib) {
			continue
		}
		l, err := a.db.Library(lib)
		if err != nil || l.SourceType != store.SourceR18 {
			continue
		}
		c.Set("r18LibraryID", lib)
		client, err := a.r18Client(l)
		if err != nil {
			a.r18Failure(c, "连接", err)
			return true
		}
		v, err := client.Get(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			a.r18Failure(c, "详情查询", err)
			return true
		}
		if err = a.prepareR18LocalSources(c, []r18.Video{v}); err != nil {
			a.r18Failure(c, "本地播放源查询", err)
			return true
		}
		a.cacheR18Video(l, v, true)
		item := a.r18ItemDTO(c, l, v)
		if r18PlaceholderMatches(item, placeholder) && favoriteCandidateMatches(c, item) {
			items = append(items, item)
		}
	}
	if c.Query("SortBy") != "" {
		sortFavoriteItems(c, items)
	}
	writeMediaItems(c, items, len(items))
	return true
}

func (a *App) r18CompatItem(c *gin.Context) {
	l, v, ok := a.r18CompatVideo(c)
	if ok {
		c.JSON(200, a.r18ItemDTO(c, l, v))
	}
}

func (a *App) r18CompatPlayback(c *gin.Context) {
	if l, v, ok := a.r18CompatVideo(c); ok {
		matches, err := a.r18LocalMatches(c, v)
		if err != nil {
			a.r18Failure(c, "本地播放源查询", err)
			return
		}
		sources := a.r18LocalMediaSources(c, matches)
		requested := c.Query("MediaSourceId")
		if c.Request.Method == "POST" {
			var body struct {
				Source string `json:"MediaSourceId"`
			}
			if c.Request.ContentLength > 0 && c.ShouldBindJSON(&body) != nil {
				c.Status(400)
				return
			}
			if body.Source != "" {
				requested = body.Source
			}
		}
		// 默认只提供本地版本；显式选择 115 时才读取该来源的就绪绑定。
		if len(sources) == 0 || strings.HasPrefix(requested, "115-") {
			sources = append(sources, a.r18ReadySources(c, l, v.ID)...)
		}
		if requested != "" {
			selected := []gin.H{}
			for _, s := range sources {
				if s["Id"] == requested {
					selected = append(selected, s)
				}
			}
			sources = selected
		}
		if len(sources) == 0 {
			c.JSON(200, gin.H{"MediaSources": sources, "ErrorCode": "NoCompatibleStream", "Message": "当前影片没有有效的本地 STRM 或已就绪 115 播放源，请刷新并选择其它播放方式"})
			return
		}
		c.JSON(200, gin.H{"MediaSources": sources, "PlaySessionId": randomSessionID()})
	}
}

func (a *App) r18CompatImageInfo(c *gin.Context) {
	l, v, ok := a.r18CompatVideo(c)
	if !ok {
		return
	}
	images := []gin.H{}
	if v.CoverURL != "" {
		images = append(images, gin.H{"ImageType": "Primary", "ImageIndex": 0, "ImageTag": r18ArtworkTag(l, v.CoverURL, "poster")}, gin.H{"ImageType": "Thumb", "ImageIndex": 0, "ImageTag": r18ArtworkTag(l, v.CoverURL, "cover")})
	}
	for i, raw := range v.Images {
		images = append(images, gin.H{"ImageType": "Backdrop", "ImageIndex": i, "ImageTag": r18ArtworkTag(l, raw, "backdrop")})
	}
	c.JSON(200, images)
}

func (a *App) r18CompatImage(c *gin.Context) {
	// 本地图片继续兼容匿名访问，JavDB 图片必须鉴权。
	if _, ok := c.Get("accountUser"); !ok {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	lib, id, ok := parseR18ItemID(c.Param("id"))
	if !ok {
		c.Status(404)
		return
	}
	l, ok := a.r18CompatLibrary(c, lib)
	if !ok {
		return
	}
	index := 0
	if raw := c.Param("index"); raw != "" {
		var err error
		index, err = strconv.Atoi(raw)
		if err != nil || index < 0 || strconv.Itoa(index) != raw {
			c.Status(404)
			return
		}
	}
	kind, transform := "cover", "resize"
	switch strings.ToLower(c.Param("kind")) {
	case "primary", "poster":
		transform = "poster"
		if index != 0 {
			c.Status(404)
			return
		}
	case "thumb":
		if index != 0 {
			c.Status(404)
			return
		}
	case "backdrop", "fanart":
		if index >= r18.MaxImages {
			c.Status(404)
			return
		}
		kind = strconv.Itoa(index)
	default:
		c.Status(404)
		return
	}
	a.serveR18SourceImage(c, l, id, kind, transform)
}
