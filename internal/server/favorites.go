package server

import (
	"cmp"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func normalizeEmbyQuery(c *gin.Context) {
	query := c.Request.URL.Query()
	for _, canonical := range []string{"UserId", "ParentId", "IncludeItemTypes", "ExcludeItemTypes", "IsFavorite", "Filters", "SortBy", "SortOrder", "SearchTerm", "StartIndex", "Limit", "EnableImages", "Recursive", "Ids", "NameStartsWith", "NameStartsWithOrGreater", "NameLessThan", "Genres", "GenreIds", "Tags", "TagIds", "Studios", "StudioIds", "Person", "PersonIds", "Years", "OfficialRatings", "MediaTypes"} {
		if _, exists := query[canonical]; exists {
			continue
		}
		for key, values := range query {
			if strings.EqualFold(key, canonical) {
				query[canonical] = values
				break
			}
		}
	}
	c.Request.URL.RawQuery = query.Encode()
}

func favoriteFilter(c *gin.Context) *bool {
	if raw := c.Query("IsFavorite"); raw != "" {
		value := strings.EqualFold(raw, "true")
		return &value
	}
	for _, filter := range splitQueryTypes(c.Query("Filters")) {
		if strings.EqualFold(filter, "IsFavorite") {
			value := true
			return &value
		}
	}
	return nil
}

func validFavoriteQuery(c *gin.Context) bool {
	if raw := c.Query("IsFavorite"); raw != "" && !strings.EqualFold(raw, "true") && !strings.EqualFold(raw, "false") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "IsFavorite must be true or false"})
		return false
	}
	if uid := c.Query("UserId"); uid != "" && func() bool {
		value, ok := c.Get("accountUser")
		return !ok || uid != strconv.FormatInt(value.(store.User).ID, 10)
	}() {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return false
	}
	return true
}

func matchesFavorite(c *gin.Context, value bool) bool {
	filter := favoriteFilter(c)
	return filter == nil || *filter == value
}

func (a *App) itemUserData(id string) gin.H {
	data := zeroUserData()
	data["IsFavorite"] = a.db.ItemFavorite(id)
	data["Key"], data["ItemId"] = id, id
	return data
}

func userDataOf(item gin.H) gin.H {
	if data, ok := item["UserData"].(gin.H); ok {
		return data
	}
	if data, ok := item["UserData"].(store.UserData); ok {
		return gin.H{"IsFavorite": data.IsFavorite, "Played": data.Played, "PlaybackPositionTicks": data.PositionTicks, "PlayCount": data.PlayCount, "LastPlayedDate": data.LastPlayedAt}
	}
	return zeroUserData()
}

func (a *App) namedItem(kind, name string, images bool) gin.H {
	id := entityId(kind, name)
	item := gin.H{"Id": id, "Name": name, "SortName": name, "Type": kind,
		"IsFolder": kind == "Genre" || kind == "Studio" || kind == "Tag", "ServerId": a.serverID,
		"ImageTags": gin.H{}, "BackdropImageTags": []string{}, "UserData": a.itemUserData(id)}
	if images {
		tag := ""
		if kind == "Person" {
			_, tag = a.personAvatar(name)
		}
		if tag == "" {
			if poster := a.entityPosterPath(kind, name); poster != "" {
				tag = a.posterTag(poster)
			}
		}
		if tag != "" {
			item["ImageTags"] = gin.H{"Primary": tag}
		}
	}
	return item
}

func (a *App) namedItemDetail(c *gin.Context, kind string) {
	if !validFavoriteQuery(c) {
		return
	}
	name := c.Param("name")
	exists, err := a.db.EntityExists(kind, name)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
	} else if !exists {
		c.JSON(404, gin.H{"error": "not found"})
	} else {
		c.JSON(200, a.namedItem(kind, name, !strings.EqualFold(c.Query("EnableImages"), "false")))
	}
}

