package store

import (
	"encoding/json"
	"errors"
	"time"

	"emby-go/internal/cloud115"
	"github.com/google/uuid"
)

type Cloud115Connection struct {
	AccountID string `json:"account_id"`
	FolderID  string `json:"folder_id"`
	Revision  int64  `json:"revision"`
	Secrets   []byte `json:"-"`
}
type OfflineTask struct {
	ID               string          `json:"id"`
	AccountID        string          `json:"account_id"`
	InfoHash         string          `json:"info_hash"`
	Magnet           string          `json:"magnet"`
	FolderID         string          `json:"folder_id"`
	Status           string          `json:"status"`
	Resume           string          `json:"resume"`
	Stage            string          `json:"stage"`
	Error            string          `json:"error"`
	Retryable        bool            `json:"retryable"`
	Progress         int             `json:"progress"`
	ResultID         string          `json:"result_id"`
	Attempts         int             `json:"attempts"`
	NextPoll         int64           `json:"next_poll"`
	VisibilityUntil  int64           `json:"visibility_until,omitempty"`
	CreatedAt        int64           `json:"created_at"`
	Deadline         int64           `json:"deadline"`
	UpdatedAt        int64           `json:"updated_at"`
	Version          int64           `json:"version"`
	Files            []cloud115.File `json:"files"`
	CacheFolderID    string          `json:"cache_folder_id,omitempty"`
	ExpiresAt        int64           `json:"expires_at,omitempty"`
	RecoveryFileID   string          `json:"recovery_file_id,omitempty"`
	RecoveryResultID string          `json:"recovery_result_id,omitempty"`
	RecoveryFolderID string          `json:"recovery_folder_id,omitempty"`
	RecoveryRemoved  bool            `json:"recovery_removed,omitempty"`
	Recoveries       int             `json:"recoveries,omitempty"`
}

func (t OfflineTask) CacheExpired(now int64) bool {
	return t.ExpiresAt > 0 && t.ExpiresAt <= now || t.Status == "expired" || t.Status == "cleanup_pending"
}

type OfflineBinding struct {
	Task       OfflineTask
	ResourceID string
	FileID     string
	Default    bool
}

