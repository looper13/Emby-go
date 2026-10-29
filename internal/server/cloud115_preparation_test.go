package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

func TestCloud115SelectionPreparationHandoffAndInvalidation(t *testing.T) {
	for _, scenario := range []string{"single_use", "expired_proof", "tampered_nonce", "wrong_user", "changed_file", "changed_task", "changed_connection", "expired_task", "revoked_library", "missing_after_prepare"} {
		t.Run(scenario, func(t *testing.T) {
			a, server, adminToken := newProbeTestApp(t, t.TempDir())
			a.offlineService.Close()
			fake := &fakeR18{video: r18.Video{ID: r18.EncodeID("test001", "digital"), Number: "TEST-001", Images: []string{}}}
			a.r18Open = func(r18.Config) (r18Source, error) { return fake, nil }
			p := &cloudFixture{}
			a.cloud115Open = func(cloud115.Tokens, func(cloud115.Tokens) error) cloud115.Provider { return p }
			vault, err := a.cloudVault()
			if err != nil {
				t.Fatal(err)
			}
			secrets, err := vault.Seal(cloud115.Tokens{Cookie: "UID=115_fixture; CID=fixture; SEID=fixture"})
			if err != nil {
				t.Fatal(err)
			}
			if err := a.db.SaveCloud115Connection(store.Cloud115Connection{AccountID: "115-account", FolderID: "12", Secrets: secrets}, 0); err != nil {
				t.Fatal(err)
			}
			l, err := a.db.CreateR18Library("R18", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
			if err != nil {
				t.Fatal(err)
			}
			task, err := a.db.CreateOfflineTask("115-account", "12", "magnet:?xt=urn:btih:"+strings.Repeat("a", 40), strings.Repeat("a", 40), l, fake.video.ID, "1")
			if err != nil {
				t.Fatal(err)
			}
			task.Status, task.ExpiresAt = "waiting_selection", time.Now().Add(time.Hour).Unix()
			task.Files = []cloud115.File{{ID: "file", Name: "movie.mkv", PickCode: "private-code", SHA1: "file-sha", Size: 100}, {ID: "other", Name: "other.mkv", PickCode: "other-code", SHA1: "file-sha", Size: 200}}
			if err := a.db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			call := func(method, path, token string, body any) (int, map[string]any) {
				t.Helper()
				raw, _ := json.Marshal(body)
				req, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
				req.Header.Set("X-Emby-Token", token)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("User-Agent", "Preparation-Player-UA")
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				raw, _ = io.ReadAll(resp.Body)
				if strings.Contains(string(raw), "fresh-code") || strings.Contains(string(raw), "private-code") {
					t.Fatal("metadata leaked", string(raw))
				}
				out := map[string]any{}
				_ = json.Unmarshal(raw, &out)
				return resp.StatusCode, out
			}
			base := "/api/r18/libraries/" + strconv.FormatInt(l.ID, 10) + "/videos/" + fake.video.ID + "/offline"
			before := fake.reads
			status, out := call("POST", base+"/selection", adminToken, map[string]any{"task_id": task.ID, "file_id": "file", "prepare": true, "default": false})
			if status != 200 || p.infos != 1 || p.urls != 0 || fake.reads != before+1 {
				t.Fatal("selection preparation repeated queries or issued a URL", status, out, p, fake.reads)
			}
			rawURL := out["sources"].([]any)[0].(map[string]any)["DirectStreamUrl"].(string)
			u, _ := url.Parse(rawURL)
			nonce := u.Query().Get("preparation_token")
			if len(nonce) != 36 {
				t.Fatal("preparation nonce missing", rawURL)
			}
			task, _ = a.db.OfflineTask(task.ID)
			streamToken := ""
			switch scenario {
			case "expired_proof":
				data, _ := a.cloud115Prepared.Get(nonce)
				a.cloud115Prepared.Set(nonce, data, time.Nanosecond)
				time.Sleep(time.Millisecond)
			case "tampered_nonce":
				q := u.Query()
				q.Set("preparation_token", strings.Repeat("x", 36))
				u.RawQuery = q.Encode()
				rawURL = u.String()
			case "wrong_user":
				user, err := a.db.CreateUser("other-reader", "password-1234")
				if err != nil {
					t.Fatal(err)
				}
				a.db.SaveUserToken("other-reader-token", user.ID)
				a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{l.ID}})
				streamToken = "other-reader-token"
			case "changed_file":
				if err := a.db.SelectOfflineFile(l, fake.video.ID, task.ID, "other", false); err != nil {
					t.Fatal(err)
				}
			case "changed_task":
				if err := a.db.UpdateOfflineTask(task, task.Version); err != nil {
					t.Fatal(err)
				}
			case "changed_connection":
				conn, _ := a.db.Cloud115Connection()
				if err := a.db.SaveCloud115Connection(conn, conn.Revision); err != nil {
					t.Fatal(err)
				}
			case "expired_task":
				task.ExpiresAt = time.Now().Add(-time.Second).Unix()
				a.db.UpdateOfflineTask(task, task.Version)
			case "revoked_library":
				user, err := a.db.CreateUser("reader", "password-1234")
				if err != nil {
					t.Fatal(err)
				}
				a.db.SaveUserToken("reader-token", user.ID)
				reader := a.db.ForUser(user.ID)
				reader.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{l.ID}})
				status, out = call("GET", base+"/prepare?source_id=115-"+task.ID, "reader-token", nil)
				if status != 200 {
					t.Fatal(status, out)
				}
				rawURL = out["sources"].([]any)[0].(map[string]any)["DirectStreamUrl"].(string)
				reader.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{})
			case "missing_after_prepare":
				p.missing, p.urlErr = true, &cloud115.Error{Message: "missing", Missing: true}
			}
			infos := p.infos
			status, out = call("GET", rawURL, streamToken, nil)
			if scenario == "expired_task" || scenario == "revoked_library" {
				if status != 404 || p.urls != 0 || p.infos != infos {
					t.Fatal("invalid local source contacted cloud", status, out, p)
				}
				return
			}
			if scenario == "missing_after_prepare" {
				current, _ := a.db.OfflineTask(task.ID)
				if status != 502 || current.Status != "discovering" || current.RecoveryFileID != "file" || p.infos != infos+1 {
					t.Fatal("post-preflight deletion did not recover", status, out, current, p)
				}
				return
			}
			if status != 302 || p.urls != 1 || p.ua != "Preparation-Player-UA" {
				t.Fatal("stream did not request a fresh actual-UA URL", status, p)
			}
			if scenario == "single_use" {
				if p.infos != infos {
					t.Fatal("stream repeated successful preflight", p)
				}
				status, _ = call("GET", rawURL, "", nil)
				if status != 302 || p.infos != infos+1 || p.urls != 2 {
					t.Fatal("proof replay skipped a fresh check", status, p)
				}
			} else if p.infos != infos+1 {
				t.Fatal("invalid proof bypassed fresh Info", scenario, p)
			}
		})
	}
}
