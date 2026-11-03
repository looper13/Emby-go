package cloudguangya

import (
	"context"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"emby-go/internal/clouddrive"
)

const testHash = "0123456789abcdef0123456789abcdef01234567"
const testMagnet = "magnet:?xt=urn:btih:" + testHash

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockReply(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func successReply(data any) (*http.Response, error) {
	b, _ := json.Marshal(map[string]any{"code": 0, "msg": "success", "data": data})
	return mockReply(200, string(b))
}

func requestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	body := map[string]any{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func mockClient(fn roundTripFunc) *Client {
	return newClient(Tokens{Access: "test-access", DeviceID: strings.Repeat("a", 32)}, nil, fn, 0)
}

func requireError(t *testing.T, err error) *clouddrive.Error {
	t.Helper()
	var cloudErr *clouddrive.Error
	if !errors.As(err, &cloudErr) {
		t.Fatalf("期望网盘错误，实际为 %v", err)
	}
	return cloudErr
}

func taskData(id, hash string, status int) map[string]any {
	return map[string]any{"taskId": id, "res": "magnet:?xt=urn:btih:" + hash, "status": status, "progress": 35}
}

func fileData(id, parent, name string, directory bool) map[string]any {
	resource := 1
	if directory {
		resource = 2
	}
	return map[string]any{"fileId": id, "parentId": parent, "fileName": name, "fileSize": "300000", "resType": resource}
}

func fileDataWithoutParent(id, name string, directory bool) map[string]any {
	item := fileData(id, "", name, directory)
	delete(item, "parentId")
	return item
}

func TestNormalizeTokensAndSafeError(t *testing.T) {
	tokens, err := NormalizeTokens(Tokens{Access: " Bearer opaque-access ", Refresh: " refresh-secret ", DeviceID: strings.Repeat("AB", 16)})
	if err != nil || tokens.Access != "opaque-access" || tokens.Refresh != "refresh-secret" || tokens.DeviceID != strings.Repeat("ab", 16) {
		t.Fatalf("凭证规范化失败：%+v %v", tokens, err)
	}
	generated, err := NormalizeTokens(Tokens{Refresh: "refresh-only"})
	if err != nil || len(generated.DeviceID) != 32 || len(DefaultTokens().DeviceID) != 32 {
		t.Fatalf("未生成设备身份：%+v %v", generated, err)
	}
	for _, invalid := range []Tokens{{}, {Access: "secret\r\nheader"}, {Refresh: "secret\x00"}, {Access: "token", DeviceID: "123"}} {
		if _, err := NormalizeTokens(invalid); err == nil {
			t.Fatalf("接受了无效凭证：%+v", invalid)
		}
	}
	err = SafeError(&clouddrive.Error{Message: "opaque-access refresh-secret https://cdn.example/video?signature=private", Missing: true}, tokens)
	cloudErr := requireError(t, err)
	if !cloudErr.Missing || cloudErr.Retryable || strings.Contains(err.Error(), "opaque-access") || strings.Contains(err.Error(), "refresh-secret") || strings.Contains(err.Error(), "signature") {
		t.Fatalf("错误脱敏或分类不正确：%+v", cloudErr)
	}
}

func TestRefreshPersistsDeviceAndPrefersSignedURL(t *testing.T) {
	apiCalls, refreshCalls, persistCalls := 0, 0, 0
	c := newClient(Tokens{Access: "old-access", Refresh: "old-refresh"}, func(tokens Tokens) error {
		persistCalls++
		if tokens.Access != "new-access" || tokens.Refresh != "new-refresh" || len(tokens.DeviceID) != 32 {
			t.Fatalf("刷新凭证不完整：%+v", tokens)
		}
		return nil
	}, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "account.guangyapan.com" {
			if r.URL.Path == "/v1/user/me" {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer new-access" {
					t.Fatal("账号核验没有使用刷新后的令牌")
				}
				return mockReply(200, `{"sub":"account-id"}`)
			}
			refreshCalls++
			body := requestBody(t, r)
			if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/token" || body["refresh_token"] != "old-refresh" || body["grant_type"] != "refresh_token" || r.Header.Get("X-Device-Id") == "" {
				t.Fatalf("刷新请求错误：%v %v", r.URL, body)
			}
			return mockReply(200, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)
		}
		apiCalls++
		if r.Header.Get("Did") == "" || r.Header.Get("Dt") != "4" || len(r.Header.Get("Traceparent")) != 55 || requestBody(t, r)["fileId"] != "video-id" {
			t.Fatal("资源请求头或文件身份错误")
		}
		if apiCalls == 1 {
			return mockReply(401, `{"error":"old-access old-refresh https://private.example?signature=secret"}`)
		}
		if r.Header.Get("Authorization") != "Bearer new-access" {
			t.Fatal("没有使用刷新后的令牌重试")
		}
		return successReply(map[string]string{"signedURL": "https://cdn.example/signed?signature=secret", "downloadUrl": "https://cdn.example/fallback"})
	}), 0)
	device := c.tokens.DeviceID
	raw, err := c.URL(context.Background(), "video-id", "Browser-UA")
	if err != nil || raw != "https://cdn.example/signed?signature=secret" || apiCalls != 2 || refreshCalls != 1 || persistCalls != 1 || c.tokens.DeviceID != device {
		t.Fatalf("刷新或直链选择失败：%s %v %d %d %d", raw, err, apiCalls, refreshCalls, persistCalls)
	}
}

func TestRefreshOnlyOnceAndPersistenceFailureStopsRequests(t *testing.T) {
	for _, persistFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(persistFailure), func(t *testing.T) {
			apiCalls, refreshCalls := 0, 0
			c := newClient(Tokens{Access: "old-secret", Refresh: "refresh-secret"}, func(Tokens) error {
				if persistFailure {
					return errors.New("refresh-secret https://private.example?token=secret")
				}
				return nil
			}, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "account.guangyapan.com" {
					if r.URL.Path == "/v1/user/me" {
						return mockReply(200, `{"sub":"account-id"}`)
					}
					refreshCalls++
					return mockReply(200, `{"access_token":"new-secret","refresh_token":"new-refresh"}`)
				}
				apiCalls++
				return mockReply(401, `{"msg":"old-secret https://private.example?signature=secret"}`)
			}), 0)
			_, err := c.URL(context.Background(), "video", "")
			cloudErr := requireError(t, err)
			wantCalls := 2
			if persistFailure {
				wantCalls = 1
			}
			if !cloudErr.Auth || cloudErr.Missing || refreshCalls != 1 || apiCalls != wantCalls || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private.example") {
				t.Fatalf("刷新重试或脱敏错误：%v api=%d refresh=%d", err, apiCalls, refreshCalls)
			}
		})
	}
}

