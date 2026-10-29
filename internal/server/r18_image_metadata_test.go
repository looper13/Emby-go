package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	"emby-go/internal/r18"
	"emby-go/internal/store"
)

// 列表刻意不含图片详情，防止同一个完整夹具掩盖缓存降级问题。
type r18ImageMetadataSource struct {
	fakeR18
	listVideo   r18.Video
	detailVideo r18.Video
	listReads   atomic.Int64
	detailReads atomic.Int64
}

func (f *r18ImageMetadataSource) List(ctx context.Context, q r18.Query) (r18.Page, error) {
	if err := ctx.Err(); err != nil {
		return r18.Page{}, err
	}
	f.listReads.Add(1)
	return r18.Page{Items: []r18.Video{f.listVideo}, Page: q.Page, PageSize: q.Limit}, nil
}

func (f *r18ImageMetadataSource) Get(ctx context.Context, _ string) (r18.Video, error) {
	if err := ctx.Err(); err != nil {
		return r18.Video{}, err
	}
	f.detailReads.Add(1)
	return f.detailVideo, nil
}

type r18ImageMetadataFixture struct {
	app          *App
	server       *httptest.Server
	token        string
	library      store.Library
	source       *r18ImageMetadataSource
	base         string
	png          []byte
	coverHits    atomic.Int64
	galleryHits  atomic.Int64
	updatedHits  atomic.Int64
	updatedCover string
}

