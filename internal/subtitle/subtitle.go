// Package subtitle 负责外挂字幕文件的发现与文件名解析。
//
// 规则对齐 Emby/Kodi 的约定：字幕与媒体文件同目录、同基础名，语言与标记
// 追加在扩展名之前，用 "." 分隔。例如：
//
//	ABF-018.srt
//	ABF-018.zh-CN.ass
//	ABF-018.en.forced.srt
//	ABF-018.zh-CN.sdh.default.ass
//
// 发现仅发生在单条详情/播放请求中，不在列表或入库时执行。
// 这里只读取同目录文件名，不读取字幕内容；新增文件无需整库重扫。
package subtitle

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/text/language"
)

// File 一条可用的外挂字幕文件。
type File struct {
	Path            string // 绝对路径
	Name            string // 文件名（含扩展名）
	Codec           string // 归一化格式：srt/ass/ssa/vtt/smi/sup
	Language        string // Emby 风格语言代码；无法识别时为空
	Title           string // 文件名里剩余的自定义标题
	Default         bool   // .default
	Forced          bool   // .forced / .foreign
	HearingImpaired bool   // .sdh / .cc / .hi
}

// extensionCodec 当前支持的外挂字幕扩展名。
//
// .idx/.sub 是成对的 VobSub，单独取任何一个都无法播放；.mks 是多轨容器，
// 需要额外探测。两者留到后续版本，先不给出取不到的字幕轨。
var extensionCodec = map[string]string{
	".srt":  "srt",
	".ass":  "ass",
	".ssa":  "ssa",
	".vtt":  "vtt",
	".smi":  "smi",
	".sami": "smi",
	".sup":  "sup",
}

// iso3 把 x/text 归一化后的两位语言码映射回 Emby/NFO 常用的三位码。
// 中文单独处理：带地区的标签保留原始形式（zh-CN/zh-TW），客户端据此区分简繁。
var iso3 = map[string]string{
	"en": "eng", "zh": "chi", "ja": "jpn", "ko": "kor",
	"fr": "fre", "de": "ger", "es": "spa", "pt": "por",
	"ru": "rus", "it": "ita", "th": "tha", "vi": "vie",
	"id": "ind", "ms": "may", "ar": "ara", "hi": "hin",
	"tr": "tur", "pl": "pol", "nl": "dut", "sv": "swe",
	"da": "dan", "fi": "fin", "no": "nor", "cs": "cze",
	"hu": "hun", "ro": "rum", "uk": "ukr", "el": "gre",
	"he": "heb", "fa": "per", "bn": "ben", "ta": "tam",
	"te": "tel", "ml": "mal", "fil": "fil", "tl": "tgl",
}

var (
	defaultFlags = map[string]bool{"default": true}
	forcedFlags  = map[string]bool{"forced": true, "foreign": true}
	// hi 同时是印地语代码；字幕命名里几乎总是 hearing impaired，按标记优先。
	hiFlags = map[string]bool{"sdh": true, "cc": true, "hi": true}
)

// Discover 发现 videoPath（通常是 .strm）同目录下的外挂字幕。
// 返回顺序按文件名排序，保证同一请求内的 Index 稳定。
func Discover(videoPath string) []File {
	return discover(videoPath, ParseName)
}

var audioExtensions = map[string]string{".mka": "matroska", ".flac": "flac", ".aac": "aac", ".ac3": "ac3", ".eac3": "eac3", ".dts": "dts", ".m4a": "aac", ".mp3": "mp3", ".ogg": "vorbis", ".opus": "opus", ".wav": "pcm"}

// DiscoverAudio matches sidecar audio names without opening or probing media.
func DiscoverAudio(videoPath string) []File {
	return discover(videoPath, func(base, name string) (File, bool) { return parseName(base, name, audioExtensions) })
}

func discover(videoPath string, parse func(string, string) (File, bool)) []File {
	if strings.TrimSpace(videoPath) == "" {
		return nil
	}
	dir := filepath.Dir(videoPath)
	videoBase := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	files := make([]File, 0, 4)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		file, ok := parse(videoBase, entry.Name())
		if !ok {
			continue
		}
		file.Path = filepath.Join(dir, entry.Name())
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		left, right := strings.ToLower(files[i].Name), strings.ToLower(files[j].Name)
		if left == right {
			return files[i].Name < files[j].Name
		}
		return left < right
	})
	return files
}

