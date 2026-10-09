---
kind: issue
title: 补齐客户端调用的筛选、用户、媒体库和预览集合接口
type: ff
status: closed
created: 2026-10-09
---

补充用户请求日志中的 8 类接口：Tags、Studios、Years、OfficialRatings、Users、Users/{uid}/Authenticate、Library/VirtualFolders、Items/{id}/ThumbnailSet；共用裸路径、/emby 前缀及全小写路由。筛选值由可见影片索引在 SQLite 聚合，支持库/合集范围、用户校验、支持的媒体类型、搜索、名称范围、排序分页，每页最多 1000，不读取源 STRM/NFO。标签/制片商 ID 与现有浏览和筛选一致，影片列表补齐 OfficialRatings 过滤及缓存隔离。

用户列表与媒体库目录返回兼容旧客户端的数组，按 ID 登录复用现有密码验证与令牌签发，支持 JSON/表单；本服务用户 ID 仍为 1，未知 ID 不映射到管理员。ThumbnailSet 校验影片/分段可见性并返回 AspectRatio=0、Thumbnails=[]，表示没有时间点预览帧，未加入抽帧或下载功能。README 明确支持条件及未覆盖全部 Emby 高级查询参数的边界。

- 改动：server.go 路由、auth.go 登录复用、compat_catalog.go/test.go、store/catalog.go、library.go 与 store.go 分级筛选，README 同步读取契约；无数据库迁移。
- 依据：[Emby SDK OpenAPI](https://github.com/MediaBrowser/Emby.SDK/blob/master/Resources/OpenApi/openapi_v3.json)、[Tags](https://dev.emby.media/reference/RestAPI/TagService/getTags.html)、[Years](https://dev.emby.media/reference/RestAPI/TagService/getYears.html)、[ThumbnailSet](https://dev.emby.media/reference/RestAPI/BifService/getItemsByIdThumbnailset.html)；Users 数组及无令牌密码登录采用 [Emby 历史 UserService](https://github.com/MediaBrowser/Emby/blob/master/MediaBrowser.Api/UserService.cs) 的兼容契约，配置库字段参考当前 VirtualFolderInfo。
- 验证：新增 HTTP 契约用例覆盖两种前缀、全小写、鉴权、错误密码/未知用户、JSON/表单登录及签发令牌可用、返回形状、未知/不可见影片、空数组、可见状态去重、媒体库/合集隔离、搜索排序分页和超大偏移、索引版本缓存失效、返回筛选值用于影片检索及分级缓存隔离。服务端和 store 全套通过；全项目回归排除既有 TestMediaProbe/TestProbeCircuitBreaker，scraper 出现一次 Windows 临时 NFO rename 拒绝访问，单独重跑 scraper 全套通过，其余模块通过。go vet、git diff --check 通过；未连接用户客户端作真机验证。
- 构建：重新生成 dist/emby-go-linux-amd64，linux/amd64 v1、CGO=0，包含 011/012 修复；SHA256=856753C32C5B24000C4C227E35CB8CC3139495B1385CB7BB66BF0CA5A04089A2。尚未在 Linux 运行或部署。
- codestable：无 spec/epic 需同步，README 已同步。未提交或推送。
