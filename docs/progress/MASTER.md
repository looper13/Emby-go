# React 前端迁移进度

## 任务

将 Emby-go `frontend/` 全站从 Vue 迁移到 React，保持 Go API 和全部用户功能，按 nowen-video 的媒体库、筛选和详情布局重点改进视觉。设计参考范围、许可边界和代码基线见[分析文档](../analysis/react-migration-overview.md)。

## 分析与计划

- [CodeStable Epic：React 前端迁移与媒体界面重制](../../codestable/epics/001-o-react-frontend-migration/spec.md)
- [项目概览](../analysis/react-migration-overview.md)
- [模块清单](../analysis/react-migration-module-inventory.md)
- [风险评估](../analysis/react-migration-risk-assessment.md)
- [任务分解](../plan/react-migration-task-breakdown.md)
- [依赖图](../plan/react-migration-dependency-graph.md)
- [里程碑](../plan/react-migration-milestones.md)
- [本项目迁移技能](../skills/react-frontend-migration/SKILL.md)

## 阶段状态

- [x] Phase 0: 准备与基线 (4/4 tasks) [details](./phase-0-preparation.md)
- [x] Phase 1: React 工程与应用骨架 (5/5 tasks) [details](./phase-1-foundation.md)
- [x] Phase 2: 媒体墙、筛选与详情 (5/5 tasks) [details](./phase-2-media-ui.md)
- [x] Phase 3: 管理和业务页面 (5/5 tasks) [details](./phase-3-feature-parity.md)
- [x] Phase 4: 测试、Go 嵌入与发布 (6/6 tasks) [details](./phase-4-validation.md)
- [x] Phase 5: 文档、Vue 退役与残余检查 (4/4 tasks) [details](./phase-5-vue-removal.md)
- [ ] Phase 6: 临时研究产物清理 (0/1 tasks) [details](./phase-6-artifact-cleanup.md)

## 当前状态

Phase 0–5 已完成。正式 `/web/ui/` 入口由 React 提供，媒体墙、筛选、影片详情、播放器、AVdb 目录与清单页面均已接入 React 路由。Vue-only 页面、组件、stores、composables、组件测试和构建依赖已退役；共享 API/DTO 与纯逻辑保留，`/admin-vue` 继续重定向到 `/admin`。Phase 4 与 Phase 5 均完成（6/6、4/4）；原 Vue 行为清单已审计并补齐 React 页面覆盖。本次迁移改动未提交或推送；AVdb 清单收藏的既有实现仍是独立推送的 `b12b260`。

当前状态：Phase 3 功能矩阵已完成 5/5；CodeStable Epic `001` 与 Issues `001`–`004` 均维持 open。Issue `001`–`003` 的执行和验证项已有完成记录，尚未执行关闭；Phase 6 / Issue `004` 的临时参考 checkout 清理被执行策略拦截。真实 AVdb 上游、生产 Emby 服务和实际播放器验收未执行。

2026-11-19 Issue `001` 续作：管理页测试覆盖媒体库冲突、账号/用户凭据、系统配置与密钥、演员筛选、刮削设置、计划任务、AVdb 连接和云盘凭据；Overview 五项行为及媒体库 302 解析开关也已验证。全局搜索通过桌面/手机、浅色/深色隔离浏览器检查。本轮修复 AVdb 本地 STRM 优先时误读网盘状态、详情未就绪时提前读网盘，以及磁力列表加载后重复读取状态；新增提供方隔离、任务离页后重入恢复测试。单片刮削候选确认/取消及其它管理页窄屏检查已完成；真实 AVdb/网盘播放仍未验收。

## 下一步

1. 用户可在隔离预览中实测 React 页面；真实 AVdb、生产 Emby 和原生播放器验收仍需对应服务环境。
2. Phase 6 临时参考 checkout 已核验；用户要求继续后尝试清理，但递归删除被执行策略拒绝，目标仍存在。不要用其他命令绕过该限制；分析、计划和进度文档继续保留在 `docs/`。
3. CodeStable Epic 与 Issues 保持 open；关闭和提交推送尚未执行。

## 验证记录

2026-11-19 Vue 基线：`typecheck` 通过；45 个文件、537 项单测通过；前端 build 和 Go embedui 测试通过；安全 Go 回归脚本通过并排除了既有 Redis DB15 清理用例。React 工具接入后，46 个文件、538 项单测通过，React/Vue 双插件 build 通过。运行时依赖审计为 0 漏洞；6 个 moderate 告警仅在开发依赖，Vue 测试工具移除后重查。当前 build 的 500 KB 以上 chunk 警告与基线一致。

2026-11-19 React R02：`typecheck` 通过；46 个文件、542 项单测通过；11 项构建脚本测试通过；`npm.cmd run build` 通过并发布 7 个 Vue 基线 UI 资源。构建中的 500 KB chunk 警告与基线一致。

2026-11-19 React R03：认证专项测试通过；全量 `typecheck` 通过；47 个文件、549 项 `test:unit` 通过。React 页面仍作为并行实现，正式页面映射与静态入口切换待 R04、R06–R21。

