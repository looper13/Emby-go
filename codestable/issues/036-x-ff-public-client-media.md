---
kind: issue
title: 允许客户端匿名读取播放流与封面
type: ff
status: closed
created: 2026-10-23
---

- 播放流、代理流、预告片与 Emby 图片文件接口改为可匿名读取；有有效令牌时仍绑定当前用户并执行媒体库权限检查。播放信息、图片元信息、其他 Emby API 与管理接口保持鉴权。
- 改动：`internal/server/auth.go` 增加可选鉴权中间件；`internal/server/server.go` 对流与图片文件路由使用可选鉴权；`internal/server/account_test.go` 覆盖无令牌图片/播放以及受限账号仍不能读取未授权媒体库。
- 验证：服务端账号、契约、图片与本地图片扫描定向测试通过；`git diff --check` 通过。
- codestable：无 spec/epic 需同步。
