package server

import (
	"cmp"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"emby-go/internal/anime"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

// animeDTO reuses media DTO construction and user state but replaces display
// identity and hierarchy. Folder counts include only currently usable bindings.
func (a *App) animeDTO(raw string, c *gin.Context) gin.H {
	kind, id, season, ok := anime.ParseID(raw)
	if !ok {
		return nil
	}
	l, err := a.db.AnimeLibrary()
	if err != nil || !l.Enabled {
		return nil
	}
	if kind == "movie" || kind == "episode" {
		m, ok := a.animePlayable(raw)
		if !ok {
			return nil
		}
		sub, ep, _, err := a.db.AnimeTarget(raw)
		if err != nil {
			return nil
		}
		d, _ := a.db.Data(m.ID)
		item := a.embyItemDetail(m, d, c)
		item["Id"], item["Name"], item["Overview"], item["OriginalTitle"] = raw, sub.Name, sub.Summary, sub.OriginalName
		item["ProviderIds"] = gin.H{"Bangumi": strconv.FormatInt(sub.BangumiID, 10)}
		// Catalog presentation uses its own metadata; the bound NFO only supplies
		// playback properties and fallback artwork, not unrelated genres or cast.
		item["Genres"], item["Tags"], item["Studios"], item["Taglines"] = []string{}, []string{}, []string{}, []string{}
		item["People"], item["OfficialRating"], item["SortName"] = []gin.H{}, "", sub.Name
		item["CommunityRating"] = sub.Rating
		if len(sub.Date) >= 4 {
			if year, err := strconv.Atoi(sub.Date[:4]); err == nil {
				item["ProductionYear"] = year
			}
		}
		item["ParentId"] = externalLibraryID(l.ID)
		item["MediaSources"] = []gin.H{a.mediaSourceFor(m, raw, m.SourcePath, c)}
		if kind == "episode" {
			name := ep.Name
			if name == "" {
				name = ep.OriginalName
			}
			if name == "" {
				name = "第 " + ep.Sort.String() + " 集"
			}
			item["Type"], item["Name"], item["Overview"] = "Episode", name, ep.Summary
			item["SeriesId"], item["SeriesName"] = anime.ID("series", sub.ID), sub.Name
			item["SeasonId"], item["SeasonName"] = anime.SeasonID(sub.ID, ep.Season), animeSeasonName(ep.Season)
			item["ParentId"], item["ParentIndexNumber"], item["IndexNumber"] = anime.SeasonID(sub.ID, ep.Season), ep.Season, ep.Index
			item["ProviderIds"] = gin.H{"Bangumi": strconv.FormatInt(ep.BangumiID, 10)}
		}
		if sub.Image != "" {
			item["ImageTags"] = gin.H{"Primary": anime.SourceVersion(sub.Image)}
		}
		return item
	}
	sub, err := a.db.AnimeSubject(id)
	if err != nil || !sub.IsSeries() {
		return nil
	}
	eps, err := a.db.AnimeEpisodes(id)
	if err != nil {
		return nil
	}
	count, unplayed := 0, 0
	seasons := map[int]bool{}
	var cover store.Movie
	for _, ep := range eps {
		if kind == "season" && ep.Season != season {
			continue
		}
		if m, ok := a.animePlayable(anime.ID("episode", ep.ID)); ok {
			count++
			seasons[ep.Season] = true
			cover = m
			d, _ := a.db.Data(m.ID)
			if !d.Played {
				unplayed++
			}
		}
	}
	if count == 0 {
		return nil
	}
	name, typ, parent := sub.Name, "Series", externalLibraryID(l.ID)
	childCount := len(seasons)
	if kind == "season" {
		name, typ, parent = animeSeasonName(season), "Season", anime.ID("series", id)
		childCount = count
	}
	item := gin.H{"Id": raw, "Name": name, "OriginalTitle": sub.OriginalName, "Overview": sub.Summary, "Type": typ, "IsFolder": true, "ServerId": a.serverID, "ParentId": parent, "ChildCount": childCount, "RecursiveItemCount": count, "ImageTags": gin.H{}, "BackdropImageTags": []string{}, "ProviderIds": gin.H{"Bangumi": strconv.FormatInt(sub.BangumiID, 10)}, "CommunityRating": sub.Rating, "UserData": gin.H{"PlaybackPositionTicks": 0, "PlayCount": 0, "Played": unplayed == 0, "IsFavorite": false, "UnplayedItemCount": unplayed}}
	item["DateCreated"] = embyTime(sub.UpdatedAt)
	if len(sub.Date) >= 4 {
		if year, err := strconv.Atoi(sub.Date[:4]); err == nil {
			item["ProductionYear"] = year
		}
	}
	if kind == "season" {
		item["IndexNumber"] = season
		item["SeriesId"] = anime.ID("series", id)
		item["SeriesName"] = sub.Name
	}
	cover = a.movieArtwork(cover)
	if cover.PosterPath != "" {
		item["ImageTags"] = gin.H{"Primary": a.posterTag(cover.PosterPath)}
	}
	if sub.Image != "" {
		item["ImageTags"] = gin.H{"Primary": anime.SourceVersion(sub.Image)}
	}
	return item
}

func animeSeasonName(season int) string {
	if season == 0 {
		return "特别篇"
	}
	return "第 1 季"
}

func (a *App) animeItems(c *gin.Context) bool {
	parent := c.Query("ParentId")
	kind, id, season, parsed := anime.ParseID(parent)
	if strings.HasPrefix(parent, "anime-") && (!parsed || (kind != "series" && kind != "season")) {
		c.JSON(404, gin.H{"error": "parent not found"})
		return true
	}
	l, err := a.db.AnimeLibrary()
	if err != nil {
		return false
	}
	libID, _ := parseParentScope(parent)
	if !parsed && libID != l.ID {
		return false
	}
	if parsed && a.animeDTO(parent, c) == nil {
		c.JSON(404, gin.H{"error": "parent not found"})
		return true
	}
	out := a.animeBrowseCandidates(c, kind, id, season)
	sortAnimeItems(c, out)
	writeAnimeItems(c, out, len(out))
	return true
}

// Only bound media are visited here; a large unbound online catalog has no cost
// for Emby browsing. Filters are applied before pagination.
func (a *App) animeBrowseCandidates(c *gin.Context, kind string, id int64, season int) []gin.H {
	include := splitQueryTypes(c.Query("IncludeItemTypes"))
	recursive := strings.EqualFold(c.Query("Recursive"), "true")
	wants := func(typ string) bool {
		if len(include) == 0 {
			return true
		}
		for _, v := range include {
			if v == typ {
				return true
			}
		}
		return false
	}
	ids := map[string]bool{}
	for _, p := range a.animeProjection() {
		if id != 0 && p.SubjectID != id {
			continue
		}
		if kind == "season" && p.Season != season {
			continue
		}
		if p.EpisodeID == 0 {
			if kind == "" && wants("Movie") {
				ids[p.ItemID()] = true
			}
			continue
		}
		if kind == "" && wants("Series") {
			ids[anime.ID("series", p.SubjectID)] = true
		}
		if kind != "season" && wants("Season") && (kind == "series" || recursive || len(include) > 0) {
			ids[anime.SeasonID(p.SubjectID, p.Season)] = true
		}
		if wants("Episode") && (kind == "season" || recursive || len(include) > 0) {
			ids[p.ItemID()] = true
		}
	}
	out := []gin.H{}
	for raw := range ids {
		if item := a.animeDTO(raw, c); item != nil && animeItemMatches(c, item) {
			term := strings.ToLower(c.Query("SearchTerm"))
			names := fmt.Sprint(item["Name"], " ", item["OriginalTitle"], " ", item["SeriesName"])
			if term == "" || strings.Contains(strings.ToLower(names), term) {
				out = append(out, item)
			}
		}
	}
	return out
}

func sortAnimeItems(c *gin.Context, items []gin.H) {
	key := firstSortKey(c.DefaultQuery("SortBy", "SortName"))
	desc := strings.EqualFold(c.Query("SortOrder"), "Descending")
	sort.SliceStable(items, func(i, j int) bool {
		x, y := items[i], items[j]
		compare := 0
		switch strings.ToLower(key) {
		case "productionyear", "communityrating", "indexnumber":
			field := "ProductionYear"
			if strings.EqualFold(key, "CommunityRating") {
				field = "CommunityRating"
			}
			if strings.EqualFold(key, "IndexNumber") {
				field = "IndexNumber"
			}
			compare = cmp.Compare(animeNumber(x[field]), animeNumber(y[field]))
		case "datecreated":
			compare = strings.Compare(fmt.Sprint(x["DateCreated"]), fmt.Sprint(y["DateCreated"]))
		default:
			// Season/episode order follows the stable numeric mapping.
			if x["Type"] == y["Type"] && (x["Type"] == "Episode" || x["Type"] == "Season") && x["SeriesId"] == y["SeriesId"] {
				compare = cmp.Compare(intValue(x["ParentIndexNumber"]), intValue(y["ParentIndexNumber"]))
				if compare == 0 {
					compare = cmp.Compare(intValue(x["IndexNumber"]), intValue(y["IndexNumber"]))
				}
			} else {
				compare = strings.Compare(strings.ToLower(fmt.Sprint(x["Name"])), strings.ToLower(fmt.Sprint(y["Name"])))
			}
		}
		if compare == 0 {
			compare = strings.Compare(fmt.Sprint(x["Id"]), fmt.Sprint(y["Id"]))
		}
		if desc {
			return compare > 0
		}
		return compare < 0
	})
}
func animeNumber(v any) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	return float64(intValue(v))
}
func animePageWindow(c *gin.Context) (int, int) {
	start, _ := strconv.Atoi(c.Query("StartIndex"))
	start = max(0, start)
	limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "100"))
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	return start, limit
}
func writeAnimeItems(c *gin.Context, items []gin.H, total int) {
	start, limit := animePageWindow(c)
	from := min(start, len(items))
	to := from + min(limit, len(items)-from)
	c.JSON(http.StatusOK, gin.H{"Items": items[from:to], "TotalRecordCount": total, "StartIndex": start})
}

