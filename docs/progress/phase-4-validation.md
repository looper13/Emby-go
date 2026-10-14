# Phase 4: 验证

- [ ] P4.1 Go 定向/回归、Vue 类型检查、单测、构建。
- [x] P4.2 隔离数据库和媒体源下的桌面/移动端主路径、截图与无重叠检查。
- [x] P4.3 更新使用、来源、备份/恢复文档与未验证边界。

## Notes

定向 Go、Vue 类型检查、213 个单测、11 个构建测试、构建和桌面/手机 E2E 通过。追加 P1.5 后桌面/手机回归截图位于 frontend/test-results/archive-regression/；下载按钮和模拟进度截图位于 output/playwright/。默认 Playwright Chromium 未安装，改用已安装的 msedge 后两端通过。

P4.1 暂未标为全绿：全量 Go wrapper 报告唯一失败为既有 TestMediaProbe，NFO 缺少 <reframes>1</reframes>。不修改无关功能。真实 TMDB 鉴权测试因 tmdb_request_failed 失败，未取得鉴权结果；该显式联网测试默认跳过，不影响离线单测。

竞态检测命令未执行成功：go -race requires cgo，本机 Go 未启用 CGO。并发边界有普通单测覆盖，但不能替代 race 检测。

不连接或迁移用户运行库，不启动自动全量图片抓取，不执行部署或推送。真实 Archive 全量导入、图片规模、Linux 和实际 Emby 客户端仍为环境验收项。
