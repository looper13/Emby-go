package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

type televisionGroup struct {
	ID       string
	Episodes []store.Movie
}

func televisionSeasonName(season int) string {
	if season == 0 {
		return "特别篇"
	}
	return fmt.Sprintf("第 %d 季", season)
}

func groupTelevision(episodes []store.Movie) []televisionGroup {
	groups := []televisionGroup{}
	indexes := map[string]int{}
	for _, m := range episodes {
		id := store.TVSeriesID(m.LibraryID, m.Television.SeriesPath)
		i, ok := indexes[id]
		if !ok {
			i = len(groups)
			indexes[id] = i
			groups = append(groups, televisionGroup{ID: id})
		}
		groups[i].Episodes = append(groups[i].Episodes, m)
	}
	return groups
}

func televisionFolderMovie(raw string, episodes []store.Movie) (store.Movie, int, int) {
	kind, _, _, season, _ := store.ParseTVID(raw)
	count := 0
	seasons := map[int]bool{}
	var first store.Movie
	for _, m := range episodes {
		if kind == "Season" && m.Television.Season != season {
			continue
		}
		if count == 0 {
			first = m
		}
		count++
		seasons[m.Television.Season] = true
	}
	if count == 0 {
		return store.Movie{}, 0, 0
	}
	tv := first.Television
	dir, name := tv.SeriesPath, tv.Title
	children := len(seasons)
	if kind == "Season" {
		dir, name, children = first.OutputDir, televisionSeasonName(season), count
	}
	m := store.Movie{LibraryID: first.LibraryID, Title: name, OriginalTitle: tv.OriginalTitle, Plot: tv.Plot, Year: tv.Year, OutputDir: dir, SourcePath: filepath.Join(dir, "tvshow.strm"), Genres: first.Genres, Studios: first.Studios, CreatedAt: first.CreatedAt, UpdatedAt: first.UpdatedAt}
	return m, count, children
}

func (a *App) televisionFolder(raw string, c *gin.Context) gin.H {
	eps, err := a.db.TVFolderEpisodes(raw)
	if err != nil {
		return nil
	}
	return a.televisionFolderDTO(raw, eps, c)
}

func (a *App) televisionFolderDTO(raw string, episodes []store.Movie, c *gin.Context) gin.H {
	kind, lib, path, season, ok := store.ParseTVID(raw)
	if !ok {
		return nil
	}
	raw = store.TVSeriesID(lib, path)
	if kind == "Season" {
		raw = store.TVSeasonID(lib, path, season)
	}
	m, count, children := televisionFolderMovie(raw, episodes)
	if count == 0 {
		return nil
	}
	data, err := a.db.DataFor(movieIDs(episodes))
	if err != nil {
		return nil
	}
	unplayed := 0
	for _, ep := range episodes {
		if (kind != "Season" || ep.Television.Season == season) && !data[ep.ID].Played {
			unplayed++
		}
	}
	m = a.movieArtwork(m)
	item := a.embyItemWithArtwork(m, store.UserData{}, nil, true, true)
	item["Id"], item["Type"], item["IsFolder"] = raw, kind, true
	item["ParentId"] = externalLibraryID(lib)
	item["ChildCount"], item["RecursiveItemCount"] = children, count
	item["Overview"], item["OriginalTitle"], item["ProductionYear"] = m.Plot, m.OriginalTitle, m.Year
	item["UserData"] = a.itemUserData(raw)
	item["UserData"].(gin.H)["Played"], item["UserData"].(gin.H)["UnplayedItemCount"] = unplayed == 0, unplayed
	if kind == "Season" {
		seriesFolder := store.TVSeriesID(lib, path)
		item["ParentId"], item["SeriesId"], item["SeriesName"], item["IndexNumber"] = seriesFolder, seriesFolder, episodes[0].Television.Title, season
		item["SeasonId"] = raw
		seriesMovie, _, _ := televisionFolderMovie(seriesFolder, episodes)
		seriesMovie = a.movieArtwork(seriesMovie)
		if seriesMovie.PosterPath != "" {
			item["SeriesPrimaryImageTag"] = a.posterTag(seriesMovie.PosterPath)
		}
	}
	delete(item, "MediaType")
	item["CollectionType"] = "tvshows"
	return item
}