2026-11-19 React R04：旧 hash/query 转换器接入 React main；history entry 记录、详情回退和焦点/滚动/分页快照覆盖。`typecheck` 通过；全量 `test:unit` 连续两次均为 48 个文件、552 项通过。

2026-11-19 React R05：Zustand 状态层及页面 GET 生命周期 hooks 完成；任务 store 已纳入 React root 生命周期。`typecheck` 通过；全量 `test:unit` 通过（53 个文件、571 项）。

2026-11-19 React Phase 2：媒体类型、搜索、筛选、排序、卡片密度、海报网格、分页、详情 Hero、播放轨道控件及 AVdb 清单/收藏均已接入 React；桌面筛选 URL 到详情再返回的状态保留已由隔离浏览器确认。修复详情页手机端管理操作标签换行，窄屏显示图标并保留可访问名称。`npm.cmd run typecheck` 通过；全量 `test:unit` 为 58 个文件、586 项通过；`test:build` 11 项通过；React build 发布 47 个 `/web/ui/` 资源；Go embedui 静态路由测试和安全 Go 回归通过。Playwright 在 1440×1000 与 390×844 检查媒体墙和详情，无水平溢出或浏览器错误；截图保存于忽略目录 `output/playwright/react-migration-20261119/`。隔离环境未配置真实 AVdb 服务，仅验收缺少配置状态；300 条恢复压力场景和生产/Emby 客户端验收仍待执行。构建仍提示 HLS chunk 592.89 KB 超过 500 KB。

2026-11-19 React Phase 4 partial：登录后的媒体墙筛选→影片详情→返回流程在隔离浏览器通过，`status=success` 查询完整保留；桌面和手机页无水平溢出。`npm.cmd run test:go-build` 通过纯 Go 构建及 Windows/Linux `embedui` 交叉构建。Go UI 资源测试覆盖 Manifest、MIME、缓存、HEAD、404 与未知 API；功能矩阵迁移、300 条恢复压力及发布说明仍未完成。Phase 3 路由冒烟确认 15 个已注册页面加载预期标题且无页面级错误，逐项副作用检查未完成。

2026-11-19 React Issue 001 续作：`react-management-pages.test.tsx` 扩至 22 项，覆盖用户创建/禁用/删除、账号密码失败与重试、媒体库版本冲突草稿、配置/密钥、演员/刮削/计划任务、AVdb Revision/API Key、115/光鸭凭据与重复副本删除确认。媒体库、任务、演员和计划任务页面的首屏读取各保留一次；AVdb 编辑重新携带服务器 Revision。`npm.cmd run typecheck` 通过；全量 `npm.cmd run test:unit -- --run` 为 59 个文件、608 项通过。未运行真实 AVdb、网盘写入、生产服务或移动管理页浏览器验收；Issue 保持 open。

2026-11-19 React Issue 001 全局搜索续作：实现全局搜索弹窗、快捷键与结果导航，补充焦点循环/返回和 listbox 活动项语义；修复 portal 脱离控制台主题变量作用域的问题。全量前端单测 61 个文件、619 项通过，类型检查通过；`test:build` 11 项、`test:support` 4 项、build（48 个 UI 资源）及 Go `test:go-build` 通过。Playwright 隔离 fixture 在 1440×1000 与 390×844 检查浅/深主题和影片结果，无水平溢出、控制台 errors 或 warnings；截图在忽略目录 `output/playwright/react-global-search-20261119/`。HLS chunk 592.89 KB 构建警告仍与已记录基线一致；未操作真实 AVdb、网盘、生产服务。Issue `001` 保持 open，Overview、媒体库写操作、单片刮削确认及 AVdb/云盘播放仍待验收。

2026-11-19 React Issue 001 后续：`react-overview.test.tsx` 五项覆盖空档案、最新入库链接排序、全量重建、刷新失败保留数据和账号切换取消旧请求；媒体库 302 解析开关按当前 Revision 提交。修复 AVdb 详情有本地 STRM 时误读网盘状态、下一详情未就绪时读取旧网盘，以及资源列表更新导致重复状态 GET；云盘状态现在只在无本地来源或用户手动启用磁力流程后读取。新增 3 项 R18 页面测试，覆盖本地播放优先、115/光鸭隔离、任务离页后重入恢复和旧读取取消。全量 `test:unit` 为 62 个文件、628 项通过；`typecheck`、`test:build` 11 项、`test:support` 4 项、build（48 个 UI 资源）及 `test:go-build` 通过。build 仍提示已知 HLS chunk 592.89 KB。隔离 live-env `/actors` 在 390×844 与 1440×1000 检查均无页面横向溢出；手机端文档宽度 385px、底部内容未被固定导航遮挡，控制台 0 errors/0 warnings；截图位于忽略目录 `output/playwright/react-management-20261119/`。未连接真实 AVdb、未执行网盘写入或生产服务；Issue `001` 与 Phase 3 继续保持未完成。

