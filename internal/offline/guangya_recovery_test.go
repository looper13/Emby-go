package offline

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"emby-go/internal/clouddrive"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type conditionalGuangyaFixture struct {
	*guangyaFixture
	findCalls, conditionalCalls, unconditionalCalls int
	replaceOnFind                                   int
	beforeConditional                               func()
}

func newConditionalGuangyaFixture() *conditionalGuangyaFixture {
	return &conditionalGuangyaFixture{guangyaFixture: &guangyaFixture{newCacheFixture()}}
}

func (p *conditionalGuangyaFixture) Find(ctx context.Context, _ string) (*clouddrive.RemoteTask, error) {
	p.findCalls++
	if p.replaceOnFind > 0 && p.findCalls == p.replaceOnFind {
		copy := *p.remote
		copy.RemoteID = "replacement-record"
		p.remote = &copy
	}
	if p.findError != nil || p.remote == nil {
		return nil, p.findError
	}
	remote := *p.remote
	if remote.Status == 2 {
		file, err := p.Info(ctx, remote.ResultID)
		if isMissing(err) {
			remote.FolderID, remote.ResultMissing = "", true
			return &remote, nil
		}
		if err != nil {
			return nil, err
		}
		remote.FolderID, remote.ResultMissing = file.ParentID, false
	}
	return &remote, nil
}

func (p *conditionalGuangyaFixture) RemoveTask(context.Context, string) error {
	p.unconditionalCalls++
	return &clouddrive.Error{Message: "光鸭必须按任务身份条件移除"}
}

func (p *conditionalGuangyaFixture) RemoveTaskIf(ctx context.Context, hash string, expected clouddrive.RemoteTask) error {
	p.conditionalCalls++
	if p.beforeConditional != nil {
		p.beforeConditional()
	}
	current, err := p.Find(ctx, hash)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if current.RemoteID != expected.RemoteID || current.ResultID != expected.ResultID || current.Status != expected.Status || current.FolderID != "" && current.FolderID != expected.FolderID || current.FolderID == "" && (!current.ResultMissing || !expected.ResultMissing) {
		return &clouddrive.Error{Message: "光鸭任务身份变化，未移除记录", Retryable: true}
	}
	return p.cacheFixture.RemoveTask(ctx, hash)
}

func TestGuangyaDeletedOwnedResultWithoutParentRecoversAfterReopen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "guangya-recovery.db")
	db, err := store.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	library, err := db.CreateR18Library("JAVDB", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	video := r18.EncodeID("test", "digital")
	task := newGuangyaTask(t, db, library, video)
	p := newConditionalGuangyaFixture()
	service := guangyaService(db, p)
	service.process(p, task)
	p.remote = &clouddrive.RemoteTask{RemoteID: "owned-record", Status: 2, ResultID: "old-result"}
	p.entries["old-result"] = clouddrive.File{ID: "old-result", ParentID: "cache", Name: "movie.mp4", Size: 100, PickCode: "fixture"}
	service.process(p, read(t, db, task.ID))
	task = read(t, db, task.ID)
	if task.Status != "ready" || task.ResultID != "old-result" || len(task.Files) != 1 {
		t.Fatal("原始光鸭结果未通过归属核验并发布", task.Status)
	}
	created, expires := task.CreatedAt, task.ExpiresAt
	delete(p.entries, "old-result")
	// 模拟播放确认文件删除后进入发现阶段，持久结果归属仍然保留。
	task.Status, task.RecoveryFileID, task.Files = "discovering", "old-result", []clouddrive.File{}
	if err := db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	service.process(p, read(t, db, task.ID))
	task = read(t, db, task.ID)
	if task.RecoveryResultID != "old-result" || task.RecoveryFolderID != "cache" || task.Recoveries != 1 || p.removed != 0 {
		t.Fatal("已证明归属的缺失结果没有生成持久恢复意图", task.Status, task.RecoveryFolderID)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	service = guangyaService(db, p)
	for i := 0; i < 8 && p.adds < 2; i++ {
		service.process(p, read(t, db, task.ID))
	}
	task = read(t, db, task.ID)
	if p.adds != 2 || p.removed != 1 || p.conditionalCalls != 1 || p.unconditionalCalls != 0 || len(p.deleted) != 0 || task.Recoveries != 1 || task.CreatedAt != created || task.ExpiresAt != expires || task.Status != "downloading" {
		t.Fatal("重启后恢复未保持任务身份、缓存期限或单次提交边界", task.Status, p.adds, p.removed, p.conditionalCalls)
	}
	bindings, err := db.OfflineBindings(library, video, store.CloudGuangyaAccount("account"))
	if err != nil || len(bindings) != 1 || bindings[0].FileID != "" || len(bindings[0].Task.Files) != 0 {
		t.Fatal("已删除的光鸭播放源未失效", err)
	}
}

func TestGuangyaMissingResultDoesNotInventOwnershipOrDeletionEvidence(t *testing.T) {
	for _, scenario := range []string{"stranger", "network", "auth", "auth-missing", "missing-flag-absent", "wrong-recovery-folder", "invalid-cache"} {
		t.Run(scenario, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			task.CacheFolderID, task.ExpiresAt, task.Status = "cache", time.Now().Add(time.Hour).Unix(), "downloading"
			p := newConditionalGuangyaFixture()
			p.entries["cache"] = clouddrive.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
			p.remote = &clouddrive.RemoteTask{RemoteID: "unknown-record", Status: 2, ResultID: "stranger-result"}
			if scenario != "stranger" {
				task.ResultID = "stranger-result"
			}
			switch scenario {
			case "network":
				p.infoErrors = map[string]error{"stranger-result": &clouddrive.Error{Message: "光鸭网络失败", Retryable: true}}
			case "auth":
				p.infoErrors = map[string]error{"stranger-result": &clouddrive.Error{Message: "光鸭鉴权失败", Auth: true}}
			case "auth-missing":
				p.infoErrors = map[string]error{"stranger-result": &clouddrive.Error{Message: "光鸭鉴权失败", Auth: true, Missing: true}}
			case "missing-flag-absent":
				remote := &clouddrive.RemoteTask{RemoteID: "unknown-record", Status: 2, ResultID: task.ResultID}
				if provenMissingTaskFolder(p, task, remote).FolderID != "" {
					t.Fatal("没有明确删除标记仍补充了保存目录")
				}
				return
			case "wrong-recovery-folder":
				task.ResultID, task.RecoveryResultID, task.RecoveryFolderID = "", "stranger-result", "external"
			case "invalid-cache":
				task.CacheFolderID = "work"
			}
			if err := db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			service := guangyaService(db, p)
			for i := 0; i < 2; i++ {
				service.process(p, read(t, db, task.ID))
			}
			current := read(t, db, task.ID)
			if current.Recoveries != 0 || p.adds != 0 || p.removed != 0 || p.conditionalCalls != 0 || len(p.deleted) != 0 {
				t.Fatal("未证明归属或删除的结果触发了恢复或远程写入", scenario, current.Status, p.adds, p.removed)
			}
		})
	}
}

