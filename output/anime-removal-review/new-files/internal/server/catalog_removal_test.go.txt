package server

import (
	"net/http"
	"testing"
)

func TestRemovedOnlineCatalogEndpoints(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/anime/bangumi"},
		{http.MethodPost, "/api/admin/anime/archive"},
		{http.MethodPut, "/api/admin/anime/tmdb/settings"},
		{http.MethodGet, "/api/admin/anime/entries/1/releases"},
		{http.MethodGet, "/api/anime/images/1"},
		{http.MethodGet, "/api/anime/subjects/1/image"},
		{http.MethodGet, "/api/anime/episodes/1/stills/1/0"},
	} {
		if response := accountRequest(a, route.method, route.path, token, `{}`); response.Code != http.StatusNotFound {
			t.Errorf("退役接口仍可访问：%s %s，状态 %d", route.method, route.path, response.Code)
		}
	}
}

func TestRemovedOnlineCatalogSchedulesRejected(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	for _, kind := range []string{"anime_sync", "anime_archive"} {
		body := `{"name":"已移除的资料同步","type":"` + kind + `","cron":"@daily","enabled":true}`
		if response := accountRequest(a, http.MethodPost, "/api/admin/scheduled", token, body); response.Code != http.StatusBadRequest {
			t.Errorf("退役任务类型仍可创建：%s，状态 %d", kind, response.Code)
		}
	}
	tasks, err := a.db.ScheduledTasks()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("无效任务不应落库：%v，错误 %v", tasks, err)
	}
}
