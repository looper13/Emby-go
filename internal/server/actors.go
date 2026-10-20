package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type adminActor struct {
	Name       string `json:"name"`
	MovieCount int    `json:"movie_count"`
	HasImage   bool   `json:"has_image"`
	ImageTag   string `json:"image_tag"`
	Favorite   bool   `json:"favorite"`
}

// adminActors is read-only: never downloads portraits or substitutes movie posters.
func (a *App) adminActors(c *gin.Context) {
	libraryID, libErr := strconv.ParseInt(c.DefaultQuery("library_id", "0"), 10, 64)
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "48"))
	offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
	status := c.DefaultQuery("status", "all")
	if value := c.Query("favorite"); value != "" && value != "true" && value != "false" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的收藏筛选参数"})
		return
	}
	if libErr != nil || libraryID < 0 || limitErr != nil || limit < 1 || limit > 200 || offsetErr != nil || offset < 0 || (status != "all" && status != "missing" && status != "ready") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的演员筛选或分页参数"})
		return
	}
	ctx, cancel := a.requestContext(c)
	defer cancel()
	actors, err := a.db.ActorSummaries(ctx, libraryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	search := strings.ToLower(strings.TrimSpace(c.Query("search")))
	items := make([]adminActor, 0, limit)
	total, ready := 0, 0
	for _, actor := range actors {
		if ctx.Err() != nil {
			c.JSON(http.StatusRequestTimeout, gin.H{"error": "演员列表读取超时，请重试"})
			return
		}
		// Reuse the same short-lived local asset cache as the Person image API.
		path, tag := a.personAvatar(actor.Name)
		hasImage := path != ""
		if hasImage {
			ready++
		}
		if !strings.Contains(strings.ToLower(actor.Name), search) || (status == "missing" && hasImage) || (status == "ready" && !hasImage) {
			continue
		}
		favorite := a.db.ItemFavorite(entityId("Person", actor.Name))
		if value := c.Query("favorite"); value != "" && favorite != (value == "true") {
			continue
		}
		if total >= offset && len(items) < limit {
			items = append(items, adminActor{Name: actor.Name, MovieCount: actor.MovieCount, HasImage: hasImage, ImageTag: tag, Favorite: favorite})
		}
		total++
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "stats": gin.H{"total": len(actors), "ready": ready, "missing": len(actors) - ready}})
}
