package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
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
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	v, err := client.Get(ctx, id)
	if err != nil {
		a.r18Failure(c, "详情查询", err)
		return l, v, false
	}
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
		"ProviderIds": gin.H{"R18": v.ContentID, "DVD": v.Number},
		"Overview":    v.Number + " · " + l.Name + "\n只读资料条目，播放接入待实现（TODO）。磁力资源请在网页详情中查看。",
		"People":      []gin.H{}, "Genres": []string{}, "Tags": []string{}, "Studios": []gin.H{},
	}
	if date, err := time.Parse("2006-01-02", v.ReleaseDate); err == nil {
		item["PremiereDate"], item["ProductionYear"] = normalizeDate(v.ReleaseDate), date.Year()
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
	// Seed only private source metadata; DTOs contain tags, never upstream URLs.
	a.r18Presentation(l, v)
	if sources := a.r18ReadySources(c, l, v.ID); len(sources) > 0 {
		item["IsPlaceHolder"] = false
		item["PlayAccess"] = "Full"
		item["MediaSources"] = sources
		item["LocationType"] = "Remote"
		item["MediaStreams"] = sources[0]["MediaStreams"]
		item["Overview"] = v.Number + " · " + l.Name + "\n115 已就绪播放源；下载和版本选择请在网页详情中管理。"
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
	start, limit := mediaPageWindow(c)
	q := r18.CatalogQuery{Start: start, Limit: limit, Search: c.Query("SearchTerm"),
		Sort: firstSortKey(c.DefaultQuery("SortBy", "PremiereDate")), Descending: strings.EqualFold(c.DefaultQuery("SortOrder", "Descending"), "Descending"),
		NameStartsWith: c.Query("NameStartsWith"), NameStartsWithOrGreater: c.Query("NameStartsWithOrGreater"), NameLessThan: c.Query("NameLessThan")}
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
	if !catalogIncludesMovies(c) || strings.EqualFold(c.Query("IsFolder"), "true") || strings.EqualFold(c.Query("IsPlaceHolder"), "false") {
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
	// These properties/user states are not provided by the read-only source.
	for _, key := range []string{"Genres", "GenreIds", "Tags", "TagIds", "Studios", "StudioIds", "Person", "PersonIds", "OfficialRatings"} {
		if c.Query(key) != "" {
			return false
		}
	}
	return true
}

func (a *App) r18CatalogCount(ctx context.Context, l store.Library, client r18Source, q r18.CatalogQuery) (int64, error) {
	q.Start, q.Limit, q.Sort, q.Descending = 0, 0, "", false
	key := responseKey("r18count", l.ID, l.Revision, q)
	raw, err := a.cache.Load(ctx, key, 30*time.Second, func() ([]byte, error) {
		total, err := client.CatalogCount(ctx, q)
		return []byte(strconv.FormatInt(total, 10)), err
	})
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(raw), 10, 64)
}

func (a *App) r18CatalogResponse(c *gin.Context, l store.Library, latest bool) {
	q, err := r18CatalogQuery(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
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
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var total int64
	if !latest {
		total, err = a.r18CatalogCount(ctx, l, client, q)
		if err != nil {
			a.r18Failure(c, "目录计数", err)
			return
		}
	}
	items := []gin.H{}
	if latest || int64(q.Start) < total {
		videos, err := client.CatalogList(ctx, q)
		if err != nil {
			a.r18Failure(c, "目录查询", err)
			return
		}
		for _, v := range videos {
			items = append(items, a.r18ItemDTO(c, l, v))
		}
	}
	if latest {
		c.JSON(200, items)
	} else {
		c.JSON(200, gin.H{"Items": items, "TotalRecordCount": total, "StartIndex": q.Start})
	}
}

// Dispatch source scopes before SQLite/TV/favorite handlers paginate local data.
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
	items := []gin.H{}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	seen := map[string]bool{}
	for _, raw := range ids {
		if seen[raw] {
			continue
		}
		seen[raw] = true
		if !strings.HasPrefix(raw, "r18-") {
			if m, err := a.resolveUserMedia(raw); err == nil && (parent == 0 || m.LibraryID == parent) {
				d, _ := a.db.Data(m.ID)
				item := a.embyItem(m, d)
				a.applyItemFields(item, m, requestedFields(c), c)
				if favoriteCandidateMatches(c, item) {
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
		item := a.r18ItemDTO(c, l, v)
		if favoriteCandidateMatches(c, item) {
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
		sources := a.r18ReadySources(c, l, v.ID)
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
			c.JSON(200, gin.H{"MediaSources": sources, "ErrorCode": "NoCompatibleStream", "Message": "R18 暂无就绪的 115 播放源，请管理员先完成离线下载和文件选择"})
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
	// Keep existing anonymous local artwork compatibility, but R18 stays private.
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
		if err != nil || index < 0 {
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
		if index > 1 {
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
