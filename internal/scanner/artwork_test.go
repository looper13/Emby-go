package scanner

import "testing"

// TestScanStoresTrailerAndCover 扫库把 NFO 的预告片与远程封面写进索引：
// 详情页靠它们播预告片，并在本地没有图片时用远程封面兜底。
func TestScanStoresTrailerAndCover(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "HMN-145.strm", "https://media.test/hmn145.mp4")
	writeScanFile(t, root, "HMN-145.nfo",
		`<movie><title>HMN-145 标题</title><trailer>https://cdn.test/hmn145.mp4</trailer><cover>https://cdn.test/hmn145.jpg</cover></movie>`)
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Success: 1})
	movies := visible(t, database)
	if len(movies) != 1 || movies[0].TrailerURL != "https://cdn.test/hmn145.mp4" || movies[0].CoverURL != "https://cdn.test/hmn145.jpg" {
		t.Fatalf("预告片/远程封面未入库: %+v", movies)
	}
}