func TestCheckRefreshOnlyAndRootDoesNotQueryFile(t *testing.T) {
	calls := 0
	c := newClient(Tokens{Refresh: "refresh-only"}, nil, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch r.URL.Path {
		case "/v1/auth/token":
			if r.Method != http.MethodPost {
				t.Fatal("刷新令牌必须使用 POST")
			}
			return mockReply(200, `{"access_token":"access-new"}`)
		case "/v1/user/me":
			if r.Method != http.MethodGet || r.Body != nil {
				t.Fatal("账号核验必须使用无请求体的 GET")
			}
			return mockReply(200, `{"sub":"account-id"}`)
		default:
			t.Fatalf("根目录不应请求文件详情：%s", r.URL)
			return nil, nil
		}
	}), 0)
	account, err := c.Check(context.Background(), "0")
	if err != nil || account != "account-id" || calls != 3 || !c.PendingTaskFolderUnknown() {
		t.Fatalf("刷新登录或根目录验证失败：%s %v calls=%d", account, err, calls)
	}
}

func TestFilePaginationRootMappingAndDownloadIdentity(t *testing.T) {
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		body := requestBody(t, r)
		if r.URL.Path != fileAPI+"file/get_file_list" || body["parentId"] != "" || body["pageSize"] != float64(pageSize) {
			t.Fatalf("根目录请求参数不正确：%v %v", r.URL, body)
		}
		page := int(body["page"].(float64))
		files := []any{}
		length := pageSize
		if page == 1 {
			length = 2
		}
		for i := 0; i < length; i++ {
			files = append(files, fileData(fmt.Sprintf("file-%d", page*pageSize+i), "", "film.mp4", false))
		}
		return successReply(map[string]any{"list": files, "total": "102"})
	})
	first, more, err := c.List(context.Background(), "0", 0)
	if err != nil || !more || len(first) != pageSize || first[0].ParentID != "0" || first[0].PickCode != first[0].ID {
		t.Fatalf("首页读取失败：%d %v %v", len(first), more, err)
	}
	last, more, err := c.List(context.Background(), "0", len(first))
	if err != nil || more || len(last) != 2 || last[0].ID != "file-100" {
		t.Fatalf("末页读取失败：%+v %v %v", last, more, err)
	}
}

func TestInfoUsesFileIdentityEndpoint(t *testing.T) {
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || requestBody(t, r)["fileId"] != "video-id" {
			t.Fatalf("文件身份请求参数错误：%s %v", r.Method, r.URL)
		}
		if r.URL.Path == "/userres/v1/file/get_info_by_file_id" {
			return successReply(fileData("video-id", "owned-child", "film.mp4", false))
		}
		// 详情接口返回嵌套结构，不能作为平铺资源身份接口使用。
		return successReply(map[string]any{"fileInfo": fileData("video-id", "owned-child", "film.mp4", false), "location": "缓存/film.mp4"})
	})
	file, err := c.Info(context.Background(), "video-id")
	if err != nil || file.ID != "video-id" || file.ParentID != "owned-child" || file.Directory {
		t.Fatalf("未使用平铺资源身份接口：%+v %v", file, err)
	}
}

func TestInfoConfirmsOmittedRootParentFromCurrentListing(t *testing.T) {
	rootCalls := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		body := requestBody(t, r)
		item := fileData("cache-root", "", clouddrive.CacheFolderName, true)
		delete(item, "parentId")
		if r.URL.Path == fileAPI+"file/get_file_list" {
			rootCalls++
			if body["parentId"] != "" || body["page"] != float64(0) {
				t.Fatalf("省略父目录后未从当前根列表核验：%v", body)
			}
			return successReply(map[string]any{"list": []any{item}, "total": 1})
		}
		if body["fileId"] != "cache-root" {
			t.Fatalf("缓存目录身份错误：%v", body)
		}
		item["depth"] = 1
		return successReply(item)
	})
	file, err := c.Info(context.Background(), "cache-root")
	if err != nil || file.ID != "cache-root" || file.ParentID != "0" || !file.Directory || rootCalls != 1 {
		t.Fatalf("未核验服务端省略父目录的缓存根：%+v %v root=%d", file, err, rootCalls)
	}
}

func TestInfoConfirmsRootIdentityAcrossCompletePagination(t *testing.T) {
	for _, targetOnFirstPage := range []bool{false, true} {
		t.Run(fmt.Sprint(targetOnFirstPage), func(t *testing.T) {
			rootCalls := 0
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				body := requestBody(t, r)
				if r.URL.Path == "/userres/v1/file/get_info_by_file_id" {
					if body["fileId"] != "target" {
						t.Fatalf("目标文件身份错误：%v", body)
					}
					item := fileDataWithoutParent("target", "目标目录", true)
					item["parentId"] = nil
					// 深度和显示路径都不能代替真实根目录成员证明。
					item["depth"], item["location"] = 4, "其它/显示/路径/目标目录"
					return successReply(item)
				}
				if r.URL.Path != fileAPI+"file/get_file_list" || body["parentId"] != "" || body["pageSize"] != float64(pageSize) || body["page"] != float64(rootCalls) {
					t.Fatalf("根目录分页请求错误：%v %v", r.URL, body)
				}
				page := rootCalls
				rootCalls++
				if page == 0 {
					items := make([]any, pageSize)
					for i := range items {
						items[i] = fileDataWithoutParent(fmt.Sprintf("first-%d", i), "普通文件.mp4", false)
					}
					if targetOnFirstPage {
						items[0] = fileDataWithoutParent("target", "目标目录", true)
					}
					return successReply(map[string]any{"list": items, "total": pageSize + 1})
				}
				last := fileDataWithoutParent("last", "普通文件.mp4", false)
				if !targetOnFirstPage {
					last = fileDataWithoutParent("target", "目标目录", true)
				}
				return successReply(map[string]any{"list": []any{last}, "total": pageSize + 1})
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			file, err := c.Info(ctx, "target")
			if err != nil || file.ParentID != "0" || !file.Directory || rootCalls != 2 {
				t.Fatalf("未完成所有根目录分页核验：%+v %v pages=%d", file, err, rootCalls)
			}
		})
	}
}