func newR18ImageMetadataFixture(t *testing.T) *r18ImageMetadataFixture {
	t.Helper()
	f := &r18ImageMetadataFixture{}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	f.png = encoded.Bytes()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/img-proxy/" {
			t.Errorf("图片未使用固定代理接口：%s", req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		for _, header := range []string{"Cookie", "Authorization", "X-API-Key", "X-Emby-Token", "X-MediaBrowser-Token"} {
			if req.Header.Get(header) != "" {
				t.Errorf("图片代理携带凭据：%s", header)
			}
		}
		source, err := url.Parse(req.URL.Query().Get("url"))
		if err != nil || source == nil {
			t.Error("图片代理缺少有效原图参数")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch source.Path {
		case "/cover.png":
			f.coverHits.Add(1)
		case "/gallery.png":
			f.galleryHits.Add(1)
		case "/updated-cover.png":
			f.updatedHits.Add(1)
		default:
			t.Errorf("缓存误用了公开图片路径或未知原图：%s", source.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(f.png)
	}))
	t.Cleanup(upstream.Close)
	f.app, f.server, f.token = newProbeTestApp(t, t.TempDir())
	// 只有临时夹具绕过公网 DNS 校验，不接触实际图片或 API 服务。
	f.app.r18ImageHTTP = upstream.Client()
	var err error
	f.library, err = f.app.db.CreateR18Library("Metadata", r18.Config{BaseURL: upstream.URL, APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	video := r18.Video{ID: r18.EncodeID("metadata", "javdb"), ContentID: "metadata", ServiceCode: "javdb",
		Number: "META-001", Title: "Metadata", CoverURL: upstream.URL + "/cover.png", Images: []string{upstream.URL + "/gallery.png"}}
	f.source = &r18ImageMetadataSource{detailVideo: video, listVideo: video}
	f.source.listVideo.CoverURL = ""
	f.source.listVideo.Images = []string{}
	f.app.r18Open = func(r18.Config) (r18Source, error) { return f.source, nil }
	f.base = "/api/r18/libraries/" + strconv.FormatInt(f.library.ID, 10) + "/videos"
	f.updatedCover = upstream.URL + "/updated-cover.png"
	return f
}

func (f *r18ImageMetadataFixture) get(t *testing.T, path, token string, want int) []byte {
	t.Helper()
	resp, raw := embyRaw(t, f.server, http.MethodGet, path, token, "")
	if resp.StatusCode != want {
		t.Fatalf("GET %s：状态 %d，预期 %d，响应 %s", path, resp.StatusCode, want, raw)
	}
	return raw
}

func TestR18ImageMetadataFullDetailSurvivesSparseList(t *testing.T) {
	f := newR18ImageMetadataFixture(t)
	item := f.base + "/" + f.source.detailVideo.ID
	var detail struct {
		Video r18.Video `json:"video"`
	}
	if err := json.Unmarshal(f.get(t, item, f.token, http.StatusOK), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Video.CoverURL != item+"/images/poster" || len(detail.Video.Images) != 1 || detail.Video.Images[0] != item+"/images/0" {
		t.Fatal("详情未返回受控图片地址", detail.Video)
	}
	f.get(t, f.base, f.token, http.StatusOK)
	for _, kind := range []string{"0", "cover"} {
		if body := f.get(t, item+"/images/"+kind, f.token, http.StatusOK); !bytes.Equal(body, f.png) {
			t.Fatalf("稀疏列表覆盖了完整详情的 %s 图片", kind)
		}
	}
	if f.source.detailReads.Load() != 1 || f.source.listReads.Load() != 1 || f.galleryHits.Load() != 1 || f.coverHits.Load() != 1 {
		t.Fatal("完整详情缓存未复用，或列表清空了图片资料")
	}
	// 完整详情已确认不存在的索引不能反复请求上游资料。
	for range 2 {
		f.get(t, item+"/images/1", f.token, http.StatusNotFound)
	}
	if f.source.detailReads.Load() != 1 || f.galleryHits.Load() != 1 {
		t.Fatal("完整详情的缺失索引重新访问上游")
	}
	// 新列表的有效封面可更新原图，不能继续命中旧封面文件缓存。
	f.source.listVideo.CoverURL = f.updatedCover
	f.get(t, f.base, f.token, http.StatusOK)
	f.get(t, item+"/images/cover", f.token, http.StatusOK)
	f.get(t, item+"/images/cover", f.token, http.StatusOK)
	if f.updatedHits.Load() != 1 || f.coverHits.Load() != 1 || f.source.detailReads.Load() != 1 {
		t.Fatal("列表更新封面后没有按新原图缓存，或重复读取详情")
	}
	user, err := f.app.db.CreateUser("metadata-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	const viewerToken = "metadata-viewer-token"
	if err = f.app.db.SaveUserToken(viewerToken, user.ID); err != nil {
		t.Fatal(err)
	}
	userDB := f.app.db.ForUser(user.ID)
	if err = userDB.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID}}); err != nil {
		t.Fatal(err)
	}
	f.get(t, item+"/images/0", viewerToken, http.StatusOK)
	if err = userDB.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	f.get(t, item+"/images/0", viewerToken, http.StatusNotFound)
	f.get(t, item+"/images/cover", viewerToken, http.StatusNotFound)
	if f.galleryHits.Load() != 1 || f.coverHits.Load() != 1 || f.updatedHits.Load() != 1 || f.source.detailReads.Load() != 1 {
		t.Fatal("撤权请求绕过鉴权访问了暖缓存或上游")
	}
}

func TestR18ImageMetadataColdSparseListLoadsDetailOnce(t *testing.T) {
	f := newR18ImageMetadataFixture(t)
	item := f.base + "/" + f.source.detailVideo.ID
	f.get(t, f.base, f.token, http.StatusOK)
	if f.source.detailReads.Load() != 0 {
		t.Fatal("列表提前读取了详情")
	}
	for range 2 {
		if body := f.get(t, item+"/images/0", f.token, http.StatusOK); !bytes.Equal(body, f.png) {
			t.Fatal("冷稀疏缓存未补齐剧照")
		}
	}
	for range 2 {
		f.get(t, item+"/images/1", f.token, http.StatusNotFound)
	}
	if f.source.detailReads.Load() != 1 || f.source.listReads.Load() != 1 || f.galleryHits.Load() != 1 || f.coverHits.Load() != 0 {
		t.Fatal("冷稀疏列表应仅补取一次完整详情和剧照")
	}
}
