package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeProxyHeaders(t *testing.T) {
	headers, err := NormalizeProxyHeaders(map[string]string{" referer ": " https://source.example/ ", "origin": "https://source.example", "USER-agent": "Custom Player", "X-Source-Key": "key"})
	want := map[string]string{"Referer": "https://source.example/", "Origin": "https://source.example", "User-Agent": "Custom Player", "X-Source-Key": "key"}
	if err != nil || !reflect.DeepEqual(headers, want) {
		t.Fatalf("规范化结果不正确：%v %v", headers, err)
	}
	for _, input := range []map[string]string{
		{"Referer": "https://source.example/\r\nCookie: stolen"},
		{"Origin": "https://source.example\n"},
		{"X-Test": "value\tvalue"},
		{"Bad Header": "value"},
		{"Referer\n": "value"},
		{"": "value"},
		{"Referer": "one", "referer": "two"},
		{"Range": "bytes=0-1"},
		{"Accept-Encoding": "gzip"},
		{"Connection": "X-Key"},
		{"Host": "other.example"},
		{"Content-Length": "100"},
		{"X-Emby-Token": "account-token"},
		{"X-Long": strings.Repeat("v", maxProxyHeaderValueBytes+1)},
	} {
		if _, err := NormalizeProxyHeaders(input); err == nil {
			t.Fatalf("接受了无效或受保护的请求头：%v", input)
		}
	}
	tooMany := map[string]string{}
	for i := range maxProxyHeaderCount + 1 {
		tooMany["X-"+strings.Repeat("A", i+1)] = "value"
	}
	if _, err := NormalizeProxyHeaders(tooMany); err == nil {
		t.Fatal("接受了过多请求头")
	}
}
