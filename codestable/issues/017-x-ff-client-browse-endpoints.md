---
kind: issue
title: 补齐 Web 入口、媒体库分页与演员列表接口
type: ff
status: closed
created: 2026-10-10
---

用户请求日志还出现 GET /web/index.html、/Library/VirtualFolders/Query、/Persons 与 /emby/Persons。现有路由未注册这些路径，按现有客户端兼容方式补齐。

- /web/index.html 返回与 /admin 相同的嵌入控制台 HTML，text/html、no-store；外壳可直接访问，业务 API 沿用令牌鉴权，前端处理登录。
- /Library/VirtualFolders/Query 与原 VirtualFolders 共用目录 DTO，返回 Items、TotalRecordCount、StartIndex，支持偏移及数量限制，空页返回数组。原 VirtualFolders 仍返回数组。
- /Persons 复用现有可见影片演员索引及目录筛选逻辑，支持用户/媒体库/合集校验、媒体类型、搜索、名称范围、名称排序及分页；Person ID 与既有浏览、详情、图片端点一致。图片优先本地演员头像，否则回退参演影片海报，EnableImages=false 关闭图片标识。响应缓存纳入演员版本，头像写入后立即失效。当前只涵盖演员，未添加导演等人物模型或全部高级查询参数。
- 两类 API 沿用鉴权，同时提供裸路径、/emby 和全小写变体；无数据库迁移，不改变扫描语义。
- 契约依据：[VirtualFolders Query](https://dev.emby.media/reference/RestAPI/LibraryStructureService/getLibraryVirtualfoldersQuery.html)、[Persons](https://dev.emby.media/reference/RestAPI/PersonsService/getPersons.html)。README 已同步，仓库没有需同步的 spec/epic。
- 验证：服务端 Emby、WebIndexCompatibility、CachePolicyHeaders、ActorAvatarChain、Contract、CoreAPI 用例通过（4.880 秒），覆盖前缀/小写、鉴权、返回形状、空库/空页、分页及超大偏移、可见性/去重、范围过滤、图片回退/禁用、头像与演员变更缓存失效、图片请求、Web MIME/缓存及旧接口回归。go vet ./internal/server、go build ./...、git diff --check 通过。

本地实现与验证完成。未连接用户客户端作运行验证；2026-10-10 用户要求与上一轮 016 扫描计时改动一起提交并推送，未部署。
