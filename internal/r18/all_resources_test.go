package r18

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestR18APIAllResourcesSingleReadAndFreshRevalidation(t *testing.T) {
	rows := []apiMagnet{}
	for index := 1; index <= 121; index++ {
		rows = append(rows, apiMagnet{Name: fmt.Sprintf("完整资源版本 %d", index), Number: "TEST-001", MagnetURL: fmt.Sprintf("magnet:?xt=urn:btih:%040x", index)})
	}
	var hits atomic.Int64
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		hits.Add(1)
		if req.URL.Path != "/api/v1/javdb/movies/movie001/magnets" || req.URL.Query().Get("number") != "TEST-001" {
			t.Error("完整资源读取丢失当前影片身份")
		}
		writeFixtureResponse(w, map[string]any{"online_magnets": rows, "library_magnets": []apiMagnet{
			{Name: "同哈希本地来源", MagnetURL: "magnet:?xt=urn:btih:" + fmt.Sprintf("%040x", 1)},
		}})
	})
	video := Video{ID: EncodeID("movie001", "javdb"), Number: "TEST-001"}
	all, err := client.AllResources(context.Background(), video)
	if err != nil || len(all) != 121 || hits.Load() != 1 || len(all[0].Origins) != 2 || all[120].ID != fmt.Sprintf("btih-%040x", 121) {
		t.Fatal("完整接口重复请求、截断资源或改变既有去重规则", len(all), hits.Load(), err)
	}
	page, err := client.Resources(context.Background(), video, 50)
	if err != nil || len(page.Items) != 50 || page.NextOffset != 100 || !page.HasMore || hits.Load() != 2 {
		t.Fatal("新增完整接口改变了既有分页合同", hits.Load(), err)
	}
	resource, err := client.Resource(context.Background(), video, all[120].ID)
	if err != nil || resource.ID != all[120].ID || hits.Load() != 3 {
		t.Fatal("单资源身份核验被完整读取缓存代替", hits.Load(), err)
	}
	rows = rows[:120]
	if _, err := client.Resource(context.Background(), video, all[120].ID); !errors.Is(err, sql.ErrNoRows) || hits.Load() != 4 {
		t.Fatal("已下架资源没有在提交前重新核验", hits.Load(), err)
	}
}

func TestR18APIAllResourcesValidationAndDedicatedArrayErrors(t *testing.T) {
	var hits atomic.Int64
	data := map[string]any{"magnets": []apiMagnet{{MagnetURL: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}}}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		hits.Add(1)
		writeFixtureResponse(w, data)
	})
	video := Video{ID: EncodeID("movie001", "javdb"), Number: "TEST-001"}
	if _, err := client.AllResources(context.Background(), video); err == nil || hits.Load() != 1 {
		t.Fatal("完整资源接口错误回退到非专用合并磁力数组", err)
	}
	data = map[string]any{"online_magnets": []apiMagnet{}, "library_magnets": []apiMagnet{}}
	if items, err := client.AllResources(context.Background(), video); err != nil || items == nil || len(items) != 0 || hits.Load() != 2 {
		t.Fatal("完整资源接口没有正确返回有效空列表", items, err)
	}
	for _, invalid := range []Video{{ID: "invalid", Number: "TEST-001"}, {ID: EncodeID("movie001", "digital"), Number: "TEST-001"}, {ID: video.ID}} {
		if _, err := client.AllResources(context.Background(), invalid); !errors.Is(err, sql.ErrNoRows) || hits.Load() != 2 {
			t.Fatal("非法影片身份触发了上游完整资源读取", hits.Load(), err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.AllResources(ctx, video); !errors.Is(err, context.Canceled) || hits.Load() != 2 {
		t.Fatal("已取消请求仍读取上游完整资源", hits.Load(), err)
	}
}
