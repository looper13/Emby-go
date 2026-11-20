---
kind: issue
title: "完成 React 回归矩阵与发行验证"
type: chore
status: open
created: 2026-11-19
---

# 完成 React 回归矩阵与发行验证

## 做成以后是什么样

React 关键用户路径有与 Vue 基线相当的回归证据；长列表、详情回退、筛选、媒体播放资源释放、Go 嵌入资源和开发者发布说明均验证当前 React 入口。

**范围：** `frontend/tests/`、隔离 live-env/Playwright 流程、构建与 Go 静态资源测试、发行/回滚说明。

**归属：** `codestable/epics/001-o-react-frontend-migration/spec.md`

## 为什么现在做

本 Issue 源于 React 单测、构建、Go 静态资源回归和跨平台嵌入构建之外，仍需逐项确认原 Vue 测试行为在 React 中的等价覆盖。Vue Test Utils 和 Vue-only 测试已随前端退役；该行为审计和发行复验现已完成，执行证据见下方记录。

## 动哪些、验哪些

- 转换或替代仍有价值的 Vue 组件测试，按用户可见行为建立 React Testing Library 覆盖；纯 API/纯函数用例继续复用，不为追求比例而改写。
- 登录→媒体墙→筛选→详情→返回在桌面和 390px 手机执行；分别检查 URL、条目数、滚动和焦点。
- 使用 300 条隔离数据验证页面往返恢复、请求次数和旧响应隔离；运行播放器 HLS/音轨测试与资源销毁回归。
- 运行 npm 安装、typecheck、全量单测、构建、Go 安全回归、Go embed 资源测试和 Windows/Linux `embedui` 构建。
- 更新正式 React 的开发入口、发布/回滚说明和隔离预览脚本；不能让测试脚本读取或打印用户凭据。
- 不执行会清理本机 Redis DB15 的 `TestRedisBehavior`；使用仓库安全回归脚本。

## 关闭条件

所有被保留的行为有可运行测试或明确浏览器证据；300 条恢复结果和平台构建已记录；用户可按说明生成 React 资源并通过 `/web/ui/` 运行。真实 AVdb/生产 Emby 客户端测试不作为本 Issue 的前置条件，但须明确列为未验证环境。

## 执行记录

2026-11-20：新增浏览器历史路由下的媒体墙集成用例，实际渲染 React 媒体墙 300 张卡片，打开第 250 项再返回，断言 URL、卡片数、焦点、滚动位置均恢复且只请求原有三页。Phase 4 的 300 项恢复任务完成。全量前端单测 62 个文件、634 项通过；类型检查、build 脚本 11 项、隔离支持 4 项、React 生产构建（49 个资源）、Go embed UI 路由测试、安全 Go 回归和 Windows/Linux amd64 嵌入交叉构建通过。安全回归排除会清理本机 Redis DB15 的 `TestRedisBehavior`，报告在忽略目录 `frontend/test-results/go-regression.json`。

续作：为避免替换共享依赖目录影响仍运行的隔离预览，在临时副本执行 `npm ci` 后重新通过 typecheck、62 个文件/634 项单测、build/support 脚本、前端构建及 Go 嵌入检查。HLS chunk 592.89 KB 警告与基线相同。Vue Test Utils 等价用例替代、发行/回滚说明仍待完成。

2026-11-20 文档续作：`docs/README.md` 与 `docs/dev-plan.md` 已改为描述当前 React 控制台和 React UI 构建；保留 `/admin-vue` 到 `/admin` 的查询兼容入口。回归说明区分 React Testing Library 覆盖与仍待替换的 Vue Test Utils 对照用例；隔离预览显示 `/admin`；服务端缺少静态 UI 时改为通用 Web UI 提示。

发行续作：`npm.cmd run test:release` 对当前 tracked 和非忽略 untracked 工作树建立干净导出，`npm ci`、typecheck、63 个文件/639 项单测、构建脚本 11 项、React build（49 个资源）、五个平台 embedui 交叉构建以及固定迁移前 Windows/Linux 回滚对照均通过；`test:support` 4 项通过。结果保存在忽略目录 `frontend/test-results/release/verification.json`。Phase 3 功能矩阵现为 5/5；Issue 保持 open。验证不等于已提交检出、生产部署或真实外部服务验收。

2026-11-20 React 退役后续验：隔离干净副本执行 `npm ci` 后，typecheck、36 个文件/235 项单测、构建脚本 11 项、React 生产构建通过；Windows/Linux/macOS amd64/arm64 五个平台 `embedui` 交叉构建通过。安全 Go 全回归最终通过全部包，排除会清理本机 Redis DB15 的 `TestRedisBehavior`，摘要在忽略目录 `frontend/test-results/go-regression.json`。此前 `TestLibraryWatchEndToEnd` 因测试在监听扫描释放 NFO 写入门闩前就删除媒体库而收到预期 409；测试现等待扫描与门闩释放后再删除。该用例和 `TestPollingMonitorDebouncesRetriesAndTracksMoves` 各连续定向运行 3 次通过，完整安全回归通过。当时剩余的原 Vue 行为审计已在下方续作记录完成。实际生产 Emby、AVdb 上游和原生播放器验收不在本 Issue 的本地验证结论内。

2026-11-20 React 行为审计续作：对照原 Vue AVdb 目录页测试，在 `react-r18-pages.test.tsx` 增加 29 项交互回归，覆盖资料库/账号切换隔离、请求取消和迟到响应、网络错误旧结果提示与原返回查询、403 及刷新权限/Revision 清理、分页保护、标签组 40 项分批/本地搜索/5 项上限/类型筛选、分类刷新与切模式取消、TOP250 页数与年份/类型互斥、年份排序限制、榜单周期/顺序、搜索规范化和隐藏封面。修复跨库旧卡片带新库 ID、切换账号展示旧影片、403 保留旧结果、分类迟到响应回填、标签整组一次渲染以及发行年份仍允许最近更新排序等缺口。验证：R18 页面专项 29 项通过；全量 36 个文件/255 项单测和 typecheck 通过；构建脚本 11 项、隔离支持 4 项通过；干净导出 `npm.cmd run test:release` 通过 `npm ci`、全量测试、React build 与 Windows/Linux/macOS 五平台嵌入构建。HLS chunk 592.89 KB 警告与基线相同。Phase 4 为 6/6；Issue 执行范围完成但状态保留 open，未提交/推送，也未连接真实 AVdb、生产 Emby 或原生播放器。