func (a *App) televisionImage(c *gin.Context) {
	eps, err := a.db.TVFolderEpisodes(c.Param("id"))
	if err != nil {
		c.Status(404)
		return
	}
	m, count, _ := televisionFolderMovie(c.Param("id"), eps)
	if count == 0 {
		c.Status(404)
		return
	}
	m = a.movieArtwork(m)
	index := 0
	if c.Param("index") != "" {
		index, err = strconv.Atoi(c.Param("index"))
		if err != nil || index < 0 {
			c.Status(404)
			return
		}
	}
	path := movieImagePath(m, c.Param("kind"))
	if strings.EqualFold(c.Param("kind"), "Backdrop") {
		path = ""
		if index < len(m.Backdrops()) {
			path = m.Backdrops()[index]
		}
	} else if index != 0 {
		path = ""
	}
	if path == "" {
		c.Status(404)
		return
	}
	a.serveImage(c, path)
}

func queryParam(c *gin.Context, keys ...string) string {
	q := c.Request.URL.Query()
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			return v
		}
	}
	for k, vs := range q {
		if len(vs) == 0 || vs[0] == "" {
			continue
		}
		for _, target := range keys {
			if strings.EqualFold(k, target) {
				return vs[0]
			}
		}
	}
	return ""
}

// Intercept local hierarchy scopes before the movie-only compatibility path.
// 电影和本地电视条目在分页前统一排序。
func (a *App) televisionItems(c *gin.Context) bool {
	parent := c.Query("ParentId")
	seasonParam := queryParam(c, "SeasonId", "seasonId", "seasonID")
	seriesParam := queryParam(c, "SeriesId", "seriesId", "seriesID")
	seasonNumParam := queryParam(c, "Season", "season")

	kind, lib, path, season, folder := store.ParseTVID(parent)
	if strings.HasPrefix(parent, "tv-") {
		if !folder {
			c.JSON(404, gin.H{"error": "not found"})
			return true
		}
		if kind == "Series" {
			if seasonParam != "" {
				if sk, sl, sp, sn, sok := store.ParseTVID(seasonParam); sok && sk == "Season" {
					if sl != lib || sp != path {
						c.JSON(404, gin.H{"error": "season does not belong to series"})
						return true
					}
					parent = seasonParam
					kind, season = "Season", sn
				} else if n, err := strconv.Atoi(seasonParam); err == nil && n >= 0 && n <= 1000 {
					parent = store.TVSeasonID(lib, path, n)
					kind, season = "Season", n
				} else {
					c.JSON(404, gin.H{"error": "season does not belong to series"})
					return true
				}
			} else if seasonNumParam != "" {
				n, err := strconv.Atoi(seasonNumParam)
				if err != nil || n < 0 || n > 1000 {
					c.JSON(400, gin.H{"error": "invalid season"})
					return true
				}
				parent = store.TVSeasonID(lib, path, n)
				kind, season = "Season", n
			}
		}
	} else {
		if sk, sl, sp, sn, sok := store.ParseTVID(seasonParam); sok && sk == "Season" {
			parent = seasonParam
			kind, lib, path, season, folder = sk, sl, sp, sn, true
		} else if sk, sl, sp, _, sok := store.ParseTVID(seriesParam); sok && sk == "Series" {
			targetSeason := -1
			if seasonParam != "" {
				if ssk, ssl, ssp, ssn, ssok := store.ParseTVID(seasonParam); ssok && ssk == "Season" && ssl == sl && ssp == sp {
					targetSeason = ssn
				} else if n, err := strconv.Atoi(seasonParam); err == nil && n >= 0 && n <= 1000 {
					targetSeason = n
				}
			} else if seasonNumParam != "" {
				if n, err := strconv.Atoi(seasonNumParam); err == nil && n >= 0 && n <= 1000 {
					targetSeason = n
				}
			}
			if targetSeason >= 0 {
				parent = store.TVSeasonID(sl, sp, targetSeason)
				kind, lib, path, season, folder = "Season", sl, sp, targetSeason, true
			} else {
				parent = seriesParam
				kind, lib, path, season, folder = "Series", sl, sp, 0, true
			}
		} else if parent != "" {
			var collection string
			lib, collection = parseParentScope(parent)
			if collection != "" || lib <= 0 {
				return false
			}
			l, err := a.db.Library(lib)
			if err != nil {
				return false
			}
			if l.Type != store.LibraryTVShows && l.Type != store.LibraryMixed {
				return false
			}
		}
	}
	include := splitQueryTypes(c.Query("IncludeItemTypes"))
	if len(include) > 0 {
		relevant := false
		for _, typ := range include {
			relevant = relevant || typ == "Movie" || typ == "Video" || typ == "Series" || typ == "Season" || typ == "Episode"
		}
		if !relevant {
			return false
		}
	}
	eps, err := a.db.TVEpisodes(lib, path)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return true
	}
	if parent == "" && len(eps) == 0 {
		return false
	}
	if folder && len(eps) == 0 {
		c.JSON(404, gin.H{"error": "not found"})
		return true
	}
	if !a.validQueryUser(c) {
		c.JSON(404, gin.H{"error": "user not found"})
		return true
	}
	if folder && kind == "Season" {
		found := false
		for _, ep := range eps {
			found = found || ep.Television.Season == season
		}
		if !found {
			c.JSON(404, gin.H{"error": "not found"})
			return true
		}
	}
	wants := func(typ string) bool {
		if len(include) == 0 {
			if kind == "Series" {
				return typ == "Season"
			}
			if kind == "Season" {
				return typ == "Episode"
			}
			return typ == "Movie" || typ == "Series" || (strings.EqualFold(c.Query("Recursive"), "true") && typ == "Episode")
		}
		for _, v := range include {
			if v == typ || (v == "Video" && (typ == "Movie" || typ == "Episode")) {
				return true
			}
		}
		return false
	}
	out := []gin.H{}
	for _, group := range groupTelevision(eps) {
		if !folder && wants("Series") {
			if item := a.televisionFolderDTO(group.ID, group.Episodes, c); item != nil && televisionItemMatches(c, item) {
				out = append(out, item)
			}
		}
		if wants("Season") {
			seen := map[int]bool{}
			for _, ep := range group.Episodes {
				n := ep.Television.Season
				if (kind == "Season" && n != season) || seen[n] {
					continue
				}
				seen[n] = true
				id := store.TVSeasonID(ep.LibraryID, ep.Television.SeriesPath, n)
				if item := a.televisionFolderDTO(id, group.Episodes, c); item != nil && televisionItemMatches(c, item) {
					out = append(out, item)
				}
			}
		}
		if wants("Episode") {
			data, _ := a.db.DataFor(movieIDs(group.Episodes))
			for _, ep := range group.Episodes {
				if kind == "Season" && ep.Television.Season != season {
					continue
				}
				item := a.embyItemDetail(ep, data[ep.ID], c)
				if televisionItemMatches(c, item) {
					out = append(out, item)
				}
			}
		}
	}
	total := len(out)
	if !folder && wants("Movie") {
		start, limit := mediaPageWindow(c)
		filters := strings.ToLower(c.Query("Filters"))
		ms, count, err := a.db.SearchMediaExcluding(nil, []string{"Movie"}, lib, c.Query("SearchTerm"), c.Query("Years"), joinEntityNames(c, "Genres", "Genre", "GenreIds"), joinEntityNames(c, "Tags", "Tag", "TagIds"), joinEntityNames(c, "Studios", "Studio", "StudioIds"), joinEntityNames(c, "Person", "Person", "PersonIds"), strings.Contains(filters, "isunplayed"), favoriteFilter(c), firstSortKey(c.DefaultQuery("SortBy", "SortName")), strings.EqualFold(c.Query("SortOrder"), "Descending"), start+limit, 0, c.Query("OfficialRatings"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return true
		}
		total += count
		for _, m := range ms {
			d, _ := a.db.Data(m.ID)
			out = append(out, a.embyItemDetail(m, d, c))
		}
	}
	sortMediaItems(c, out)
	writeMediaItems(c, out, total)
	return true
}

