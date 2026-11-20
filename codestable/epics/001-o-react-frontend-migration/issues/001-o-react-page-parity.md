---
kind: issue
title: "验收 React 管理与业务页面功能对等"
type: refactor
status: open
created: 2026-11-19
---

# 验收 React 管理与业务页面功能对等

## 做成以后是什么样

所有非媒体墙管理页和业务页的 React 操作与现有产品契约一致：权限边界、输入字段、确认、保存/取消、副作用、错误恢复和跨路由任务生命周期均经过针对性测试或隔离浏览器验证。

**范围：** 总览、媒体库与重复项、手动补录、配置/API Key、账号和用户、收藏/演员/搜索、任务与扫描、探测、刮削候选、计划任务、AVdb、115 和光鸭入口。只承接既有服务端 API 与操作契约。

**不包含：** 新增后端能力、真实 AVdb/网盘数据写入或部署/生产服务修改。Vue-only 前端退役由同 Epic 的 Issue `003` 跟踪。

**归属：** `codestable/epics/001-o-react-frontend-migration/spec.md`

## 为什么现在做

React 已成为 `/web/ui/` 正式入口并注册了 15 个页面。隔离 fixture 的路由冒烟检查显示预期标题且没有页面级错误，但这只证明页面能挂载。Phase 3 功能矩阵现为 5/5，管理/业务页面的关键字段、权限、副作用和失败恢复已有 React 行为测试；Vue-only 测试已随 Phase 5 退役。原 Vue 测试中的剩余行为映射由 Issue `002` 跟踪，本 Issue 仍保持 open。

React 页面复用现有 API 层，服务端继续校验权限并持有后台任务；跨页面任务状态由 React store 管理。需要用可观察结果确认：页面离开只停止可安全取消的读取，写操作和后台任务按原生命周期继续。

## 动哪些、验哪些

- 必须验：每个页面的入口和权限、关键字段/默认值、输入校验、防重复提交、确认与取消、写入成功/失败反馈、用户切换数据隔离、任务离页持续与重入恢复。
- 必须改：React 行为与旧契约不一致时，在 React 模块及其行为测试中修复；不要为迁移改变后端 API 或持久化模型。
- 需要验：隔离 live-env 的只读浏览、可取消写入前的表单验证，以及可安全模拟的确认/取消流程；危险扫描、删除、外部云盘写入仅通过 fixture 或明确拦截的模拟层验证。
- 仍未知：真实 AVdb 服务和外部云盘的当前配置/数据不可用；生产行为不作为本 Issue 的通过证据。

## 实施安排

以原 Vue 页面与现有 API 单测作为行为索引，针对每组相邻流程补 React Testing Library 测试或受控浏览器证据。用例优先断言页面可见结果、请求参数、权限和失败后的状态，而非 React 内部状态。先完成媒体库/设置/账号/用户，再完成任务/刮削/计划任务与 AVdb/云盘工作流。

主要质量目标：功能正确性与权限兼容；可靠性要求任务不因 React 路由卸载而意外停止、旧请求不覆盖新用户数据；交互能力要求移动端主要动作可发现且有可访问名称。证据分别来自 React 行为测试、网络记录与 390px/桌面浏览器检查。

## 验证

- 对应的 React 单测必须在当前实现损坏时失败，并覆盖保存成功、失败、取消/回滚、权限拒绝和重入行为。
- 运行前端全量类型检查和单测；路由页用隔离 live-env 逐项打开并检查页面级 alert 与控制台异常。
- 每个不可在 fixture 安全执行的写入，只验证表单状态/请求拦截结果，不提交真实云端操作。

## 执行记录

2026-11-19 开始：`/overview`、`/libraries`、`/items`、`/favorites`、`/manual`、`/tasks`、`/probe`、`/apikeys`、`/account`、`/users`、`/settings`、`/actors`、`/scrape`、`/scheduled` 和 `/r18` 15 个路由在隔离预览均渲染预期标题，未发现页面级 alert。媒体墙/详情单独完成筛选返回浏览器验证。其余页面行为尚未逐项验收，本 Issue 保持 open。

