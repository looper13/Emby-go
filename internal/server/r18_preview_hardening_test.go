package server

import "testing"

func TestR18PreviewURIAttributesCannotBypassBrowserWhitespaceTrimming(t *testing.T) {
	for _, line := range []string{
		"#EXT-X-KEY:METHOD=AES-128,\uFEFFURI=http://127.0.0.1/key",
		"#EXT-X-KEY:METHOD=AES-128,URI\uFEFF=http://127.0.0.1/key",
		"#EXT-X-KEY:METHOD=AES-128,\uFEFFuri='http://127.0.0.1/key'",
	} {
		if err := validateHLSURIAttributes(line); err == nil {
			t.Fatal("浏览器额外空白处理绕过了 URI 引号校验")
		}
	}
}
