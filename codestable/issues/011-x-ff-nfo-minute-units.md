---
kind: issue
title: 兼容 NFO 分钟单位，避免合法时长在每轮扫描中重复失败
type: ff
status: closed
created: 2026-10-09
---

用户日志中的 `94分`、`87分` 等值无法由 XML 解码器直接写入整数字段，导致整份 NFO 解析失败；失败项不保存成功指纹，因此每次后续扫描都会重试，并非单轮扫描内无限循环。

现在单独解码 `<runtime>` 和视频 `<duration>`：兼容纯整数、分/分钟/分鐘及 min/mins/minute/minutes，允许空白及英文大小写。保留原有字段类型、时长优先级和纯整数写入格式；读取不改写源文件。未知单位、非法数值、溢出、其它字段错误及损坏 XML 仍然报错并保留旧索引。未修改扫描重试策略、指纹版本或数据库结构。

- 改动：`internal/nfo/runtime.go`、`runtime_test.go`、`internal/scanner/incremental_test.go`；README 补充兼容格式。
- 验证：修复前用 `94分` 复现相同 ParseInt 错误及扫描 Failed=1；修复后正常入库并保存 5640 秒片长，下一轮 Skipped=1。覆盖源 NFO 不被改写、纯数字输出、流信息保留、时长回退与非法值保护、修好后再次增量恢复。NFO/扫描模块全套通过；全项目 `go test ./... -p 1 -skip '^Test(MediaProbe|ProbeCircuitBreaker)$' -count=1 -timeout 180s` 通过（两项跳过的 ffprobe 测试为既有问题）；`go vet ./...` 通过。默认 Go 构建缓存权限失败后使用独立临时 GOCACHE 完成检查，未修改原缓存权限。
- 构建：已生成 `dist/emby-go-linux-amd64`，Go 构建信息确认为 linux/amd64、GOAMD64=v1、CGO_ENABLED=0；尚未在 Linux 运行。
- codestable：无 spec/epic 需同步，README 已同步当前读取契约。未提交、推送或部署；线上需更新程序后执行增量扫描确认。
