package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cache"
	"emby-go/internal/config"
	"emby-go/internal/store"
)

func librarySettingsApp(t *testing.T) (*App, string) {
	t.Helper()
	a, err := newApp(config.Config{DBPath: filepath.Join(t.TempDir(), "libraries.db"), DisableLibraryMonitor: true}, cache.NewMemory(512))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if err := a.db.InitializeAdministrator("admin", "password-1234"); err != nil {
		t.Fatal(err)
	}
	a.setAdminName("admin")
	return a, accountLogin(t, a)
}

func librarySettingsJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAdminLibraryDirectoriesAndHeaders(t *testing.T) {
	a, token := librarySettingsApp(t)
	first, second := t.TempDir(), t.TempDir()
	for index, root := range []string{first, second} {
		writeFile(t, filepath.Join(root, "film.strm"), "https://media.test/film.mp4\n")
		writeFile(t, filepath.Join(root, "film.nfo"), `<movie><title>目录 `+strconv.Itoa(index+1)+`</title></movie>`)
	}
	headers := map[string]string{"referer": "https://source.test/watch/", "origin": "https://source.test", "User-Agent": "媒体库测试", "Cookie": "fixture=value"}
	w := accountRequest(a, "POST", "/api/admin/libraries", token, librarySettingsJSON(t, map[string]any{
		"Name": " 多目录库 ", "Paths": []string{first, second, first}, "Type": "movies", "ProxyEnabled": true, "ResolveRedirects": true, "ProxyHeaders": headers,
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("创建多目录库：%d %s", w.Code, w.Body)
	}
	var lib store.Library
	if err := json.Unmarshal(w.Body.Bytes(), &lib); err != nil {
		t.Fatal(err)
	}
	if lib.Name != "多目录库" || lib.Path != first || !reflect.DeepEqual(lib.Paths, []string{first, second}) || lib.ProxyHeaders["Referer"] != headers["referer"] || lib.ProxyHeaders["Origin"] != headers["origin"] {
		t.Fatalf("目录及请求头未规范化：%+v", lib)
	}
	endpoint := "/api/admin/libraries/" + strconv.FormatInt(lib.ID, 10)
	w = accountRequest(a, "POST", "/api/admin/scan", token, librarySettingsJSON(t, map[string]any{"library_id": lib.ID}))
	if w.Code != http.StatusOK {
		t.Fatalf("扫描多目录库：%d %s", w.Code, w.Body)
	}
	retainedID, err := a.db.MovieIDByPath(filepath.Join(first, "film.strm"))
	if err != nil {
		t.Fatal(err)
	}
	removedID, err := a.db.MovieIDByPath(filepath.Join(second, "film.strm"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.SavePlayback(retainedID, 234567, 3, time.Now().UTC().Format(time.RFC3339), 234567); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetFavorite(retainedID, true); err != nil {
		t.Fatal(err)
	}
	retainedData, err := a.db.Data(retainedID)
	if err != nil {
		t.Fatal(err)
	}

	w = accountRequest(a, "GET", "/Library/VirtualFolders", token, "")
	var virtual []struct {
		Locations []string
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &virtual) != nil || len(virtual) != 1 || !reflect.DeepEqual(virtual[0].Locations, []string{first, second}) {
		t.Fatalf("Emby 目录列表：%d %s", w.Code, w.Body)
	}

	// 单独保存请求头不会覆盖其他播放设置、目录或媒体库授权。
	newHeaders := map[string]string{"Referer": "https://source.test/updated/", "Origin": "https://source.test"}
	w = accountRequest(a, "PUT", endpoint, token, librarySettingsJSON(t, map[string]any{"ProxyHeaders": newHeaders, "Revision": lib.Revision}))
	if w.Code != http.StatusOK {
		t.Fatalf("部分更新请求头：%d %s", w.Code, w.Body)
	}
	oldRevision := lib.Revision
	lib = store.Library{}
	if err := json.Unmarshal(w.Body.Bytes(), &lib); err != nil {
		t.Fatal(err)
	}
	if !lib.ProxyEnabled || !lib.ResolveRedirects || !reflect.DeepEqual(lib.Paths, []string{first, second}) || !reflect.DeepEqual(lib.ProxyHeaders, newHeaders) {
		t.Fatalf("部分更新覆盖了未提供的设置：%+v", lib)
	}
	w = accountRequest(a, "PUT", endpoint, token, librarySettingsJSON(t, map[string]any{"Paths": []string{first}, "Revision": oldRevision}))
	if w.Code != http.StatusConflict {
		t.Fatalf("过期版本没有拒绝：%d %s", w.Code, w.Body)
	}
	if !a.beginScan(1) {
		t.Fatal("无法建立扫描互斥夹具")
	}
	w = accountRequest(a, "PUT", endpoint, token, librarySettingsJSON(t, map[string]any{"Paths": []string{first}, "Revision": lib.Revision}))
	a.endScan(nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("扫描期间修改目录未拒绝：%d %s", w.Code, w.Body)
	}

	viewerID, viewerToken := createViewer(t, a, token, "library-viewer")
	w = accountRequest(a, "POST", "/Users/"+viewerID+"/Policy", token, librarySettingsJSON(t, map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{externalLibraryID(lib.ID)}}))
	if w.Code != http.StatusNoContent {
		t.Fatalf("媒体库授权：%d %s", w.Code, w.Body)
	}
	w = accountRequest(a, "GET", "/api/browse/libraries", viewerToken, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "多目录库") || strings.Contains(w.Body.String(), "ProxyHeaders") || strings.Contains(w.Body.String(), "source.test") || strings.Contains(w.Body.String(), filepath.Base(first)) {
		t.Fatalf("浏览接口暴露了目录或源请求头：%d %s", w.Code, w.Body)
	}
	if w = accountRequest(a, "PUT", endpoint, viewerToken, librarySettingsJSON(t, map[string]any{"ProxyEnabled": false, "Revision": lib.Revision})); w.Code != http.StatusForbidden {
		t.Fatalf("普通用户可修改媒体库：%d %s", w.Code, w.Body)
	}

	w = accountRequest(a, "PUT", endpoint, token, librarySettingsJSON(t, map[string]any{"Name": "更新后的库", "Paths": []string{first}, "ProxyHeaders": map[string]string{}, "Revision": lib.Revision}))
	if w.Code != http.StatusOK {
		t.Fatalf("移除目录：%d %s", w.Code, w.Body)
	}
	if _, err := a.db.Movie(removedID); !store.NotFound(err) {
		t.Fatalf("移除根目录后仍有越界索引：%v", err)
	}
	if movie, err := a.db.Movie(retainedID); err != nil || movie.LibraryID != lib.ID {
		t.Fatalf("保留目录的影片改变：%+v %v", movie, err)
	}
	if data, err := a.db.Data(retainedID); err != nil || data != retainedData {
		t.Fatalf("保留影片的进度或收藏改变：%+v %v", data, err)
	}
	if refreshed, err := a.db.Library(lib.ID); err != nil || refreshed.Name != "更新后的库" || len(refreshed.ProxyHeaders) != 0 || !refreshed.ProxyEnabled || !refreshed.ResolveRedirects {
		t.Fatalf("更新后配置：%+v %v", refreshed, err)
	}
}

func TestAdminLibrarySettingsRejectInvalidRequests(t *testing.T) {
	a, token := librarySettingsApp(t)
	root := t.TempDir()
	lib, err := a.db.AddDirectoryLibrary("现有库", []string{root}, store.LibraryMovies, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/admin/libraries/" + strconv.FormatInt(lib.ID, 10)
	for _, test := range []struct {
		name, method, endpoint string
		body                   map[string]any
	}{
		{"空目录列表", "POST", "/api/admin/libraries", map[string]any{"Name": "空目录", "Paths": []string{}}},
		{"跨库重复目录", "POST", "/api/admin/libraries", map[string]any{"Name": "重复目录", "Paths": []string{root}}},
		{"跨库嵌套目录", "POST", "/api/admin/libraries", map[string]any{"Name": "嵌套目录", "Paths": []string{filepath.Join(root, "nested")}}},
		{"清空所有目录", "PUT", endpoint, map[string]any{"Paths": []string{}, "Revision": lib.Revision}},
		{"请求头注入", "PUT", endpoint, map[string]any{"ProxyHeaders": map[string]string{"Referer": "https://source.test/\r\nX-Test: injected"}, "Revision": lib.Revision}},
		{"覆盖代理 Range", "PUT", endpoint, map[string]any{"ProxyHeaders": map[string]string{"Range": "bytes=0-"}, "Revision": lib.Revision}},
		{"缺少版本", "PUT", endpoint, map[string]any{"ProxyEnabled": true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := accountRequest(a, test.method, test.endpoint, token, librarySettingsJSON(t, test.body))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("非法设置没有拒绝：%d %s", w.Code, w.Body)
			}
			if current, err := a.db.Library(lib.ID); err != nil || current.Revision != lib.Revision || !reflect.DeepEqual(current.DirectoryPaths(), []string{root}) {
				t.Fatalf("失败请求改变了媒体库：%+v %v", current, err)
			}
		})
	}
}
