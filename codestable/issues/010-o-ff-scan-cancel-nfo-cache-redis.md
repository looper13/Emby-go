---
kind: issue
title: 修复 NFO 解析失败覆盖索引与失败缓存不失效，并收紧扫描取消、进程内缓存与 Redis 访问
type: ff
status: open
created: 2026-10-09
---

按上一轮只读复现的 6 项结论逐条修正，未引入依赖或数据库迁移。

- **NFO 解析失败不再覆盖索引**：扫描区分「NFO 文件不存在＝待补录」与「文件在但读不了或 XML 非法」。后者保留已入库的元数据与图片，记一条含路径与具体错误的告警，并且不写入本轮指纹，因此后续每轮扫描都会重试；NFO 被刮削重写修好后自动恢复为 success。修复前该分支被当成待补录：影片标题被清空、状态变 pending，还写入了新指纹，下一轮直接跳过，影片就此隐没。
- **NFO 读取失败缓存真的会过期**：进程内 NFO 流信息缓存按路径保存，命中要求「路径 + 文件 mtime tag + 显式失效版本」都没变；读取或解析失败的条目只保留 30 秒，而「NFO 正常但没有 `streamdetails`」按 mtime 长期命中，两者分开。同路径并发只读盘解析一次（等待者在读盘 panic 时退回通用轨，不会拿到空条目）。
- **进程内 NFO 缓存逐条淘汰**：容量仍为 2 万条，但同路径改写替换旧条目而不是新增，超限时按 LRU 淘汰最久未用条目，不再整表清空；实测到上限后原先只剩刚插入的一条，热点条目会全部丢失。旧的「失效后整表清空」（无调用方的 `evictNFOStreams`）已删除。
- **大目录稳定性复核不再探测不存在的图片**：大目录复核复用本轮目录清单，只对清单里真实存在的图片取新属性；修复前每部变化影片要探测 114 个候选文件名。扫描中途新增/删除图片仍由批次写入前的 `ArtworkNames` 比对兜住，指纹会被清空而不留错误的稳定性结论。
- **扫描贯通取消与阶段进度**：`Scan`/`ScanWithProgress`/`RebuildWithProgress`/`RefreshDirectory`/`RefreshFiles` 接收 `context.Context`，遍历与处理循环都响应取消；取消时先落库已处理的分批、跳过按磁盘现状的删除阶段（不完整遍历不能当删除依据），进度与任务记成「已取消」。手动扫描跟随请求 context（关闭或刷新页面即中止），计划任务与媒体库监听各自跟随自己的 context；`App.Close` 改为先取消 rootCtx 再等调度器收尾，大库扫描不再卡住停机。进度新增 `phase`：`walk` 表示正在遍历目录（`total` 为已发现候选数），`process` 表示处理中，前端显示「已发现 N 个候选」与「扫描已取消」。
- **路径失效记录可回收**：版本号按失效批次单调分配；有效版本取路径自身、祖先目录子树与基线的最大值，读取路径不登记记录。目录失效覆盖所有后代，包括尚未完成加载的路径。记录超过 5 万条时先淘汰 24 小时内未再变化的路径，仍超限则整表重置；任何回收都把基线抬到已分配版本之上，避免重新生成历史 ImageTag/ETag。回收时旧进程内缓存按新基线重建。
- **Redis 访问有预算且失败可见**：建连 2 秒、单命令（含 1 次重试）800 毫秒、`ContextTimeoutEnabled`，go-redis 默认单次读 3 秒 + 3 次重试的十几秒最坏耗时不再出现；失败一律按未命中继续（不阻断请求），累计计数并写 30 秒限流日志。管理端设置页的「Redis 在线」由写死改为带超时探活的真实状态，并显示累计失败次数。

