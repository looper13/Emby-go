package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/cloudguangya"
	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

type r18PagedMagnetTestSource struct {
	fakeR18
	pages      map[int]r18.ResourcePage
	pageErrors map[int]error
	offsets    []int
}

func (s *r18PagedMagnetTestSource) Resources(_ context.Context, _ r18.Video, offset int) (r18.ResourcePage, error) {
	s.offsets = append(s.offsets, offset)
	return s.pages[offset], s.pageErrors[offset]
}

type r18CompleteMagnetTestSource struct {
	r18PagedMagnetTestSource
	resources []r18.Resource
	err       error
	complete  int
	video     r18.Video
	deadline  time.Duration
}

func (s *r18CompleteMagnetTestSource) AllResources(ctx context.Context, video r18.Video) ([]r18.Resource, error) {
	s.complete++
	s.video = video
	if deadline, ok := ctx.Deadline(); ok {
		s.deadline = time.Until(deadline)
	}
	return s.resources, s.err
}

func r18MagnetTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/Items/fixture", nil)
	return c
}

func newR18MagnetTestApp(t *testing.T, source r18Source, configured bool) (*App, store.Library, r18.Video) {
	t.Helper()
	a, _, _ := newProbeTestApp(t, t.TempDir())
	a.offlineService.Close()
	a.guangyaOffline.Close()
	a.r18Open = func(r18.Config) (r18Source, error) { return source, nil }
	// 详情读取不得打开任一网盘，也不能依赖凭据解密或真实服务。
	a.cloud115Open = func(cloud115.Tokens, func(cloud115.Tokens) error) cloud115.Provider {
		t.Error("生成磁力版本打开了 115 网盘")
		return &cloudFixture{}
	}
	a.guangyaOpen = func(cloudguangya.Tokens, func(cloudguangya.Tokens) error) cloud115.Provider {
		t.Error("生成磁力版本打开了光鸭网盘")
		return &cloudFixture{}
	}
	library, err := a.db.CreateR18Library("磁力版本测试", r18.Config{BaseURL: "https://avdb.fixture.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		if err := a.db.SaveCloudGuangyaConnection(store.CloudGuangyaConnection{
			AccountID: "fixture-account", FolderID: "fixture-folder", Secrets: []byte("fixture-encrypted"),
		}, 0); err != nil {
			t.Fatal(err)
		}
	}
	video := r18.Video{ID: r18.EncodeID("magnet001", "javdb"), Number: "TEST-001", Title: "磁力版本测试影片", Duration: 120}
	return a, library, video
}

func TestR18MagnetSourceIDCanonicalIdentity(t *testing.T) {
	hash := strings.Repeat("ab", 20)
	for _, input := range []string{hash, strings.ToUpper(hash), " " + hash + " "} {
		id := r18MagnetSourceID(input)
		parsed, ok := parseR18MagnetSourceID(id)
		if id != "guangya-magnet-"+hash || !ok || parsed != hash {
			t.Fatal("合法 BTIH 没有生成稳定的光鸭版本身份", input, id)
		}
	}
	for _, input := range []string{"", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("g", 40), strings.Repeat("Ａ", 40), "btih-" + hash} {
		if r18MagnetSourceID(input) != "" {
			t.Fatal("非法哈希生成了可点播版本", input)
		}
	}
	for _, source := range []string{
		"", "115-magnet-" + hash, "guangya-" + hash, "guangya-magnet-" + strings.ToUpper(hash),
		"guangya-magnet-" + strings.Repeat("a", 39), "guangya-magnet-" + strings.Repeat("a", 41),
		"guangya-magnet-" + strings.Repeat("g", 40), "guangya-magnet-" + hash + "?extra=true",
		" guangya-magnet-" + hash, "guangya-magnet-" + hash + " ", "guangya-magnet-task-uuid",
	} {
		if parsed, ok := parseR18MagnetSourceID(source); ok || parsed != "" {
			t.Fatal("非规范来源 ID 被当成光鸭磁力版本", source)
		}
	}
}

