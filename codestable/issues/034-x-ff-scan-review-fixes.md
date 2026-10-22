---
kind: issue
title: 并行扫描与筛选优化评审修复
type: ff
status: closed
created: 2026-10-22
---

保留最多 8 路元数据准备，结果按原扫描顺序分批入库，限制待处理结果数量，保留影片 ID 分配与共享演员头像覆盖顺序；扫描写库失败或取消时停止派发任务，并等待生产者和工作协程退出。完整汇总 NFO 读取、解析次数和字节数；所有 SQLite 连接明确使用 WAL + synchronous=FULL。

- 类型、标签、厂商筛选继续使用特征表，增加 NOCASE 比较及配套索引，保留原有 ASCII 大小写、多值任一匹配和跨字段共同限定规则；相似度仍使用原有特征规则。新增索引启动时自动补建，额外耗时和空间随特征数量增长。
- 改动：`internal/scanner/{scanner,concurrency_test,performance_test}.go`、`internal/store/{store,open_test,search_indexes,search_indexes_test,search_filters_test}.go`、README。
- 验证：并行完成顺序与共享头像、500 条任务的失败/取消协程回收、205 条 NFO 统计、8 条连接的 FULL 设置、后台与客户端筛选、旧库索引补建及查询计划回归连续 5 次通过。`go test ./... -skip='^(TestMediaProbe|TestProbeCircuitBreaker)$' -count=1 -timeout=180s` 中服务器（106.604 秒）、扫描、存储及其余包通过；Windows `TestPollingMonitorDebouncesRetriesAndTracksMoves` 一次时序失败，随后 `go test ./internal/librarywatch -count=1 -timeout=60s` 整包通过。相关 `go vet`、gofmt、`git diff --check` 通过。排除两项既有 ffprobe 异常；CGO_ENABLED=0，未运行 race。
- codestable：新增本条 ff；无相关 project spec/epic 需同步，其他事项状态不变。未部署；未验证真实 SMB/NFS 或 Emby 客户端。
