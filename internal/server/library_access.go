package server

import (
	"strconv"

	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

// Check identities before DTO/image caches and before reading any source file.
// Aggregate listings use the same boundary in SQL, before counting/pagination.
func (a *App) canAccessItem(raw string) bool {
	if raw == "" || raw == "0" {
		return true
	}
	if lib, _, ok := parseR18ItemID(raw); ok {
		return a.db.CanAccessLibrary(lib)
	}
	if _, lib, _, _, ok := store.ParseTVID(raw); ok {
		return a.db.CanAccessLibrary(lib)
	}
	if id, _, ok := parseVirtualPartID(raw); ok {
		return a.db.CanAccessMovie(id)
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if lib, ok := parseLibraryExternal(id); ok {
			return a.db.CanAccessLibrary(lib)
		}
		if m, err := a.db.Movie(id); err == nil {
			return a.db.CanAccessLibrary(m.LibraryID)
		}
		return a.db.CanAccessLibrary(id)
	}
	if kind, name, ok := entityKind(raw); ok {
		exists, err := a.db.EntityExists(kind, name)
		return err == nil && exists
	}
	if name, ok := parseBoxsetID(raw); ok {
		count, _, err := a.db.CollectionSummary(name)
		return err == nil && count > 0
	}
	// Unknown identities remain the responsibility of their typed handler.
	return true
}

func (a *App) requireLibraryAccess(c *gin.Context) {
	a = a.forRequest(c)
	// Do not populate Gin's query cache: the season/episode adapters rewrite
	// RawQuery before dispatching to the common browsing handler.
	query := c.Request.URL.Query()
	if uid := c.Param("uid"); uid != "" && uid != strconv.FormatInt(a.db.UserID(), 10) {
		c.AbortWithStatusJSON(404, gin.H{"error": "user not found"})
		return
	}
	if uid := query.Get("UserId"); uid != "" && uid != strconv.FormatInt(a.db.UserID(), 10) {
		c.AbortWithStatusJSON(404, gin.H{"error": "user not found"})
		return
	}
	ids := []string{c.Param("id"), query.Get("SeriesId")}
	for _, id := range ids {
		if !a.canAccessItem(id) {
			c.AbortWithStatusJSON(404, gin.H{"error": "item not found"})
			return
		}
	}
	parent := query.Get("ParentId")
	lib, _ := parseParentScope(parent)
	if (lib > 0 && !a.db.CanAccessLibrary(lib)) || (lib == 0 && !a.canAccessItem(parent)) {
		c.AbortWithStatusJSON(404, gin.H{"error": "parent not found"})
		return
	}
	c.Next()
}
