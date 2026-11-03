package cloud115

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func cookieTaskResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestCookieTaskIDUsesCanonicalHashWithoutRequests(t *testing.T) {
	hash := strings.Repeat("a", 40)
	c := newCookieClient(fixtureCookie, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("本地生成或核验身份不得请求网盘")
		return nil, nil
	}), 0)
	for _, value := range []string{hash, strings.ToUpper(hash)} {
		id, err := c.TaskIDForHash(value)
		if err != nil || id != hash {
			t.Fatal("未生成规范任务身份", id, err)
		}
	}
	for _, value := range []string{"", "short", strings.Repeat("g", 40), hash + "a"} {
		if _, err := c.TaskIDForHash(value); err == nil {
			t.Fatal("接受了非法任务身份")
		}
	}
	for _, id := range []string{"", "invalid", strings.Repeat("b", 40)} {
		if task, err := c.FindTask(context.Background(), hash, id); task != nil || err == nil {
			t.Fatal("接受了与磁力不一致的查询身份", task, err)
		}
	}
	if err := c.RemoveTask(context.Background(), hash); err == nil {
		t.Fatal("缺少缓存归属时不应使用旧删除接口")
	}
}

func TestCookieTaskSubmissionReceiptRequiresOneAcceptedIdentity(t *testing.T) {
	hash := strings.Repeat("a", 40)
	result := `{"info_hash":"` + hash + `"}`
	for _, test := range []struct {
		name, body string
		failed     bool
		retryable  bool
		auth       bool
	}{
		{name: "唯一规范身份", body: `{"state":true,"result":[` + result + `]}`},
		{name: "大小写身份", body: `{"result":[{"state":true,"info_hash":"` + strings.ToUpper(hash) + `","errcode":"0"}]}`},
		{name: "匹配资源地址", body: `{"result":[{"info_hash":"` + hash + `","url":"magnet:?xt=urn:btih:` + hash + `"}]}`},
		{name: "损坏结构", body: `{`, failed: true, retryable: true},
		{name: "缺少结果", body: `{"state":true}`, failed: true, retryable: true},
		{name: "空结果", body: `{"result":[]}`, failed: true, retryable: true},
		{name: "多项回执", body: `{"result":[` + result + `,` + result + `]}`, failed: true, retryable: true},
		{name: "缺少身份", body: `{"result":[{"state":true}]}`, failed: true, retryable: true},
		{name: "身份冲突", body: `{"result":[{"info_hash":"` + strings.Repeat("b", 40) + `"}]}`, failed: true, retryable: true},
		{name: "地址身份冲突", body: `{"result":[{"info_hash":"` + hash + `","url":"magnet:?xt=urn:btih:` + strings.Repeat("b", 40) + `"}]}`, failed: true, retryable: true},
		{name: "地址格式错误", body: `{"result":[{"info_hash":"` + hash + `","url":"invalid"}]}`, failed: true, retryable: true},
		{name: "状态码格式错误", body: `{"errno":"unknown","result":[` + result + `]}`, failed: true, retryable: true},
		{name: "单项状态码格式错误", body: `{"result":[{"errcode":1.5,"info_hash":"` + hash + `"}]}`, failed: true, retryable: true},
		{name: "外层明确拒绝", body: `{"state":false,"result":[` + result + `]}`, failed: true},
		{name: "单项明确拒绝", body: `{"result":[{"state":false,"info_hash":"` + hash + `"}]}`, failed: true},
		{name: "外层配额拒绝", body: `{"state":true,"code":10010,"result":[` + result + `]}`, failed: true},
		{name: "单项配额拒绝", body: `{"result":[{"errno":"10010","info_hash":"` + hash + `"}]}`, failed: true},
		{name: "外层重复任务拒绝", body: `{"errcode":10008,"result":[` + result + `]}`, failed: true},
		{name: "单项重复任务拒绝", body: `{"result":[{"state":true,"errcode":10008,"info_hash":"` + hash + `"}]}`, failed: true},
		{name: "鉴权拒绝", body: `{"result":[{"code":99,"info_hash":"` + hash + `"}]}`, failed: true, retryable: true, auth: true},
		{name: "限速拒绝", body: `{"result":[{"code":429,"info_hash":"` + hash + `"}]}`, failed: true, retryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := parseCookieTaskSubmission([]byte(test.body), hash)
			if !test.failed {
				if err != nil {
					t.Fatal("有效创建回执被拒绝", err)
				}
				return
			}
			var remote *Error
			if !errors.As(err, &remote) || remote.Retryable != test.retryable || remote.Auth != test.auth || remote.NotSubmitted {
				t.Fatal("创建失败的证据不正确", err)
			}
		})
	}
}

