---
kind: issue
title: "PostgreSQL R18 资料库与 article 磁力展示"
type: ff
status: closed
created: 2026-10-28
---

# PostgreSQL R18 资料库与 article 磁力展示

按用户确认的展示优先范围，新增只读 PostgreSQL 来源：网页浏览 `r18_video`，进入详情单独加载 `article` 磁力，可复制及打开来源页面。播放、下载、订阅与 Emby/Yamby 来源枚举不在本期范围，保留 TODO。实现与本地验证完成，用户确认并授权提交推送；提交记录见 Git 历史。正式实例更新/配置仍需另行进行，未部署。

- 改动：`internal/r18/`、`internal/store/r18.go`、`internal/server/r18.go`，SQLite 来源约束升级/连接持久化、既有媒体库授权、复合条目 ID 与游标分页；不修改 PostgreSQL 数据或索引。
- 页面：`frontend/src/pages/R18CatalogPage.vue`、`frontend/src/pages/R18DetailPage.vue`、`frontend/src/components/libraries/R18LibraryForm.vue`；接入媒体库/导航/返回历史，手机导航宽度与表单列宽修正。说明见 `R18-CATALOG.md`。
- 验证：R18 Go 测试、旧 SQLite 迁移/序列/授权/视图/索引/触发器/用户状态保留测试通过；目录/剧集/账号/目录查询相关回归通过。前端类型检查及 35 文件、351 单元测试通过；Vue 与 embedui 构建通过。
- 实库：只读连接、顺逆序前后分页、影片详情及 `DASS-385` 的 3 条 article 资源通过。全库约 1,908,717 为 PostgreSQL 统计估计，不是精确计数或筛选总数。
- 浏览器：隔离 Go/Redis/SQLite 实例接入真实只读来源；创建/空密码编辑/刷新持久化，40 条分页无重复、详情返回恢复滚动与卡片焦点、3 条磁力复制、无资源空态、模拟 article 503 后单独重试恢复通过。1440/390 像素与浅深色检查，无页面横向溢出；未进行下载可用性验收。
- codestable：无现有 spec 需要同步；本记录保留展示/播放分离及实际验证边界。连接密码仅存本地私有 SQLite，未加密，需保护数据库及备份。真实配置只创建在隔离预览，不修改用户现有实例。