2026-11-19 React Issue 001 单片刮削与管理页窄屏续作：详情入口、候选确认、取消无写入和防重复提交测试补齐；修复系统配置长标识溢出、用户管理行样式和管理操作按钮依赖详情 CSS chunk 的问题。全量前端单测 62 个文件、631 项通过；类型检查、构建脚本 11 项、隔离支持 4 项通过；前端 build 发布 49 个 UI 资源；纯 Go、缺失 UI 拒绝与 Windows/Linux amd64 `embedui` 交叉构建通过。构建保留已知 HLS chunk 592.89 KB 警告。隔离浏览器 15 路由在 390×844 与 375×812 无水平溢出或页面 alert，媒体墙加载完 304 条后末行高于底栏；用户管理 1440×1000 双栏检查通过。真实 AVdb/网盘尚未连接，未执行外部写入，Issue `001` 继续 open。

2026-11-19 React Issue 001 手动补录续作：行为测试断言完整 payload、年份数字转换、逗号分隔字段整理、失败后保留草稿、重试成功后清空，并确认拒绝 `ftp:` 媒体直链且不发写请求。管理页专项 27 项、全量单测 62 个文件/632 项和类型检查通过；测试使用 mock，未执行真实补录。Phase 3 其余页面矩阵与真实 AVdb/网盘验收仍未完成。

2026-11-20 React 查重与 Phase 4 续作：修复确认删除后自动刷新清除成功提示的问题，并覆盖预览路径、确认、预览 token 删除与结果刷新；管理页专项 28 项。HashRouter 集成回归验证 300 卡片加载后打开第 250 项并返回，焦点、滚动、URL 和条目数恢复，未重复请求页面。全量前端单测 62 个文件/634 项、typecheck、build/test:support、React build（49 资源）、Go embed 路由、安全 Go 全回归及 Windows/Linux amd64 embedui 交叉构建通过。安全回归排除 `TestRedisBehavior`（会清理本机 Redis DB15），报告位于忽略目录 `frontend/test-results/go-regression.json`。在隔离临时副本执行 `npm ci` 后，类型检查、全量单测、生产构建和 Go 嵌入检查均再次通过；Phase 4 为 4/6，剩余 Vue-only 测试替代和发行/回滚说明。工作树未提交。

2026-11-20 React 前端退役与全回归续作：移除 Vue-only SFC、入口/路由、stores、composables、Vue 组件测试和 Vue/Pinia 构建依赖；保留共享 API/DTO/纯逻辑及 `/admin-vue` 兼容重定向。隔离干净副本 `npm ci` 后，typecheck、36 个文件/235 项单测、11 项构建脚本测试、React 生产构建通过；Windows/Linux/macOS 的 amd64/arm64 五平台 `embedui` 构建通过。安全 Go 全回归最终通过全部包，排除会清理本机 Redis DB15 的 `TestRedisBehavior`，报告位于忽略目录 `frontend/test-results/go-regression.json`。此前发现的监听测试问题已复核：`TestLibraryWatchEndToEnd` 删除前补充等待扫描和 NFO 写入门闩释放；该用例与轮询移动用例各连续定向运行 3 次通过，完整安全回归也通过。HLS chunk 592.89 KB 警告仍与基线一致。Phase 4 为 5/6（剩余原 Vue 行为清单审计），Phase 5 为 4/4；Phase 6 尚未执行。真实 AVdb、生产 Emby/原生播放器验收未做；工作树未提交或推送。

2026-11-20 React AVdb 行为审计与发行复验：对照原 Vue 目录页用例，在 `react-r18-pages.test.tsx` 增加 29 项页面交互覆盖，涵盖账号/资料库切换隔离、迟到请求取消、普通网络错误保留并标记旧结果、403/刷新失权清理、旧结果分页保护、分类标签 5 项上限与 40 项分批、类型筛选和全量本地搜索、标签刷新/取消、TOP250 互斥与页数、榜单周期/顺序、搜索和隐藏封面等。修复跨库卡片串显、账号旧卡片残留、权限失败旧结果残留、分类迟到响应回填、标签一次渲染过多及年份排序选项未禁用。全量单测 36 个文件/255 项、typecheck、构建脚本 11 项、隔离支持 4 项通过；`npm.cmd run test:release` 的干净导出再次通过 `npm ci`、全量测试、React 构建和五个平台嵌入构建。HLS 592.89 KB 警告与基线相同。Phase 4 现为 6/6；Issue `002` 执行项完成但保持 open，Epic/Issue 未关闭，工作树未提交或推送。Phase 6 临时参考 checkout 清理和真实 AVdb/生产 Emby/原生播放器验收仍未执行。

2026-11-20 隔离浏览器续验：使用 `npm.cmd run dev:isolated` 启动独立后端和临时媒体数据，在 Playwright 中成功登录并打开总览及影片详情；总览显示 304 条夹具媒体。详情显示无可访问 AVdb 资料库状态（隔离环境未配置真实 AVdb）；390×844 视口下 document/body 宽度均为 390px，浏览器 0 errors/0 warnings。截图保存于忽略目录 `output/playwright/react-migration-current/detail-mobile.png`。未访问现有数据库或真实外部服务。