2026-11-19 续作：扩展 `react-management-pages.test.tsx` 到 22 项，覆盖用户凭据创建、失败重试、禁用和删除；个人账号密码校验/失败恢复/成功登出；媒体库目录副作用确认和 Revision 冲突草稿；系统配置差异保存/恢复、API Key、演员筛选与头像配置、刮削配置、计划任务验证与创建、AVdb API Key 保密与 Revision，以及 115/光鸭失败写入后的凭据草稿。查重删除测试确认先取删除预览、取消后不调用删除 API。修复媒体库、任务、演员、计划任务页面因 hook 自动读取和页面 effect 重复触发的首屏 GET；AVdb 更新恢复提交连接 Revision。验证：`npm.cmd run typecheck` 通过；`npm.cmd run test:unit -- --run` 为 59 个文件、608 项通过；`git diff --check` 无空白错误。尚未完成 Overview/全局搜索、媒体库创建/删除/代理开关、单片刮削候选检查与确认、AVdb/云盘离线播放等流程及 390px 管理页面浏览器验收，故 Issue 继续保持 open。

2026-11-19 全局搜索续作：新增 React 全局搜索弹窗和顶栏入口，支持 Ctrl/Command+K、影片/演员/媒体库结果、常用导航、方向键循环与回车跳转；弹窗使用 body portal，补上焦点约束/返回、滚动锁和 listbox 活动项语义。隔离浏览器以 fixture 搜索 ABF-307，检查 1440×1000 桌面和 390×844 手机布局、浅色与深色主题；弹窗无水平溢出，控制台 0 errors/0 warnings。证据保存在忽略目录 `output/playwright/react-global-search-20261119/`。`npm.cmd run typecheck` 通过；全量单测 61 个文件、619 项通过；`test:build` 11 项、`test:support` 4 项通过；前端 build 发布 48 个 UI 资源；`test:go-build` 通过纯 Go、缺失 UI 拒绝及 Windows/Linux `embedui` 交叉构建；`git diff --check` 无空白错误。构建仍有已知 HLS chunk 592.89 KB 警告。全局搜索验收完成；Overview、媒体库创建/删除/代理开关、单片刮削候选确认、AVdb/云盘离线播放和 390px 管理页验收仍待完成，本 Issue 保持 open。

2026-11-19 后续：`react-overview.test.tsx` 五项覆盖零数据提示、最新入库链接排序、全量重建、刷新失败保留旧数据，以及用户切换时取消前一用户的 Resume 请求；媒体库创建/删除、播放代理与上游 302 解析开关已有 React 测试。R18 播放恢复 Vue 既有边界：详情有本地 STRM 时不读取网盘状态，只有用户手动进入磁力流程才启用；无本地来源时等详情解析完成后再读取。路由切换取消旧状态读取，115 和光鸭请求保持隔离，磁力列表加载不重复读取网盘状态。新增 3 项 R18 页面行为测试：本地 STRM 播放优先、选择 115 后仅向 115 提交磁力、离开详情不取消服务端任务且重新进入并选择 115 后恢复状态。服务操作均由 mock 拦截，未执行真实 AVdb、115、光鸭或生产写入。

验证：全量 `npm.cmd run test:unit -- --run` 为 62 个文件、628 项通过；`npm.cmd run typecheck`、`test:build`（11 项）、`test:support`（4 项）、`npm.cmd run build`（发布 48 个 UI 资源）和 `npm.cmd run test:go-build`（纯 Go、缺 UI 拒绝、Windows/Linux amd64 embedui）通过。HLS chunk 592.89 KB 警告与既有基线一致。隔离 live-env `/actors` 在 390×844 与 1440×1000 浏览器检查均无页面横向溢出；手机端底部卡片不被固定导航遮挡，console 0 errors/0 warnings；截图位于忽略目录 `output/playwright/react-management-20261119/`。

2026-11-19 单片刮削与窄屏续作：新增详情页端到端组件路径，验证“刮削”使用当前媒体 ID 查候选、候选差异可见、确认只提交所选来源及候选 ID、成功后关闭弹窗；新增选中候选后取消无写入并恢复焦点、确认进行中拦截重复提交。与失败后保留候选并可重试、取消热键既有用例合计由 `react-scrape-preview-dialog.test.tsx` / `react-item-detail.test.tsx` 定向测试 10 项通过。

