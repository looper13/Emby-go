package cloudguangya

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// 这些测试检查真实 Client 发出的接口请求，不连接光鸭或验证真实 CDN。
func TestDownloadURLUsesPlayerUserAgentOnlyForDownloadRequest(t *testing.T) {
	const playerUA = "Hill-Yamby-Player/1.0"
	var paths []string
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == fileAPI+"get_res_download_url" {
			if r.UserAgent() != playerUA {
				t.Fatal("下载地址请求没有使用实际播放器 UA")
			}
			if r.Method != http.MethodPost || !reflect.DeepEqual(requestBody(t, r), map[string]any{"fileId": "video-id"}) {
				t.Fatal("下载地址接口的请求方法或正文发生变化")
			}
			if r.Header.Get("Origin") != "https://www.guangyapan.com" || r.Header.Get("Referer") != "https://www.guangyapan.com/" || r.Header.Get("Did") == "" || r.Header.Get("Dt") != "4" {
				t.Fatal("下载地址接口的原有请求身份发生变化")
			}
			return successReply(map[string]string{"signedURL": "https://cdn.fixture.invalid/video.mkv"})
		}
		if r.UserAgent() != userAgent {
			t.Fatal("播放器 UA 污染了账号或其它资源接口")
		}
		switch r.URL.Path {
		case "/v1/user/me":
			if r.Method != http.MethodGet || r.Body != nil {
				t.Fatal("账号核验请求发生变化")
			}
			return mockReply(http.StatusOK, `{"sub":"fixture-account"}`)
		case "/userres/v1/file/get_info_by_file_id":
			return successReply(fileData("video-id", "parent-id", "正片.mkv", false))
		default:
			t.Fatal("出现了未预期的接口请求")
			return nil, nil
		}
	})
	c.SetExpectedAccount("fixture-account")
	if raw, err := c.URL(context.Background(), "video-id", playerUA); err != nil || raw != "https://cdn.fixture.invalid/video.mkv" {
		t.Fatal("获取下载地址失败")
	}
	if _, err := c.Info(context.Background(), "video-id"); err != nil {
		t.Fatal("下载地址请求后无法查询文件")
	}
	if !reflect.DeepEqual(paths, []string{"/v1/user/me", fileAPI + "get_res_download_url", "/userres/v1/file/get_info_by_file_id"}) {
		t.Fatal("下载地址请求改变了账号或资源查询流程")
	}
}

func TestDownloadURLEmptyUserAgentKeepsBrowserDefault(t *testing.T) {
	calls := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != fileAPI+"get_res_download_url" || r.UserAgent() != userAgent {
			t.Fatal("空播放器 UA 没有沿用原有浏览器 UA")
		}
		return successReply(map[string]string{"downloadUrl": "https://cdn.fixture.invalid/video.mkv"})
	})
	if _, err := c.URL(context.Background(), "video-id", ""); err != nil || calls != 1 {
		t.Fatal("空播放器 UA 改变了原有下载地址流程")
	}
}

func TestDownloadURLUserAgentLimitAndRequestIsolation(t *testing.T) {
	// 接受上限长度的 UA，后续新请求仍各自选择播放器或默认浏览器身份。
	values := []string{strings.Repeat("a", 1024), "Yamby-Player/3.0", ""}
	index := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		if index >= len(values) || r.URL.Path != fileAPI+"get_res_download_url" {
			t.Fatal("出现了未预期的下载地址请求")
		}
		want := values[index]
		if want == "" {
			want = userAgent
		}
		if r.UserAgent() != want {
			t.Fatal("上限长度的 UA 不可用，或播放器 UA 泄漏到后续请求")
		}
		index++
		return successReply(map[string]string{"signedURL": "https://cdn.fixture.invalid/video.mkv"})
	})
	for _, ua := range values {
		if _, err := c.URL(context.Background(), "video-id", ua); err != nil {
			t.Fatal("有效播放器 UA 获取下载地址失败")
		}
	}
	if index != len(values) {
		t.Fatal("有效播放器 UA 丢失下载地址请求")
	}
}

