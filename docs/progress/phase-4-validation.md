# Phase 4: 测试、Go 嵌入与发布

## Tasks

- [x] 审计原 Vue Test Utils 用例中仍有价值的用户行为；验收：每项保留行为都有 React Testing Library 测试或明确的浏览器证据，缺口已补齐。
- [x] 补登录→媒体墙→筛选→详情→返回隔离浏览器流程；验收：无外网依赖，desktop/mobile 均通过。
- [x] 运行 300 条媒体墙返回恢复及大内容页面交互；验收：条目数、焦点、滚动和请求次数正确。
- [x] 更新构建产物和 `/web/ui/` 的 Go 静态资源测试；验收：manifest SHA、MIME、缓存、HEAD、404 和未知 API 回归通过。
- [x] 运行 `npm ci`、typecheck、全量单测、前端 build 和 Go 安全回归；验收：全部通过并保存摘要。
- [x] 更新 Release/回滚说明和隔离预览脚本；验收：执行脚本针对 React 产物且不读取私有凭据。

## Notes

- 不运行会清理本机 Redis DB15 的直接全量 Go 套件；使用仓库安全回归脚本。
- 构建与测试通过不等同于生产部署或真实 Emby 客户端验收。
- 2026-11-19: 登录后的筛选→详情→返回流程通过，返回 URL 为 `#/items?status=success`；桌面与 390×844 手机视口均检查媒体墙和详情，无水平溢出或浏览器 console error。`go test -tags embedui ./internal/server -run 'Test(WebUI|CachePolicyHeaders)' -count=1` 和 `npm.cmd run test:go-build` 通过，后者完成纯 Go、Windows/amd64 和 Linux/amd64 构建。其余四项迁移覆盖、压力、依赖重装和发布说明待完成。
- 2026-11-20: 新增 HashRouter 媒体墙集成回归，加载 300 项后打开第 250 项并返回，恢复 300 张卡片、原焦点、滚动位置和筛选 URL，媒体请求保持为三页。React 生产构建发布 49 个 UI 资源；Go embed UI 路由测试、安全回归（排除本机 Redis DB15 清理测试）和 Windows/Linux amd64 嵌入交叉构建通过。全量前端单测为 62 个文件、634 项；类型检查、build 脚本 11 项、隔离支持 4 项通过。仍待干净依赖安装、剩余 Vue-only 测试替代和发行/回滚说明。
- 2026-11-20 续作：在隔离临时副本执行 `npm ci` 后，重新通过 typecheck、62 个文件/634 项单测、`test:build` 11 项、`test:support` 4 项、49 资源生产构建、Go embed UI 路由测试和 Windows/Linux amd64 嵌入交叉构建。HLS chunk 592.89 KB 警告与既有基线相同。Phase 4 现为 4/6；Vue-only 测试替代及发行/回滚说明仍未完成。
- 2026-11-20 发行续作：主说明和开发计划已切换到 React 当前入口，隔离预览打印 `/admin`；`npm.cmd run test:release` 从当前工作树建立干净源码导出后通过 `npm ci`、typecheck、63 个文件/637 项单测、构建脚本 11 项、React build（49 个资源）及 Linux amd64/arm64、Windows amd64、macOS amd64/arm64 五平台嵌入构建，并生成迁移前提交 `5c457893698e74928277d013931f55a2aafb1c84` 的 Windows/Linux 回滚对照。`test:support` 4 项通过。报告在忽略目录 `frontend/test-results/release/verification.json`；它验证当前工作树导出，不等于已提交检出、实际运行或生产部署。Phase 4 为 5/6，仍待有价值的 Vue Test Utils 行为等价覆盖。
- 2026-11-20 React 退役后续验：隔离干净副本执行 `npm ci` 后，typecheck、36 个文件/235 项单测、构建脚本 11 项和 React 生产构建通过；Windows/Linux/macOS amd64/arm64 五个平台 `embedui` 交叉构建通过。安全 Go 全回归最终通过全部包，排除会清理本机 Redis DB15 的 `TestRedisBehavior`，结果在忽略目录 `frontend/test-results/go-regression.json`。此前 `TestLibraryWatchEndToEnd` 的 409 是测试在监听扫描尚持有 NFO 写入门闩时提前删除媒体库；测试现等待扫描与门闩释放后再删除。该用例及 `TestPollingMonitorDebouncesRetriesAndTracksMoves` 各连续定向运行 3 次通过，安全全回归也通过。Vue-only 代码和依赖已退役；Phase 4 仍为 5/6，唯一剩余项是审计原 Vue 行为清单并补齐有价值的 React 覆盖。构建的 HLS chunk 592.89 KB 警告与原基线一致。
- 2026-11-20 React AVdb 行为审计完成：对照 Vue 目录页用例，在 `react-r18-pages.test.tsx` 补齐账号/资料库隔离、旧结果失败状态与分页保护、403 清理、迟到请求取消、分类标签 5 项上限与分批/搜索、类型筛选、标签刷新、TOP250 与榜单页面操作等可见行为。修复切换资料库/账号时旧卡片串显、权限错误保留旧卡片、标签响应迟到回填、过多分类项一次挂载及发行年份与更新排序选项状态不一致。AVdb 页面专项 29 项通过；全量前端单测 36 个文件/255 项、typecheck、构建脚本 11 项、隔离支持 4 项通过。`npm.cmd run test:release` 从干净导出执行 `npm ci`、全量单测、React 生产构建及 Windows/Linux/macOS 五平台嵌入构建通过。HLS chunk 592.89 KB 警告与既有基线相同。Phase 4 为 6/6；Issue `002` 执行项完成但未关闭。验证未连接真实 AVdb/生产 Emby，也未证明实际原生播放器行为。
