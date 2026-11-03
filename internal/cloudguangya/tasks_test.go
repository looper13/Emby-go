package cloudguangya

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"emby-go/internal/clouddrive"
)

func taskQueryBody(taskID string) map[string]any {
	return map[string]any{"taskIds": []any{taskID}, "pageSize": float64(100), "mode": "cursor"}
}

func TestAddTaskReturnsCreatedIDWithoutInspectingHistory(t *testing.T) {
	magnet, _, err := clouddrive.NormalizeMagnet(testMagnet)
	if err != nil {
		t.Fatal(err)
	}
	resolves, creates, polls := 0, 0, 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		body := requestBody(t, r)
		switch r.URL.Path {
		case taskAPI + "resolve_res":
			resolves++
			if body["url"] != magnet {
				t.Fatalf("解析了错误磁力：%v", body)
			}
			return successReply(map[string]any{"btResInfo": map[string]any{"infoHash": testHash}})
		case taskAPI + "create_task":
			creates++
			if !reflect.DeepEqual(body, map[string]any{"url": magnet, "parentId": "owned-child"}) {
				t.Fatalf("创建了错误资源或目录：%v", body)
			}
			return successReply(map[string]any{"taskId": "created-task", "url": testMagnet})
		case taskAPI + "list_task":
			polls++
			if !reflect.DeepEqual(body, taskQueryBody("created-task")) {
				t.Fatalf("轮询没有使用本次创建的身份：%v", body)
			}
			return successReply(map[string]any{"list": []any{taskData("created-task", testHash, 1)}, "total": 1})
		default:
			t.Fatalf("请求了额外接口：%s", r.URL.Path)
			return nil, nil
		}
	})
	id, err := c.AddTask(context.Background(), testMagnet, "owned-child")
	if err != nil || id != "created-task" || creates != 1 || resolves != 1 || polls != 0 {
		t.Fatalf("直接创建或返回任务 ID 失败：%s %v 创建=%d 解析=%d 查询=%d", id, err, creates, resolves, polls)
	}
	remote, err := c.Find(context.Background(), testHash)
	if err != nil || remote == nil || remote.RemoteID != id || remote.FolderID != "owned-child" || polls != 1 {
		t.Fatalf("已知提交未按任务 ID 轮询或丢失目录：%+v %v 查询=%d", remote, err, polls)
	}
}

func TestFindTaskAfterRestartUsesPersistedID(t *testing.T) {
	creator := mockClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case taskAPI + "resolve_res":
			return successReply(map[string]any{"btResInfo": map[string]any{"infoHash": testHash}})
		case taskAPI + "create_task":
			return successReply(map[string]any{"taskId": "persisted-task"})
		default:
			t.Fatalf("创建前搜索了历史：%s", r.URL.Path)
			return nil, nil
		}
	})
	id, err := creator.AddTask(context.Background(), testMagnet, "owned-child")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	restarted := mockClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != taskAPI+"list_task" || !reflect.DeepEqual(requestBody(t, r), taskQueryBody(id)) {
			t.Fatalf("重启后没有使用保存的任务 ID：%s", r.URL.Path)
		}
		task := taskData(id, testHash, 1)
		task["parentId"] = "owned-child"
		return successReply(map[string]any{"list": []any{task}, "total": 1, "hasMore": false, "cursor": "final-cursor"})
	})
	remote, err := restarted.FindTask(context.Background(), testHash, id)
	if err != nil || remote == nil || remote.RemoteID != id || remote.FolderID != "owned-child" || calls != 1 || len(restarted.submissions) != 0 {
		t.Fatalf("无内存提交记录时恢复失败：%+v %v 调用=%d", remote, err, calls)
	}
}

func TestFindAndRemoveWithoutTaskIDNeverRequestHistory(t *testing.T) {
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("未知任务 ID 仍请求了远端：%s", r.URL.Path)
		return nil, nil
	})
	remote, err := c.Find(context.Background(), testHash)
	if remote != nil || err == nil || !requireError(t, err).Retryable || requireError(t, err).Missing {
		t.Fatalf("未知任务 ID 被视为不存在：%+v %v", remote, err)
	}
	if err := c.RemoveTask(context.Background(), testHash); err == nil || !requireError(t, err).Retryable || requireError(t, err).Missing {
		t.Fatalf("未知任务 ID 被用于删除历史：%v", err)
	}
}