func (s *Store) migrateCloud115() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS cloud115_connection(id INTEGER PRIMARY KEY CHECK(id=1), account_id TEXT NOT NULL, folder_id TEXT NOT NULL, revision INTEGER NOT NULL, secrets BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS offline_tasks(id TEXT PRIMARY KEY,account_id TEXT NOT NULL,info_hash TEXT NOT NULL,status TEXT NOT NULL,next_poll INTEGER NOT NULL,version INTEGER NOT NULL,data TEXT NOT NULL,UNIQUE(account_id,info_hash));
CREATE INDEX IF NOT EXISTS idx_offline_tasks_due ON offline_tasks(account_id,status,next_poll);
CREATE TABLE IF NOT EXISTS r18_offline_bindings(library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,library_revision INTEGER NOT NULL,video_id TEXT NOT NULL,resource_id TEXT NOT NULL,task_id TEXT NOT NULL REFERENCES offline_tasks(id),file_id TEXT NOT NULL DEFAULT '',is_default INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(library_id,library_revision,video_id,task_id));`)
	return err
}
func (s *Store) Cloud115Connection() (Cloud115Connection, error) {
	var c Cloud115Connection
	err := s.db.QueryRow("SELECT account_id,folder_id,revision,secrets FROM cloud115_connection WHERE id=1").Scan(&c.AccountID, &c.FolderID, &c.Revision, &c.Secrets)
	return c, err
}
func (s *Store) SaveCloud115Connection(c Cloud115Connection, expected int64) error {
	if expected == 0 {
		_, err := s.db.Exec("INSERT INTO cloud115_connection VALUES(1,?,?,1,?)", c.AccountID, c.FolderID, c.Secrets)
		return err
	}
	return affectedOne(s.db.Exec("UPDATE cloud115_connection SET account_id=?,folder_id=?,secrets=?,revision=revision+1 WHERE id=1 AND revision=?", c.AccountID, c.FolderID, c.Secrets, expected))
}
func (s *Store) RefreshCloud115Tokens(account string, revision int64, secrets []byte) error {
	return affectedOne(s.db.Exec("UPDATE cloud115_connection SET secrets=? WHERE id=1 AND account_id=? AND revision=?", secrets, account, revision))
}
func (s *Store) CreateOfflineTask(account, folder, magnet, hash string, l Library, video, resource string) (OfflineTask, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return OfflineTask{}, err
	}
	defer tx.Rollback()
	t := OfflineTask{ID: uuid.NewString(), AccountID: account, FolderID: folder, Magnet: magnet, InfoHash: hash, Status: "queued", CreatedAt: time.Now().Unix(), Version: 1, Files: []cloud115.File{}}
	b, err := json.Marshal(t)
	if err != nil {
		return t, err
	}
	if _, err = tx.Exec("INSERT INTO offline_tasks VALUES(?,?,?,?,?,?,?) ON CONFLICT(account_id,info_hash) DO NOTHING", t.ID, account, hash, t.Status, t.NextPoll, t.Version, string(b)); err != nil {
		return t, err
	}
	var data string
	if err = tx.QueryRow("SELECT data FROM offline_tasks WHERE account_id=? AND info_hash=?", account, hash).Scan(&data); err != nil {
		return t, err
	}
	if err = json.Unmarshal([]byte(data), &t); err != nil {
		return t, err
	}
	if t.Status == "expired" {
		t = OfflineTask{ID: t.ID, AccountID: account, FolderID: folder, Magnet: magnet, InfoHash: hash, Status: "queued", CreatedAt: time.Now().Unix(), Version: t.Version + 1, Files: []cloud115.File{}}
		b, _ = json.Marshal(t)
		if _, err = tx.Exec("UPDATE offline_tasks SET status=?,next_poll=0,version=?,data=? WHERE id=?", t.Status, t.Version, string(b), t.ID); err != nil {
			return t, err
		}
		if _, err = tx.Exec("UPDATE r18_offline_bindings SET file_id='',is_default=0 WHERE task_id=?", t.ID); err != nil {
			return t, err
		}
	}
	selected := ""
	if t.Status == "ready" && len(t.Files) == 1 {
		selected = t.Files[0].ID
	}
	var revision int64
	if err = tx.QueryRow("SELECT revision FROM libraries WHERE id=? AND revision=? AND source_type=?", l.ID, l.Revision, SourceR18).Scan(&revision); err != nil {
		return t, err
	}
	_, err = tx.Exec("INSERT INTO r18_offline_bindings(library_id,library_revision,video_id,resource_id,task_id,file_id) SELECT id,revision,?,?,?,? FROM libraries WHERE id=? AND revision=? AND source_type=? ON CONFLICT DO NOTHING", video, resource, t.ID, selected, l.ID, l.Revision, SourceR18)
	if err != nil {
		return t, err
	}
	if err = tx.Commit(); err != nil {
		return t, err
	}
	return t, nil
}
func (s *Store) OfflineTask(id string) (OfflineTask, error) {
	var t OfflineTask
	var data string
	err := s.db.QueryRow("SELECT data FROM offline_tasks WHERE id=?", id).Scan(&data)
	if err == nil {
		err = json.Unmarshal([]byte(data), &t)
	}
	return t, err
}
func (s *Store) UpdateOfflineTask(t OfflineTask, expected int64) error {
	t.Version = expected + 1
	t.UpdatedAt = time.Now().Unix()
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = affectedOne(tx.Exec("UPDATE offline_tasks SET status=?,next_poll=?,version=?,data=? WHERE id=? AND version=?", t.Status, t.NextPoll, t.Version, string(b), t.ID, expected)); err != nil {
		return err
	}
	if t.Status == "ready" && len(t.Files) == 1 {
		if _, err = tx.Exec("UPDATE r18_offline_bindings SET file_id=? WHERE task_id=? AND file_id=''", t.Files[0].ID, t.ID); err != nil {
			return err
		}
	}
	if t.CacheExpired(time.Now().Unix()) || t.Status == "queued" || t.Status == "submitting" || t.Status == "discovering" {
		if _, err = tx.Exec("UPDATE r18_offline_bindings SET file_id='',is_default=0 WHERE task_id=?", t.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DueOfflineCleanup(account string, now int64) ([]OfflineTask, error) {
	rows, err := s.db.Query("SELECT data FROM offline_tasks WHERE account_id=? AND status!='expired' AND json_extract(data,'$.expires_at')>0 AND json_extract(data,'$.expires_at')<=? AND (status!='cleanup_pending' OR next_poll<=?) ORDER BY next_poll,id LIMIT 50", account, now, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []OfflineTask{}
	for rows.Next() {
		var data string
		var t OfflineTask
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &t); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}
func (s *Store) DueOfflineTasks(account string, now int64) ([]OfflineTask, error) {
	rows, err := s.db.Query("SELECT data FROM offline_tasks WHERE account_id=? AND status IN ('queued','submitting','downloading','discovering') AND next_poll<=? AND (COALESCE(json_extract(data,'$.expires_at'),0)<=0 OR json_extract(data,'$.expires_at')>?) ORDER BY next_poll,id LIMIT 50", account, now, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []OfflineTask{}
	for rows.Next() {
		var data string
		var t OfflineTask
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &t); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}
func (s *Store) OfflineBindings(l Library, video, account string) ([]OfflineBinding, error) {
	rows, err := s.db.Query("SELECT t.data,b.resource_id,b.file_id,b.is_default FROM r18_offline_bindings b JOIN offline_tasks t ON t.id=b.task_id WHERE b.library_id=? AND b.library_revision=? AND b.video_id=? AND t.account_id=? ORDER BY b.is_default DESC,t.id", l.ID, l.Revision, video, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []OfflineBinding{}
	for rows.Next() {
		var b OfflineBinding
		var data string
		if err = rows.Scan(&data, &b.ResourceID, &b.FileID, &b.Default); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &b.Task); err != nil {
			return nil, err
		}
		list = append(list, b)
	}
	return list, rows.Err()
}
func (s *Store) SelectOfflineFile(l Library, video, taskID, fileID string, makeDefault bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Reserve the writer before reading: upgrading a deferred read snapshot can
	// return SQLITE_BUSY immediately when progress/backfill writes concurrently.
	if _, err = tx.Exec("UPDATE offline_tasks SET version=version WHERE id=?", taskID); err != nil {
		return err
	}
	var data string
	if err = tx.QueryRow("SELECT data FROM offline_tasks WHERE id=? AND status IN ('ready','waiting_selection')", taskID).Scan(&data); err != nil {
		return err
	}
	var t OfflineTask
	if err = json.Unmarshal([]byte(data), &t); err != nil {
		return err
	}
	if t.CacheExpired(time.Now().Unix()) {
		return errors.New("115 缓存已到期，不能再选择播放文件")
	}
	valid := false
	for _, f := range t.Files {
		if f.ID == fileID && cloud115.VideoFile(f) {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("所选文件不属于该任务")
	}
	if err = affectedOne(tx.Exec("UPDATE r18_offline_bindings SET file_id=? WHERE library_id=? AND library_revision=? AND video_id=? AND task_id=?", fileID, l.ID, l.Revision, video, taskID)); err != nil {
		return err
	}
	if makeDefault {
		if _, err = tx.Exec("UPDATE r18_offline_bindings SET is_default=(task_id=?) WHERE library_id=? AND library_revision=? AND video_id=?", taskID, l.ID, l.Revision, video); err != nil {
			return err
		}
	}
	t.Status = "ready"
	t.Version++
	t.UpdatedAt = time.Now().Unix()
	b, _ := json.Marshal(t)
	if _, err = tx.Exec("UPDATE offline_tasks SET status=?,version=?,data=? WHERE id=?", t.Status, t.Version, string(b), taskID); err != nil {
		return err
	}
	return tx.Commit()
}