- 改动：`internal/scanner/{scanner,directory}.go` 及扫描测试；`internal/server/{nfo_cache.go(新增),media,cache_policy,library,admin,scheduled,scrape,library_watch,server,probe}.go`、`internal/server/web/app.js`；`internal/cache/{redis,cache,managed}.go` 及缓存测试；README 同步实际行为。`RescanOne` 仍是单文件重扫，未加 context。
- 验证：新增 `TestIncrementalCorruptNFOPreservesIndex`、`TestIncrementalMissingNFOStaysPending`、`TestLargeDirectoryImageProbeStaysBounded`、`TestLargeDirectoryMidScanImageChangeIsDetected`（临时停用批次级目录清单复核确认它会失败，证明「扫描中途新增图片」的检出链路真的被守护）、`TestScanCancelKeepsPartialIndex`、`TestScanReportsWalkPhase`、`TestNFOReadFailureCacheExpires`、`TestNFOCacheReplacesPerPathAndEvictsLRU`、`TestDiskVersionPruneNeverServesStaleTag`、`TestRedisBudgetAndFailureVisibility`。三项修复先做了带回退的复现：损坏 NFO 在修复前得到 `pending:1` 并覆盖索引；大目录复核在修复前恰好探测 **114** 个候选名；一天前的失败缓存在修复前仍返回通用流信息。`BenchmarkLibraryScan/flat`（300 部同目录）：rebuild 740→151 ms/op、initial 774→608 ms/op，unchanged 未受影响；未改动布局（folders）无回归。另起临时实例（临时库 + 3003 个夹具 + Redis DB 15 + 18099 端口）做端到端验证：首轮扫描 `success:3 pending:3000 added:3003`；把一部影片的 NFO 改成非法 XML 后重扫得到 `failed:1 skipped:3002 deleted:0`，该影片仍是 `Status=success` 且标题保留；修好 NFO 后重扫 `updated:1` 并索引到新标题；扫描中途断开请求（curl 超时关闭连接）后进度显示 `cancelled=true`、`error=扫描已取消`，日志记下部分计数；`/api/admin/scan/progress` 带 `phase`，`/api/admin/settings` 的 `redis_online` 为探活结果（true，failures=0）。`go vet ./...`、`node --check web/app.js` 通过；全项目测试除既有失败外通过。
- 验证限制：`TestMediaProbe` 的 `<reframes>1</reframes>` 是既有失败，已在 HEAD 工作树复现确认与本次改动无关；`TestProbeCircuitBreaker` 在整轮运行时偶发 60 秒超时、单独运行 2.9 秒通过，`internal/librarywatch` 与 `internal/nfo` 的 Windows 重命名用例同样偶发失败、单独重跑通过（与 003 记录一致）。`App.Close` 先取消 rootCtx 再等调度器收尾这一停机顺序只做了代码核对，Windows 下没有可用的优雅停机信号（taskkill 不走信号）而未做实机验证。未运行 race（本机 CGO_ENABLED=0）；未在真实大媒体库、SMB/NFS 或真实 Redis 故障下计时。
- codestable：无 spec/epic 需同步（仓库只有 issues 树）；本条为快改记录，README 已同步扫描取消、NFO 缓存与 Redis 预算的实际行为。未提交或推送 Git。

## 2026-10-09 评审修复

- 修复评审确认的 4 项问题：目录失效对已读路径和正在加载的后代路径都推进版本，且保留同名前缀的兄弟目录缓存；版本记录回收后基线严格大于所有历史分配版本，避免保留 mtime 的图片重新生成旧 ETag；NFO 并发读取按路径、tag、版本隔离，显式失效前的读取不能写回当前版本缓存；扫描取消保留 context 取消身份，任务历史、计划任务持久化结果、日志与前端统一显示 `cancelled` /「已取消」。
- 改动：`internal/server/{cache_policy,server,nfo_cache,media,admin}.go`；`internal/scheduler/scheduler.go`；前端 `app.js` / `style.css`；计划任务 DTO 注释；README 与相关回归测试。无新依赖或数据库迁移。
- 验证：新增 `TestDirectoryInvalidationChangesClientImageValidator`（实际图片 200/304 与缩略图、目录边界、在途版本快照）、`TestDiskVersionPruneNeverReusesImageTag`（按时间回收与回收后整表重置）、`TestNFOFlightRespectsDiskChanges`（显式失效与 mtime 变化时，新请求不等待尚未结束的旧读取）、`TestCancelledScanTaskStatus`（手动 scan/reindex 的 499、任务状态与扫描锁释放）、`TestCancelledScheduledScanRecordsCancellation`（经过真实 scheduler Runner 写回持久化结果）；调度器结果测试补充取消与超时。针对性测试通过，保留共享依赖每阶段仅失效一次的既有回归。
- 全项目使用独立临时 GOCACHE、`-p 1 -count=1`，排除既有 `TestMediaProbe` / `TestProbeCircuitBreaker`；服务器、扫描、缓存、调度及其他包通过，仅 Windows `TestPollingMonitorDebouncesRetriesAndTracksMoves` 一次失败、随后单独重跑通过。首次运行还遇到默认 Go 缓存权限错误及 NFO 文件重命名权限失败，独立缓存重跑后 NFO 包通过。`go vet ./...`、`node --check internal/server/web/app.js`、`git diff --check` 与 gofmt 检查通过。未运行 race（本机 CGO_ENABLED=0）。
- codestable：在本条已有快改记录内补充评审修复与验证，README 已同步；无 spec/epic 需更新。未提交或推送。
