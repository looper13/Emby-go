---
kind: issue
title: "R18 接入 Emby 库内只读浏览并按发行日期排序"
type: ff
status: open
created: 2026-10-28
---

# R18 接入 Emby 库内只读浏览并按发行日期排序

R18 进入 Emby/Yamby 媒体库枚举、库内列表/搜索/偏移分页、详情和图片接口；网页与兼容接口默认按 `release_date DESC NULLS LAST` 排序，Latest 按最新发行返回。实现完成，用户已授权提交推送，待客户端实机确认；未部署。

- 改动：`internal/r18/catalog.go` 提供绑定参数的目录查询/实际计数；`internal/server/r18_compat.go` 及现有媒体库/权限/详情分派接入独立复合 ID 和虚拟 Movie DTO。DVD 非空过滤、用户授权、源库只读与 SQL 超时保护保持不变；同条件计数缓存 30 秒。
- 图片与排序：沿用服务器图片代理，Primary 服务端裁为 2:3，Thumb/Backdrop 不裁；GET/HEAD、ETag/304、缩放和所有缓存前的授权检查共用。`client.go` 网页游标在同一查询解析发行日期边界，同日以内容 ID/来源稳定排序，顺逆序均将空日期放最后，旧条目 ID 游标仍可用；页面明确提供最新/最早发行排序。
- 验证：R18/图片聚焦测试、`go vet`、35 文件/354 前端单元测试、类型检查、Vue/embedui 构建和 diff 检查通过。Go 广泛回归通过 482 个顶层测试，明确排除 `TestRedisBehavior`（会清理既有 localhost Redis DB15）及既有探测测试 `TestMediaProbe`/`TestProbeCircuitBreaker`，不声称全部测试通过。
- 实库：临时本地 HTTP 实例使用真实 PostgreSQL 只读验收，验证枚举/目录计数/库内搜索/偏移分页/详情/Latest；本次 DVD 过滤计数快照为 1,526,052。实际 PostgreSQL CTE 夹具验证同日不同来源、空日期跨页、正反排序和上一页还原，不写源库数据/索引。图片尺寸、认证载体、授权撤销、混合 ID、类型过滤及本地媒体回归由自动测试覆盖；未使用原生 Yamby 做客户端验收。
- 边界与 codestable：播放保持 TODO，PlaybackInfo 返回 `NoCompatibleStream`，不把磁力当视频；跨库影片聚合/全局搜索/推荐、收藏/已看/进度/演员检索不纳入 R18。无现有 spec，已同步 `docs/R18-CATALOG.md` 和 README，不重写此前图片/筛选记录的历史边界。
