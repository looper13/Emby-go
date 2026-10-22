package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Batch member lookups preserve each collection's full count and metadata.
// The request-scoped store and explicit library check preserve account grants.
func (a *App) memberCollections(values []string, libraryID int64) (map[string]bool, error) {
	owned := make(map[string]bool)
	seen := make(map[int64]bool)
	var ids []int64
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			raw = strings.TrimSpace(raw)
			id, err := strconv.ParseInt(raw, 10, 64)
			if err == nil && id > 0 && !strings.HasPrefix(raw, "+") && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	for len(ids) > 0 {
		n := min(500, len(ids))
		movies, err := a.db.MoviesByIDs(ids[:n])
		if err != nil {
			return nil, err
		}
		for _, movie := range movies {
			if movie.IsVisible() && movie.Collection != "" && a.db.CanAccessLibrary(movie.LibraryID) && (libraryID == 0 || movie.LibraryID == libraryID) {
				owned[movie.Collection] = true
			}
		}
		ids = ids[n:]
	}
	return owned, nil
}

func (a *App) itemCollections(c *gin.Context) {
	if c.Param("uid") != "" && !a.validUser(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.Status(http.StatusNotFound)
		return
	}
	movie, err := a.db.Movie(id)
	if err != nil || !movie.IsVisible() || !a.db.CanAccessLibrary(movie.LibraryID) {
		c.Status(http.StatusNotFound)
		return
	}
	stats, err := a.db.CollectionStats(a.collectionMinMovies())
	if err != nil {
		c.JSON(500, gin.H{"error": "item data unavailable"})
		return
	}
	items := make([]gin.H, 0, 1)
	if stat, ok := stats[movie.Collection]; movie.Collection != "" && ok {
		items = append(items, a.boxsetItemFrom(movie.Collection, stat))
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": len(items), "StartIndex": 0})
}