func televisionItemMatches(c *gin.Context, item gin.H) bool {
	if include := splitQueryTypes(c.Query("IncludeItemTypes")); len(include) > 0 {
		matched := false
		for _, typ := range include {
			matched = matched || item["Type"] == typ || (typ == "Video" && (item["Type"] == "Movie" || item["Type"] == "Episode"))
		}
		if !matched {
			return false
		}
	}
	if term := strings.ToLower(c.Query("SearchTerm")); term != "" && !strings.Contains(strings.ToLower(fmt.Sprint(item["Name"], " ", item["OriginalTitle"], " ", item["SeriesName"])), term) {
		return false
	}
	if years := c.Query("Years"); years != "" {
		match := false
		for _, y := range strings.Split(years, ",") {
			match = match || strings.TrimSpace(y) == strconv.Itoa(intValue(item["ProductionYear"]))
		}
		if !match {
			return false
		}
	}
	for _, field := range []string{"Genres", "Tags", "Studios", "OfficialRatings", "PersonIds", "GenreIds", "TagIds", "StudioIds", "Person"} {
		if raw := c.Query(field); raw != "" {
			key := field
			if field == "OfficialRatings" {
				key = "OfficialRating"
			}
			if field == "Person" || strings.HasSuffix(field, "Ids") {
				return false
			}
			matched := false
			for _, value := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '|' }) {
				matched = matched || strings.Contains(fmt.Sprint(item[key]), value)
			}
			if !matched {
				return false
			}
		}
	}
	filters := strings.ToLower(c.Query("Filters"))
	d := userDataOf(item)
	return (!strings.Contains(filters, "isunplayed") || d["Played"] != true) && (!strings.Contains(filters, "isplayed") || d["Played"] == true) && matchesFavorite(c, d["IsFavorite"] == true)
}

