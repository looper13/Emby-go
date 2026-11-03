package store

// CloudGuangyaConnection 与 115 独立保存，避免凭据和配置版本相互覆盖。
type CloudGuangyaConnection struct {
	AccountID string `json:"account_id"`
	FolderID  string `json:"folder_id"`
	Revision  int64  `json:"revision"`
	Secrets   []byte `json:"-"`
}

// CloudGuangyaAccount 给共享离线任务使用的账号增加网盘命名空间。
func CloudGuangyaAccount(account string) string { return "guangya:" + account }

func (s *Store) migrateCloudGuangya() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS cloud_guangya_connection(id INTEGER PRIMARY KEY CHECK(id=1), account_id TEXT NOT NULL, folder_id TEXT NOT NULL, revision INTEGER NOT NULL, secrets BLOB NOT NULL)`)
	return err
}

func (s *Store) CloudGuangyaConnection() (CloudGuangyaConnection, error) {
	var c CloudGuangyaConnection
	err := s.db.QueryRow("SELECT account_id,folder_id,revision,secrets FROM cloud_guangya_connection WHERE id=1").Scan(&c.AccountID, &c.FolderID, &c.Revision, &c.Secrets)
	return c, err
}

func (s *Store) SaveCloudGuangyaConnection(c CloudGuangyaConnection, expected int64) error {
	if expected == 0 {
		_, err := s.db.Exec("INSERT INTO cloud_guangya_connection VALUES(1,?,?,1,?)", c.AccountID, c.FolderID, c.Secrets)
		return err
	}
	return affectedOne(s.db.Exec("UPDATE cloud_guangya_connection SET account_id=?,folder_id=?,secrets=?,revision=revision+1 WHERE id=1 AND revision=?", c.AccountID, c.FolderID, c.Secrets, expected))
}

func (s *Store) RefreshCloudGuangyaTokens(account string, revision int64, secrets []byte) error {
	return affectedOne(s.db.Exec("UPDATE cloud_guangya_connection SET secrets=? WHERE id=1 AND account_id=? AND revision=?", secrets, account, revision))
}