// Mixed global search keeps every physical movie only once. SQL counts and
// filters originals after excluding playable bindings, then the two sorted
// result sets are merged before slicing the requested page.
func (a *App) animeRootItems(c *gin.Context) bool {
	if c.Query("ParentId") != "" {
		return false
	}
	include := splitQueryTypes(c.Query("IncludeItemTypes"))
	movieWanted, animeWanted := len(include) == 0, len(include) == 0
	for _, v := range include {
		if v == "Movie" || v == "Video" {
			movieWanted = true
		}
		if v == "Movie" || v == "Series" || v == "Season" || v == "Episode" {
			animeWanted = true
		}
	}
	if !animeWanted {
		return false
	}
	rows := a.animeProjection()
	if len(rows) == 0 {
		return false
	}
	out := a.animeBrowseCandidates(c, "", 0, 0)
	total := len(out)
	if movieWanted {
		exclude := make([]int64, 0, len(rows))
		for _, p := range rows {
			exclude = append(exclude, p.MovieID)
		}
		start, limit := animePageWindow(c)
		filters := strings.ToLower(c.Query("Filters"))
		ms, count, err := a.db.SearchWithoutAnime(exclude, c.Query("SearchTerm"), c.Query("Years"), joinEntityNames(c, "Genres", "Genre", "GenreIds"), joinEntityNames(c, "Tags", "Tag", "TagIds"), joinEntityNames(c, "Studios", "Studio", "StudioIds"), joinEntityNames(c, "Person", "Person", "PersonIds"), strings.Contains(filters, "isunplayed"), strings.Contains(filters, "isfavorite"), firstSortKey(c.DefaultQuery("SortBy", "SortName")), strings.EqualFold(c.Query("SortOrder"), "Descending"), start+limit, c.Query("OfficialRatings"))
		if err != nil {
			c.JSON(500, gin.H{"error": "media query failed"})
			return true
		}
		total += count
		for _, m := range ms {
			d, _ := a.db.Data(m.ID)
			out = append(out, a.embyItemDetail(m, d, c))
		}
	}
	sortAnimeItems(c, out)
	writeAnimeItems(c, out, total)
	return true
}