func TestFindTaskRejectsInvalidIdentityBeforeRequests(t *testing.T) {
	for _, input := range []struct{ hash, id string }{
		{strings.Repeat("f", 39), "task"},
		{testHash, ""},
		{testHash, "0"},
		{testHash, " task "},
		{testHash, "other/task"},
		{testHash, "task\n"},
		{testHash, strings.Repeat("a", 513)},
	} {
		c := mockClient(func(r *http.Request) (*http.Response, error) {
			t.Fatalf("无效身份被用于远端请求：%s", r.URL.Path)
			return nil, nil
		})
		remote, err := c.FindTask(context.Background(), input.hash, input.id)
		if remote != nil || err == nil || !requireError(t, err).Retryable {
			t.Fatalf("接受了无效身份：%+v %v", input, err)
		}
	}
}

func TestFindTaskRequiresConsistentExactResponse(t *testing.T) {
	conflicting := taskData("target", testHash, 1)
	conflicting["infoHash"] = strings.Repeat("f", 40)
	badStatus := taskData("target", testHash, 1)
	badStatus["status"] = 99
	badProgress := taskData("target", testHash, 1)
	badProgress["progress"] = 101
	for name, data := range map[string]map[string]any{
		"缺少数量":    {"list": []any{taskData("target", testHash, 1)}},
		"空数量字段":   {"list": []any{}, "total": nil},
		"空列表字段":   {"list": nil, "total": 0},
		"非数字数量":   {"list": []any{}, "total": "unknown"},
		"负数数量":    {"list": []any{}, "total": -1},
		"数量过少":    {"list": []any{taskData("target", testHash, 1)}, "total": 0},
		"数量过多":    {"list": []any{taskData("target", testHash, 1)}, "total": 2},
		"重复任务记录":  {"list": []any{taskData("target", testHash, 1), taskData("target", testHash, 1)}, "total": 2},
		"无磁力身份":   {"list": []any{map[string]any{"taskId": "target", "status": 1, "fileName": testHash}}, "total": 1},
		"其他资源身份":  {"list": []any{map[string]any{"taskId": "target", "status": 1, "res": "https://example.com/resource"}}, "total": 1},
		"磁力身份冲突":  {"list": []any{conflicting}, "total": 1},
		"非法任务状态":  {"list": []any{badStatus}, "total": 1},
		"非法任务进度":  {"list": []any{badProgress}, "total": 1},
		"分页字段异常":  {"list": []any{taskData("target", testHash, 1)}, "total": 1, "hasMore": "false"},
		"空结果仍续页":  {"hasMore": true},
		"空结果有游标":  {"list": []any{}, "total": 0, "cursor": "unexpected-cursor"},
		"空结果未知字段": {"unknownField": 1},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if !reflect.DeepEqual(requestBody(t, r), taskQueryBody("target")) {
					t.Fatal("未使用精确任务筛选")
				}
				return successReply(data)
			})
			remote, err := c.FindTask(context.Background(), testHash, "target")
			if remote != nil || err == nil || !requireError(t, err).Retryable || requireError(t, err).Missing || calls != 1 {
				t.Fatalf("异常响应被当作不存在或触发续页：%+v %v 调用=%d", remote, err, calls)
			}
		})
	}
}

func TestFindTaskConfirmsEmptySelectionWithoutPagination(t *testing.T) {
	calls := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if !reflect.DeepEqual(requestBody(t, r), taskQueryBody("not-visible-yet")) {
			t.Fatal("未按本次任务查询")
		}
		return successReply(map[string]any{"list": []any{}, "total": 0, "cursor": ""})
	})
	remote, err := c.FindTask(context.Background(), testHash, "not-visible-yet")
	if err != nil || remote != nil || calls != 1 {
		t.Fatalf("明确空结果仍被续页或误分类：%+v %v 调用=%d", remote, err, calls)
	}
}

