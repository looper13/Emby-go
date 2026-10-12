package actorassets

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func picture(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	img.Set(0, 0, color.White)
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestIdentityPublishAndResume(t *testing.T) {
	if Key(" Alice ") != Key("Alice") || Key("Alice") == Key("alice") || Key("Ａlice") == Key("Alice") {
		t.Fatal("identity normalization changed")
	}
	s := New(t.TempDir())
	m, b, err := Prepare("演员/../A", picture(t), Avatar{Origin: "gfriends", SourceURL: "https://example.org/image?t=2&token=secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Avatar.SHA256 != Hash(b) || m.Avatar.SourceURL != "" {
		t.Fatal(m)
	}
	p, err := s.Stage(m, b, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Resolve(m.Name); !os.IsNotExist(err) {
		t.Fatal("unpublished asset visible", err)
	}
	p2, err := New(s.Dir).Pending(m.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(p2); err != nil {
		t.Fatal(err)
	}
	path, tag, err := s.Resolve(m.Name)
	if err != nil || tag != Hash(b)[:16] {
		t.Fatal(path, tag, err)
	}
	if err := s.Publish(&p); err != nil {
		t.Fatal("idempotent publish", err)
	}
	if err := s.Complete(m.Name); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Resolve(m.Name); err == nil {
		t.Fatal("corruption accepted")
	}
}
func TestRejectUnsafeAndFutureManifest(t *testing.T) {
	s := New(t.TempDir())
	m, b, err := Prepare("A", picture(t), Avatar{Origin: "legacy_local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Stage(m, b, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(&p); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"../../secret.webp", "C:/secret.webp", "images/other.webp"} {
		m.Avatar.File = file
		if err := WriteJSON(s.Dir, filepath.Join(PersonDir("A"), "actor.json"), m); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Resolve("A"); err == nil {
			t.Fatal("unsafe path accepted", file)
		}
	}
	m.SchemaVersion = 9
	if err := WriteJSON(s.Dir, filepath.Join(PersonDir("A"), "actor.json"), m); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("A"); err == nil {
		t.Fatal("future version accepted")
	}
	if err := AtomicWrite(s.Dir, "../escape", []byte("bad")); err == nil {
		t.Fatal("escaped root")
	}
}
func TestRejectInvalidImage(t *testing.T) {
	if _, _, err := Prepare("A", []byte("not a picture"), Avatar{Origin: "gfriends"}, nil); err == nil {
		t.Fatal("accepted invalid image")
	}
	data := picture(t)
	binary.BigEndian.PutUint32(data[16:20], 100000)
	binary.BigEndian.PutUint32(data[20:24], 100000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, _, err := Prepare("A", data, Avatar{Origin: "gfriends"}, nil); err == nil {
		t.Fatal("decoded oversized pixel dimensions")
	}
}

func TestAliasProvenanceAndLegacyManifest(t *testing.T) {
	s := New(t.TempDir())
	m, data, err := Prepare("Alice", picture(t), Avatar{Origin: "gfriends", SourceURL: "https://example.org/a.jpg?t=123"}, []Alias{{Name: "Ally", Source: "gfriends/hand"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Stage(m, data, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(&p); err != nil {
		t.Fatal(err)
	}
	loaded, err := New(s.Dir).Load("Alice")
	if err != nil || len(loaded.Aliases) != 1 || loaded.Aliases[0].Source != "gfriends/hand" || loaded.Avatar.SourceVersion != "123" || loaded.Avatar.DownloadedAt == nil {
		t.Fatalf("provenance was not retained: %+v, %v", loaded, err)
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	legacy := bytes.Replace(encoded, []byte(`[{"name":"Ally","source":"gfriends/hand"}]`), []byte(`["Ally"]`), 1)
	if bytes.Equal(legacy, encoded) {
		t.Fatal("legacy fixture did not replace aliases")
	}
	if err := AtomicWrite(s.Dir, filepath.Join(PersonDir("Alice"), "actor.json"), legacy); err != nil {
		t.Fatal(err)
	}
	loaded, err = s.Load("Alice")
	if err != nil || loaded.Aliases[0].Source != "legacy_manifest" {
		t.Fatalf("legacy provenance invented: %+v, %v", loaded.Aliases, err)
	}
	if _, _, err := Prepare("Alice", picture(t), Avatar{Origin: "gfriends"}, []Alias{{Name: "Ally"}}); err == nil {
		t.Fatal("alias with no source accepted")
	}
	imported, _, err := Prepare("Alice", picture(t), Avatar{Origin: "legacy_local"}, nil)
	if err != nil || imported.Avatar.DownloadedAt != nil {
		t.Fatal("local import invented a download timestamp", err)
	}
}