// Resolve only visible, real identities. Decoding a virtual ID is not proof
// that the entity exists. Playable aliases continue sharing the movie row.
func (a *App) favoriteTarget(c *gin.Context, raw string) (string, *store.Movie, gin.H, error) {
	if kind, name, ok := entityKind(raw); ok {
		exists, err := a.db.EntityExists(kind, name)
		if err != nil {
			return "", nil, nil, err
		}
		if !exists {
			return "", nil, nil, sql.ErrNoRows
		}
		return entityId(kind, name), nil, a.namedItem(kind, name, false), nil
	}
	var item gin.H
	if name, ok := parseBoxsetID(raw); ok {
		item = a.boxsetItemDTO(name)
	} else if _, _, _, _, ok := store.ParseTVID(raw); ok {
		item = a.televisionFolder(raw, c)
	} else {
		movie, err := a.resolveUserMedia(raw)
		if err != nil {
			return "", nil, nil, err
		}
		return strconv.FormatInt(movie.ID, 10), &movie, nil, nil
	}
	if item == nil {
		return "", nil, nil, sql.ErrNoRows
	}
	return item["Id"].(string), nil, item, nil
}

// 收藏查询合并电影、电视和其他实体，再统一排序和分页。
func (a *App) favoriteRootItems(c *gin.Context) bool {
	filter := favoriteFilter(c)
	if filter == nil || !*filter || (c.Query("ParentId") != "" && c.Query("ParentId") != "0") {
		return false
	}
	movies, _, err := a.db.SearchScoped(0, "", "", "", "", "", "", "", false, filter, "SortName", false, -1, 0)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return true
	}
	data, err := a.db.DataFor(movieIDs(movies))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return true
	}
	items := []gin.H{}
	seen := map[string]bool{}
	appendItem := func(item gin.H) {
		if item == nil || !favoriteCandidateMatches(c, item) {
			return
		}
		id := item["Id"].(string)
		if !seen[id] {
			seen[id] = true
			items = append(items, item)
		}
	}
	for _, movie := range movies {
		item := a.embyItem(movie, data[movie.ID])
		a.applyItemFields(item, movie, requestedFields(c), c)
		if strings.EqualFold(firstSortKey(c.Query("SortBy")), "DateCreated") {
			item["DateCreated"] = embyTime(movie.CreatedAt)
		}
		appendItem(item)
	}
	for _, raw := range a.db.ItemFavoriteIDs() {
		_, _, item, err := a.favoriteTarget(c, raw)
		if err == nil {
			if kind, name, ok := entityKind(raw); ok {
				item = a.namedItem(kind, name, !strings.EqualFold(c.Query("EnableImages"), "false"))
			}
			appendItem(item)
		} else if !store.NotFound(err) {
			c.JSON(500, gin.H{"error": err.Error()})
			return true
		}
	}
	sortFavoriteItems(c, items)
	writeMediaItems(c, items, len(items))
	return true
}

func sortFavoriteItems(c *gin.Context, items []gin.H) {
	key := strings.ToLower(firstSortKey(c.DefaultQuery("SortBy", "SortName")))
	desc := strings.EqualFold(c.Query("SortOrder"), "Descending")
	text := func(item gin.H, field string) string {
		value, _ := item[field].(string)
		return strings.ToLower(value)
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i], items[j]
		comparison := 0
		switch key {
		case "productionyear", "communityrating", "runtime", "indexnumber":
			field := map[string]string{"productionyear": "ProductionYear", "communityrating": "CommunityRating", "runtime": "RunTimeTicks", "indexnumber": "IndexNumber"}[key]
			comparison = cmp.Compare(mediaNumber(left[field]), mediaNumber(right[field]))
		case "datecreated", "premieredate":
			field := "DateCreated"
			if key == "premieredate" {
				field = "PremiereDate"
			}
			comparison = strings.Compare(text(left, field), text(right, field))
		case "playcount":
			comparison = cmp.Compare(intValue(userDataOf(left)["PlayCount"]), intValue(userDataOf(right)["PlayCount"]))
		case "dateplayed":
			comparison = strings.Compare(text(userDataOf(left), "LastPlayedDate"), text(userDataOf(right), "LastPlayedDate"))
		default:
			comparison = strings.Compare(text(left, "Name"), text(right, "Name"))
		}
		if comparison == 0 {
			comparison = strings.Compare(text(left, "Id"), text(right, "Id"))
		}
		if desc {
			return comparison > 0
		}
		return comparison < 0
	})
}

