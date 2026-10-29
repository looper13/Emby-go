package server

import (
	"encoding/json"
	"time"

	"emby-go/internal/cache"
	"emby-go/internal/cloud115"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const cloudPreparationLifetime = 5 * time.Second

type preparedCloudFile struct {
	Nonce, Video, Task, Account      string
	User, Library, Revision, Version int64
	ConnectionRevision               int64
	File                             cloud115.File
}

// Only fresh metadata crosses this single-play, bounded in-memory handoff.
// Neither credentials nor signed CDN URLs are cached or included in the nonce.
func (a *App) prepareCloudFile(c *gin.Context, l store.Library, video string, task store.OfflineTask, file cloud115.File) {
	conn, err := a.db.Cloud115Connection()
	if err != nil || conn.AccountID != task.AccountID {
		return
	}
	proof := preparedCloudFile{Nonce: uuid.NewString(), User: a.db.UserID(), Library: l.ID, Revision: l.Revision, Video: video, Task: task.ID, Version: task.Version, Account: conn.AccountID, ConnectionRevision: conn.Revision, File: file}
	data, err := json.Marshal(proof)
	if err != nil || len(data) > 8192 {
		return
	}
	a.cloud115PrepareMu.Lock()
	if a.cloud115Prepared == nil {
		a.cloud115Prepared = cache.NewMemoryLimited(256, 8192)
	}
	a.cloud115Prepared.Set(proof.Nonce, data, cloudPreparationLifetime)
	a.cloud115PrepareMu.Unlock()
	c.Set("cloud115Preparation", proof)
}

func (a *App) takeCloudPreparation(c *gin.Context, l store.Library, video string, task store.OfflineTask, chosen cloud115.File) (cloud115.File, bool) {
	nonce := c.Query("preparation_token")
	if len(nonce) != 36 {
		return cloud115.File{}, false
	}
	conn, err := a.db.Cloud115Connection()
	if err != nil {
		return cloud115.File{}, false
	}
	a.cloud115PrepareMu.Lock()
	defer a.cloud115PrepareMu.Unlock()
	if a.cloud115Prepared == nil {
		return cloud115.File{}, false
	}
	data, ok := a.cloud115Prepared.Get(nonce)
	var proof preparedCloudFile
	if !ok || json.Unmarshal(data, &proof) != nil || proof.User != a.db.UserID() || proof.Library != l.ID || proof.Revision != l.Revision || proof.Video != video || proof.Task != task.ID || proof.Version != task.Version || proof.Account != task.AccountID || proof.ConnectionRevision != conn.Revision || proof.Account != conn.AccountID || proof.File.ID != chosen.ID || proof.File.Directory || proof.File.PickCode == "" || chosen.SHA1 != "" && proof.File.SHA1 != chosen.SHA1 {
		return cloud115.File{}, false
	}
	a.cloud115Prepared.Delete(nonce)
	return proof.File, true
}
