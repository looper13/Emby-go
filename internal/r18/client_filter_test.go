package r18

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestR18APIListAndDetailValidation(t *testing.T) {
	hits := 0
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		hits++
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{fixtureMovie(1)}, "current_page": 1, "has_next": false})
	})
	for _, query := range []Query{
		{Search: strings.Repeat("x", 121)}, {Type: "digital"}, {Filter: "unsupported"}, {Order: "random"},
		{Page: -1}, {Page: 10000}, {Limit: -1}, {Limit: 51}, {Sort: "unsafe"},
		{Year: "0"}, {Year: "10000"}, {Year: "not-a-year"},
		{Search: "Fixture", Sort: "release"}, {Search: "Fixture", Year: "2024"},
	} {
		if ValidateQuery(query) == nil {
			t.Fatal("参数校验接受了非法页码、范围或查询组合")
		}
		if _, err := client.List(context.Background(), query); err == nil {
			t.Fatal("非法查询未被拒绝")
		}
	}
	for _, id := range []string{"bad", EncodeID("movie001", "digital"), EncodeID("../movie", "javdb"), EncodeID(strings.Repeat("x", 121), "javdb")} {
		if _, err := client.Get(context.Background(), id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("非法身份没有在请求前拒绝", err)
		}
	}
	if hits != 0 {
		t.Fatal("无效查询或旧数据库身份访问了上游", hits)
	}
	page, err := client.List(context.Background(), Query{})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != EncodeID("movie001", "javdb") || page.Page != 1 || page.PageSize != 24 || page.HasMore {
		t.Fatal("合法列表未映射为 JavDB 身份", page, err)
	}
}

func TestR18APIListRejectsMalformedPages(t *testing.T) {
	for _, data := range []map[string]any{
		{"movies": nil, "current_page": 1},
		{"movies": []apiMovie{}, "current_page": 2},
		{"movies": "invalid", "current_page": 1},
	} {
		client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) { writeFixtureResponse(w, data) })
		if _, err := client.List(context.Background(), Query{}); err == nil {
			t.Fatal("接受了非法列表结构或错误页码")
		}
	}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		invalid := fixtureMovie(2)
		invalid.Number = " "
		badID := fixtureMovie(3)
		badID.ID = "../invalid"
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{invalid, badID, fixtureMovie(1)}, "current_page": 1, "has_next": false})
	})
	if _, err := client.List(context.Background(), Query{}); err == nil {
		t.Fatal("接受了缺失番号或非法身份的影片，无法安全缓存或保证分页位置")
	}
}

func TestR18APIDetailFullGalleryAndIdentity(t *testing.T) {
	images := []map[string]string{{"large_url": "javascript:bad", "thumb_url": "https://images.invalid/fallback.jpg"}, {"large_url": "https://images.invalid/fallback.jpg"}}
	for i := 0; i < 110; i++ {
		images = append(images, map[string]string{"large_url": fmt.Sprintf("https://images.invalid/gallery-%d.jpg", i)})
	}
	movie := map[string]any{"id": "movie001", "number": "TEST-001", "title": "", "origin_title": "Fixture original", "cover_url": "https://user:secret@images.invalid/cover.jpg", "thumb_url": "https://images.invalid/thumb.jpg", "preview_images": images,
		"duration": 120, "score": 4.5, "magnets_count": 7}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/javdb/movies/movie001" {
			t.Error("详情请求没有使用上游影片 ID")
		}
		writeFixtureResponse(w, map[string]any{"movie": movie})
	})
	video, err := client.Get(context.Background(), EncodeID("movie001", "javdb"))
	if err != nil || video.Title != "Fixture original" || video.CoverURL != "https://images.invalid/thumb.jpg" || len(video.Images) != MaxImages || video.Images[0] != "https://images.invalid/fallback.jpg" || video.ID != EncodeID("movie001", "javdb") {
		t.Fatal("详情身份、标题回退、图片清洗或完整图库上限错误", err)
	}
	if video.Duration != 120 || video.Score != 4.5 || video.MagnetsCount != 7 {
		t.Fatal("详情没有按分钟、原始评分和磁力数量映射展示字段")
	}
	movie["duration"], movie["score"], movie["magnets_count"] = -1, -1, -1
	video, err = client.Get(context.Background(), EncodeID("movie001", "javdb"))
	if err != nil || video.Duration != 0 || video.Score != 0 || video.MagnetsCount != 0 {
		t.Fatal("异常负数时长、评分或磁力数量没有清洗", err)
	}
	for _, changed := range []map[string]any{{"id": "movie002", "number": "TEST-001"}, {"id": "movie001", "number": ""}} {
		movie = changed
		if _, err := client.Get(context.Background(), EncodeID("movie001", "javdb")); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("接受错误影片身份或缺失番号", err)
		}
	}
}