func (a *App) televisionSeasons(c *gin.Context) {
	if c.Param("uid") != "" && !a.validUser(c) {
		return
	}
	parent := c.Param("id")
	if kind, lib, path, _, ok := store.ParseTVID(parent); ok && kind == "Season" {
		parent = store.TVSeriesID(lib, path)
	}
	q := c.Request.URL.Query()
	q.Set("ParentId", parent)
	q.Set("IncludeItemTypes", "Season")
	c.Request.URL.RawQuery = q.Encode()
	if !a.televisionItems(c) {
		a.emptyItems(c)
	}
}

func (a *App) televisionEpisodes(c *gin.Context) {
	if c.Param("uid") != "" && !a.validUser(c) {
		return
	}
	rawID := c.Param("id")
	kind, lib, path, seasonNum, ok := store.ParseTVID(rawID)
	if !ok || (kind != "Series" && kind != "Season") {
		c.JSON(404, gin.H{"error": "series or season not found"})
		return
	}
	q := c.Request.URL.Query()
	parent := rawID
	if kind == "Season" {
		parent = store.TVSeasonID(lib, path, seasonNum)
	} else {
		if raw := queryParam(c, "SeasonId", "seasonId", "seasonID"); raw != "" {
			if k, l, p, _, valid := store.ParseTVID(raw); valid && k == "Season" {
				if l != lib || p != path {
					c.JSON(404, gin.H{"error": "season does not belong to series"})
					return
				}
				parent = raw
			} else if n, err := strconv.Atoi(raw); err == nil && n >= 0 && n <= 1000 {
				parent = store.TVSeasonID(lib, path, n)
			} else {
				c.JSON(404, gin.H{"error": "season does not belong to series"})
				return
			}
		} else if raw := queryParam(c, "Season", "season"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 || n > 1000 {
				c.JSON(400, gin.H{"error": "invalid season"})
				return
			}
			parent = store.TVSeasonID(lib, path, n)
		}
	}
	q.Set("ParentId", parent)
	q.Set("IncludeItemTypes", "Episode")
	c.Request.URL.RawQuery = q.Encode()
	a.televisionItems(c)
}

