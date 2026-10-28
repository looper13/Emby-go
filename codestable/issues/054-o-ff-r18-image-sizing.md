---
kind: issue
title: "R18 封面、剧照及媒体库窄屏尺寸对齐"
type: ff
status: open
created: 2026-10-28
---

# R18 封面、剧照及媒体库窄屏尺寸对齐

修正 R18 独立的 3:4 封面框、固定卡片宽度及横向封套完整缩入造成的留白。与现有媒体墙共用 2:3 海报、网格尺寸和小/中/大偏好；图片代理、分页、磁力及播放 TODO 不变。实现与本地验证完成，用户已授权提交推送；未部署。

- 改动：新增 `R18Cover.vue`，横向封套等比填充并展示右侧正面，竖版居中；缺图/隐藏占位固定铺满。抽取 `CardSizeSelector.vue`，R18 和 `WallFilters.vue` 共用既有 `useCardSize`；网格最小宽度不超过容器。详情封面桌面 156px、移动端 120px，剧照改为自适应 16:9；`LibrariesPage.vue` 修复 360px 屏幕卡片超出面板的问题。
- 验证：类型检查、35 文件/353 单元测试、Vue 生产构建、Go embedui 构建及 `git diff --check` 通过；R18 后端聚焦测试通过。隔离预览只读接入实库，在 1440/768/390/360px 下验证全部三档封面宽高与原媒体墙一致，卡片/表格不造成页面横向溢出。详情封面为 156x234 / 120x180，剧照保持 16:9；实际封面/两张剧照通过同源代理加载，3 条磁力保留。模拟图片错误不缩框，隐藏时 40 个封面占位且无 img；详情返回恢复 URL、1428px 滚动及原卡片焦点。
- 接口核对：`internal/server/library.go` 的 Views 与 `internal/server/compat_catalog.go` 的 VirtualFolders/MediaFolders 明确跳过 `SourceR18`；当前仅 `/api/r18/...` 网页展示，Emby/Yamby 尚不能枚举或浏览 R18 条目。本次未扩大为 Emby 适配或播放实现。
- codestable：无现有 spec；同步 `R18-CATALOG.md` 的图片尺寸与当前客户端边界。浏览器证据位于忽略目录 `output/playwright/r18/`，临时预览不修改正式服务配置或源库。
