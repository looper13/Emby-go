package r18

import (
	"context"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestR18APIResourcesOnlineStableHashesAndOwnership(t *testing.T) {
	hash := strings.Repeat("ab", 20)
	decoded, _ := hex.DecodeString(hash)
	base32Hash := base32.StdEncoding.EncodeToString(decoded)
	rows := []apiMagnet{
		{Name: "Fixture first", MagnetURL: "magnet:?xt=urn:btih:" + strings.ToUpper(hash), Number: "test_001", Size: -1, SizeBytes: -1, CNSub: true},
		{Name: "Fixture duplicate", MagnetURL: "magnet:?xt=urn:btih:" + hash},
		{Name: "Fixture base32 duplicate", MagnetURL: "magnet:?xt=urn:btih:" + base32Hash},
		{Name: "Other film", Number: "OTHER-001", MagnetURL: "magnet:?xt=urn:btih:" + strings.Repeat("b", 40)},
		{Name: "Invalid protocol", MagnetURL: "ed2k://invalid"},
		{Name: "Fixture second", MagnetURL: "magnet:?xt=urn:btih:" + strings.Repeat("c", 40), Size: 2048.75, SizeBytes: 2147483648, Tags: []string{"fixture"}, HD: true},
	}
	hits := 0
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		hits++
		if req.URL.Path != "/api/v1/javdb/movies/movie001/magnets" || req.URL.Query().Get("number") != "Test-001" {
			t.Error("资源请求丢失影片归属或番号")
		}
		writeFixtureResponse(w, map[string]any{"online_magnets": rows, "magnets": []apiMagnet{{Name: "Merged only", MagnetURL: "magnet:?xt=urn:btih:" + strings.Repeat("d", 40)}}})
	})
	video := Video{ID: EncodeID("movie001", "javdb"), Number: "Test-001"}
	page, err := client.Resources(context.Background(), video, 0)
	if err != nil || len(page.Items) != 2 || page.HasMore || page.NextOffset != 2 {
		t.Fatal("在线资源过滤或分页不正确", err)
	}
	first, second := page.Items[0], page.Items[1]
	if first.ID != "btih-"+hash || first.SizeMB != 0 || first.SizeBytes != 0 || first.Website != "JavDB 在线" || !reflect.DeepEqual(first.Origins, []string{"online"}) || first.Tags == nil || len(first.Tags) != 1 || second.SizeMB != 2048 || second.SizeBytes != 2147483648 || second.ID != "btih-"+strings.Repeat("c", 40) {
		t.Fatal("大小写/Base32 去重、非负大小或元数据映射错误")
	}
	resource, err := client.Resource(context.Background(), video, first.ID)
	if err != nil || resource.ID != first.ID || hits != 2 {
		t.Fatal("单资源查询没有重新核验来源和归属", err, hits)
	}
	rows = []apiMagnet{}
	if _, err := client.Resource(context.Background(), video, first.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("已经消失的资源仍可提交", err)
	}
	before := hits
	for _, invalid := range []string{"", "77", "forged", "0", "-1"} {
		if _, err := client.Resource(context.Background(), video, invalid); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("非法资源 ID 被接受", err)
		}
	}
	if _, err := client.Resources(context.Background(), Video{ID: EncodeID("movie001", "digital"), Number: "Test-001"}, 0); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("旧来源身份访问在线资源", err)
	}
	if hits != before {
		t.Fatal("非法资源或影片身份触发了上游请求")
	}
}

func TestR18APIResourcesDoNotFallbackToMerged(t *testing.T) {
	for _, data := range []map[string]any{
		{"magnets": []apiMagnet{{MagnetURL: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}}},
		{"online_magnets": nil, "magnets": []apiMagnet{}},
		{"library_magnets": nil, "magnets": []apiMagnet{}},
		{"online_magnets": nil, "library_magnets": nil},
		{"online_magnets": map[string]any{}, "library_magnets": []apiMagnet{}},
		{"online_magnets": []apiMagnet{}, "library_magnets": "invalid"},
	} {
		client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) { writeFixtureResponse(w, data) })
		if _, err := client.Resources(context.Background(), Video{ID: EncodeID("movie001", "javdb"), Number: "TEST-001"}, 0); err == nil {
			t.Fatal("缺少专用列表或类型无效时错误回退到合并资源")
		}
	}
}