func TestCookieTaskInvalidInputIsNeverSubmitted(t *testing.T) {
	hash := strings.Repeat("a", 40)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name, cookie, magnet, folder string
		ctx                          context.Context
	}{
		{name: "磁力格式错误", cookie: fixtureCookie, magnet: "invalid", folder: "12"},
		{name: "根目录", cookie: fixtureCookie, magnet: "magnet:?xt=urn:btih:" + hash, folder: "0"},
		{name: "目录格式错误", cookie: fixtureCookie, magnet: "magnet:?xt=urn:btih:" + hash, folder: "cache"},
		{name: "目录溢出", cookie: fixtureCookie, magnet: "magnet:?xt=urn:btih:" + hash, folder: strings.Repeat("9", 40)},
		{name: "Cookie缺失", cookie: "", magnet: "magnet:?xt=urn:btih:" + hash, folder: "12"},
		{name: "账号身份错误", cookie: "UID=invalid; CID=fixture-cid; SEID=fixture-seid", magnet: "magnet:?xt=urn:btih:" + hash, folder: "12"},
		{name: "已取消请求", cookie: fixtureCookie, magnet: "magnet:?xt=urn:btih:" + hash, folder: "12", ctx: cancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := newCookieClient(test.cookie, roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("输入校验失败时不应发送创建请求")
				return nil, nil
			}), 0)
			ctx := test.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			id, err := c.AddTask(ctx, test.magnet, test.folder)
			var remote *Error
			if id != "" || !errors.As(err, &remote) || !remote.NotSubmitted {
				t.Fatal("未保留发送前失败的证据", id, err)
			}
		})
	}
}

func TestCookieTaskCancelledRateLimitWaitIsNotSubmitted(t *testing.T) {
	hash := strings.Repeat("a", 40)
	c := newCookieClient(fixtureCookie, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("限速等待取消后不得开始传输")
		return nil, nil
	}), 0)
	limiter := c.http.Transport.(*limitedTransport)
	limiter.next = time.Now().Add(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	id, err := c.AddTask(ctx, "magnet:?xt=urn:btih:"+hash, "12")
	var remote *Error
	if id != hash || !errors.As(err, &remote) || !remote.NotSubmitted || !remote.Retryable || ctx.Err() == nil {
		t.Fatal("限速等待取消被误判为已提交", id, err)
	}
}

