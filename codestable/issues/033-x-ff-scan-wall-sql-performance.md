---
kind: issue
title: 电影批次入库与电影墙查询优化
type: ff
status: closed
created: 2026-10-22
---

扫描按批合并同名演员资料，保留最后非空头像地址及已有本地图标；演员关系不变时不重写。相似特征复用事务内语句，按最多 128 条特征合并插入；保留原有批次原子性、特征规则及未变化跳过。

- 电影墙补建全库／单库的可见条目标题、入库顺序索引；启动和有变化扫描收尾刷新规划统计。单库查询在同一 SQL 中校验指定库权限。列表总数按用户、权限、筛选和变更版本缓存在进程内 LRU（256 条／30 秒），翻页和排序共享，收藏、已看及权限改变后更新，播放心跳保留计数。
- 改动：`internal/store/{scan_write,features_write,search_cache,search_indexes,cache_version,store}.go`、`internal/scanner/scanner.go`、对应回归与基准、README。
- 验证：演员单次写入与头像覆盖顺序、300 条特征分块及删除、事务失败回滚、筛选／分页计数复用、跨用户收藏／已看／授权撤销、旧库索引升级与查询计划测试通过。`go test ./... -skip='^(TestMediaProbe|TestProbeCircuitBreaker)$' -count=1 -timeout=180s` 通过（server 108.225 秒）；相关 `go vet`、`go build ./...`、`git diff --check` 通过。两项历史 ffprobe 异常未纳入；CGO_ENABLED=0，未运行 race。
- 基准：Windows／i7-13650HX，HEAD 源码通过 Go overlay 对照，未修改 SQLite 持久化设置。100 部／批、每部 10 名共享演员和 17 个特征，5x／3 组中位数：首次入库 62.4→57.8 ms，重建 29.6→27.4 ms；分配次数约 56,705→36,147／46,729→36,466。
- 百万主表基准：单库 1,000,000 条（10% pending，900,000 条可见），每次返回 100 条，3x；全库标题冷查询 936.1→114.6 ms，单库 1076.3→134.7 ms；标题翻页 937.7→1.68／1030.9→2.11 ms；入库顺序翻页 113.7→1.72／100.5→1.59 ms。首次入库顺序查询仍受精确 COUNT 扫描影响，没有稳定提升：全库 114.6→112.2 ms，单库 99.1→127.4 ms。仅 SQL 层合成数据，不包含关联大表、NFO、图片、HTTP、浏览器或真实 SMB/NFS。
- 边界：首启需补建四个索引，耗时和额外空间随存量规模增长；冷态精确计数及大 OFFSET 的成本仍在。未部署，未验证真实媒体库，未提交或推送。
- codestable：新增本条 ff；无相关 project spec／epic 需同步，其他事项状态不变。