func TestR18APIResourcesDedicatedArrayPresence(t *testing.T) {
	row := apiMagnet{Name: "Fixture resource", MagnetURL: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}
	for _, tc := range []struct {
		name    string
		data    map[string]any
		origins []string
	}{
		{"online_only", map[string]any{"online_magnets": []apiMagnet{row}}, []string{"online"}},
		{"library_only", map[string]any{"library_magnets": []apiMagnet{row}}, []string{"library"}},
		{"online_null", map[string]any{"online_magnets": nil, "library_magnets": []apiMagnet{row}}, []string{"library"}},
		{"online_empty", map[string]any{"online_magnets": []apiMagnet{}, "library_magnets": []apiMagnet{row}}, []string{"library"}},
		{"library_null", map[string]any{"online_magnets": []apiMagnet{row}, "library_magnets": nil}, []string{"online"}},
		{"library_empty", map[string]any{"online_magnets": []apiMagnet{row}, "library_magnets": []apiMagnet{}}, []string{"online"}},
		{"both_empty", map[string]any{"online_magnets": []apiMagnet{}, "library_magnets": []apiMagnet{}}, nil},
		{"empty_online_no_combined_fallback", map[string]any{"online_magnets": []apiMagnet{}, "magnets": []apiMagnet{row}}, nil},
		{"empty_library_no_combined_fallback", map[string]any{"library_magnets": []apiMagnet{}, "magnets": []apiMagnet{row}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) { writeFixtureResponse(w, tc.data) })
			page, err := client.Resources(context.Background(), Video{ID: EncodeID("movie001", "javdb"), Number: "TEST-001"}, 0)
			if err != nil || page.HasMore || page.NextOffset != len(page.Items) {
				t.Fatal("有效专用数组没有返回正确资源页", err)
			}
			if tc.origins == nil {
				if len(page.Items) != 0 || page.Items == nil {
					t.Fatal("空专用数组回退了合并资源或返回 null")
				}
				return
			}
			if len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0].Origins, tc.origins) {
				t.Fatal("专用数组来源标记丢失")
			}
			website := "JavDB 在线"
			if tc.origins[0] == "library" {
				website = "AVdb 本地资源库"
			}
			if page.Items[0].Website != website {
				t.Fatal("资源展示来源与专用数组不一致")
			}
		})
	}
}

