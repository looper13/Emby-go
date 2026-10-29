package r18

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestR18APIMovieMetadataTagsAndPreview(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		writeFixtureResponse(w, map[string]any{"movie": map[string]any{
			"id": "movie001", "number": "TEST-001",
			"tags": []map[string]string{
				{"id": "tag001", "name": " 剧情 "}, {"id": "tag002", "name": "剧情"},
				{"id": "tag003", "name": " \t "}, {"id": "tag004", "name": "Tag  Two"},
			},
			"has_preview_video": true, "preview_video_url": " https://preview.invalid/playlist.m3u8?token=fixture-signed ",
			"preview_video_proxy_url": "/api/v1/javdb/movies/movie001/preview.m3u8",
		}})
	})
	video, err := client.Get(context.Background(), EncodeID("movie001", "javdb"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(video.Tags, []string{"剧情", "Tag Two"}) || !video.HasPreview || video.PreviewURL != "https://preview.invalid/playlist.m3u8?token=fixture-signed" {
		t.Fatal("真实详情对象标签或预览信号映射错误", video.Tags, video.HasPreview)
	}
}

func TestR18APIMovieMetadataTagsBounded(t *testing.T) {
	longName := strings.Repeat("界", MaxMovieTagRunes+20)
	tags := []map[string]string{{"name": longName}, {"name": longName + "后缀"}}
	for i := 0; i < MaxMovieTags+10; i++ {
		tags = append(tags, map[string]string{"name": fmt.Sprintf("Tag %d", i)})
	}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		writeFixtureResponse(w, map[string]any{"movie": map[string]any{"id": "movie001", "number": "TEST-001", "tags": tags}})
	})
	video, err := client.Get(context.Background(), EncodeID("movie001", "javdb"))
	if err != nil || len(video.Tags) != MaxMovieTags || len([]rune(video.Tags[0])) != MaxMovieTagRunes || !utf8.ValidString(video.Tags[0]) || video.Tags[1] != "Tag 0" {
		t.Fatal("影片标签没有按字符裁剪、裁剪后去重或限制数量", err)
	}
}

func TestR18APIMovieMetadataPreviewSignalAndSafeURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		flag bool
		raw  string
		want string
	}{
		{"https", true, "https://preview.invalid/master.m3u8?token=fixture", "https://preview.invalid/master.m3u8?token=fixture"},
		{"http", true, "http://preview.invalid/master.m3u8", "http://preview.invalid/master.m3u8"},
		{"missing_original", true, "", ""},
		{"explicit_false", false, "https://preview.invalid/master.m3u8", "https://preview.invalid/master.m3u8"},
		{"credentials", true, "https://user:fixture@preview.invalid/master.m3u8", ""},
		{"relative", true, "/api/v1/javdb/movies/movie001/preview.m3u8", ""},
		{"scheme_relative", true, "//preview.invalid/master.m3u8", ""},
		{"javascript", true, "javascript:alert(1)", ""},
		{"file", true, "file:///private/preview.m3u8", ""},
		{"control", true, "https://preview.invalid/master.m3u8\ninvalid", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				writeFixtureResponse(w, map[string]any{"movie": map[string]any{
					"id": "movie001", "number": "TEST-001", "has_preview_video": tc.flag, "preview_video_url": tc.raw,
					"preview_video_proxy_url": "/api/v1/javdb/movies/movie001/preview.m3u8",
				}})
			})
			video, err := client.Get(context.Background(), EncodeID("movie001", "javdb"))
			if err != nil || video.HasPreview != tc.flag || video.PreviewURL != tc.want {
				t.Fatal("预览能力必须使用明确布尔信号，原地址必须独立安全清洗", err)
			}
		})
	}
}

func TestR18APIMovieMetadataSparseListFieldsAreOptional(t *testing.T) {
	for _, nullFields := range []bool{false, true} {
		t.Run(fmt.Sprintf("null_%t", nullFields), func(t *testing.T) {
			movie := map[string]any{"id": "movie001", "number": "TEST-001"}
			if nullFields {
				movie["tags"], movie["has_preview_video"], movie["preview_video_url"] = nil, nil, nil
			}
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				writeFixtureResponse(w, map[string]any{"movies": []any{movie}, "current_page": 1, "has_next": false})
			})
			page, err := client.List(context.Background(), Query{})
			if err != nil || len(page.Items) != 1 {
				t.Fatal("缺少详情标签与预览字段不应使列表失败", err)
			}
			video := page.Items[0]
			if video.Tags == nil || len(video.Tags) != 0 || video.HasPreview || video.PreviewURL != "" {
				t.Fatal("稀疏字段未返回空标签与无预览默认值")
			}
			data, err := json.Marshal(video)
			if err != nil || !strings.Contains(string(data), `"tags":[]`) || !strings.Contains(string(data), `"has_preview":false`) {
				t.Fatal("标签或预览 JSON 契约不稳定", err)
			}
		})
	}
}

func TestR18APIMovieMetadataTagsDoNotMixResourceTags(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/magnets") {
			writeFixtureResponse(w, map[string]any{"online_magnets": []map[string]any{{
				"name": "Fixture", "number": "TEST-001", "magnet_url": "magnet:?xt=urn:btih:" + strings.Repeat("a", 40), "tags": []string{"HD", "中文字幕"},
			}}, "library_magnets": []any{}})
			return
		}
		writeFixtureResponse(w, map[string]any{"movie": map[string]any{
			"id": "movie001", "number": "TEST-001", "tags": []map[string]string{{"id": "tag001", "name": "剧情"}},
		}})
	})
	video, err := client.Get(context.Background(), EncodeID("movie001", "javdb"))
	if err != nil {
		t.Fatal(err)
	}
	resources, err := client.Resources(context.Background(), video, 0)
	if err != nil || len(resources.Items) != 1 || !reflect.DeepEqual(video.Tags, []string{"剧情"}) || !reflect.DeepEqual(resources.Items[0].Tags, []string{"HD", "中文字幕"}) {
		t.Fatal("影片题材标签与资源编码、字幕标签发生混合", err)
	}
}