func (a *App) animeSeasons(c *gin.Context) {
	q := c.Request.URL.Query()
	if q.Get("UserId") != "" && q.Get("UserId") != "1" {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}
	q.Set("ParentId", c.Param("id"))
	q.Set("IncludeItemTypes", "Season")
	c.Request.URL.RawQuery = q.Encode()
	if !a.animeItems(c) {
		a.emptyItems(c)
	}
}
func (a *App) animeEpisodes(c *gin.Context) {
	q := c.Request.URL.Query()
	if q.Get("UserId") != "" && q.Get("UserId") != "1" {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}
	parent := c.Param("id")
	kind, subjectID, _, valid := anime.ParseID(parent)
	if strings.HasPrefix(parent, "anime-") && (!valid || kind != "series") {
		c.JSON(404, gin.H{"error": "series not found"})
		return
	}
	if v := q.Get("SeasonId"); v != "" {
		k, sid, _, ok := anime.ParseID(v)
		if !valid || !ok || k != "season" || sid != subjectID {
			c.JSON(404, gin.H{"error": "season does not belong to series"})
			return
		}
		parent = v
	} else if v := q.Get("Season"); v != "" && valid {
		if v != "0" && v != "1" {
			c.JSON(400, gin.H{"error": "invalid season"})
			return
		}
		n, _ := strconv.Atoi(v)
		parent = anime.SeasonID(subjectID, n)
	}
	q.Set("ParentId", parent)
	q.Set("IncludeItemTypes", "Episode")
	c.Request.URL.RawQuery = q.Encode()
	if !a.animeItems(c) {
		a.emptyItems(c)
	}
}

