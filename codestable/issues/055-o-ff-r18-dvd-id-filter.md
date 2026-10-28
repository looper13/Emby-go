---
kind: issue
title: "R18 仅展示 dvd_id 有值的影片"
type: ff
status: open
created: 2026-10-28
---

# R18 仅展示 dvd_id 有值的影片

按用户要求，在 PostgreSQL 查询层增加 `dvd_id IS NOT NULL AND TRIM(dvd_id)<>''`，排除 NULL、空字符串及仅含空格的值。实现与验证完成，用户已授权提交推送；未部署。

- 改动：`internal/r18/client.go` 的列表基础条件及直接详情读取共用过滤；番号/内容 ID/标题搜索、来源筛选及顺逆序前后游标分页均不能绕过条件。现有图片代理、尺寸和磁力 TODO 保持不变。
- 页面与数量：列表提示仅展示 dvd_id 有值的影片，数量改标“源表约”，继续使用源表统计估计，不冒充过滤后计数；不新增百万级 COUNT 查询。
- 验证：新增覆盖 8 类列表查询及直接详情的条件回归测试；R18 后端聚焦测试、前端类型检查、35 文件/353 单元测试、Vue/embedui 构建与 diff 检查通过。真实 PostgreSQL 只读验收通过：NULL/空串/空格样例过滤、顺逆序前后分页、详情、被排除条目直接读取检查以及番号关联的 3 条磁力资源；不修改源库数据或索引。
- codestable：无现有 spec；同步 `R18-CATALOG.md` 的筛选条件和源表估计数边界。