隔离 live-env 用 390×844 与 375×812 浏览 15 个 React 路由，均无页面级 alert 或水平溢出；等待媒体墙触底加载完 304 条夹具后确认末行高于固定底栏。检查中修复三处实际 UI 问题：系统配置实例/版本长标识导致手机页面宽度增加到 439px；用户管理 React 页面缺少用户行、选中/焦点与权限列表样式；刮削和媒体库共用操作按钮依赖详情页 CSS chunk，导致管理页显示默认按钮。用户管理另在桌面 1440×1000 检查为列表/编辑双栏。变更均沿用共享主题 token 和 Lucide 图标，无后端改动。

本轮追加的验证：全量 `npm.cmd run test:unit -- --run` 为 62 个文件、631 项通过；`npm.cmd run typecheck`、`test:build` 11 项、`test:support` 4 项、`npm.cmd run build`（49 个 UI 资源）及 `npm.cmd run test:go-build`（纯 Go、缺 UI 拒绝、Windows/Linux amd64 embedui）通过；`git diff --check` 无空白错误。隔离预览的 15 个路由在 390×844 与 375×812 完成布局检查，触底等待全部 304 条媒体墙夹具载入后末行仍在固定导航栏上方；用户管理 390px/375px 单栏、1440px 双栏均通过；干净浏览器会话在 375px 用户页无溢出且控制台 0 errors/0 warnings。真实 AVdb 与外部网盘仍未连接或写入。尚未覆盖的管理业务权限/副作用及真实外部播放保持 Issue 范围内待验，Issue 继续 open。

2026-11-19 手动补录续作：补强 React 页面行为测试，完整填写字段后断言精确 API payload（年份转数字，类型/标签/制作商按中英文逗号拆分并去空），写入失败保留草稿、重试成功显示完成提示并清空表单；另验证合法格式的 `ftp:` URL 被页面校验拒绝且不发写请求。验证：管理页专项 27 项通过；全量 `npm.cmd run test:unit -- --run` 为 62 个文件、632 项通过；`npm.cmd run typecheck` 通过。仅使用 mock，无实际补录写入；Phase 3 其余管理页矩阵及真实外部服务验收仍待完成。

2026-11-20 查重续作：新增确认删除的 React 完整路径测试，验证删除前先读取服务器预览、确认文本列出本地及远程路径、只以预览 token 删除选中副本、刷新后保留成功提示。修复自动刷新会清掉成功提示的问题。管理页专项 28 项通过；全量前端单测 62 个文件、634 项通过。删除 API 全由 mock 拦截。

2026-11-20 收藏与 AVdb 目录续作：修复收藏页在认证 token 切换后保留旧账号条目和迟到响应回填的问题；新增用例覆盖收藏标签/搜索 URL、末页删除回退，以及旧账号刷新请求不能覆盖新账号数据。另为 React AVdb 目录增加 TOP250 切换清理旧条件/重置页码、组合分类条件精确传入 API 的覆盖。全量单测 63 个文件/639 项、typecheck、React build（49 个资源）和当前工作树干净导出的五平台 release 检查通过。Phase 3 功能矩阵 5/5；真实 AVdb/网盘未连接且外部写入均由 mock 拦截。Issue 保持 open，未执行关闭。

2026-11-20 React 退役与行为审计后状态：Phase 3 功能矩阵仍为 5/5；当前 React 测试套件为 36 个文件、255 项。Vue-only 源码、测试和依赖已从工作树移除，原 Vue 行为清单已由 Issue `002` 完成审计并补齐 AVdb 目录状态、筛选和失败恢复覆盖。本 Issue 保持 open；未连接真实 AVdb/网盘，也未对生产 Emby 或原生播放器作验收。

2026-11-20 隔离浏览器续验：`npm.cmd run dev:isolated` 的临时账号成功登录，总览与影片详情页面可用，总览显示 304 条夹具媒体。详情页显示“还没有可访问的 AVdb 资料库”状态；该环境未连接真实 AVdb。390×844 视口无横向溢出，document/body 宽度均为 390px；Playwright 检查为 0 errors、0 warnings。截图：`output/playwright/react-migration-current/detail-mobile.png`。本次只读隔离浏览，未产生服务端写入。

## 关闭时

- 回写到 Epic spec：已确认的请求生命周期、权限与用户切换不变量。
- 遗留：需真实上游才能证明的 AVdb/外部播放事项继续作为环境边界，不扩大为线上验收结论。
