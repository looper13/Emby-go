package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type cloudFixture struct {
	infos, urls int
	ua          string
	missing     bool
	infoErr     error
	urlErr      error
}

func (f *cloudFixture) Check(context.Context, string) (string, error)              { return "115-account", nil }
func (f *cloudFixture) Find(context.Context, string) (*cloud115.RemoteTask, error) { return nil, nil }
func (f *cloudFixture) Add(context.Context, string, string) error                  { return nil }
func (f *cloudFixture) Info(_ context.Context, id string) (cloud115.File, error) {
	f.infos++
	if f.infoErr != nil {
		return cloud115.File{}, f.infoErr
	}
	if f.missing {
		return cloud115.File{}, &cloud115.Error{Message: "file not found", Missing: true}
	}
	return cloud115.File{ID: id, Name: "movie.mkv", PickCode: "fresh-code", SHA1: "file-sha", Size: 100}, nil
}
func (f *cloudFixture) List(context.Context, string, int) ([]cloud115.File, bool, error) {
	return []cloud115.File{{ID: "12", Name: cloud115.CacheFolderName, Directory: true, ParentID: "0"}}, false, nil
}
func (f *cloudFixture) Mkdir(context.Context, string, string) (string, error) { return "12", nil }
func (f *cloudFixture) Delete(context.Context, string) error                  { return nil }
func (f *cloudFixture) RemoveTask(context.Context, string) error              { return nil }
func (f *cloudFixture) URL(_ context.Context, code, ua string) (string, error) {
	f.urls++
	if f.urlErr != nil {
		return "", f.urlErr
	}
	f.ua = ua
	return "https://cdn.example/movie?sign=private", nil
}
func TestCloud115ConfigurationBindingsTicketsAndRedirect(t *testing.T) {
	a, ts, token := newProbeTestApp(t, t.TempDir())
	a.offlineService.Close()
	fake := &fakeR18{video: r18.Video{ID: r18.EncodeID("test", "digital"), Title: "test", Number: "TEST", Images: []string{}}}
	a.r18Open = func(r18.Config) (r18Source, error) { return fake, nil }
	p := &cloudFixture{}
	a.cloud115Open = func(cloud115.Tokens, func(cloud115.Tokens) error) cloud115.Provider { return p }
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(method, path, auth string, body any, want int) (http.Header, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
		req, _ := http.NewRequest(method, ts.URL+path, reader)
		req.Header.Set("X-Emby-Token", auth)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Real-Player-UA")
		req.Header.Set("Range", "bytes=100-200")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s: %d: %s", method, path, resp.StatusCode, raw)
		}
		if strings.Contains(string(raw), "token-secret") || strings.Contains(string(raw), "pick_code") || strings.Contains(string(raw), "fresh-code") {
			t.Fatal("private credential leaked", string(raw))
		}
		if method == "HEAD" && len(raw) > 0 {
			t.Fatal("HEAD body")
		}
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		return resp.Header, out
	}
	call("GET", "/api/admin/115/connection", "", nil, 401)
	call("PUT", "/api/admin/115/connection", token, map[string]any{"cookie": "UID=115-token-secret; CID=cid-token-secret; SEID=seid-token-secret", "revision": 0}, 204)
	conn, err := a.db.Cloud115Connection()
	if err != nil || strings.Contains(string(conn.Secrets), "token-secret") {
		t.Fatal(conn, err)
	}
	call("GET", "/api/admin/115/connection", token, nil, 200)
	call("PUT", "/api/admin/115/connection", token, map[string]any{"cookie": "", "revision": 0}, 409)
	call("PUT", "/api/admin/115/connection", token, map[string]any{"cookie": "bad", "revision": 1}, 400)
	call("PUT", "/api/admin/115/connection", token, map[string]any{"cookie": "", "revision": 1}, 204)
	l, err := a.db.CreateR18Library("R18", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/r18/libraries/" + strconv.FormatInt(l.ID, 10) + "/videos/" + fake.video.ID + "/offline"
	item := r18ItemID(l.ID, fake.video.ID)
	call("POST", base, token, map[string]any{"resource_id": "forged"}, 404)
	call("POST", base, token, map[string]any{"resource_id": "1", "magnet": "bad"}, 400)
	_, created := call("POST", base, token, map[string]any{"resource_id": "1"}, 200)
	createdTask := created["tasks"].([]any)[0].(map[string]any)
	_, createdHash, normalizeErr := cloud115.NormalizeMagnet(createdTask["magnet"].(string))
	if normalizeErr != nil || createdTask["info_hash"] != "0123456789012345678901234567890123456789" || createdHash != createdTask["info_hash"] {
		t.Fatal("missing playback version identity", createdTask)
	}
	list, err := a.db.OfflineBindings(l, fake.video.ID, conn.AccountID)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	task := list[0].Task
	call("POST", base, token, map[string]any{"resource_id": "1"}, 200)
	list, _ = a.db.OfflineBindings(l, fake.video.ID, conn.AccountID)
	if len(list) != 1 {
		t.Fatal("duplicate binding")
	}
	task.Status = "waiting_selection"
	task.ResultID = "result"
	task.Files = []cloud115.File{{ID: "file", Name: "movie.mkv", PickCode: "private-code", SHA1: "file-sha", Size: 100}}
	if err = a.db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	call("POST", base+"/selection", token, map[string]any{"task_id": task.ID, "file_id": "forged", "default": true}, 409)
	call("POST", base+"/selection", token, map[string]any{"task_id": task.ID, "file_id": "file", "default": true}, 200)
	user, err := a.db.CreateUser("cloud-reader", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	a.db.SaveUserToken("cloud-reader-token", user.ID)
	reader := a.db.ForUser(user.ID)
	reader.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{l.ID}})
	call("GET", "/api/admin/115/connection", "cloud-reader-token", nil, 403)
	call("POST", base, "cloud-reader-token", map[string]any{"resource_id": "1"}, 403)
	_, out := call("GET", base, "cloud-reader-token", nil, 200)
	if len(out["tasks"].([]any)) != 0 || len(out["sources"].([]any)) != 1 {
		t.Fatal(out)
	}
	readySource := out["sources"].([]any)[0].(map[string]any)
	if readySource["InfoHash"] != task.InfoHash || readySource["ResourceId"] != "1" {
		t.Fatal("ready version identity missing for authorized reader", readySource)
	}
	preparePath := base + "/prepare?source_id=" + readySource["Id"].(string)
	call("GET", preparePath, "", nil, 401)
	call("GET", base+"/prepare?source_id=forged", token, nil, 400)
	beforePrepare := p.urls
	_, prepared := call("GET", preparePath, "cloud-reader-token", nil, 200)
	if p.urls != beforePrepare || len(prepared["sources"].([]any)) != 1 || len(prepared["tasks"].([]any)) != 0 {
		t.Fatal("preflight must not issue signed URLs or expose administrator tasks", prepared, p)
	}
	p.infoErr = &cloud115.Error{Message: "temporary network failure", Retryable: true}
	call("GET", preparePath, token, nil, 502)
	unchanged, _ := a.db.OfflineTask(task.ID)
	if unchanged.Status != "ready" {
		t.Fatal("preflight network failure triggered redownload", unchanged)
	}
	p.infoErr = nil
	_, out = call("GET", "/Items/"+item+"/PlaybackInfo", "cloud-reader-token", nil, 200)
	source := out["MediaSources"].([]any)[0].(map[string]any)
	ticketURL := source["DirectStreamUrl"].(string)
	call("GET", "/Videos/"+item+"/stream", "", nil, 401)
	h, _ := call("GET", ticketURL, "", nil, 302)
	if h.Get("Location") != "https://cdn.example/movie?sign=private" || p.ua != "Real-Player-UA" || h.Get("Cache-Control") != "no-store" {
		t.Fatal(h, p)
	}
	call("HEAD", ticketURL, "", nil, 302)
	expiring, _ := a.db.OfflineTask(task.ID)
	expiring.ExpiresAt = time.Now().Add(-time.Second).Unix()
	if err := a.db.UpdateOfflineTask(expiring, expiring.Version); err != nil {
		t.Fatal(err)
	}
	beforeExpiry := p.urls
	call("GET", ticketURL, "", nil, 404)
	_, expiredOut := call("GET", base, token, nil, 200)
	if p.urls != beforeExpiry || len(expiredOut["sources"].([]any)) != 0 || expiredOut["tasks"].([]any)[0].(map[string]any)["status"] != "cleanup_pending" {
		t.Fatal("expired cache remained playable", expiredOut)
	}
	expiring, _ = a.db.OfflineTask(task.ID)
	expiring.ExpiresAt = 0
	if err := a.db.UpdateOfflineTask(expiring, expiring.Version); err != nil {
		t.Fatal(err)
	}
	call("POST", base+"/selection", token, map[string]any{"task_id": task.ID, "file_id": "file", "default": true}, 200)
	call("GET", strings.Replace(ticketURL, "MediaSourceId=115-", "MediaSourceId=forged-", 1), "", nil, 401)
	before := p.urls
	reader.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{})
	call("GET", ticketURL, "", nil, 404)
	if p.urls != before {
		t.Fatal("revoked ticket contacted provider")
	}
	reader.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{l.ID}})
	p.missing = true
	_, missingOut := call("GET", ticketURL, "", nil, 502)
	if !strings.Contains(missingOut["error"].(string), "自动重新下载") {
		t.Fatal("missing file did not explain automatic recovery", missingOut)
	}
	call("GET", "/Items/"+item+"/PlaybackInfo", "cloud-reader-token", nil, 200)
	current, _ := a.db.OfflineTask(task.ID)
	if current.Stage != "file" || current.Status != "discovering" || current.RecoveryFileID != "file" || len(current.Files) != 0 {
		t.Fatal(current)
	}
	bindings, _ := a.db.OfflineBindings(l, fake.video.ID, conn.AccountID)
	if len(bindings) != 1 || bindings[0].FileID != "" || bindings[0].Default {
		t.Fatal("deleted source binding was not invalidated", bindings)
	}
	current.Status = "ready"
	current.Files = task.Files
	if err := a.db.UpdateOfflineTask(current, current.Version); err != nil {
		t.Fatal(err)
	}
	call("POST", base+"/selection", token, map[string]any{"task_id": task.ID, "file_id": "file", "default": false}, 200)
	_, repairing := call("GET", preparePath, "cloud-reader-token", nil, 200)
	if len(repairing["sources"].([]any)) != 0 || len(repairing["tasks"].([]any)) != 0 {
		t.Fatal("missing preflight retained a deleted source", repairing)
	}
	recovering, _ := a.db.OfflineTask(task.ID)
	if recovering.Status != "discovering" || recovering.RecoveryFileID != "file" {
		t.Fatal("preflight did not enter automatic recovery", recovering)
	}
}
