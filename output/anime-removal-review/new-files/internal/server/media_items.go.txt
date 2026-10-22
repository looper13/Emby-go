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

func sortMediaItems(c *gin.Context, items []gin.H) {
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
			compare = cmp.Compare(mediaNumber(x[field]), mediaNumber(y[field]))
		case "datecreated":
			compare = strings.Compare(fmt.Sprint(x["DateCreated"]), fmt.Sprint(y["DateCreated"]))
		default:
			// 同一剧集中的季和章节按数字编号稳定排序。
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
func mediaNumber(v any) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	return float64(intValue(v))
}
func mediaPageWindow(c *gin.Context) (int, int) {
	start, _ := strconv.Atoi(c.Query("StartIndex"))
	start = max(0, start)
	limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "100"))
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	return start, limit
}
func writeMediaItems(c *gin.Context, items []gin.H, total int) {
	start, limit := mediaPageWindow(c)
	from := min(start, len(items))
	to := from + min(limit, len(items)-from)
	c.JSON(http.StatusOK, gin.H{"Items": items[from:to], "TotalRecordCount": total, "StartIndex": start})
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

func (a *App) resolveUserMedia(raw string) (store.Movie, error) {
	m, _, _, ok := a.resolvePlaybackTarget(raw)
	if !ok || !m.IsVisible() {
		return store.Movie{}, sql.ErrNoRows
	}
	return m, nil
}
