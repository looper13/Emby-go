package server

import (
	"net/http"
	"strings"

	"emby-go/internal/scraper"
	"github.com/gin-gonic/gin"
)

func (a *App) adminActorImportPreview(c *gin.Context) {
	ctx, cancel := a.requestContext(c)
	defer cancel()
	preview, err := scraper.New(a.scrapeConfig(), a.db).PreviewActorImport(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, preview)
}
func (a *App) adminActorImport(c *gin.Context) {
	cfg := a.scrapeConfig()
	names, err := a.db.ActorNames(0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !a.startScrapeTask(c, "actor_import", len(names)) {
		return
	}
	go func() {
		taskID := a.startTask("actor_import")
		_, err := scraper.New(cfg, a.db).ImportActors(a.scrapeContext(), a.updateScrapeProgress, func(result scraper.AvatarResult) {
			if result.Status == scraper.StatusFailed || result.Category == "ambiguous" || result.Category == "partial" {
				a.addScrapeFailure("演员 " + result.Actor + "：" + result.Reason)
			}
		})
		a.releaseNFO("actor_import")
		a.finishTask(taskID, err)
		a.endScrape(err)
		a.cache.Clear()
		a.imgMeta.DeleteMatching(func(key string) bool { return strings.HasPrefix(key, "av:") || strings.HasPrefix(key, "ep:") })
	}()
	c.JSON(http.StatusAccepted, gin.H{"total": len(names)})
}
