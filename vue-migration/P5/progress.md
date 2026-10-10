# P5 任务与刮削（已完成）

2026-10-10，接续 P4，完成 VUE-11/12/13。Vue 入口仍为 `/admin-vue`，正式 `/admin` 仍是旧版；未提交、未部署、未切换入口。旧 `internal/server/web/`、`web_test/` 与 `legacy.css` 继续冻结。

- [x] VUE-11：应用持有探测/刮削/头像任务；同类提交锁、互斥、取消、恢复、失败退避、结束刷新。
- [x] VUE-12：刮削配置/测试/候选/批量与头像；预览/inspect/差异/确认/取消和 CT-03。
- [x] VUE-13：计划任务 CRUD、cron 校验、编辑跨路由保留、类型参数、立即执行与结果联动。
- [x] 单元/组件、真实 embedui 桌面/手机、后端契约及安全阶段基线。Go 全包的既有失败单独记录，不作为通过项。

页面布局沿用旧版，面板顺序和 CSS 未变：刮削页 `[配置 + 两个连通测试] → [库/只补缺失/覆盖/启动/中止/头像]`，详情页 `[刮削] → 预览浮层 [候选 | 详情、字段差异、图片] [覆盖开关、取消、确认写入]`，计划页 `[新建/编辑 + cron 预览 + 动态参数] → [任务列表 + 行内动作]`。浮层沿用旧 `scrape-overlay/modal/body/foot` 类，内部滚动、手机单列、候选切换不移动底部按钮。

## 实现与状态归属

| 范围 | 行为与归属 |
|---|---|
| 应用任务 | `stores/tasks.ts` 持有扫描/重建与 `useBackgroundTask` 的两个实例（probe/scrape）。扫描 600ms、探测 1000ms、刮削 1500ms，采用“本轮结束再安排下一次”，网络失败保留最后状态并标记 `unavailable`，退避上限 8s；单一轮询所有者，页面卸载/401/注销/结束清理。 |
| 提交与互斥 | `mutationBusy` 同时覆盖扫描、重建、批量探测、批量刮削与头像；总览、媒体库、媒体墙、刮削页按钮与 `ScrapeRunPanel` 一致禁用。提交锁防止重复点击，无自动重试。 |
| 生命周期语义 | 扫描/重建 POST 由应用持有，组件卸载不取消，标签刷新仍由后端跟随 context 取消；批量探测/刮削/头像挂服务 rootCtx，标签刷新后重开页面从 progress 恢复。`restoreAll()` 在登录/重新认证时读回进度，历史已完成任务不会被当成新完成播报。 |
| 结束与失败 | 服务重启导致 progress 丢失 `finished_at` 时显示“任务状态已重置，服务可能已重启…”，绝不播报成功；`cancelled`/`aborted` 单独表述；成功/跳过/失败计数与失败样例（`failures[0]`）可见；探测成功 8s 后自动隐藏，刮削结束保留面板。 |
| 刮削配置 | `ScrapeSettingsForm` 提交 `metatube_token`/`api_key` 留空表示不改，掩码值不回填；保存失败保留草稿，保存锁在离开页面后仍不重复提交；两个连通测试各自 loading，结果含耗时或错误。 |
| 候选与批量 | `ScrapeRunPanel` 由页面 `usePageRequest` 读候选数（切库/切“只补缺失”推进代次、卸载取消）；启动参数 `{library_id, only_missing, overwrite}` 显式传布尔；中止复用任务域 cancel；头像任务与刮削同一 lifetime，`kind=scrape_avatars` 单独文案。 |
| 单条预览 | `stores/scrape-preview.ts` + `ScrapePreviewDialog`/`CandidateList`/`ScrapeDiff`。preview 后自动 inspect 推荐候选；切换候选 abort 旧读取并以代次丢弃迟到结果；inspect 与 `/scrape/test` 标记 `readOnly`，不使媒体墙快照 dirty；确认始终发送显式 `overwrite`，写入锁防重复，失败保留浮层可重试。 |
| 关闭与焦点 | 关闭只 abort 读取，短写不随页面卸载取消；确认回调核对 `route.fullPath` + 影片 ID + 组件仍挂载，离开后不刷新后来页面。Escape 按最上层浮层处理，关闭恢复触发控件、还原 body overflow；Tab 焦点留在浮层。切路由/认证失效关闭浮层。 |
| 计划任务 | `ScheduledPage` + `ScheduledForm`/`ScheduledList`。cron 由页面域读请求 `POST /scheduled/validate`（300ms 防抖、卸载取消、只保留最新回复），含预设与下次执行预览;表单按类型显隐动态参数，覆写三态（缺省取全局/false 只补缺失/true 强制覆盖）与 limit/status 空值语义保持；编辑选择放 `stores/scheduled.ts`，离开再回来仍在编辑态，取消/保存/删除/注销清空；行内启停/立即执行/删除各自加锁，删除确认文案说明不影响影片数据，立即执行后联动 `tasks.restoreAll()` 与任务页。 |

组件边界按 [P0/component-split.md](../P0/component-split.md) 落地，实际差异回写该文件。DTO 按真实后端字段解析（`*bool` 缺省、`failures` 可空、`success/failed/skipped` 非法类型报错），未新增后端写接口，未修改后端生产逻辑。

## 对照与修正