func TestFindTaskAcceptsOnlyVerifiedSparseEmptyResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"成功空对象", `{"msg":"success","data":{}}`, true},
		{"实测状态计数包装", `{"msg":"success","data":{"statusCounts":[{"status":2,"count":50000}]}}`, true},
		{"状态计数省略零值", `{"msg":"success","data":{"statusCounts":[{"count":11},{"status":1},{"status":2,"count":50000}]}}`, true},
		{"省略零值并重复状态", `{"msg":"success","data":{"statusCounts":[{},{"status":1},{"status":2,"count":50000},{"status":3,"count":4},{"status":1},{"status":5,"count":6}]}}`, true},
		{"显式零值分页", `{"msg":"success","data":{"list":[],"total":0,"cursor":"","hasMore":false,"statusCounts":[]}}`, true},
		{"省略列表保留零总数", `{"msg":"success","data":{"total":"0"}}`, true},
		{"无成功信封", `{"data":{}}`, false},
		{"失败主代码", `{"code":112,"msg":"success","data":{}}`, false},
		{"失败错误代码", `{"code":0,"error_code":500,"msg":"success","data":{}}`, false},
		{"失败消息", `{"code":0,"msg":"temporarily unavailable","data":{}}`, false},
		{"缺少数据", `{"msg":"success"}`, false},
		{"空数据值", `{"msg":"success","data":null}`, false},
		{"数组数据", `{"msg":"success","data":[]}`, false},
		{"实测未完成包装", `{"msg":"success","data":{"hasMore":true,"statusCounts":[{"status":2,"count":50000}]}}`, false},
		{"空查询保留游标", `{"msg":"success","data":{"cursor":"next","statusCounts":[{"status":2,"count":50000}]}}`, false},
		{"空查询非零总数", `{"msg":"success","data":{"total":1,"statusCounts":[{"status":2,"count":50000}]}}`, false},
		{"空查询未知字段", `{"msg":"success","data":{"statusCounts":[],"unknown":true}}`, false},
		{"空分页字段", `{"msg":"success","data":{"hasMore":null}}`, false},
		{"空列表字段", `{"msg":"success","data":{"list":null}}`, false},
		{"非布尔分页字段", `{"msg":"success","data":{"hasMore":"false"}}`, false},
		{"非字符串游标", `{"msg":"success","data":{"cursor":0}}`, false},
		{"空状态计数", `{"msg":"success","data":{"statusCounts":null}}`, false},
		{"非数组状态计数", `{"msg":"success","data":{"statusCounts":{}}}`, false},
		{"状态计数未知字段", `{"msg":"success","data":{"statusCounts":[{"status":2,"unknown":1}]}}`, false},
		{"负数状态计数", `{"msg":"success","data":{"statusCounts":[{"status":2,"count":-1}]}}`, false},
		{"未限定状态枚举", `{"msg":"success","data":{"statusCounts":[{"status":6,"count":1}]}}`, true},
		{"非数字状态计数", `{"msg":"success","data":{"statusCounts":[{"status":2,"count":"unknown"}]}}`, false},
		{"重复状态计数", `{"msg":"success","data":{"statusCounts":[{"status":2,"count":1},{"status":2,"count":2}]}}`, true},
		{"空状态计数值", `{"msg":"success","data":{"statusCounts":[{"status":2,"count":null}]}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != taskAPI+"list_task" || !reflect.DeepEqual(requestBody(t, r), taskQueryBody("saved-task")) {
					t.Fatal("稀疏空查询未使用完整的精确任务参数")
				}
				return mockReply(200, tc.body)
			})
			remote, err := c.FindTask(context.Background(), testHash, "saved-task")
			if remote != nil || (err == nil) != tc.valid || calls != 1 {
				t.Fatalf("稀疏空查询分类错误：有效=%v 任务=%+v 错误=%v 调用=%d", tc.valid, remote, err, calls)
			}
			if !tc.valid && requireError(t, err).Missing {
				t.Fatalf("未知或失败包装被判为任务不存在：%v", err)
			}
		})
	}
}

func TestFindTaskMatchedIdentityDoesNotDependOnAggregateCounts(t *testing.T) {
	for name, metadata := range map[string]any{
		"省略零值并重复状态": []any{
			map[string]any{},
			map[string]any{"status": 1},
			map[string]any{"status": 2, "count": 50000},
			map[string]any{"status": 3, "count": 4},
			map[string]any{"status": 1},
			map[string]any{"status": 5, "count": 6},
		},
		"未来聚合状态枚举": []any{map[string]any{"status": 7, "count": 1}},
		"无关聚合字段变化": []any{map[string]any{"future": true}},
		"无关聚合结构变化": map[string]any{"future": true},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == taskAPI+"list_task" {
					if !reflect.DeepEqual(requestBody(t, r), taskQueryBody("saved-task")) {
						t.Fatal("聚合计数导致查询了其他任务")
					}
					task := taskData("saved-task", testHash, 2)
					task["fileId"], task["parentId"] = "result", "owned-child"
					return successReply(map[string]any{"list": []any{task}, "total": 1, "cursor": "final-cursor", "statusCounts": metadata})
				}
				if r.URL.Path != "/userres/v1/file/get_info_by_file_id" || requestBody(t, r)["fileId"] != "result" {
					t.Fatalf("完成任务核验了错误结果：%s", r.URL.Path)
				}
				return successReply(fileData("result", "owned-child", "movie", true))
			})
			remote, err := c.FindTask(context.Background(), testHash, "saved-task")
			if err != nil || remote == nil || remote.RemoteID != "saved-task" || remote.ResultID != "result" || remote.FolderID != "owned-child" || calls != 2 {
				t.Fatalf("无关聚合计数阻断了已知任务或结果核验：%+v %v 调用=%d", remote, err, calls)
			}
		})
	}
}

func TestFindTaskKeepsExplicitIDWhenMemoryHasAnotherSubmission(t *testing.T) {
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		if !reflect.DeepEqual(requestBody(t, r), taskQueryBody("saved-task")) {
			t.Fatal("使用内存中的其他提交替换了显式任务 ID")
		}
		task := taskData("saved-task", testHash, 1)
		task["parentId"] = "saved-folder"
		return successReply(map[string]any{"list": []any{task}, "total": 1})
	})
	c.submissions[testHash] = submission{taskID: "other-task", folder: "other-folder"}
	remote, err := c.FindTask(context.Background(), testHash, "saved-task")
	if err != nil || remote == nil || remote.RemoteID != "saved-task" || remote.FolderID != "saved-folder" {
		t.Fatalf("显式身份或目录被其他提交污染：%+v %v", remote, err)
	}
}

func TestFindTaskRejectsForeignSubmissionFolderAndResult(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(fmt.Sprint(completed), func(t *testing.T) {
			c := mockClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == taskAPI+"list_task" {
					task := taskData("task", testHash, 1)
					if completed {
						task["status"], task["fileId"] = 2, "foreign-result"
					} else {
						task["parentId"] = "foreign-folder"
					}
					return successReply(map[string]any{"list": []any{task}, "total": 1})
				}
				if r.URL.Path != "/userres/v1/file/get_info_by_file_id" || requestBody(t, r)["fileId"] != "foreign-result" {
					t.Fatalf("核验了错误结果：%s", r.URL.Path)
				}
				return successReply(fileData("foreign-result", "foreign-folder", "movie", true))
			})
			c.submissions[testHash] = submission{taskID: "task", folder: "owned-child"}
			remote, err := c.FindTask(context.Background(), testHash, "task")
			if err == nil || remote != nil {
				t.Fatalf("使用了本次提交目录之外的结果：%+v %v", remote, err)
			}
		})
	}
}

func TestConditionalRemovalOfAbsentIDDoesNotDeleteAnotherTask(t *testing.T) {
	calls := 0
	c := mockClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != taskAPI+"list_task" || !reflect.DeepEqual(requestBody(t, r), taskQueryBody("saved-task")) {
			t.Fatalf("目标任务不存在时仍请求了删除或历史扫描：%s", r.URL.Path)
		}
		return successReply(map[string]any{"list": []any{}, "total": 0})
	})
	expected := clouddrive.RemoteTask{Hash: testHash, RemoteID: "saved-task", FolderID: "owned-child", Status: 1}
	if err := c.RemoveTaskIf(context.Background(), testHash, expected); err != nil || calls != 1 {
		t.Fatalf("精确空结果清理失败：%v 调用=%d", err, calls)
	}
}