func TestInfoDoesNotReusePriorRootMembership(t *testing.T) {
	rootCalls := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/userres/v1/file/get_info_by_file_id" {
			return successReply(fileDataWithoutParent("target", "目标目录", true))
		}
		if r.URL.Path != fileAPI+"file/get_file_list" {
			t.Fatalf("未预期的文件接口：%v", r.URL)
		}
		rootCalls++
		if rootCalls == 1 {
			return successReply(map[string]any{"list": []any{fileDataWithoutParent("target", "目标目录", true)}, "total": 1})
		}
		return mockReply(200, `{"msg":"success","data":{}}`)
	})
	if file, err := c.Info(context.Background(), "target"); err != nil || file.ParentID != "0" {
		t.Fatalf("首次根目录归属核验失败：%+v %v", file, err)
	}
	file, err := c.Info(context.Background(), "target")
	if err == nil || file.ID != "" || requireError(t, err).Missing || rootCalls != 2 {
		t.Fatalf("文件移出根目录后复用了旧归属证明：%+v %v root=%d", file, err, rootCalls)
	}
}

func TestInfoRejectsUnprovenRootMembership(t *testing.T) {
	for _, test := range []struct {
		name string
		data any
	}{
		{"empty-object", map[string]any{}},
		{"empty-list", map[string]any{"list": []any{}, "total": 0}},
		{"same-name-different-id", map[string]any{"list": []any{fileData("different-id", "", "目标目录", true)}, "total": 1}},
		{"same-id-different-name", map[string]any{"list": []any{fileData("target", "", "不同目录", true)}, "total": 1}},
		{"same-id-wrong-type", map[string]any{"list": []any{fileData("target", "", "目标目录", false)}, "total": 1}},
		{"foreign-parent", map[string]any{"list": []any{fileData("target", "foreign-folder", "目标目录", true)}, "total": 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/userres/v1/file/get_info_by_file_id" {
					item := fileDataWithoutParent("target", "目标目录", true)
					item["depth"], item["location"] = 1, "目标目录"
					return successReply(item)
				}
				if r.URL.Path != fileAPI+"file/get_file_list" {
					t.Fatalf("未预期的根目录请求：%v", r.URL)
				}
				return successReply(test.data)
			})
			file, err := c.Info(context.Background(), "target")
			if err == nil || file.ID != "" || requireError(t, err).Missing {
				t.Fatalf("无法证明根归属时接受了文件或误判删除：%+v %v", file, err)
			}
		})
	}
}

func TestInfoRejectsIncompleteRootPagination(t *testing.T) {
	firstPage := make([]any, pageSize)
	for i := range firstPage {
		firstPage[i] = fileDataWithoutParent(fmt.Sprintf("first-%d", i), "普通文件.mp4", false)
	}
	firstPage[0] = fileDataWithoutParent("target", "目标目录", true)
	for _, test := range []struct {
		name      string
		first     any
		next      any
		wantCalls int
	}{
		{"missing-list", map[string]any{"unknownField": 1}, nil, 1},
		{"short-nonfinal-page", map[string]any{"list": []any{firstPage[0]}, "total": pageSize + 1}, nil, 1},
		{"missing-final-page", map[string]any{"list": firstPage, "total": pageSize + 1}, map[string]any{}, 2},
		{"empty-nonfinal-page", map[string]any{"list": firstPage, "total": pageSize + 1}, map[string]any{"list": []any{}, "total": pageSize + 1}, 2},
		{"repeated-page", map[string]any{"list": firstPage, "total": 2 * pageSize}, map[string]any{"list": firstPage, "total": 2 * pageSize}, 2},
		{"changing-total", map[string]any{"list": firstPage, "total": pageSize + 1}, map[string]any{"list": []any{fileDataWithoutParent("last", "最后文件.mp4", false)}, "total": pageSize + 2}, 2},
		{"invalid-total", map[string]any{"list": []any{firstPage[0]}, "total": "unknown"}, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			rootCalls := 0
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/userres/v1/file/get_info_by_file_id" {
					return successReply(fileDataWithoutParent("target", "目标目录", true))
				}
				if r.URL.Path != fileAPI+"file/get_file_list" {
					t.Fatalf("未预期的接口：%v", r.URL)
				}
				rootCalls++
				if rootCalls == 1 {
					return successReply(test.first)
				}
				if rootCalls > test.wantCalls {
					t.Fatal("不完整分页后仍继续请求根目录")
				}
				return successReply(test.next)
			})
			file, err := c.Info(context.Background(), "target")
			if err == nil || file.ID != "" || requireError(t, err).Missing || rootCalls != test.wantCalls {
				t.Fatalf("分页不完整时提前确认了归属：%+v %v calls=%d", file, err, rootCalls)
			}
		})
	}
}

func TestInfoRootVerificationHasPageLimit(t *testing.T) {
	rootCalls := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/userres/v1/file/get_info_by_file_id" {
			return successReply(fileDataWithoutParent("target", "目标目录", true))
		}
		body := requestBody(t, r)
		if r.URL.Path != fileAPI+"file/get_file_list" || body["page"] != float64(rootCalls) {
			t.Fatalf("分页未递增：%v %v", r.URL, body)
		}
		items := make([]any, pageSize)
		for i := range items {
			items[i] = fileDataWithoutParent(fmt.Sprintf("page-%d-file-%d", rootCalls, i), "普通文件.mp4", false)
		}
		if rootCalls == 0 {
			items[0] = fileDataWithoutParent("target", "目标目录", true)
		}
		rootCalls++
		return successReply(map[string]any{"list": items, "total": maxPages*pageSize + 1})
	})
	file, err := c.Info(context.Background(), "target")
	if err == nil || file.ID != "" || !requireError(t, err).Retryable || rootCalls != maxPages {
		t.Fatalf("根目录核验未限制分页或提前确认归属：%+v %v pages=%d", file, err, rootCalls)
	}
}

