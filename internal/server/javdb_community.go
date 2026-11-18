package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

// Optional capability keeps ordinary R18 catalog sources and their consumers
// independent of the additional public community endpoints.
type javdbCommunitySource interface {
	MatchNumber(context.Context, string) (r18.Video, error)
	Reviews(context.Context, string, int, int) (r18.ReviewPage, error)
	RelatedLists(context.Context, string, int, int) (r18.RelatedListPage, error)
	ListMovies(context.Context, string, int, int) (r18.Page, error)
}

var javdbListID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,120}$`)

type javdbLocalMovie struct {
	ID     int64  `json:"id"`
	Number string `json:"number"`
	Title  string `json:"title"`
}

type javdbListMovie struct {
	ID          string            `json:"id"`
	Number      string            `json:"number"`
	Title       string            `json:"title"`
	CoverURL    string            `json:"cover_url"`
	HasLocal    bool              `json:"has_local"`
	LocalMovies []javdbLocalMovie `json:"local_movies"`
}

type javdbListMoviePage struct {
	javdbCommunityIdentity
	Items          []javdbListMovie `json:"items"`
	ListID         string           `json:"list_id"`
	LocalLibraryID int64            `json:"local_library_id"`
	NextPage       int              `json:"next_page"`
	HasMore        bool             `json:"has_more"`
}

type javdbCommunityIdentity struct {
	Number    string `json:"number"`
	VideoID   string `json:"video_id"`
	LibraryID int64  `json:"library_id"`
}

func (a *App) registerJavdbCommunityRoutes(browse *gin.RouterGroup) {
	browse.GET("/items/:id/match", a.bind((*App).javdbItemMatch))
	browse.GET("/items/:id/reviews", a.bind((*App).javdbItemReviews))
	browse.GET("/items/:id/related-lists", a.bind((*App).javdbItemRelatedLists))
	browse.GET("/items/:id/related-lists/:list/movies", a.bind((*App).javdbItemListMovies))
}

func (a *App) javdbCommunityItem(c *gin.Context) (store.Movie, store.Library, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 || !a.db.CanAccessMovie(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "影片不存在或无权访问"})
		return store.Movie{}, store.Library{}, false
	}
	movie, err := a.db.Movie(id)
	if err != nil || !movie.IsVisible() {
		c.JSON(http.StatusNotFound, gin.H{"error": "影片不存在或无权访问"})
		return store.Movie{}, store.Library{}, false
	}
	local, err := a.db.Library(movie.LibraryID)
	if err != nil || !local.IsDirectory() {
		c.JSON(http.StatusNotFound, gin.H{"error": "影片不属于可访问的目录媒体库"})
		return store.Movie{}, store.Library{}, false
	}
	id, err = strconv.ParseInt(c.Query("library_id"), 10, 64)
	if err != nil || id <= 0 || !a.db.CanAccessLibrary(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "JavDB 资料库不存在或无权访问"})
		return store.Movie{}, store.Library{}, false
	}
	library, err := a.db.Library(id)
	if err != nil || library.SourceType != store.SourceR18 {
		c.JSON(http.StatusNotFound, gin.H{"error": "JavDB 资料库不存在或无权访问"})
		return store.Movie{}, store.Library{}, false
	}
	if _, err := r18FreshRequested(c); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return store.Movie{}, store.Library{}, false
	}
	c.Set("r18LibraryID", library.ID)
	return movie, library, true
}

func (a *App) javdbCommunityClient(c *gin.Context, library store.Library) (javdbCommunitySource, bool) {
	client, err := a.r18Client(library)
	if err != nil {
		a.r18Failure(c, "评论与清单连接", err)
		return nil, false
	}
	source, ok := client.(javdbCommunitySource)
	if !ok {
		c.JSON(http.StatusBadGateway, gin.H{"error": "当前 JavDB 来源不支持评论和相关清单"})
		return nil, false
	}
	return source, true
}

func (a *App) javdbCommunityFailure(c *gin.Context, stage string, err error) {
	if errors.Is(err, r18.ErrAmbiguousNumber) || errors.Is(err, r18.ErrNumberSearchLimit) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	a.r18Failure(c, stage, err)
}

func (a *App) javdbItemMatch(c *gin.Context) {
	movie, library, ok := a.javdbCommunityItem(c)
	if !ok {
		return
	}
	number := strings.TrimSpace(movie.Number)
	result := gin.H{"status": "not_found", "number": number, "library_id": library.ID, "library_name": library.Name}
	if number == "" || movie.Television != nil {
		result["status"] = "no_number"
		c.JSON(http.StatusOK, result)
		return
	}
	source, ok := a.javdbCommunityClient(c, library)
	if !ok {
		return
	}
	ctx, cancel := r18RequestContext(c)
	defer cancel()
	video, err := source.MatchNumber(ctx, number)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusOK, result)
		return
	}
	if err != nil {
		a.javdbCommunityFailure(c, "番号匹配", err)
		return
	}
	result["status"] = "matched"
	result["video"] = gin.H{"id": video.ID, "number": video.Number, "title": video.Title}
	c.JSON(http.StatusOK, result)
}

func javdbCommunityPage(c *gin.Context) (int, int, bool) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "15"))
	if err != nil || limitErr != nil || page < 1 || page > 9999 || limit < 1 || limit > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的评论或清单分页条件"})
		return 0, 0, false
	}
	return page, limit, true
}

func (a *App) javdbCommunityRead(c *gin.Context, lists bool) {
	movie, library, ok := a.javdbCommunityItem(c)
	if !ok {
		return
	}
	page, limit, ok := javdbCommunityPage(c)
	if !ok {
		return
	}
	if strings.TrimSpace(movie.Number) == "" || movie.Television != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "影片没有可匹配的番号"})
		return
	}
	source, ok := a.javdbCommunityClient(c, library)
	if !ok {
		return
	}
	ctx, cancel := r18RequestContext(c)
	defer cancel()
	// Repeat the current local identity and permission checks on every page.
	// The client cannot replace this number or choose an unrelated movie_id.
	video, err := source.MatchNumber(ctx, movie.Number)
	if err != nil {
		a.javdbCommunityFailure(c, "番号匹配", err)
		return
	}
	identity := javdbCommunityIdentity{Number: strings.TrimSpace(movie.Number), VideoID: video.ID, LibraryID: library.ID}
	if lists {
		result, err := source.RelatedLists(ctx, video.ID, page, limit)
		if err != nil {
			a.javdbCommunityFailure(c, "相关清单读取", err)
			return
		}
		c.JSON(http.StatusOK, struct {
			r18.RelatedListPage
			javdbCommunityIdentity
		}{result, identity})
		return
	}
	result, err := source.Reviews(ctx, video.ID, page, limit)
	if err != nil {
		a.javdbCommunityFailure(c, "评论读取", err)
		return
	}
	c.JSON(http.StatusOK, struct {
		r18.ReviewPage
		javdbCommunityIdentity
	}{result, identity})
}

func (a *App) javdbItemReviews(c *gin.Context)      { a.javdbCommunityRead(c, false) }
func (a *App) javdbItemRelatedLists(c *gin.Context) { a.javdbCommunityRead(c, true) }

func (a *App) javdbItemListMovies(c *gin.Context) {
	movie, library, ok := a.javdbCommunityItem(c)
	if !ok {
		return
	}
	listID := c.Param("list")
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	relatedPage, relatedErr := strconv.Atoi(c.DefaultQuery("related_page", "1"))
	if !javdbListID.MatchString(listID) || err != nil || relatedErr != nil || page < 1 || page > 9999 || relatedPage < 1 || relatedPage > 9999 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的关联清单或分页条件"})
		return
	}
	if strings.TrimSpace(movie.Number) == "" || movie.Television != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "影片没有可匹配的番号"})
		return
	}
	source, ok := a.javdbCommunityClient(c, library)
	if !ok {
		return
	}
	ctx, cancel := r18RequestContext(c)
	defer cancel()
	video, err := source.MatchNumber(ctx, movie.Number)
	if err != nil {
		a.javdbCommunityFailure(c, "番号匹配", err)
		return
	}
	// The list must come from this matched film's current related-list page.
	// A client-supplied list ID cannot select another film's arbitrary list.
	related, err := source.RelatedLists(ctx, video.ID, relatedPage, 15)
	if err != nil {
		a.javdbCommunityFailure(c, "关联清单核验", err)
		return
	}
	found := false
	for _, list := range related.Items {
		if list.ID == listID {
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "该影片的关联清单不存在"})
		return
	}
	result := javdbListMoviePage{Items: []javdbListMovie{}, ListID: listID, LocalLibraryID: movie.LibraryID,
		javdbCommunityIdentity: javdbCommunityIdentity{Number: strings.TrimSpace(movie.Number), VideoID: video.ID, LibraryID: library.ID}}
	// Every upstream film remains visible. Local playback is an annotation,
	// independently verified against the current library's actual STRM files.
	batch, err := source.ListMovies(ctx, listID, page, 50)
	if err != nil {
		a.javdbCommunityFailure(c, "清单影片读取", err)
		return
	}
	if batch.Page != page || batch.PageSize != 50 || len(batch.Items) > 50 {
		a.javdbCommunityFailure(c, "清单影片读取", errors.New("JavDB 清单影片分页格式不正确"))
		return
	}
	result.NextPage = page + 1
	result.HasMore = batch.HasMore && result.NextPage <= 9999
	if err := a.prepareR18LocalSourcesForLibrary(ctx, c, movie.LibraryID, batch.Items); err != nil {
		a.javdbCommunityFailure(c, "本地 STRM 查询", err)
		return
	}
	snapshot, _ := c.Get("r18LocalSources")
	local := snapshot.(r18LocalSnapshot).Sources
	for _, video := range batch.Items {
		matches := local[store.NormalizeDuplicateNumber(video.Number)]
		video = a.r18Presentation(library, video)
		item := javdbListMovie{ID: video.ID, Number: video.Number, Title: video.Title, CoverURL: video.CoverURL,
			HasLocal: len(matches) > 0, LocalMovies: []javdbLocalMovie{}}
		seen := map[int64]bool{}
		for _, match := range matches {
			if seen[match.Movie.ID] {
				continue
			}
			seen[match.Movie.ID] = true
			item.LocalMovies = append(item.LocalMovies, javdbLocalMovie{ID: match.Movie.ID, Number: match.Movie.Number, Title: match.Movie.Title})
		}
		result.Items = append(result.Items, item)
	}
	c.JSON(http.StatusOK, result)
}