- 边界变更：probe/scrape 面板使用新增的 `BackgroundTaskProgress`，扫描/重建继续用 `TaskProgress`（P0 §1 原写“三种任务共用一个 TaskProgress”）。原因是探测/刮削状态在 `useBackgroundTask` 中，取消、退避、隐藏策略与扫描不同；已回写 P0 清单。
- 计划任务编辑态由页面本地改为 `stores/scheduled.ts` 的 `editingId`：GL-21 要求离开页面再回来仍在编辑态，页面本地 ref 无法满足。
- 媒体墙的“探测媒体信息”沿用旧版全局动作，没有库选择器；P5 浏览器用例只在测试内用 `page.route` 给该请求补 `library_id`，仅为隔离自有临时媒体，未改变产品行为。
- `scrape/test`、`inspect` 在 API 层标记 `readOnly`，写成功后仍只失效一次；探测/刮削结束通过 `tasks.revision` 让墙变 dirty，不因轮询重复刷新媒体墙。
- 工具链说明：WSL 环境不会把 `PW_CHANNEL`、`EMBY_MIGRATION_PHASE` 等变量传给 Win32 进程，浏览器与 Go 回归必须经 PowerShell 执行；首次误从 WSL 运行导致 58 项在启动浏览器阶段全失败，重跑通过，属于运行方式问题而非代码回归。

## 验证结果

| 验证 | 结果 |
|---|---|
| TypeScript 严格检查 | 通过 |
| Vitest 单元/组件 | 181/181（12 文件；P5 新增 3 文件 28 项：p5-tasks/p5-scrape/p5-scheduled） |
| 真实 embedui Playwright | 58/58（desktop 29 + mobile 29），含 P5 8 场景 × 2 视口 |
| Vite/Go 发布资源 | 通过；5 个 UI 文件，主 JS 231.48 kB（gzip 82.14），Artplayer 动态 chunk 133.43 kB（gzip 36.27），CSS 29.12 kB（gzip 6.69） |
| Node 构建脚本 | 11/11 |
| Go 契约 | CT-01/02 各 19 例沿用通过；新增 `TestVueMigrationOverwriteContract`（8 子例）与 `TestVueMigrationPreviewAndRootCancellation` 通过 |
| Go 静态资源/缓存 | 默认与 embedui 定向测试通过 |
| Go vet | 默认与 embedui 均通过 |
| 干净源码与平台构建 | 默认不依赖 frontend/web_dist；缺资源 embedui 正确失败；Windows/Linux amd64 交叉构建通过（未在 Linux 运行） |
| 安全 Go 全包 | 14 包通过、1 跳过、1 失败：`TestMediaProbe`（既有 ffprobe `<reframes>` 差异）。`TestProbeCircuitBreaker` 曾在带负载首轮 60s 超时，单跑 2.878s 通过、复跑全量也通过，记为负载敏感性而非回归 |
| 冻结基线 | `web/`、`web_test/` 在 P5 无写入（mtime 皆早于迁移开始）；`legacy.css` 与 `web/style.css` SHA256 同为 `2F8FB7F756C0BC96BBB856E4CB76CF19E06CDDA762F4F48F5CD5D597F0A73DF3` |

真实浏览器只操作拥有的临时 SQLite、独立 Redis、本地媒体源和本地刮削桩服务。P5 场景涵盖：配置失败后重试、掩码密钥留空、两处连通测试、候选数随筛选变化；预览取消与切页不写盘、候选切换不跳动、焦点圈定与 Escape 恢复；CT-03 真实确认（显式 false 保留、true 覆盖、缺省跟随全局；NFO 保留自定义标签）；真实批量跨导航与刷新继续、与扫描互斥（409）、部分失败可见；真实批量取消后零写盘、头像任务只写自有媒体并回填 `<thumb>`；批量探测刷新后恢复、`only_missing` 复跑全部跳过；计划任务真实 CRUD、cron 非法保留草稿、跨路由保留编辑、启停/立即执行/取消删除与磁盘不变；服务重启后任务定义保留、后台任务不续跑、页面可重新操作。

GL-11/16/19/20/29/31 的探测/刮削任务部分与 GL-07/08 的刮削浮层部分在本阶段落地并复验；GL-12 的浮层取消、GL-21 的编辑态保留由 P5 单测/E2E 覆盖。P6 仍需按页面操作做全量对照。

## 可重复命令与接续点

```powershell
npm --prefix frontend run typecheck
npm --prefix frontend run test:unit -- --run
npm --prefix frontend run test:build
npm --prefix frontend run build
$env:PW_CHANNEL = 'msedge'
npm --prefix frontend run test:e2e -- --output=../vue-migration/artifacts/P5/full-suite
npm --prefix frontend run test:go-build
go test ./internal/scraper ./internal/server -run '^TestVueMigration(Number|Entity|Overwrite|PreviewAndRootCancellation)Contract$' -count=1 -v
go test -tags embedui ./internal/server -run 'Test(WebUI|CachePolicyHeaders)' -count=1
go vet ./... ; go vet -tags embedui ./...
$env:EMBY_MIGRATION_PHASE = 'P5'
npm --prefix frontend run test:go-regression
Remove-Item Env:EMBY_MIGRATION_PHASE
go test ./internal/server -run '^TestProbeCircuitBreaker$' -count=1
```

不要直接执行 `go test ./...`：既有 `TestRedisBehavior` 会清理本机 6379 的 DB15。所有浏览器/回归命令需在 PowerShell 中执行并显式设置环境变量（见上）。

生成证据不入库：完整浏览器结果 `artifacts/P5/full-suite/`、首轮失败报告与 trace `artifacts/P5/first-run-report/`、安全 Go 全包报告 `artifacts/P5/go-regression.json`，其余为过程目录。机器可读汇总见 `artifacts/P5/verification.json`。

下一步为 P6/VUE-14：既有 17 场景的新测试落点核对、全部页面功能对照、旧地址与异常路径、畸形/空响应与任务生命周期，并在真实 Go 服务下补桌面/手机检查；继续保留冻结基线与隔离环境。