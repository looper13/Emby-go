package subtitle

import (
	"bytes"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// DecodeText 把常见文本字幕编码统一转成 UTF-8。
// 顺序：UTF-8 BOM → UTF-16 BOM → 合法 UTF-8 → GB18030。
// 全部失败时原样返回，交给客户端自行猜测，避免服务端直接 500。
func DecodeText(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if len(raw) >= 2 && (raw[0] == 0xFF && raw[1] == 0xFE || raw[0] == 0xFE && raw[1] == 0xFF) {
		if out, _, err := transform.Bytes(unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder(), raw); err == nil {
			return bytes.TrimPrefix(out, []byte{0xEF, 0xBB, 0xBF})
		}
	}
	if utf8.Valid(raw) {
		return raw
	}
	if out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw); err == nil {
		return bytes.TrimPrefix(out, []byte{0xEF, 0xBB, 0xBF})
	}
	return raw
}
