package server

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emby-go/internal/actorassets"
	"emby-go/internal/avatar"
)

func TestBatchPersonAvatarsMatchSingle(t *testing.T) {
	app := actorQueryFixture(t, 20)
	names := []string{"Actor00001", "Actor00002", "Actor00003", "Actor00004", "Unindexed", "Actor00005"}
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 4, 6))); err != nil {
		t.Fatal(err)
	}
	legacy := app.avatarsDir()
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{names[0], names[2], names[4]} {
		if err := os.WriteFile(avatar.Path(legacy, name), source.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacy, strings.ToUpper(avatar.FileName(names[5]))), source.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{names[1], names[2], names[4]} {
		manifest, data, err := actorassets.Prepare(name, source.Bytes(), actorassets.Avatar{Origin: "legacy_local"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		pending, err := app.actorAssets().Stage(manifest, data, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := app.actorAssets().Publish(&pending); err != nil {
			t.Fatal(err)
		}
		if name == names[2] {
			path, _, err := app.actorAssets().Resolve(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	want := make(map[string]actorassets.Resolved)
	for _, name := range names {
		path, tag := app.personAvatar(name)
		want[name] = actorassets.Resolved{Path: path, Tag: tag}
	}
	if want[names[0]].Tag == "" || want[names[1]].Tag == "" || want[names[2]].Path != avatar.Path(legacy, names[2]) || want[names[3]].Path != "" || want[names[4]].Path != "" {
		t.Fatal("invalid fixture", want)
	}
	app.imgMeta.Clear()
	got, err := app.personAvatars(context.Background(), names, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if got[name] != want[name] {
			t.Fatalf("%s: got %+v want %+v", name, got[name], want[name])
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.personAvatars(ctx, names, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func TestPersonsOnlyLoadsPageImages(t *testing.T) {
	app := actorQueryFixture(t, 20)
	w := actorQueryRequest(app, "/Persons?Limit=2&EnableImages=false", app.persons)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	key := "av:" + app.db.ActorVersion() + ":"
	if _, ok := app.imgMeta.Get(key + "Actor00001"); ok {
		t.Fatal("disabled images still loaded avatars")
	}
	w = actorQueryRequest(app, "/Persons?Limit=2", app.persons)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, name := range []string{"Actor00001", "Actor00002"} {
		if _, ok := app.imgMeta.Get(key + name); !ok {
			t.Fatal("page avatar was not cached", name)
		}
	}
	if _, ok := app.imgMeta.Get(key + "Actor00003"); ok {
		t.Fatal("off-page avatar was loaded")
	}
}

func TestAdminActorInventoryCancellation(t *testing.T) {
	app := actorQueryFixture(t, 20)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.adminActorInventory(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if items, err := app.adminActorInventory(context.Background(), 0); err != nil || len(items) != 20 {
		t.Fatalf("cancelled load poisoned inventory: %d %v", len(items), err)
	}
}
