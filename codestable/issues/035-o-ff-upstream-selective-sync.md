---
kind: issue
title: 上游提交审核与选择性同步
type: ff
status: open
created: 2026-10-22
---

已从 holll/Emby-go 的 98 个分叉提交中逐项筛选，以 8cdcc1b 为上游快照，适配兼容修复和运维能力。来源、取舍及完整验证记录见 [上游审核记录](../../docs/UPSTREAM-REVIEW.md)。保留本地 Vue、多用户/库授权、电视、字幕/音轨、播放代理及缓存契约。最终按用户“都帮我提交”的授权，一并交付本地前端图片显示/隐藏功能；该功能不计为上游移植。

- 改动：MetaTube/刮削、NFO、图片解码预算、会话摘要迁移、合集归属、请求预算/诊断、调度防重入、运行日志/指标/版本、备份 CLI、Go 工具链与发行元数据；README/config 示例同步。修复上游备份覆盖已有目录、503 不熔断及取消时已提交产物漏发布的问题。
- 验证：服务器全包、其余后端相关包通过；新合集/会话/取消/备份回归通过。Windows 监听时序及 NFO 文件改名分别出现偶发失败，监听整包后续一轮、NFO 整包后三轮通过，未宣称首次全量全绿。排除既有 MediaProbe/ProbeCircuitBreaker 及会清理本机 Redis DB15 的 RedisBehavior。Windows 普通/embedui 和 Linux amd64 embedui 编译、两种 go vet、格式/diff 检查通过；govulncheck 无当前可触达漏洞。CGO 关闭，未跑 race；符号链接权限场景跳过。
- 前端验证：类型检查、289 项单元测试、11 项构建测试及生产构建通过；用新 UI 产物完成 Windows embedui 编译。冻结的 legacy.css 未改动。
- codestable：无相关 spec/epic 树需同步；本条记录取舍，不改变其他事项状态。代码适配和本地验证完成，保持 open 以保留真实客户端/部署验收边界。全部当前源代码和记录纳入本次授权的 Git 交付，推送目标 origin/main，以包含本文件的 Git 提交为准；未部署。

## 2026-11-10 增量同步

用户授权检查上游后将值得采用的改动合入当前分支。本轮快照为 acfc560，比上次 8cdcc1b 新增 51 个提交；先将干净的本地 main 从 71011f8 快进至自己的 origin/main 7f1e1bb，再按本地调用方手工适配。完整提交处置清单见 [本轮审核记录](../../docs/UPSTREAM-REVIEW-2026-11-10.md)。

- 改动：合法 NULL 元数据/空 JSON 快路径、标签缓存容量与摊销清理、缓存副本锁外复制、FC2 完整数值别名、8 MiB NFO 普通文件读取、空范围探测先跳过、探测取消原因及不计失败、紧凑媒体信息缓存；补对应回归与 docs/README 边界。
- 保留：Vue、电视/多音轨、用户/库授权、Managed 缓存与磁盘版本、115/光鸭/本地优先播放、媒体库代理。上游自升级/302-only/FTS/旧 UI/新迁移及删除备份探针不整体合入。
- 验证：第一轮七包定向测试通过；扫描超限保留旧索引/修复后重试、NULL 与非法非空数据、缓存隔离/容量、取消计数及旧缓存兼容回归通过。修正现有 Go 测试的退役夹具引用后，scraper 整包通过。前端类型检查、43 文件/505 单元测试、Vue 构建、Go vet、Windows 普通/embedui 和 Linux amd64 embedui 编译通过。
- 广泛回归：后端多数包通过；NFO 出现 Windows rename Access denied，三轮复测又有一次头像改名失败，最终独立整包一轮通过。基线 7f1e1bb NFO 整包二十轮通过，未在基线复现，不能宣称根因已证明。监听聚合及 server 监听删除遇 409 的两项用例初轮失败，独立各一轮通过；监听实现未改动，不宣称全量全绿。排除会清理本机 Redis DB15 的 RedisBehavior 及已有 MediaProbe/ProbeCircuitBreaker；CGO=0，Unix 测试仅交叉编译，未跑 race。
- codestable：同主题续写本条，不改变其他事项状态；无 Project Spec/Notes 需要毕业。保持 open，真实客户端与部署未验收。本轮 Git 交付已获用户明确授权，目标为 origin/main，以包含本条记录的提交及远端一致性核验为准；临时证据和编译产物不入库，未部署。