func TestR18APIResourcesMergeOriginsAndRevalidateStableIdentity(t *testing.T) {
	hash := strings.Repeat("ab", 20)
	decoded, _ := hex.DecodeString(hash)
	base32Hash := base32.StdEncoding.EncodeToString(decoded)
	btmh := "1220" + strings.Repeat("d", 64)
	localHash := strings.Repeat("c", 40)
	online := []apiMagnet{
		{Name: "Fixture online", MagnetURL: "magnet:?xt=urn:btih:" + strings.ToUpper(hash), Number: "TEST-001"},
		{Name: "Fixture online duplicate", MagnetURL: "magnet:?xt=urn:btih:" + hash},
		{Name: "Fixture hybrid hash", MagnetURL: "magnet:?xt=urn:btmh:" + strings.ToUpper(btmh)},
	}
	library := []apiMagnet{
		{Name: "Fixture local duplicate", MagnetURL: "magnet:?xt=urn:btih:" + base32Hash + "&dn=Fixture", Number: "test_001"},
		{Name: "Fixture local duplicate again", MagnetURL: "magnet:?xt=urn:btih:" + hash},
		{Name: "Fixture local hybrid duplicate", MagnetURL: "magnet:?xt=urn:btmh:" + btmh},
		{Name: "Fixture local", MagnetURL: "magnet:?xt=urn:btih:" + localHash, Number: "test 001", UHD: true},
		{Name: "Other film", MagnetURL: "magnet:?xt=urn:btih:" + strings.Repeat("e", 40), Number: "OTHER-001"},
		{Name: "Other film duplicate", MagnetURL: "magnet:?xt=urn:btih:" + hash, Number: "OTHER-001"},
		{Name: "Invalid protocol", MagnetURL: "https://files.invalid/test.strm"},
	}
	hits := 0
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		hits++
		if req.URL.Path != "/api/v1/javdb/movies/movie001/magnets" || req.URL.Query().Get("number") != "TEST-001" {
			t.Error("双来源资源查询丢失当前影片身份")
		}
		writeFixtureResponse(w, map[string]any{"online_magnets": online, "library_magnets": library,
			"magnets": []apiMagnet{{Name: "Combined only", MagnetURL: "magnet:?xt=urn:btih:" + strings.Repeat("f", 40)}}})
	})
	video := Video{ID: EncodeID("movie001", "javdb"), Number: "TEST-001"}
	page, err := client.Resources(context.Background(), video, 0)
	if err != nil || len(page.Items) != 3 || page.NextOffset != 3 || page.HasMore {
		t.Fatal("双来源规范哈希去重或资源归属过滤错误", err)
	}
	for index, expectedID := range []string{"btih-" + hash, "btmh-" + btmh, "btih-" + localHash} {
		r := page.Items[index]
		origins, website := []string{"online", "library"}, "JavDB 在线 / AVdb 本地资源库"
		if index == 2 {
			origins, website = []string{"library"}, "AVdb 本地资源库"
		}
		if r.ID != expectedID || !reflect.DeepEqual(r.Origins, origins) || r.Website != website {
			t.Fatal("资源 ID、双来源顺序或展示标签不稳定")
		}
		resolved, err := client.Resource(context.Background(), video, r.ID)
		if err != nil || !reflect.DeepEqual(resolved, r) {
			t.Fatal("单资源复查没有采用同样的双来源规则", err)
		}
	}
	if hits != 4 {
		t.Fatal("单资源复查没有重新读取上游专用列表")
	}
	online = []apiMagnet{}
	resource, err := client.Resource(context.Background(), video, "btih-"+hash)
	if err != nil || resource.ID != "btih-"+hash || !reflect.DeepEqual(resource.Origins, []string{"library"}) {
		t.Fatal("在线来源移除后本地来源的稳定资源 ID 失效", err)
	}
	library = []apiMagnet{}
	if _, err := client.Resource(context.Background(), video, "btih-"+hash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("两个专用来源已移除的资源仍可提交", err)
	}
}

func TestR18APIResourcesDoNotAssertLocalPlayback(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/magnets") {
			writeFixtureResponse(w, map[string]any{"library_magnets": []map[string]any{{
				"name": "Fixture local", "magnet_url": "magnet:?xt=urn:btih:" + strings.Repeat("a", 40),
				"downloaded": true, "has_local": true, "playable": true,
			}}})
			return
		}
		movie := map[string]any{"id": "movie001", "number": "TEST-001", "title": "Fixture film", "has_local": true}
		if req.URL.Path == "/api/v1/javdb/movies/movie001" {
			writeFixtureResponse(w, map[string]any{"movie": movie})
		} else {
			writeFixtureResponse(w, map[string]any{"movies": []any{movie}, "has_next": false})
		}
	})
	video, err := client.Get(context.Background(), EncodeID("movie001", "javdb"))
	if err != nil || video.HasLocal {
		t.Fatal("上游影片状态绕过服务器本地 STRM 校验", err)
	}
	list, err := client.List(context.Background(), Query{})
	if err != nil || len(list.Items) != 1 || list.Items[0].HasLocal {
		t.Fatal("上游列表状态被直接视为本地可播放", err)
	}
	page, err := client.Resources(context.Background(), video, 0)
	if err != nil || len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0].Origins, []string{"library"}) {
		t.Fatal("本地资源库专用字段未正确映射", err)
	}
	body, err := json.Marshal(page.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"downloaded", "has_local", "playable", "path", "play_url"} {
		if _, exists := fields[key]; exists {
			t.Fatal("磁力资源返回了未经验证的本地播放状态", key)
		}
	}
}

