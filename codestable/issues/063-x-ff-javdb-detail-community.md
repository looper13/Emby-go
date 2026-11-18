---
kind: issue
title: 普通影片详情读取 JavDB 评论与清单
type: ff
status: closed
created: 2026-11-18
---

# 普通影片详情读取 JavDB 评论与清单

普通目录库影片详情在演员区域下方复用已配置的 JavDB 来源，按入库番号精确匹配后读取评论和相关清单。支持来源选择、独立分页和重试、长评论展开、清单外部链接及 JavDB 影片详情返回。

- 改动：`internal/r18/community.go`、`internal/server/javdb_community.go` 及 R18 路由；`frontend/src/api/javdb-community.ts`、`JavdbCommunitySection.vue` 和 `ItemDetailPage.vue`；三个聚焦回归文件及 `docs/JAVDB-COMMUNITY.md`、主说明和 R18 说明链接。
- 边界：每次请求重新核验目录库与 JavDB 库权限，并使用当前入库番号；上游只读、凭据不转发、搜索及缓存有容量限制。响应携带身份并由页面核对，切换影片、来源、离开或身份改变后不展示旧响应。资料读取不提交网盘任务或上游写操作。
- 验证：新增 Go 客户端/HTTP 回归、完整 R18 与服务器定向回归、Go vet、Vue 类型检查、全量 524 项前端单测、11 项构建检查、Vue/embedui 构建通过。隔离浏览器的分页、独立失败重试、来源及详情返回、迟到响应、无来源/空态与 12 组浅深色布局通过；390/320 像素完整长评论无横向溢出，桌面和手机截图已检查。
- codestable：无现有 Project/Epic Spec 需要同步；本次界面结构、接口及操作说明保存于 `docs/JAVDB-COMMUNITY.md`。用户已授权提交推送，交付记录见 Git 历史；真实 AVdb 实例待用户实测，未部署。

后续清单点击流程已按用户补充改为匹配本库已有影片，见 [本库清单匹配记录](064-x-ff-javdb-list-local-matches.md)。