func TestInfoAcceptsExactPageBoundaryWithoutTotal(t *testing.T) {
	rootCalls := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/userres/v1/file/get_info_by_file_id" {
			return successReply(fileDataWithoutParent("target", "目标文件.mp4", false))
		}
		if r.URL.Path != fileAPI+"file/get_file_list" {
			t.Fatalf("未预期的目录请求：%v", r.URL)
		}
		rootCalls++
		if rootCalls == 2 {
			return mockReply(200, `{"msg":"success","data":{}}`)
		}
		items := make([]any, pageSize)
		for i := range items {
			items[i] = fileDataWithoutParent(fmt.Sprintf("file-%d", i), "普通文件.mp4", false)
		}
		items[0] = fileDataWithoutParent("target", "目标文件.mp4", false)
		return successReply(map[string]any{"list": items})
	})
	file, err := c.Info(context.Background(), "target")
	if err != nil || file.ParentID != "0" || file.Directory || rootCalls != 2 {
		t.Fatalf("没有总数的整页目录不能完成核验：%+v %v pages=%d", file, err, rootCalls)
	}
}

func TestInfoRootVerificationPreservesUnknownFailureClassification(t *testing.T) {
	for _, failure := range []string{"network", "auth", "http404", "business-missing", "server-error"} {
		t.Run(failure, func(t *testing.T) {
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/userres/v1/file/get_info_by_file_id" {
					return successReply(fileDataWithoutParent("target", "目标目录", true))
				}
				switch failure {
				case "network":
					return nil, errors.New("目录列表连接中断")
				case "auth":
					return mockReply(401, `{}`)
				case "http404":
					return mockReply(404, `{}`)
				case "business-missing":
					return mockReply(200, `{"code":404,"msg":"file not found"}`)
				default:
					return mockReply(500, `{}`)
				}
			})
			file, err := c.Info(context.Background(), "target")
			cloudErr := requireError(t, err)
			if file.ID != "" || cloudErr.Missing || cloudErr.Auth != (failure == "auth") || failure == "business-missing" && !cloudErr.Retryable {
				t.Fatalf("归属核验失败被误判为文件删除：%+v %v", file, err)
			}
		})
	}
}

func TestInfoExplicitParentDoesNotNeedRootInference(t *testing.T) {
	for _, test := range []struct {
		name   string
		parent any
		want   string
	}{
		{"non-root", "owned-folder", "owned-folder"},
		{"explicit-empty-root", "", "0"},
		{"numeric-root", 0, "0"},
		{"invalid-parent", "foreign/parent", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != "/userres/v1/file/get_info_by_file_id" {
					t.Fatalf("明确父字段时仍查询根目录：%v", r.URL)
				}
				item := fileData("file", "", "film.mp4", false)
				item["parentId"] = test.parent
				return successReply(item)
			})
			file, err := c.Info(context.Background(), "file")
			if calls != 1 || test.want == "" && err == nil || test.want != "" && (err != nil || file.ParentID != test.want) {
				t.Fatalf("明确父字段的处理发生改变：%+v %v calls=%d", file, err, calls)
			}
		})
	}
}