func TestR18APILibraryResourcesRealFieldContractAndFlags(t *testing.T) {
	hash := strings.Repeat("a", 40)
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		writeFixtureResponse(w, map[string]any{"library_magnets": []map[string]any{{
			"name": "Fixture library resource", "magnet_url": "magnet:?xt=urn:btih:" + hash,
			"number": "TEST-001", "created_at": "2026-01-03T04:05:06Z",
			"tags":  []string{"fixture", " HD ", "中文字幕", "fixture", "", "4K", "破解"},
			"cnsub": true, "hd": true, "uhd": true, "uncensored": true,
			"size": 1024.5, "size_bytes": int64(1074266112),
		}}})
	})
	page, err := client.Resources(context.Background(), Video{ID: EncodeID("movie001", "javdb"), Number: "TEST-001"}, 0)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("本地资源库字段类型没有兼容真实接口契约", err)
	}
	r := page.Items[0]
	if r.Title != "Fixture library resource" || r.Number != "TEST-001" || r.Date != "2026-01-03T04:05:06Z" || r.SizeMB != 1024 || r.SizeBytes != 1074266112 ||
		r.ID != "btih-"+hash || r.Website != "AVdb 本地资源库" || !reflect.DeepEqual(r.Origins, []string{"library"}) ||
		!reflect.DeepEqual(r.Tags, []string{"fixture", "HD", "中文字幕", "4K", "破解"}) {
		t.Fatal("本地资源字段映射或标签去重不正确")
	}
}

func TestR18APIResourceDuplicateOriginsMergeDistinctTagsAndFlags(t *testing.T) {
	hash := strings.Repeat("a", 40)
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		writeFixtureResponse(w, map[string]any{
			"online_magnets": []apiMagnet{
				{Name: "Fixture online resource", MagnetURL: "magnet:?xt=urn:btih:" + hash, Tags: []string{"fixture", "HD"}, HD: true},
				{MagnetURL: "magnet:?xt=urn:btih:" + hash, Tags: []string{"HD"}, CNSub: true},
			},
			"library_magnets": []apiMagnet{
				{MagnetURL: "magnet:?xt=urn:btih:" + strings.ToUpper(hash), Tags: []string{"fixture", "中文字幕"}, UHD: true, Uncensored: true},
				{MagnetURL: "magnet:?xt=urn:btih:" + hash, Tags: []string{"4K", "破解"}},
			},
		})
	})
	page, err := client.Resources(context.Background(), Video{ID: EncodeID("movie001", "javdb"), Number: "TEST-001"}, 0)
	if err != nil || len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0].Tags, []string{"fixture", "HD", "中文字幕", "4K", "破解"}) ||
		!reflect.DeepEqual(page.Items[0].Origins, []string{"online", "library"}) {
		t.Fatal("同哈希跨来源合并丢失属性或重复了标签", err)
	}
}

func TestR18APIResourceOffsetPagination(t *testing.T) {
	rows := []apiMagnet{}
	for i := 0; i < 75; i++ {
		rows = append(rows, apiMagnet{Name: "Fixture resource", MagnetURL: "magnet:?xt=urn:btih:" + fmt.Sprintf("%040x", i+1)})
	}
	hits := 0
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		hits++
		writeFixtureResponse(w, map[string]any{"online_magnets": rows[:50], "library_magnets": append([]apiMagnet{rows[0]}, rows[50:]...)})
	})
	video := Video{ID: EncodeID("movie001", "javdb"), Number: "TEST-001"}
	for _, tc := range []struct {
		start, size, end int
		more             bool
	}{{0, 50, 50, true}, {49, 26, 75, false}, {50, 25, 75, false}, {75, 0, 75, false}, {10000, 0, 75, false}} {
		page, err := client.Resources(context.Background(), video, tc.start)
		if err != nil || len(page.Items) != tc.size || page.NextOffset != tc.end || page.HasMore != tc.more {
			t.Fatal("任意偏移或50条边界分页错误", tc, err)
		}
	}
	before := hits
	for _, offset := range []int{-1, 10001} {
		if _, err := client.Resources(context.Background(), video, offset); err == nil {
			t.Fatal("非法资源偏移被接受")
		}
	}
	if hits != before {
		t.Fatal("非法偏移触发上游请求")
	}
}