func TestCookieTaskMalformedRSAKeepsIdentityForExactRecovery(t *testing.T) {
	hash := strings.Repeat("a", 40)
	adds, finds := 0, 0
	c := newCookieClient(fixtureCookie, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "lixian.115.com" || r.Method != http.MethodPost {
			t.Fatal("任务恢复使用了其他协议")
		}
		switch r.URL.Query().Get("ac") {
		case "add_task_urls":
			adds++
			if adds != 1 || finds != 0 || r.URL.Path != "/lixianssp/" || r.URL.Query().Get("t") == "" || r.UserAgent() != cookieOfflineUserAgent {
				t.Fatal("新建前查询、重复提交或应用请求头不正确")
			}
			if err := r.ParseForm(); err != nil || len(r.PostForm) != 1 || r.PostForm.Get("data") == "" {
				t.Fatal("创建请求未使用生产加密协议")
			}
			// 由生产解密函数拒绝损坏回执，不替换密钥或注入解密结果。
			return cookieTaskResponse(r, `{"state":true,"data":"malformed"}`), nil
		case "task_lists":
			finds++
			q := r.URL.Query()
			if adds != 1 || finds != 1 || r.URL.Path != "/lixian/" || q.Get("info_hash") != hash || q.Get("page") != "1" || q.Get("page_size") != "1" {
				t.Fatal("损坏回执后未使用单项确定性查询")
			}
			return cookieTaskResponse(r, `{"state":true,"page":1,"count":1,"page_count":1,"tasks":[{"info_hash":"`+hash+`","status":2,"percentDone":100,"file_id":"44","wp_path_id":"12"}]}`), nil
		default:
			t.Fatal("任务恢复出现额外请求")
			return nil, nil
		}
	}), 0)
	id, err := c.AddTask(context.Background(), "magnet:?xt=urn:btih:"+strings.ToUpper(hash), "12")
	var remoteError *Error
	if id != hash || !errors.As(err, &remoteError) || !remoteError.Retryable || remoteError.NotSubmitted || adds != 1 || finds != 0 {
		t.Fatal("发送后损坏回执丢失了恢复身份", id, err, adds, finds)
	}
	remote, err := c.FindTask(context.Background(), hash, id)
	if err != nil || remote == nil || remote.RemoteID != id || remote.Hash != hash || remote.FolderID != "12" || remote.ResultID != "44" || adds != 1 || finds != 1 {
		t.Fatal("未通过生产查询适配器恢复本次任务", remote, err, adds, finds)
	}
}

func TestCookieTaskRestartUsesSavedDeterministicIdentity(t *testing.T) {
	hash := strings.Repeat("a", 40)
	initial := newCookieClient(fixtureCookie, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("生成身份不得访问网盘")
		return nil, nil
	}), 0)
	id, err := initial.TaskIDForHash(hash)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	restarted := newCookieClient(fixtureCookie, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("info_hash") != hash || r.URL.Query().Get("page_size") != "1" {
			t.Fatal("重启后未按已保存的身份查询")
		}
		return cookieTaskResponse(r, `{"state":true,"page":1,"count":1,"page_count":1,"tasks":[{"info_hash":"`+hash+`","status":1,"percentDone":42.5,"file_id":"0","wp_path_id":"12"}]}`), nil
	}), 0)
	remote, err := restarted.FindTask(context.Background(), strings.ToUpper(hash), strings.ToUpper(id))
	if err != nil || remote == nil || remote.RemoteID != hash || remote.Status != 1 || remote.Progress != 42 || calls != 1 {
		t.Fatal("重启后的任务身份或状态不正确", remote, err, calls)
	}
}

func TestCookieTaskQueryRejectsInvalidStateAndResultIdentity(t *testing.T) {
	hash := strings.Repeat("a", 40)
	for name, task := range map[string]string{
		"状态超过范围": `"status":3,"percentDone":0`,
		"状态低于范围": `"status":-2,"percentDone":0`,
		"进度超过范围": `"status":1,"percentDone":101`,
		"进度低于范围": `"status":1,"percentDone":-1`,
		"错误结果身份": `"status":1,"file_id":"invalid","wp_path_id":"12"`,
		"错误目录身份": `"status":1,"file_id":"0","wp_path_id":"invalid"`,
		"完成无结果":  `"status":2,"percentDone":100,"wp_path_id":"12"`,
		"结果等于目录": `"status":2,"percentDone":100,"file_id":"12","wp_path_id":"12"`,
	} {
		t.Run(name, func(t *testing.T) {
			c := newCookieClient(fixtureCookie, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return cookieTaskResponse(r, `{"state":true,"page":1,"count":1,"page_count":1,"tasks":[{"info_hash":"`+hash+`",`+task+`}]}`), nil
			}), 0)
			remote, err := c.FindTask(context.Background(), hash, hash)
			var failure *Error
			if remote != nil || !errors.As(err, &failure) || !failure.Retryable {
				t.Fatal("非法任务被作为可用结果返回", remote, err)
			}
		})
	}
}