func (a *App) adminTelevision(c *gin.Context) {
	lib, err := strconv.ParseInt(c.Query("library_id"), 10, 64)
	if err != nil || lib <= 0 {
		c.JSON(400, gin.H{"error": "library_id required"})
		return
	}
	if _, err = a.db.Library(lib); err != nil {
		c.JSON(404, gin.H{"error": "library not found"})
		return
	}
	eps, err := a.db.TVEpisodes(lib, "")
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	items := []gin.H{}
	for _, group := range groupTelevision(eps) {
		item := a.televisionFolderDTO(group.ID, group.Episodes, c)
		if item != nil && (c.Query("search") == "" || strings.Contains(strings.ToLower(fmt.Sprint(item["Name"], " ", item["OriginalTitle"])), strings.ToLower(c.Query("search")))) {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return fmt.Sprint(items[i]["Name"]) < fmt.Sprint(items[j]["Name"]) })
	start, _ := strconv.Atoi(c.Query("offset"))
	if start < 0 {
		start = 0
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "48"))
	if limit < 1 || limit > 1000 {
		limit = 48
	}
	total := len(items)
	from := min(start, total)
	to := from + min(limit, total-from)
	c.JSON(http.StatusOK, gin.H{"Items": items[from:to], "TotalRecordCount": total, "StartIndex": start})
}

func (a *App) televisionNextUpCandidates(c *gin.Context, libraryID int64, path string) []gin.H {
	eps, err := a.db.TVEpisodes(libraryID, path)
	if err != nil {
		return nil
	}
	data, err := a.db.DataFor(movieIDs(eps))
	if err != nil {
		return nil
	}
	out := []gin.H{}
	for _, group := range groupTelevision(eps) {
		last := -1
		for i, ep := range group.Episodes {
			if ep.Television.Season > 0 && (data[ep.ID].Played || data[ep.ID].PositionTicks > 0 || data[ep.ID].PlayCount > 0) {
				last = i
			}
		}
		if last < 0 {
			continue
		}
		if !data[group.Episodes[last].ID].Played && data[group.Episodes[last].ID].PositionTicks > 0 {
			continue
		}
		for _, ep := range group.Episodes[last+1:] {
			d := data[ep.ID]
			if ep.Television.Season == 0 || d.Played {
				continue
			}
			if d.PositionTicks > 0 || d.HideFromResume {
				break
			}
			item := a.embyItemDetail(ep, d, c)
			if televisionItemMatches(c, item) {
				out = append(out, item)
			}
			break
		}
	}
	return out
}
func (a *App) televisionNextUp(c *gin.Context) bool {
	lib, path := int64(0), ""
	if raw := c.Query("SeriesId"); raw != "" {
		if !strings.HasPrefix(raw, "tv-") {
			return false
		}
		kind, l, p, _, ok := store.ParseTVID(raw)
		if !ok || kind != "Series" {
			c.JSON(404, gin.H{"error": "series not found"})
			return true
		}
		if _, err := a.db.TVFolderEpisodes(raw); err != nil {
			c.JSON(404, gin.H{"error": "series not found"})
			return true
		}
		lib, path = l, p
	} else if raw := c.Query("ParentId"); raw != "" {
		lib, _ = parseParentScope(raw)
		l, err := a.db.Library(lib)
		if err != nil || (l.Type != store.LibraryTVShows && l.Type != store.LibraryMixed) {
			return false
		}
	}
	out := a.televisionNextUpCandidates(c, lib, path)
	sortMediaItems(c, out)
	writeMediaItems(c, out, len(out))
	return true
}
