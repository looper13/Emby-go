package store

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
)

func newOfflineAutomaticSelectionFixture(t *testing.T) (*Store, Library, string, OfflineTask) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "automatic-selection.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.waitFeaturesReady()
	library, err := s.CreateR18Library("JAVDB", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	video := r18.EncodeID("automatic-selection", "javdb")
	hash := strings.Repeat("a", 40)
	task, err := s.CreateOfflineTask(CloudGuangyaAccount("fixture"), "work", "magnet:?xt=urn:btih:"+hash, hash, library, video, "btih-"+hash)
	if err != nil {
		t.Fatal(err)
	}
	task.Status, task.ExpiresAt, task.CacheFolderID = "waiting_selection", time.Now().Add(time.Hour).Unix(), "private-cache"
	task.Files = []cloud115.File{{ID: "small", Name: "small.mp4", PickCode: "small", Size: 100}, {ID: "large", Name: "large.mp4", PickCode: "large", Size: 200}}
	if err := s.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	task, err = s.OfflineTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, library, video, task
}

func TestOfflineAutomaticSelectionConcurrentCASSelectsOnlyOnce(t *testing.T) {
	s, library, video, task := newOfflineAutomaticSelectionFixture(t)
	otherHash := strings.Repeat("b", 40)
	other, err := s.CreateOfflineTask(task.AccountID, task.FolderID, "magnet:?xt=urn:btih:"+otherHash, otherHash, library, video, "btih-"+otherHash)
	if err != nil {
		t.Fatal(err)
	}
	other.Status = "waiting_selection"
	other.Files = []cloud115.File{{ID: "other", Name: "other.mp4", PickCode: "other", Size: 100}}
	if err := s.UpdateOfflineTask(other, other.Version); err != nil {
		t.Fatal(err)
	}
	if err := s.SelectOfflineFile(library, video, other.ID, "other", true); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 16)
	var workers sync.WaitGroup
	for index := 0; index < 16; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- s.SelectOfflineFileIfUnselected(library, video, task.ID, "large", task.Version)
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal("自动选择竞争返回了非 CAS 错误", err)
		}
	}
	current, err := s.OfflineTask(task.ID)
	if err != nil || successes != 1 || current.Version != task.Version+1 || current.Status != "ready" || current.ExpiresAt != task.ExpiresAt {
		t.Fatal("自动选择未保持单次版本更新和缓存生命周期", successes, current, err)
	}
	bindings, err := s.OfflineBindings(library, video, task.AccountID)
	if err != nil || len(bindings) != 2 {
		t.Fatal(bindings, err)
	}
	for _, binding := range bindings {
		if binding.Task.ID == task.ID && (binding.FileID != "large" || binding.Default) {
			t.Fatal("自动选择覆盖了全局默认版本", binding)
		}
		if binding.Task.ID == other.ID && (binding.FileID != "other" || !binding.Default) {
			t.Fatal("自动选择修改了另一条磁力的绑定", binding)
		}
	}
}

func TestOfflineAutomaticSelectionNeverOverwritesManualSelection(t *testing.T) {
	s, library, video, task := newOfflineAutomaticSelectionFixture(t)
	if err := s.SelectOfflineFile(library, video, task.ID, "small", true); err != nil {
		t.Fatal(err)
	}
	manual, err := s.OfflineTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int64{task.Version, manual.Version} {
		if err := s.SelectOfflineFileIfUnselected(library, video, task.ID, "large", version); !errors.Is(err, ErrConflict) {
			t.Fatal("自动选择覆盖了人工选择", err)
		}
	}
	bindings, err := s.OfflineBindings(library, video, task.AccountID)
	current, readErr := s.OfflineTask(task.ID)
	if err != nil || readErr != nil || len(bindings) != 1 || bindings[0].FileID != "small" || !bindings[0].Default || current.Version != manual.Version {
		t.Fatal("CAS 失败仍修改了人工选择或任务版本", bindings, current, err, readErr)
	}
	if err := s.SelectOfflineFile(library, video, task.ID, "large", true); err != nil {
		t.Fatal("原有人工选择语义受到限制", err)
	}
	bindings, err = s.OfflineBindings(library, video, task.AccountID)
	if err != nil || bindings[0].FileID != "large" {
		t.Fatal("人工选择不能继续修改版本", bindings, err)
	}
}