func (a *App) animeRequestedItems(c *gin.Context) bool {
	raw := c.Query("Ids")
	if !strings.Contains(raw, "anime-") {
		return false
	}
	ids := strings.Split(raw, ",")
	if len(ids) > 1000 {
		c.JSON(400, gin.H{"error": "too many item IDs"})
		return true
	}
	out := []gin.H{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		var item gin.H
		if strings.HasPrefix(id, "anime-") {
			item = a.animeDTO(id, c)
		} else if m, err := a.resolveUserMedia(id); err == nil {
			d, _ := a.db.Data(m.ID)
			item = a.embyItemDetail(m, d, c)
		}
		if item != nil && animeItemMatches(c, item) {
			out = append(out, item)
		}
	}
	writeAnimeItems(c, out, len(out))
	return true
}

func (a *App) animeImage(c *gin.Context) {
	raw := c.Param("id")
	kind, id, season, ok := anime.ParseID(raw)
	if !ok {
		c.Status(404)
		return
	}
	var media store.Movie
	if kind == "movie" || kind == "episode" {
		media, ok = a.animePlayable(raw)
	} else {
		ok = false
		if eps, err := a.db.AnimeEpisodes(id); err == nil {
			for _, ep := range eps {
				if kind == "season" && ep.Season != season {
					continue
				}
				if m, found := a.animePlayable(anime.ID("episode", ep.ID)); found {
					media, ok = m, true
					break
				}
			}
		}
	}
	if !ok {
		c.Status(404)
		return
	}
	subjectID := id
	if kind == "episode" {
		if ep, err := a.db.AnimeEpisode(id); err == nil {
			subjectID = ep.SubjectID
		}
	}
	if c.Param("kind") == "Primary" || c.Param("kind") == "primary" {
		if sub, err := a.db.AnimeSubject(subjectID); err == nil && a.serveAnimeCover(c, anime.ID("cover", subjectID), sub.Image) {
			return
		}
	}
	media = a.movieArtwork(media)
	path := movieImagePath(media, c.Param("kind"))
	if path == "" {
		c.Status(404)
		return
	}
	a.serveImage(c, path)
}
