// Package gfriends reads ordered public portrait indexes without touching media or SQLite.
package gfriends

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"emby-go/internal/actorassets"
)

const DefaultBaseURL = "https://raw.githubusercontent.com/gfriends/gfriends/master/"
const DefaultIndexURL = DefaultBaseURL + "Filetree.json"
const MaxIndexBytes = 24 << 20

type Candidate struct {
	Group      string
	File       string
	Name       string
	TargetName string
	Person     string
	Priority   int
	Number     int
}
type Index struct {
	Version   string
	Timestamp json.Number
	Groups    int
	Entries   int
	lookup    map[string][]Candidate
}

var numbered = regexp.MustCompile(`^(.*)-([0-9]+)$`)

func Fold(raw string) string {
	var b strings.Builder
	for _, c := range strings.TrimSpace(raw) {
		if c == 0x3000 || unicode.IsSpace(c) {
			continue
		}
		if c >= 0xff01 && c <= 0xff5e {
			c -= 0xfee0
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}
func stem(raw string) (string, int) {
	raw, _, _ = strings.Cut(raw, "?")
	raw = strings.TrimSuffix(raw, path.Ext(raw))
	raw = strings.TrimPrefix(raw, "AI-Fix-")
	if parts := numbered.FindStringSubmatch(raw); parts != nil {
		n, _ := strconv.Atoi(parts[2])
		return parts[1], n
	}
	return raw, 0
}
func validPart(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00\r\n")
}
func ImageURL(base string, c Candidate) (string, error) {
	u, err := url.Parse(base)
	if err != nil || ValidateURL(base) != nil {
		return "", errors.New("invalid image base URL")
	}
	file, q, _ := strings.Cut(c.File, "?")
	if !validPart(c.Group) || !validPart(file) {
		return "", errors.New("invalid image path")
	}
	params, err := url.ParseQuery(q)
	if err != nil {
		return "", err
	}
	for k := range params {
		if k != "t" {
			return "", errors.New("unsupported image query")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/Content/" + c.Group + "/" + file
	u.RawPath = ""
	u.RawQuery = params.Encode()
	return u.String(), nil
}
func ValidateURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" {
		return errors.New("需要不含凭据或查询参数的 HTTP(S) 地址")
	}
	return nil
}

// Parse keeps Content member order. Maps are only used for lookups after priority is assigned.
func Parse(data []byte) (*Index, error) {
	if len(data) > MaxIndexBytes {
		return nil, errors.New("GFriends index size limit")
	}
	var top struct {
		Information struct{ Timestamp json.Number }
		Content     json.RawMessage
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	if _, err := top.Information.Timestamp.Float64(); err != nil {
		return nil, errors.New("invalid index timestamp")
	}
	i := &Index{Version: actorassets.Hash(data), Timestamp: top.Information.Timestamp, lookup: map[string][]Candidate{}}
	d := json.NewDecoder(bytes.NewReader(top.Content))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("missing Content object")
	}
	groups := map[string]bool{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		group, ok := t.(string)
		if !ok || !validPart(group) || groups[group] {
			return nil, errors.New("invalid/duplicate source group")
		}
		groups[group] = true
		t, err = d.Token()
		if err != nil || t != json.Delim('{') {
			return nil, errors.New("invalid source object")
		}
		keys := map[string]bool{}
		for d.More() {
			t, err := d.Token()
			if err != nil {
				return nil, err
			}
			key, ok := t.(string)
			if !ok || !validPart(key) || keys[key] {
				return nil, errors.New("invalid/duplicate name key")
			}
			keys[key] = true
			var file string
			if err := d.Decode(&file); err != nil {
				return nil, err
			}
			name, n := stem(key)
			person, _ := stem(file)
			c := Candidate{Group: group, File: file, Name: name, TargetName: person, Person: Fold(person), Priority: i.Groups, Number: n}
			if _, err := ImageURL(DefaultBaseURL, c); err != nil {
				return nil, fmt.Errorf("invalid source entry: %w", err)
			}
			if Fold(name) == "" || c.Person == "" {
				return nil, errors.New("empty actor name")
			}
			i.lookup[Fold(name)] = append(i.lookup[Fold(name)], c)
			i.Entries++
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		i.Groups++
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("trailing index data")
	}
	if i.Entries == 0 {
		return nil, errors.New("empty GFriends index")
	}
	return i, nil
}

// Match refuses multiple target identities even if the highest quality image is unique.
func (i *Index) Match(name string) ([]Candidate, string) {
	list := i.lookup[Fold(name)]
	if len(list) == 0 {
		return nil, "unmatched"
	}
	people := map[string]bool{}
	spellings := map[string]bool{}
	for _, c := range list {
		people[c.Person] = true
		spellings[c.Name] = true
	}
	// Different case/fullwidth keys can be different people. Exact query spelling
	// does not prove that the other normalized identity is an alias.
	if len(people) != 1 || len(spellings) != 1 {
		return nil, "ambiguous"
	}
	out := append([]Candidate(nil), list...)
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Priority != out[b].Priority {
			return out[a].Priority > out[b].Priority
		}
		return out[a].Number < out[b].Number
	})
	seen := map[string]bool{}
	unique := out[:0]
	for _, c := range out {
		k := c.Group + "/" + c.File
		if !seen[k] {
			unique = append(unique, c)
			seen[k] = true
		}
	}
	return unique, "matched"
}
