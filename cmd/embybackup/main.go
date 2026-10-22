// embybackup：手动或由外部计划任务调用的 SQLite 快照与媒体库 NFO 备份入口。
package main

import (
	"flag"
	"fmt"
	"os"

	"emby-go/internal/backup"
)

func main() {
	db := flag.String("db", "", "SQLite 数据库路径（必填）")
	out := flag.String("out", "", "备份根目录（必填）")
	retain := flag.Int("retain", 7, "保留最近 N 份")
	nfo := flag.Bool("nfo", true, "同时备份媒体库中的 .nfo 文件")
	flag.Parse()
	result, err := backup.Run(backup.Options{DBPath: *db, OutDir: *out, Retain: *retain, NFORoot: *nfo})
	if err != nil {
		fmt.Fprintln(os.Stderr, "备份失败:", err)
		os.Exit(1)
	}
	fmt.Printf("备份完成: %s（NFO %d 个）\n", result.Dir, result.NFOCount)
	for _, name := range result.Pruned {
		fmt.Println("已清理旧备份:", name)
	}
}