func TestDownloadURLRejectsUnsafeUserAgentBeforeRequests(t *testing.T) {
	for _, tc := range []struct {
		name, ua string
	}{
		{"回车换行", "private-ua\r\nInjected: value"},
		{"换行", "private-ua\n"},
		{"制表符", "private-ua\t"},
		{"空字符", "private-ua\x00"},
		{"控制字符", "private-ua\x1f"},
		{"删除字符", "private-ua\x7f"},
		{"Unicode 控制字符", "private-ua\u0085"},
		{"超过长度上限", strings.Repeat("a", 1025)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == "/v1/user/me" {
					return mockReply(http.StatusOK, `{"sub":"fixture-account"}`)
				}
				return successReply(map[string]string{"signedURL": "https://cdn.fixture.invalid/video.mkv"})
			})
			c.SetExpectedAccount("fixture-account")
			raw, err := c.URL(context.Background(), "video-id", tc.ua)
			if err == nil || raw != "" || calls != 0 {
				t.Fatal("非法播放器 UA 没有在任何网络请求之前拒绝")
			}
			cloudErr := requireError(t, err)
			if cloudErr.Auth || cloudErr.Retryable || cloudErr.Missing || strings.Contains(err.Error(), "private-ua") || strings.Contains(err.Error(), tc.ua) {
				t.Fatal("非法播放器 UA 的错误分类或脱敏不正确")
			}
		})
	}
}

func TestDownloadURLKeepsPlayerUserAgentAcrossRefreshRetry(t *testing.T) {
	for _, reject := range []string{"download", "account"} {
		t.Run(reject, func(t *testing.T) {
			const playerUA = "Hill-Yamby-Player/2.0"
			var steps []string
			refreshCalls, downloadCalls, persistCalls := 0, 0, 0
			c := newClient(Tokens{Access: "fixture-old-access", Refresh: "fixture-refresh", DeviceID: strings.Repeat("a", 32)}, func(Tokens) error {
				persistCalls++
				return nil
			}, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == fileAPI+"get_res_download_url" {
					steps = append(steps, "download")
					downloadCalls++
					if r.UserAgent() != playerUA || !reflect.DeepEqual(requestBody(t, r), map[string]any{"fileId": "video-id"}) {
						t.Fatal("刷新前后下载地址请求丢失播放器 UA 或改变正文")
					}
					if reject == "download" && downloadCalls == 1 {
						return mockReply(http.StatusUnauthorized, `{"error":"unauthorized"}`)
					}
					if r.Header.Get("Authorization") != "Bearer fixture-new-access" || persistCalls != 1 {
						t.Fatal("刷新后没有使用已保存的新凭证请求下载地址")
					}
					return successReply(map[string]string{"signedURL": "https://cdn.fixture.invalid/video.mkv"})
				}
				if r.UserAgent() != userAgent {
					t.Fatal("下载地址的播放器 UA 污染了刷新、账号或其它资源接口")
				}
				switch r.URL.Path {
				case "/v1/auth/token":
					steps = append(steps, "refresh")
					refreshCalls++
					if r.Method != http.MethodPost || r.Header.Get("X-Device-Id") == "" || requestBody(t, r)["grant_type"] != "refresh_token" {
						t.Fatal("刷新请求方法、正文或设备身份发生变化")
					}
					return mockReply(http.StatusOK, `{"access_token":"fixture-new-access","refresh_token":"fixture-new-refresh","expires_in":3600}`)
				case "/v1/user/me":
					steps = append(steps, "account")
					if reject == "account" && refreshCalls == 0 {
						return mockReply(http.StatusUnauthorized, `{"error":"unauthorized"}`)
					}
					return mockReply(http.StatusOK, `{"sub":"fixture-account"}`)
				case "/userres/v1/file/get_info_by_file_id":
					steps = append(steps, "info")
					return successReply(fileData("video-id", "parent-id", "正片.mkv", false))
				default:
					t.Fatal("出现了未预期的接口请求")
					return nil, nil
				}
			}), 0)
			c.SetExpectedAccount("fixture-account")
			if _, err := c.URL(context.Background(), "video-id", playerUA); err != nil {
				t.Fatal("认证刷新后获取下载地址失败")
			}
			if _, err := c.Info(context.Background(), "video-id"); err != nil {
				t.Fatal("刷新重试后无法查询文件")
			}
			want := []string{"account", "download", "refresh", "account", "download", "info"}
			if reject == "account" {
				want = []string{"account", "refresh", "account", "download", "info"}
			}
			if !reflect.DeepEqual(steps, want) || refreshCalls != 1 || persistCalls != 1 {
				t.Fatal("播放器 UA 改变了刷新次数、账号核验或其它资源请求")
			}
		})
	}
}
