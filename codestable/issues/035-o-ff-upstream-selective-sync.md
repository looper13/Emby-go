---
kind: issue
title: 上游提交审核与选择性同步
type: ff
status: open
created: 2026-10-22
---

已从 holll/Emby-go 的 98 个分叉提交中逐项筛选，以 8cdcc1b 为上游快照，适配兼容修复和运维能力。来源、取舍及完整验证记录见 [上游审核记录](../../UPSTREAM-REVIEW.md)。保留本地 Vue、多用户/库授权、电视、字幕/音轨、播放代理及缓存契约。最终按用户“都帮我提交”的授权，一并交付本地前端图片显示/隐藏功能；该功能不计为上游移植。

- 改动：MetaTube/刮削、NFO、图片解码预算、会话摘要迁移、合集归属、请求预算/诊断、调度防重入、运行日志/指标/版本、备份 CLI、Go 工具链与发行元数据；README/config 示例同步。修复上游备份覆盖已有目录、503 不熔断及取消时已提交产物漏发布的问题。
- 验证：服务器全包、其余后端相关包通过；新合集/会话/取消/备份回归通过。Windows 监听时序及 NFO 文件改名分别出现偶发失败，监听整包后续一轮、NFO 整包后三轮通过，未宣称首次全量全绿。排除既有 MediaProbe/ProbeCircuitBreaker 及会清理本机 Redis DB15 的 RedisBehavior。Windows 普通/embedui 和 Linux amd64 embedui 编译、两种 go vet、格式/diff 检查通过；govulncheck 无当前可触达漏洞。CGO 关闭，未跑 race；符号链接权限场景跳过。
- 前端验证：类型检查、289 项单元测试、11 项构建测试及生产构建通过；用新 UI 产物完成 Windows embedui 编译。冻结的 legacy.css 未改动。
- codestable：无相关 spec/epic 树需同步；本条记录取舍，不改变其他事项状态。代码适配和本地验证完成，保持 open 以保留真实客户端/部署验收边界。全部当前源代码和记录纳入本次授权的 Git 交付，推送目标 origin/main，以包含本文件的 Git 提交为准；未部署。