func TestR18MagnetSourcesAllVersionsStableContractAndNoTasks(t *testing.T) {
	resources := []r18.Resource{}
	for index := 1; index <= 63; index++ {
		hash := fmt.Sprintf("%040x", index)
		resources = append(resources, r18.Resource{ID: "btih-" + hash, Title: fmt.Sprintf("测试版本 %d", index), Number: "test_001",
			Magnet: "magnet:?xt=urn:btih:" + hash, SizeBytes: int64(index) * 1024})
	}
	duplicateHash := strings.Repeat("ab", 20)
	decoded, _ := hex.DecodeString(duplicateHash)
	resources = append(resources,
		r18.Resource{ID: "btih-" + duplicateHash, Magnet: "magnet:?xt=urn:btih:" + strings.ToUpper(duplicateHash)},
		r18.Resource{ID: "btih-" + duplicateHash, Magnet: "magnet:?xt=urn:btih:" + base32.StdEncoding.EncodeToString(decoded)},
		r18.Resource{ID: "btih-" + duplicateHash, Magnet: "magnet:?xt=urn:btih:" + duplicateHash},
	)
	source := &r18CompleteMagnetTestSource{resources: resources}
	a, library, video := newR18MagnetTestApp(t, source, true)
	sources, err := a.r18MagnetSources(r18MagnetTestContext(), library, video)
	if err != nil || len(sources) != 64 || source.complete != 1 || len(source.offsets) != 0 {
		t.Fatal("完整资源读取没有在单次请求中列出全部规范版本", len(sources), source.complete, source.offsets, err)
	}
	if !reflect.DeepEqual(source.video, video) || source.deadline <= 0 || source.deadline > 20*time.Second {
		t.Fatal("完整资源查询丢失当前影片或请求时间预算", source.deadline)
	}
	ids := map[string]string{}
	for index, media := range sources {
		hash := media["InfoHash"].(string)
		id, ok := media["Id"].(string)
		if !ok || id != "guangya-magnet-"+hash || ids[hash] != "" || media["Provider"] != "guangya" || media["ResourceId"] != "btih-"+hash {
			t.Fatal("同一 BTIH 生成重复版本或版本身份丢失", media)
		}
		ids[hash] = id
		if media["Default"] != (index == 0) || media["ItemId"] != r18ItemID(library.ID, video.ID) || media["Name"] == "" {
			t.Fatal("默认版本、影片身份或展示名称不正确", media)
		}
		assertR18MagnetSourceContract(t, a, library, video, media)
	}
	if sources[0]["Size"] != int64(1024) || sources[0]["RunTimeTicks"] != int64(120)*600000000 {
		t.Fatal("有依据的大小或分钟时长没有正确转换")
	}
	if _, exists := sources[len(sources)-1]["Size"]; exists {
		t.Fatal("未知磁力大小被伪造")
	}
	// 上游顺序和标题变化不能改变客户端已经选择的版本 ID。
	for left, right := 0, len(source.resources)-1; left < right; left, right = left+1, right-1 {
		source.resources[left], source.resources[right] = source.resources[right], source.resources[left]
	}
	for index := range source.resources {
		source.resources[index].Title = "更新后的版本名称"
	}
	updated, err := a.r18MagnetSources(r18MagnetTestContext(), library, video)
	if err != nil || len(updated) != len(sources) || source.complete != 2 {
		t.Fatal("更新资源元数据后丢失版本", err)
	}
	for _, media := range updated {
		if ids[media["InfoHash"].(string)] != media["Id"] {
			t.Fatal("资源重新排序或改名改变了稳定版本 ID")
		}
	}
	db, err := sql.Open("sqlite", a.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tasks int
	if err := db.QueryRow("SELECT COUNT(*) FROM offline_tasks").Scan(&tasks); err != nil || tasks != 0 {
		t.Fatal("读取版本详情创建了离线任务", tasks, err)
	}
}

func assertR18MagnetSourceContract(t *testing.T, a *App, library store.Library, video r18.Video, media gin.H) {
	t.Helper()
	for _, flag := range []string{"IsRemote", "SupportsDirectPlay", "SupportsDirectStream"} {
		if media[flag] != true {
			t.Fatal("媒体源缺少直连播放能力", flag)
		}
	}
	for _, flag := range []string{"HasMixedProtocols", "SupportsTranscoding", "SupportsProbing", "RequiresOpening", "RequiresClosing", "RequiresLooping", "IsInfiniteStream", "AddApiKeyToDirectStreamUrl", "ReadAtNativeFramerate"} {
		if media[flag] != false {
			t.Fatal("媒体源声明了无依据的播放能力", flag)
		}
	}
	if media["Protocol"] != "Http" || media["Type"] != "Default" || media["DefaultAudioStreamIndex"] != -1 || media["DefaultSubtitleStreamIndex"] != -1 {
		t.Fatal("媒体源协议、类型或默认轨道索引不正确")
	}
	streams, ok := media["MediaStreams"].([]gin.H)
	if !ok || len(streams) != 1 || streams[0]["Type"] != "Video" || streams[0]["Index"] != 0 || streams[0]["IsExternal"] != false || streams[0]["IsDefault"] != true {
		t.Fatal("未准备版本缺少客户端必需的通用视频轨")
	}
	for _, key := range []string{"Codec", "Width", "Height", "BitRate"} {
		if _, exists := streams[0][key]; exists {
			t.Fatal("未准备版本伪造了轨道参数", key)
		}
	}
	raw, err := json.Marshal(media)
	if err != nil {
		t.Fatal(err)
	}
	var strict tsukimiMediaSource
	if err := json.Unmarshal(raw, &strict); err != nil || strict.ID == "" || strict.Chapters == nil || len(strict.MediaStreams) != 1 {
		t.Fatal("媒体源不满足原生客户端字段合同", err)
	}
	assertClientStreamIndexes(t, strict.MediaStreams)
	if formats, ok := media["Formats"].([]string); !ok || formats == nil {
		t.Fatal("Formats 必须是数组")
	}
	if headers, ok := media["RequiredHttpHeaders"].(gin.H); !ok || headers == nil || len(headers) != 0 {
		t.Fatal("RequiredHttpHeaders 必须是空对象")
	}
	stream := strict.DirectStream
	parsed, err := url.Parse(stream)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(stream, "/") || strings.HasPrefix(stream, "/emby") || parsed.Path != streamURL(r18ItemID(library.ID, video.ID)) || media["Path"] != stream {
		t.Fatal("播放入口不是本服务的相对 HTTP 视频地址", stream)
	}
	if parsed.Query().Get("MediaSourceId") != strict.ID || parsed.Query().Get("Static") != "true" || parsed.Query().Get("api_key") != "" || strings.Contains(stream, "magnet:") {
		t.Fatal("播放地址没有绑定稳定版本或暴露了不必要参数")
	}
	parts := strings.Split(parsed.Query().Get("playback_token"), ".")
	if len(parts) != 2 {
		t.Fatal("播放地址缺少签名票据")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	signature, sigErr := base64.RawURLEncoding.DecodeString(parts[1])
	mac := hmac.New(sha256.New, a.subtitleSigningKey[:])
	mac.Write(data)
	var ticket cloudTicket
	if err != nil || sigErr != nil || !hmac.Equal(signature, mac.Sum(nil)) || json.Unmarshal(data, &ticket) != nil {
		t.Fatal("播放票据签名无效")
	}
	if ticket.User != a.db.UserID() || ticket.Item != r18ItemID(library.ID, video.ID) || ticket.Source != strict.ID || ticket.Revision != library.Revision || ticket.Expiry <= time.Now().Unix() {
		t.Fatal("播放票据没有绑定用户、影片、版本或资料库修订")
	}
}

func TestR18MagnetSourcesRejectUnsupportedAndMismatchedResources(t *testing.T) {
	hash, other := strings.Repeat("a", 40), strings.Repeat("b", 40)
	good := r18.Resource{ID: "btih-" + hash, Title: "合法版本", Magnet: "magnet:?xt=urn:btih:" + hash}
	source := &r18CompleteMagnetTestSource{resources: []r18.Resource{
		{ID: "btmh-1220" + strings.Repeat("c", 64), Magnet: "magnet:?xt=urn:btmh:1220" + strings.Repeat("c", 64)},
		{ID: "btih-" + other, Magnet: "magnet:?xt=urn:btih:" + other + "&xt=urn:btih:" + hash},
		{ID: "btih-" + other, Number: "OTHER-001", Magnet: "magnet:?xt=urn:btih:" + other},
		{ID: "btih-" + hash, Magnet: "magnet:?xt=urn:btih:" + other},
		{ID: "forged", Magnet: "magnet:?xt=urn:btih:" + other},
		{ID: "btih-" + other, Magnet: "https://files.fixture.invalid/video.mp4"},
		{ID: "btih-" + other, Magnet: "magnet:/invalid?xt=urn:btih:" + other},
		{ID: "btih-" + other, Magnet: "magnet:?xt=urn:btih:" + other + "&dn=%ZZ"},
		good, good,
	}}
	a, library, video := newR18MagnetTestApp(t, source, true)
	sources, err := a.r18MagnetSources(r18MagnetTestContext(), library, video)
	if err != nil || len(sources) != 1 || sources[0]["InfoHash"] != hash || sources[0]["Default"] != true {
		t.Fatal("非法、跨影片或重复磁力生成了可点播版本", len(sources), err)
	}
}

func TestR18MagnetSourcesRequireGuangyaConfiguration(t *testing.T) {
	source := &r18CompleteMagnetTestSource{resources: []r18.Resource{{ID: "btih-" + strings.Repeat("a", 40), Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}}}
	a, library, video := newR18MagnetTestApp(t, source, false)
	if err := a.db.SaveCloud115Connection(store.Cloud115Connection{AccountID: "fixture-115", FolderID: "fixture-folder", Secrets: []byte("fixture-encrypted")}, 0); err != nil {
		t.Fatal(err)
	}
	opens := 0
	a.r18Open = func(r18.Config) (r18Source, error) { opens++; return source, nil }
	sources, err := a.r18MagnetSources(r18MagnetTestContext(), library, video)
	if err != nil || sources == nil || len(sources) != 0 || source.complete != 0 || len(source.offsets) != 0 || opens != 0 {
		t.Fatal("未配置光鸭时读取了上游或借用 115 列出版本", opens, err)
	}
}

func TestR18MagnetSourcesPropagateErrorsAndRequestCancellation(t *testing.T) {
	upstreamErr := errors.New("上游夹具不可用")
	source := &r18CompleteMagnetTestSource{err: upstreamErr}
	a, library, video := newR18MagnetTestApp(t, source, true)
	if sources, err := a.r18MagnetSources(r18MagnetTestContext(), library, video); !errors.Is(err, upstreamErr) || len(sources) != 0 {
		t.Fatal("上游资源失败被伪装成成功的空版本列表", sources, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := r18MagnetTestContext()
	c.Request = c.Request.WithContext(ctx)
	if sources, err := a.r18MagnetSources(c, library, video); !errors.Is(err, context.Canceled) || len(sources) != 0 || source.complete != 1 {
		t.Fatal("请求取消后继续读取了资源", source.complete, err)
	}
	short, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	c = r18MagnetTestContext()
	c.Request = c.Request.WithContext(short)
	_, _ = a.r18MagnetSources(c, library, video)
	if source.deadline <= 0 || source.deadline > time.Second {
		t.Fatal("磁力查询延长了调用方的短时间预算", source.deadline)
	}
}

func TestR18MagnetSourcesPagedCompatibilityAndInvalidProgress(t *testing.T) {
	one := r18.Resource{ID: "btih-" + strings.Repeat("a", 40), Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}
	two := r18.Resource{ID: "btih-" + strings.Repeat("b", 40), Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("b", 40)}
	source := &r18PagedMagnetTestSource{pages: map[int]r18.ResourcePage{
		0: {Items: []r18.Resource{one}, HasMore: true, NextOffset: 1},
		1: {Items: []r18.Resource{two}, NextOffset: 2},
	}}
	a, library, video := newR18MagnetTestApp(t, source, true)
	sources, err := a.r18MagnetSources(r18MagnetTestContext(), library, video)
	if err != nil || len(sources) != 2 || !reflect.DeepEqual(source.offsets, []int{0, 1}) {
		t.Fatal("未实现完整读取的来源不能继续按页返回全部版本", source.offsets, err)
	}
	for _, page := range []r18.ResourcePage{
		{Items: []r18.Resource{one}, HasMore: true, NextOffset: 0},
		{Items: []r18.Resource{one}, HasMore: true, NextOffset: -1},
		{Items: []r18.Resource{one}, HasMore: true, NextOffset: 10001},
		{Items: []r18.Resource{}, HasMore: true, NextOffset: 50},
	} {
		bad := &r18PagedMagnetTestSource{pages: map[int]r18.ResourcePage{0: page}}
		if items, err := readR18MagnetResources(context.Background(), bad, video); err == nil || items != nil || !reflect.DeepEqual(bad.offsets, []int{0}) {
			t.Fatal("分页异常导致重复上游查询或静默截断", bad.offsets, err)
		}
	}
	pageErr := errors.New("后续页读取失败")
	partial := &r18PagedMagnetTestSource{pages: map[int]r18.ResourcePage{0: {Items: []r18.Resource{one}, HasMore: true, NextOffset: 1}}, pageErrors: map[int]error{1: pageErr}}
	if items, err := readR18MagnetResources(context.Background(), partial, video); !errors.Is(err, pageErr) || items != nil {
		t.Fatal("后续分页失败返回了误导性的部分版本", items, err)
	}
}
