---
kind: issue
title: 兼容 NFO 时分秒片长
type: ff
status: closed
created: 2026-10-26
---

`<runtime>1:21:00</runtime>` 原来按整数分钟解析，导致整份 NFO 失败。现在读取 `H:MM:SS`，并在扫描索引中保留秒级精度；视频流 `<duration>` 共用此格式。两位分钟、秒必须小于 60，其他非法内容继续报错并保留旧索引。

- 改动：`internal/nfo/runtime.go`、`nfo.go` 扩展解析与片长计算；`runtime_test.go`、`internal/scanner/incremental_test.go` 覆盖用户格式、带秒数值、非法值与失败后恢复；README 同步兼容格式。
- 验证：NFO 与扫描器整包测试、相关 `go vet`、`go build ./...`、`git diff --check` 通过。夹具中的 `GAREA-412-破解.nfo` 修正后入库为 4860 秒，下一轮扫描跳过未变化文件；未访问用户实际 NAS 文件。
- codestable：无 spec/epic 需同步；延续 `011-x-ff-nfo-minute-units.md` 的读取与失败边界。尚未部署。
