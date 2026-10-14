package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, change func(map[string]string)) (string, Manifest, []byte) {
	t.Helper()
	files := map[string]string{
		"subject.jsonlines":            `{"id":10,"type":2,"name":"Original","name_cn":"Animation","platform":1,"date":"2026-10-01","score":8.2,"infobox":"raw wiki","nsfw":true}` + "\n" + `{"id":20,"type":2,"name":"Film","platform":3,"date":""}` + "\n" + `{"id":99,"type":1,"name":"Book","platform":1}`,
		"episode.jsonlines":            `{"id":100,"subject_id":10,"name":"One","description":"episode summary","sort":1,"type":0}` + "\n" + `{"id":101,"subject_id":10,"name":"Special","sort":1.5,"type":0}` + "\n" + `{"id":999,"subject_id":99,"sort":1,"type":0}`,
		"character.jsonlines":          `{"id":7,"name":"Character","summary":"Fiction"}`,
		"person.jsonlines":             `{"id":7,"name":"Person","summary":"Human"}` + "\n" + `{"id":9,"name":"Unrelated"}`,
		"subject-characters.jsonlines": `{"subject_id":10,"character_id":7,"type":1,"order":2}`,
		"subject-persons.jsonlines":    `{"subject_id":10,"person_id":7,"position":4}`,
		"person-characters.jsonlines":  `{"subject_id":10,"person_id":7,"character_id":7,"summary":"Voice"}`,
		"subject-relations.jsonlines":  `{"subject_id":10,"related_subject_id":20,"relation_type":1,"order":1}`,
	}
	if change != nil {
		change(files)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, body := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(body + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "fixture.zip")
	if err := os.WriteFile(p, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b.Bytes())
	m := Manifest{Name: "dump-test.zip", URL: "https://github.com/bangumi/Archive/releases/download/archive/dump-test.zip", Size: int64(b.Len()), Digest: "sha256:" + hex.EncodeToString(sum[:]), CreatedAt: "2026-10-14T00:00:00Z"}
	return p, m, b.Bytes()
}

func TestCatalogImportQueryAndReopen(t *testing.T) {
	p, m, _ := fixture(t, nil)
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err = c.Search(ctx, Filter{}); !errors.Is(err, ErrNotReady) {
		t.Fatal(err)
	}
	s, err := c.Import(ctx, p, m, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Subjects != 2 || s.Episodes != 2 || s.Characters != 1 || s.Persons != 1 {
		t.Fatalf("%+v", s)
	}
	items, total, err := c.Search(ctx, Filter{Year: 2026, Season: 4, Search: "original"})
	if err != nil || total != 1 || len(items) != 1 || !items[0].NSFW {
		t.Fatal(items, total, err)
	}
	_, n, err := c.Search(ctx, Filter{Search: "%"})
	if err != nil || n != 0 {
		t.Fatal("wildcard not escaped", n, err)
	}
	r, eps, err := c.Detail(ctx, 10)
	if err != nil || r.Platform != "TV" || len(eps) != 2 || eps[0].Season != 1 || eps[1].Season != 0 || eps[0].Summary != "episode summary" {
		t.Fatal(r, eps, err)
	}
	people, err := c.People(ctx, 10)
	if err != nil || len(people) != 2 || people[0].Kind == people[1].Kind || people[0].Name == people[1].Name {
		t.Fatal(people, err)
	}
	c.Close()
	c, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Snapshot().File != s.File {
		t.Fatal("lost pointer")
	}
	r, _, err = c.Detail(ctx, 20)
	if err != nil || !r.IsMovie() || r.Year != 0 {
		t.Fatal(r, err)
	}
}

func TestCatalogFailureKeepsActiveSnapshot(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p, m, _ := fixture(t, nil)
	old, err := c.Import(context.Background(), p, m, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(map[string]string)
	}{
		{"missing", func(f map[string]string) { delete(f, "episode.jsonlines") }},
		{"empty", func(f map[string]string) { f["episode.jsonlines"] = "" }},
		{"malformed", func(f map[string]string) { f["person.jsonlines"] = "bad json" }},
		{"path", func(f map[string]string) { f["../evil"] = "bad" }},
		{"duplicate_identity", func(f map[string]string) {
			f["subject.jsonlines"] += "\n" + `{"id":10,"type":2,"name":"Duplicate","platform":1}`
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, m, _ := fixture(t, test.change)
			if _, err = c.Import(context.Background(), p, m, nil, nil); err == nil {
				t.Fatal("accepted invalid archive")
			}
			if c.Snapshot().File != old.File {
				t.Fatal("replaced good snapshot")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.Import(ctx, p, m, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	denied := errors.New("configuration changed")
	if _, err = c.Import(context.Background(), p, m, nil, func(func() error) error { return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if c.Snapshot().File != old.File {
		t.Fatal("cancel replaced active catalog")
	}
	if _, err = os.Stat(filepath.Join(c.dir, old.File)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.Detail(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestArchiveDownloadIntegrityAndURLPolicy(t *testing.T) {
	_, m, data := fixture(t, nil)
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(data)), Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header), Request: r}, nil
	})}
	var progress []Progress
	p, err := Download(context.Background(), client, t.TempDir(), m, func(p Progress) { progress = append(progress, p) })
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	if len(progress) < 2 || progress[0].Phase != "download" || progress[0].Bytes != 0 || progress[0].TotalBytes != m.Size || progress[len(progress)-1].Bytes != m.Size {
		t.Fatalf("incomplete download progress: %+v", progress)
	}
	m.Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err = Download(context.Background(), client, t.TempDir(), m, nil); err == nil {
		t.Fatal("accepted bad digest")
	}
	for _, u := range []string{"http://github.com/bangumi/Archive/releases/download/archive/dump-test.zip", "https://example.org/dump-test.zip", "https://github.com/other/repo/releases/download/archive/dump-test.zip"} {
		m.URL = u
		if err = m.Validate(); err == nil {
			t.Fatal(u)
		}
	}
	r, _ := http.NewRequest("GET", "https://127.0.0.1/file", nil)
	if err = NewHTTPClient().CheckRedirect(r, nil); err == nil {
		t.Fatal("private redirect accepted")
	}
}

func TestCatalogReportsEveryImportStage(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p, m, _ := fixture(t, nil)
	stages := map[string]bool{}
	_, err = c.Import(context.Background(), p, m, func(p Progress) { stages[p.Phase] = true }, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range append([]string{"verify", "indexes", "activate"}, requiredFiles...) {
		if !stages[stage] {
			t.Errorf("missing stage %s", stage)
		}
	}
}

func TestArchiveReplacementRetainsPreviousFile(t *testing.T) {
	c, _ := Open(t.TempDir())
	defer c.Close()
	p, m, _ := fixture(t, nil)
	old, err := c.Import(context.Background(), p, m, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, m, _ = fixture(t, func(f map[string]string) {
		f["subject.jsonlines"] = strings.ReplaceAll(f["subject.jsonlines"], "Animation", "Updated")
	})
	newSnap, err := c.Import(context.Background(), p, m, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if old.File == newSnap.File {
		t.Fatal("reused active filename")
	}
	if _, err = os.Stat(filepath.Join(c.dir, old.File)); err != nil {
		t.Fatal("old snapshot removed", err)
	}
	r, _, err := c.Detail(context.Background(), 10)
	if err != nil || r.Name != "Updated" {
		t.Fatal(r, err)
	}
}