func TestOfflineAutomaticSelectionRejectsStaleVersionAndInvalidOwnership(t *testing.T) {
	s, library, video, task := newOfflineAutomaticSelectionFixture(t)
	wrongLibrary := library
	wrongLibrary.Revision++
	for _, selection := range []struct {
		library Library
		video   string
		file    string
		version int64
	}{
		{library, video, "large", 0},
		{library, video, "large", task.Version - 1},
		{wrongLibrary, video, "large", task.Version},
		{library, r18.EncodeID("another-video", "javdb"), "large", task.Version},
		{library, video, "foreign-file", task.Version},
	} {
		if err := s.SelectOfflineFileIfUnselected(selection.library, selection.video, task.ID, selection.file, selection.version); err == nil {
			t.Fatal("无效归属或版本允许自动选择", selection)
		}
	}
	current, err := s.OfflineTask(task.ID)
	bindings, bindingErr := s.OfflineBindings(library, video, task.AccountID)
	if err != nil || bindingErr != nil || current.Version != task.Version || len(bindings) != 1 || bindings[0].FileID != "" {
		t.Fatal("拒绝选择时仍改动了任务或绑定", current, bindings, err, bindingErr)
	}
	task.ExpiresAt = time.Now().Add(-time.Second).Unix()
	if err := s.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	task, err = s.OfflineTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SelectOfflineFileIfUnselected(library, video, task.ID, "large", task.Version); err == nil {
		t.Fatal("过期缓存允许自动选文件")
	}
}

func TestOfflineTaskFolderChangeDoesNotCreateBindingBeforeCleanup(t *testing.T) {
	s, library, video, task := newOfflineAutomaticSelectionFixture(t)
	newVideo := r18.EncodeID("another-video", "javdb")
	if _, err := s.CreateOfflineTask(task.AccountID, "new-work", task.Magnet, task.InfoHash, library, newVideo, "btih-"+task.InfoHash); !errors.Is(err, ErrConflict) {
		t.Fatal("未清理任务允许绑定其它缓存根目录", err)
	}
	bindings, err := s.OfflineBindings(library, newVideo, task.AccountID)
	current, readErr := s.OfflineTask(task.ID)
	if err != nil || readErr != nil || len(bindings) != 0 || current.Version != task.Version || current.FolderID != task.FolderID {
		t.Fatal("拒绝旧任务仍留下新绑定或修改任务", bindings, current, err, readErr)
	}
	task.Status = "expired"
	if err := s.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := s.CreateOfflineTask(task.AccountID, "new-work", task.Magnet, task.InfoHash, library, newVideo, "btih-"+task.InfoHash)
	if err != nil || rebuilt.ID != task.ID || rebuilt.Status != "queued" || rebuilt.FolderID != "new-work" || rebuilt.ExpiresAt != 0 || rebuilt.CacheFolderID != "" {
		t.Fatal("已完成清理的任务不能使用新缓存根目录重建", rebuilt, err)
	}
	bindings, err = s.OfflineBindings(library, newVideo, task.AccountID)
	oldBindings, oldErr := s.OfflineBindings(library, video, task.AccountID)
	if err != nil || oldErr != nil || len(bindings) != 1 || len(oldBindings) != 1 || bindings[0].Task.ID != task.ID || oldBindings[0].FileID != "" {
		t.Fatal("重建后的新旧绑定不一致", bindings, oldBindings, err, oldErr)
	}
}

func TestOfflineAutomaticSelectionRejectsHistoricLibraryRevision(t *testing.T) {
	s, library, video, task := newOfflineAutomaticSelectionFixture(t)
	if _, err := s.UpdateR18Connection(library.ID, library.Revision, library.Name, r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-new-key"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SelectOfflineFileIfUnselected(library, video, task.ID, "large", task.Version); !errors.Is(err, ErrConflict) {
		t.Fatal("历史资料库版本允许自动选文件", err)
	}
	bindings, err := s.OfflineBindings(library, video, task.AccountID)
	current, readErr := s.OfflineTask(task.ID)
	if err != nil || readErr != nil || len(bindings) != 1 || bindings[0].FileID != "" || current.Version != task.Version {
		t.Fatal("资料库版本变化仍修改了历史绑定", bindings, current, err, readErr)
	}
}
