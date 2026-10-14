# Bangumi 主目录与图片归档迁移

## 授权与范围

2026-10-14 用户确认：以 Bangumi Archive 为主目录，蜜柑补充发布资源；封面、角色/人物头像、每集剧照分开；图片文件不存入 SQLite。参考 nz3u/bangumi-data 后开始实施。

保留目录库与 NFO 业务边界；正常目录更新保持当前 STRM 绑定、业务主键、Emby ID 和观看状态。不实现自动下载/订阅。只导入动画与相关人物/章节。

同日用户补充：该版本尚未发布，不做旧动漫库数据兼容。TMDB 使用可配置的 v3 API Key，用户提供的初始值存放在 Git 忽略的本地 YAML；本进度文档、源码和日志不记录密钥。无配置仍可浏览和播放。

## 分析与计划

- [项目现状](../analysis/project-overview.md)
- [模块范围](../analysis/module-inventory.md)
- [风险与验证](../analysis/risk-assessment.md)
- [任务分解](../plan/task-breakdown.md)
- [依赖关系](../plan/dependency-graph.md)
- [里程碑](../plan/milestones.md)

## 进度

- [x] Phase 1: Archive 主目录 (5/5 tasks) [details](./phase-1-catalog.md)
- [x] Phase 2: 图片归档与人物 (3/3 tasks) [details](./phase-2-images.md)
- [x] Phase 3: TMDB 章节剧照 (3/3 tasks) [details](./phase-3-tmdb.md)
- [ ] Phase 4: 回归与运行验证 (2/3 tasks) [details](./phase-4-validation.md)

## Current Status

功能已接入，图片任务取消测试、使用文档和最终截图复核已完成。追加 P1.5 下载/更新入口已完成：主目录与设置页共用入口，显示下载字节/百分比及导入阶段，支持取消/重试、任务重入及无新版本提示。首次导入由用户点击启动，成功后保留每周更新计划。

定向 Go 测试、213 个前端测试、11 个构建测试与桌面/手机 E2E 通过。追加服务端验证使用合成 ZIP，浏览器下载进度使用模拟状态；未宣称真实全量下载完成。全量 Go 回归仍有既有 TestMediaProbe 的 reframes 断言失败。TMDB 实网验证连接失败，未获得鉴权结果；未验证用户 Key 的有效性。竞态检测未能运行：本机 Go 未启用 CGO。

未修改用户运行数据库，未执行真实全量 Archive 导入、全量图片下载、部署或提交。参考项目核对版本：bb747dfc3dcb9b5240549263d8e788054cc60e46；只参考设计，独立实现。

## Next Steps

真实全量导入、TMDB 网络可达性、具体 Emby 客户端和 Linux 部署保留为环境验收项。代码未提交，未清理规划资料。新隔离预览通过 frontend/scripts/preview-local.mjs 启动，临时地址与登录信息在 Git 忽略的 frontend/test-results/archive-preview.stdout.log。
