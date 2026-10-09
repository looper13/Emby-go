package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
)

// 损坏的 NFO 不能被当成「待补录」：那会用空标题覆盖已入库的元数据，
// 并写入新指纹让后续扫描直接跳过（影片就此隐没）。正确行为是保留旧索引、
// 记成失败，并且下一轮仍然重试（不写指纹），直到 NFO 被修好。
func TestIncrementalCorruptNFOPreservesIndex(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Success: 1})

	writeScanFile(t, root, "a.nfo", "<movie><title>A</title>")
	requireScan(t, database, library, Result{Failed: 1})
	requireScan(t, database, library, Result{Failed: 1})
	if movie := visible(t, database)[0]; movie.Title != "A" || movie.Status != "success" {
		t.Fatalf("corrupt NFO overwrote existing index: %+v", movie)
	}

	writeScanFile(t, root, "a.nfo", "<movie><title>Recovered</title></movie>")
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if movie := visible(t, database)[0]; movie.Title != "Recovered" {
		t.Fatalf("recovered NFO not indexed: %+v", movie)
	}
}

// 没有 NFO 与 NFO 损坏语义不同：前者是待补录（可被刮削补齐），保留 pending。
func TestIncrementalMissingNFOStaysPending(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Pending: 1})
	// 空文件不是合法 NFO：按损坏处理，不建立指纹（下一轮仍会重试）。
	writeScanFile(t, root, "a.nfo", "")
	requireScan(t, database, library, Result{Failed: 1})
	requireScan(t, database, library, Result{Failed: 1})
}

// 大目录（>64 条目）复核稳定性时，只应对目录清单里真实存在的图片取属性，
// 而不是把上百个候选文件名逐个探测一遍。修复前实测每部变化影片 114 次。
func TestLargeDirectoryImageProbeStaysBounded(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 65; index++ {
		writeScanFile(t, root, fmt.Sprintf("junk-%02d.txt", index), "unused")
	}
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Success: 1})

	var probes atomic.Int64
	statFile = func(name string) (os.FileInfo, error) {
		probes.Add(1)
		return os.Stat(name)
	}
	t.Cleanup(func() { statFile = os.Stat })

	// 源内容变化 ⇒ 该片要重新选图并复核稳定性（修复前就发生在这里）。
	writeScanFile(t, root, "a.strm", "http://media.test/a2.mp4\n")
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if got := probes.Load(); got > 2 {
		t.Fatalf("目录里没有图片，却探测了 %d 个候选文件名", got)
	}
}

// 取消必须贯通到遍历与处理循环：返回部分结果 + context.Canceled，
// 已处理的分批落库，且不执行按磁盘现状的删除阶段。
func TestScanCancelKeepsPartialIndex(t *testing.T) {
	root := t.TempDir()
	const total = 40
	for index := 0; index < total; index++ {
		name := fmt.Sprintf("movie-%02d", index)
		writeScanFile(t, root, name+".strm", "http://media.test/"+name+".mp4\n")
		writeScanFile(t, root, name+".nfo", "<movie><title>"+name+"</title></movie>")
	}
	database, library := scanLibrary(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var walkReported, processed atomic.Int64
	result, err := ScanWithProgress(ctx, database, library, func(progress Progress) {
		switch progress.Phase {
		case PhaseWalk:
			walkReported.Add(1)
		case PhaseProcess:
			if progress.Done > 0 && processed.Add(1) == 1 {
				cancel()
			}
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消未贯通: result=%+v err=%v", result, err)
	}
	if walkReported.Load() == 0 {
		t.Fatal("遍历阶段没有进度回报")
	}
	if result.Added == 0 || result.Added > total {
		t.Fatalf("取消时的部分统计不合理: %+v", result)
	}
	indexed := len(visible(t, database))
	if indexed == 0 || indexed > result.Added {
		t.Fatalf("已处理的分批没有落库: indexed=%d result=%+v", indexed, result)
	}
	// 取消后重扫：剩下的影片要补齐，已入库的条目也不能因为「删除阶段」丢索引。
	result, err = ScanWithProgress(context.Background(), database, library, nil)
	if err != nil || result.Failed != 0 || result.Added+result.Updated+result.Skipped != total {
		t.Fatalf("取消后重扫: result=%+v err=%v", result, err)
	}
	if indexed := len(visible(t, database)); indexed != total {
		t.Fatalf("取消后重扫丢条目: indexed=%d want %d", indexed, total)
	}
}

// 遍历阶段的进度回报必须按 PhaseWalk 标注（管理端据此显示「正在遍历目录」）。
func TestScanReportsWalkPhase(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title></movie>")
	database, library := scanLibrary(t, root)
	var phases []string
	var last Progress
	_, err := ScanWithProgress(context.Background(), database, library, func(progress Progress) {
		if len(phases) == 0 || phases[len(phases)-1] != progress.Phase {
			phases = append(phases, progress.Phase)
		}
		last = progress
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(phases) < 2 || phases[0] != PhaseWalk || phases[len(phases)-1] != PhaseProcess {
		t.Fatalf("阶段顺序 = %v", phases)
	}
	if last.Phase != PhaseProcess || last.Done != last.Total || last.Total != 1 {
		t.Fatalf("最终进度 = %+v", last)
	}
}