func favoriteCandidateMatches(c *gin.Context, item gin.H) bool {
	typ, _ := item["Type"].(string)
	if mediaTypes := splitQueryTypes(c.Query("MediaTypes")); len(mediaTypes) > 0 {
		found := false
		for _, value := range mediaTypes {
			found = found || strings.EqualFold(value, fmt.Sprint(item["MediaType"]))
		}
		if !found {
			return false
		}
	}
	typeMatches := func(value string) bool {
		return strings.EqualFold(value, typ) || strings.EqualFold(value, "Video") && (typ == "Movie" || typ == "Episode")
	}
	if include := splitQueryTypes(c.Query("IncludeItemTypes")); len(include) > 0 {
		found := false
		for _, value := range include {
			found = found || typeMatches(value)
		}
		if !found {
			return false
		}
	}
	for _, value := range splitQueryTypes(c.Query("ExcludeItemTypes")) {
		if typeMatches(value) {
			return false
		}
	}
	if ids := splitQueryTypes(c.Query("Ids")); len(ids) > 0 {
		found := false
		for _, id := range ids {
			found = found || id == item["Id"]
		}
		if !found {
			return false
		}
	}
	if term := strings.ToLower(c.Query("SearchTerm")); term != "" && !strings.Contains(strings.ToLower(fmt.Sprint(item["Name"], " ", item["OriginalTitle"], " ", item["SeriesName"])), term) {
		return false
	}
	if years := splitQueryTypes(c.Query("Years")); len(years) > 0 {
		found := false
		for _, year := range years {
			found = found || year == strconv.Itoa(intValue(item["ProductionYear"]))
		}
		if !found {
			return false
		}
	}
	for _, entry := range []struct{ param, kind, ids, field string }{
		{"Genres", "Genre", "GenreIds", "Genres"}, {"Tags", "Tag", "TagIds", "Tags"},
		{"Studios", "Studio", "StudioIds", "Studios"}, {"Person", "Person", "PersonIds", "People"},
	} {
		wanted := joinEntityNames(c, entry.param, entry.kind, entry.ids)
		if wanted == "" {
			continue
		}
		values := []string{}
		switch rows := item[entry.field].(type) {
		case []string:
			values = rows
		case []gin.H:
			for _, row := range rows {
				name, _ := row["Name"].(string)
				values = append(values, name)
			}
		}
		found := false
		for _, name := range strings.FieldsFunc(wanted, func(r rune) bool { return r == ',' || r == '|' }) {
			for _, value := range values {
				found = found || strings.EqualFold(strings.TrimSpace(name), value)
			}
		}
		if !found {
			return false
		}
	}
	if ratings := splitQueryTypes(c.Query("OfficialRatings")); len(ratings) > 0 {
		found := false
		for _, rating := range ratings {
			found = found || rating == item["OfficialRating"]
		}
		if !found {
			return false
		}
	}
	data := userDataOf(item)
	for _, filter := range splitQueryTypes(c.Query("Filters")) {
		switch strings.ToLower(filter) {
		case "isplayed":
			if data["Played"] != true {
				return false
			}
		case "isunplayed":
			if data["Played"] == true {
				return false
			}
		case "isfolder":
			if item["IsFolder"] != true {
				return false
			}
		case "isnotfolder":
			if item["IsFolder"] == true {
				return false
			}
		}
	}
	return matchesFavorite(c, data["IsFavorite"] == true)
}