func TestGuangyaRecoveryNeverRemovesReplacedRemoteRecord(t *testing.T) {
	for _, scenario := range []string{"stable", "between-find-checks", "before-conditional-remove"} {
		t.Run(scenario, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			task.Status, task.CacheFolderID, task.ExpiresAt = "queued", "cache", time.Now().Add(time.Hour).Unix()
			task.RecoveryResultID, task.RecoveryFileID, task.RecoveryFolderID = "old-result", "old-result", "cache"
			if err := db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			p := newConditionalGuangyaFixture()
			p.remote = &clouddrive.RemoteTask{RemoteID: "owned-record", Status: 2, ResultID: "old-result"}
			p.entries["cache"] = clouddrive.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
			if scenario == "between-find-checks" {
				p.replaceOnFind = 2
			} else if scenario == "before-conditional-remove" {
				p.beforeConditional = func() {
					p.remote = &clouddrive.RemoteTask{RemoteID: "replacement-record", Status: 2, ResultID: "external-result"}
				}
			}
			service := guangyaService(db, p)
			service.process(p, read(t, db, task.ID))
			if p.unconditionalCalls != 0 || p.adds != 0 || len(p.deleted) != 0 {
				t.Fatal("恢复调用了无条件移除、重复提交或删除文件")
			}
			if scenario == "stable" {
				if p.removed != 1 || p.conditionalCalls != 1 {
					t.Fatal("身份一致的已失效记录未条件移除", p.removed, p.conditionalCalls)
				}
			} else if p.removed != 0 || scenario == "between-find-checks" && p.conditionalCalls != 0 {
				t.Fatal("替换记录被移除或远程 ID 变化后仍尝试移除", scenario, p.removed, p.conditionalCalls)
			}
		})
	}
}

func TestGuangyaCleanupMissingOwnedResultUsesConditionalRemoval(t *testing.T) {
	for _, scenario := range []string{"stable", "stranger-result", "replacement"} {
		t.Run(scenario, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			task.Status, task.CacheFolderID, task.ResultID = "ready", "cache", "old-result"
			task.ExpiresAt = time.Now().Add(-time.Second).Unix()
			if err := db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			p := newConditionalGuangyaFixture()
			p.entries["cache"] = clouddrive.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
			p.remote = &clouddrive.RemoteTask{RemoteID: "owned-record", Status: 2, ResultID: "old-result"}
			if scenario == "stranger-result" {
				p.remote.ResultID = "stranger-result"
			} else if scenario == "replacement" {
				p.beforeConditional = func() {
					p.remote = &clouddrive.RemoteTask{RemoteID: "replacement-record", Status: 2, ResultID: "stranger-result"}
				}
			}
			service := guangyaService(db, p)
			service.tickCleanup()
			current := read(t, db, task.ID)
			if p.unconditionalCalls != 0 {
				t.Fatal("光鸭清理使用了无条件移除")
			}
			if scenario == "stable" {
				if current.Status != "expired" || p.removed != 1 || p.conditionalCalls != 1 || len(p.deleted) != 1 || p.deleted[0] != "cache" {
					t.Fatal("已证明归属的缺失结果无法自动清理", current.Status, p.removed, p.deleted)
				}
			} else if current.Status != "cleanup_pending" || p.removed != 0 || len(p.deleted) != 0 {
				t.Fatal("清理移除了陌生或替换记录", current.Status, p.removed, p.deleted)
			}
		})
	}
}
