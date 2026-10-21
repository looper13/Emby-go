package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"emby-go/internal/anime"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *App) animeProjection() []store.AnimeProjection {
	l, err := a.db.AnimeLibrary()
	if err != nil || !l.Enabled || !a.db.CanAccessLibrary(l.ID) {
		return nil
	}
	rows, err := a.db.AnimeProjection()
	if err != nil {
		return nil
	}
	out := make([]store.AnimeProjection, 0, len(rows))
	for _, p := range rows {
		if _, ok := a.animePlayable(p.ItemID()); ok {
			out = append(out, p)
		}
	}
	return out
}

func (a *App) projectAnimeMovie(c *gin.Context, movieID int64) gin.H {
	raw, err := a.db.ResolveAnimeDisplay(movieID)
	if err != nil {
		return nil
	}
	return a.animeDTO(raw, c)
}

// animePlaylist handles an explicit anime library scope. Global Latest/Resume
// already contain each original movie exactly once; their DTOs are replaced in
// place, preserving the original pagination and deduplicating by movie identity.
func (a *App) animePlaylist(c *gin.Context, resume bool) bool {
	l, err := a.db.AnimeLibrary()
	if err != nil {
		return false
	}
	lib, _ := parseParentScope(c.Query("ParentId"))
	if lib != l.ID {
		return false
	}
	rows := a.animeProjection()
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].MovieID > rows[j].MovieID })
	out := []gin.H{}
	for _, p := range rows {
		if resume && (p.Position <= 0 || p.Hidden || p.Played) {
			continue
		}
		if item := a.animeDTO(p.ItemID(), c); item != nil && animeItemMatches(c, item) {
			out = append(out, item)
		}
	}
	start, _ := strconv.Atoi(c.Query("StartIndex"))
	limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "30"))
	if start < 0 {
		start = 0
	}
	if limit < 1 || limit > 1000 {
		limit = 30
	}
	total := len(out)
	from := min(start, total)
	to := from + min(limit, total-from)
	if resume {
		c.JSON(200, gin.H{"Items": out[from:to], "TotalRecordCount": total, "StartIndex": start})
	} else {
		c.JSON(200, out[from:to])
	}
	return true
}

func (a *App) animeNextUp(c *gin.Context) {
	rows := a.animeProjection()
	started := map[int64]bool{}
	seriesID := int64(0)
	if raw := c.Query("SeriesId"); raw != "" {
		kind, id, _, ok := anime.ParseID(raw)
		if !ok || kind != "series" {
			c.JSON(404, gin.H{"error": "series not found"})
			return
		}
		seriesID = id
	}
	if parent := c.Query("ParentId"); parent != "" {
		lib, _ := parseParentScope(parent)
		l, err := a.db.AnimeLibrary()
		if err != nil || lib != l.ID {
			a.emptyItems(c)
			return
		}
	}
	history, _ := a.db.AnimeProjection()
	for _, p := range history {
		if p.EpisodeID > 0 && p.Season == 1 && (p.Played || p.Position > 0 || p.PlayCount > 0) {
			started[p.SubjectID] = true
		}
	}
	seen := map[int64]bool{}
	out := []gin.H{}
	for _, p := range rows {
		if p.EpisodeID == 0 || p.Season != 1 || !started[p.SubjectID] || seen[p.SubjectID] || (seriesID != 0 && p.SubjectID != seriesID) || p.Played {
			continue
		}
		seen[p.SubjectID] = true
		if p.Position > 0 || p.Hidden {
			continue
		}
		if item := a.animeDTO(p.ItemID(), c); item != nil {
			out = append(out, item)
		}
	}
	start, _ := strconv.Atoi(c.Query("StartIndex"))
	limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "100"))
	if c.Query("ParentId") == "" && c.Query("SeriesId") == "" {
		out = append(out, a.televisionNextUpCandidates(c, 0, "")...)
	}
	if start < 0 {
		start = 0
	}
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	total := len(out)
	from := min(start, total)
	to := from + min(limit, total-from)
	c.JSON(http.StatusOK, gin.H{"Items": out[from:to], "TotalRecordCount": total, "StartIndex": start})
}

func animeItemMatches(c *gin.Context, item gin.H) bool {
	if include := splitQueryTypes(c.Query("IncludeItemTypes")); len(include) > 0 {
		found := false
		for _, kind := range include {
			if item["Type"] == kind {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	// These catalog fields are currently unknown; don't falsely match an
	// unrelated genre, performer, studio, or content rating from the bound NFO.
	for _, field := range []string{"Genres", "GenreIds", "Tags", "TagIds", "Studios", "StudioIds", "Person", "PersonIds", "OfficialRatings"} {
		if c.Query(field) != "" {
			return false
		}
	}
	if years := c.Query("Years"); years != "" {
		year := item["ProductionYear"]
		found := false
		for _, y := range strings.Split(years, ",") {
			if strconv.Itoa(intValue(year)) == y {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	data := userDataOf(item)
	filters := strings.ToLower(c.Query("Filters"))
	if strings.Contains(filters, "isunplayed") && data["Played"] == true {
		return false
	}
	if strings.Contains(filters, "isplayed") && data["Played"] != true {
		return false
	}
	if !matchesFavorite(c, data["IsFavorite"] == true) {
		return false
	}
	return true
}
func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
