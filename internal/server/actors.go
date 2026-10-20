package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"emby-go/internal/store"

	"github.com/gin-gonic/gin"
)

type adminActor struct {
	Name       string `json:"name"`
	MovieCount int    `json:"movie_count"`
	HasImage   bool   `json:"has_image"`
	ImageTag   string `json:"image_tag"`
	Favorite   bool   `json:"favorite"`
}

func (a *App) adminActorInventory(ctx context.Context, libraryID int64) ([]adminActor, error) {
	key := responseKey("adminactors-inventory", a.cacheScope(libraryID))
	raw, err := a.cache.Load(ctx, key, 30*time.Second, func() ([]byte, error) {
		actors, err := a.db.ActorSummaries(ctx, libraryID)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(actors))
		known := make(map[string]store.ActorRef, len(actors))
		for _, actor := range actors {
			names = append(names, actor.Name)
			known[actor.Name] = store.ActorRef{Name: actor.Name, AvatarTag: actor.AvatarTag}
		}
		portraits, err := a.personAvatars(ctx, names, known)
		if err != nil {
			return nil, err
		}
		items := make([]adminActor, 0, len(actors))
		for _, actor := range actors {
			portrait := portraits[actor.Name]
			items = append(items, adminActor{Name: actor.Name, MovieCount: actor.MovieCount, HasImage: portrait.Path != "", ImageTag: portrait.Tag})
		}
		return json.Marshal(items)
	})
	if err != nil {
		return nil, err
	}
	var items []adminActor
	err = json.Unmarshal(raw, &items)
	return items, err
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
	actors, err := a.adminActorInventory(ctx, libraryID)
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
		hasImage := actor.HasImage
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
			actor.Favorite = favorite
			items = append(items, actor)
		}
		total++
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "stats": gin.H{"total": len(actors), "ready": ready, "missing": len(actors) - ready}})
}
