package metatube

import (
	"strings"
	"testing"
)

func TestNormalizeMovieID(t *testing.T) {
	cases := []struct{ in, want string }{
		{" aege-0015 ", "AEGE-015"},
		{"AEGE-015", "AEGE-015"},
		{"109IENFH-29503", "IENFH-29503"},
		{"259LUXU-0001234", "LUXU-1234"},
		{"T28-00036", "T28-036"},
		{"FC2-0001234567", "FC2-1234567"},
		{"A1B2-7", "A1B2-007"},
		{"ABF-000", "ABF-000"},
		{"ABF-0", "ABF-000"},
		{"SSIS-00123", "SSIS-123"},
		{"ABF-" + strings.Repeat("0", 30) + strings.Repeat("9", 30), "ABF-" + strings.Repeat("9", 30)},
		{" fc2-ppv-0012345 ", "FC2-PPV-0012345"},
		{" abf_0015 ", "ABF_0015"},
		{" abf 0015 ", "ABF 0015"},
		{"AEGE-0015-CD1", "AEGE-0015-CD1"},
		{"AEGE0015", "AEGE0015"},
		{"123456-00015", "123456-00015"},
		{"H4610", "H4610"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := NormalizeMovieID(tc.in); got != tc.want {
			t.Errorf("NormalizeMovieID(%q) = %q, want %q", tc.in, got, tc.want)
		} else if twice := NormalizeMovieID(got); twice != got {
			t.Errorf("NormalizeMovieID is not idempotent: %q -> %q", got, twice)
		}
	}
}

// Ported 行为的回归用例：覆盖常见文件名噪声，确保归一化后能命中真实番号。
func TestTrimNormalizesFilenames(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ABF-018", "ABF-018"},
		{"ABF-018.mp4", "ABF-018"},
		{"[FHD] ABF-018 uncensored.mp4", "ABF-018"},
		{"ABF-018-C.mp4", "ABF-018"},
		{"FC2-PPV-1234567.mp4", "FC2-1234567"},
		{"fc2ppv_1234567.mp4", "FC2-1234567"},
		{"SSIS001.mp4", "SSIS001"},
		// 归一化取的是「第一个带分隔符的编号段」，前置的 1080p 不会被单独剥离——
		// 这是上游既有行为，本项目逐条照搬（服务端搜索用同一套逻辑，结果一致）。
		{"1080p_HD_SSIS-001_4K.mkv", "1080p_SSIS-001"},
		{"Tokyo-Hot-n1234.mp4", "n1234"},
	}
	for _, tc := range cases {
		if got := Trim(tc.in); got != tc.want {
			t.Errorf("Trim(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeAndSameNumber(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{"ABF-018", "ABF-018", true},
		{"abf-018", "ABF_018", true},
		{"ABF018", "ABF-018", true},
		{"ABF-018-c", "ABF-018", false}, // 未归一化的噪声不参与比较
		{"ABF-018", "ABF-019", false},
		// provider 语义后缀（JAVHD 搜索侧的马赛克版标记）应被忽略：
		// 同一 provider 的详情接口返回的是不带后缀的 NHDTB-944。
		{"NHDTB-944-mosaic", "NHDTB-944", true},
		{"NHDTB-944-mosaic", "NHDTB-945", false},
		{"ABF018-mosaic", "ABF-018", true},
		{"AEGE-0015", "AEGE-015", true},
		{"AEGE-15", "AEGE-000015", true},
		{"AEGE0015", "AEGE-015", true},
		{"AEGE-0015-mosaic", "AEGE-015", true},
		{"AEGE0015-mosaic", "AEGE-015", true},
		{"109IENFH-29503", "IENFH-29503", true},
		{"109IENFH29503", "IENFH-29503", true},
		{"T28-00036", "T28-036", true},
		{"AEGE-0015", "AEGE-016", false},
		{"AEGE-0015", "AEGE-00150", false},
		{"AEGE-0015", "OTHER-015", false},
		{"AEGE-0015-UC", "AEGE-015", false},
		{"AEGE-0015X", "AEGE-015", false},
		{"123456-00015", "123456-015", false},
		{"FC2-PPV-0012345", "FC2-PPV-12345", false},
		{"H004610", "H-004610", true},
		{"H004610", "H4610", false},
		{"N00123", "N-00123", true},
		{"", "ABF-018", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := SameNumber(tc.a, tc.b); got != tc.same {
			t.Errorf("SameNumber(%q, %q) = %v，期望 %v", tc.a, tc.b, got, tc.same)
		}
	}
}

// 归一化 + 比较连起来用：文件名里的番号要能和接口返回的番号对上。
func TestTrimThenSameNumber(t *testing.T) {
	if !SameNumber(Trim("[FHD] ABF-018 uncensored.mp4"), "ABF-018") {
		t.Error("从文件名提取的番号应能与接口番号匹配")
	}
	if SameNumber(Trim("完全无关的文件名.mp4"), "ABF-018") {
		t.Error("无关文件名不应误判为命中")
	}
}