func TestListAcceptsOnlyConfirmedEmptyResponses(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		ok   bool
	}{
		{"success-empty-object", `{"msg":"success","data":{}}`, true},
		{"code-empty-object", `{"code":0,"data":{}}`, true},
		{"zero-code-success-message", `{"code":0,"msg":"success","data":{}}`, true},
		{"200-code-success-message", `{"code":200,"msg":"success","data":{}}`, true},
		{"200-code-no-message", `{"code":200,"data":{}}`, true},
		{"success-omitted-list-zero-total", `{"msg":"success","data":{"total":0}}`, true},
		{"success-string-zero-total", `{"msg":"success","data":{"total":"0"}}`, true},
		{"success-explicit-empty-list", `{"msg":"success","data":{"list":[],"total":0}}`, true},
		{"unconfirmed-empty-object", `{"data":{}}`, false},
		{"blank-confirmation", `{"msg":" ","data":{}}`, false},
		{"unknown-fields", `{"msg":"success","data":{"unknownField":1}}`, false},
		{"zero-total-unknown-fields", `{"msg":"success","data":{"total":0,"unknownField":1}}`, false},
		{"positive-total-no-list", `{"msg":"success","data":{"total":1}}`, false},
		{"null-list", `{"msg":"success","data":{"list":null,"total":0}}`, false},
		{"null-total", `{"msg":"success","data":{"total":null}}`, false},
		{"array-data", `{"msg":"success","data":[]}`, false},
		{"string-data", `{"msg":"success","data":"empty"}`, false},
		{"null-data", `{"msg":"success","data":null}`, false},
		{"missing-data", `{"msg":"success"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := mockClient(func(*http.Request) (*http.Response, error) { return mockReply(200, test.body) })
			files, more, err := c.List(context.Background(), "cache-root", 0)
			if test.ok {
				if err != nil || more || files == nil || len(files) != 0 {
					t.Fatalf("明确成功的空目录被拒绝：%+v %v %v", files, more, err)
				}
			} else if err == nil || files != nil || more || requireError(t, err).Missing {
				t.Fatalf("不完整或未确认的响应被认为空目录：%+v %v %v", files, more, err)
			}
		})
	}
}

func TestFailureCodesOverrideSuccessMessage(t *testing.T) {
	for _, test := range []struct {
		name      string
		code      any
		errorCode any
		auth      bool
		retry     bool
	}{
		{"unauthorized", 401, nil, true, true},
		{"forbidden", 403, nil, true, true},
		{"server-failure", 500, nil, false, true},
		{"rate-limit", 429, nil, false, true},
		{"success-code-auth-error", 200, 401, true, true},
		{"zero-code-auth-error", 0, 403, true, true},
		{"success-code-server-error", 200, 500, false, true},
		{"combined-server-auth-error", 500, 401, true, true},
		{"invalid-success-code", "unknown", nil, false, false},
		{"invalid-error-code", 200, "unknown", false, false},
		{"nonzero-error-code", 200, 200, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			envelope := map[string]any{"code": test.code, "msg": "success", "data": map[string]any{}}
			if test.errorCode != nil {
				envelope["error_code"] = test.errorCode
			}
			body, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			c := mockClient(func(*http.Request) (*http.Response, error) { return mockReply(200, string(body)) })
			files, more, err := c.List(context.Background(), "cache-root", 0)
			cloudErr := requireError(t, err)
			if files != nil || more || cloudErr.Missing || cloudErr.Auth != test.auth || cloudErr.Retryable != test.retry {
				t.Fatalf("失败代码被 success 文本覆盖或错误分类丢失：%+v %v %v", files, more, err)
			}
		})
	}
}

func TestRetryableFailureCodesDoNotProveFileMissing(t *testing.T) {
	for _, test := range []struct {
		name      string
		code      int
		errorCode int
		missing   bool
	}{
		{"explicit-missing", 404, 0, true},
		{"rate-limit", 429, 0, false},
		{"server-failure", 500, 0, false},
		{"success-code-server-error", 200, 500, false},
		{"zero-code-rate-limit", 0, 429, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"code": test.code, "error_code": test.errorCode, "msg": "file not found"})
			if err != nil {
				t.Fatal(err)
			}
			c := mockClient(func(*http.Request) (*http.Response, error) { return mockReply(200, string(body)) })
			file, err := c.Info(context.Background(), "target")
			cloudErr := requireError(t, err)
			if file.ID != "" || cloudErr.Missing != test.missing || cloudErr.Retryable == test.missing || cloudErr.Auth {
				t.Fatalf("可重试服务失败被当作文件缺失：%+v %v", file, err)
			}
		})
	}
}

func TestCheckRefreshesUsingGETAndVerifiesOmittedRootParent(t *testing.T) {
	accountCalls, refreshCalls, resources, persisted := 0, 0, 0, 0
	c := newClient(Tokens{Access: "expired-access", Refresh: "saved-refresh"}, func(tokens Tokens) error {
		persisted++
		if tokens.Access != "refreshed-access" {
			t.Fatal("账号核验通过后未保存刷新凭证")
		}
		return nil
	}, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/user/me":
			accountCalls++
			if r.Method != http.MethodGet || r.Body != nil {
				t.Fatal("账号核验改用了非 GET 协议")
			}
			if r.Header.Get("Authorization") == "Bearer expired-access" {
				return mockReply(401, `{}`)
			}
			if r.Header.Get("Authorization") != "Bearer refreshed-access" {
				t.Fatal("账号核验未使用候选刷新令牌")
			}
			return mockReply(200, `{"sub":"saved-account"}`)
		case "/v1/auth/token":
			refreshCalls++
			if r.Method != http.MethodPost || requestBody(t, r)["refresh_token"] != "saved-refresh" {
				t.Fatal("刷新请求协议错误")
			}
			return mockReply(200, `{"access_token":"refreshed-access"}`)
		case "/userres/v1/file/get_info_by_file_id":
			resources++
			if persisted != 1 || r.Header.Get("Authorization") != "Bearer refreshed-access" || requestBody(t, r)["fileId"] != "cache-root" {
				t.Fatal("完成账号核验前就访问了缓存目录")
			}
			return successReply(fileDataWithoutParent("cache-root", clouddrive.CacheFolderName, true))
		case fileAPI + "file/get_file_list":
			resources++
			if persisted != 1 || r.Header.Get("Authorization") != "Bearer refreshed-access" || requestBody(t, r)["parentId"] != "" {
				t.Fatal("当前根目录核验使用了错误账号或父目录")
			}
			return successReply(map[string]any{"list": []any{fileDataWithoutParent("cache-root", clouddrive.CacheFolderName, true)}, "total": 1})
		default:
			t.Fatalf("缓存目录验证请求了错误接口：%v", r.URL)
			return nil, nil
		}
	}), 0)
	c.SetExpectedAccount("saved-account")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	account, err := c.Check(ctx, "cache-root")
	if err != nil || account != "saved-account" || accountCalls != 3 || refreshCalls != 1 || resources != 2 || persisted != 1 {
		t.Fatalf("绑定账号刷新后不能验证缓存根：%s %v account=%d refresh=%d resources=%d persist=%d", account, err, accountCalls, refreshCalls, resources, persisted)
	}
}

func TestRejectsForeignFileParentAndWrongInfoIdentity(t *testing.T) {
	for _, operation := range []string{"list", "info"} {
		t.Run(operation, func(t *testing.T) {
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				if operation == "list" {
					return successReply(map[string]any{"list": []any{fileData("file", "foreign-folder", "film.mp4", false)}, "total": 1})
				}
				return successReply(fileData("different-id", "folder", "film.mp4", false))
			})
			var err error
			if operation == "list" {
				_, _, err = c.List(context.Background(), "folder", 0)
			} else {
				_, err = c.Info(context.Background(), "requested-id")
			}
			if err == nil {
				t.Fatal("接受了错误父目录或文件身份")
			}
		})
	}
}

func TestURLValidationAndFallback(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "https://user:secret@cdn.example/video", "https://cdn.example\\video", "https://cdn.example/video\r\nheader:value", ""} {
		c := mockClient(func(*http.Request) (*http.Response, error) { return successReply(map[string]string{"signedURL": raw}) })
		if _, err := c.URL(context.Background(), "video", ""); err == nil {
			t.Fatalf("接受了无效直链：%q", raw)
		}
	}
	c := mockClient(func(*http.Request) (*http.Response, error) {
		return successReply(map[string]string{"downloadUrl": "https://cdn.example/video"})
	})
	if raw, err := c.URL(context.Background(), "video", ""); err != nil || raw != "https://cdn.example/video" {
		t.Fatalf("下载直链回退失败：%s %v", raw, err)
	}
}

func TestFindTaskStatusesAndRestoresParentFromResult(t *testing.T) {
	for _, state := range []struct{ upstream, shared int }{{0, 0}, {1, 1}, {2, 2}, {3, -1}, {4, -1}, {5, 2}} {
		t.Run(fmt.Sprint(state.upstream), func(t *testing.T) {
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == taskAPI+"list_task" {
					body := requestBody(t, r)
					if !reflect.DeepEqual(body["status"], []any{float64(0), float64(1), float64(2), float64(3), float64(4), float64(5)}) {
						t.Fatal("对账遗漏了完成或失败状态")
					}
					task := taskData("task", strings.ToUpper(testHash), state.upstream)
					if state.shared == 2 {
						task["fileId"] = "result"
					}
					return successReply(map[string]any{"list": []any{task}, "total": 1})
				}
				if r.URL.Path != "/userres/v1/file/get_info_by_file_id" || requestBody(t, r)["fileId"] != "result" {
					t.Fatalf("结果详情请求错误：%v", r.URL)
				}
				return successReply(fileData("result", "owned-child", "movie", true))
			})
			task, err := c.Find(context.Background(), testHash)
			if err != nil || task == nil || task.Status != state.shared || task.Hash != testHash {
				t.Fatalf("状态映射失败：%+v %v", task, err)
			}
			if state.shared == 2 && (task.FolderID != "owned-child" || task.ResultID != "result") || state.shared != 2 && task.FolderID != "" {
				t.Fatalf("结果目录还原错误：%+v", task)
			}
		})
	}
}

func TestFindStrictHashAndCursorPagination(t *testing.T) {
	bytes, _ := hex.DecodeString(testHash)
	base32Hash := base32.StdEncoding.EncodeToString(bytes)
	calls := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		calls++
		body := requestBody(t, r)
		if calls == 1 {
			unrelated := taskData("unrelated", strings.Repeat("f", 40), 1)
			unrelated["fileName"] = testHash
			return successReply(map[string]any{"list": []any{unrelated}, "total": 2, "cursor": "next-page"})
		}
		if body["cursor"] != "next-page" || body["page"] != nil {
			t.Fatalf("游标续页参数错误：%v", body)
		}
		return successReply(map[string]any{"list": []any{taskData("matching", base32Hash, 1)}, "total": 2, "cursor": ""})
	})
	task, err := c.Find(context.Background(), testHash)
	if err != nil || task == nil || task.Hash != testHash || calls != 2 {
		t.Fatalf("严格磁力匹配或游标分页失败：%+v %v %d", task, err, calls)
	}
}

func TestFindDoesNotTreatIncompleteOrDuplicateTasksAsAbsent(t *testing.T) {
	for _, data := range []map[string]any{
		{},
		{"list": []any{}, "total": 1},
		{"list": []any{taskData("first", testHash, 1), taskData("second", testHash, 1)}, "total": 2},
		{"list": []any{map[string]any{"taskId": "unknown", "status": 1}}, "total": 1},
	} {
		c := mockClient(func(*http.Request) (*http.Response, error) { return successReply(data) })
		task, err := c.Find(context.Background(), testHash)
		if err == nil || task != nil || !requireError(t, err).Retryable {
			t.Fatalf("不完整对账被当作任务不存在：%+v %v", data, err)
		}
	}
}

func TestFindCompletionRequiresIndependentAndOwnedResult(t *testing.T) {
	for _, resultID := range []string{"", "0", "owned-child", "foreign-result"} {
		c := mockClient(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == taskAPI+"list_task" {
				task := taskData("task", testHash, 2)
				task["parentId"], task["fileId"] = "owned-child", resultID
				return successReply(map[string]any{"list": []any{task}, "total": 1})
			}
			return successReply(fileData(resultID, "foreign-parent", "movie", true))
		})
		if task, err := c.Find(context.Background(), testHash); err == nil || task != nil {
			t.Fatalf("接受了无独立身份或目录不符的结果：%s %+v %v", resultID, task, err)
		}
	}
}

func TestFindMissingCompletedResultKeepsRemoteIdentity(t *testing.T) {
	for _, failure := range []string{"missing", "http404", "network", "auth"} {
		t.Run(failure, func(t *testing.T) {
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == taskAPI+"list_task" {
					task := taskData("old-task", testHash, 2)
					task["fileId"] = "old-result"
					return successReply(map[string]any{"list": []any{task}, "total": 1})
				}
				switch failure {
				case "missing":
					return mockReply(200, `{"code":404,"msg":"file not found"}`)
				case "http404":
					return mockReply(404, `{}`)
				case "network":
					return nil, errors.New("连接读取失败")
				default:
					return mockReply(401, `{}`)
				}
			})
			remote, err := c.Find(context.Background(), testHash)
			if failure == "missing" {
				if err != nil || remote == nil || !remote.ResultMissing || remote.RemoteID != "old-task" || remote.ResultID != "old-result" || remote.FolderID != "" || remote.Status != 2 {
					t.Fatalf("缺失结果丢失了可恢复的远程身份：%+v %v", remote, err)
				}
			} else if err == nil || remote != nil || requireError(t, err).Missing {
				t.Fatalf("未知失败或认证失败被视为可恢复的结果缺失：%+v %v", remote, err)
			}
		})
	}
}

func TestConditionalRemovalRechecksRecordAndResultBeforeDeleting(t *testing.T) {
	for _, change := range []string{"same-missing", "record-replaced", "result-replaced", "status-changed", "foreign-folder", "missing-not-proven"} {
		t.Run(change, func(t *testing.T) {
			deletes := 0
			expected := clouddrive.RemoteTask{Hash: testHash, RemoteID: "old-task", ResultID: "old-result", FolderID: "owned-child", Status: 2, ResultMissing: true}
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == taskAPI+"list_task" {
					task := taskData("old-task", testHash, 2)
					task["fileId"] = "old-result"
					switch change {
					case "record-replaced":
						task["taskId"] = "new-external-task"
					case "result-replaced":
						task["fileId"] = "new-external-result"
					case "status-changed":
						task["status"], task["parentId"] = 1, "owned-child"
					case "foreign-folder":
						task["parentId"] = "foreign-folder"
					}
					return successReply(map[string]any{"list": []any{task}, "total": 1})
				}
				if r.URL.Path == "/userres/v1/file/get_info_by_file_id" {
					if change == "foreign-folder" {
						return successReply(fileData("old-result", "foreign-folder", "movie", true))
					}
					return mockReply(200, `{"code":404,"msg":"file not found"}`)
				}
				deletes++
				if r.URL.Path != "/cloudcollection/v2/delete_task" || !reflect.DeepEqual(requestBody(t, r), map[string]any{"taskIds": []any{"old-task"}}) {
					t.Fatalf("条件取消请求了错误记录或文件删除接口：%v", r.URL)
				}
				return mockReply(200, `{"msg":"success"}`)
			})
			if change == "missing-not-proven" {
				expected.ResultMissing = false
			}
			err := c.RemoveTaskIf(context.Background(), testHash, expected)
			if change == "same-missing" {
				if err != nil || deletes != 1 {
					t.Fatalf("持久化身份相同的缺失结果未安全取消：%v deletes=%d", err, deletes)
				}
			} else if err == nil || deletes != 0 || !requireError(t, err).Retryable {
				t.Fatalf("远程身份变化后取消了新记录：%s %v deletes=%d", change, err, deletes)
			}
		})
	}
}

func TestAddResolvesHashAndOnlySubmitsSelectedIndexes(t *testing.T) {
	calls := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		calls++
		body := requestBody(t, r)
		if r.URL.Path == taskAPI+"resolve_res" {
			return successReply(map[string]any{"btResInfo": map[string]any{"infoHash": testHash, "fileName": "film", "subfiles": []any{map[string]any{"fileIndex": 2}, map[string]any{"fileIndex": 7}}, "excludeIndices": []int{7}}})
		}
		if r.URL.Path != taskAPI+"create_task" || body["parentId"] != "owned-child" || body["newName"] != "film" || !reflect.DeepEqual(body["fileIndexes"], []any{float64(2)}) {
			t.Fatalf("创建云下载参数错误：%v %v", r.URL, body)
		}
		return successReply(map[string]any{"taskId": "task", "url": testMagnet})
	})
	if err := c.Add(context.Background(), testMagnet, "owned-child"); err != nil || calls != 2 || c.submissions[testHash].folder != "owned-child" {
		t.Fatalf("云下载创建失败：%v calls=%d", err, calls)
	}
}

func TestAddNeverRepeatsAmbiguousSubmission(t *testing.T) {
	for _, failure := range []string{"network", "missing-task-id", "bad-hash"} {
		t.Run(failure, func(t *testing.T) {
			creates, resolves := 0, 0
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == taskAPI+"resolve_res" {
					resolves++
					hash := testHash
					if failure == "bad-hash" {
						hash = strings.Repeat("f", 40)
					}
					return successReply(map[string]any{"btResInfo": map[string]any{"infoHash": hash}})
				}
				creates++
				if failure == "network" {
					return nil, errors.New("连接在任务接受后断开")
				}
				return successReply(map[string]any{})
			})
			err := c.Add(context.Background(), testMagnet, "owned-child")
			if err == nil || creates > 1 || resolves != 1 || failure == "bad-hash" && creates != 0 || requireError(t, err).NotSubmitted != (failure == "bad-hash") {
				t.Fatalf("不明确提交被重复或错误提交：%s %v creates=%d resolves=%d", failure, err, creates, resolves)
			}
		})
	}
}

func TestResolveFailureIsKnownNotSubmittedAndCanBeRetried(t *testing.T) {
	resolves, creates := 0, 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == taskAPI+"resolve_res" {
			resolves++
			if resolves == 1 {
				return mockReply(500, `{"msg":"temporarily unavailable"}`)
			}
			return successReply(map[string]any{"btResInfo": map[string]any{"infoHash": testHash}})
		}
		creates++
		return successReply(map[string]any{"taskId": "created-on-retry"})
	})
	err := c.Add(context.Background(), testMagnet, "owned-child")
	cloudErr := requireError(t, err)
	if !cloudErr.NotSubmitted || !cloudErr.Retryable || creates != 0 {
		t.Fatalf("解析失败被视为模糊创建：%+v creates=%d", cloudErr, creates)
	}
	if err := c.Add(context.Background(), testMagnet, "owned-child"); err != nil || resolves != 2 || creates != 1 {
		t.Fatalf("解析失败后无法安全重试创建：%v resolves=%d creates=%d", err, resolves, creates)
	}
}

func TestCanceledWaitBeforeCreateIsKnownNotSubmitted(t *testing.T) {
	creates := 0
	c := newClient(Tokens{Access: "access"}, nil, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == taskAPI+"create_task" {
			creates++
			return successReply(map[string]any{"taskId": "unexpected"})
		}
		return successReply(map[string]any{"btResInfo": map[string]any{"infoHash": testHash}})
	}), time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := c.Add(ctx, testMagnet, "owned-child")
	if !requireError(t, err).NotSubmitted || creates != 0 {
		t.Fatalf("创建发送前取消未标记为未提交：%v creates=%d", err, creates)
	}
}

func TestCheckRejectsDifferentExpectedAccount(t *testing.T) {
	calls := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/v1/user/me" {
			t.Fatal("账号不匹配时仍查询缓存目录")
		}
		return mockReply(200, `{"sub":"different-account"}`)
	})
	c.SetExpectedAccount("saved-account")
	_, err := c.Check(context.Background(), "owned-child")
	if !requireError(t, err).Auth || calls != 1 || c.expectedAccount != "saved-account" {
		t.Fatalf("接受了与已存连接不同的账号：%v calls=%d", err, calls)
	}
}

func TestBoundAccountIsVerifiedBeforeFirstResourceRequest(t *testing.T) {
	accountCalls, resources := 0, 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/user/me" {
			accountCalls++
			return mockReply(200, `{"sub":"foreign-account"}`)
		}
		resources++
		return successReply(map[string]string{"downloadUrl": "https://cdn.example/video"})
	})
	c.SetExpectedAccount("saved-account")
	_, err := c.URL(context.Background(), "owned-file", "")
	if !requireError(t, err).Auth || resources != 0 || accountCalls != 1 {
		t.Fatalf("未核验已存凭证的账号就访问资源：%v account=%d resources=%d", err, accountCalls, resources)
	}
}

func TestRefreshVerifiesBoundAccountBeforePersistingOrRetrying(t *testing.T) {
	for _, bindViaCheck := range []bool{false, true} {
		t.Run(fmt.Sprint(bindViaCheck), func(t *testing.T) {
			resources, persistCalls, refreshCalls := 0, 0, 0
			c := newClient(Tokens{Access: "saved-access", Refresh: "saved-refresh"}, func(Tokens) error {
				persistCalls++
				return nil
			}, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/v1/auth/token" {
					refreshCalls++
					return mockReply(200, `{"access_token":"foreign-access","refresh_token":"foreign-refresh"}`)
				}
				if r.URL.Path == "/v1/user/me" {
					if r.Header.Get("Authorization") == "Bearer saved-access" {
						return mockReply(200, `{"sub":"saved-account"}`)
					}
					if r.Header.Get("Authorization") != "Bearer foreign-access" {
						t.Fatalf("刷新账号核验使用错误令牌：%v", r.Header)
					}
					return mockReply(200, `{"sub":"foreign-account"}`)
				}
				resources++
				if resources > 1 {
					t.Fatal("发现跨账号刷新后仍请求了资源")
				}
				return mockReply(401, `{}`)
			}), 0)
			if bindViaCheck {
				if account, err := c.Check(context.Background(), "0"); err != nil || account != "saved-account" {
					t.Fatalf("初次 Check 未绑定账号：%s %v", account, err)
				}
			} else {
				c.SetExpectedAccount("saved-account")
			}
			_, err := c.URL(context.Background(), "owned-file", "")
			cloudErr := requireError(t, err)
			if !cloudErr.Auth || cloudErr.Missing || resources != 1 || refreshCalls != 1 || persistCalls != 0 || !c.pendingRefresh || c.expectedAccount != "saved-account" || strings.Contains(err.Error(), "foreign") {
				t.Fatalf("账号切换未被阻止或新凭证被保存：%v resources=%d refresh=%d persist=%d", err, resources, refreshCalls, persistCalls)
			}
			if _, err := c.URL(context.Background(), "owned-file", ""); !requireError(t, err).Auth || resources != 1 || persistCalls != 0 {
				t.Fatalf("候选凭证未验证时后续调用绕过了账号核验：%v resources=%d persist=%d", err, resources, persistCalls)
			}
		})
	}
}

func TestTemporaryAccountValidationFailureRetainsUnpersistedRefreshForRetry(t *testing.T) {
	resources, refreshes, validations, persisted := 0, 0, 0, 0
	c := newClient(Tokens{Access: "old-access", Refresh: "old-refresh"}, func(tokens Tokens) error {
		persisted++
		if tokens.Access != "new-access" || tokens.Refresh != "new-refresh" {
			t.Fatal("验证后未保存轮换的新凭证")
		}
		return nil
	}, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/auth/token":
			refreshes++
			return mockReply(200, `{"access_token":"new-access","refresh_token":"new-refresh"}`)
		case "/v1/user/me":
			if r.Header.Get("Authorization") == "Bearer new-access" {
				validations++
				if validations == 1 {
					return mockReply(500, `{}`)
				}
			}
			return mockReply(200, `{"sub":"saved-account"}`)
		default:
			resources++
			if resources == 1 {
				return mockReply(401, `{}`)
			}
			return successReply(map[string]string{"downloadUrl": "https://cdn.example/video"})
		}
	}), 0)
	c.SetExpectedAccount("saved-account")
	_, err := c.URL(context.Background(), "file", "")
	if !requireError(t, err).Retryable || persisted != 0 || resources != 1 || !c.pendingRefresh {
		t.Fatalf("账号验证临时失败后候选凭证被保存或仍访问资源：%v persist=%d resources=%d", err, persisted, resources)
	}
	if raw, err := c.URL(context.Background(), "file", ""); err != nil || raw == "" || persisted != 1 || resources != 2 || refreshes != 1 || validations != 2 || c.pendingRefresh {
		t.Fatalf("临时失败后未复核候选凭证，或重复刷新：%s %v persist=%d resources=%d refresh=%d validation=%d", raw, err, persisted, resources, refreshes, validations)
	}
}

func TestMkdirRootMappingDeletePollingAndRootProtection(t *testing.T) {
	calls := []string{}
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Path)
		body := requestBody(t, r)
		switch r.URL.Path {
		case fileAPI + "file/create_dir":
			if body["parentId"] != "" || body["dirName"] != clouddrive.CacheFolderName || body["failIfNameExist"] != true {
				t.Fatalf("缓存目录创建参数错误：%v", body)
			}
			return successReply(map[string]any{"fileId": "cache-root"})
		case fileAPI + "file/delete_file":
			if !reflect.DeepEqual(body["fileIds"], []any{"owned-child"}) {
				t.Fatalf("删除文件参数错误：%v", body)
			}
			return successReply(map[string]any{"taskId": "delete-task"})
		case fileAPI + "get_task_status":
			if body["taskId"] != "delete-task" {
				t.Fatal("删除任务身份错误")
			}
			return successReply(map[string]any{"status": 2})
		default:
			t.Fatalf("未预期的接口请求：%v", r.URL)
			return nil, nil
		}
	})
	if id, err := c.Mkdir(context.Background(), "0", clouddrive.CacheFolderName); err != nil || id != "cache-root" {
		t.Fatalf("缓存根目录创建失败：%s %v", id, err)
	}
	if err := c.Delete(context.Background(), "owned-child"); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(context.Background(), "0"); err == nil || len(calls) != 3 {
		t.Fatalf("未保护网盘根目录：%v %v", err, calls)
	}
}

func TestRemoveTaskNeverDeletesResultFiles(t *testing.T) {
	requests := []string{}
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.Path)
		if r.URL.Path == taskAPI+"list_task" {
			task := taskData("target-task", testHash, 2)
			task["fileId"] = "keep-this-result"
			return successReply(map[string]any{"list": []any{task}, "total": 1})
		}
		body := requestBody(t, r)
		if r.URL.Path != "/cloudcollection/v2/delete_task" || !reflect.DeepEqual(body, map[string]any{"taskIds": []any{"target-task"}}) {
			t.Fatalf("移除离线记录请求了错误接口或删除文件参数：%v %v", r.URL, body)
		}
		return mockReply(200, `{"msg":"success"}`)
	})
	if err := c.RemoveTask(context.Background(), testHash); err != nil || len(requests) != 2 {
		t.Fatalf("移除离线记录失败：%v %v", err, requests)
	}
}

func TestErrorsDoNotMisclassifyUnknownFailuresAsMissing(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		c := mockClient(func(*http.Request) (*http.Response, error) {
			return mockReply(status, `{"msg":"secret https://cdn.example?signature=private"}`)
		})
		_, err := c.Info(context.Background(), "file")
		cloudErr := requireError(t, err)
		if cloudErr.Missing || cloudErr.Auth != (status == 401 || status == 403) || strings.Contains(err.Error(), "private") {
			t.Fatalf("错误分类或脱敏不正确：%d %+v", status, cloudErr)
		}
	}
	c := mockClient(func(*http.Request) (*http.Response, error) {
		return mockReply(200, `{"code":404,"msg":"file not found"}`)
	})
	if _, err := c.Info(context.Background(), "file"); !requireError(t, err).Missing {
		t.Fatalf("明确的文件不存在错误未识别：%v", err)
	}
}

func TestRateLimitAndGateHonorCancellation(t *testing.T) {
	calls := 0
	c := newClient(Tokens{Access: "access"}, nil, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return successReply(map[string]string{"downloadUrl": "https://cdn.example/video"})
	}), time.Second)
	if _, err := c.URL(context.Background(), "first", ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.URL(ctx, "second", ""); !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatalf("取消后仍发起了限频请求：%v calls=%d", err, calls)
	}
	if err := c.lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.unlock()
	if _, err := c.URL(ctx, "third", ""); !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatalf("锁等待未尊重取消：%v calls=%d", err, calls)
	}
}