func TestCookieTaskRemovalRequiresFreshIdentityAndCacheOwnership(t *testing.T) {
	hash := strings.Repeat("a", 40)
	expected := RemoteTask{Hash: hash, RemoteID: hash, Status: 2, Progress: 100, ResultID: "44", FolderID: "12"}
	matching := `{"state":true,"page":1,"count":1,"page_count":1,"tasks":[{"info_hash":"` + hash + `","status":2,"percentDone":100,"file_id":"44","wp_path_id":"12"}]}`
	matchingFile := `{"state":true,"data":[{"fid":"44","cid":"12","n":"movie.mkv"}]}`
	for _, test := range []struct {
		name, task, file string
		expected         RemoteTask
		calls            int
		removed, failed  bool
	}{
		{name: "核验通过", task: matching, file: matchingFile, expected: expected, calls: 3, removed: true},
		{name: "明确缺失结果", task: matching, file: `{"state":false,"errno":20018}`, expected: expected, calls: 3, removed: true},
		{name: "任务已不存在", task: `{"state":true,"page":1,"count":0,"page_count":0,"tasks":[]}`, expected: expected, calls: 1},
		{name: "结果身份已变化", task: strings.ReplaceAll(matching, `"file_id":"44"`, `"file_id":"45"`), expected: expected, calls: 1, failed: true},
		{name: "保存目录已变化", task: strings.ReplaceAll(matching, `"wp_path_id":"12"`, `"wp_path_id":"13"`), expected: expected, calls: 1, failed: true},
		{name: "任务状态已变化", task: strings.ReplaceAll(matching, `"status":2`, `"status":1`), expected: expected, calls: 1, failed: true},
		{name: "列表不完整", task: `{"state":true,"page":1,"count":1,"page_count":1,"tasks":[]}`, expected: expected, calls: 1, failed: true},
		{name: "结果实际位于外部目录", task: matching, file: strings.ReplaceAll(matchingFile, `"cid":"12"`, `"cid":"13"`), expected: expected, calls: 2, failed: true},
		{name: "文件查询未知错误", task: matching, file: `{"state":false,"errno":123}`, expected: expected, calls: 2, failed: true},
		{name: "文件查询需要鉴权", task: matching, file: `{"state":false,"errno":99}`, expected: expected, calls: 2, failed: true},
		{name: "文件查询网络故障", task: matching, file: `{"state":false,"errno":503}`, expected: expected, calls: 2, failed: true},
		{name: "缺少磁力身份", expected: RemoteTask{RemoteID: hash, Status: 2, ResultID: "44", FolderID: "12"}, failed: true},
		{name: "错误远程身份", expected: RemoteTask{Hash: hash, RemoteID: strings.Repeat("b", 40), Status: 2, ResultID: "44", FolderID: "12"}, failed: true},
		{name: "缺少缓存归属", expected: RemoteTask{Hash: hash, RemoteID: hash, Status: 2, ResultID: "44"}, failed: true},
		{name: "根目录归属", expected: RemoteTask{Hash: hash, RemoteID: hash, Status: 2, ResultID: "44", FolderID: "0"}, failed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, removed := 0, false
			c := newCookieClient(fixtureCookie, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				switch {
				case r.URL.Query().Get("ac") == "task_lists":
					if calls != 1 || r.URL.Query().Get("info_hash") != hash {
						t.Fatal("移除前未重新核验任务身份")
					}
					return cookieTaskResponse(r, test.task), nil
				case r.URL.Path == "/files/get_info":
					if calls != 2 || r.URL.Query().Get("file_id") != "44" {
						t.Fatal("未核验本次任务的实际结果归属")
					}
					return cookieTaskResponse(r, test.file), nil
				case r.URL.Query().Get("ac") == "task_del":
					removed = true
					if !test.removed || r.Method != http.MethodPost || r.ParseForm() != nil || r.PostForm.Get("hash[0]") != hash || r.PostForm.Get("flag") != "0" {
						t.Fatal("身份未核验或删除请求扩展到了结果文件")
					}
					return cookieTaskResponse(r, `{"state":true}`), nil
				default:
					t.Fatal("移除过程中出现额外请求")
					return nil, nil
				}
			}), 0)
			err := c.RemoveTaskIf(context.Background(), hash, test.expected)
			if (err != nil) != test.failed || calls != test.calls || removed != test.removed {
				t.Fatal("条件移除结果不正确", err, calls, removed)
			}
		})
	}
}
