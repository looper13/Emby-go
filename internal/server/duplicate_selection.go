package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"unicode/utf8"

	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

type signedDuplicateSelection struct {
	store.DuplicateSelectionItem
	Token string `json:"selection_token"`
}

func (a *App) duplicateSelectionToken(scope int64, item store.DuplicateSelectionItem) string {
	type identity struct {
		ID, LibraryID                              int64
		Number, SourcePath, CreatedAt, LibraryPath string
		Parts                                      []string
	}
	snapshot := func(m store.DuplicateSelectionMovie) identity {
		return identity{m.ID, m.LibraryID, m.Number, m.SourcePath, m.CreatedAt, m.LibraryPath, append([]string{}, m.AdditionalParts...)}
	}
	data, _ := json.Marshal([]identity{snapshot(item.Movie), snapshot(item.Keep)})
	mac := hmac.New(sha256.New, a.subtitleSigningKey[:])
	fmt.Fprintf(mac, "duplicate-selection-v1:%d:%s:", scope, item.Key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *App) adminDuplicateSelection(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	libraryID, err := strconv.ParseInt(c.DefaultQuery("library_id", "0"), 10, 64)
	if err != nil || libraryID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的媒体库参数"})
		return
	}
	if libraryID > 0 && !a.db.CanAccessLibrary(libraryID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "媒体库不存在或不可访问"})
		return
	}
	rule := c.Query("rule")
	items, err := a.db.DuplicateMovieSelection(c.Request.Context(), libraryID, rule)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateRule) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的全量选择规则"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "全量选择重复影片失败"})
		}
		return
	}
	out := make([]signedDuplicateSelection, 0, len(items))
	groups := 0
	previous := ""
	for _, item := range items {
		if c.Request.Context().Err() != nil {
			return
		}
		if item.Key != previous {
			groups++
			previous = item.Key
		}
		out = append(out, signedDuplicateSelection{item, a.duplicateSelectionToken(libraryID, item)})
	}
	c.JSON(http.StatusOK, gin.H{"rule": rule, "library_id": libraryID, "items": out, "total": len(out), "groups": groups})
}

// A batch uses fixed target/keeper identities, not a freshly recomputed rule.
// Reject changed or missing keepers even when resuming a partial deletion.
func (a *App) validateDuplicateSelection(id, scope int64, key string, keepID int64, token string) error {
	if keepID == 0 && token == "" {
		return nil
	}
	if keepID <= 0 || keepID == id || len(token) != sha256.Size*2 {
		return errors.New("批量选择凭据无效，请重新全量选择")
	}
	read := func(id int64) (store.DuplicateSelectionMovie, error) {
		movie, err := a.db.Movie(id)
		if err != nil || !a.db.CanAccessLibrary(movie.LibraryID) || scope != 0 && movie.LibraryID != scope || movie.Television != nil || store.NormalizeDuplicateNumber(movie.Number) != key {
			return store.DuplicateSelectionMovie{}, errors.New("待删或保留副本不存在、不可访问或已改变分组，请重新全量选择")
		}
		lib, err := a.db.Library(movie.LibraryID)
		if err != nil {
			return store.DuplicateSelectionMovie{}, errors.New("媒体库不存在或不可访问")
		}
		return store.DuplicateSelectionMovie{DuplicateMovie: store.DuplicateMovie{ID: movie.ID, LibraryID: movie.LibraryID, LibraryName: lib.Name, Number: movie.Number, Title: movie.Title, SourcePath: movie.SourcePath, Status: movie.Status, AdditionalParts: movie.AdditionalParts}, CreatedAt: movie.CreatedAt, PathLength: utf8.RuneCountInString(movie.SourcePath), LibraryPath: lib.Path}, nil
	}
	movie, err := read(id)
	if err != nil {
		return err
	}
	keep, err := read(keepID)
	if err != nil {
		return err
	}
	expected := a.duplicateSelectionToken(scope, store.DuplicateSelectionItem{Key: key, Movie: movie, Keep: keep})
	if !hmac.Equal([]byte(token), []byte(expected)) {
		return errors.New("待删或保留副本的路径、分段或加入时间已变化，请重新全量选择")
	}
	return nil
}
