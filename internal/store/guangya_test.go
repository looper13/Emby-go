package store

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
)

func TestCloudGuangyaConnectionReopenAndRevisionIsolation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "guangya.db")
	s, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	if err := s.SaveCloud115Connection(Cloud115Connection{AccountID: "same-account", FolderID: "115-folder", Secrets: []byte("115-test-ciphertext")}, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCloudGuangyaConnection(CloudGuangyaConnection{AccountID: "same-account", FolderID: "guangya-folder", Secrets: []byte("guangya-test-ciphertext")}, 0); err != nil {
		t.Fatal(err)
	}
	connection, err := s.CloudGuangyaConnection()
	if err != nil || connection.Revision != 1 {
		t.Fatal(connection, err)
	}
	connection.Secrets = []byte("new-guangya-ciphertext")
	if err := s.SaveCloudGuangyaConnection(connection, connection.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshCloudGuangyaTokens(connection.AccountID, 1, []byte("stale-test-ciphertext")); err == nil {
		t.Fatal("过期客户端覆盖了新凭据")
	}
	if err := s.RefreshCloudGuangyaTokens("another-account", 2, []byte("wrong-account-ciphertext")); err == nil {
		t.Fatal("其它账号覆盖了光鸭凭据")
	}
	if err := s.RefreshCloudGuangyaTokens(connection.AccountID, 2, []byte("refreshed-guangya-ciphertext")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	guangya, err := s.CloudGuangyaConnection()
	if err != nil || guangya.AccountID != "same-account" || guangya.FolderID != "guangya-folder" || guangya.Revision != 2 || !bytes.Equal(guangya.Secrets, []byte("refreshed-guangya-ciphertext")) {
		t.Fatal("光鸭配置未正确持久化", guangya.Revision, err)
	}
	legacy, err := s.Cloud115Connection()
	if err != nil || legacy.AccountID != "same-account" || legacy.FolderID != "115-folder" || legacy.Revision != 1 || !bytes.Equal(legacy.Secrets, []byte("115-test-ciphertext")) {
		t.Fatal("光鸭更新影响了 115 配置", legacy.Revision, err)
	}
	encoded, err := json.Marshal(guangya)
	if err != nil || bytes.Contains(encoded, guangya.Secrets) || bytes.Contains(encoded, []byte("secrets")) {
		t.Fatal("配置响应包含加密凭据", err)
	}
}

func TestCloudGuangyaOfflineAccountNamespaceIsolation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "namespace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	library, err := s.CreateR18Library("JAVDB", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	video := r18.EncodeID("test", "digital")
	hash := strings.Repeat("a", 40)
	magnet := "magnet:?xt=urn:btih:" + hash
	legacy, err := s.CreateOfflineTask("same-account", "115-folder", magnet, hash, library, video, "resource")
	if err != nil {
		t.Fatal(err)
	}
	account := CloudGuangyaAccount("same-account")
	guangya, err := s.CreateOfflineTask(account, "guangya-folder", magnet, hash, library, video, "resource")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateOfflineTask(account, "guangya-folder", magnet, hash, library, video, "resource")
	if err != nil || guangya.ID == legacy.ID || again.ID != guangya.ID || guangya.AccountID != "guangya:same-account" {
		t.Fatal("不同网盘相同账号和磁力未隔离，或光鸭重复创建任务", err)
	}
	for _, expected := range []OfflineTask{legacy, guangya} {
		bindings, err := s.OfflineBindings(library, video, expected.AccountID)
		if err != nil || len(bindings) != 1 || bindings[0].Task.ID != expected.ID {
			t.Fatal("影片绑定跨网盘混用", expected.AccountID, err)
		}
		due, err := s.DueOfflineTasks(expected.AccountID, time.Now().Unix())
		if err != nil || len(due) != 1 || due[0].ID != expected.ID {
			t.Fatal("后台队列跨网盘混用", expected.AccountID, err)
		}
	}
}

func TestCloudGuangyaRemoteTaskIDReopensAndReadyResourceIsReusedLocally(t *testing.T) {
	file := filepath.Join(t.TempDir(), "task-id.db")
	s, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	library, err := s.CreateR18Library("JAVDB", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	account := CloudGuangyaAccount("fixture-account")
	video, hash := r18.EncodeID("test", "digital"), strings.Repeat("b", 40)
	magnet := "magnet:?xt=urn:btih:" + hash
	task, err := s.CreateOfflineTask(account, "cache-root", magnet, hash, library, video, "resource")
	if err != nil {
		t.Fatal(err)
	}
	task.Status, task.RemoteTaskID, task.CacheFolderID = "ready", "submitted-task", "owned-child"
	task.ExpiresAt = time.Now().Add(time.Hour).Unix()
	task.Files = []cloud115.File{{ID: "video", ParentID: "owned-child", PickCode: "video", Name: "正片.mp4", Size: 100}}
	if err := s.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateOfflineTask(account, "cache-root", magnet, hash, library, video, "resource")
	if err != nil || again.ID != task.ID || again.RemoteTaskID != "submitted-task" || again.Status != "ready" || again.Version != task.Version+1 {
		t.Fatal("重启丢失远程任务身份或重复创建本地就绪任务", again, err)
	}
	bindings, err := s.OfflineBindings(library, video, account)
	if err != nil || len(bindings) != 1 || bindings[0].FileID != "video" {
		t.Fatal("本地已就绪资源未直接复用播放绑定", bindings, err)
	}
	again.Status = "expired"
	if err := s.UpdateOfflineTask(again, again.Version); err != nil {
		t.Fatal(err)
	}
	restarted, err := s.CreateOfflineTask(account, "cache-root", magnet, hash, library, video, "resource")
	if err != nil || restarted.Status != "queued" || restarted.RemoteTaskID != "" || restarted.CacheFolderID != "" {
		t.Fatal("到期后新下载复用了旧远程任务身份", restarted, err)
	}
}