// ParseName 判断 fileName 是否是 videoBase 的外挂字幕，并解析语言/标记/标题。
// 只接受与视频基础名完全相等或 "基础名.<后缀>" 的文件；"Movie2.srt" 不会
// 被误挂到 "Movie.strm"。
func ParseName(videoBase, fileName string) (File, bool) {
	return parseName(videoBase, fileName, extensionCodec)
}

func parseName(videoBase, fileName string, extensions map[string]string) (File, bool) {
	name := filepath.Base(fileName)
	ext := strings.ToLower(filepath.Ext(name))
	codec, ok := extensions[ext]
	if !ok {
		return File{}, false
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	remainder := ""
	switch {
	case strings.EqualFold(base, videoBase):
	case len(base) > len(videoBase) &&
		strings.EqualFold(base[:len(videoBase)], videoBase) &&
		base[len(videoBase)] == '.':
		remainder = base[len(videoBase)+1:]
	default:
		return File{}, false
	}

	file := File{Name: name, Codec: codec}
	if remainder == "" {
		return file, true
	}
	segments := strings.Split(remainder, ".")
	titleParts := make([]string, 0, len(segments))
	// 从右往左解析：末尾是语言/标记，剩余部分按原顺序组成标题。
	for index := len(segments) - 1; index >= 0; index-- {
		segment := strings.TrimSpace(segments[index])
		if segment == "" {
			continue
		}
		lower := strings.ToLower(segment)
		switch {
		case defaultFlags[lower]:
			file.Default = true
		case forcedFlags[lower]:
			file.Forced = true
		case hiFlags[lower]:
			file.HearingImpaired = true
		default:
			if file.Language == "" {
				if lang, ok := normalizeLanguage(segment); ok {
					file.Language = lang
					continue
				}
			}
			titleParts = append(titleParts, segment)
		}
	}
	slices.Reverse(titleParts)
	file.Title = strings.Join(titleParts, ".")
	return file, true
}

// normalizeLanguage 把文件名里的语言段归一化为 Emby 风格代码。
// 无法识别的普通单词（Commentary/2020/Extended）返回 false，交给标题处理。
func normalizeLanguage(raw string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" || value == "und" {
		return "", false
	}
	// 这两个不是合法 ISO 语言代码，但中文外挂字幕里极常见。
	switch value {
	case "chs":
		return "zh-CN", true
	case "cht":
		return "zh-TW", true
	}
	tag, err := language.Parse(raw)
	if err != nil || tag == language.Und {
		return "", false
	}
	base, confidence := tag.Base()
	if confidence == language.No {
		return "", false
	}
	normalized := tag.String()
	// 带地区/文字的标签保留完整形式：zh-CN、zh-TW、zh-Hans、pt-BR。
	if strings.ContainsRune(normalized, '-') {
		return normalized, true
	}
	if mapped, ok := iso3[base.String()]; ok {
		return mapped, true
	}
	return normalized, true
}

// NormalizeFormat 归一化字幕格式名，兼容客户端请求里的别名。
func NormalizeFormat(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "subrip", "srt":
		return "srt"
	case "webvtt", "vtt":
		return "vtt"
	case "ass":
		return "ass"
	case "ssa":
		return "ssa"
	case "smi", "sami":
		return "smi"
	case "sup", "pgs", "pgssub", "hdmv_pgs_subtitle":
		return "sup"
	case "sub", "vobsub", "dvdsub", "dvd_subtitle":
		return "sub"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// IsText 判断格式是否为文本字幕（可转码/转 UTF-8）。
func IsText(format string) bool {
	switch NormalizeFormat(format) {
	case "sup", "sub":
		return false
	default:
		return true
	}
}

// MIME 返回字幕响应的 Content-Type。
func MIME(format string) string {
	switch NormalizeFormat(format) {
	case "srt":
		return "application/x-subrip; charset=utf-8"
	case "vtt":
		return "text/vtt; charset=utf-8"
	case "ass", "ssa":
		return "text/x-ssa; charset=utf-8"
	case "sup":
		return "application/octet-stream"
	default:
		return "text/plain; charset=utf-8"
	}
}
