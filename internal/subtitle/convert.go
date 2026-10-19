package subtitle

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

const MaxFileSize = 16 << 20

// UTF8 decodes BOM-marked Unicode and legacy Chinese subtitle files.
func UTF8(data []byte) (string, error) {
	if len(data) > MaxFileSize {
		return "", errors.New("字幕文件超过 16 MiB")
	}
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		order := unicode.BigEndian
		if binary.LittleEndian.Uint16(data[:2]) == 0xfeff {
			order = unicode.LittleEndian
		}
		decoded, err := unicode.UTF16(order, unicode.ExpectBOM).NewDecoder().Bytes(data)
		return strings.ReplaceAll(string(decoded), "\r\n", "\n"), err
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) {
		var err error
		data, err = simplifiedchinese.GB18030.NewDecoder().Bytes(data)
		if err != nil {
			return "", fmt.Errorf("无法识别字幕编码: %w", err)
		}
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n"), nil
}

var (
	srtTime      = regexp.MustCompile(`(?m)(\d{1,3}:\d{2}:\d{2})[,\.](\d{3})`)
	assOverrides = regexp.MustCompile(`\{[^}]*\}`)
	markup       = regexp.MustCompile(`<[^>]*>`)
)

// Convert retains original text formats; VTT conversion intentionally omits ASS styling.
func Convert(data []byte, from, to string) ([]byte, error) {
	from, to = NormalizeFormat(from), NormalizeFormat(to)
	text, err := UTF8(data)
	if err != nil {
		return nil, err
	}
	if from == to {
		return []byte(text), nil
	}
	if to != "vtt" {
		return nil, errors.New("不支持此字幕格式转换")
	}
	switch from {
	case "srt":
		var out strings.Builder
		out.WriteString("WEBVTT\n\n")
		for _, block := range strings.Split(strings.TrimSpace(text), "\n\n") {
			lines := strings.Split(strings.TrimSpace(block), "\n")
			if len(lines) > 0 && !strings.Contains(lines[0], "-->") {
				lines = lines[1:]
			}
			if len(lines) < 2 || !strings.Contains(lines[0], "-->") {
				continue
			}
			out.WriteString(srtTime.ReplaceAllString(lines[0], "$1.$2"))
			out.WriteByte('\n')
			out.WriteString(html.EscapeString(markup.ReplaceAllString(strings.Join(lines[1:], "\n"), "")))
			out.WriteString("\n\n")
		}
		return []byte(out.String()), nil
	case "ass", "ssa":
		var out strings.Builder
		out.WriteString("WEBVTT\n\n")
		columns := []string{"layer", "start", "end", "style", "name", "marginl", "marginr", "marginv", "effect", "text"}
		inEvents := false
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") {
				inEvents = strings.EqualFold(line, "[Events]")
				continue
			}
			if !inEvents {
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			if strings.EqualFold(key, "Format") {
				columns = strings.Split(strings.ToLower(value), ",")
				for i := range columns {
					columns[i] = strings.TrimSpace(columns[i])
				}
				continue
			}
			if !strings.EqualFold(key, "Dialogue") {
				continue
			}
			parts := strings.SplitN(strings.TrimSpace(value), ",", len(columns))
			if len(parts) != len(columns) {
				continue
			}
			fields := map[string]string{}
			for i, column := range columns {
				fields[column] = parts[i]
			}
			start, e1 := assTime(fields["start"])
			end, e2 := assTime(fields["end"])
			if e1 != nil || e2 != nil {
				continue
			}
			body := assOverrides.ReplaceAllString(fields["text"], "")
			body = strings.NewReplacer(`\N`, "\n", `\n`, "\n", `\h`, " ").Replace(body)
			fmt.Fprintf(&out, "%s --> %s\n%s\n\n", start, end, html.EscapeString(body))
		}
		return []byte(out.String()), nil
	default:
		return nil, errors.New("此字幕不能转换为 WebVTT")
	}
}

func assTime(raw string) (string, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 3 {
		return "", errors.New("无效字幕时间")
	}
	h, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	s, e3 := strconv.ParseFloat(parts[2], 64)
	if e1 != nil || e2 != nil || e3 != nil || h < 0 || m < 0 || m > 59 || s < 0 || s >= 60 {
		return "", errors.New("无效字幕时间")
	}
	return fmt.Sprintf("%02d:%02d:%06.3f", h, m, s), nil
}
